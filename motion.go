package mosaic

import "gocv.io/x/gocv"

// RotateFrame rotates a frame to align motion to the right.
func RotateFrame(frame gocv.Mat, direction Direction) gocv.Mat {
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

// RotateFrameBack reverts rotation applied for alignment.
func RotateFrameBack(frame gocv.Mat, direction Direction) gocv.Mat {
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

// DetectMotionDirection detects the dominant motion direction in a video.
func DetectMotionDirection(frames []gocv.Mat, cfg Config, lg *Logger) Direction {
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
		_, dir := AlignImages(frames[0], frames[i], true, cfg, lg)
		votes[dir]++
	}
	// find direction with highest votes
	bestDir := Left
	maxVotes := -1
	for dir, count := range votes {
		if count > maxVotes {
			maxVotes = count
			bestDir = dir
		}
	}
	return bestDir
}
