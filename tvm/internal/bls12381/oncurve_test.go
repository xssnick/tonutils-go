package bls12381

import (
	"bytes"
	"testing"

	"github.com/cloudflare/circl/ecc/bls12381/ff"
)

// findOffSubgroupG1 deterministically finds a point that is on the G1 curve but
// NOT in the r-torsion subgroup (exists because the G1 cofactor is > 1). Such a
// point is exactly what the raw TON BLS opcodes must accept and circl's SetBytes
// must reject.
func findOffSubgroupG1(t *testing.T) *G1 {
	t.Helper()
	for i := uint64(1); i < 1_000_000; i++ {
		var g G1
		g.x.SetUint64(i)
		x3b := &ff.Fp{}
		x3b.Sqr(&g.x)
		x3b.Mul(x3b, &g.x)
		x3b.Add(x3b, &g1Params.b)
		if g.y.Sqrt(x3b) == 0 {
			continue // x is not the abscissa of a curve point
		}
		g.z.SetOne()
		if !g.isOnCurve() {
			continue
		}
		if g.isRTorsion() {
			continue // in-subgroup, keep looking
		}
		return &g
	}
	t.Fatal("no off-subgroup G1 point found")
	return nil
}

func findOffSubgroupG2(t *testing.T) *G2 {
	t.Helper()
	for i := uint64(1); i < 1_000_000; i++ {
		var g G2
		g.x[0].SetUint64(i)
		x3b := &ff.Fp2{}
		x3b.Sqr(&g.x)
		x3b.Mul(x3b, &g.x)
		x3b.Add(x3b, &g2Params.b)
		if g.y.Sqrt(x3b) == 0 {
			continue
		}
		g.z.SetOne()
		if !g.isOnCurve() {
			continue
		}
		if g.isRTorsion() {
			continue
		}
		return &g
	}
	t.Fatal("no off-subgroup G2 point found")
	return nil
}

// TestSetBytesOnCurveG1AcceptsOffSubgroup verifies the whole point of the fix:
// an on-curve, off-subgroup encoding is REJECTED by SetBytes but ACCEPTED by
// SetBytesOnCurve, and round-trips to the same bytes.
func TestSetBytesOnCurveG1AcceptsOffSubgroup(t *testing.T) {
	g := findOffSubgroupG1(t)
	for _, enc := range [][]byte{g.BytesCompressed(), g.Bytes()} {
		var full G1
		if err := full.SetBytes(enc); err == nil {
			t.Fatal("SetBytes unexpectedly accepted an off-subgroup G1 point")
		}

		var oc G1
		if err := oc.SetBytesOnCurve(enc); err != nil {
			t.Fatalf("SetBytesOnCurve rejected a valid on-curve G1 point: %v", err)
		}
		if !bytes.Equal(oc.BytesCompressed(), g.BytesCompressed()) {
			t.Fatal("SetBytesOnCurve G1 decoded to a different point")
		}
	}
	// Sanity: it really is off-subgroup.
	if g.isRTorsion() {
		t.Fatal("test point was actually in-subgroup")
	}
}

func TestSetBytesOnCurveG2AcceptsOffSubgroup(t *testing.T) {
	g := findOffSubgroupG2(t)
	for _, enc := range [][]byte{g.BytesCompressed(), g.Bytes()} {
		var full G2
		if err := full.SetBytes(enc); err == nil {
			t.Fatal("SetBytes unexpectedly accepted an off-subgroup G2 point")
		}

		var oc G2
		if err := oc.SetBytesOnCurve(enc); err != nil {
			t.Fatalf("SetBytesOnCurve rejected a valid on-curve G2 point: %v", err)
		}
		if !bytes.Equal(oc.BytesCompressed(), g.BytesCompressed()) {
			t.Fatal("SetBytesOnCurve G2 decoded to a different point")
		}
	}
	if g.isRTorsion() {
		t.Fatal("test point was actually in-subgroup")
	}
}

// TestSetBytesOnCurveEquivalentInSubgroupG1 proves that for in-subgroup points
// (the only points circl's SetBytes accepts) SetBytesOnCurve decodes to a
// byte-identical point — i.e. the relaxed decoder is a strict superset that does
// not change behavior on the previously-accepted domain.
func TestSetBytesOnCurveEquivalentInSubgroupG1(t *testing.T) {
	gen := G1Generator()
	for k := uint64(1); k <= 256; k++ {
		var s Scalar
		s.SetUint64(k)
		var p G1
		p.ScalarMult(&s, gen)
		enc := p.BytesCompressed()

		var full, oc G1
		if err := full.SetBytes(enc); err != nil {
			t.Fatalf("k=%d: SetBytes rejected an in-subgroup point: %v", k, err)
		}
		if err := oc.SetBytesOnCurve(enc); err != nil {
			t.Fatalf("k=%d: SetBytesOnCurve rejected an in-subgroup point: %v", k, err)
		}
		if !bytes.Equal(full.BytesCompressed(), oc.BytesCompressed()) {
			t.Fatalf("k=%d: SetBytes and SetBytesOnCurve decoded to different points", k)
		}
		if !bytes.Equal(oc.BytesCompressed(), enc) {
			t.Fatalf("k=%d: SetBytesOnCurve did not round-trip", k)
		}
	}
}

