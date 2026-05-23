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
	// With frameXOffset=5 the regular strips shift right by 5 cols.
	// The leading strip fills cols [0, L_0+5) from frame 0 so the
	// left edge isn't black; the trailing strip extends frame 1's
	// content to the canvas edge (or its content boundary).
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

	// Cols [0, 5) covered by the leading strip from frame 0.
	for x := 0; x < 5; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 80 || v[1] != 0 {
			t.Errorf("col %d (leading strip): got %v, want frame 0 colour", x, v)
			break
		}
	}
	// Cols [5, 15) regular strip from frame 0 between L_0+5 and L_1+5.
	for x := 5; x < 15; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 80 || v[1] != 0 {
			t.Errorf("col %d frame0 strip shifted: got %v", x, v)
			break
		}
	}
	// Cols [15, 30) tail strip from frame 1 (content extends to 30).
	for x := 15; x < 30; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 0 || v[1] != 80 {
			t.Errorf("col %d frame1 tail shifted: got %v", x, v)
			break
		}
	}
}

func TestStitchPanorama_LeadingStripFillsLeftWhenOffsetLarge(t *testing.T) {
	// When frameXOffset is large (mimicking the Python reference's
	// MINIMAL_PIXEL_COLUMN_INDEX..len(warped_frames) linspace, which
	// for a long video can shift strips by hundreds of cols), the
	// regular strips alone leave a gaping black wedge on the left.
	// The leading strip must paint cols [0, L_0+offset) from the
	// first non-empty frame.
	canvasW, canvasH := 200, 10
	frames := []gocv.Mat{
		// Frame 0 spans cols [0, 60).
		makeWarped(t, canvasW, canvasH, 0, 60, 90, 0, 0),
		// Frame 1 spans cols [20, 80).
		makeWarped(t, canvasW, canvasH, 20, 60, 0, 90, 0),
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	// Pick offset=30 so the first regular strip would start at col 30
	// instead of col 0 → cols [0, 30) need the leading-strip fill.
	out := StitchPanorama("lead", frames, canvasW, canvasH, 30)
	defer out.Close()

	for x := 0; x < 30; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 90 || v[1] != 0 || v[2] != 0 {
			t.Errorf("leading-strip col %d: got %v, want frame 0 colour [90 0 0]", x, v)
			break
		}
	}
	// Sanity: the regular strip (frame 0 between L_0+offset=30 and
	// L_1+offset=50) should also be frame 0's colour.
	for x := 30; x < 50; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 90 || v[1] != 0 {
			t.Errorf("regular strip col %d: got %v, want frame 0 colour", x, v)
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
