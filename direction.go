package mosaic

// Direction is the dominant direction of camera motion across a clip. It is
// detected during alignment and used to orient frames before stitching.
type Direction string

const (
	Left  Direction = "left"
	Right Direction = "right"
	Up    Direction = "up"
	Down  Direction = "down"
)
