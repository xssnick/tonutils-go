package helpers

import "math/big"

var bigIntOne = big.NewInt(1)

// division with rounding floor (return quotient and remainder)
func DivFloor(x, y *big.Int) (*big.Int, *big.Int) {
	q := new(big.Int)
	r := new(big.Int)
	q.DivMod(x, y, r)

	if y.Sign() > 0 {
		if r.Sign() < 0 {
			q.Sub(q, bigIntOne)
			r.Add(r, y)
		}
	} else {
		if r.Sign() > 0 {
			q.Sub(q, bigIntOne)
			r.Add(r, y)
		}
	}

	return q, r
}

// division with rounding to nearest integer (return quotient)
func DivRound(x, y *big.Int) *big.Int {
	q := new(big.Int)
	r := new(big.Int)
	q.QuoRem(x, y, r)
	if r.Sign() == 0 {
		return q
	}

	twiceRem := new(big.Int).Abs(r)
	twiceRem.Lsh(twiceRem, 1)
	cmp := twiceRem.CmpAbs(y)

	if x.Sign() == y.Sign() {
		if cmp >= 0 {
			q.Add(q, bigIntOne)
		}
	} else if cmp > 0 {
		q.Sub(q, bigIntOne)
	}

	return q
}

// division with round ceiling (return quotient)
func DivCeil(x, y *big.Int) *big.Int {
	q := new(big.Int)
	r := new(big.Int)
	q.DivMod(x, y, r)

	if r.Sign() != 0 && x.Sign() > 0 && y.Sign() > 0 {
		q.Add(q, bigIntOne)
	}

	if r.Sign() != 0 && x.Sign() < 0 && y.Sign() > 0 {
		q.Add(q, bigIntOne)
	}

	return q
}
