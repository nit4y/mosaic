package config

import (
	"image"

	"gocv.io/x/gocv"
)

const (
	// BlurResolution is the downscale factor applied before Lucas-Kanade
	// optical flow. Tracking is more stable on a slightly blurred image.
	BlurResolution = 0.5

	// RANSAC parameters for estimateAffinePartial2D in AlignImages.
	// Defaults match OpenCV's Python defaults: confidence 0.99, ~2000
	// iterations. Lower confidence lets RANSAC converge on poor estimates
	// and was a major source of jittery transforms between frame pairs.
	RansacThreshold     = 1    // px reprojection error threshold for inliers
	RansacConfidence    = 0.99 // probability the estimate is correct
	RansacMaxIterations = 2000 // max RANSAC iterations
	RansacFlag          = 0

	// Corner detection (GoodFeaturesToTrack / Shi-Tomasi). These feed
	// Lucas-Kanade optical flow and then RANSAC; more well-distributed
	// corners produce a more robust translation estimate.
	MaxCorners    = 2000 // upper bound on detected corners per frame
	CornerQuality = 0.01 // minimum corner quality (1% of max response)
	CornerMinDist = 7    // px min distance between detected corners

	// MinimalPixelColumnIndex is the first column offset swept when
	// stitching panoramas (skips the extreme edge where alignment is
	// weakest).
	MinimalPixelColumnIndex = 10

	// FlattenVertical controls the panorama's vertical layout, applied once
	// to the accumulated transforms (not to per-pair alignment, which must
	// keep recovering true translation).
	//   true  → zero the accumulated vertical translation, so every frame
	//           sits in one horizontal band. Keeps the canvas ~one frame
	//           tall instead of staircasing into the diagonal black wedges
	//           that give the output its "smeared edge" look (see
	//           scripts/compare_mosaics.sh vs the flattened school ref).
	//   false → re-center frames on the median vertical drift, preserving
	//           genuine vertical camera motion (taller, wedge-prone canvas).
	// Default true: the target footage is a horizontal pan.
	FlattenVertical = true

	// YTranslationDamping scales the per-pair vertical translation (ty) of
	// each homography inside AlignImages. 1.0 is a no-op and the normal
	// value; it exists only as an advanced knob. Use FlattenVertical to
	// control panorama vertical layout — that is the right layer for it.
	YTranslationDamping = 1.0

	// Output settings. Total output frames = OutputFPS * OutputLengthInSeconds.
	OutputFPS             = 30
	OutputLengthInSeconds = 4

	// I/O
	InputDir = "input"

	// Concurrency guardrails.
	//
	// MaxWorkers caps the goroutines used by any single parallel stage
	// (frame warping, panorama stitching). 0 = auto (runtime.NumCPU()).
	// This is the CPU guardrail.
	MaxWorkers = 0

	// VideoConcurrency caps how many videos are processed at once. Each
	// in-flight video holds many large Mats, so the default is 1
	// (sequential = lightest on memory). Raise it to trade RAM for
	// throughput when processing a directory of clips. This is the
	// across-video memory guardrail.
	VideoConcurrency = 1

	// ProcessPoolWorkers / StitcherWorkers are the legacy per-stage worker
	// counts, kept until those stages are migrated onto MaxWorkers.
	ProcessPoolWorkers = 4
	StitcherWorkers    = 4

	// Motion directions.
	Left  = "left"
	Right = "right"
	Up    = "up"
	Down  = "down"
)

var (
	// Lucas-Kanade optical flow parameters.
	LKWinSize         = image.Pt(15, 15)
	LKMaxLevel        = 2
	LKCriteria        = gocv.NewTermCriteria(gocv.Count|gocv.EPS, 10, 0.03)
	LKFlags           = 0
	LKMinEigThreshold = 1e-4
)
