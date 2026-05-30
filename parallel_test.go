package mosaic

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveWorkers(t *testing.T) {
	ncpu := runtime.NumCPU()
	cases := []struct {
		name      string
		requested int
		want      int
	}{
		{"zero means auto", 0, ncpu},
		{"negative means auto", -4, ncpu},
		{"above ncpu clamps down", ncpu + 100, ncpu},
		{"one is allowed", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveWorkers(tc.requested); got != tc.want {
				t.Errorf("resolveWorkers(%d) = %d, want %d", tc.requested, got, tc.want)
			}
		})
	}
	// An in-range request is returned unchanged (only meaningful with >1 CPU).
	if ncpu >= 2 {
		if got := resolveWorkers(2); got != 2 {
			t.Errorf("resolveWorkers(2) = %d, want 2", got)
		}
	}
}

func TestParallelMap_PreservesOrder(t *testing.T) {
	const n = 100
	got := parallelMap(n, 4, func(i int) int { return i * i })
	for i := 0; i < n; i++ {
		if got[i] != i*i {
			t.Fatalf("result[%d] = %d, want %d", i, got[i], i*i)
		}
	}
}

func TestParallelMap_EmptyAndSingle(t *testing.T) {
	if got := parallelMap(0, 4, func(i int) int { return i }); len(got) != 0 {
		t.Errorf("n=0: got len %d, want 0", len(got))
	}
	if got := parallelMap(1, 8, func(i int) int { return 42 }); len(got) != 1 || got[0] != 42 {
		t.Errorf("n=1: got %v, want [42]", got)
	}
}

// TestParallelMap_RespectsWorkerCap verifies the in-flight goroutine count
// never exceeds the requested worker limit (the CPU guardrail). Each task
// bumps a counter, records the peak, holds briefly so overlap is observable,
// then decrements.
func TestParallelMap_RespectsWorkerCap(t *testing.T) {
	const (
		n       = 50
		workers = 3
	)
	var inFlight int32
	var peak int32
	var mu sync.Mutex

	parallelMap(n, workers, func(i int) int {
		cur := atomic.AddInt32(&inFlight, 1)
		mu.Lock()
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return i
	})

	if peak > workers {
		t.Errorf("peak in-flight = %d, exceeds worker cap %d", peak, workers)
	}
	if peak == 0 {
		t.Error("peak in-flight = 0, tasks did not run")
	}
}