func TestSetBytesOnCurveEquivalentInSubgroupG2(t *testing.T) {
	gen := G2Generator()
	for k := uint64(1); k <= 256; k++ {
		var s Scalar
		s.SetUint64(k)
		var p G2
		p.ScalarMult(&s, gen)
		enc := p.BytesCompressed()

		var full, oc G2
		if err := full.SetBytes(enc); err != nil {
			t.Fatalf("k=%d: SetBytes rejected an in-subgroup point: %v", k, err)
		}
		if err := oc.SetBytesOnCurve(enc); err != nil {
			t.Fatalf("k=%d: SetBytesOnCurve rejected an in-subgroup point: %v", k, err)
		}
		if !bytes.Equal(full.BytesCompressed(), oc.BytesCompressed()) {
			t.Fatalf("k=%d: SetBytes and SetBytesOnCurve decoded to different points", k)
		}
		if !bytes.Equal(oc.BytesCompressed(), enc) {
			t.Fatalf("k=%d: SetBytesOnCurve did not round-trip", k)
		}
	}
}

// TestInSubgroupMatchesFullDecode verifies InSubgroup agrees with SetBytes: an
// on-curve point is in the subgroup iff SetBytes (the full IsOnG1/IsOnG2 check)
// accepts it. Exercised for in-subgroup, off-subgroup, and the identity.
func TestInSubgroupMatchesFullDecode(t *testing.T) {
	// identity is in every subgroup
	var idG1 G1
	idG1.SetIdentity()
	if !idG1.InSubgroup() {
		t.Fatal("G1 identity reported out-of-subgroup")
	}
	var idG2 G2
	idG2.SetIdentity()
	if !idG2.InSubgroup() {
		t.Fatal("G2 identity reported out-of-subgroup")
	}

	// off-subgroup: on curve, InSubgroup false, SetBytes rejects
	offG1 := findOffSubgroupG1(t)
	if offG1.InSubgroup() {
		t.Fatal("off-subgroup G1 reported in-subgroup")
	}
	offG2 := findOffSubgroupG2(t)
	if offG2.InSubgroup() {
		t.Fatal("off-subgroup G2 reported in-subgroup")
	}

	// in-subgroup generator multiples: InSubgroup true, and consistent with SetBytes
	g1 := G1Generator()
	g2 := G2Generator()
	for k := uint64(1); k <= 64; k++ {
		var s Scalar
		s.SetUint64(k)

		var p1 G1
		p1.ScalarMult(&s, g1)
		var dec1 G1
		if err := dec1.SetBytesOnCurve(p1.BytesCompressed()); err != nil {
			t.Fatalf("k=%d: G1 SetBytesOnCurve failed: %v", k, err)
		}
		if !dec1.InSubgroup() {
			t.Fatalf("k=%d: in-subgroup G1 reported out-of-subgroup", k)
		}

		var p2 G2
		p2.ScalarMult(&s, g2)
		var dec2 G2
		if err := dec2.SetBytesOnCurve(p2.BytesCompressed()); err != nil {
			t.Fatalf("k=%d: G2 SetBytesOnCurve failed: %v", k, err)
		}
		if !dec2.InSubgroup() {
			t.Fatalf("k=%d: in-subgroup G2 reported out-of-subgroup", k)
		}
	}
}

// TestSetBytesOnCurveRejectsGarbage confirms malformed encodings and off-curve
// x-coordinates are still rejected by the relaxed decoder (it only drops the
// subgroup check, not the on-curve / encoding checks).
func TestSetBytesOnCurveRejectsGarbage(t *testing.T) {
	// All-0xFF: bad field element / bad prefix.
	bad := bytes.Repeat([]byte{0xFF}, G1SizeCompressed)
	var g G1
	if err := g.SetBytesOnCurve(bad); err == nil {
		t.Fatal("SetBytesOnCurve accepted garbage G1 bytes")
	}

	badG2 := bytes.Repeat([]byte{0xFF}, G2SizeCompressed)
	var g2 G2
	if err := g2.SetBytesOnCurve(badG2); err == nil {
		t.Fatal("SetBytesOnCurve accepted garbage G2 bytes")
	}

	// Too short.
	var short G1
	if err := short.SetBytesOnCurve(make([]byte, G1SizeCompressed-1)); err == nil {
		t.Fatal("SetBytesOnCurve accepted a too-short G1 slice")
	}
	var shortG2 G2
	if err := shortG2.SetBytesOnCurve(make([]byte, G2SizeCompressed-1)); err == nil {
		t.Fatal("SetBytesOnCurve accepted a too-short G2 slice")
	}

	for _, prefix := range []byte{0x20, 0x60, 0xE0} {
		invalidG1 := make([]byte, G1Size)
		invalidG1[0] = prefix
		if err := g.SetBytesOnCurve(invalidG1); err == nil {
			t.Fatalf("SetBytesOnCurve accepted invalid G1 prefix 0x%02x", prefix)
		}

		invalidG2 := make([]byte, G2Size)
		invalidG2[0] = prefix
		if err := g2.SetBytesOnCurve(invalidG2); err == nil {
			t.Fatalf("SetBytesOnCurve accepted invalid G2 prefix 0x%02x", prefix)
		}
	}

	invalidInfinityG1 := make([]byte, G1SizeCompressed)
	invalidInfinityG1[0] = 0xC0
	invalidInfinityG1[len(invalidInfinityG1)-1] = 1
	if err := g.SetBytesOnCurve(invalidInfinityG1); err == nil {
		t.Fatal("SetBytesOnCurve accepted non-zero G1 infinity payload")
	}
	invalidInfinityG2 := make([]byte, G2SizeCompressed)
	invalidInfinityG2[0] = 0xC0
	invalidInfinityG2[len(invalidInfinityG2)-1] = 1
	if err := g2.SetBytesOnCurve(invalidInfinityG2); err == nil {
		t.Fatal("SetBytesOnCurve accepted non-zero G2 infinity payload")
	}
}
