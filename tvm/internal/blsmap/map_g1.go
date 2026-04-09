package blsmap

import "github.com/cloudflare/circl/ecc/bls12381/ff"

type g1 struct{ x, y, z ff.Fp }
type isogG1Point struct{ x, y, z ff.Fp }

func MapToG1(data []byte) ([]byte, error) {
	if len(data) < ff.FpSize {
		return nil, errInputLength
	}

	var u ff.Fp
	u.SetBytes(data[:ff.FpSize])

	var q isogG1Point
	q.sswu(&u)

	var p g1
	p.evalIsogG1(&q)
	p.clearCofactor()
	return p.bytesCompressed(), nil
}

func (p *isogG1Point) sswu(u *ff.Fp) {
	tv1, tv2, tv3, tv4 := &ff.Fp{}, &ff.Fp{}, &ff.Fp{}, &ff.Fp{}
	xd, x1n, gxd, gx1 := &ff.Fp{}, &ff.Fp{}, &ff.Fp{}, &ff.Fp{}
	y, y1, x2n, y2, xn := &ff.Fp{}, &ff.Fp{}, &ff.Fp{}, &ff.Fp{}, &ff.Fp{}

	tv1.Sqr(u)
	tv3.Mul(&g1sswu.Z, tv1)
	tv2.Sqr(tv3)
	xd.Add(tv2, tv3)
	tv4.SetOne()
	x1n.Add(xd, tv4)
	x1n.Mul(x1n, &g1Isog11.b)
	xd.Mul(&g1Isog11.a, xd)
	xd.Neg()
	e1 := xd.IsZero()
	tv4.Mul(&g1sswu.Z, &g1Isog11.a)
	xd.CMov(xd, tv4, e1)
	tv2.Sqr(xd)
	gxd.Mul(tv2, xd)
	tv2.Mul(&g1Isog11.a, tv2)
	gx1.Sqr(x1n)
	gx1.Add(gx1, tv2)
	gx1.Mul(gx1, x1n)
	tv2.Mul(&g1Isog11.b, gxd)
	gx1.Add(gx1, tv2)
	tv4.Sqr(gxd)
	tv2.Mul(gx1, gxd)
	tv4.Mul(tv4, tv2)
	y1.ExpVarTime(tv4, g1sswu.c1[:])
	y1.Mul(y1, tv2)
	x2n.Mul(tv3, x1n)
	y2.Mul(y1, &g1sswu.c2)
	y2.Mul(y2, tv1)
	y2.Mul(y2, u)
	tv2.Sqr(y1)
	tv2.Mul(tv2, gxd)
	e2 := tv2.IsEqual(gx1)
	xn.CMov(x2n, x1n, e2)
	y.CMov(y2, y1, e2)
	e3 := u.Sgn0() ^ y.Sgn0()
	*tv1 = *y
	tv1.Neg()
	y.CMov(tv1, y, ^e3)
	p.x = *xn
	p.y.Mul(y, xd)
	p.z = *xd
}

func (g *g1) evalIsogG1(p *isogG1Point) {
	x, y, z := &p.x, &p.y, &p.z
	t, zi := &ff.Fp{}, &ff.Fp{}
	xNum, xDen, yNum, yDen := &ff.Fp{}, &ff.Fp{}, &ff.Fp{}, &ff.Fp{}

	ixn := len(g1Isog11.xNum) - 1
	ixd := len(g1Isog11.xDen) - 1
	iyn := len(g1Isog11.yNum) - 1
	iyd := len(g1Isog11.yDen) - 1

	*xNum = g1Isog11.xNum[ixn]
	*xDen = g1Isog11.xDen[ixd]
	*yNum = g1Isog11.yNum[iyn]
	*yDen = g1Isog11.yDen[iyd]
	*zi = *z

	for (ixn | ixd | iyn | iyd) != 0 {
		if ixn > 0 {
			ixn--
			t.Mul(zi, &g1Isog11.xNum[ixn])
			xNum.Mul(xNum, x)
			xNum.Add(xNum, t)
		}
		if ixd > 0 {
			ixd--
			t.Mul(zi, &g1Isog11.xDen[ixd])
			xDen.Mul(xDen, x)
			xDen.Add(xDen, t)
		}
		if iyn > 0 {
			iyn--
			t.Mul(zi, &g1Isog11.yNum[iyn])
			yNum.Mul(yNum, x)
			yNum.Add(yNum, t)
		}
		if iyd > 0 {
			iyd--
			t.Mul(zi, &g1Isog11.yDen[iyd])
			yDen.Mul(yDen, x)
			yDen.Add(yDen, t)
		}
		zi.Mul(zi, z)
	}

	g.x.Mul(xNum, yDen)
	g.y.Mul(yNum, xDen)
	g.y.Mul(&g.y, y)
	g.z.Mul(xDen, yDen)
	g.z.Mul(&g.z, z)
}

