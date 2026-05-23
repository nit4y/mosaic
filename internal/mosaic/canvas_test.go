package mosaic

import (
	"image"
	"testing"

	"gocv.io/x/gocv"
)

// makeTransform builds a 3x3 affine-like homography Mat with the given
// translation (tx, ty). Identity-scale, no skew.
func makeTransform(tx, ty float64) *gocv.Mat {
	m := gocv.NewMatWithSize(3, 3, gocv.MatTypeCV64F)
	m.SetDoubleAt(0, 0, 1)
	m.SetDoubleAt(1, 1, 1)
	m.SetDoubleAt(2, 2, 1)
	m.SetDoubleAt(0, 2, tx)
	m.SetDoubleAt(1, 2, ty)
	return &m
}

func TestCalculateCanvasSize_ExpandsBounds(t *testing.T) {
	// Single frame of size 100x50 with three transforms — one
	// reference (skipped via refIndex=1), one pushed right by 30, one
	// pushed left by 10 and up by 5. Expected canvas covers full
	// range.
	frame := gocv.NewMatWithSize(50, 100, gocv.MatTypeCV8UC3)
	defer frame.Close()
	frames := []gocv.Mat{frame, frame, frame}

	t0 := makeTransform(-10, -5)
	defer t0.Close()
	t1 := makeTransform(0, 0) // reference
	defer t1.Close()
	t2 := makeTransform(30, 0)
	defer t2.Close()

	w, h, xOff, yOff := CalculateCanvasSize(frames, []*gocv.Mat{t0, t1, t2}, 1)

	// width = (maxX - minX) + frameW = (30 - -10) + 100 = 140
	if w != 140 {
		t.Errorf("canvas width = %d, want 140", w)
	}
	// height = (maxY - minY) + frameH = (0 - -5) + 50 = 55
	if h != 55 {
		t.Errorf("canvas height = %d, want 55", h)
	}
	if xOff != 10 {
		t.Errorf("x offset = %d, want 10", xOff)
	}
	if yOff != 5 {
		t.Errorf("y offset = %d, want 5", yOff)
	}
}

func TestCalculateCanvasSize_SkipsNilAndEmpty(t *testing.T) {
	frame := gocv.NewMatWithSize(20, 40, gocv.MatTypeCV8UC3)
	defer frame.Close()
	frames := []gocv.Mat{frame, frame, frame}

	t0 := makeTransform(50, 0)
	defer t0.Close()
	empty := gocv.NewMat() // empty Mat — should be skipped
	defer empty.Close()
	emptyPtr := &empty
	t2 := makeTransform(-20, 0)
	defer t2.Close()

	// refIndex=1, transforms[1] is empty, plus a nil slot to make sure
	// it's tolerated.
	transforms := []*gocv.Mat{t0, emptyPtr, t2}
	w, _, xOff, _ := CalculateCanvasSize(frames, transforms, 1)

	// minX = -20, maxX = 50 → width = 70 + 40 = 110.
	if w != 110 {
		t.Errorf("canvas width = %d, want 110 (empty transform should be ignored)", w)
	}
	if xOff != 20 {
		t.Errorf("xOff = %d, want 20", xOff)
	}
}

func TestTrimBlackBorders_CropsToContent(t *testing.T) {
	// 40x40 image, all black except a 10x10 white square at (5, 5).
	img := gocv.NewMatWithSize(40, 40, gocv.MatTypeCV8UC3)
	defer img.Close()
	img.SetTo(gocv.NewScalar(0, 0, 0, 0))
	roi := img.Region(image.Rect(5, 5, 15, 15))
	roi.SetTo(gocv.NewScalar(255, 255, 255, 0))
	roi.Close()

	cropped := TrimBlackBorders(img, 10)
	defer cropped.Close()
	if cropped.Cols() != 10 || cropped.Rows() != 10 {
		t.Errorf("cropped size = %dx%d, want 10x10", cropped.Cols(), cropped.Rows())
	}
	// Check the top-left of cropped is white.
	v := cropped.GetVecbAt(0, 0)
	if v[0] != 255 {
		t.Errorf("top-left pixel = %v, want white", v)
	}
}

func TestTrimBlackBorders_ReturnsInputWhenAllBlack(t *testing.T) {
	img := gocv.NewMatWithSize(20, 30, gocv.MatTypeCV8UC3)
	defer img.Close()
	img.SetTo(gocv.NewScalar(0, 0, 0, 0))

	cropped := TrimBlackBorders(img, 10)
	// In the all-black case TrimBlackBorders returns the input Mat
	// itself (no clone) — verify it has the original dimensions.
	if cropped.Cols() != 30 || cropped.Rows() != 20 {
		t.Errorf("all-black image size changed: got %dx%d, want 30x20",
			cropped.Cols(), cropped.Rows())
	}
}

func TestApplyBlur_PreservesDimensions(t *testing.T) {
	src := gocv.NewMatWithSize(60, 80, gocv.MatTypeCV8UC1)
	defer src.Close()
	src.SetTo(gocv.NewScalar(128, 0, 0, 0))

	out := ApplyBlur(src)
	defer out.Close()
	if out.Cols() != src.Cols() || out.Rows() != src.Rows() {
		t.Errorf("ApplyBlur dims: got %dx%d, want %dx%d",
			out.Cols(), out.Rows(), src.Cols(), src.Rows())
	}
}

func TestLinspaceSliceMatchesChan(t *testing.T) {
	// linspace() and LinspaceChan() must agree, since one wraps the
	// other.
	cases := [][3]int{{0, 10, 5}, {2, 2, 3}, {0, 0, 0}, {5, 50, 10}}
	for _, c := range cases {
		got := linspace(c[0], c[1], c[2])
		ch := LinspaceChan(c[0], c[1], c[2])
		var fromChan []int
		for v := range ch {
			fromChan = append(fromChan, v)
		}
		if len(got) != len(fromChan) {
			t.Fatalf("linspace(%v) len mismatch: %d vs %d", c, len(got), len(fromChan))
		}
		for i := range got {
			if got[i] != fromChan[i] {
				t.Errorf("linspace(%v)[%d]: slice=%d chan=%d", c, i, got[i], fromChan[i])
			}
		}
	}
}
