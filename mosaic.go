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

// GenerateMosaicVideo generates a panoramic mosaic video using a worker pool.
func GenerateMosaicVideo(videoPath, outputDir string, kind Kind, cfg Config, lg *Logger) error {
	videoName := filepath.Base(videoPath)
	log := lg.With("video", videoName)
	log.Info("Starting video processing", "kind", kind)
	start := time.Now()

	// Extract frames
	frames, err := extractFrames(videoPath, lg)
	if err != nil {
		return fmt.Errorf("failed to extract frames: %w", err)
	}
	log.Info("Extracted frames", "count", len(frames))
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	// Trim black borders from all frames, replacing each Mat in place
	// (replaceMat closes the original so it isn't leaked).
	for i := range frames {
		replaceMat(frames, i, trimBlackBorders(frames[i], 10))
	}

	// Detect and normalize motion direction
	dir := detectMotionDirection(frames, cfg, lg)
	log.Info("Detected motion direction", "direction", dir)
	for i := range frames {
		replaceMat(frames, i, rotateFrame(frames[i], dir))
	}

	// Calculate transformations between consecutive frames
	transforms, refIndex := calculateTransformations(frames, cfg, lg)
	log.Info("Calculated frame transformations", "reference_frame", refIndex)

	// Calculate canvas size
	canvasWidth, canvasHeight, frameXOffset, frameYOffset := calculateCanvasSize(frames, transforms, refIndex, lg)
	log.Info("Calculated canvas dimensions",
		"width", canvasWidth,
		"height", canvasHeight,
		"x_offset", frameXOffset,
		"y_offset", frameYOffset,
	)

	// Invert each transform and apply the canvas offsets.
	invTransforms := make([]*gocv.Mat, len(transforms))
	for i, T := range transforms {
		if T == nil || T.Empty() {
			// calculateTransformations leaves transforms[i] = nil for
			// frames it couldn't align — propagate that nil through so
			// downstream skips them instead of nil-derefing.
			if T != nil {
				T.Close()
			}
			continue
		}
		inv := gocv.NewMat()
		if ok := gocv.Invert(*T, &inv, gocv.SolveDecompositionLu); ok <= 0 {
			log.Error("Failed to invert transform", "index", i, "matrix", prettyPrintMatrix(*T))
			inv.Close()
			T.Close()
			continue
		}
		tx := inv.GetDoubleAt(0, 2) + float64(frameXOffset)
		ty := inv.GetDoubleAt(1, 2) + float64(frameYOffset)
		inv.SetDoubleAt(0, 2, tx)
		inv.SetDoubleAt(1, 2, ty)

		invTransforms[i] = &inv
		T.Close()
	}
	transforms = invTransforms
	defer func() {
		for _, t := range transforms {
			if t != nil {
				t.Close()
			}
		}
	}()

	// Warp every frame onto the canvas with bounded parallelism. A nil
	// transform (a frame we couldn't align) yields an empty placeholder so
	// indices stay aligned with `frames`.
	workers := cfg.MaxWorkers
	warpedFrames := parallelMap(len(frames), workers, func(i int) gocv.Mat {
		if transforms[i] == nil {
			return gocv.NewMat()
		}
		warped := gocv.NewMat()
		// Bicubic resampling reconstructs high-frequency texture (foliage,
		// railings) with far less moiré than bilinear under a sub-pixel
		// shift/rotation.
		gocv.WarpPerspectiveWithParams(
			frames[i],
			&warped,
			*transforms[i],
			image.Pt(canvasWidth, canvasHeight),
			gocv.InterpolationCubic,
			gocv.BorderConstant,
			color.RGBA{0, 0, 0, 0},
		)
		return warped
	})
	defer func() {
		for _, f := range warpedFrames {
			if !f.Empty() {
				f.Close()
			}
		}
	}()

	if debugWritesEnabled() {
		for i, m := range warpedFrames {
			debugPath := filepath.Join(outputDir, fmt.Sprintf("warped_frame_%d.jpg", i))
			if ok := gocv.IMWrite(debugPath, m); !ok {
				log.Error("Failed to save warped frame", "index", i)
			}
		}
	}

	outputPath := filepath.Join(outputDir, kind.String()+".mp4")

	// Sweep a panorama at evenly-spaced column offsets. Static stitches
	// half the frame count (the forward-reverse loop doubles it back);
	// Dynamic uses them all (played forward once).
	totalFrames := cfg.OutputFPS * cfg.OutputLengthInSeconds
	nPanoramas := panoramaCount(kind, totalFrames)
	// The sweep spans column offsets [MinimalPixelColumnIndex, len(frames)].
	// The upper bound is the frame count by design: each frame contributes one
	// strip, so the panorama is at most that many strip-columns wide, and one
	// offset step per frame walks the brush across the full mosaic.
	selectedOffsets := linspace(cfg.MinimalPixelColumnIndex, len(warpedFrames), nPanoramas)
	log.Info("Selected offsets for mosaic", "count", len(selectedOffsets), "kind", kind)

	// Stitch each offset into a panorama with bounded parallelism.
	// parallelMap preserves offset order, so no post-sort is needed.
	panoramas := parallelMap(len(selectedOffsets), workers, func(i int) resJob {
		offset := selectedOffsets[i]
		return resJob{
			idx: offset,
			mat: stitchPanorama(videoName, warpedFrames, canvasWidth, canvasHeight, offset, cfg.FeatherWidth, lg),
		}
	})
	defer func() {
		for _, p := range panoramas {
			p.mat.Close()
		}
	}()

	// The pipeline aligned and stitched everything in a rotated space where
	// the pan runs horizontally (see rotateFrame above). Rotate each finished
	// panorama back to the clip's original orientation before sequencing —
	// without this, any non-left pan comes out mirrored (right) or sideways
	// (up/down). Left needs no rotation, so skip the needless clone+close.
	if dir != Left {
		for i := range panoramas {
			restored := rotateFrameBack(panoramas[i].mat, dir)
			panoramas[i].mat.Close()
			panoramas[i].mat = restored
		}
	}

	// Turn the panoramas into the final frame sequence (static
	// forward-reverse loop, or dynamic trim+pad played forward). cleanupSeq
	// frees any Mats the builder allocated; the panoramas are freed by defer
	// above.
	videoSeq, cleanupSeq := buildSequence(panoramas, kind, cfg)
	defer cleanupSeq()

	if err := generateVideoFromFrames(videoSeq, outputPath, cfg.OutputFPS, lg); err != nil {
		return fmt.Errorf("failed to save video: %w", err)
	}

	log.Info("Generated mosaic", "duration", time.Since(start))
	return nil
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
