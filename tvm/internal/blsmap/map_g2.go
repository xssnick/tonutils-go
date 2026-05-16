package blsmap

import "github.com/cloudflare/circl/ecc/bls12381/ff"

type g2 struct{ x, y, z ff.Fp2 }
type isogG2Point struct{ x, y, z ff.Fp2 }

func MapToG2(data []byte) ([]byte, error) {
	if len(data) < ff.Fp2Size {
		return nil, errInputLength
	}

	var u ff.Fp2
	u[0].SetBytes(data[:ff.FpSize])
	u[1].SetBytes(data[ff.FpSize : 2*ff.FpSize])

	var q isogG2Point
	q.sswu(&u)

	var p g2
	p.evalIsogG2(&q)
	p.clearCofactor()
	return p.bytesCompressed(), nil
}

func (p *isogG2Point) sswu(u *ff.Fp2) {
	tv1, tv2, tv3, tv4 := &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}
	tv5, xd, x1n, gxd := &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}
	gx1, y, xn, gx2 := &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}

	tv1.Sqr(u)
	tv3.Mul(&g2sswu.Z, tv1)
	tv5.Sqr(tv3)
	xd.Add(tv5, tv3)
	tv2.SetOne()
	x1n.Add(xd, tv2)
	x1n.Mul(x1n, &g2Isog3.b)
	xd.Mul(&g2Isog3.a, xd)
	xd.Neg()
	e1 := xd.IsZero()
	tv2.Mul(&g2sswu.Z, &g2Isog3.a)
	xd.CMov(xd, tv2, e1)
	tv2.Sqr(xd)
	gxd.Mul(tv2, xd)
	tv2.Mul(&g2Isog3.a, tv2)
	gx1.Sqr(x1n)
	gx1.Add(gx1, tv2)
	gx1.Mul(gx1, x1n)
	tv2.Mul(&g2Isog3.b, gxd)
	gx1.Add(gx1, tv2)
	tv4.Sqr(gxd)
	tv2.Mul(tv4, gxd)
	tv4.Sqr(tv4)
	tv2.Mul(tv2, tv4)
	tv2.Mul(tv2, gx1)
	tv4.Sqr(tv4)
	tv4.Mul(tv2, tv4)
	y.ExpVarTime(tv4, g2sswu.c1[:])
	y.Mul(y, tv2)
	tv4.Mul(y, &g2sswu.c2)
	tv2.Sqr(tv4)
	tv2.Mul(tv2, gxd)
	e2 := tv2.IsEqual(gx1)
	y.CMov(y, tv4, e2)
	tv4.Mul(y, &g2sswu.c3)
	tv2.Sqr(tv4)
	tv2.Mul(tv2, gxd)
	e3 := tv2.IsEqual(gx1)
	y.CMov(y, tv4, e3)
	tv4.Mul(tv4, &g2sswu.c2)
	tv2.Sqr(tv4)
	tv2.Mul(tv2, gxd)
	e4 := tv2.IsEqual(gx1)
	y.CMov(y, tv4, e4)
	gx2.Mul(gx1, tv5)
	gx2.Mul(gx2, tv3)
	tv5.Mul(y, tv1)
	tv5.Mul(tv5, u)
	tv1.Mul(tv5, &g2sswu.c4)
	tv4.Mul(tv1, &g2sswu.c2)
	tv2.Sqr(tv4)
	tv2.Mul(tv2, gxd)
	e5 := tv2.IsEqual(gx2)
	tv1.CMov(tv1, tv4, e5)
	tv4.Mul(tv5, &g2sswu.c5)
	tv2.Sqr(tv4)
	tv2.Mul(tv2, gxd)
	e6 := tv2.IsEqual(gx2)
	tv1.CMov(tv1, tv4, e6)
	tv4.Mul(tv4, &g2sswu.c2)
	tv2.Sqr(tv4)
	tv2.Mul(tv2, gxd)
	e7 := tv2.IsEqual(gx2)
	tv1.CMov(tv1, tv4, e7)
	tv2.Sqr(y)
	tv2.Mul(tv2, gxd)
	e8 := tv2.IsEqual(gx1)
	y.CMov(tv1, y, e8)
	tv2.Mul(tv3, x1n)
	xn.CMov(tv2, x1n, e8)
	e9 := 1 ^ u.Sgn0() ^ y.Sgn0()
	*tv1 = *y
	tv1.Neg()
	y.CMov(tv1, y, e9)
	p.x = *xn
	p.y.Mul(y, xd)
	p.z = *xd
}

