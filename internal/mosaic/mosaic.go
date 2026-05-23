package mosaic

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/nit4y/go-panoramic-mosaic/internal/config"
	"github.com/nit4y/go-panoramic-mosaic/internal/logger"
	"gocv.io/x/gocv"
)

// StabilizeHorizontalMotion removes rotational components from a 3×3 transform,
// preserving only horizontal translation.
func StabilizeHorizontalMotion(matrix gocv.Mat) gocv.Mat {
	// zero out rotational terms
	matrix.SetDoubleAt(0, 1, 0)
	matrix.SetDoubleAt(1, 0, 0)
	return matrix
}

// StabilizeNoScale zeroes out any scale in rows 0 and 1 of a 3×3 matrix,
// so after this call the diagonal entries are 1.0 (unit scale).
func StabilizeScale(mat gocv.Mat) gocv.Mat {
	// set X scale to 1
	mat.SetDoubleAt(0, 0, 1.0)
	// set Y scale to 1
	mat.SetDoubleAt(1, 1, 1.0)
	return mat
}

func StablizeTranslation(mat gocv.Mat) gocv.Mat {
	mat = StabilizeScale(StabilizeHorizontalMotion(mat))
	return DampYTranslation(mat, config.YTranslationDamping)
}

// DampYTranslation scales the ty component (element [1,2]) of a 3×3
// affine homography by `factor`. factor=1.0 is a no-op; factor=0.0
// removes vertical translation entirely. Mutates the input Mat in
// place and returns it (consistent with the other stabilize helpers).
func DampYTranslation(mat gocv.Mat, factor float64) gocv.Mat {
	if mat.Empty() || mat.Rows() < 2 || mat.Cols() < 3 {
		return mat
	}
	ty := mat.GetDoubleAt(1, 2)
	mat.SetDoubleAt(1, 2, ty*factor)
	return mat
}

