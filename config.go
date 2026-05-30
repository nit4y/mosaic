package mosaic

import (
	"image"

	"gocv.io/x/gocv"
)

// Config holds every tunable parameter for the mosaic pipeline. Start from
// DefaultConfig and override only the fields you care about, then pass the
// result to the Generate* functions. The zero value is NOT usable — always
// build on DefaultConfig.
type Config struct {
	// BlurResolution is the downscale factor applied before Lucas-Kanade
	// optical flow; tracking is more stable on a slightly blurred image.
	BlurResolution float64

	// Lucas-Kanade optical flow parameters. Larger windows/levels and
	// tighter criteria trade speed for sub-pixel tracking accuracy, which
	// feeds the RANSAC rotation/translation estimate.
	LKWinSize         image.Point
	LKMaxLevel        int
	LKCriteria        gocv.TermCriteria
	LKFlags           int
	LKMinEigThreshold float64

	// RANSAC parameters for estimateAffinePartial2D in AlignImages.
	RansacThreshold     int     // px reprojection error threshold for inliers
	RansacConfidence    float64 // probability the estimate is correct
	RansacMaxIterations int     // max RANSAC iterations
	RansacFlag          int

	// Corner detection (Shi-Tomasi / GoodFeaturesToTrack) feeding LK.
	MaxCorners    int     // upper bound on detected corners per frame
	CornerQuality float64 // minimum corner quality (fraction of max response)
	CornerMinDist int     // px min distance between detected corners

	// MinimalPixelColumnIndex is the first column offset swept when
	// stitching panoramas (skips the extreme edge where alignment is weakest).
	MinimalPixelColumnIndex int

	// FlattenVertical, when true, zeroes the accumulated vertical
	// translation so every frame sits in one horizontal band (no diagonal
	// wedges) — for purely horizontal pans. When false it re-centers on the
	// median vertical drift, preserving genuine vertical motion.
	FlattenVertical bool

	// YTranslationDamping scales the per-pair vertical translation (ty) of
	// each homography inside AlignImages. 1.0 is the normal no-op value;
	// use FlattenVertical to control panorama vertical layout.
	YTranslationDamping float64

	// FeatherWidth is the px width of the linear cross-fade at each strip
	// seam in stitching. 0 = hard seams; a few px hides seam tearing.
	FeatherWidth int

	// CropToCoveredBand, when true, crops the output vertically to the band
	// of rows well-covered in every panorama, removing the diagonal wedges
	// that FlattenVertical=false can leave.
	CropToCoveredBand bool

	// CoverageThreshold is the minimum fraction of non-black pixels a row
	// must have (per panorama's content width) to be kept when
	// CropToCoveredBand is enabled.
	CoverageThreshold float64

	// Output video settings. Total output frames = OutputFPS * OutputLengthInSeconds.
	OutputFPS             int
	OutputLengthInSeconds int

	// Default I/O directories for the GenerateVideos convenience wrapper.
	InputDir  string
	OutputDir string

	// Concurrency guardrails. MaxWorkers caps goroutines per parallel stage
	// (0 = auto = runtime.NumCPU()). VideoConcurrency caps how many videos
	// are processed at once (default 1 = lightest on memory).
	MaxWorkers       int
	VideoConcurrency int
}

// DefaultConfig returns the tuned baseline configuration. Override fields on
// the returned value to customise the pipeline.
func DefaultConfig() Config {
	return Config{
		BlurResolution: 0.5,

		LKWinSize:         image.Pt(21, 21),
		LKMaxLevel:        3,
		LKCriteria:        gocv.NewTermCriteria(gocv.Count|gocv.EPS, 30, 0.01),
		LKFlags:           0,
		LKMinEigThreshold: 1e-4,

		RansacThreshold:     1,
		RansacConfidence:    0.99,
		RansacMaxIterations: 2000,
		RansacFlag:          0,

		MaxCorners:    2000,
		CornerQuality: 0.01,
		CornerMinDist: 7,

		MinimalPixelColumnIndex: 10,

		FlattenVertical:     false,
		YTranslationDamping: 1.0,

		FeatherWidth:      2,
		CropToCoveredBand: false,
		CoverageThreshold: 0.97,

		OutputFPS:             30,
		OutputLengthInSeconds: 4,

		InputDir:  "input",
		OutputDir: "output",

		MaxWorkers:       0,
		VideoConcurrency: 1,
	}
}
