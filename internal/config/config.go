package config

import (
	"image"

	"gocv.io/x/gocv"
)

const (
	// Harris corner detector parameters
	HarrisBlockSize    = 7    // Neighborhood size
	HarrisApertureSize = 9    // Aperture parameter for Sobel
	HarrisK            = 0.05 // Harris detector free parameter

	// Lucas-Kanade optical flow
	BlurResolution = 0.5 // Downscale factor for images

	// RANSAC
	RansacThreshold     = 1   // Threshold for inlier determination
	RansacConfidence    = 0.5 // Confidence level for RANSAC
	RansacMaxIterations = 100 // Maximum number of RANSAC iterations
	RansacFlag          = 0

	// Stitching
	MinimalPixelColumnIndex = 10 // Minimal column index for overlapping region

	// YTranslationDamping scales the per-pair vertical translation
	// component (ty) of each homography before accumulation.
	//   1.0 → keep ty as-is (preserve real vertical motion)
	//   0.0 → fully remove ty (panorama stays at a constant y)
	// Default 1.0 preserves the camera's Y motion, so the canvas
	// height spans the true (maxY - minY) range and frame strips
	// land at their correct y indentation. Set lower if accumulated
	// per-pair ty noise is dominating real motion in a particular
	// video.
	YTranslationDamping = 1.0

	// Output settings
	OutputFPS             = 15 // Frames per second for output video
	OutputLengthInSeconds = 1  // Length of output video in seconds
	StartFrame            = 10 // First frame index to process

	// I/O and concurrency
	InputDir           = "input"
	OutputDir          = "my_output"
	ProcessPoolWorkers = 4
	ThreadPoolWorkers  = 2

	StitcherWorkers = 4

	// Directions
	Left  = "left"
	Right = "right"
	Up    = "up"
	Down  = "down"

	// Strategies
	DocsImagesStrategy = "docs_images"
	VideosStrategy     = "videos"
)

var (
	// Lucas-Kanade parameters
	LKWinSize         = image.Pt(15, 15)
	LKMaxLevel        = 2
	LKCriteria        = gocv.NewTermCriteria(gocv.Count|gocv.EPS, 10, 0.03)
	LKFlags           = 0
	LKMinEigThreshold = 1e-4
	LKBlurKernelSize  = image.Pt(5, 5)

	// Dynamic mosaics video filenames
	DynamicMosaics = []string{"Trees.mp4", "Iguazu.mp4"}
)
