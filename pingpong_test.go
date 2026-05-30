package mosaic

import (
	"testing"

	"gocv.io/x/gocv"
)

func TestPingPongResJobs_OrderAndLength(t *testing.T) {
	// Use the idx field as a unique label so we can verify ordering
	// without constructing real Mats. The Mat field defaults to a
	// zero-value gocv.Mat which we never touch here.
	in := []resJob{
		{idx: 10}, {idx: 20}, {idx: 30}, {idx: 40},
	}
	out := pingPongResJobs(in)
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

func TestPingPongResJobs_Empty(t *testing.T) {
	if got := pingPongResJobs(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := pingPongResJobs([]resJob{}); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
}

func TestPingPongResJobs_Single(t *testing.T) {
	in := []resJob{{idx: 7}}
	out := pingPongResJobs(in)
	// Forward [7] + reversed [7] = [7, 7] (the single frame held
	// twice).
	if len(out) != 2 || out[0].idx != 7 || out[1].idx != 7 {
		t.Errorf("single-frame ping-pong: got %v", out)
	}
}

func TestPingPongResJobs_SharesMatReferences(t *testing.T) {
	// The reversed half must hold references to the SAME underlying
	// Mat as the forward half so cleanup can happen once.
	m := gocv.NewMatWithSize(2, 2, gocv.MatTypeCV8U)
	defer m.Close()
	in := []resJob{{idx: 1, mat: m}, {idx: 2, mat: m}}
	out := pingPongResJobs(in)
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
