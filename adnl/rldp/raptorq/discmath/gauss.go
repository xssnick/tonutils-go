package discmath

import "errors"

var ErrNotSolvable = errors.New("not solvable")

func GaussianElimination(a, d *MatrixGF256) (*MatrixGF256, error) {
	rows := a.RowsNum()

	rowPerm := make([]uint32, rows)
	for i := uint32(0); i < rows; i++ {
		rowPerm[i] = i
	}

	for row := uint32(0); row < a.ColsNum(); row++ {
		nonZero := row
		for nonZero < rows && a.Get(rowPerm[nonZero], row) == 0 {
			nonZero++
		}
		if nonZero == rows {
			return nil, ErrNotSolvable
		}

		if nonZero != row {
			rowPerm[nonZero], rowPerm[row] = rowPerm[row], rowPerm[nonZero]
		}

		mul := OctInverse(a.Get(rowPerm[row], row))

		a.RowMul(rowPerm[row], mul)
		d.RowMul(rowPerm[row], mul)

		for zeroRow := uint32(0); zeroRow < rows; zeroRow++ {
			if zeroRow == row {
				continue
			}
			x := a.Get(rowPerm[zeroRow], row)
			if x != 0 {
				a.RowAddMul(rowPerm[zeroRow], a.GetRow(rowPerm[row]), x)
				d.RowAddMul(rowPerm[zeroRow], d.GetRow(rowPerm[row]), x)
			}
		}
	}

	return d.ApplyPermutation(rowPerm), nil
}