// ApplyBlur downscales the image by config.BlurResolution, then upscales it
// back to its original size, producing a simple blur.
func ApplyBlur(img gocv.Mat) gocv.Mat {
	h := img.Rows()
	w := img.Cols()

	// compute downscaled dimensions (at least 1×1)
	smallW := int(math.Max(1, float64(w)*config.BlurResolution))
	smallH := int(math.Max(1, float64(h)*config.BlurResolution))

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

// ToHomogeneous converts a 2×3 affine transformation Mat into a 3×3 homogeneous Mat.
// affine must be a Mat of size 2×3.
func ToHomogeneous(affine gocv.Mat) gocv.Mat {
	// Create a new 3×3 Mat with the same type as the affine input
	dtype := affine.Type()
	h := gocv.NewMatWithSize(3, 3, dtype)

	// Copy the 2×3 affine values into the top two rows of the 3×3
	for r := 0; r < 2; r++ {
		for c := 0; c < 3; c++ {
			val := affine.GetDoubleAt(r, c)
			h.SetDoubleAt(r, c, val)
		}
	}

	// Set the last row to [0, 0, 1]
	h.SetDoubleAt(2, 0, 0)
	h.SetDoubleAt(2, 1, 0)
	h.SetDoubleAt(2, 2, 1)

	return h
}

// CalcMotionDirection estimates the dominant motion direction from two
// corresponding slices of points. Returns "left", "right", "up", or "down".
func CalcMotionDirection(pts1, pts2 []gocv.Point2f) string {
	n := len(pts1)
	if n == 0 {
		return config.Left // default if no points
	}
	var sumDx, sumDy float64
	for i := 0; i < n; i++ {
		dx := float64(pts2[i].X - pts1[i].X)
		dy := float64(pts2[i].Y - pts1[i].Y)
		sumDx += dx
		sumDy += dy
	}
	// compute mean displacement
	dxMean := sumDx / float64(n)
	dyMean := sumDy / float64(n)

	// pick dominant axis
	if math.Abs(dxMean) > math.Abs(dyMean) {
		if dxMean > 0 {
			return config.Right
		}
		return config.Left
	} else {
		if dyMean > 0 {
			return config.Down
		}
		return config.Up
	}
}

func KeyPointsToMat(keypoints []gocv.KeyPoint) gocv.Mat {
	points := gocv.NewMatWithSize(len(keypoints), 1, gocv.MatTypeCV32FC2)
	for i, kp := range keypoints {
		points.SetFloatAt(i, 0, float32(kp.X))
		points.SetFloatAt(i, 1, float32(kp.Y))
	}
	return points
}

// AlignImages aligns img2 to img1 using ORB keypoints + Lucas-Kanade optical flow.
// Returns a 3×3 homogeneous Mat with horizontal-only motion and the motion direction.
func AlignImages(img1, img2 gocv.Mat, calcDirection bool) (*gocv.Mat, string) {
	log := logger.WithOperation("align_images")

	// convert to grayscale
	gray1 := gocv.NewMat()
	gray2 := gocv.NewMat()
	defer gray1.Close()
	defer gray2.Close()
	gocv.CvtColor(img1, &gray1, gocv.ColorBGRToGray)
	gocv.CvtColor(img2, &gray2, gocv.ColorBGRToGray)

	// detect ORB keypoints on gray1
	orb := gocv.NewORBWithParams(500, 1.2, 8, 31, 0, 2, gocv.ORBScoreTypeHarris, 31, 20)
	defer orb.Close()
	kps := orb.Detect(gray1)

	// build slice of points
	ptsList := make([]gocv.Point2f, len(kps))
	for i, kp := range kps {
		ptsList[i] = gocv.Point2f{X: float32(kp.X), Y: float32(kp.Y)}
	}

	// convert slice to Point2fVector then to Mat
	prevPtsMat := KeyPointsToMat(kps)
	defer prevPtsMat.Close()

	// blur for LK stability
	b1 := ApplyBlur(gray1)
	b2 := ApplyBlur(gray2)
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
		config.LKWinSize,
		config.LKMaxLevel,
		config.LKCriteria,
		config.LKFlags,
		config.LKMinEigThreshold,
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
		float64(config.RansacThreshold),
		config.RansacMaxIterations,
		config.RansacConfidence,
		config.RansacFlag,
	)
	defer aff.Close()

	// compute direction if needed
	dir := config.Left
	if calcDirection {
		dir = CalcMotionDirection(valid1, valid2)
	}

	if aff.Empty() || aff.Rows() < 2 || aff.Cols() < 3 {
		log.Error("Failed to estimate affine transformation",
			"empty", aff.Empty(),
			"rows", aff.Rows(),
			"cols", aff.Cols())
		return nil, dir
	}

	// convert to homogeneous (Hh shares storage with H — they are the
	// same Mat returned by StablizeTranslation, so we close it only on
	// the failure path).
	H := ToHomogeneous(aff)
	Hh := StablizeTranslation(H)
	if Hh.Empty() {
		log.Error("Failed to stabilize horizontal motion - empty matrix")
		Hh.Close()
		return nil, dir
	}
	return &Hh, dir
}

// ExtractFrames extracts all frames from a video file and returns them as a slice of Mats.
func ExtractFrames(videoPath string) ([]gocv.Mat, error) {
	log := logger.WithVideo(filepath.Base(videoPath))
	log.Info("Opening video file")

	video, err := gocv.VideoCaptureFile(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open video: %w", err)
	}
	defer video.Close()

	if !video.IsOpened() {
		// VideoCaptureFile can return nil error while still failing to
		// open the underlying backend (missing codec, corrupt header,
		// unsupported container) — IsOpened catches that case.
		return nil, fmt.Errorf("video capture failed to open: %s", videoPath)
	}

	fps := video.Get(gocv.VideoCaptureFPS)
	frameCount := int(video.Get(gocv.VideoCaptureFrameCount))
	log.Info("Video properties", "fps", fps, "total_frames", frameCount)

	// Cap initial capacity to avoid huge allocations if the container
	// reports a bogus frame count.
	cap := frameCount
	if cap < 0 || cap > 100000 {
		cap = 0
	}
	frames := make([]gocv.Mat, 0, cap)
	frame := gocv.NewMat()
	defer frame.Close()

	for {
		if ok := video.Read(&frame); !ok {
			break
		}
		if frame.Empty() {
			continue
		}
		frames = append(frames, frame.Clone())
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames could be decoded from %s", videoPath)
	}

	return frames, nil
}

// Median returns the median value of the input slice.
// If the slice is empty, it returns 0.
func Median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	// Copy so original slice isn’t modified
	sorted := make([]float64, n)
	copy(sorted, xs)
	sort.Float64s(sorted)

	mid := n / 2
	if n%2 == 0 {
		// even length: average two middle values
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	// odd length: return the middle value
	return sorted[mid]
}

