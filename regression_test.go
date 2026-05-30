package mosaic

import (
	"testing"

	"gocv.io/x/gocv"
)

// panFrames builds a sequence of `count` frames simulating a camera pan by
// `step` pixels per frame in (dx, dy) from a textured base frame.
func panFrames(t *testing.T, count, dx, dy int) []gocv.Mat {
	t.Helper()
	base := makeTexturedFrame(t)
	defer base.Close()
	frames := make([]gocv.Mat, count)
	for i := range frames {
		frames[i] = shiftedCopy(base, i*dx, i*dy)
	}
	return frames
}

// TestPickDirection_DeterministicTieBreak locks in the deterministic tie-break:
// on a tie the winner must be the first candidate in the fixed order. This
// guards against anyone reverting to a plain `range votes` map iteration, whose
// winner on a tie would be random and make the pipeline non-reproducible.
func TestPickDirection_DeterministicTieBreak(t *testing.T) {
	// Four-way tie → must always resolve to Left (first in the fixed order).
	tie := map[Direction]int{Left: 1, Right: 1, Up: 1, Down: 1}
	for i := 0; i < 100; i++ {
		if got := pickDirection(tie); got != Left {
			t.Fatalf("tie did not resolve deterministically: got %q, want %q", got, Left)
		}
	}
	// Two-way tie between Right and Up → Right wins (earlier in the order).
	if got := pickDirection(map[Direction]int{Right: 2, Up: 2}); got != Right {
		t.Errorf("Right/Up tie = %q, want Right", got)
	}
	// A clear winner is always respected.
	if got := pickDirection(map[Direction]int{Left: 0, Right: 1, Up: 3, Down: 0}); got != Up {
		t.Errorf("clear winner = %q, want Up", got)
	}
}

// TestDetectMotionDirection_Deterministic checks the end-to-end direction vote:
// the result must be stable across repeated calls and recover the obvious
// dominant direction for each pan.
func TestDetectMotionDirection_Deterministic(t *testing.T) {
	cases := []struct {
		name    string
		dx, dy  int
		wantDir Direction
	}{
		{"pan right", 6, 0, Right},
		{"pan left", -6, 0, Left},
		{"pan down", 0, 6, Down},
		{"pan up", 0, -6, Up},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := panFrames(t, 6, tc.dx, tc.dy)
			defer closeMats(frames)

			cfg := DefaultConfig()
			first := detectMotionDirection(frames, cfg, nil)
			if first != tc.wantDir {
				t.Fatalf("direction = %q, want %q", first, tc.wantDir)
			}
			// Repeat: a map-iteration tie-break would eventually diverge.
			for i := 0; i < 20; i++ {
				if got := detectMotionDirection(frames, cfg, nil); got != first {
					t.Fatalf("non-deterministic direction: run %d = %q, first = %q", i, got, first)
				}
			}
		})
	}
}

// TestGenerateMosaicVideo_RestoresOrientation locks in the rotate-back step: a
// non-left pan must produce an output whose dimensions match the un-rotated
// orientation. For an up/down (vertical) pan the alignment space is rotated 90°,
// so restoreOrientation must transpose the finished mosaic back rather than
// leaving it in the rotated (wide) orientation.
func TestGenerateMosaicVideo_RestoresOrientation(t *testing.T) {
	// A vertical (downward) pan: frames are rotated 90° for alignment, so
	// without the restore step the output would stay in the rotated (wide)
	// orientation. After restore, the long axis of the mosaic is vertical.
	frames := panFrames(t, 8, 0, 5)
	defer closeMats(frames)

	dir := detectMotionDirection(frames, DefaultConfig(), nil)
	if dir != Down {
		t.Skipf("synthetic clip detected as %q, not Down; orientation check needs a vertical pan", dir)
	}

	// Drive the inner pipeline the way GenerateMosaicVideo does, then assert
	// the restore step changes orientation back for a vertical pan.
	cfg := DefaultConfig()
	rotated := make([]gocv.Mat, len(frames))
	for i := range frames {
		rotated[i] = rotateFrame(frames[i], dir)
	}
	defer closeMats(rotated)

	transforms, geom := buildCanvasTransforms(rotated, cfg, nil)
	defer closeMats(transforms)
	warped := warpFrames(rotated, transforms, geom, cfg)
	defer closeMats(warped)

	panoramas := sweepPanoramas("test", warped, geom, Static, cfg, nil)
	defer closeResJobs(panoramas)

	if len(panoramas) == 0 || panoramas[0].mat.Empty() {
		t.Fatal("no panorama produced")
	}
	beforeW, beforeH := panoramas[0].mat.Cols(), panoramas[0].mat.Rows()

	restoreOrientation(panoramas, dir)
	afterW, afterH := panoramas[0].mat.Cols(), panoramas[0].mat.Rows()

	// A 90° restore swaps width and height.
	if afterW != beforeH || afterH != beforeW {
		t.Errorf("restoreOrientation(Down) did not transpose dimensions: before %dx%d, after %dx%d",
			beforeW, beforeH, afterW, afterH)
	}
}
