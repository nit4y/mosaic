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

// toPoint2fVector converts a Go slice of Point2f into a gocv.Point2fVector.
func toPoint2fVector(pts []gocv.Point2f) gocv.Point2fVector {
	return gocv.NewPoint2fVectorFromPoints(pts)
}

func GetCornerPoints(harris gocv.Mat) gocv.Mat {
	// Dilate the Harris response
	kernel := gocv.NewMat()
	defer kernel.Close()
	gocv.Dilate(harris, &harris, kernel)

	// Calculate threshold: 1% of max value
	_, maxVal, _, _ := gocv.MinMaxLoc(harris)
	threshold := 0.01 * maxVal

	// Threshold the image
	mask := gocv.NewMat()
	defer mask.Close()
	gocv.Threshold(harris, &mask, threshold, 255, gocv.ThresholdBinary)

	// Find non-zero coordinates
	coords := gocv.NewMat()
	defer coords.Close()
	gocv.FindNonZero(mask, &coords)

	// Create points Mat (N, 1, 2) of type CV_32FC2
	count := coords.Rows()
	points := gocv.NewMatWithSize(count, 1, gocv.MatTypeCV32FC2)

	for i := 0; i < count; i++ {
		x := coords.GetVeciAt(i, 0)[0]
		y := coords.GetVeciAt(i, 0)[1]
		points.SetFloatAt(i, 0, float32(x))
		points.SetFloatAt(i, 1, float32(y))
	}

	return points
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
	aff := gocv.EstimateAffinePartial2DWithParams(v1, v2, gocv.NewMat(), int(gocv.HomographyMethodRANSAC),
		float64(config.RansacThreshold), 2000, 0.99, 10,
	)

	if aff.Empty() {
		log.Error("Failed to estimate affine transformation - empty matrix")
	}

	// defer aff.Close()

	log.Info("Affine matrix type", "type", aff.Type().String())

	// convert to homogeneous and stabilize
	H := ToHomogeneous(aff)
	if H.Empty() {
		log.Error("Failed to convert affine to homogeneous matrix - empty matrix")
	}
	log.Info("Homogeneous matrix type", "type", H.Type().String())

	// defer aff.Close()
	Hh := StabilizeHorizontalMotion(H)
	if Hh.Empty() {
		log.Error("Failed to stabilize horizontal motion - empty matrix")
	}
	// defer H.Close()

	// compute direction if needed
	dir := config.Left
	if calcDirection {
		dir = CalcMotionDirection(valid1, valid2)
	}

	log.Info("Stabilized matrix type", "type", Hh.Type().String())
	if Hh.Empty() {
		log.Error("Failed to stabilize horizontal motion - empty matrix")
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

	fps := video.Get(gocv.VideoCaptureFPS)
	frameCount := int(video.Get(gocv.VideoCaptureFrameCount))
	log.Info("Video properties", "fps", fps, "total_frames", frameCount)

	// Extract frames
	frames := make([]gocv.Mat, 0, frameCount)
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
		if H.Empty() {
			log.Error("Failed to align frames for right side", "i", i)
			H.Close()
			continue
		}
		// accum = accum @ H
		tmp := gocv.NewMat()
		gocv.Gemm(accum, *H, 1.0, gocv.NewMat(), 0.0, &tmp, 0)
		accum.Close()
		H.Close()
		accum = tmp

		// clone for output
		cl := accum.Clone()
		transforms[i] = &cl
		yTranslations = append(yTranslations, accum.GetDoubleAt(1, 2))
	}

	// 5) accumulate to the left of refIdx
	accum = id.Clone()
	for i := refIdx - 1; i >= 0; i-- {
		H, _ := AlignImages(frames[i+1], frames[i], false)
		if H.Empty() {
			log.Error("Failed to align frames for left side", "i", i)
			H.Close()
			continue
		}
		// accum = H @ accum
		tmp := gocv.NewMat()
		gocv.Gemm(*H, accum, 1.0, gocv.NewMat(), 0.0, &tmp, 0)
		accum.Close()
		H.Close()
		accum = tmp

		// insert at transforms[i]
		cl := accum.Clone()
		transforms[i] = &cl
		yTranslations = append([]float64{accum.GetDoubleAt(1, 2)}, yTranslations...)
	}

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

	// Calculate final canvas dimensions
	canvasWidth := int(math.Ceil(maxX - minX + float64(width)))
	canvasHeight := int(math.Ceil(maxY - minY + float64(height)))
	frameXOffset := int(math.Abs(minX))
	frameYOffset := int(math.Abs(minY))

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
	// save debug image
	gocv.IMWrite("cropped.jpg", cropped)
	return cropped
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

// StitchPanorama builds a panoramic mosaic by copying only the non-black columns
// between consecutive warped frames into a larger canvas.
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
		"x_offset", frameXOffset,
	)

	// 1) Blank canvas
	canvas := gocv.NewMatWithSize(canvasHeight, canvasWidth, gocv.MatTypeCV8UC3)
	canvas.SetTo(gocv.NewScalar(0, 0, 0, 0))

	var prevWarped gocv.Mat
	prevLeft := 0
	hasPrev := false

	for idx, warped := range warpedFrames {
		// 2) find leftmost non-black column
		currLeft := findLeftmostNonBlack(warped)
		if currLeft < 0 {
			continue
		}

		if hasPrev {
			// clamp coords
			srcX1 := clampInt(prevLeft+frameXOffset, 0, prevWarped.Cols())
			srcX2 := clampInt(currLeft+frameXOffset, 0, prevWarped.Cols())
			dstX1 := clampInt(srcX1, 0, canvas.Cols())
			dstX2 := clampInt(srcX2, 0, canvas.Cols())

			if srcX2 > srcX1 && dstX2 > dstX1 {
				// extract ROIs
				srcRect := image.Rect(srcX1, 0, srcX2, prevWarped.Rows())
				dstRect := image.Rect(dstX1, 0, dstX2, canvas.Rows())
				srcRoi := prevWarped.Region(srcRect)
				dstRoi := canvas.Region(dstRect)

				// build mask of non-black pixels
				gray := gocv.NewMat()
				mask := gocv.NewMat()
				gocv.CvtColor(srcRoi, &gray, gocv.ColorBGRToGray)
				gocv.Threshold(gray, &mask, 1, 255, gocv.ThresholdBinary)

				// copy only where mask != 0
				srcRoi.CopyToWithMask(&dstRoi, mask)

				// cleanup
				gray.Close()
				mask.Close()
				srcRoi.Close()
				dstRoi.Close()
			}
		}

		// prepare next iteration
		if hasPrev {
			prevWarped.Close()
		}
		prevWarped = warped.Clone()
		prevLeft = currLeft
		hasPrev = true

		// optional debug dump
		dbg := fmt.Sprintf("output/canvas_after_frame_%d.jpg", idx)
		if ok := gocv.IMWrite(dbg, canvas); !ok {
			log.Error("Failed to save canvas", "path", dbg)
		}
	}

	// 3) final tail slice
	if hasPrev {
		srcX1 := clampInt(prevLeft, 0, prevWarped.Cols())
		srcX2 := clampInt(prevWarped.Cols(), 0, prevWarped.Cols())
		dstX1 := clampInt(srcX1+frameXOffset, 0, canvas.Cols())
		dstX2 := clampInt(srcX2+frameXOffset, 0, canvas.Cols())

		if srcX2 > srcX1 && dstX2 > dstX1 {
			srcRect := image.Rect(srcX1, 0, srcX2, prevWarped.Rows())
			dstRect := image.Rect(dstX1, 0, dstX2, canvas.Rows())
			srcRoi := prevWarped.Region(srcRect)
			dstRoi := canvas.Region(dstRect)

			gray := gocv.NewMat()
			mask := gocv.NewMat()
			gocv.CvtColor(srcRoi, &gray, gocv.ColorBGRToGray)
			gocv.Threshold(gray, &mask, 1, 255, gocv.ThresholdBinary)
			srcRoi.CopyToWithMask(&dstRoi, mask)

			gray.Close()
			mask.Close()
			srcRoi.Close()
			dstRoi.Close()
		}
		prevWarped.Close()
	}

	log.Info("Completed panorama stitching")
	return canvas
}

