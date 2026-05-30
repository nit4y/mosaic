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

	// Generate a static (ping-pong) panoramic mosaic for every video in
	// the "input" directory, written under "output/<video>/static.mp4".
	//
	// Dynamic ("video brush") mosaics are disabled by default. To produce
	// them, call:
	//
	//	mosaic.GenerateVideosFromDir("input", "output", mosaic.Dynamic, lg)
	if err := mosaic.GenerateVideos(lg); err != nil {
		slogger.Error("failed to generate static mosaics", "error", err)
		return
	}

	slogger.Info("all mosaics generated successfully")
}
