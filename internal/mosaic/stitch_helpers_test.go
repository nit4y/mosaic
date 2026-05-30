package mosaic

import (
	"image"
	"testing"

	"gocv.io/x/gocv"
)

// TestLeftmostNonBlackColumn_* exercise the leftmost-non-black column
// scan on tiny synthetic mats so the test reasons about exact pixel
// positions without needing the full pipeline.

func TestLeftmostNonBlackColumn_AllBlackReturnsMinusOne(t *testing.T) {
	m := gocv.NewMatWithSize(4, 8, gocv.MatTypeCV8UC3)
	defer m.Close()
	m.SetTo(gocv.NewScalar(0, 0, 0, 0))
	if got := leftmostNonBlackColumn(m); got != -1 {
		t.Errorf("all-black: got %d, want -1", got)
	}
}

func TestLeftmostNonBlackColumn_PixelAtCol3(t *testing.T) {
	m := gocv.NewMatWithSize(4, 8, gocv.MatTypeCV8UC3)
	defer m.Close()
	m.SetTo(gocv.NewScalar(0, 0, 0, 0))
	// Single non-black pixel at row=2, col=3.
	m.SetUCharAt(2, 3*3+0, 5) // B channel of (row=2, col=3)
	if got := leftmostNonBlackColumn(m); got != 3 {
		t.Errorf("single pixel at col 3: got %d, want 3", got)
	}
}

func TestLeftmostNonBlackColumn_BlockStartsAtCol5(t *testing.T) {
	m := gocv.NewMatWithSize(4, 12, gocv.MatTypeCV8UC3)
	defer m.Close()
	m.SetTo(gocv.NewScalar(0, 0, 0, 0))
	// 4x3 white block starting at col=5.
	roi := m.Region(image.Rect(5, 0, 8, 4))
	roi.SetTo(gocv.NewScalar(100, 100, 100, 0))
	roi.Close()
	if got := leftmostNonBlackColumn(m); got != 5 {
		t.Errorf("block at col 5: got %d, want 5", got)
	}
}

func TestLeftmostNonBlackColumn_SingleChannel(t *testing.T) {
	m := gocv.NewMatWithSize(3, 6, gocv.MatTypeCV8U)
	defer m.Close()
	m.SetTo(gocv.NewScalar(0, 0, 0, 0))
	m.SetUCharAt(1, 2, 200)
	if got := leftmostNonBlackColumn(m); got != 2 {
		t.Errorf("single-channel mat: got %d, want 2", got)
	}
}

func TestLeftmostNonBlackColumn_EmptyMat(t *testing.T) {
	m := gocv.NewMat()
	defer m.Close()
	if got := leftmostNonBlackColumn(m); got != -1 {
		t.Errorf("empty mat: got %d, want -1", got)
	}
}

// TestPaintStrip_* exercise the column-strip blit. Goal: only the
// requested column range gets painted, black source pixels stay
// transparent, and out-of-range arguments are clamped instead of
// crashing.

func TestPaintStrip_CopiesOnlyRequestedColumns(t *testing.T) {
	src := gocv.NewMatWithSize(2, 10, gocv.MatTypeCV8UC3)
	defer src.Close()
	src.SetTo(gocv.NewScalar(50, 60, 70, 0))

	dst := gocv.NewMatWithSize(2, 10, gocv.MatTypeCV8UC3)
	defer dst.Close()
	dst.SetTo(gocv.NewScalar(0, 0, 0, 0))

	painted := paintStrip(dst, src, 3, 6)
	if painted != 3 {
		t.Errorf("painted = %d, want 3", painted)
	}
	// Cols [3,6) should be (50,60,70).
	for x := 3; x < 6; x++ {
		v := dst.GetVecbAt(0, x)
		if v[0] != 50 || v[1] != 60 || v[2] != 70 {
			t.Errorf("col %d: got %v, want [50 60 70]", x, v)
		}
	}
	// Cols outside [3,6) should remain untouched (black).
	for _, x := range []int{0, 1, 2, 6, 7, 9} {
		v := dst.GetVecbAt(0, x)
		if v[0] != 0 || v[1] != 0 || v[2] != 0 {
			t.Errorf("col %d outside strip should be black, got %v", x, v)
		}
	}
}

func TestPaintStrip_BlackSourcePixelsAreMasked(t *testing.T) {
	src := gocv.NewMatWithSize(2, 8, gocv.MatTypeCV8UC3)
	defer src.Close()
	src.SetTo(gocv.NewScalar(0, 0, 0, 0))
	// Only col 4 of src has content.
	roi := src.Region(image.Rect(4, 0, 5, 2))
	roi.SetTo(gocv.NewScalar(123, 0, 0, 0))
	roi.Close()

	dst := gocv.NewMatWithSize(2, 8, gocv.MatTypeCV8UC3)
	defer dst.Close()
	// Pre-fill dst with a sentinel so we can verify masked pixels
	// remain untouched.
	dst.SetTo(gocv.NewScalar(99, 99, 99, 0))

	paintStrip(dst, src, 2, 7)
	// dst col 4 should be repainted to (123,0,0); others in [2,7)
	// remain at the sentinel (99) because src was black there.
	for x := 2; x < 7; x++ {
		v := dst.GetVecbAt(0, x)
		if x == 4 {
			if v[0] != 123 || v[1] != 0 || v[2] != 0 {
				t.Errorf("col %d: got %v, want [123 0 0]", x, v)
			}
		} else {
			if v[0] != 99 || v[1] != 99 || v[2] != 99 {
				t.Errorf("col %d should keep sentinel, got %v", x, v)
			}
		}
	}
}

func TestPaintStrip_ClampsToBounds(t *testing.T) {
	src := gocv.NewMatWithSize(2, 10, gocv.MatTypeCV8UC3)
	defer src.Close()
	src.SetTo(gocv.NewScalar(40, 40, 40, 0))
	dst := gocv.NewMatWithSize(2, 10, gocv.MatTypeCV8UC3)
	defer dst.Close()
	dst.SetTo(gocv.NewScalar(0, 0, 0, 0))

	// Out-of-bounds args clamp without panicking.
	if got := paintStrip(dst, src, -5, 20); got != 10 {
		t.Errorf("over-wide range: painted %d, want 10", got)
	}
	// Inverted range: should paint nothing.
	if got := paintStrip(dst, src, 7, 3); got != 0 {
		t.Errorf("inverted range: painted %d, want 0", got)
	}
}
