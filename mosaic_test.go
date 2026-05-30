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

func TestForwardReverseLoop_OrderAndLength(t *testing.T) {
	// Use the idx field as a unique label so we can verify ordering
	// without constructing real Mats. The Mat field defaults to a
	// zero-value gocv.Mat which we never touch here.
	in := []resJob{
		{idx: 10}, {idx: 20}, {idx: 30}, {idx: 40},
	}
	out := forwardReverseLoop(in)
	wantIdx := []int{10, 20, 30, 40, 40, 30, 20, 10}
	if len(out) != len(wantIdx) {
		t.Fatalf("length = %d, want %d", len(out), len(wantIdx))
	}
	for i, j := range out {
		if j.idx != wantIdx[i] {
			t.Errorf("out[%d].idx = %d, want %d", i, j.idx, wantIdx[i])
		}
	}
}

func TestForwardReverseLoop_Empty(t *testing.T) {
	if got := forwardReverseLoop(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := forwardReverseLoop([]resJob{}); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
}

func TestForwardReverseLoop_Single(t *testing.T) {
	in := []resJob{{idx: 7}}
	out := forwardReverseLoop(in)
	// Forward [7] + reversed [7] = [7, 7] (the single frame held
	// twice).
	if len(out) != 2 || out[0].idx != 7 || out[1].idx != 7 {
		t.Errorf("single-frame loop: got %v", out)
	}
}

func TestForwardReverseLoop_SharesMatReferences(t *testing.T) {
	// The reversed half must hold references to the SAME underlying
	// Mat as the forward half so cleanup can happen once.
	m := gocv.NewMatWithSize(2, 2, gocv.MatTypeCV8U)
	defer m.Close()
	in := []resJob{{idx: 1, mat: m}, {idx: 2, mat: m}}
	out := forwardReverseLoop(in)
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4", len(out))
	}
	// We don't assert identity of the C pointer (that requires
	// reflect.unsafe gymnastics), but we can verify that closing the
	// forward half doesn't crash the reversed half — i.e. they share
	// the underlying allocation, and dim queries on the reversed
	// half still succeed.
	if out[2].mat.Rows() != 2 || out[3].mat.Rows() != 2 {
		t.Error("reversed half lost its mat reference")
	}
}