// CalculateTransformations computes cumulative homographies aligning each frame
// to the middle (reference) frame, then recenters them by the median vertical shift.
func CalculateTransformations(frames []gocv.Mat) ([]*gocv.Mat, int) {
	log := logger.WithOperation("calculate_transformations")
	n := len(frames)
	log.Info("Starting transformation calculations", "frame_count", n)

	if n == 0 {
		log.Error("No frames provided for transformation calculation")
		return nil, -1
	}

	// 1) reference index is the middle frame
	refIdx := n / 2

	// 2) allocate output slice
	transforms := make([]*gocv.Mat, n)
	yTranslations := make([]float64, 0, n)

	// 3) identity homography for the reference frame
	id := gocv.Eye(3, 3, gocv.MatTypeCV64F)
	transforms[refIdx] = &id
	yTranslations = append(yTranslations, 0.0)

	// 4) accumulate to the right of refIdx
	accum := id.Clone() // running product
	for i := refIdx + 1; i < n; i++ {
		H, _ := AlignImages(frames[i-1], frames[i], true)
		if H == nil || H.Empty() {
			log.Error("Failed to align frames for right side", "i", i)
			if H != nil {
				H.Close()
			}
			// Keep transforms[i] = nil; downstream code must guard.
			continue
		}
		// accum = accum @ H
		tmp := gocv.NewMat()
		emptyC := gocv.NewMat()
		gocv.Gemm(accum, *H, 1.0, emptyC, 0.0, &tmp, 0)
		emptyC.Close()
		accum.Close()
		H.Close()
		accum = tmp

		// clone for output
		cl := accum.Clone()
		transforms[i] = &cl
		yTranslations = append(yTranslations, accum.GetDoubleAt(1, 2))
	}
	accum.Close() // release the right-side running product before reusing the name

	// 5) accumulate to the left of refIdx
	accum = id.Clone()
	for i := refIdx - 1; i >= 0; i-- {
		H, _ := AlignImages(frames[i+1], frames[i], false)
		if H == nil || H.Empty() {
			log.Error("Failed to align frames for left side", "i", i)
			if H != nil {
				H.Close()
			}
			continue
		}
		// accum = H @ accum
		tmp := gocv.NewMat()
		emptyC := gocv.NewMat()
		gocv.Gemm(*H, accum, 1.0, emptyC, 0.0, &tmp, 0)
		emptyC.Close()
		accum.Close()
		H.Close()
		accum = tmp

		// insert at transforms[i]
		cl := accum.Clone()
		transforms[i] = &cl
		yTranslations = append([]float64{accum.GetDoubleAt(1, 2)}, yTranslations...)
	}
	accum.Close()

	// 6) compute median of the vertical translations
	median := Median(yTranslations)

	// 7) subtract median from each transform’s ty (element [1,2])
	for _, Tptr := range transforms {
		if Tptr == nil {
			continue
		}
		ty := Tptr.GetDoubleAt(1, 2) - median
		Tptr.SetDoubleAt(1, 2, ty)
	}

	log.Info("Finished calculating transformations", "ref_index", refIdx)
	return transforms, refIdx
}

