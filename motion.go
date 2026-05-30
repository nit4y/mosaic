package mosaic

import "gocv.io/x/gocv"

// rotateFrame rotates a frame so the detected pan runs horizontally
// (the orientation the alignment and stitching stages assume).
func rotateFrame(frame gocv.Mat, direction Direction) gocv.Mat {
	var rotated gocv.Mat
	switch direction {
	case Right:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate180Clockwise)
	case Left:
		// no rotation
		rotated = frame.Clone()
	case Up:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate90Clockwise)
	case Down:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate90CounterClockwise)
	default:
		rotated = frame.Clone()
	}
	return rotated
}

// rotateFrameBack reverts the rotation applied by rotateFrame, returning a
// frame (or finished panorama) to the clip's original orientation.
func rotateFrameBack(frame gocv.Mat, direction Direction) gocv.Mat {
	var original gocv.Mat
	switch direction {
	case Right:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate180Clockwise)
	case Left:
		original = frame.Clone()
	case Up:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate90CounterClockwise)
	case Down:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate90Clockwise)
	default:
		original = frame.Clone()
	}
	return original
}

// detectMotionDirection detects the dominant motion direction in a video.
func detectMotionDirection(frames []gocv.Mat, cfg Config, lg *Logger) Direction {
	// vote with motion of first 5 frames relative to the first frame
	votes := map[Direction]int{
		Left:  0,
		Right: 0,
		Up:    0,
		Down:  0,
	}
	// limited to available frames
	limit := 6
	if len(frames) < limit {
		limit = len(frames)
	}
	for i := 1; i < limit; i++ {
		// We only need the direction here, but alignImages also returns a
		// homography Mat — close it so it isn't leaked.
		H, dir := alignImages(frames[0], frames[i], true, cfg, lg)
		if H != nil {
			H.Close()
		}
		votes[dir]++
	}
	// Pick the winner over a fixed candidate order so ties break
	// deterministically — ranging a map would randomise the result and make
	// the whole pipeline non-reproducible.
	bestDir := Left
	maxVotes := -1
	for _, dir := range []Direction{Left, Right, Up, Down} {
		if votes[dir] > maxVotes {
			maxVotes = votes[dir]
			bestDir = dir
		}
	}
	return bestDir
}
