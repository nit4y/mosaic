package mosaic

import "testing"

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

func TestLinspace(t *testing.T) {
	cases := []struct {
		start, stop, count int
		want               []int
	}{
		{0, 10, 0, []int{}},
		{5, 5, 1, []int{5}},
		{0, 10, 2, []int{0, 10}},
		{0, 10, 3, []int{0, 5, 10}},
		{10, 0, 3, []int{10, 5, 0}},
	}
	for _, tc := range cases {
		got := linspace(tc.start, tc.stop, tc.count)
		if len(got) != len(tc.want) {
			t.Fatalf("linspace(%d,%d,%d) len = %d, want %d", tc.start, tc.stop, tc.count, len(got), len(tc.want))
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("linspace(%d,%d,%d)[%d] = %d, want %d", tc.start, tc.stop, tc.count, i, got[i], tc.want[i])
			}
		}
	}
}
