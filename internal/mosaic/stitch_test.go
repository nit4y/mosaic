package mosaic

import (
	"image"
	"testing"

	"gocv.io/x/gocv"
)

// makeWarped creates a canvas-sized Mat with a solid-color rectangle of
// width `contentW` placed starting at column `leftEdge`. The rest is
// black. This mimics what WarpPerspective produces for a frame whose
// content lands at a particular x position on the canvas.
func makeWarped(t *testing.T, canvasW, canvasH, leftEdge, contentW int, b, g, r uint8) gocv.Mat {
	t.Helper()
	m := gocv.NewMatWithSize(canvasH, canvasW, gocv.MatTypeCV8UC3)
	m.SetTo(gocv.NewScalar(0, 0, 0, 0))
	rect := image.Rect(leftEdge, 0, leftEdge+contentW, canvasH)
	roi := m.Region(rect)
	defer roi.Close()
	roi.SetTo(gocv.NewScalar(float64(b), float64(g), float64(r), 0))
	return m
}

// columnSum returns the total intensity (summed B+G+R) of a single
// column of a BGR Mat.
func columnSum(m gocv.Mat, x int) int {
	if x < 0 || x >= m.Cols() {
		return 0
	}
	sum := 0
	for y := 0; y < m.Rows(); y++ {
		v := m.GetVecbAt(y, x)
		sum += int(v[0]) + int(v[1]) + int(v[2])
	}
	return sum
}

func TestStitchPanorama_EmptyInput(t *testing.T) {
	out := StitchPanorama("t.mp4", nil, 100, 50, 0)
	defer out.Close()
	if out.Cols() != 100 || out.Rows() != 50 {
		t.Fatalf("empty input: got %dx%d, want 100x50", out.Cols(), out.Rows())
	}
}

func TestStitchPanorama_FillsCanvasFromOverlappingFrames(t *testing.T) {
	// Simulate a horizontal pan: each warped frame is 40 wide at the
	// canvas, sliding right by 10 pixels. Canvas should be fully
	// covered from col 0 to col 80 (frame 4's right edge).
	canvasW, canvasH := 100, 20
	frames := []gocv.Mat{
		makeWarped(t, canvasW, canvasH, 0, 40, 100, 0, 0),
		makeWarped(t, canvasW, canvasH, 10, 40, 0, 100, 0),
		makeWarped(t, canvasW, canvasH, 20, 40, 0, 0, 100),
		makeWarped(t, canvasW, canvasH, 30, 40, 200, 200, 0),
		makeWarped(t, canvasW, canvasH, 40, 40, 0, 200, 200),
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	out := StitchPanorama("test", frames, canvasW, canvasH, 0)
	defer out.Close()

	// Every column in [0, 80) should have non-zero content because
	// some frame covers it. The first frame (color (100,0,0)) wins at
	// its overlap regions due to first-write-wins.
	for x := 0; x < 80; x++ {
		if columnSum(out, x) == 0 {
			t.Errorf("column %d is fully black after stitching", x)
		}
	}
	// First frame is the anchor in this call (frameOffset=0). It
	// covers cols 0..40 fully. Within that range, every pixel should
	// be the first frame's colour (100,0,0).
	for x := 0; x < 40; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 100 || v[1] != 0 || v[2] != 0 {
			t.Errorf("col %d anchor region: got %v, want [100 0 0]", x, v)
			break
		}
	}
	// Columns 80..99 should still be black (no frame covered them).
	for x := 80; x < canvasW; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("column %d should be black but has content", x)
		}
	}
}

func TestStitchPanorama_AnchorPriority(t *testing.T) {
	// Two frames fully overlap at cols 20..60. With anchor=1 (second
	// frame), its content should appear in the overlap.
	canvasW, canvasH := 80, 10
	frames := []gocv.Mat{
		makeWarped(t, canvasW, canvasH, 20, 40, 50, 50, 50),
		makeWarped(t, canvasW, canvasH, 20, 40, 200, 200, 200),
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	out := StitchPanorama("anchor", frames, canvasW, canvasH, 1)
	defer out.Close()

	v := out.GetVecbAt(0, 30)
	if v[0] != 200 || v[1] != 200 || v[2] != 200 {
		t.Errorf("anchor frame should win overlap: got %v, want [200 200 200]", v)
	}
}

func TestStitchPanorama_HandlesEmptyMats(t *testing.T) {
	canvasW, canvasH := 50, 10
	frames := []gocv.Mat{
		makeWarped(t, canvasW, canvasH, 0, 30, 80, 0, 0),
		gocv.NewMat(), // content-empty mat — should be skipped without panic
		makeWarped(t, canvasW, canvasH, 20, 30, 0, 80, 0),
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	out := StitchPanorama("empty", frames, canvasW, canvasH, 0)
	defer out.Close()
	// Cols 0..49 should be covered (frame 0: 0..30, frame 2: 20..50).
	for x := 0; x < canvasW; x++ {
		if columnSum(out, x) == 0 {
			t.Errorf("column %d unexpectedly black", x)
		}
	}
}
