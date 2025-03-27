package helpers

import "math/big"

// division with rounding to nearest integer
func DivRound(x, y *big.Int) *big.Int {
	xFloat := new(big.Float).SetInt(x)
	yFloat := new(big.Float).SetInt(y)

	quotient := new(big.Float).Quo(xFloat, yFloat)

	q, _ := quotient.Int(nil)
	r := new(big.Float).Sub(quotient, new(big.Float).SetInt(q))

	if quotient.Sign() >= 0 && r.Cmp(big.NewFloat(0.5)) >= 0 {
		q.Add(q, big.NewInt(1)) // ↑ if r >= 0.5
	} else if quotient.Sign() < 0 && r.Cmp(big.NewFloat(-0.5)) < 0 {
		q.Sub(q, big.NewInt(1)) // ↓ if r < -0.5
	}

	return q
}

// division with round ceiling
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
