# mosaic

[![CI](https://github.com/nit4y/mosaic/actions/workflows/ci.yml/badge.svg)](https://github.com/nit4y/mosaic/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nit4y/mosaic.svg)](https://pkg.go.dev/github.com/nit4y/mosaic)
[![Go Report Card](https://goreportcard.com/badge/github.com/nit4y/mosaic)](https://goreportcard.com/report/github.com/nit4y/mosaic)
[![Release](https://img.shields.io/github/v/release/nit4y/mosaic)](https://github.com/nit4y/mosaic/releases)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

Turn a panning video into a wide **panoramic mosaic** — the "VideoBrush"
strip-mosaicing technique (Peleg et al.) implemented in Go on top of
[GoCV](https://gocv.io/) / OpenCV.

## How it works

The pipeline aligns consecutive frames and paints a panorama from thin
per-frame strips:

1. **Extract & prepare** — decode frames, trim black borders, and detect the
   dominant pan direction so motion is horizontal.
2. **Align** — for each adjacent pair, detect Shi-Tomasi corners, track them
   with **Lucas-Kanade optical flow**, and fit a partial-affine transform via
   **RANSAC**. Transforms are reduced to horizontal translation and
   accumulated relative to a central reference frame.
3. **Warp** — project every frame onto a shared canvas (bounded, parallel).
4. **Stitch** — sweep a column offset across the aligned frames, painting the
   strip each frame contributes; optional feather-blending hides seams.
5. **Sequence** — emit a **static** (forward + reverse ping-pong) or
   **dynamic** (forward "video brush") mosaic and write it as MP4.

## Requirements

GoCV requires OpenCV (4.x) installed locally — see the
[GoCV install guide](https://gocv.io/getting-started/).

```sh
go get github.com/nit4y/mosaic
```

## Usage

```go
package main

import (
	"log/slog"
	"os"

	"github.com/nit4y/mosaic"
)

func main() {
	// The caller owns logging; pass nil (or verbose=false) to silence it.
	log := mosaic.NewLogger(slog.New(slog.NewTextHandler(os.Stdout, nil)), true)

	// Start from tuned defaults and override what you need.
	cfg := mosaic.DefaultConfig()
	cfg.FeatherWidth = 4

	// Static mosaics for every video in cfg.InputDir → cfg.OutputDir.
	if err := mosaic.GenerateVideos(cfg, log); err != nil {
		log.Error("generate failed", "error", err)
		os.Exit(1)
	}

	// Or a dynamic ("video brush") mosaic for a specific directory:
	// mosaic.GenerateVideosFromDir("clips", "out", mosaic.Dynamic, cfg, log)
}
```

## Configuration

All tunables live in `mosaic.Config` (`DefaultConfig()` returns the tuned
baseline). Highlights:

| Field | Purpose |
|------|---------|
| `FlattenVertical` | Flatten vertical drift into one band (horizontal pans). |
| `FeatherWidth` | Width of the seam cross-fade; `0` = hard seams. |
| `CropToCoveredBand` | Crop output to the fully-covered rows (drop wedges). |
| `MaxWorkers` | Per-stage goroutine cap (`0` = `NumCPU`). |
| `VideoConcurrency` | How many videos to process at once. |
| `OutputFPS`, `OutputLengthInSeconds` | Output video timing. |

LK / RANSAC / corner-detection parameters are exposed too — see the
[GoDoc](https://pkg.go.dev/github.com/nit4y/mosaic#Config).

## Development

```sh
go test ./...          # unit + integration tests (needs OpenCV)
go test -race ./...    # race detector
golangci-lint run      # lint + format checks
```

## License

[GPL-3.0](LICENSE).
