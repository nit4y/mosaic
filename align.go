package mosaic

import (
	"image"
	"math"

	"gocv.io/x/gocv"
)

// applyBlur downscales the image by blurResolution, then upscales it back to
// its original size, producing a simple blur.
func applyBlur(img gocv.Mat, blurResolution float64) gocv.Mat {
	h := img.Rows()
	w := img.Cols()

	// compute downscaled dimensions (at least 1×1)
	smallW := int(math.Max(1, float64(w)*blurResolution))
	smallH := int(math.Max(1, float64(h)*blurResolution))

	// downscale
	small := gocv.NewMat()
	gocv.Resize(img, &small, image.Pt(smallW, smallH), 0, 0, gocv.InterpolationLinear)

	// upscale back to original size
	result := gocv.NewMat()
	gocv.Resize(small, &result, image.Pt(w, h), 0, 0, gocv.InterpolationLinear)

	// free intermediate Mat to avoid memory leak
	small.Close()

	return result
}

// detectCorners returns trackable Shi-Tomasi corners (GoodFeaturesToTrack)
// from a grayscale image as a slice of points plus the N×1 CV_32FC2 Mat
// that calcOpticalFlowPyrLK expects.
func detectCorners(gray gocv.Mat, maxCorners int, quality float64, minDist float64) ([]gocv.Point2f, gocv.Mat) {
	corners := gocv.NewMat()
	if err := gocv.GoodFeaturesToTrack(gray, &corners, maxCorners, quality, minDist); err != nil {
		corners.Close()
		return nil, gocv.NewMat()
	}
	n := corners.Rows()
	pts := make([]gocv.Point2f, n)
	out := gocv.NewMatWithSize(n, 1, gocv.MatTypeCV32FC2)
	for i := 0; i < n; i++ {
		// GoodFeaturesToTrack returns Nx1 CV_32FC2; each entry is
		// (x, y) as a 2-float vector.
		v := corners.GetVecfAt(i, 0)
		pts[i] = gocv.Point2f{X: v[0], Y: v[1]}
		out.SetFloatAt(i, 0, v[0])
		out.SetFloatAt(i, 1, v[1])
	}
	corners.Close()
	return pts, out
}

// alignImages aligns img2 to img1 using Shi-Tomasi corner detection
// + Lucas-Kanade optical flow + RANSAC affine. Returns a 3×3
// homogeneous Mat with horizontal-only motion (no rotation/skew, unit
// scale, Y-damped per config) and the motion direction.
func alignImages(img1, img2 gocv.Mat, calcDirection bool, cfg Config, lg *Logger) (*gocv.Mat, Direction) {
	log := lg.With("operation", "align_images")

	// convert to grayscale
	gray1 := gocv.NewMat()
	gray2 := gocv.NewMat()
	defer gray1.Close()
	defer gray2.Close()
	gocv.CvtColor(img1, &gray1, gocv.ColorBGRToGray)
	gocv.CvtColor(img2, &gray2, gocv.ColorBGRToGray)

	// Detect Shi-Tomasi corners in gray1 — these are what
	// Lucas-Kanade will track from frame 1 to frame 2.
	ptsList, prevPtsMat := detectCorners(gray1, cfg.MaxCorners, cfg.CornerQuality, float64(cfg.CornerMinDist))
	defer prevPtsMat.Close()
	if len(ptsList) == 0 {
		log.Error("No corners detected in gray1")
		return nil, Left
	}

	// blur for LK stability
	b1 := applyBlur(gray1, cfg.BlurResolution)
	b2 := applyBlur(gray2, cfg.BlurResolution)
	defer b1.Close()
	defer b2.Close()

	// allocate Mats for nextPts, status, error
	nextPtsMat := gocv.NewMat()
	defer nextPtsMat.Close()
	status := gocv.NewMat()
	defer status.Close()
	errMat := gocv.NewMat()
	defer errMat.Close()

	// compute sparse optical flow (Lucas-Kanade)
	gocv.CalcOpticalFlowPyrLKWithParams(
		b1,
		b2,
		prevPtsMat,
		nextPtsMat,
		&status,
		&errMat,
		cfg.LKWinSize,
		cfg.LKMaxLevel,
		cfg.LKCriteria,
		cfg.LKFlags,
		cfg.LKMinEigThreshold,
	)

	// filter valid correspondences
	var valid1, valid2 []gocv.Point2f
	rows := status.Rows()
	for i := 0; i < rows; i++ {
		if status.GetUCharAt(i, 0) == 1 {
			valid1 = append(valid1, ptsList[i])
			vec := nextPtsMat.GetVecfAt(i, 0)
			valid2 = append(valid2, gocv.Point2f{X: vec[0], Y: vec[1]})
		}
	}

	// estimate affine partial via RANSAC
	v1 := gocv.NewPoint2fVectorFromPoints(valid1)
	defer v1.Close()
	v2 := gocv.NewPoint2fVectorFromPoints(valid2)
	defer v2.Close()
	inliersMask := gocv.NewMat()
	defer inliersMask.Close()
	aff := gocv.EstimateAffinePartial2DWithParams(
		v1,
		v2,
		inliersMask,
		int(gocv.HomographyMethodRANSAC),
		float64(cfg.RansacThreshold),
		uint(cfg.RansacMaxIterations),
		cfg.RansacConfidence,
		uint(cfg.RansacFlag),
	)
	defer aff.Close()

	// compute direction if needed
	dir := Left
	if calcDirection {
		dir = calcMotionDirection(valid1, valid2)
	}

	if aff.Empty() || aff.Rows() < 2 || aff.Cols() < 3 {
		log.Error("Failed to estimate affine transformation",
			"empty", aff.Empty(),
			"rows", aff.Rows(),
			"cols", aff.Cols())
		return nil, dir
	}

	// convert to homogeneous (Hh shares storage with H — they are the
	// same Mat returned by stabilizeTranslation, so we close it only on
	// the failure path).
	H := toHomogeneous(aff)
	Hh := stabilizeTranslation(H, cfg.YTranslationDamping)
	if Hh.Empty() {
		log.Error("Failed to stabilize horizontal motion - empty matrix")
		Hh.Close()
		return nil, dir
	}
	return &Hh, dir
}
