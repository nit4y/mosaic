package mosaic

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gocv.io/x/gocv"
)

// forwardReverseLoop returns a slice of length 2*N that plays the input
// forward then in reverse: [j_0, j_1, ..., j_{n-1}, j_{n-1}, ...,
// j_1, j_0], producing a seamless loop. The reversed half shares gocv.Mat
// references with the input — the caller must close each unique Mat exactly
// once after writing. If the input is empty, returns nil.
func forwardReverseLoop(jobs []resJob) []resJob {
	n := len(jobs)
	if n == 0 {
		return nil
	}
	out := make([]resJob, 0, 2*n)
	out = append(out, jobs...)
	for i := n - 1; i >= 0; i-- {
		out = append(out, jobs[i])
	}
	return out
}

// resJob represents a result from a worker in the processing pool
type resJob struct {
	idx int
	mat gocv.Mat
}

// prettyPrintMatrix prints a gocv.Mat in a human-readable format.
func prettyPrintMatrix(mat gocv.Mat) string {
	rows, cols := mat.Rows(), mat.Cols()
	if rows == 0 || cols == 0 {
		return "Empty matrix"
	}
	var b strings.Builder
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if c > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%.2f", mat.GetDoubleAt(r, c))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// debugWritesEnabled returns true when MOSAIC_DEBUG_FRAMES is set. The
// per-frame JPEG dumps are several MB of I/O each and are useful for
// diagnostics but pure noise in production / CI runs.
func debugWritesEnabled() bool {
	return os.Getenv("MOSAIC_DEBUG_FRAMES") != ""
}

// replaceMat closes the old Mat at frames[i] and writes the new one
// in. Use when an in-place transform returns a fresh Mat for each
// frame — otherwise the original frame leaks every iteration.
func replaceMat(frames []gocv.Mat, i int, next gocv.Mat) {
	old := frames[i]
	frames[i] = next
	old.Close()
}

// canvasGeometry describes the shared panorama canvas: its pixel size and the
// x/y offsets that shift every inverted transform so the leftmost/topmost frame
// lands at the origin.
type canvasGeometry struct {
	width, height    int
	xOffset, yOffset int
}

// GenerateMosaicVideo builds a single panoramic mosaic from one video and
// writes it under outputDir as <kind>.mp4. It runs the pipeline stage by stage:
// prepare frames, solve the canvas transforms, warp, sweep panoramas, restore
// orientation, and encode. Each stage owns its Mats; this function closes them.
func GenerateMosaicVideo(videoPath, outputDir string, kind Kind, cfg Config, lg *Logger) error {
	videoName := filepath.Base(videoPath)
	log := lg.With("video", videoName)
	log.Info("Starting video processing", "kind", kind)
	start := time.Now()

	// Stage 1: decode, trim borders, and rotate so the pan runs horizontally.
	frames, dir, err := prepareFrames(videoPath, cfg, lg)
	if err != nil {
		return err
	}
	defer closeMats(frames)

	// Stage 2: solve cumulative transforms and the canvas they project onto.
	transforms, geom := buildCanvasTransforms(frames, cfg, lg)
	defer closeMats(transforms)

	// Stage 3: warp every frame onto the shared canvas (bounded parallelism).
	warped := warpFrames(frames, transforms, geom, cfg)
	defer closeMats(warped)
	dumpWarpedFrames(warped, outputDir, log)

	// Stage 4: sweep column offsets into panoramas, then undo the alignment
	// rotation so the output sits in the clip's original orientation.
	panoramas := sweepPanoramas(videoName, warped, geom, kind, cfg, lg)
	defer closeResJobs(panoramas)
	restoreOrientation(panoramas, dir)

	// Stage 5: order the panoramas into the final frame sequence and encode.
	videoSeq, cleanupSeq := buildSequence(panoramas, kind, cfg)
	defer cleanupSeq()

	outputPath := filepath.Join(outputDir, kind.String()+".mp4")
	if err := generateVideoFromFrames(videoSeq, outputPath, cfg.OutputFPS, lg); err != nil {
		return fmt.Errorf("failed to save video: %w", err)
	}

	log.Info("Generated mosaic", "duration", time.Since(start))
	return nil
}

// prepareFrames decodes the video, trims black borders, detects the dominant
// pan direction, and rotates every frame so the pan runs horizontally. The
// returned frames are owned by the caller. The Direction is needed later to
// rotate the finished mosaic back to its original orientation.
func prepareFrames(videoPath string, cfg Config, lg *Logger) ([]gocv.Mat, Direction, error) {
	frames, err := extractFrames(videoPath, lg)
	if err != nil {
		return nil, Left, fmt.Errorf("failed to extract frames: %w", err)
	}
	lg.Info("Extracted frames", "count", len(frames))

	// replaceMat closes the original so the in-place transform doesn't leak it.
	for i := range frames {
		replaceMat(frames, i, trimBlackBorders(frames[i], 10))
	}

	dir := detectMotionDirection(frames, cfg, lg)
	lg.Info("Detected motion direction", "direction", dir)
	for i := range frames {
		replaceMat(frames, i, rotateFrame(frames[i], dir))
	}
	return frames, dir, nil
}

// buildCanvasTransforms computes the per-frame cumulative transforms, sizes the
// canvas, and inverts each transform onto it. The returned slice is index-aligned
// with frames (an empty Mat marks a frame that could not be aligned) and is owned
// by the caller.
func buildCanvasTransforms(frames []gocv.Mat, cfg Config, lg *Logger) ([]gocv.Mat, canvasGeometry) {
	frameToRef, refIndex := calculateTransformations(frames, cfg, lg)
	w, h, xOff, yOff := calculateCanvasSize(frames, frameToRef, refIndex, lg)
	geom := canvasGeometry{width: w, height: h, xOffset: xOff, yOffset: yOff}
	// invertTransforms consumes frameToRef and returns the ref→frame transforms
	// (shifted onto the canvas) that warpFrames needs.
	return invertTransforms(frameToRef, geom, lg), geom
}

// invertTransforms inverts each transform (frame→ref becomes ref→frame) and
// folds in the canvas offsets. It consumes transforms (closing each) and returns
// a new index-aligned slice; unalignable or non-invertible frames become empty
// Mats so the indices stay in step with the frame slice.
func invertTransforms(transforms []gocv.Mat, geom canvasGeometry, lg *Logger) []gocv.Mat {
	out := make([]gocv.Mat, len(transforms))
	for i := range transforms {
		T := transforms[i]
		if T.Empty() {
			out[i] = gocv.NewMat()
			T.Close()
			continue
		}
		inv := gocv.NewMat()
		if ok := gocv.Invert(T, &inv, gocv.SolveDecompositionLu); ok <= 0 {
			lg.Error("Failed to invert transform", "index", i, "matrix", prettyPrintMatrix(T))
			inv.Close()
			out[i] = gocv.NewMat()
			T.Close()
			continue
		}
		inv.SetDoubleAt(0, 2, inv.GetDoubleAt(0, 2)+float64(geom.xOffset))
		inv.SetDoubleAt(1, 2, inv.GetDoubleAt(1, 2)+float64(geom.yOffset))
		out[i] = inv
		T.Close()
	}
	return out
}

// warpFrames projects every frame onto the shared canvas with bounded
// parallelism. A frame with an empty (unalignable) transform yields an empty
// placeholder so indices stay aligned with frames. Results are caller-owned.
func warpFrames(frames, transforms []gocv.Mat, geom canvasGeometry, cfg Config) []gocv.Mat {
	return parallelMap(len(frames), cfg.MaxWorkers, func(i int) gocv.Mat {
		if transforms[i].Empty() {
			return gocv.NewMat()
		}
		warped := gocv.NewMat()
		// Bicubic resampling reconstructs high-frequency texture (foliage,
		// railings) with far less moiré than bilinear under a sub-pixel
		// shift/rotation.
		gocv.WarpPerspectiveWithParams(
			frames[i],
			&warped,
			transforms[i],
			image.Pt(geom.width, geom.height),
			gocv.InterpolationCubic,
			gocv.BorderConstant,
			color.RGBA{0, 0, 0, 0},
		)
		return warped
	})
}

// sweepPanoramas stitches one panorama per evenly-spaced column offset with
// bounded parallelism. Static stitches half the frame count (the forward-reverse
// loop doubles it back); Dynamic uses them all (played forward once). The sweep
// spans offsets [MinimalPixelColumnIndex, len(warped)]: each frame contributes
// one strip, so one offset step per frame walks the brush across the full
// mosaic. parallelMap preserves offset order, so no post-sort is needed.
func sweepPanoramas(videoName string, warped []gocv.Mat, geom canvasGeometry, kind Kind, cfg Config, lg *Logger) []resJob {
	totalFrames := cfg.OutputFPS * cfg.OutputLengthInSeconds
	nPanoramas := panoramaCount(kind, totalFrames)
	offsets := linspace(cfg.MinimalPixelColumnIndex, len(warped), nPanoramas)
	lg.Info("Selected offsets for mosaic", "count", len(offsets), "kind", kind)

	return parallelMap(len(offsets), cfg.MaxWorkers, func(i int) resJob {
		return resJob{
			idx: offsets[i],
			mat: stitchPanorama(videoName, warped, geom.width, geom.height, offsets[i], cfg.FeatherWidth, lg),
		}
	})
}

// restoreOrientation rotates each finished panorama back to the clip's original
// orientation. The pipeline aligns and stitches in a rotated space where the pan
// runs horizontally, so without this any non-left pan would come out mirrored
// (right) or sideways (up/down). Left needs no rotation, so the work is skipped.
func restoreOrientation(panoramas []resJob, dir Direction) {
	if dir == Left {
		return
	}
	for i := range panoramas {
		restored := rotateFrameBack(panoramas[i].mat, dir)
		panoramas[i].mat.Close()
		panoramas[i].mat = restored
	}
}

// dumpWarpedFrames writes each warped frame as a JPEG when MOSAIC_DEBUG_FRAMES
// is set; it is a no-op otherwise (the dumps are several MB of I/O each).
func dumpWarpedFrames(warped []gocv.Mat, outputDir string, lg *Logger) {
	if !debugWritesEnabled() {
		return
	}
	for i, m := range warped {
		debugPath := filepath.Join(outputDir, fmt.Sprintf("warped_frame_%d.jpg", i))
		if ok := gocv.IMWrite(debugPath, m); !ok {
			lg.Error("Failed to save warped frame", "index", i)
		}
	}
}

// closeMats closes every Mat in s. Empty placeholder Mats are allocated too, so
// they are closed unconditionally to avoid leaking them.
func closeMats(s []gocv.Mat) {
	for i := range s {
		s[i].Close()
	}
}

// closeResJobs closes the Mat carried by every resJob in s.
func closeResJobs(s []resJob) {
	for i := range s {
		s[i].mat.Close()
	}
}

// GenerateVideos processes all .mp4 videos in the default input
// directory ("input/") and writes mosaics under "output/". Convenience
// wrapper for the CLI; tests and external callers should use
// GenerateVideosFromDir to keep paths injectable. lg may be nil (silent).
func GenerateVideos(cfg Config, lg *Logger) error {
	return GenerateVideosFromDir(cfg.InputDir, cfg.OutputDir, Static, cfg, lg)
}

// GenerateVideosFromDir generates a mosaic of the given Kind for every
// video in inputDir, writing each under outputDir/<video name>/. Videos are
// processed with bounded concurrency (config.VideoConcurrency); it returns
// the first error encountered, after attempting every video. lg is the
// caller-supplied logger (nil or non-verbose = silent).
func GenerateVideosFromDir(inputDir, outputDir string, kind Kind, cfg Config, lg *Logger) error {
	videoFiles, err := listVideoFiles(inputDir)
	if err != nil {
		return err
	}
	if len(videoFiles) == 0 {
		return fmt.Errorf("no video files found in input directory %q", inputDir)
	}

	lg.Info("Found video files to process",
		"count", len(videoFiles), "input_dir", inputDir, "kind", kind)

	// Each in-flight video holds many large Mats, so VideoConcurrency
	// defaults to 1 (sequential = lightest on memory). Raise it to trade
	// RAM for throughput — see config for the guardrail rationale.
	errs := parallelMap(len(videoFiles), cfg.VideoConcurrency, func(i int) error {
		videoPath := videoFiles[i]
		log := lg.With("video", filepath.Base(videoPath))

		videoOutputDir := filepath.Join(outputDir, filepath.Base(videoPath))
		if err := os.MkdirAll(videoOutputDir, 0o755); err != nil {
			log.Error("Failed to create output directory", "error", err)
			return err
		}
		if err := GenerateMosaicVideo(videoPath, videoOutputDir, kind, cfg, lg); err != nil {
			log.Error("Failed to generate mosaic", "error", err, "kind", kind)
			return err
		}
		log.Info("Generated mosaic", "kind", kind)
		return nil
	})

	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// listVideoFiles returns sorted absolute (or input-dir-relative) paths
// for video files in dir. Sorted output gives deterministic processing
// order — useful for both reproducible debugging and stable tests.
func listVideoFiles(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read input directory %q: %w", dir, err)
	}
	var videoFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		ext := filepath.Ext(file.Name())
		if ext == ".mp4" || ext == ".avi" || ext == ".mov" {
			videoFiles = append(videoFiles, filepath.Join(dir, file.Name()))
		}
	}
	sort.Strings(videoFiles)
	return videoFiles, nil
}
