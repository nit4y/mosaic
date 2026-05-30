package mosaic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gocv.io/x/gocv"
)

func TestPrettyPrintMatrix(t *testing.T) {
	empty := gocv.NewMat()
	defer empty.Close()
	if got := prettyPrintMatrix(empty); got != "Empty matrix" {
		t.Errorf("empty matrix: got %q, want %q", got, "Empty matrix")
	}

	m := gocv.NewMatWithSize(2, 2, gocv.MatTypeCV64F)
	defer m.Close()
	m.SetDoubleAt(0, 0, 1.5)
	m.SetDoubleAt(1, 1, -2)
	got := prettyPrintMatrix(m)
	for _, want := range []string{"1.50", "-2.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("prettyPrintMatrix missing %q in:\n%s", want, got)
		}
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines != 1 {
		t.Errorf("2-row matrix should print on 2 lines, got %d newlines", lines)
	}
}

// TestGenerateVideos drives the default-config convenience wrapper end to
// end against a temp input dir holding the short fixture.
func TestGenerateVideos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in -short mode")
	}
	in := t.TempDir()
	out := t.TempDir()

	data, err := os.ReadFile(shortClip)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(in, "clip.mp4"), data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg := DefaultConfig()
	cfg.InputDir = in
	cfg.OutputDir = out
	if err := GenerateVideos(cfg, nil); err != nil {
		t.Fatalf("GenerateVideos: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, "clip.mp4", "static.mp4")); err != nil {
		t.Fatalf("expected static mosaic output: %v", err)
	}
}