// findLeftmostNonBlack returns the x-coordinate of the first column
// in m that contains any non-black pixel, or –1 if none.
func findLeftmostNonBlack(m gocv.Mat) int {
	rows, cols := m.Rows(), m.Cols()
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			pix := m.GetVecbAt(y, x)
			if pix[0] != 0 || pix[1] != 0 || pix[2] != 0 {
				return x
			}
		}
	}
	return -1
}

// GenerateVideoFromFrames converts a slice of Mats into an MP4 video file.
func GenerateVideoFromFrames(images []gocv.Mat, outputPath string, fps int) error {
	log := logger.WithOperation("create_video")
	log.Info("Creating video", "output", outputPath, "fps", fps, "frame_count", len(images))

	if len(images) == 0 {
		return fmt.Errorf("no images provided")
	}

	// Get dimensions from first image
	height := images[0].Rows()
	width := images[0].Cols()
	log.Info("Video dimensions", "width", width, "height", height)

	// Create video writer
	writer, err := gocv.VideoWriterFile(outputPath, "mp4v", float64(fps), width, height, true)
	if err != nil {
		return fmt.Errorf("failed to create video writer: %w", err)
	}
	defer writer.Close()

	// Write frames
	for i, img := range images {
		if err := writer.Write(img); err != nil {
			return fmt.Errorf("failed to write frame %d: %w", i, err)
		}
		log.Info("Wrote frame", "frame", i)
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
		gocv.Rotate(frame, &original, gocv.Rotate90CounterClockwise)
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

	// Trim black borders from all frames
	for i := range frames {
		frames[i] = TrimBlackBorders(frames[i], 10)
	}

	// Detect and normalize motion direction
	dir := DetectMotionDirection(frames)
	log.Info("Detected motion direction", "direction", dir)
	for i := range frames {
		frames[i] = RotateFrame(frames[i], dir)
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

	// Invert each transform and apply the canvas offsets, just like your Python version
	invTransforms := make([]*gocv.Mat, len(transforms))
	for i, T := range transforms {
		// invert T
		inv := gocv.NewMat()
		ma := *T

		if ok := gocv.Invert(ma, &inv, gocv.SolveDecompositionLu); ok <= 0 {
			// print the matrix `ma``

			//pretty print `ma`
			log.Error("Failed to invert transform", "index", i, "matrix", prettyPrintMatrix(ma))

			log.Error("Failed to invert transform", "index", i)
			continue
		}
		// translate by the offsets
		tx := inv.GetDoubleAt(0, 2) + float64(frameXOffset)
		ty := inv.GetDoubleAt(1, 2) + float64(frameYOffset)
		inv.SetDoubleAt(0, 2, tx)
		inv.SetDoubleAt(1, 2, ty)

		// store and release the original
		invTransforms[i] = &inv
		T.Close()
	}
	transforms = invTransforms

	// Now warp all frames using the inverted, offset transforms
	warpedFrames := make([]gocv.Mat, len(frames))
	for i := range frames {
		warpedFrames[i] = gocv.NewMat()
	}

	// Create a worker pool for parallel processing
	jobs := make(chan int, len(frames))
	results := make(chan resJob, len(frames))
	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < config.ProcessPoolWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				transform := transforms[i]

				affine := gocv.NewMatWithSize(2, 3, gocv.MatTypeCV64F)
				affine.SetDoubleAt(0, 0, transform.GetDoubleAt(0, 0))
				affine.SetDoubleAt(0, 1, transform.GetDoubleAt(0, 1))
				affine.SetDoubleAt(0, 2, transform.GetDoubleAt(0, 2))
				affine.SetDoubleAt(1, 0, transform.GetDoubleAt(1, 0))
				affine.SetDoubleAt(1, 1, transform.GetDoubleAt(1, 1))
				affine.SetDoubleAt(1, 2, transform.GetDoubleAt(1, 2))

				warped := gocv.NewMat()
				gocv.WarpAffineWithParams(
					frames[i],
					&warped,
					affine,
					image.Pt(canvasWidth, canvasHeight),
					gocv.InterpolationLinear,
					gocv.BorderConstant,
					color.RGBA{0, 0, 0, 0},
				)
				results <- resJob{idx: i, mat: warped}
			}
		}()
	}

	// Send jobs
	for i := range frames {
		jobs <- i
	}
	close(jobs)

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	for job := range results {
		warpedFrames[job.idx] = job.mat
		debugPath := filepath.Join(outputDir, fmt.Sprintf("warped_frame_%d.jpg", job.idx))
		if ok := gocv.IMWrite(debugPath, job.mat); !ok {
			log.Error("Failed to save warped frame", "index", job.idx)
		} else {
			log.Info("Saved warped frame", "index", job.idx, "path", debugPath)
		}
	}

	// Generate output filename
	outputName := "static_mosaic"
	if dynamic {
		outputName = "dynamic_mosaic"
	}
	outputPath := filepath.Join(outputDir, outputName+".mp4")

	// Generate evenly spaced indices between config.config.MinimalPixelColumnIndex and length of frames
	// in python its selected_indices = np.linspace(MINIMAL_PIXEL_COLUMN_INDEX, total_frames, num_frames, dtype=int).tolist()
	// in Go:
	// numFrames := len(frames)
	// selectedIndices := make([]int, 0, numFrames)
	// for i := 0; i < numFrames; i++ {
	// 	if i >= config.MinimalPixelColumnIndex && i < numFrames {
	// 		selectedIndices = append(selectedIndices, i)
	// 	}
	// }

	// log.Info("Selected frames for mosaic", "count", selectedIndices)

	// Stitch panorama
	mosaic := StitchPanorama(videoName, warpedFrames, canvasWidth, canvasHeight, config.MinimalPixelColumnIndex)
	log.Info("Stitched panorama", "output", outputPath)

	// Save as video
	if err := GenerateVideoFromFrames([]gocv.Mat{mosaic}, outputPath, 30); err != nil {
		return fmt.Errorf("failed to save video: %w", err)
	}

	// Clean up
	for _, frame := range frames {
		frame.Close()
	}
	for _, frame := range warpedFrames {
		frame.Close()
	}
	mosaic.Close()

	log.Info("Generated mosaic", "duration", time.Since(start))
	return nil
}

