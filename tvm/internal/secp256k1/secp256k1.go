package secp256k1

import "math/big"

type point struct {
	x   *big.Int
	y   *big.Int
	inf bool
}

var (
	curveP = mustBigHex("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F")
	curveN = mustBigHex("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141")
	curveG = point{
		x: mustBigHex("79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798"),
		y: mustBigHex("483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8"),
	}
	curveSqrtExp = new(big.Int).Rsh(new(big.Int).Add(curveP, big.NewInt(1)), 2)
	three        = big.NewInt(3)
	seven        = big.NewInt(7)
)

func CurveOrderBytes() []byte {
	return curveN.FillBytes(make([]byte, 32))
}

func mustBigHex(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("invalid secp256k1 constant")
	}
	return v
}

func scalarFromBytes(b []byte) *big.Int {
	out := new(big.Int).SetBytes(b)
	out.Mod(out, curveN)
	return out
}

func inverse(v, m *big.Int) *big.Int {
	return new(big.Int).ModInverse(v, m)
}

func isOnCurve(p point) bool {
	if p.inf {
		return true
	}

	left := new(big.Int).Mul(p.y, p.y)
	left.Mod(left, curveP)
	return left.Cmp(curveY2(p.x)) == 0
}

func negate(p point) point {
	if p.inf {
		return p
	}

	y := new(big.Int).Neg(p.y)
	y.Mod(y, curveP)
	return point{x: new(big.Int).Set(p.x), y: y}
}

func add(p, q point) point {
	if p.inf {
		return copyPoint(q)
	}
	if q.inf {
		return copyPoint(p)
	}

	if p.x.Cmp(q.x) == 0 {
		ySum := new(big.Int).Add(p.y, q.y)
		ySum.Mod(ySum, curveP)
		if ySum.Sign() == 0 {
			return point{inf: true}
		}
		return double(p)
	}

	num := new(big.Int).Sub(q.y, p.y)
	num.Mod(num, curveP)
	den := new(big.Int).Sub(q.x, p.x)
	den.Mod(den, curveP)
	denInv := inverse(den, curveP)
	if denInv == nil {
		return point{inf: true}
	}

	lambda := new(big.Int).Mul(num, denInv)
	lambda.Mod(lambda, curveP)

	xr := new(big.Int).Mul(lambda, lambda)
	xr.Sub(xr, p.x)
	xr.Sub(xr, q.x)
	xr.Mod(xr, curveP)

	yr := new(big.Int).Sub(p.x, xr)
	yr.Mul(lambda, yr)
	yr.Sub(yr, p.y)
	yr.Mod(yr, curveP)
	return point{x: xr, y: yr}
}

func double(p point) point {
	if p.inf || p.y.Sign() == 0 {
		return point{inf: true}
	}

	num := new(big.Int).Mul(p.x, p.x)
	num.Mul(num, three)
	num.Mod(num, curveP)
	den := new(big.Int).Lsh(p.y, 1)
	den.Mod(den, curveP)
	denInv := inverse(den, curveP)
	if denInv == nil {
		return point{inf: true}
	}

	lambda := new(big.Int).Mul(num, denInv)
	lambda.Mod(lambda, curveP)

	xr := new(big.Int).Mul(lambda, lambda)
	twoX := new(big.Int).Lsh(p.x, 1)
	xr.Sub(xr, twoX)
	xr.Mod(xr, curveP)

	yr := new(big.Int).Sub(p.x, xr)
	yr.Mul(lambda, yr)
	yr.Sub(yr, p.y)
	yr.Mod(yr, curveP)
	return point{x: xr, y: yr}
}

func scalarMult(base point, k *big.Int) point {
	if base.inf || k.Sign() == 0 {
		return point{inf: true}
	}

	res := point{inf: true}
	addend := copyPoint(base)
	bitLen := k.BitLen()
	for i := 0; i < bitLen; i++ {
		if k.Bit(i) != 0 {
			res = add(res, addend)
		}
		addend = double(addend)
	}
	return res
}

func copyPoint(p point) point {
	if p.inf {
		return point{inf: true}
	}
	return point{x: new(big.Int).Set(p.x), y: new(big.Int).Set(p.y)}
}

func modSqrt(v *big.Int) *big.Int {
	root := new(big.Int).Exp(v, curveSqrtExp, curveP)
	check := new(big.Int).Mul(root, root)
	check.Mod(check, curveP)
	vMod := new(big.Int).Mod(v, curveP)
	if check.Cmp(vMod) != 0 {
		return nil
	}
	return root
}

func curveY2(x *big.Int) *big.Int {
	out := new(big.Int).Mul(x, x)
	out.Mul(out, x)
	out.Add(out, seven)
	out.Mod(out, curveP)
	return out
}