func CalculateCanvasSize(frames []gocv.Mat, transforms []*gocv.Mat, refIndex int) (int, int, int, int) {
	log := logger.WithOperation("calculate_canvas_size")
	log.Info("Calculating canvas dimensions", "reference_frame", refIndex)

	// Get dimensions of the first frame
	height := frames[0].Rows()
	width := frames[0].Cols()
	log.Info("Base frame dimensions", "width", width, "height", height)

	// Calculate the maximum translation in each direction
	var minX, maxX, minY, maxY float64
	for i, transform := range transforms {
		if i == refIndex || transform == nil {
			continue
		}

		// Check if matrix is valid
		if (*transform).Empty() {
			log.Warn("Empty transformation matrix", "frame", i)
			continue
		}

		// Get translation components with error checking
		if (*transform).Rows() < 2 || (*transform).Cols() < 3 {
			log.Error("Invalid transformation matrix dimensions",
				"frame", i,
				"rows", (*transform).Rows(),
				"cols", (*transform).Cols())
			continue
		}

		// Get translation components
		tx := (*transform).GetDoubleAt(0, 2)
		ty := (*transform).GetDoubleAt(1, 2)

		log.Debug("Frame translation",
			"frame", i,
			"tx", tx,
			"ty", ty)

		// Update bounds
		minX = math.Min(minX, tx)
		maxX = math.Max(maxX, tx)
		minY = math.Min(minY, ty)
		maxY = math.Max(maxY, ty)
	}

	// Canvas dimensions = the span of original-transform translations
	// plus one frame width/height.
	canvasWidth := int(math.Ceil(maxX - minX + float64(width)))
	canvasHeight := int(math.Ceil(maxY - minY + float64(height)))
	// Offsets must shift INVERTED transforms (frame i → ref) so the
	// leftmost/topmost frame lands at canvas origin (0, 0). Since
	// inv(T_i).tx = -T_i.tx, the smallest inv.tx is -max(T_i.tx) =
	// -maxX. To make that frame land at canvas col 0, we add +maxX
	// as the X offset. (The earlier code used -minX, which placed
	// the chronologically-last frame past the right edge of the
	// canvas — empirically reproducible by inspecting warped frame
	// dumps where the final frame came out entirely black.) Same
	// logic applies to Y.
	frameXOffset := int(math.Ceil(maxX))
	frameYOffset := int(math.Ceil(maxY))
	if frameXOffset < 0 {
		frameXOffset = 0
	}
	if frameYOffset < 0 {
		frameYOffset = 0
	}

	log.Info("Calculated canvas dimensions",
		"width", canvasWidth,
		"height", canvasHeight,
		"x_offset", frameXOffset,
		"y_offset", frameYOffset,
		"min_x", minX,
		"max_x", maxX,
		"min_y", minY,
		"max_y", maxY)

	return canvasWidth, canvasHeight, frameXOffset, frameYOffset
}

// TrimBlackBorders crops nearly black borders from an image and saves a debug crop.
func TrimBlackBorders(img gocv.Mat, threshold uint8) gocv.Mat {
	// convert to grayscale if needed
	var gray gocv.Mat
	if img.Channels() == 3 {
		gray = gocv.NewMat()
		gocv.CvtColor(img, &gray, gocv.ColorBGRToGray)
		defer gray.Close()
	} else {
		gray = img
	}

	// threshold to binary mask of non-black
	binary := gocv.NewMat()
	defer binary.Close()
	gocv.Threshold(gray, &binary, float32(threshold), 255, gocv.ThresholdBinary)

	// find bounding box of white pixels
	rows, cols := binary.Rows(), binary.Cols()
	minX, minY := cols, rows
	maxX, maxY := 0, 0
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if binary.GetUCharAt(y, x) > 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	// no non-black found
	if maxX < minX || maxY < minY {
		return img
	}
	// crop and clone
	r := img.Region(image.Rect(minX, minY, maxX+1, maxY+1))
	cropped := r.Clone()
	r.Close()

	return cropped
}

// leftmostNonBlackColumn returns the x-coordinate of the leftmost
// column in m that contains any non-black pixel, or -1 if the image
// is entirely black. Implemented by collapsing rows with cv::reduce
// (max) into a 1×W mask and scanning at most W bytes Go-side — much
// faster than per-pixel BGR iteration over the whole frame.
func leftmostNonBlackColumn(m gocv.Mat) int {
	if m.Empty() {
		return -1
	}
	gray := gocv.NewMat()
	defer gray.Close()
	if m.Channels() > 1 {
		gocv.CvtColor(m, &gray, gocv.ColorBGRToGray)
	} else {
		m.CopyTo(&gray)
	}

	binary := gocv.NewMat()
	defer binary.Close()
	// Any non-zero gray pixel is treated as content. Matches the
	// Python `sum(axis=2) > 0` check on warped frames produced by
	// warpPerspective with BORDER_CONSTANT=0.
	gocv.Threshold(gray, &binary, 0, 255, gocv.ThresholdBinary)

	colMax := gocv.NewMat()
	defer colMax.Close()
	// dim=0 collapses rows → 1×W, taking max per column.
	if err := gocv.Reduce(binary, &colMax, 0, gocv.ReduceMax, gocv.MatTypeCV8U); err != nil {
		return -1
	}

	for x := 0; x < colMax.Cols(); x++ {
		if colMax.GetUCharAt(0, x) != 0 {
			return x
		}
	}
	return -1
}

