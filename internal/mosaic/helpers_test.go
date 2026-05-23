package mosaic

import (
	"math"
	"testing"

	"github.com/nit4y/go-panoramic-mosaic/internal/config"
	"gocv.io/x/gocv"
)

func TestMedian(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{42}, 42},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{1, 2, 3, 4}, 2.5},
		{"negatives", []float64{-5, -1, -3}, -3},
		{"mixed", []float64{-2, 0, 2}, 0},
		{"duplicates", []float64{5, 5, 5, 5}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Median(tc.in)
			if got != tc.want {
				t.Errorf("Median(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMedianDoesNotMutateInput(t *testing.T) {
	in := []float64{5, 2, 4, 1, 3}
	cp := append([]float64(nil), in...)
	_ = Median(in)
	for i := range in {
		if in[i] != cp[i] {
			t.Fatalf("Median mutated input at %d: got %v want %v", i, in[i], cp[i])
		}
	}
}

func TestClampInt(t *testing.T) {
	cases := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
		{0, 0, 10, 0},
		{10, 0, 10, 10},
		{-5, -10, -1, -5},
	}
	for _, tc := range cases {
		got := clampInt(tc.v, tc.lo, tc.hi)
		if got != tc.want {
			t.Errorf("clampInt(%d, %d, %d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func TestCalcMotionDirection(t *testing.T) {
	cases := []struct {
		name     string
		pts1     []gocv.Point2f
		pts2     []gocv.Point2f
		wantDir  string
	}{
		{
			name:    "empty defaults to left",
			pts1:    nil,
			pts2:    nil,
			wantDir: config.Left,
		},
		{
			name:    "right motion",
			pts1:    []gocv.Point2f{{X: 0, Y: 0}, {X: 0, Y: 0}},
			pts2:    []gocv.Point2f{{X: 10, Y: 0}, {X: 12, Y: 1}},
			wantDir: config.Right,
		},
		{
			name:    "left motion",
			pts1:    []gocv.Point2f{{X: 100, Y: 50}},
			pts2:    []gocv.Point2f{{X: 90, Y: 51}},
			wantDir: config.Left,
		},
		{
			name:    "down motion (positive Y)",
			pts1:    []gocv.Point2f{{X: 0, Y: 0}},
			pts2:    []gocv.Point2f{{X: 1, Y: 20}},
			wantDir: config.Down,
		},
		{
			name:    "up motion (negative Y)",
			pts1:    []gocv.Point2f{{X: 0, Y: 100}},
			pts2:    []gocv.Point2f{{X: -1, Y: 50}},
			wantDir: config.Up,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CalcMotionDirection(tc.pts1, tc.pts2)
			if got != tc.wantDir {
				t.Errorf("CalcMotionDirection() = %q, want %q", got, tc.wantDir)
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

	h := ToHomogeneous(aff)
	defer h.Close()

	if h.Rows() != 3 || h.Cols() != 3 {
		t.Fatalf("ToHomogeneous result is %dx%d, want 3x3", h.Rows(), h.Cols())
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
				t.Errorf("ToHomogeneous[%d,%d] = %v, want %v", r, c, got, want[r][c])
			}
		}
	}
}

func TestStablizeTranslation(t *testing.T) {
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

	out := StablizeTranslation(h)
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
	// translations should be preserved
	if got := out.GetDoubleAt(0, 2); math.Abs(got-25) > 1e-9 {
		t.Errorf("tx = %v, want 25", got)
	}
	if got := out.GetDoubleAt(1, 2); math.Abs(got-(-7)) > 1e-9 {
		t.Errorf("ty = %v, want -7", got)
	}
}

func TestLinspaceChan(t *testing.T) {
	cases := []struct {
		name  string
		start int
		stop  int
		count int
		want  []int
	}{
		{"empty", 0, 10, 0, []int{}},
		{"single", 5, 5, 1, []int{5}},
		{"endpoints", 0, 10, 11, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{"three", 0, 10, 3, []int{0, 5, 10}},
		{"two", 0, 10, 2, []int{0, 10}},
		{"start equals stop with count", 7, 7, 3, []int{7, 7, 7}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := []int{}
			for v := range LinspaceChan(tc.start, tc.stop, tc.count) {
				got = append(got, v)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("LinspaceChan(%d,%d,%d) length = %d, want %d (got %v)",
					tc.start, tc.stop, tc.count, len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("LinspaceChan[%d] = %d, want %d", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRotateFrameRoundTrip(t *testing.T) {
	// A frame with a recognizable pattern: 4x4 with a unique value at (0,0).
	src := gocv.NewMatWithSize(4, 4, gocv.MatTypeCV8UC3)
	defer src.Close()
	src.SetTo(gocv.NewScalar(50, 100, 150, 0))
	// Mark top-left corner so we can detect orientation flips.
	src.SetUCharAt(0, 0, 200)
	src.SetUCharAt(0, 1, 200)
	src.SetUCharAt(0, 2, 200)

	for _, dir := range []string{config.Left, config.Right, config.Up, config.Down} {
		t.Run(dir, func(t *testing.T) {
			rotated := RotateFrame(src, dir)
			defer rotated.Close()
			restored := RotateFrameBack(rotated, dir)
			defer restored.Close()

			if restored.Rows() != src.Rows() || restored.Cols() != src.Cols() {
				t.Fatalf("dir=%s dims changed: got %dx%d, want %dx%d",
					dir, restored.Rows(), restored.Cols(), src.Rows(), src.Cols())
			}
			// Compare every pixel against original to ensure round-trip identity.
			for r := 0; r < src.Rows(); r++ {
				for c := 0; c < src.Cols(); c++ {
					sv := src.GetVecbAt(r, c)
					rv := restored.GetVecbAt(r, c)
					if sv[0] != rv[0] || sv[1] != rv[1] || sv[2] != rv[2] {
						t.Fatalf("dir=%s pixel (%d,%d): got %v want %v", dir, r, c, rv, sv)
					}
				}
			}
		})
	}
}