// GenerateVideos processes all .mp4 videos in the input directory.
func GenerateVideos() error {
	// Get all video files from input directory
	files, err := os.ReadDir("input")
	if err != nil {
		return fmt.Errorf("failed to read input directory: %w", err)
	}

	// Filter for video files
	var videoFiles []string
	for _, file := range files {
		if !file.IsDir() {
			ext := filepath.Ext(file.Name())
			if ext == ".mp4" || ext == ".avi" || ext == ".mov" {
				videoFiles = append(videoFiles, filepath.Join("input", file.Name()))
			}
		}
	}

	if len(videoFiles) == 0 {
		return fmt.Errorf("no video files found in input directory")
	}

	logger.Log.Info("Found video files to process", "count", len(videoFiles))

	// Process each video
	for _, videoPath := range videoFiles {
		videoName := filepath.Base(videoPath)
		log := logger.WithVideo(videoName)

		log.Info("Starting video processing")

		// Create output directory if it doesn't exist
		outputDir := filepath.Join("output", filepath.Base(videoPath))
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			log.Error("Failed to create output directory", "error", err)
			continue
		}

		// Generate both static and dynamic mosaics
		if err := GenerateMosaicVideo(videoPath, outputDir, false); err != nil {
			log.Error("Failed to generate static mosaic", "error", err)
			continue
		}
		log.Info("Generated static mosaic")

		// if err := GenerateMosaicVideo(videoPath, outputDir, true); err != nil {
		// 	log.Error("Failed to generate dynamic mosaic", "error", err)
		// 	continue
		// }
		log.Info("Generated dynamic mosaic")
	}

	return nil
}