// paintStrip copies the vertical column-range [x1, x2) from src onto
// dst at the same column range, masked by src's non-black pixels.
// Used by StitchPanorama to splice one frame's contribution into the
// panorama at its already-aligned position. Returns the number of
// columns actually painted (after clamping); useful for tests.
func paintStrip(dst, src gocv.Mat, x1, x2 int) int {
	if x1 < 0 {
		x1 = 0
	}
	if x2 > dst.Cols() {
		x2 = dst.Cols()
	}
	if x2 > src.Cols() {
		x2 = src.Cols()
	}
	if x2 <= x1 {
		return 0
	}
	h := dst.Rows()
	if src.Rows() < h {
		h = src.Rows()
	}

	stripRect := image.Rect(x1, 0, x2, h)
	srcRoi := src.Region(stripRect)
	defer srcRoi.Close()
	dstRoi := dst.Region(stripRect)
	defer dstRoi.Close()

	gray := gocv.NewMat()
	defer gray.Close()
	gocv.CvtColor(srcRoi, &gray, gocv.ColorBGRToGray)

	mask := gocv.NewMat()
	defer mask.Close()
	gocv.Threshold(gray, &mask, 0, 255, gocv.ThresholdBinary)

	srcRoi.CopyToWithMask(&dstRoi, mask)
	return x2 - x1
}

// clampInt ensures v is between min and max (inclusive).
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// StitchPanorama composes a single panorama image from a sequence of
// canvas-sized warped frames using the column-strip algorithm from
// the Python reference implementation in ref/ex4.py.
//
// For each consecutive pair (prev, curr) we find the leftmost
// non-black column of each (L_prev, L_curr) and paint the column
// strip [L_prev+frameXOffset, L_curr+frameXOffset) of prev_warped
// onto the canvas at the same column range. Because each warped
// frame is already aligned to its target position on the canvas, the
// strip lands exactly where it needs to be — no horizontal
// accumulator, no overlay of full frames.
//
// frameXOffset shifts which column slice of each frame contributes
// to the panorama. For dynamic mosaics, varying frameXOffset across
// the output sequence produces a time-evolving panorama. For static
// mosaics it is typically a small constant (e.g.
// config.MinimalPixelColumnIndex).
//
// Caveat (matches the Python reference): the trailing frame's strip
// is intentionally not painted because there is no next frame to
// bound it. We also paint a final tail strip from the last frame
// using its full content width — this is a small, deliberate
// extension over the Python so the rightmost ~frame_width of the
// canvas isn't always black.
func StitchPanorama(
	videoName string,
	warpedFrames []gocv.Mat,
	canvasWidth,
	canvasHeight,
	frameXOffset int,
) gocv.Mat {
	log := logger.WithVideo(videoName)
	log.Info("Starting panorama stitching",
		"frame_count", len(warpedFrames),
		"canvas_width", canvasWidth,
		"canvas_height", canvasHeight,
		"frame_x_offset", frameXOffset,
	)

	canvas := gocv.NewMatWithSize(canvasHeight, canvasWidth, gocv.MatTypeCV8UC3)
	canvas.SetTo(gocv.NewScalar(0, 0, 0, 0))

	if len(warpedFrames) == 0 {
		return canvas
	}

	var firstWarped, prevWarped *gocv.Mat
	firstLeftmostX, prevLeftmostX := 0, 0

	for i := range warpedFrames {
		warped := &warpedFrames[i]
		if warped.Empty() {
			continue
		}
		currLeftmostX := leftmostNonBlackColumn(*warped)
		if currLeftmostX < 0 {
			// frame is entirely black — skip without disturbing
			// prev/cur tracking, so the next non-black frame still
			// pairs with the right predecessor.
			continue
		}

		if prevWarped != nil {
			paintStrip(canvas, *prevWarped, prevLeftmostX+frameXOffset, currLeftmostX+frameXOffset)
		} else {
			// First non-empty frame seen — remember it for the
			// leading strip painted after the loop.
			firstWarped = warped
			firstLeftmostX = currLeftmostX
		}
		prevWarped = warped
		prevLeftmostX = currLeftmostX
	}

	// Leading strip: paint canvas cols [0, firstLeftmostX+offset)
	// from the first non-empty frame so the panorama's left edge
	// isn't black even when frameXOffset shifts the regular strips
	// far to the right (the offset can be > the panorama height for
	// long videos because the Python reference linspaces it from
	// MinimalPixelColumnIndex up to len(warpedFrames)).
	if firstWarped != nil {
		paintStrip(canvas, *firstWarped, 0, firstLeftmostX+frameXOffset)
	}
	// Tail strip: paint cols [prevLeftmostX+offset, canvas.Cols())
	// from the last non-empty frame so the right edge is filled in
	// the same way.
	if prevWarped != nil {
		paintStrip(canvas, *prevWarped, prevLeftmostX+frameXOffset, canvas.Cols())
	}

	log.Info("Completed panorama stitching")
	return canvas
}

