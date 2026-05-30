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
	out := stitchPanorama("t.mp4", nil, 100, 50, 0, 0, nil)
	defer out.Close()
	if out.Cols() != 100 || out.Rows() != 50 {
		t.Fatalf("empty input: got %dx%d, want 100x50", out.Cols(), out.Rows())
	}
}

func TestStitchPanorama_PaintsExpectedColumnStrips(t *testing.T) {
	// Simulate a horizontal pan with five frames, each 40px wide content
	// sliding right by 10px. Leftmost columns: 0,10,20,30,40.
	//
	// With frameXOffset=0 the column-strip algorithm paints one strip per
	// consecutive pair:
	//   cols [0, 10)   from frame 0 (color A)
	//   cols [10, 20)  from frame 1 (color B)
	//   cols [20, 30)  from frame 2 (color C)
	//   cols [30, 40)  from frame 3 (color D)
	//   cols [40, 100) black — frame 4 is the last frame and is never a
	//                  "prev", so its strip isn't painted (the black is
	//                  cropped away later by buildSequence). No synthetic
	//                  tail strip — that was the old edge smear.
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

	out := stitchPanorama("strip", frames, canvasW, canvasH, 0, 0, nil)
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
	// Cols 40..99 stay black: the last frame's strip is not painted.
	for x := 40; x < canvasW; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("col %d should remain black (no tail strip), got non-zero", x)
		}
	}
}

func TestStitchPanorama_RespectsFrameXOffset(t *testing.T) {
	// frameXOffset=5 shifts the regular strips right by 5 cols. With two
	// frames (L_0=0, L_1=10) the only pair paints frame 0's strip at
	// [L_0+5, L_1+5) = [5, 15). Nothing is painted left of 5 or right of
	// 15 (no synthetic edge strips, and frame 1 is the last frame).
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

	out := stitchPanorama("offset", frames, canvasW, canvasH, 5, 0, nil)
	defer out.Close()

	// Cols [0, 5) black — no leading strip.
	for x := 0; x < 5; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("col %d should be black (no leading strip), got non-zero", x)
			break
		}
	}
	// Cols [5, 15) painted from frame 0 (its color).
	for x := 5; x < 15; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 80 || v[1] != 0 {
			t.Errorf("col %d (shifted frame0 strip): got %v, want frame 0 colour", x, v)
			break
		}
	}
	// Cols [15, 60) black — frame 1 is last, its strip isn't painted.
	for x := 15; x < canvasW; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("col %d should be black (no tail strip), got non-zero", x)
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

	out := stitchPanorama("empty", frames, canvasW, canvasH, 0, 0, nil)
	defer out.Close()

	// The empty mat is skipped, so the only pair is (frame0, frame2):
	// frame0's strip [L_0, L_2) = [0, 20) is painted; [20, 50) stays black.
	for x := 0; x < 20; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] != 80 || v[1] != 0 {
			t.Errorf("col %d should be frame0 colour, got %v", x, v)
			break
		}
	}
	for x := 20; x < canvasW; x++ {
		if columnSum(out, x) != 0 {
			t.Errorf("col %d should be black (frame2 is last), got non-zero", x)
			break
		}
	}
}

func TestStitchPanorama_FeatherBlendsSeam(t *testing.T) {
	// Two frames: f0 blue content [0,40), f1 red content [20,60). The seam
	// sits at x=20; with feathering the band [20,20+feather) cross-fades
	// blue -> red instead of switching hard.
	canvasW, canvasH := 80, 10
	f0 := makeWarped(t, canvasW, canvasH, 0, 40, 255, 0, 0)  // blue (B,G,R)
	f1 := makeWarped(t, canvasW, canvasH, 20, 40, 0, 0, 255) // red
	frames := []gocv.Mat{f0, f1}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	const feather = 8
	out := stitchPanorama("feather", frames, canvasW, canvasH, 0, feather, nil)
	defer out.Close()

	// f0's opaque core (before the seam) stays pure blue.
	if v := out.GetVecbAt(0, 5); v[0] != 255 || v[2] != 0 {
		t.Errorf("f0 core col 5 = %v, want pure blue", v)
	}
	// Somewhere in the seam band both channels are non-zero — a genuine
	// blend, not a hard edge.
	mixed := false
	for x := 20; x < 20+feather; x++ {
		v := out.GetVecbAt(0, x)
		if v[0] > 0 && v[2] > 0 {
			mixed = true
			break
		}
	}
	if !mixed {
		t.Error("seam band has no blended column; feather not applied")
	}
	// The cross-fade is monotone: blue falls and red rises across the band.
	early := out.GetVecbAt(0, 21)
	late := out.GetVecbAt(0, 26)
	if !(early[0] >= late[0] && early[2] <= late[2]) {
		t.Errorf("seam not monotone blue->red: x21=%v x26=%v", early, late)
	}
}
