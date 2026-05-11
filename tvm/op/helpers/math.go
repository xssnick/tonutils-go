package helpers

import "math/big"

// division with rounding floor (return quotient and remainder)
func DivFloor(x, y *big.Int) (*big.Int, *big.Int) {
	q := new(big.Int).Div(x, y)
	r := new(big.Int).Mod(x, y)

	if y.Sign() > 0 {
		if r.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
			r.Add(r, y)
		}
	} else {
		if r.Sign() > 0 {
			q.Sub(q, big.NewInt(1))
			r.Add(r, y)
		}
	}

	return q, r
}

// division with rounding to nearest integer (return quotient)
func DivRound(x, y *big.Int) *big.Int {
	q := new(big.Int).Quo(x, y)
	r := new(big.Int).Rem(x, y)
	if r.Sign() == 0 {
		return q
	}

	twiceRem := new(big.Int).Abs(r)
	twiceRem.Lsh(twiceRem, 1)
	absY := new(big.Int).Abs(y)
	cmp := twiceRem.Cmp(absY)

	if x.Sign() == y.Sign() {
		if cmp >= 0 {
			q.Add(q, big.NewInt(1))
		}
	} else if cmp > 0 {
		q.Sub(q, big.NewInt(1))
	}

	return q
}

// division with round ceiling (return quotient)
func DivCeil(x, y *big.Int) *big.Int {
	q := new(big.Int).Div(x, y)
	r := new(big.Int).Mod(x, y)

	if r.Sign() != 0 && x.Sign() > 0 && y.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}

	if r.Sign() != 0 && x.Sign() < 0 && y.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}

	return q
}