// GenerateVideoFromFrames converts a slice of Mats into an MP4 video file.
func GenerateVideoFromFrames(
	images []resJob,
	height int,
	width int,
	outputPath string,
	fps int) error {

	log := logger.WithOperation("create_video")
	log.Info("Creating video", "output", outputPath, "fps", fps, "frame_count", len(images))

	// Get dimensions from first image
	log.Info("Video dimensions", "width", width, "height", height)

	// Create video writer
	writer, err := gocv.VideoWriterFile(outputPath, "mp4v", float64(fps), width, height, true)
	if err != nil {
		return fmt.Errorf("failed to create video writer: %w", err)
	}
	defer writer.Close()

	// Write frames
	for _, job := range images {
		if err := writer.Write(job.mat); err != nil {
			return fmt.Errorf("failed to write frame %d: %w", job.idx, err)
		}
		log.Info("Wrote frame", "frame", job.idx)
	}

	log.Info("Video creation completed")
	return nil
}

// // RotateFrame rotates a frame to align motion to the right.
func RotateFrame(frame gocv.Mat, direction string) gocv.Mat {
	var rotated gocv.Mat
	switch direction {
	case config.Right:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate180Clockwise)
	case config.Left:
		// no rotation
		rotated = frame.Clone()
	case config.Up:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate90Clockwise)
	case config.Down:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate90CounterClockwise)
	default:
		rotated = frame.Clone()
	}
	return rotated
}

// RotateFrameBack reverts rotation applied for alignment.
func RotateFrameBack(frame gocv.Mat, direction string) gocv.Mat {
	var original gocv.Mat
	switch direction {
	case config.Right:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate180Clockwise)
	case config.Left:
		original = frame.Clone()
	case config.Up:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate90CounterClockwise)
	case config.Down:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate90Clockwise)
	default:
		original = frame.Clone()
	}
	return original
}

// DetectMotionDirection detects the dominant motion direction in a video.
func DetectMotionDirection(frames []gocv.Mat) string {
	// vote with motion of first 5 frames relative to the first frame
	votes := map[string]int{
		config.Left:  0,
		config.Right: 0,
		config.Up:    0,
		config.Down:  0,
	}
	// limited to available frames
	limit := 6
	if len(frames) < limit {
		limit = len(frames)
	}
	for i := 1; i < limit; i++ {
		_, dir := AlignImages(frames[0], frames[i], true)
		votes[dir]++
	}
	// find direction with highest votes
	bestDir := config.Left
	maxVotes := -1
	for dir, count := range votes {
		if count > maxVotes {
			maxVotes = count
			bestDir = dir
		}
	}
	return bestDir
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

func LinspaceChan(start, stop, count int) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for _, v := range linspace(start, stop, count) {
			ch <- v
		}
	}()
	return ch
}

