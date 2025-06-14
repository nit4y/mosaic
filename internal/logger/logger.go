package logger

import (
	"log/slog"
	"os"
)

var Log *slog.Logger

func init() {
	// Create a new logger with JSON handler for structured logging
	Log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// WithVideo adds video-related context to the logger
func WithVideo(videoName string) *slog.Logger {
	return Log.With("video", videoName)
}

// WithFrame adds frame-related context to the logger
func WithFrame(frameIndex int) *slog.Logger {
	return Log.With("frame", frameIndex)
}

// WithOperation adds operation-related context to the logger
func WithOperation(operation string) *slog.Logger {
	return Log.With("operation", operation)
}
