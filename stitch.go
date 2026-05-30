package mosaic

import (
	"image"

	"github.com/nit4y/mosaic/internal/config"
	"github.com/nit4y/mosaic/internal/logger"
	"gocv.io/x/gocv"
)

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

// blendSeam linearly cross-fades the two warped frames `left` and `right`
// across canvas columns [x0, x1): at the left edge the result is all
// `left`, at the right edge all `right`. This softens the seam between two
// neighbouring strips so small per-frame misalignments don't read as
// tearing. Because the frames are Y-aligned, their content occupies the
// same rows in the overlap, so a straight per-column weighted average is
// correct (rows that are black in both stay black).
func blendSeam(dst, left, right gocv.Mat, x0, x1 int) {
	if x0 < 0 {
		x0 = 0
	}
	x1 = clampInt(x1, x0, dst.Cols())
	x1 = clampInt(x1, x0, left.Cols())
	x1 = clampInt(x1, x0, right.Cols())
	if x1 <= x0 {
		return
	}
	h := dst.Rows()
	if left.Rows() < h {
		h = left.Rows()
	}
	if right.Rows() < h {
		h = right.Rows()
	}
	n := x1 - x0
	for x := x0; x < x1; x++ {
		// t goes (0,1) across the band so the endpoints still lean fully
		// toward their owning strip.
		t := float64(x-x0+1) / float64(n+1)
		rect := image.Rect(x, 0, x+1, h)
		lc := left.Region(rect)
		rc := right.Region(rect)
		dc := dst.Region(rect)
		gocv.AddWeighted(lc, 1-t, rc, t, 0, &dc)
		lc.Close()
		rc.Close()
		dc.Close()
	}
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
// We paint NO synthetic leading/trailing strips. The old code stretched
// the first/last frame across empty canvas to avoid black edges, which is
// exactly what produced the visible edge smear. The black margins left
// here (and the last frame's unpainted body) are removed downstream by
// cropping every panorama to the common content box (see buildSequence),
// giving clean rectangular edges instead of a smear.
func StitchPanorama(
	videoName string,
	warpedFrames []gocv.Mat,
	canvasWidth,
	canvasHeight,
	frameXOffset int,
) gocv.Mat {
	return stitchPanorama(videoName, warpedFrames, canvasWidth, canvasHeight, frameXOffset, config.FeatherWidth)
}

// stitchPanorama is the core stitcher. `feather` is the seam cross-fade
// width in pixels (0 = hard seams). It is exposed separately from the
// public wrapper so tests can pin the feather rather than depend on the
// configured default.
func stitchPanorama(
	videoName string,
	warpedFrames []gocv.Mat,
	canvasWidth,
	canvasHeight,
	frameXOffset int,
	feather int,
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

	var prevWarped *gocv.Mat
	// prevSeam is the canvas column where the current "prev" frame's
	// opaque strip begins; it starts after the previous seam's feather
	// band so the cross-fade isn't overwritten.
	prevSeam := 0

	for i := range warpedFrames {
		warped := &warpedFrames[i]
		if warped.Empty() {
			continue
		}
		currLeftmostX := leftmostNonBlackColumn(*warped)
		if currLeftmostX < 0 {
			// Frame is entirely black — skip without disturbing
			// prev/cur tracking, so the next non-black frame still
			// pairs with the right predecessor.
			continue
		}
		seam := currLeftmostX + frameXOffset

		if prevWarped != nil {
			// Paint prev opaquely up to the seam, then cross-fade prev→curr
			// over [seam, seam+feather). curr's own opaque strip begins
			// after that band (next iteration), so the blend survives.
			paintStrip(canvas, *prevWarped, prevSeam, seam)
			if feather > 0 {
				blendSeam(canvas, *prevWarped, *warped, seam, seam+feather)
			}
			prevSeam = seam + feather
		} else {
			prevSeam = seam
		}
		prevWarped = warped
	}

	log.Info("Completed panorama stitching")
	return canvas
}