// linspace returns `count` evenly-spaced integer values from start to
// stop (inclusive of both endpoints when count >= 2). Returns an empty
// slice for count <= 0.
func linspace(start, stop, count int) []int {
	if count <= 0 {
		return []int{}
	}
	out := make([]int, 0, count)
	if count == 1 {
		out = append(out, start)
		return out
	}
	step := float64(stop-start) / float64(count-1)
	for i := 0; i < count; i++ {
		out = append(out, int(float64(start)+step*float64(i)))
	}
	return out
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
func GenerateMosaicVideo(videoPath, outputDir string, dynamic bool) error {
	videoName := filepath.Base(videoPath)
	log := logger.WithVideo(videoName)
	log.Info("Starting video processing", "dynamic", dynamic)
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

	// Now warp all frames using the inverted, offset transforms
	warpedFrames := make([]gocv.Mat, len(frames))
	defer func() {
		for _, f := range warpedFrames {
			if !f.Empty() {
				f.Close()
			}
		}
	}()

	jobs := make(chan int, len(frames))
	results := make(chan resJob, len(frames))
	var wg sync.WaitGroup

	for w := 0; w < config.ProcessPoolWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if transforms[i] == nil {
					// Skip frames whose transform couldn't be computed
					// — send a placeholder NewMat so the index slot
					// stays aligned with `frames`.
					results <- resJob{idx: i, mat: gocv.NewMat()}
					continue
				}
				warped := gocv.NewMat()
				gocv.WarpPerspectiveWithParams(
					frames[i],
					&warped,
					*transforms[i],
					image.Pt(canvasWidth, canvasHeight),
					gocv.InterpolationLinear,
					gocv.BorderConstant,
					color.RGBA{0, 0, 0, 0},
				)
				results <- resJob{idx: i, mat: warped}
			}
		}()
	}

	for i := range frames {
		jobs <- i
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	debugFrames := debugWritesEnabled()
	for job := range results {
		warpedFrames[job.idx] = job.mat
		if debugFrames {
			debugPath := filepath.Join(outputDir, fmt.Sprintf("warped_frame_%d.jpg", job.idx))
			if ok := gocv.IMWrite(debugPath, job.mat); !ok {
				log.Error("Failed to save warped frame", "index", job.idx)
			}
		}
	}

	// Generate output filename
	outputName := "static_mosaic"
	if dynamic {
		outputName = "dynamic_mosaic"
	}
	outputPath := filepath.Join(outputDir, outputName+".mp4")

	selectedIndices := linspace(config.MinimalPixelColumnIndex, len(warpedFrames), config.OutputFPS*config.OutputLengthInSeconds)
	log.Info("Selected indices for mosaic", "indices", selectedIndices)

	mosaics := make(chan resJob, len(selectedIndices))
	jobsCh := make(chan int, len(selectedIndices))
	for _, idx := range selectedIndices {
		jobsCh <- idx
	}
	close(jobsCh)

	stitchWg := sync.WaitGroup{}
	for sticher := 0; sticher < config.StitcherWorkers; sticher++ {
		stitchWg.Add(1)
		go func() {
			defer stitchWg.Done()
			for offset := range jobsCh {
				mosaics <- resJob{
					idx: offset,
					mat: StitchPanorama(videoName, warpedFrames, canvasWidth, canvasHeight, offset),
				}
			}
		}()
	}

	go func() {
		stitchWg.Wait()
		close(mosaics)
	}()

	outputFrames := make([]resJob, 0, len(selectedIndices))
	for job := range mosaics {
		outputFrames = append(outputFrames, job)
	}
	defer func() {
		for _, f := range outputFrames {
			f.mat.Close()
		}
	}()

	sort.Slice(outputFrames, func(i, j int) bool {
		return outputFrames[i].idx < outputFrames[j].idx
	})

	if err := GenerateVideoFromFrames(outputFrames, canvasHeight, canvasWidth, outputPath, config.OutputFPS); err != nil {
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
	return GenerateVideosFromDir(config.InputDir, "output")
}

// GenerateVideosFromDir is the testable form of GenerateVideos.
func GenerateVideosFromDir(inputDir, outputDir string) error {
	videoFiles, err := listVideoFiles(inputDir)
	if err != nil {
		return err
	}
	if len(videoFiles) == 0 {
		return fmt.Errorf("no video files found in input directory %q", inputDir)
	}

	logger.Log.Info("Found video files to process", "count", len(videoFiles), "input_dir", inputDir)

	var firstErr error
	for _, videoPath := range videoFiles {
		videoName := filepath.Base(videoPath)
		log := logger.WithVideo(videoName)

		log.Info("Starting video processing")

		videoOutputDir := filepath.Join(outputDir, videoName)
		if err := os.MkdirAll(videoOutputDir, 0o755); err != nil {
			log.Error("Failed to create output directory", "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		if err := GenerateMosaicVideo(videoPath, videoOutputDir, false); err != nil {
			log.Error("Failed to generate static mosaic", "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		log.Info("Generated static mosaic")
	}

	return firstErr
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
