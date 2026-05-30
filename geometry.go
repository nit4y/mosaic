package mosaic

import (
	"math"

	"gocv.io/x/gocv"
)

// stabilizeHorizontalMotion removes rotational components from a 3×3 transform,
// preserving only horizontal translation.
func stabilizeHorizontalMotion(matrix gocv.Mat) gocv.Mat {
	// zero out rotational terms
	matrix.SetDoubleAt(0, 1, 0)
	matrix.SetDoubleAt(1, 0, 0)
	return matrix
}

// stabilizeScale forces unit scale on a 3×3 matrix by setting both diagonal
// entries [0,0] and [1,1] to 1.0.
func stabilizeScale(mat gocv.Mat) gocv.Mat {
	// set X scale to 1
	mat.SetDoubleAt(0, 0, 1.0)
	// set Y scale to 1
	mat.SetDoubleAt(1, 1, 1.0)
	return mat
}

// stabilizeTranslation reduces a homography to horizontal translation: it
// zeroes rotation, forces unit scale, and damps the vertical translation by
// yDamping (1.0 = keep ty as-is, 0.0 = remove it).
func stabilizeTranslation(mat gocv.Mat, yDamping float64) gocv.Mat {
	mat = stabilizeScale(stabilizeHorizontalMotion(mat))
	return dampYTranslation(mat, yDamping)
}

// dampYTranslation scales the ty component (element [1,2]) of a 3×3
// affine homography by `factor`. factor=1.0 is a no-op; factor=0.0
// removes vertical translation entirely. Mutates the input Mat in
// place and returns it (consistent with the other stabilize helpers).
func dampYTranslation(mat gocv.Mat, factor float64) gocv.Mat {
	if mat.Empty() || mat.Rows() < 2 || mat.Cols() < 3 {
		return mat
	}
	ty := mat.GetDoubleAt(1, 2)
	mat.SetDoubleAt(1, 2, ty*factor)
	return mat
}

// toHomogeneous converts a 2×3 affine transformation Mat into a 3×3 homogeneous Mat.
// affine must be a Mat of size 2×3.
func toHomogeneous(affine gocv.Mat) gocv.Mat {
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

// calcMotionDirection estimates the dominant motion direction from two
// corresponding slices of points.
func calcMotionDirection(pts1, pts2 []gocv.Point2f) Direction {
	n := len(pts1)
	if n == 0 {
		return Left // default if no points
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
			return Right
		}
		return Left
	} else {
		if dyMean > 0 {
			return Down
		}
		return Up
	}
}
