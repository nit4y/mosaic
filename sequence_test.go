package mosaic

import (
	"image"
	"testing"

	"gocv.io/x/gocv"
)

// solidMat returns a w×h BGR Mat filled with one color.
func solidMat(w, h int, b, g, r uint8) gocv.Mat {
	m := gocv.NewMatWithSize(h, w, gocv.MatTypeCV8UC3)
	m.SetTo(gocv.NewScalar(float64(b), float64(g), float64(r), 0))
	return m
}

// blackWithContent returns a canvasW×canvasH black Mat with a solid white
// rectangle at the given position.
func blackWithContent(canvasW, canvasH int, rect image.Rectangle) gocv.Mat {
	m := gocv.NewMatWithSize(canvasH, canvasW, gocv.MatTypeCV8UC3)
	m.SetTo(gocv.NewScalar(0, 0, 0, 0))
	roi := m.Region(rect)
	roi.SetTo(gocv.NewScalar(255, 255, 255, 0))
	roi.Close()
	return m
}

func TestKindString(t *testing.T) {
	if Static.String() != "static" {
		t.Errorf("Static.String() = %q, want static", Static.String())
	}
	if Dynamic.String() != "dynamic" {
		t.Errorf("Dynamic.String() = %q, want dynamic", Dynamic.String())
	}
}

func TestPanoramaCount(t *testing.T) {
	cases := []struct {
		kind  Kind
		total int
		want  int
	}{
		{Static, 120, 60},
		{Dynamic, 120, 120},
		{Static, 1, 1},
		{Static, 0, 1},
		{Dynamic, 0, 1},
	}
	for _, tc := range cases {
		if got := panoramaCount(tc.kind, tc.total); got != tc.want {
			t.Errorf("panoramaCount(%v, %d) = %d, want %d", tc.kind, tc.total, got, tc.want)
		}
	}
}

func TestContentRect(t *testing.T) {
	t.Run("content in middle", func(t *testing.T) {
		want := image.Rect(10, 5, 30, 15)
		m := blackWithContent(100, 40, want)
		defer m.Close()
		got := contentRect(m)
		if got != want {
			t.Errorf("contentRect = %v, want %v", got, want)
		}
	})
	t.Run("all black returns full", func(t *testing.T) {
		m := gocv.NewMatWithSize(20, 50, gocv.MatTypeCV8UC3)
		m.SetTo(gocv.NewScalar(0, 0, 0, 0))
		defer m.Close()
		got := contentRect(m)
		want := image.Rect(0, 0, 50, 20)
		if got != want {
			t.Errorf("contentRect(all black) = %v, want full %v", got, want)
		}
	})
	t.Run("empty returns zero-size full", func(t *testing.T) {
		m := gocv.NewMat()
		defer m.Close()
		got := contentRect(m)
		if got != image.Rect(0, 0, 0, 0) {
			t.Errorf("contentRect(empty) = %v, want zero rect", got)
		}
	})
}

func TestPadToCommonSize(t *testing.T) {
	small := solidMat(20, 10, 50, 60, 70)
	big := solidMat(40, 30, 80, 90, 100)
	defer small.Close()
	defer big.Close()

	out := padToCommonSize([]gocv.Mat{small, big})
	defer func() {
		for _, m := range out {
			m.Close()
		}
	}()

	for i, m := range out {
		if m.Cols() != 40 || m.Rows() != 30 {
			t.Fatalf("frame %d: got %dx%d, want 40x30", i, m.Cols(), m.Rows())
		}
	}
	// small's content preserved at top-left, no distortion.
	if v := out[0].GetVecbAt(0, 0); v[0] != 50 || v[1] != 60 || v[2] != 70 {
		t.Errorf("small top-left = %v, want [50 60 70]", v)
	}
	// padding region (bottom-right beyond small's 20x10) is black.
	if v := out[0].GetVecbAt(20, 30); v[0] != 0 || v[1] != 0 || v[2] != 0 {
		t.Errorf("small padding = %v, want black", v)
	}
}

func TestBuildSequence_StaticIsPingPong(t *testing.T) {
	panoramas := []resJob{
		{idx: 0, mat: solidMat(4, 4, 1, 0, 0)},
		{idx: 1, mat: solidMat(4, 4, 2, 0, 0)},
		{idx: 2, mat: solidMat(4, 4, 3, 0, 0)},
	}
	defer func() {
		for _, p := range panoramas {
			p.mat.Close()
		}
	}()

	frames, cleanup := buildSequence(panoramas, Static)
	defer cleanup()

	wantIdx := []int{0, 1, 2, 2, 1, 0}
	if len(frames) != len(wantIdx) {
		t.Fatalf("static sequence len = %d, want %d", len(frames), len(wantIdx))
	}
	for i, w := range wantIdx {
		if frames[i].idx != w {
			t.Errorf("frame %d idx = %d, want %d", i, frames[i].idx, w)
		}
	}
}

func TestBuildSequence_DynamicTrimsPadsForward(t *testing.T) {
	// Two canvas-sized panoramas with differently-sized content regions.
	panoramas := []resJob{
		{idx: 0, mat: blackWithContent(100, 40, image.Rect(10, 5, 30, 25))},  // 20x20
		{idx: 1, mat: blackWithContent(100, 40, image.Rect(0, 0, 50, 15))},   // 50x15
	}
	defer func() {
		for _, p := range panoramas {
			p.mat.Close()
		}
	}()

	frames, cleanup := buildSequence(panoramas, Dynamic)
	defer cleanup()

	// Forward only, one frame per panorama.
	if len(frames) != 2 {
		t.Fatalf("dynamic sequence len = %d, want 2", len(frames))
	}
	// All frames padded to the common max content size: max(20,50) x max(20,15) = 50x20.
	for i, f := range frames {
		if f.mat.Cols() != 50 || f.mat.Rows() != 20 {
			t.Errorf("frame %d: got %dx%d, want 50x20", i, f.mat.Cols(), f.mat.Rows())
		}
	}
	// First frame's trimmed content (white) should be present at its top-left.
	if v := frames[0].mat.GetVecbAt(0, 0); v[0] != 255 {
		t.Errorf("dynamic frame0 top-left = %v, want white content", v)
	}
}