func (g *g2) evalIsogG2(p *isogG2Point) {
	x, y, z := &p.x, &p.y, &p.z
	t, zi := &ff.Fp2{}, &ff.Fp2{}
	xNum, xDen, yNum, yDen := &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}, &ff.Fp2{}

	ixn := len(g2Isog3.xNum) - 1
	ixd := len(g2Isog3.xDen) - 1
	iyn := len(g2Isog3.yNum) - 1
	iyd := len(g2Isog3.yDen) - 1

	*xNum = g2Isog3.xNum[ixn]
	*xDen = g2Isog3.xDen[ixd]
	*yNum = g2Isog3.yNum[iyn]
	*yDen = g2Isog3.yDen[iyd]
	*zi = *z

	for (ixn | ixd | iyn | iyd) != 0 {
		if ixn > 0 {
			ixn--
			t.Mul(zi, &g2Isog3.xNum[ixn])
			xNum.Mul(xNum, x)
			xNum.Add(xNum, t)
		}
		if ixd > 0 {
			ixd--
			t.Mul(zi, &g2Isog3.xDen[ixd])
			xDen.Mul(xDen, x)
			xDen.Add(xDen, t)
		}
		if iyn > 0 {
			iyn--
			t.Mul(zi, &g2Isog3.yNum[iyn])
			yNum.Mul(yNum, x)
			yNum.Add(yNum, t)
		}
		if iyd > 0 {
			iyd--
			t.Mul(zi, &g2Isog3.yDen[iyd])
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

func (g *g2) clearCofactor() {
	x := bls12381.minusZ[:]
	xP, psiP := &g2{}, &g2{}
	twoP := *g

	twoP.double()
	twoP.psi()
	twoP.psi()
	xP.scalarMultShort(x, g)
	xP.add(xP, g)
	*psiP = *xP
	psiP.psi()
	xP.scalarMultShort(x, xP)
	g.add(g, psiP)
	g.neg()
	g.add(g, xP)
	g.add(g, &twoP)
}

func (g *g2) psi() {
	g.x.Frob(&g.x)
	g.y.Frob(&g.y)
	g.z.Frob(&g.z)
	g.x.Mul(&g2Psi.alpha, &g.x)
	g.y.Mul(&g2Psi.beta, &g.y)
}

func (g *g2) scalarMultShort(k []byte, p *g2) {
	var q g2
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

func (g *g2) setIdentity() { g.x = ff.Fp2{}; g.y.SetOne(); g.z = ff.Fp2{} }

func (g *g2) neg() { g.y.Neg() }

func (g *g2) double() { doubleG2(g) }

func (g *g2) add(p, q *g2) { addG2(g, p, q) }

func doubleG2(p *g2) {
	var r g2
	X, Y, Z := &p.x, &p.y, &p.z
	X3, Y3, Z3 := &r.x, &r.y, &r.z
	_3B := &g2Params._3b
	var A, B, C, D, E, F, G, T ff.Fp2
	B.Sqr(Y)
	C.Sqr(Z)
	D.Mul(_3B, &C)
	F.Add(Y, Z)
	F.Sqr(&F)
	F.Sub(&F, &B)
	F.Sub(&F, &C)
	A.Sqr(X)
	E.Add(X, Y)
	E.Sqr(&E)
	E.Sub(&E, &A)
	E.Sub(&E, &B)
	T.Add(&D, &D)
	G.Add(&T, &D)
	X3.Sub(&B, &G)
	X3.Mul(X3, &E)
	T.Sqr(&T)
	Y3.Add(&B, &G)
	Y3.Sqr(Y3)
	Y3.Sub(Y3, &T)
	Y3.Sub(Y3, &T)
	Y3.Sub(Y3, &T)
	Z3.Mul(&B, &F)
	Z3.Add(Z3, Z3)
	Z3.Add(Z3, Z3)
	*p = r
}

func addG2(dst, p, q *g2) {
	var r g2
	X1, Y1, Z1 := &p.x, &p.y, &p.z
	X2, Y2, Z2 := &q.x, &q.y, &q.z
	X3, Y3, Z3 := &r.x, &r.y, &r.z
	_3B := &g2Params._3b

	var X1X2, Y1Y2, Z1Z2, _3bZ1Z2 ff.Fp2
	var A, B, C, D, E, F, G ff.Fp2
	t0, t1 := &ff.Fp2{}, &ff.Fp2{}

	X1X2.Mul(X1, X2)
	Y1Y2.Mul(Y1, Y2)
	Z1Z2.Mul(Z1, Z2)
	_3bZ1Z2.Mul(&Z1Z2, _3B)

	A.Add(&X1X2, &X1X2)
	A.Add(&A, &X1X2)
	B.Add(&Y1Y2, &_3bZ1Z2)
	C.Sub(&Y1Y2, &_3bZ1Z2)

	t0.Add(X1, Y1)
	D.Add(X2, Y2)
	D.Mul(&D, t0)
	D.Sub(&D, &X1X2)
	D.Sub(&D, &Y1Y2)

	t0.Add(Y1, Z1)
	t1.Add(Y2, Z2)
	E.Mul(t0, t1)
	E.Sub(&E, &Y1Y2)
	E.Sub(&E, &Z1Z2)

	t0.Add(X1, Z1)
	t1.Add(X2, Z2)
	F.Mul(t0, t1)
	F.Sub(&F, &X1X2)
	F.Sub(&F, &Z1Z2)

	G.Mul(&F, _3B)

	t0.Mul(&E, &G)
	X3.Mul(&D, &C)
	X3.Sub(X3, t0)

	t0.Mul(&A, &G)
	Y3.Mul(&B, &C)
	Y3.Add(Y3, t0)

	t0.Mul(&A, &D)
	Z3.Mul(&E, &B)
	Z3.Add(Z3, t0)

	*dst = r
}

func (g *g2) toAffine() {
	if g.z.IsZero() != 1 {
		var invZ ff.Fp2
		invZ.Inv(&g.z)
		g.x.Mul(&g.x, &invZ)
		g.y.Mul(&g.y, &invZ)
		g.z.SetOne()
	}
}

func (g *g2) bytesCompressed() []byte {
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
