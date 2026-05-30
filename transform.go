package mosaic

import (
	"math"

	"gocv.io/x/gocv"
)

// calculateTransformations computes cumulative homographies aligning each frame
// to the middle (reference) frame, then recenters them by the median vertical
// shift. Every returned Mat is owned by the caller and must be closed; a frame
// that could not be aligned is left as an empty Mat (check with Mat.Empty).
func calculateTransformations(frames []gocv.Mat, cfg Config, lg *Logger) ([]gocv.Mat, int) {
	log := lg.With("operation", "calculate_transformations")
	n := len(frames)
	log.Info("Starting transformation calculations", "frame_count", n)

	if n == 0 {
		log.Error("No frames provided for transformation calculation")
		return nil, -1
	}

	// 1) reference index is the middle frame
	refIdx := n / 2

	// 2) allocate output slice
	transforms := make([]gocv.Mat, n)
	yTranslations := make([]float64, 0, n)

	// 3) identity homography for the reference frame
	transforms[refIdx] = gocv.Eye(3, 3, gocv.MatTypeCV64F)
	yTranslations = append(yTranslations, 0.0)

	// 4) accumulate to the right of refIdx
	accum := transforms[refIdx].Clone() // running product
	for i := refIdx + 1; i < n; i++ {
		H, _ := alignImages(frames[i-1], frames[i], true, cfg, lg)
		if H.Empty() {
			log.Error("Failed to align frames for right side", "i", i)
			H.Close()
			transforms[i] = gocv.NewMat() // empty placeholder; downstream skips it
			continue
		}
		next := matMul(accum, H) // accum = accum @ H
		accum.Close()
		H.Close()
		accum = next
		transforms[i] = accum.Clone()
		yTranslations = append(yTranslations, accum.GetDoubleAt(1, 2))
	}
	accum.Close() // release the right-side running product before reusing the name

	// 5) accumulate to the left of refIdx
	accum = transforms[refIdx].Clone()
	for i := refIdx - 1; i >= 0; i-- {
		H, _ := alignImages(frames[i+1], frames[i], false, cfg, lg)
		if H.Empty() {
			log.Error("Failed to align frames for left side", "i", i)
			H.Close()
			transforms[i] = gocv.NewMat()
			continue
		}
		next := matMul(H, accum) // accum = H @ accum
		accum.Close()
		H.Close()
		accum = next
		transforms[i] = accum.Clone()
		yTranslations = append([]float64{accum.GetDoubleAt(1, 2)}, yTranslations...)
	}
	accum.Close()

	// 6) Vertical layout. By default we flatten the panorama: a
	// horizontal-pan mosaic should sit in one band, so the accumulated
	// vertical translation is zeroed. That keeps the canvas ~one frame tall
	// instead of staircasing into diagonal black wedges. With
	// FlattenVertical=false we instead re-center on the median vertical
	// drift, preserving genuine vertical motion.
	medianY := median(yTranslations)
	for i := range transforms {
		if transforms[i].Empty() {
			continue
		}
		if cfg.FlattenVertical {
			transforms[i].SetDoubleAt(1, 2, 0)
		} else {
			transforms[i].SetDoubleAt(1, 2, transforms[i].GetDoubleAt(1, 2)-medianY)
		}
	}

	log.Info("Finished calculating transformations", "ref_index", refIdx)
	return transforms, refIdx
}

// matMul returns the matrix product a @ b as a new Mat (caller owns it). It
// wraps the gocv.Gemm boilerplate so the accumulation loops above read as plain
// matrix algebra.
func matMul(a, b gocv.Mat) gocv.Mat {
	product := gocv.NewMat()
	beta := gocv.NewMat() // unused C term (beta = 0)
	gocv.Gemm(a, b, 1.0, beta, 0.0, &product, 0)
	beta.Close()
	return product
}

// calculateCanvasSize returns the canvas width and height needed to hold every
// transformed frame, plus the x/y offsets that shift the inverted transforms so
// the leftmost/topmost frame lands at the canvas origin.
func calculateCanvasSize(frames []gocv.Mat, transforms []gocv.Mat, refIndex int, lg *Logger) (int, int, int, int) {
	log := lg.With("operation", "calculate_canvas_size")
	log.Info("Calculating canvas dimensions", "reference_frame", refIndex)

	// Get dimensions of the first frame
	height := frames[0].Rows()
	width := frames[0].Cols()
	log.Info("Base frame dimensions", "width", width, "height", height)

	// Calculate the maximum translation in each direction
	var minX, maxX, minY, maxY float64
	for i, transform := range transforms {
		if i == refIndex || transform.Empty() {
			continue
		}

		// Skip malformed transforms rather than index out of range below.
		if transform.Rows() < 2 || transform.Cols() < 3 {
			log.Error("Invalid transformation matrix dimensions",
				"frame", i,
				"rows", transform.Rows(),
				"cols", transform.Cols())
			continue
		}

		// Get translation components
		tx := transform.GetDoubleAt(0, 2)
		ty := transform.GetDoubleAt(1, 2)

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
	// Offsets shift the INVERTED transforms (frame i → ref) so the
	// leftmost/topmost frame lands at the canvas origin (0, 0). Since
	// inv(T_i).tx = -T_i.tx, the smallest inv.tx is -maxX; adding +maxX as
	// the X offset moves that frame to column 0. Same logic for Y.
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
