package mosaic

import (
	"testing"

	"gocv.io/x/gocv"
)

func TestRotateFrameRoundTrip(t *testing.T) {
	// A frame with a recognizable pattern: 4x4 with a unique value at (0,0).
	src := gocv.NewMatWithSize(4, 4, gocv.MatTypeCV8UC3)
	defer src.Close()
	src.SetTo(gocv.NewScalar(50, 100, 150, 0))
	// Mark top-left corner so we can detect orientation flips.
	src.SetUCharAt(0, 0, 200)
	src.SetUCharAt(0, 1, 200)
	src.SetUCharAt(0, 2, 200)

	for _, dir := range []Direction{Left, Right, Up, Down} {
		t.Run(string(dir), func(t *testing.T) {
			rotated := rotateFrame(src, dir)
			defer rotated.Close()
			restored := rotateFrameBack(rotated, dir)
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
