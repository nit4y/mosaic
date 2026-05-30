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
		if got := contentRect(m); got != want {
			t.Errorf("contentRect = %v, want %v", got, want)
		}
	})
	t.Run("all black returns full", func(t *testing.T) {
		m := gocv.NewMatWithSize(20, 50, gocv.MatTypeCV8UC3)
		m.SetTo(gocv.NewScalar(0, 0, 0, 0))
		defer m.Close()
		want := image.Rect(0, 0, 50, 20)
		if got := contentRect(m); got != want {
			t.Errorf("contentRect(all black) = %v, want full %v", got, want)
		}
	})
	t.Run("empty returns zero rect", func(t *testing.T) {
		m := gocv.NewMat()
		defer m.Close()
		if got := contentRect(m); got != image.Rect(0, 0, 0, 0) {
			t.Errorf("contentRect(empty) = %v, want zero rect", got)
		}
	})
}

func TestCommonContentRect(t *testing.T) {
	panoramas := []resJob{
		{idx: 0, mat: blackWithContent(100, 40, image.Rect(10, 5, 30, 25))},
		{idx: 1, mat: blackWithContent(100, 40, image.Rect(0, 0, 50, 15))},
	}
	defer func() {
		for _, p := range panoramas {
			p.mat.Close()
		}
	}()

	got := commonContentRect(panoramas)
	want := image.Rect(0, 0, 50, 25) // union of the two content boxes
	if got != want {
		t.Errorf("commonContentRect = %v, want %v", got, want)
	}
}

func TestBuildSequence_StaticIsPingPong(t *testing.T) {
	// Fully-colored panoramas (no black), so cropping is a no-op and we can
	// focus on the ping-pong ordering.
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

func TestBuildSequence_DynamicCropsForward(t *testing.T) {
	// Two canvas-sized panoramas with differently-placed content boxes.
	panoramas := []resJob{
		{idx: 0, mat: blackWithContent(100, 40, image.Rect(10, 5, 30, 25))},
		{idx: 1, mat: blackWithContent(100, 40, image.Rect(0, 0, 50, 15))},
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
	// All frames cropped to the common content box: union = 50x25.
	for i, f := range frames {
		if f.mat.Cols() != 50 || f.mat.Rows() != 25 {
			t.Errorf("frame %d: got %dx%d, want 50x25", i, f.mat.Cols(), f.mat.Rows())
		}
	}
	// Frame 0's white content (orig rect 10,5..30,25) sits at the same
	// place inside the cropped frame (crop origin is 0,0).
	if v := frames[0].mat.GetVecbAt(5, 10); v[0] != 255 {
		t.Errorf("dynamic frame0 (5,10) = %v, want white content", v)
	}
	if v := frames[0].mat.GetVecbAt(0, 0); v[0] != 0 {
		t.Errorf("dynamic frame0 (0,0) = %v, want black margin", v)
	}
}
