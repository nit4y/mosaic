package mosaic

import (
	"runtime"
	"sync"
)

// resolveWorkers clamps a requested worker count into [1, NumCPU].
//
// It is the single place the CPU guardrail lives: every parallel stage in
// the pipeline runs its worker count through here, so no stage can ever
// oversubscribe the machine. A requested value <= 0 means "auto" and
// resolves to runtime.NumCPU().
func resolveWorkers(requested int) int {
	maxWorkers := runtime.NumCPU()
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	if requested <= 0 || requested > maxWorkers {
		return maxWorkers
	}
	return requested
}

// parallelMap applies fn to every index in [0, n) using at most `workers`
// goroutines and returns the results in index order.
//
// It is the one concurrency primitive shared across the pipeline (frame
// warping, panorama stitching, across-video processing) so that bounded
// parallelism is expressed once and reviewed once. `workers` is passed
// through resolveWorkers, so 0 means "use NumCPU"; it is additionally
// capped at n (never spawn more goroutines than there is work).
//
// fn must be safe to call concurrently. Each result is written to its own
// slice index, so no synchronisation is needed around the return value.
func parallelMap[T any](n, workers int, fn func(i int) T) []T {
	results := make([]T, n)
	if n <= 0 {
		return results
	}

	w := resolveWorkers(workers)
	if w > n {
		w = n
	}

	indices := make(chan int, n)
	for i := 0; i < n; i++ {
		indices <- i
	}
	close(indices)

	var wg sync.WaitGroup
	wg.Add(w)
	for g := 0; g < w; g++ {
		go func() {
			defer wg.Done()
			for i := range indices {
				results[i] = fn(i)
			}
		}()
	}
	wg.Wait()
	return results
}
