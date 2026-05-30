package mosaic

import (
	"math"

	"github.com/nit4y/mosaic/internal/config"
	"gocv.io/x/gocv"
)

// StabilizeHorizontalMotion removes rotational components from a 3×3 transform,
// preserving only horizontal translation.
func StabilizeHorizontalMotion(matrix gocv.Mat) gocv.Mat {
	// zero out rotational terms
	matrix.SetDoubleAt(0, 1, 0)
	matrix.SetDoubleAt(1, 0, 0)
	return matrix
}

// StabilizeNoScale zeroes out any scale in rows 0 and 1 of a 3×3 matrix,
// so after this call the diagonal entries are 1.0 (unit scale).
func StabilizeScale(mat gocv.Mat) gocv.Mat {
	// set X scale to 1
	mat.SetDoubleAt(0, 0, 1.0)
	// set Y scale to 1
	mat.SetDoubleAt(1, 1, 1.0)
	return mat
}

func StablizeTranslation(mat gocv.Mat) gocv.Mat {
	mat = StabilizeScale(StabilizeHorizontalMotion(mat))
	return DampYTranslation(mat, config.YTranslationDamping)
}

// DampYTranslation scales the ty component (element [1,2]) of a 3×3
// affine homography by `factor`. factor=1.0 is a no-op; factor=0.0
// removes vertical translation entirely. Mutates the input Mat in
// place and returns it (consistent with the other stabilize helpers).
func DampYTranslation(mat gocv.Mat, factor float64) gocv.Mat {
	if mat.Empty() || mat.Rows() < 2 || mat.Cols() < 3 {
		return mat
	}
	ty := mat.GetDoubleAt(1, 2)
	mat.SetDoubleAt(1, 2, ty*factor)
	return mat
}

// ToHomogeneous converts a 2×3 affine transformation Mat into a 3×3 homogeneous Mat.
// affine must be a Mat of size 2×3.
func ToHomogeneous(affine gocv.Mat) gocv.Mat {
	// Create a new 3×3 Mat with the same type as the affine input
	dtype := affine.Type()
	h := gocv.NewMatWithSize(3, 3, dtype)

	// Copy the 2×3 affine values into the top two rows of the 3×3
	for r := 0; r < 2; r++ {
		for c := 0; c < 3; c++ {
			val := affine.GetDoubleAt(r, c)
			h.SetDoubleAt(r, c, val)
		}
	}

	// Set the last row to [0, 0, 1]
	h.SetDoubleAt(2, 0, 0)
	h.SetDoubleAt(2, 1, 0)
	h.SetDoubleAt(2, 2, 1)

	return h
}

// CalcMotionDirection estimates the dominant motion direction from two
// corresponding slices of points. Returns "left", "right", "up", or "down".
func CalcMotionDirection(pts1, pts2 []gocv.Point2f) string {
	n := len(pts1)
	if n == 0 {
		return config.Left // default if no points
	}
	var sumDx, sumDy float64
	for i := 0; i < n; i++ {
		dx := float64(pts2[i].X - pts1[i].X)
		dy := float64(pts2[i].Y - pts1[i].Y)
		sumDx += dx
		sumDy += dy
	}
	// compute mean displacement
	dxMean := sumDx / float64(n)
	dyMean := sumDy / float64(n)

	// pick dominant axis
	if math.Abs(dxMean) > math.Abs(dyMean) {
		if dxMean > 0 {
			return config.Right
		}
		return config.Left
	} else {
		if dyMean > 0 {
			return config.Down
		}
		return config.Up
	}
}
