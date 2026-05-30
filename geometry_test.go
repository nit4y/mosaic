package mosaic

import (
	"math"
	"testing"

	"gocv.io/x/gocv"
)

func TestCalcMotionDirection(t *testing.T) {
	cases := []struct {
		name    string
		pts1    []gocv.Point2f
		pts2    []gocv.Point2f
		wantDir Direction
	}{
		{
			name:    "empty defaults to left",
			pts1:    nil,
			pts2:    nil,
			wantDir: Left,
		},
		{
			name:    "right motion",
			pts1:    []gocv.Point2f{{X: 0, Y: 0}, {X: 0, Y: 0}},
			pts2:    []gocv.Point2f{{X: 10, Y: 0}, {X: 12, Y: 1}},
			wantDir: Right,
		},
		{
			name:    "left motion",
			pts1:    []gocv.Point2f{{X: 100, Y: 50}},
			pts2:    []gocv.Point2f{{X: 90, Y: 51}},
			wantDir: Left,
		},
		{
			name:    "down motion (positive Y)",
			pts1:    []gocv.Point2f{{X: 0, Y: 0}},
			pts2:    []gocv.Point2f{{X: 1, Y: 20}},
			wantDir: Down,
		},
		{
			name:    "up motion (negative Y)",
			pts1:    []gocv.Point2f{{X: 0, Y: 100}},
			pts2:    []gocv.Point2f{{X: -1, Y: 50}},
			wantDir: Up,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := calcMotionDirection(tc.pts1, tc.pts2)
			if got != tc.wantDir {
				t.Errorf("calcMotionDirection() = %q, want %q", got, tc.wantDir)
			}
		})
	}
}

func TestToHomogeneous(t *testing.T) {
	aff := gocv.NewMatWithSize(2, 3, gocv.MatTypeCV64F)
	defer aff.Close()
	// represent [[1.5, 0.0, 10], [0.0, 1.5, -20]]
	aff.SetDoubleAt(0, 0, 1.5)
	aff.SetDoubleAt(0, 1, 0.0)
	aff.SetDoubleAt(0, 2, 10)
	aff.SetDoubleAt(1, 0, 0.0)
	aff.SetDoubleAt(1, 1, 1.5)
	aff.SetDoubleAt(1, 2, -20)

	h := toHomogeneous(aff)
	defer h.Close()

	if h.Rows() != 3 || h.Cols() != 3 {
		t.Fatalf("toHomogeneous result is %dx%d, want 3x3", h.Rows(), h.Cols())
	}
	want := [3][3]float64{
		{1.5, 0.0, 10},
		{0.0, 1.5, -20},
		{0, 0, 1},
	}
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			got := h.GetDoubleAt(r, c)
			if math.Abs(got-want[r][c]) > 1e-9 {
				t.Errorf("toHomogeneous[%d,%d] = %v, want %v", r, c, got, want[r][c])
			}
		}
	}
}

func TestDampYTranslation(t *testing.T) {
	cases := []struct {
		name   string
		ty     float64
		factor float64
		want   float64
	}{
		{"no-op factor 1.0", 25, 1.0, 25},
		{"zero factor strips ty", 25, 0.0, 0},
		{"factor 0.3 reduces", 100, 0.3, 30},
		{"negative ty respected", -42, 0.5, -21},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := gocv.NewMatWithSize(3, 3, gocv.MatTypeCV64F)
			defer m.Close()
			m.SetDoubleAt(0, 0, 1)
			m.SetDoubleAt(1, 1, 1)
			m.SetDoubleAt(2, 2, 1)
			m.SetDoubleAt(0, 2, 50) // tx — must remain unchanged
			m.SetDoubleAt(1, 2, tc.ty)

			out := dampYTranslation(m, tc.factor)
			if got := out.GetDoubleAt(1, 2); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("ty: got %v, want %v", got, tc.want)
			}
			// tx untouched.
			if got := out.GetDoubleAt(0, 2); math.Abs(got-50) > 1e-9 {
				t.Errorf("tx changed unexpectedly: got %v, want 50", got)
			}
		})
	}
}

func TestDampYTranslation_EmptyMatNoCrash(t *testing.T) {
	m := gocv.NewMat()
	defer m.Close()
	// Just ensuring no panic / no nil-deref on a content-empty mat.
	_ = dampYTranslation(m, 0.5)
}

func TestStabilizeTranslation(t *testing.T) {
	// Start from a homogeneous matrix with scale and skew
	h := gocv.NewMatWithSize(3, 3, gocv.MatTypeCV64F)
	defer h.Close()
	h.SetDoubleAt(0, 0, 1.3)
	h.SetDoubleAt(0, 1, 0.1)
	h.SetDoubleAt(0, 2, 25)
	h.SetDoubleAt(1, 0, -0.2)
	h.SetDoubleAt(1, 1, 0.9)
	h.SetDoubleAt(1, 2, -7)
	h.SetDoubleAt(2, 0, 0)
	h.SetDoubleAt(2, 1, 0)
	h.SetDoubleAt(2, 2, 1)

	out := stabilizeTranslation(h, 1.0)
	// stabilization should zero skew (0,1) and (1,0) and set diag to 1
	if got := out.GetDoubleAt(0, 0); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("scale x = %v, want 1.0", got)
	}
	if got := out.GetDoubleAt(1, 1); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("scale y = %v, want 1.0", got)
	}
	if got := out.GetDoubleAt(0, 1); math.Abs(got) > 1e-9 {
		t.Errorf("skew (0,1) = %v, want 0", got)
	}
	if got := out.GetDoubleAt(1, 0); math.Abs(got) > 1e-9 {
		t.Errorf("skew (1,0) = %v, want 0", got)
	}
	// tx should be preserved.
	if got := out.GetDoubleAt(0, 2); math.Abs(got-25) > 1e-9 {
		t.Errorf("tx = %v, want 25", got)
	}
	// ty is scaled by the damping passed to stabilizeTranslation (1.0 = no-op).
	wantTy := -7 * 1.0
	if got := out.GetDoubleAt(1, 2); math.Abs(got-wantTy) > 1e-9 {
		t.Errorf("ty = %v, want %v (damped)", got, wantTy)
	}
}
