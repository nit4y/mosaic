package mosaic

import (
	"image"

	"gocv.io/x/gocv"
)

// Kind selects how the swept panoramas are turned into an output video.
type Kind int

const (
	// Static renders one panorama swept across column offsets and plays it
	// forward then reversed (ping-pong) for a seamless loop. Frames keep
	// their full canvas size.
	Static Kind = iota

	// Dynamic renders the swept panoramas, trims each to its non-black
	// content, pads them to a common size, and plays them forward once —
	// a time-evolving "video brush" mosaic. This is the real dynamic path
	// from the reference implementation (the old code only changed the
	// output filename).
	Dynamic
)

// String implements fmt.Stringer and doubles as the output file basename.
func (k Kind) String() string {
	if k == Dynamic {
		return "dynamic"
	}
	return "static"
}

// panoramaCount returns how many unique panoramas to stitch for the given
// kind so the final video has `total` frames. Static doubles its uniques
// via ping-pong; Dynamic plays each frame once.
func panoramaCount(kind Kind, total int) int {
	if total < 1 {
		total = 1
	}
	if kind == Dynamic {
		return total
	}
	n := total / 2
	if n < 1 {
		n = 1
	}
	return n
}

// buildSequence turns stitched panoramas into the ordered frame sequence to
// write, plus a cleanup func that releases any Mats buildSequence itself
// allocated. The input panoramas remain owned by the caller (Static shares
// their references; Dynamic copies out of them), so the caller must still
// close the panoramas after writing.
func buildSequence(panoramas []resJob, kind Kind) (frames []resJob, cleanup func()) {
	if kind == Dynamic {
		return buildDynamicSequence(panoramas)
	}
	// Static: ping-pong shares the panorama Mats; nothing extra to free.
	return pingPongResJobs(panoramas), func() {}
}

// buildDynamicSequence trims each panorama to its content and pads the trims
// onto a common (max) black canvas so the output video has uniform
// dimensions without resizing (no distortion). Frames play forward once.
// The returned cleanup closes the padded frames it allocated.
func buildDynamicSequence(panoramas []resJob) ([]resJob, func()) {
	// Region returns a *view* into each panorama (no copy, no ownership of
	// the parent's pixels) bounded to its content box.
	views := make([]gocv.Mat, len(panoramas))
	for i, p := range panoramas {
		views[i] = p.mat.Region(contentRect(p.mat))
	}
	padded := padToCommonSize(views)
	for i := range views {
		views[i].Close() // closing a Region view leaves the parent intact
	}

	frames := make([]resJob, len(padded))
	for i := range padded {
		frames[i] = resJob{idx: panoramas[i].idx, mat: padded[i]}
	}
	cleanup := func() {
		for _, f := range frames {
			f.mat.Close()
		}
	}
	return frames, cleanup
}

// padToCommonSize copies each frame to the top-left of a fresh black canvas
// sized to the largest width and height across all inputs. No resizing is
// performed, so geometry is preserved; smaller frames simply get black
// padding on the right/bottom. Inputs are not closed; returned Mats are
// freshly allocated and owned by the caller.
func padToCommonSize(frames []gocv.Mat) []gocv.Mat {
	maxW, maxH := 0, 0
	for _, f := range frames {
		if f.Empty() {
			continue
		}
		if f.Cols() > maxW {
			maxW = f.Cols()
		}
		if f.Rows() > maxH {
			maxH = f.Rows()
		}
	}
	if maxW < 1 {
		maxW = 1
	}
	if maxH < 1 {
		maxH = 1
	}

	out := make([]gocv.Mat, len(frames))
	for i, f := range frames {
		canvas := gocv.NewMatWithSize(maxH, maxW, gocv.MatTypeCV8UC3)
		canvas.SetTo(gocv.NewScalar(0, 0, 0, 0))
		if !f.Empty() {
			roi := canvas.Region(image.Rect(0, 0, f.Cols(), f.Rows()))
			f.CopyTo(&roi)
			roi.Close()
		}
		out[i] = canvas
	}
	return out
}

// contentRect returns the bounding rectangle of the non-black pixels in m,
// or the full frame if m is empty or entirely black. Mirrors the bounding
// box that TrimBlackBorders crops to, but returns the rectangle so callers
// can take a zero-copy Region view instead of a clone.
func contentRect(m gocv.Mat) image.Rectangle {
	full := image.Rect(0, 0, m.Cols(), m.Rows())
	if m.Empty() {
		return full
	}

	gray := gocv.NewMat()
	defer gray.Close()
	if m.Channels() > 1 {
		gocv.CvtColor(m, &gray, gocv.ColorBGRToGray)
	} else {
		m.CopyTo(&gray)
	}

	binary := gocv.NewMat()
	defer binary.Close()
	gocv.Threshold(gray, &binary, 0, 255, gocv.ThresholdBinary)

	rows, cols := binary.Rows(), binary.Cols()
	minX, minY := cols, rows
	maxX, maxY := -1, -1
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if binary.GetUCharAt(y, x) > 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < minX || maxY < minY {
		return full // all black
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}
