package mosaic

import (
	"image"
	"testing"

	"gocv.io/x/gocv"
)

func TestDetectCorners_FindsBlockCorners(t *testing.T) {
	// Pure gray frame with a white block at (40,40)..(80,80). The
	// block has four 90-degree corners → Shi-Tomasi should find at
	// least 4 corner candidates near them.
	m := gocv.NewMatWithSize(120, 120, gocv.MatTypeCV8U)
	defer m.Close()
	m.SetTo(gocv.NewScalar(50, 50, 50, 0))
	roi := m.Region(image.Rect(40, 40, 80, 80))
	roi.SetTo(gocv.NewScalar(220, 220, 220, 0))
	roi.Close()

	pts, ptsMat := detectCorners(m, 50, 0.01, 5)
	defer ptsMat.Close()
	if len(pts) < 4 {
		t.Fatalf("expected ≥4 corners, got %d", len(pts))
	}
	if ptsMat.Rows() != len(pts) || ptsMat.Cols() != 1 {
		t.Errorf("ptsMat dims = %dx%d, want %dx1", ptsMat.Rows(), ptsMat.Cols(), len(pts))
	}
	// All detected corners must lie within the block's bounding box
	// plus some margin (the corner response leaks slightly outside
	// the block boundary).
	for i, p := range pts {
		if p.X < 35 || p.X > 85 || p.Y < 35 || p.Y > 85 {
			t.Errorf("corner %d at (%v,%v) outside expected region", i, p.X, p.Y)
		}
	}
}

func TestDetectCorners_ReturnsEmptyForFlatImage(t *testing.T) {
	// Featureless gray image — no corners.
	m := gocv.NewMatWithSize(80, 80, gocv.MatTypeCV8U)
	defer m.Close()
	m.SetTo(gocv.NewScalar(128, 128, 128, 0))
	pts, ptsMat := detectCorners(m, 100, 0.01, 5)
	defer ptsMat.Close()
	if len(pts) != 0 {
		t.Errorf("flat image: got %d corners, want 0", len(pts))
	}
}

func TestDetectCorners_RespectsMaxCorners(t *testing.T) {
	// Image with many corners — maxCorners=3 must cap output.
	m := gocv.NewMatWithSize(120, 120, gocv.MatTypeCV8U)
	defer m.Close()
	m.SetTo(gocv.NewScalar(50, 50, 50, 0))
	for _, b := range [][4]int{
		{10, 10, 20, 20},
		{40, 10, 50, 20},
		{70, 10, 80, 20},
		{10, 40, 20, 50},
		{40, 40, 50, 50},
		{70, 40, 80, 50},
	} {
		roi := m.Region(image.Rect(b[0], b[1], b[2], b[3]))
		roi.SetTo(gocv.NewScalar(220, 220, 220, 0))
		roi.Close()
	}
	pts, ptsMat := detectCorners(m, 3, 0.01, 5)
	defer ptsMat.Close()
	if len(pts) > 3 {
		t.Errorf("got %d corners, want ≤3", len(pts))
	}
}
