package bls12381

// This file is one of the local additions to the vendored circl bls12381 package
// (see PROVENANCE.md). It provides an on-curve-only decode path that mirrors the
// upstream SetBytes in g1.go / g2.go exactly, but relaxes the final validation
// from IsOnG1/IsOnG2 (isValidProjective && isOnCurve && isRTorsion) to just
// (isValidProjective && isOnCurve): the r-torsion subgroup check is dropped.
//
// This matches the reference TON node's low-level BLS opcodes, which decode
// points with raw blst_p1_deserialize/blst_p2_deserialize (on-curve only) and
// deliberately do NOT enforce subgroup membership. Keep the decode bodies below
// byte-for-byte in sync with SetBytes when re-vendoring circl.

import (
	"crypto/subtle"

	"github.com/cloudflare/circl/ecc/bls12381/ff"
)

// SetBytesOnCurve sets g to the value in bytes, returning a non-nil error only
// if the encoding is malformed or the point is not on the G1 curve. Unlike
// SetBytes it does NOT reject points outside the r-torsion subgroup.
func (g *G1) SetBytesOnCurve(b []byte) error {
	if len(b) < G1SizeCompressed {
		return errInputLength
	}

	// Check for invalid prefixes
	switch b[0] & 0xE0 {
	case 0x20, 0x60, 0xE0:
		return errEncoding
	}

	isCompressed := int((b[0] >> 7) & 0x1)
	isInfinity := int((b[0] >> 6) & 0x1)
	isBigYCoord := int((b[0] >> 5) & 0x1)

	if isInfinity == 1 {
		l := G1Size
		if isCompressed == 1 {
			l = G1SizeCompressed
		}
		if len(b) < l {
			return errInputLength
		}
		zeros := make([]byte, l-1)
		if (b[0]&0x1F) != 0 || subtle.ConstantTimeCompare(b[1:l], zeros) != 1 {
			return errEncoding
		}
		g.SetIdentity()
		return nil
	}

	x := (&[ff.FpSize]byte{})[:]
	copy(x, b)
	x[0] &= 0x1F
	if err := g.x.UnmarshalBinary(x); err != nil {
		return err
	}

	if isCompressed == 1 {
		x3b := &ff.Fp{}
		x3b.Sqr(&g.x)
		x3b.Mul(x3b, &g.x)
		x3b.Add(x3b, &g1Params.b)
		if g.y.Sqrt(x3b) == 0 {
			return errEncoding
		}
		if g.y.IsNegative() != isBigYCoord {
			g.y.Neg()
		}
	} else {
		if len(b) < G1Size {
			return errInputLength
		}
		if err := g.y.UnmarshalBinary(b[ff.FpSize:G1Size]); err != nil {
			return err
		}
	}

	g.z.SetOne()
	if !g.isValidProjective() || !g.isOnCurve() {
		return errEncoding
	}
	return nil
}

// InSubgroup reports whether g (assumed already on the curve, e.g. decoded via
// SetBytesOnCurve) lies in the prime-order r-torsion subgroup. It is the group
// check that blst's aggregate()/core_verify() enforce; the raw TON opcodes that
// use blst's aggregate() (ADD's second operand, SUB's first operand, all but the
// first element of AGGREGATE/FASTAGGREGATEVERIFY) require it. The identity is in
// every subgroup and is short-circuited (circl's isRTorsion is only exercised on
// non-identity points).
func (g *G1) InSubgroup() bool {
	if g.IsIdentity() {
		return true
	}
	return g.isRTorsion()
}

// SetBytesOnCurve sets g to the value in bytes, returning a non-nil error only
// if the encoding is malformed or the point is not on the G2 curve. Unlike
// SetBytes it does NOT reject points outside the r-torsion subgroup.
func (g *G2) SetBytesOnCurve(b []byte) error {
	if len(b) < G2SizeCompressed {
		return errInputLength
	}

	// Check for invalid prefixes
	switch b[0] & 0xE0 {
	case 0x20, 0x60, 0xE0:
		return errEncoding
	}

	isCompressed := int((b[0] >> 7) & 0x1)
	isInfinity := int((b[0] >> 6) & 0x1)
	isBigYCoord := int((b[0] >> 5) & 0x1)

	if isInfinity == 1 {
		l := G2Size
		if isCompressed == 1 {
			l = G2SizeCompressed
		}
		if len(b) < l {
			return errInputLength
		}
		zeros := make([]byte, l-1)
		if (b[0]&0x1F) != 0 || subtle.ConstantTimeCompare(b[1:l], zeros) != 1 {
			return errEncoding
		}
		g.SetIdentity()
		return nil
	}

	x := (&[ff.Fp2Size]byte{})[:]
	copy(x, b)
	x[0] &= 0x1F
	if err := g.x.UnmarshalBinary(x); err != nil {
		return err
	}

	if isCompressed == 1 {
		x3b := &ff.Fp2{}
		x3b.Sqr(&g.x)
		x3b.Mul(x3b, &g.x)
		x3b.Add(x3b, &g2Params.b)
		if g.y.Sqrt(x3b) == 0 {
			return errEncoding
		}
		if g.y.IsNegative() != isBigYCoord {
			g.y.Neg()
		}
	} else {
		if len(b) < G2Size {
			return errInputLength
		}
		if err := g.y.UnmarshalBinary(b[ff.Fp2Size:G2Size]); err != nil {
			return err
		}
	}

	g.z.SetOne()
	if !g.isValidProjective() || !g.isOnCurve() {
		return errEncoding
	}
	return nil
}

// InSubgroup reports whether g (assumed already on the curve) lies in the
// prime-order r-torsion subgroup. See the G1 method for details.
func (g *G2) InSubgroup() bool {
	if g.IsIdentity() {
		return true
	}
	return g.isRTorsion()
}
