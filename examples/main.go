package main

import (
	"log/slog"
	"os"

	"github.com/nit4y/mosaic"
)

func main() {
	// The caller owns the logger. Wrap any *slog.Logger with the desired
	// verbosity via NewLogger and hand it to the library. Pass nil — or
	// verbose=false — to silence the library entirely.
	slogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	lg := mosaic.NewLogger(slogger, true) // verbose: emit pipeline logs

	// Start from the tuned defaults and override only what you need, e.g.:
	//	cfg.Dynamic ... cfg.FeatherWidth = 4
	cfg := mosaic.DefaultConfig()

	// Generate a static (ping-pong) panoramic mosaic for every video in
	// cfg.InputDir, written under cfg.OutputDir/<video>/static.mp4.
	//
	// Dynamic ("video brush") mosaics are disabled by default. To produce
	// them, call:
	//
	//	mosaic.GenerateVideosFromDir(cfg.InputDir, cfg.OutputDir, mosaic.Dynamic, cfg, lg)
	if err := mosaic.GenerateVideos(cfg, lg); err != nil {
		slogger.Error("failed to generate static mosaics", "error", err)
		return
	}

	slogger.Info("all mosaics generated successfully")
}
