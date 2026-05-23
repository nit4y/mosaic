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

func TestStitchPanorama_PaintsExpectedColumnStrips(t *testing.T) {
	// Simulate a horizontal pan with five frames, each 40px wide
	// content sliding right by 10px. Leftmost columns:
	//   frame 0 → 0,   frame 1 → 10, frame 2 → 20,
	//   frame 3 → 30,  frame 4 → 40
	// With frameXOffset=0 the column-strip algorithm should paint:
	//   cols [0, 10)  from frame 0 (color A)
	//   cols [10, 20) from frame 1 (color B)
	//   cols [20, 30) from frame 2 (color C)
	//   cols [30, 40) from frame 3 (color D)
	//   cols [40, 80) from frame 4 (tail strip — frame 4 content
	//                   extends to col 80, painted up to canvas edge)
	//   cols [80, 100) black (outside any frame's content)
	canvasW, canvasH := 100, 20
	a := []uint8{100, 0, 0}
	b := []uint8{0, 100, 0}
	c := []uint8{0, 0, 100}
	d := []uint8{200, 200, 0}
	e := []uint8{0, 200, 200}
	frames := []gocv.Mat{
		makeWarped(t, canvasW, canvasH, 0, 40, a[0], a[1], a[2]),
		makeWarped(t, canvasW, canvasH, 10, 40, b[0], b[1], b[2]),
		makeWarped(t, canvasW, canvasH, 20, 40, c[0], c[1], c[2]),
		makeWarped(t, canvasW, canvasH, 30, 40, d[0], d[1], d[2]),
		makeWarped(t, canvasW, canvasH, 40, 40, e[0], e[1], e[2]),
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	out := StitchPanorama("strip", frames, canvasW, canvasH, 0)
	defer out.Close()

	check := func(name string, x0, x1 int, want []uint8) {
		for x := x0; x < x1; x++ {
			v := out.GetVecbAt(0, x)
			if v[0] != want[0] || v[1] != want[1] || v[2] != want[2] {
				t.Errorf("%s col %d: got %v, want %v", name, x, v, want)
				return
			}
		}
	}
	check("frame0 strip", 0, 10, a)
	check("frame1 strip", 10, 20, b)
	check("frame2 strip", 20, 30, c)
	check("frame3 strip", 30, 40, d)
	check("frame4 tail strip", 40, 80, e)
	// Cols 80..99 are outside any frame's content → still black.
	for x := 80; x < canvasW; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("col %d should remain black, got non-zero sum", x)
		}
	}
}

func TestStitchPanorama_RespectsFrameXOffset(t *testing.T) {
	// With frameXOffset=5, every strip is shifted right by 5
	// columns. Frame 0's strip moves from [0,10) → [5,15), etc. The
	// leftmost 5 columns are unpainted (matching the Python
	// reference's behavior).
	canvasW, canvasH := 60, 10
	frames := []gocv.Mat{
		makeWarped(t, canvasW, canvasH, 0, 20, 80, 0, 0),
		makeWarped(t, canvasW, canvasH, 10, 20, 0, 80, 0),
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	out := StitchPanorama("offset", frames, canvasW, canvasH, 5)
	defer out.Close()

	// Cols [0, 5) unpainted, [5, 15) frame 0 (red-ish),
	// [15, 25) frame 1 tail (green-ish).
	for x := 0; x < 5; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("col %d should be black under offset shift, got non-zero", x)
		}
	}
	for x := 5; x < 15; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 80 || v[1] != 0 {
			t.Errorf("col %d frame0 strip shifted: got %v", x, v)
			break
		}
	}
	// frame 1's tail strip extends from leftmost(10)+5=15 to canvas
	// edge or frame 1 content edge (10+20=30). Both bound by 30.
	for x := 15; x < 30; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 0 || v[1] != 80 {
			t.Errorf("col %d frame1 tail shifted: got %v", x, v)
			break
		}
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
