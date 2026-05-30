package main

import (
	"github.com/nit4y/mosaic"
	"github.com/nit4y/mosaic/internal/logger"
)

func main() {
	// Generate panoramic mosaics for all videos in the input directory
	if err := mosaic.GenerateVideos(); err != nil {
		logger.Log.Error("Failed to generate mosaics", "error", err)
		return
	}
	logger.Log.Info("All mosaics generated successfully")
}
