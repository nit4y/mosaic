package mosaic

import (
	"image"

	"gocv.io/x/gocv"
)

// Kind selects how the swept panoramas are turned into an output video.
type Kind int

const (
	// Static renders one panorama swept across column offsets and plays it
	// forward then reversed (ping-pong) for a seamless loop.
	Static Kind = iota

	// Dynamic renders the swept panoramas and plays them forward once — a
	// time-evolving "video brush" mosaic. This is the real dynamic path
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
// write, plus a cleanup func that releases the Mats it allocated. Every
// panorama is first cropped to the common content box so the output is
// tight (no black margins) and uniform in size — an improvement over the
// reference, which leaves the wedge/margin black in the frame. Static then
// ping-pongs the cropped frames; Dynamic plays them forward once.
//
// The input panoramas are not consumed (cropping copies out of them), so
// the caller still owns and must close them.
func buildSequence(panoramas []resJob, kind Kind) (frames []resJob, cleanup func()) {
	cropped := cropToCommonContent(panoramas)
	cleanup = func() {
		for _, f := range cropped {
			f.mat.Close()
		}
	}
	if kind == Dynamic {
		return cropped, cleanup
	}
	// Static: ping-pong shares the cropped Mats; cleanup still closes each
	// unique Mat exactly once (it iterates `cropped`, not the doubled seq).
	return pingPongResJobs(cropped), cleanup
}

// cropToCommonContent crops every panorama to the union of their non-black
// content boxes, returning freshly-allocated uniform-size frames in the
// same order. Using one common rectangle (rather than per-frame trimming)
// keeps the frames identically sized so they form a valid video without
// resizing or padding.
func cropToCommonContent(panoramas []resJob) []resJob {
	rect := commonContentRect(panoramas)
	out := make([]resJob, len(panoramas))
	for i, p := range panoramas {
		view := p.mat.Region(rect)
		out[i] = resJob{idx: p.idx, mat: view.Clone()}
		view.Close()
	}
	return out
}

// commonContentRect returns the smallest rectangle covering the non-black
// content of every panorama (the union of their content boxes). All
// panoramas share the canvas size, so the union is valid for each. Returns
// a 1×1 rect if there are no frames.
func commonContentRect(panoramas []resJob) image.Rectangle {
	union := image.Rectangle{}
	for _, p := range panoramas {
		if p.mat.Empty() {
			continue
		}
		r := contentRect(p.mat)
		if union.Empty() {
			union = r
		} else {
			union = union.Union(r)
		}
	}
	if union.Empty() {
		return image.Rect(0, 0, 1, 1)
	}
	return union
}

// contentRect returns the bounding rectangle of the non-black pixels in m,
// or the full frame if m is empty or entirely black.
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
