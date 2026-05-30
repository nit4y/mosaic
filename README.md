<p align="center">
  <img src="assets/logo.png" alt="mosaic logo" width="420">
</p>

<h1 align="center">mosaic</h1>

[![CI](https://github.com/nit4y/mosaic/actions/workflows/ci.yml/badge.svg)](https://github.com/nit4y/mosaic/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nit4y/mosaic.svg)](https://pkg.go.dev/github.com/nit4y/mosaic)
[![Go Report Card](https://goreportcard.com/badge/github.com/nit4y/mosaic?style=flat)](https://goreportcard.com/report/github.com/nit4y/mosaic)
[![Release](https://img.shields.io/github/v/release/nit4y/mosaic)](https://github.com/nit4y/mosaic/releases)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

Turn a panning video into a wide **panoramic mosaic** - the "VideoBrush"
strip-mosaicing technique (Peleg et al.) implemented in Go on top of
[GoCV](https://gocv.io/) / OpenCV.

## Example

**Input** - a panning video:

<p align="center">
  <img src="https://media3.giphy.com/media/v1.Y2lkPTc5MGI3NjExMWV1cW0yMTZ6bGJxaHNwNHE4bWR5MW9sMTFrNmp3ODBlMnBvYnFxaCZlcD12MV9pbnRlcm5hbF9naWZfYnlfaWQmY3Q9Zw/wDRCa9PrISLu4qluLH/giphy.gif" alt="input panning video" width="640">
</p>

**Output** - the mosaic panoramas video:

<p align="center">
  <img src="https://media3.giphy.com/media/v1.Y2lkPTc5MGI3NjExcms3NGgwOXh2NWV3OTJmc2Nubjk0eTZ1bWZiM3UzcHE5OGo2dnIyeCZlcD12MV9pbnRlcm5hbF9naWZfYnlfaWQmY3Q9Zw/CPBBmaFvuIvQ3l4a4e/giphy.gif" alt="output panoramic mosaic" width="100%">
</p>

## How it works

1. **Extract frames** - decode the video, trim black borders, and detect the
   dominant pan direction so motion runs horizontally.
2. **Align adjacent frames** - track [Shi Tomasi corners]([url](https://en.wikipedia.org/wiki/Corner_detection#The_Harris_&_Stephens_/_Shi%E2%80%93Tomasi_corner_detection_algorithms)) across each consecutive
   pair with [Lucas-Kanade]([url](https://en.wikipedia.org/wiki/Lucas%E2%80%93Kanade_method)) optical flow, then fit a [RANSAC]([url](https://en.wikipedia.org/wiki/Random_sample_consensus)) partial-affine
   transform reduced to horizontal translation, accumulated against a central
   reference frame.
3. **Warp** - project every aligned frame onto a shared canvas in parallel.
4. **Stitch** - sweep a column offset across the frames, painting each frame's
   thin strip; optional feather-blending hides the seams.

## Requirements

GoCV requires OpenCV (4.x) installed locally - see the
[GoCV install guide](https://gocv.io/getting-started/).

```sh
go get github.com/nit4y/mosaic
```

## License

[GPL-3.0](LICENSE).
