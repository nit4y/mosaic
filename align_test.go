package mosaic

import (
	"image"
	"math"
	"testing"

	"gocv.io/x/gocv"
)

// makeTexturedFrame builds a deterministic 200×200 BGR frame with
// enough texture for ORB/LK to track. We layer multiple rectangles
// at known positions to give detectable corners.
func makeTexturedFrame(t *testing.T) gocv.Mat {
	t.Helper()
	m := gocv.NewMatWithSize(200, 200, gocv.MatTypeCV8UC3)
	m.SetTo(gocv.NewScalar(180, 180, 180, 0)) // grey background

	// A few colored blocks at varying positions = corners for tracking.
	blocks := []struct {
		x, y, w, h int
		b, g, r    uint8
	}{
		{20, 30, 40, 30, 20, 200, 20},
		{90, 50, 25, 25, 200, 20, 20},
		{50, 110, 30, 40, 20, 20, 200},
		{130, 90, 35, 35, 200, 200, 20},
		{20, 150, 50, 20, 20, 200, 200},
		{140, 150, 30, 30, 100, 50, 200},
	}
	for _, b := range blocks {
		roi := m.Region(image.Rect(b.x, b.y, b.x+b.w, b.y+b.h))
		roi.SetTo(gocv.NewScalar(float64(b.b), float64(b.g), float64(b.r), 0))
		roi.Close()
	}
	return m
}

// shiftedCopy returns img translated by (dx, dy), padded with black.
func shiftedCopy(img gocv.Mat, dx, dy int) gocv.Mat {
	out := gocv.NewMatWithSize(img.Rows(), img.Cols(), img.Type())
	out.SetTo(gocv.NewScalar(0, 0, 0, 0))

	// Source / destination rectangles after clamping the shift.
	srcX1, dstX1 := 0, dx
	if dx < 0 {
		srcX1, dstX1 = -dx, 0
	}
	srcY1, dstY1 := 0, dy
	if dy < 0 {
		srcY1, dstY1 = -dy, 0
	}
	w := img.Cols() - abs(dx)
	h := img.Rows() - abs(dy)
	if w <= 0 || h <= 0 {
		return out
	}
	srcRoi := img.Region(image.Rect(srcX1, srcY1, srcX1+w, srcY1+h))
	defer srcRoi.Close()
	dstRoi := out.Region(image.Rect(dstX1, dstY1, dstX1+w, dstY1+h))
	defer dstRoi.Close()
	srcRoi.CopyTo(&dstRoi)
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// TestAlignImages_RecoversKnownTranslation moves a textured frame by
// a known (dx, dy) and checks that alignImages recovers a translation
// within a small tolerance. This is the regression guard for the
// RANSAC hyperparameter sweep — a too-loose RANSAC config drops the
// translation estimate off by many pixels.
func TestAlignImages_RecoversKnownTranslation(t *testing.T) {
	img1 := makeTexturedFrame(t)
	defer img1.Close()

	cases := []struct {
		name   string
		dx, dy int
	}{
		{"right 5", 5, 0},
		{"left 7", -7, 0},
		{"down 4", 0, 4},
		{"diagonal 6,3", 6, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img2 := shiftedCopy(img1, tc.dx, tc.dy)
			defer img2.Close()

			H, _ := alignImages(img1, img2, false, DefaultConfig(), nil)
			if H == nil {
				t.Fatal("alignImages returned nil homography")
			}
			defer H.Close()

			// align_images maps img1 → img2; for a translation by
			// (dx, dy) in image coords, the matrix tx/ty mirror that.
			tx := H.GetDoubleAt(0, 2)
			ty := H.GetDoubleAt(1, 2)
			// Tolerance: LK + RANSAC + the blur pre-pass make recovery
			// imperfect; 2 px is comfortable for this synthetic case.
			if math.Abs(tx-float64(tc.dx)) > 2 {
				t.Errorf("tx = %v, want ≈ %v", tx, tc.dx)
			}
			if math.Abs(ty-float64(tc.dy)) > 2 {
				t.Errorf("ty = %v, want ≈ %v", ty, tc.dy)
			}
		})
	}
}

func TestAlignImages_NoCornersReturnsNil(t *testing.T) {
	flat := gocv.NewMatWithSize(100, 100, gocv.MatTypeCV8UC3)
	defer flat.Close()
	flat.SetTo(gocv.NewScalar(128, 128, 128, 0)) // uniform → no trackable corners
	H, dir := alignImages(flat, flat, true, DefaultConfig(), nil)
	if H != nil {
		H.Close()
		t.Error("expected nil homography when no corners are found")
	}
	if dir != Left {
		t.Errorf("default direction = %q, want %q", dir, Left)
	}
}
