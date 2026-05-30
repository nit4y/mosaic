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

	// EdgeStripWidth bounded the synthetic leading/trailing strips in the
	// old stitcher. Retained until the stitcher is reworked.
	EdgeStripWidth = 64

	// YTranslationDamping scales the per-pair vertical translation (ty)
	// of each homography before accumulation.
	//   1.0 → keep ty as-is (preserve real vertical motion)
	//   0.0 → fully remove ty (panorama stays at a constant y)
	// A horizontal pan mosaic wants this low: undamped ty accumulates into
	// large vertical drift, inflating the canvas with diagonal black
	// wedges. See scripts/compare_mosaics.sh output vs the reference.
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
