package main

import (
	"github.com/nit4y/mosaic"
	"github.com/nit4y/mosaic/internal/logger"
)

func main() {
	// Generate a static (ping-pong) panoramic mosaic for every video in
	// the "input" directory, written under "output/<video>/static.mp4".
	//
	// Dynamic ("video brush") mosaics are disabled by default. To produce
	// them, call:
	//
	//	mosaic.GenerateVideosFromDir("input", "output", mosaic.Dynamic)
	if err := mosaic.GenerateVideos(); err != nil {
		logger.Log.Error("Failed to generate static mosaics", "error", err)
		return
	}

	logger.Log.Info("All mosaics generated successfully")
}
