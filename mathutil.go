package mosaic

import "sort"

// median returns the median value of the input slice.
// If the slice is empty, it returns 0.
func median(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	// Copy so original slice isn’t modified
	sorted := make([]float64, n)
	copy(sorted, xs)
	sort.Float64s(sorted)

	mid := n / 2
	if n%2 == 0 {
		// even length: average two middle values
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	// odd length: return the middle value
	return sorted[mid]
}

// clampInt ensures v is between lo and hi (inclusive). The parameters are
// named lo/hi rather than min/max to avoid shadowing the Go builtins.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// linspace returns `count` evenly-spaced integer values from start to
// stop (inclusive of both endpoints when count >= 2). Returns an empty
// slice for count <= 0.
func linspace(start, stop, count int) []int {
	if count <= 0 {
		return []int{}
	}
	out := make([]int, 0, count)
	if count == 1 {
		out = append(out, start)
		return out
	}
	step := float64(stop-start) / float64(count-1)
	for i := 0; i < count; i++ {
		out = append(out, int(float64(start)+step*float64(i)))
	}
	return out
}
