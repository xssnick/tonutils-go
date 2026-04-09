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

func mod(v, m *big.Int) *big.Int {
	out := new(big.Int).Mod(new(big.Int).Set(v), m)
	if out.Sign() < 0 {
		out.Add(out, m)
	}
	return out
}

func scalarFromBytes(b []byte) *big.Int {
	return mod(new(big.Int).SetBytes(b), curveN)
}

func inverse(v, m *big.Int) *big.Int {
	return new(big.Int).ModInverse(v, m)
}

func isOnCurve(p point) bool {
	if p.inf {
		return true
	}
	left := mod(new(big.Int).Mul(p.y, p.y), curveP)
	right := mod(new(big.Int).Add(new(big.Int).Mul(new(big.Int).Mul(p.x, p.x), p.x), seven), curveP)
	return left.Cmp(right) == 0
}

func negate(p point) point {
	if p.inf {
		return p
	}
	return point{x: new(big.Int).Set(p.x), y: mod(new(big.Int).Neg(p.y), curveP)}
}

func add(p, q point) point {
	if p.inf {
		return copyPoint(q)
	}
	if q.inf {
		return copyPoint(p)
	}

	if p.x.Cmp(q.x) == 0 {
		ySum := mod(new(big.Int).Add(p.y, q.y), curveP)
		if ySum.Sign() == 0 {
			return point{inf: true}
		}
		return double(p)
	}

	num := mod(new(big.Int).Sub(q.y, p.y), curveP)
	den := mod(new(big.Int).Sub(q.x, p.x), curveP)
	denInv := inverse(den, curveP)
	if denInv == nil {
		return point{inf: true}
	}
	lambda := mod(new(big.Int).Mul(num, denInv), curveP)

	xr := mod(new(big.Int).Sub(new(big.Int).Sub(new(big.Int).Mul(lambda, lambda), p.x), q.x), curveP)
	yr := mod(new(big.Int).Sub(new(big.Int).Mul(lambda, new(big.Int).Sub(p.x, xr)), p.y), curveP)
	return point{x: xr, y: yr}
}

func double(p point) point {
	if p.inf || p.y.Sign() == 0 {
		return point{inf: true}
	}

	num := mod(new(big.Int).Mul(three, new(big.Int).Mul(p.x, p.x)), curveP)
	den := mod(new(big.Int).Mul(big.NewInt(2), p.y), curveP)
	denInv := inverse(den, curveP)
	if denInv == nil {
		return point{inf: true}
	}
	lambda := mod(new(big.Int).Mul(num, denInv), curveP)

	xr := mod(new(big.Int).Sub(new(big.Int).Mul(lambda, lambda), new(big.Int).Mul(big.NewInt(2), p.x)), curveP)
	yr := mod(new(big.Int).Sub(new(big.Int).Mul(lambda, new(big.Int).Sub(p.x, xr)), p.y), curveP)
	return point{x: xr, y: yr}
}

func scalarMult(base point, k *big.Int) point {
	if base.inf || k.Sign() == 0 {
		return point{inf: true}
	}

	res := point{inf: true}
	addend := copyPoint(base)
	for i := 0; i < k.BitLen(); i++ {
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
	if mod(new(big.Int).Mul(root, root), curveP).Cmp(mod(v, curveP)) != 0 {
		return nil
	}
	return root
}

func serializeUncompressed(p point) []byte {
	if p.inf {
		return nil
	}
	out := make([]byte, 65)
	out[0] = 0x04
	xb := p.x.Bytes()
	yb := p.y.Bytes()
	copy(out[33-len(xb):33], xb)
	copy(out[65-len(yb):65], yb)
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

	alpha := mod(new(big.Int).Add(new(big.Int).Mul(new(big.Int).Mul(x, x), x), seven), curveP)
	y := modSqrt(alpha)
	if y == nil {
		return point{}, false
	}
	if y.Bit(0) != 0 {
		y = mod(new(big.Int).Neg(y), curveP)
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

	alpha := mod(new(big.Int).Add(new(big.Int).Mul(new(big.Int).Mul(x, x), x), seven), curveP)
	y := modSqrt(alpha)
	if y == nil {
		return nil, false
	}
	if y.Bit(0) != uint(v&1) {
		y = mod(new(big.Int).Neg(y), curveP)
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
	u1 := mod(new(big.Int).Mul(e, w), curveN)
	u2 := mod(new(big.Int).Mul(r, w), curveN)
	sum := add(scalarMult(curveG, u1), scalarMult(pub, u2))
	if sum.inf {
		return false
	}
	x := mod(sum.x, curveN)
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
	r := mod(rPoint.x, curveN)
	if r.Sign() == 0 {
		return 0, nil, nil, nil, false
	}

	kInv := inverse(k, curveN)
	if kInv == nil {
		return 0, nil, nil, nil, false
	}
	e := scalarFromBytes(hash)
	s := mod(new(big.Int).Mul(kInv, new(big.Int).Add(e, new(big.Int).Mul(r, d))), curveN)
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
