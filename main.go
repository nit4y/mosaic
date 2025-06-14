package main

import (
	"github.com/nit4y/go-panoramic-mosaic/internal/logger"
	"github.com/nit4y/go-panoramic-mosaic/internal/mosaic"
)

func main() {
	// Generate panoramic mosaics for all videos in the input directory
	if err := mosaic.GenerateVideos(); err != nil {
		logger.Log.Error("Failed to generate mosaics", "error", err)
		return
	}
	logger.Log.Info("All mosaics generated successfully")
}
