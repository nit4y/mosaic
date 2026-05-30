package mosaic

import (
	"math"

	"github.com/nit4y/mosaic/internal/config"
	"github.com/nit4y/mosaic/internal/logger"
	"gocv.io/x/gocv"
)

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

	// 6) Vertical layout. By default we flatten the panorama: a
	// horizontal-pan mosaic should sit in one band, so the accumulated
	// vertical translation is zeroed. That keeps the canvas ~one frame tall
	// instead of staircasing into diagonal black wedges. With
	// FlattenVertical=false we instead re-center on the median vertical
	// drift, preserving genuine vertical motion.
	median := Median(yTranslations)
	for _, Tptr := range transforms {
		if Tptr == nil {
			continue
		}
		if config.FlattenVertical {
			Tptr.SetDoubleAt(1, 2, 0)
		} else {
			Tptr.SetDoubleAt(1, 2, Tptr.GetDoubleAt(1, 2)-median)
		}
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