func (g *g1) clearCofactor() { g.scalarMultShort(bls12381.oneMinusZ[:], g) }

func (g *g1) scalarMultShort(k []byte, p *g1) {
	var q g1
	q.setIdentity()
	N := 8 * len(k)
	for i := 0; i < N; i++ {
		q.double()
		if (k[i/8]>>(7-uint(i%8)))&1 != 0 {
			q.add(&q, p)
		}
	}
	*g = q
}

func (g *g1) setIdentity() { g.x = ff.Fp{}; g.y.SetOne(); g.z = ff.Fp{} }

func (g *g1) double() {
	var r g1
	X, Y, Z := &g.x, &g.y, &g.z
	X3, Y3, Z3 := &r.x, &r.y, &r.z
	var f0, f1, f2 ff.Fp
	t0, t1, t2 := &f0, &f1, &f2
	_3B := &g1Params._3b
	t0.Sqr(Y)
	Z3.Add(t0, t0)
	Z3.Add(Z3, Z3)
	Z3.Add(Z3, Z3)
	t1.Mul(Y, Z)
	t2.Sqr(Z)
	t2.Mul(_3B, t2)
	X3.Mul(t2, Z3)
	Y3.Add(t0, t2)
	Z3.Mul(t1, Z3)
	t1.Add(t2, t2)
	t2.Add(t1, t2)
	t0.Sub(t0, t2)
	Y3.Mul(t0, Y3)
	Y3.Add(X3, Y3)
	t1.Mul(X, Y)
	X3.Mul(t0, t1)
	X3.Add(X3, X3)
	*g = r
}

func (g *g1) add(p, q *g1) {
	var r g1
	X1, Y1, Z1 := &p.x, &p.y, &p.z
	X2, Y2, Z2 := &q.x, &q.y, &q.z
	X3, Y3, Z3 := &r.x, &r.y, &r.z
	_3B := &g1Params._3b
	var f0, f1, f2, f3, f4 ff.Fp
	t0, t1, t2, t3, t4 := &f0, &f1, &f2, &f3, &f4
	t0.Mul(X1, X2)
	t1.Mul(Y1, Y2)
	t2.Mul(Z1, Z2)
	t3.Add(X1, Y1)
	t4.Add(X2, Y2)
	t3.Mul(t3, t4)
	t4.Add(t0, t1)
	t3.Sub(t3, t4)
	t4.Add(Y1, Z1)
	X3.Add(Y2, Z2)
	t4.Mul(t4, X3)
	X3.Add(t1, t2)
	t4.Sub(t4, X3)
	X3.Add(X1, Z1)
	Y3.Add(X2, Z2)
	X3.Mul(X3, Y3)
	Y3.Add(t0, t2)
	Y3.Sub(X3, Y3)
	X3.Add(t0, t0)
	t0.Add(X3, t0)
	t2.Mul(_3B, t2)
	Z3.Add(t1, t2)
	t1.Sub(t1, t2)
	Y3.Mul(_3B, Y3)
	X3.Mul(t4, Y3)
	t2.Mul(t3, t1)
	X3.Sub(t2, X3)
	Y3.Mul(Y3, t0)
	t1.Mul(t1, Z3)
	Y3.Add(t1, Y3)
	t0.Mul(t0, t3)
	Z3.Mul(Z3, t4)
	Z3.Add(Z3, t0)
	*g = r
}

func (g *g1) toAffine() {
	if g.z.IsZero() != 1 {
		var invZ ff.Fp
		invZ.Inv(&g.z)
		g.x.Mul(&g.x, &invZ)
		g.y.Mul(&g.y, &invZ)
		g.z.SetOne()
	}
}

func (g *g1) bytesCompressed() []byte {
	g.toAffine()

	var isInfinity, isBigYCoord byte
	if g.z.IsZero() == 1 {
		isInfinity = 1
	}
	if isInfinity == 0 {
		isBigYCoord = byte(g.y.IsNegative())
	}

	data, _ := g.x.MarshalBinary()
	if isInfinity == 1 {
		for i := range data {
			data[i] = 0
		}
	}
	data[0] = data[0]&0x1F | headerEncoding(1, isInfinity, isBigYCoord)
	return data
}