func serializeUncompressed(p point) []byte {
	if p.inf {
		return nil
	}
	out := make([]byte, 65)
	out[0] = 0x04
	p.x.FillBytes(out[1:33])
	p.y.FillBytes(out[33:65])
	return out
}

func parseXOnlyEven(xonlyBytes []byte) (point, bool) {
	if len(xonlyBytes) != 32 {
		return point{}, false
	}

	x := new(big.Int).SetBytes(xonlyBytes)
	if x.Cmp(curveP) >= 0 {
		return point{}, false
	}

	alpha := curveY2(x)
	y := modSqrt(alpha)
	if y == nil {
		return point{}, false
	}
	if y.Bit(0) != 0 {
		y.Neg(y)
		y.Mod(y, curveP)
	}

	p := point{x: x, y: y}
	if !isOnCurve(p) {
		return point{}, false
	}
	return p, true
}

func XOnlyPubkeyTweakAddUncompressed(xonlyPubkeyBytes, tweakBytes []byte) ([]byte, bool) {
	p, ok := parseXOnlyEven(xonlyPubkeyBytes)
	if !ok {
		return nil, false
	}
	if len(tweakBytes) != 32 {
		return nil, false
	}

	tweak := new(big.Int).SetBytes(tweakBytes)
	if tweak.Cmp(curveN) >= 0 {
		return nil, false
	}

	out := add(p, scalarMult(curveG, tweak))
	if out.inf || !isOnCurve(out) {
		return nil, false
	}
	return serializeUncompressed(out), true
}

func RecoverUncompressed(hash []byte, r, s *big.Int, v byte) ([]byte, bool) {
	if v > 3 {
		return nil, false
	}
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(curveN) >= 0 || s.Cmp(curveN) >= 0 {
		return nil, false
	}

	x := new(big.Int).Set(r)
	if v>>1 != 0 {
		x.Add(x, curveN)
	}
	if x.Cmp(curveP) >= 0 {
		return nil, false
	}

	alpha := curveY2(x)
	y := modSqrt(alpha)
	if y == nil {
		return nil, false
	}
	if y.Bit(0) != uint(v&1) {
		y.Neg(y)
		y.Mod(y, curveP)
	}

	rPoint := point{x: x, y: y}
	if !isOnCurve(rPoint) {
		return nil, false
	}

	e := scalarFromBytes(hash)
	rInv := inverse(r, curveN)
	if rInv == nil {
		return nil, false
	}

	sr := scalarMult(rPoint, s)
	eg := scalarMult(curveG, e)
	pub := scalarMult(add(sr, negate(eg)), rInv)
	if pub.inf || !isOnCurve(pub) {
		return nil, false
	}
	if !verify(hash, r, s, pub) {
		return nil, false
	}
	return serializeUncompressed(pub), true
}

func verify(hash []byte, r, s *big.Int, pub point) bool {
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(curveN) >= 0 || s.Cmp(curveN) >= 0 || pub.inf || !isOnCurve(pub) {
		return false
	}

	e := scalarFromBytes(hash)
	w := inverse(s, curveN)
	if w == nil {
		return false
	}
	u1 := new(big.Int).Mul(e, w)
	u1.Mod(u1, curveN)
	u2 := new(big.Int).Mul(r, w)
	u2.Mod(u2, curveN)
	sum := add(scalarMult(curveG, u1), scalarMult(pub, u2))
	if sum.inf {
		return false
	}
	x := new(big.Int).Mod(sum.x, curveN)
	return x.Cmp(r) == 0
}

func SignRecoverable(privBytes, nonceBytes, hash []byte) (byte, *big.Int, *big.Int, []byte, bool) {
	d := scalarFromBytes(privBytes)
	k := scalarFromBytes(nonceBytes)
	if d.Sign() == 0 || k.Sign() == 0 {
		return 0, nil, nil, nil, false
	}

	rPoint := scalarMult(curveG, k)
	if rPoint.inf {
		return 0, nil, nil, nil, false
	}
	r := new(big.Int).Mod(rPoint.x, curveN)
	if r.Sign() == 0 {
		return 0, nil, nil, nil, false
	}

	kInv := inverse(k, curveN)
	if kInv == nil {
		return 0, nil, nil, nil, false
	}
	e := scalarFromBytes(hash)
	s := new(big.Int).Mul(r, d)
	s.Add(s, e)
	s.Mul(s, kInv)
	s.Mod(s, curveN)
	if s.Sign() == 0 {
		return 0, nil, nil, nil, false
	}

	var v byte
	if rPoint.y.Bit(0) != 0 {
		v |= 1
	}
	if rPoint.x.Cmp(curveN) >= 0 {
		v |= 2
	}

	pub := scalarMult(curveG, d)
	return v, r, s, serializeUncompressed(pub), true
}
