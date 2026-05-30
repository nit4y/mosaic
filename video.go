package mosaic

import (
	"fmt"
	"path/filepath"

	"gocv.io/x/gocv"
)

// extractFrames extracts all frames from a video file and returns them as a slice of Mats.
func extractFrames(videoPath string, lg *Logger) ([]gocv.Mat, error) {
	log := lg.With("video", filepath.Base(videoPath))
	log.Info("Opening video file")

	video, err := gocv.VideoCaptureFile(videoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open video: %w", err)
	}
	defer video.Close()

	if !video.IsOpened() {
		// VideoCaptureFile can return nil error while still failing to
		// open the underlying backend (missing codec, corrupt header,
		// unsupported container) — IsOpened catches that case.
		return nil, fmt.Errorf("video capture failed to open: %s", videoPath)
	}

	fps := video.Get(gocv.VideoCaptureFPS)
	frameCount := int(video.Get(gocv.VideoCaptureFrameCount))
	log.Info("Video properties", "fps", fps, "total_frames", frameCount)

	// Cap initial capacity to avoid huge allocations if the container
	// reports a bogus frame count.
	capacity := frameCount
	if capacity < 0 || capacity > 100000 {
		capacity = 0
	}
	frames := make([]gocv.Mat, 0, capacity)
	frame := gocv.NewMat()
	defer frame.Close()

	for {
		if ok := video.Read(&frame); !ok {
			break
		}
		if frame.Empty() {
			continue
		}
		frames = append(frames, frame.Clone())
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames could be decoded from %s", videoPath)
	}

	return frames, nil
}

// generateVideoFromFrames converts a slice of Mats into an MP4 video file.
func generateVideoFromFrames(images []resJob, outputPath string, fps int, lg *Logger) error {
	log := lg.With("operation", "create_video")
	if len(images) == 0 {
		return fmt.Errorf("no frames to write to %s", outputPath)
	}

	// Every frame in a sequence shares one size — the canvas size for
	// static, the common padded size for dynamic — so derive the writer
	// dimensions from the first frame instead of plumbing them through.
	width, height := images[0].mat.Cols(), images[0].mat.Rows()
	log.Info("Creating video", "output", outputPath, "fps", fps,
		"frame_count", len(images), "width", width, "height", height)

	writer, err := gocv.VideoWriterFile(outputPath, "mp4v", float64(fps), width, height, true)
	if err != nil {
		return fmt.Errorf("failed to create video writer: %w", err)
	}
	defer writer.Close()

	for _, job := range images {
		if err := writer.Write(job.mat); err != nil {
			return fmt.Errorf("failed to write frame %d: %w", job.idx, err)
		}
	}

	log.Info("Video creation completed")
	return nil
}
