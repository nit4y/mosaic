package mosaic

import (
	"os"
	"path/filepath"
	"testing"

	"gocv.io/x/gocv"
)

// TestGenerateMosaicVideo_EndToEnd runs the full mosaic pipeline on the
// short clip and verifies that an output video is produced with
// sensible dimensions and at least some non-black content. This is the
// catch-all integration guard for regressions in any pipeline stage.
func TestGenerateMosaicVideo_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in -short mode")
	}

	outputDir := t.TempDir()
	if err := GenerateMosaicVideo(shortClip, outputDir, false); err != nil {
		t.Fatalf("GenerateMosaicVideo failed: %v", err)
	}

	outFile := filepath.Join(outputDir, "static_mosaic.mp4")
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("expected output at %s, got: %v", outFile, err)
	}
	if info.Size() < 1024 {
		t.Errorf("output video unexpectedly small: %d bytes", info.Size())
	}

	// Crack open the produced mosaic and verify dimensions + that the
	// first frame is not entirely black.
	cap, err := gocv.VideoCaptureFile(outFile)
	if err != nil {
		t.Fatalf("could not open output mosaic: %v", err)
	}
	defer cap.Close()
	if !cap.IsOpened() {
		t.Fatal("output mosaic could not be opened")
	}
	frame := gocv.NewMat()
	defer frame.Close()
	if ok := cap.Read(&frame); !ok || frame.Empty() {
		t.Fatal("could not read first frame from output mosaic")
	}
	// Sanity: panorama should be wider than the source frame width
	// (640) — otherwise no panning was captured.
	if frame.Cols() < 700 {
		t.Errorf("mosaic narrower than expected: %d cols", frame.Cols())
	}
	if frame.Rows() < 400 {
		t.Errorf("mosaic shorter than expected: %d rows", frame.Rows())
	}
	// A non-trivial fraction of the panorama should be non-black. The
	// canvas legitimately has black corners where Y drift placed
	// frames at varying y positions, so we use a low threshold —
	// just enough to catch the historical "mostly-black mosaic" bug
	// (<10% painted). 25% is comfortably above what bounded edge
	// strips + Y drift produces on the short clip.
	step := 16
	nonBlack, total := 0, 0
	for y := 0; y < frame.Rows(); y += step {
		for x := 0; x < frame.Cols(); x += step {
			total++
			v := frame.GetVecbAt(y, x)
			if v[0] != 0 || v[1] != 0 || v[2] != 0 {
				nonBlack++
			}
		}
	}
	if total == 0 || float64(nonBlack)/float64(total) < 0.25 {
		t.Errorf("mosaic content sparse: %d/%d non-black samples", nonBlack, total)
	}
}

// TestGenerateVideosFromDir verifies the directory-driven entry point
// processes the lone test clip end-to-end.
func TestGenerateVideosFromDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in -short mode")
	}

	inputDir := t.TempDir()
	outputDir := t.TempDir()

	// Symlink (or copy) the fixture into a clean input dir so we don't
	// pollute the package testdata.
	src, err := filepath.Abs(shortClip)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	dst := filepath.Join(inputDir, "clip.mp4")
	if err := os.Symlink(src, dst); err != nil {
		// Fall back to copy if symlinks aren't supported.
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	if err := GenerateVideosFromDir(inputDir, outputDir); err != nil {
		t.Fatalf("GenerateVideosFromDir: %v", err)
	}

	outFile := filepath.Join(outputDir, "clip.mp4", "static_mosaic.mp4")
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("expected output at %s: %v", outFile, err)
	}
}

func TestGenerateVideosFromDir_EmptyInput(t *testing.T) {
	inputDir := t.TempDir()
	if err := GenerateVideosFromDir(inputDir, t.TempDir()); err == nil {
		t.Error("expected error for empty input dir, got nil")
	}
}

func TestListVideoFilesIgnoresNonVideos(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.mp4", "b.txt", "c.avi", "d.jpg", "e.mov"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listVideoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.mp4", "c.avi", "e.mov"}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d (%v)", len(files), len(want), files)
	}
	for i, f := range files {
		if filepath.Base(f) != want[i] {
			t.Errorf("idx %d: got %s want %s", i, filepath.Base(f), want[i])
		}
	}
}
