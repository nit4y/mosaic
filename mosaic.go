package mosaic

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nit4y/mosaic/internal/config"
	"github.com/nit4y/mosaic/internal/logger"
	"gocv.io/x/gocv"
)

// pingPongResJobs returns a slice of length 2*N that plays the input
// forward then in reverse: [j_0, j_1, ..., j_{n-1}, j_{n-1}, ...,
// j_1, j_0]. The reversed half shares gocv.Mat references with the
// input — the caller must close each unique Mat exactly once after
// writing. If the input is empty, returns nil.
func pingPongResJobs(jobs []resJob) []resJob {
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
	var result string
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			val := mat.GetDoubleAt(r, c)
			if c > 0 {
				result += " "
			}
			result += fmt.Sprintf("%.2f", val)
		}
		result += "\n"
	}
	return result
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
func GenerateMosaicVideo(videoPath, outputDir string, kind Kind) error {
	videoName := filepath.Base(videoPath)
	log := logger.WithVideo(videoName)
	log.Info("Starting video processing", "kind", kind)
	start := time.Now()

	// Extract frames
	frames, err := ExtractFrames(videoPath)
	if err != nil {
		return fmt.Errorf("failed to extract frames: %w", err)
	}
	log.Info("Extracted frames", "count", len(frames))
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	// Trim black borders from all frames. The in-place assignment used
	// to leak the original Mat every iteration.
	for i := range frames {
		replaceMat(frames, i, TrimBlackBorders(frames[i], 10))
	}

	// Detect and normalize motion direction
	dir := DetectMotionDirection(frames)
	log.Info("Detected motion direction", "direction", dir)
	for i := range frames {
		replaceMat(frames, i, RotateFrame(frames[i], dir))
	}

	// Calculate transformations between consecutive frames
	transforms, refIndex := CalculateTransformations(frames)
	log.Info("Calculated frame transformations", "reference_frame", refIndex)

	// Calculate canvas size
	canvasWidth, canvasHeight, frameXOffset, frameYOffset := CalculateCanvasSize(frames, transforms, refIndex)
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
			// CalculateTransformations leaves transforms[i] = nil for
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
	workers := config.MaxWorkers
	warpedFrames := parallelMap(len(frames), workers, func(i int) gocv.Mat {
		if transforms[i] == nil {
			return gocv.NewMat()
		}
		warped := gocv.NewMat()
		// Bicubic resampling (was bilinear) reconstructs high-frequency
		// texture — foliage, railings — with far less of the cross-hatch
		// moiré bilinear leaves under a sub-pixel shift/rotation.
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
	// half the frame count (ping-pong doubles it back); Dynamic uses them
	// all (played forward once).
	totalFrames := config.OutputFPS * config.OutputLengthInSeconds
	nPanoramas := panoramaCount(kind, totalFrames)
	selectedIndices := linspace(config.MinimalPixelColumnIndex, len(warpedFrames), nPanoramas)
	log.Info("Selected offsets for mosaic", "count", len(selectedIndices), "kind", kind)

	// Stitch each offset into a panorama with bounded parallelism.
	// parallelMap preserves offset order, so no post-sort is needed.
	panoramas := parallelMap(len(selectedIndices), workers, func(i int) resJob {
		offset := selectedIndices[i]
		return resJob{
			idx: offset,
			mat: StitchPanorama(videoName, warpedFrames, canvasWidth, canvasHeight, offset),
		}
	})
	defer func() {
		for _, p := range panoramas {
			p.mat.Close()
		}
	}()

	// Turn the panoramas into the final frame sequence (static ping-pong,
	// or dynamic trim+pad played forward). cleanupSeq frees any Mats the
	// builder allocated; the panoramas themselves are freed by the defer
	// above.
	videoSeq, cleanupSeq := buildSequence(panoramas, kind)
	defer cleanupSeq()

	if err := GenerateVideoFromFrames(videoSeq, outputPath, config.OutputFPS); err != nil {
		return fmt.Errorf("failed to save video: %w", err)
	}

	log.Info("Generated mosaic", "duration", time.Since(start))
	return nil
}

// GenerateVideos processes all .mp4 videos in the default input
// directory ("input/") and writes mosaics under "output/". Convenience
// wrapper for the CLI; tests and external callers should use
// GenerateVideosFromDir to keep paths injectable.
func GenerateVideos() error {
	return GenerateVideosFromDir(config.InputDir, "output", Static)
}

// GenerateVideosFromDir generates a mosaic of the given Kind for every
// video in inputDir, writing each under outputDir/<video name>/. Videos are
// processed with bounded concurrency (config.VideoConcurrency); it returns
// the first error encountered, after attempting every video.
func GenerateVideosFromDir(inputDir, outputDir string, kind Kind) error {
	videoFiles, err := listVideoFiles(inputDir)
	if err != nil {
		return err
	}
	if len(videoFiles) == 0 {
		return fmt.Errorf("no video files found in input directory %q", inputDir)
	}

	logger.Log.Info("Found video files to process",
		"count", len(videoFiles), "input_dir", inputDir, "kind", kind)

	// Each in-flight video holds many large Mats, so VideoConcurrency
	// defaults to 1 (sequential = lightest on memory). Raise it to trade
	// RAM for throughput — see config for the guardrail rationale.
	errs := parallelMap(len(videoFiles), config.VideoConcurrency, func(i int) error {
		videoPath := videoFiles[i]
		log := logger.WithVideo(filepath.Base(videoPath))

		videoOutputDir := filepath.Join(outputDir, filepath.Base(videoPath))
		if err := os.MkdirAll(videoOutputDir, 0o755); err != nil {
			log.Error("Failed to create output directory", "error", err)
			return err
		}
		if err := GenerateMosaicVideo(videoPath, videoOutputDir, kind); err != nil {
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
