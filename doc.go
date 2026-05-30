// Package mosaic turns a panning video into a wide panoramic mosaic using the
// VideoBrush strip-mosaicing technique (Peleg et al.), implemented on top of
// GoCV / OpenCV.
//
// # Pipeline
//
// Each video flows through a fixed set of stages:
//
//  1. Prepare  — decode frames, trim black borders, detect the dominant pan
//     direction, and rotate so the motion runs horizontally.
//  2. Align    — for each adjacent pair, detect Shi-Tomasi corners, track them
//     with Lucas-Kanade optical flow, and fit a partial-affine transform via
//     RANSAC; transforms are reduced to horizontal translation and accumulated
//     relative to a central reference frame.
//  3. Warp     — project every frame onto a shared canvas with bounded
//     parallelism.
//  4. Stitch   — sweep a column offset across the aligned frames, painting the
//     strip each frame contributes, with optional feather-blending.
//  5. Sequence — emit a Static (forward + reverse loop) or Dynamic (forward
//     "video brush") mosaic and write it as MP4, restoring the original
//     orientation first.
//
// # Usage
//
// Start from [DefaultConfig], override only the fields you need, and call one
// of the Generate functions. Logging is opt-in: build a [Logger] from your own
// *slog.Logger (or pass nil for silence).
//
//	cfg := mosaic.DefaultConfig()
//	cfg.FeatherWidth = 4
//	log := mosaic.NewLogger(slog.Default(), true)
//	if err := mosaic.GenerateVideos(cfg, log); err != nil {
//		// handle error
//	}
//
// # Requirements
//
// GoCV requires OpenCV 4.x installed locally; see https://gocv.io/getting-started/.
//
// # Concurrency
//
// Config.MaxWorkers caps the goroutines used per parallel stage (0 = NumCPU),
// and Config.VideoConcurrency caps how many videos are processed at once. Each
// in-flight video holds many large image buffers, so VideoConcurrency defaults
// to 1 to stay light on memory.
package mosaic
