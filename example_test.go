package mosaic_test

import (
	"log/slog"
	"os"

	"github.com/nit4y/mosaic"
)

// Generate a static (forward-reverse loop) mosaic for every video in the
// configured input directory, writing each under <OutputDir>/<video>/static.mp4.
func ExampleGenerateVideos() {
	// Logging is opt-in: wrap any *slog.Logger, or pass nil to stay silent.
	log := mosaic.NewLogger(slog.New(slog.NewTextHandler(os.Stdout, nil)), true)

	// Start from the tuned defaults and override only what you need.
	cfg := mosaic.DefaultConfig()
	cfg.FeatherWidth = 4

	if err := mosaic.GenerateVideos(cfg, log); err != nil {
		log.Error("generate failed", "error", err)
	}
}

// Produce a dynamic ("video brush") mosaic for a specific input/output pair.
func ExampleGenerateVideosFromDir_dynamic() {
	cfg := mosaic.DefaultConfig()
	if err := mosaic.GenerateVideosFromDir("clips", "out", mosaic.Dynamic, cfg, nil); err != nil {
		panic(err)
	}
}
