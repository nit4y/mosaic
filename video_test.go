package mosaic

import (
	"path/filepath"
	"testing"
)

const shortClip = "testdata/kitchen.mp4"

func TestExtractFrames_ShortClip(t *testing.T) {
	frames, err := extractFrames(shortClip, nil)
	if err != nil {
		t.Fatalf("extractFrames returned error: %v", err)
	}
	defer func() {
		for _, f := range frames {
			f.Close()
		}
	}()

	// kitchen.mp4 is a ~2s @ 30fps clip → expect ~61 frames.
	if len(frames) < 45 || len(frames) > 75 {
		t.Errorf("expected ~61 frames, got %d", len(frames))
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
	frames, err := extractFrames(filepath.Join("testdata", "does_not_exist.mp4"), nil)
	if err == nil {
		t.Fatalf("expected error for missing file, got %d frames", len(frames))
	}
	for _, f := range frames {
		f.Close()
	}
}

func TestGenerateVideoFromFrames_Empty(t *testing.T) {
	if err := generateVideoFromFrames(nil, filepath.Join(t.TempDir(), "x.mp4"), 30, nil); err == nil {
		t.Error("expected error for empty frame list, got nil")
	}
}
