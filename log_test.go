package mosaic

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// captureLogger returns a debug-level slog logger writing into a buffer so
// tests can assert exactly what was (or wasn't) emitted.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return l, &buf
}

func TestLogger_VerboseEmitsAllLevels(t *testing.T) {
	l, buf := captureLogger()
	lg := NewLogger(l, true)

	lg.Info("info-msg", "k", "v")
	lg.Error("error-msg")
	lg.Warn("warn-msg")
	lg.Debug("debug-msg")

	out := buf.String()
	for _, want := range []string{"info-msg", "error-msg", "warn-msg", "debug-msg", "k=v"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing %q; got:\n%s", want, out)
		}
	}
}

func TestLogger_NotVerboseIsNoop(t *testing.T) {
	l, buf := captureLogger()
	lg := NewLogger(l, false) // logger present, but verbose off

	lg.Info("x")
	lg.Error("x")
	lg.Warn("x")
	lg.Debug("x")

	if buf.Len() != 0 {
		t.Errorf("non-verbose logger emitted output: %q", buf.String())
	}
}

func TestLogger_NilSafe(t *testing.T) {
	// Verbose with a nil underlying logger must stay silent and not panic.
	NewLogger(nil, true).Info("x")
	NewLogger(nil, true).Error("x")

	// A nil *Logger must be a safe no-op on every method.
	var lg *Logger
	lg.Info("x")
	lg.Error("x")
	lg.Warn("x")
	lg.Debug("x")
	if got := lg.With("k", "v"); got != nil {
		t.Errorf("nil.With() = %v, want nil", got)
	}
}

func TestLogger_WithPreservesVerboseAndContext(t *testing.T) {
	l, buf := captureLogger()
	NewLogger(l, true).With("video", "boat.mp4").Info("processing")

	out := buf.String()
	if !strings.Contains(out, "processing") || !strings.Contains(out, "video=boat.mp4") {
		t.Errorf("With() context/message missing; got:\n%s", out)
	}

	// With() on a non-verbose logger remains a no-op.
	l2, buf2 := captureLogger()
	NewLogger(l2, false).With("video", "x").Info("nope")
	if buf2.Len() != 0 {
		t.Errorf("non-verbose With() emitted output: %q", buf2.String())
	}
}
