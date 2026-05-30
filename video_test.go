package mosaic

import (
	"path/filepath"
	"testing"
)

const shortClip = "testdata/boat_short.mp4"

func TestExtractFrames_ShortClip(t *testing.T) {
	frames, err := ExtractFrames(shortClip, nil)
	if err != nil {
		t.Fatalf("ExtractFrames returned error: %v", err)
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	// boat_short.mp4 is a ~2s @ 15fps clip → expect ~30 frames.
	if len(frames) < 20 || len(frames) > 40 {
		t.Errorf("expected ~30 frames, got %d", len(frames))
	}
	if len(frames) == 0 {
		t.Fatal("no frames extracted")
	}
	if frames[0].Empty() {
		t.Error("first frame is empty")
	}
	if frames[0].Cols() <= 0 || frames[0].Rows() <= 0 {
		t.Errorf("frame has invalid dimensions: %dx%d", frames[0].Cols(), frames[0].Rows())
	}
}

func TestExtractFrames_BadPath(t *testing.T) {
	// A path that points nowhere should produce an error, not a slice of
	// frames from a silently-failed VideoCapture.
	frames, err := ExtractFrames(filepath.Join("testdata", "does_not_exist.mp4"), nil)
	if err == nil {
		t.Fatalf("expected error for missing file, got %d frames", len(frames))
	}
	for _, f := range frames {
		f.Close()
	}
}

func TestGenerateVideoFromFrames_Empty(t *testing.T) {
	if err := GenerateVideoFromFrames(nil, filepath.Join(t.TempDir(), "x.mp4"), 30, nil); err == nil {
		t.Error("expected error for empty frame list, got nil")
	}
}
