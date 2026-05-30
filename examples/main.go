package main

import (
	"github.com/nit4y/mosaic"
	"github.com/nit4y/mosaic/internal/logger"
)

func main() {
	// Generate a static (ping-pong) panoramic mosaic for every video in
	// the "input" directory, written under "output/<video>/static.mp4".
	if err := mosaic.GenerateVideos(); err != nil {
		logger.Log.Error("Failed to generate static mosaics", "error", err)
		return
	}

	// The same videos as dynamic ("video brush") mosaics, written to
	// "output/<video>/dynamic.mp4". Pick whichever Kind you need per call.
	if err := mosaic.GenerateVideosFromDir("input", "output", mosaic.Dynamic); err != nil {
		logger.Log.Error("Failed to generate dynamic mosaics", "error", err)
		return
	}

	logger.Log.Info("All mosaics generated successfully")
}
