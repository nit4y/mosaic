package mosaic

import (
	"github.com/nit4y/mosaic/internal/config"
	"gocv.io/x/gocv"
)

// RotateFrame rotates a frame to align motion to the right.
func RotateFrame(frame gocv.Mat, direction string) gocv.Mat {
	var rotated gocv.Mat
	switch direction {
	case config.Right:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate180Clockwise)
	case config.Left:
		// no rotation
		rotated = frame.Clone()
	case config.Up:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate90Clockwise)
	case config.Down:
		rotated = gocv.NewMat()
		gocv.Rotate(frame, &rotated, gocv.Rotate90CounterClockwise)
	default:
		rotated = frame.Clone()
	}
	return rotated
}

// RotateFrameBack reverts rotation applied for alignment.
func RotateFrameBack(frame gocv.Mat, direction string) gocv.Mat {
	var original gocv.Mat
	switch direction {
	case config.Right:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate180Clockwise)
	case config.Left:
		original = frame.Clone()
	case config.Up:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate90CounterClockwise)
	case config.Down:
		original = gocv.NewMat()
		gocv.Rotate(frame, &original, gocv.Rotate90Clockwise)
	default:
		original = frame.Clone()
	}
	return original
}

// DetectMotionDirection detects the dominant motion direction in a video.
func DetectMotionDirection(frames []gocv.Mat) string {
	// vote with motion of first 5 frames relative to the first frame
	votes := map[string]int{
		config.Left:  0,
		config.Right: 0,
		config.Up:    0,
		config.Down:  0,
	}
	// limited to available frames
	limit := 6
	if len(frames) < limit {
		limit = len(frames)
	}
	for i := 1; i < limit; i++ {
		_, dir := AlignImages(frames[0], frames[i], true)
		votes[dir]++
	}
	// find direction with highest votes
	bestDir := config.Left
	maxVotes := -1
	for dir, count := range votes {
		if count > maxVotes {
			maxVotes = count
			bestDir = dir
		}
	}
	return bestDir
}
