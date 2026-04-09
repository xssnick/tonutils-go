package secp256k1

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"
)

func pointsEqual(a, b point) bool {
	if a.inf || b.inf {
		return a.inf == b.inf
	}
	return a.x.Cmp(b.x) == 0 && a.y.Cmp(b.y) == 0
}

func mustPointFromUncompressed(t *testing.T, data []byte) point {
	t.Helper()

	if len(data) != 65 || data[0] != 0x04 {
		t.Fatalf("unexpected uncompressed key encoding: %x", data)
	}
	p := point{
		x: new(big.Int).SetBytes(data[1:33]),
		y: new(big.Int).SetBytes(data[33:]),
	}
	if !isOnCurve(p) {
		t.Fatalf("decoded point is not on curve")
	}
	return p
}

func TestCurveArithmeticHelpers(t *testing.T) {
	if got := CurveOrderBytes(); len(got) != 32 || !bytes.Equal(got, curveN.FillBytes(make([]byte, 32))) {
		t.Fatalf("unexpected curve order bytes: %x", got)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("mustBigHex should panic on invalid input")
		}
	}()
	_ = mustBigHex("not-hex")
}

func TestCurveArithmeticAndSerialization(t *testing.T) {
	if got := mod(big.NewInt(-1), big.NewInt(7)); got.Int64() != 6 {
		t.Fatalf("unexpected mod result: %v", got)
	}

	if got := scalarFromBytes(curveN.FillBytes(make([]byte, 32))); got.Sign() != 0 {
		t.Fatalf("scalar should wrap modulo curve order, got %v", got)
	}

	if inv := inverse(big.NewInt(0), curveP); inv != nil {
		t.Fatalf("inverse(0) should be nil, got %v", inv)
	}

	if !isOnCurve(curveG) {
		t.Fatal("generator should be on curve")
	}
	if !isOnCurve(point{inf: true}) {
		t.Fatal("point at infinity should be accepted")
	}
	if isOnCurve(point{x: big.NewInt(1), y: big.NewInt(1)}) {
		t.Fatal("unexpected invalid point reported as on-curve")
	}

	cp := copyPoint(curveG)
	cp.x.Add(cp.x, big.NewInt(1))
	if curveG.x.Cmp(cp.x) == 0 {
		t.Fatal("copyPoint should deep-copy coordinates")
	}

	neg := negate(curveG)
	if !pointsEqual(add(curveG, neg), point{inf: true}) {
		t.Fatal("point plus its negation should be infinity")
	}
	if !pointsEqual(add(point{inf: true}, curveG), curveG) {
		t.Fatal("adding infinity should return the other operand")
	}
	if !pointsEqual(add(curveG, point{inf: true}), curveG) {
		t.Fatal("adding infinity should return the other operand")
	}

	if !pointsEqual(double(curveG), add(curveG, curveG)) {
		t.Fatal("double should match self-addition")
	}
	if !double(point{x: big.NewInt(1), y: big.NewInt(0)}).inf {
		t.Fatal("doubling a point with y=0 should return infinity")
	}

	if !scalarMult(curveG, big.NewInt(0)).inf {
		t.Fatal("scalar multiplication by zero should return infinity")
	}
	if !pointsEqual(scalarMult(curveG, big.NewInt(1)), curveG) {
		t.Fatal("scalar multiplication by one should return the base point")
	}
	if !pointsEqual(scalarMult(curveG, big.NewInt(2)), double(curveG)) {
		t.Fatal("scalar multiplication by two should match doubling")
	}

	square := mod(new(big.Int).Mul(curveG.y, curveG.y), curveP)
	root := modSqrt(square)
	if root == nil || mod(new(big.Int).Mul(root, root), curveP).Cmp(square) != 0 {
		t.Fatal("modSqrt should recover a square root for quadratic residues")
	}
	if got := modSqrt(new(big.Int).Sub(curveP, big.NewInt(1))); got != nil {
		t.Fatalf("expected no square root for -1 mod p, got %v", got)
	}

	enc := serializeUncompressed(curveG)
	if len(enc) != 65 || enc[0] != 0x04 {
		t.Fatalf("unexpected uncompressed encoding: %x", enc)
	}
	if got := serializeUncompressed(point{inf: true}); got != nil {
		t.Fatalf("expected nil encoding for infinity, got %x", got)
	}
}

func TestParseTweakSignAndRecover(t *testing.T) {
	if _, ok := parseXOnlyEven([]byte{1, 2, 3}); ok {
		t.Fatal("short x-only key should fail")
	}
	if _, ok := parseXOnlyEven(curveP.FillBytes(make([]byte, 32))); ok {
		t.Fatal("x >= p should fail")
	}

	var invalidX []byte
	for i := uint64(0); ; i++ {
		candidate := new(big.Int).SetUint64(i).FillBytes(make([]byte, 32))
		if _, ok := parseXOnlyEven(candidate); !ok {
			invalidX = candidate
			break
		}
	}
	if _, ok := parseXOnlyEven(invalidX); ok {
		t.Fatal("non-curve x-only key should fail")
	}

	xOnly := curveG.x.FillBytes(make([]byte, 32))
	pubPoint, ok := parseXOnlyEven(xOnly)
	if !ok {
		t.Fatal("generator x-coordinate should parse as x-only pubkey")
	}
	if pubPoint.y.Bit(0) != 0 {
		t.Fatal("parsed x-only pubkey should use the even y branch")
	}

	if _, ok = XOnlyPubkeyTweakAddUncompressed([]byte{1}, make([]byte, 32)); ok {
		t.Fatal("invalid x-only pubkey length should fail")
	}
	if _, ok = XOnlyPubkeyTweakAddUncompressed(xOnly, []byte{1}); ok {
		t.Fatal("invalid tweak length should fail")
	}
	if _, ok = XOnlyPubkeyTweakAddUncompressed(xOnly, curveN.FillBytes(make([]byte, 32))); ok {
		t.Fatal("tweak >= curve order should fail")
	}
	tweaked, ok := XOnlyPubkeyTweakAddUncompressed(xOnly, make([]byte, 32))
	if !ok {
		t.Fatal("zero tweak should succeed")
	}
	if want := serializeUncompressed(curveG); !bytes.Equal(tweaked, want) {
		t.Fatalf("unexpected tweak-add result:\nwant %x\ngot  %x", want, tweaked)
	}

	hash := sha256.Sum256([]byte("tonutils-go secp256k1"))
	if _, _, _, _, ok = SignRecoverable(make([]byte, 32), []byte{1}, hash[:]); ok {
		t.Fatal("zero private key should fail")
	}
	if _, _, _, _, ok = SignRecoverable([]byte{1}, make([]byte, 32), hash[:]); ok {
		t.Fatal("zero nonce should fail")
	}

	priv := make([]byte, 32)
	priv[31] = 9
	nonce := make([]byte, 32)
	nonce[31] = 7
	v, r, s, pub, ok := SignRecoverable(priv, nonce, hash[:])
	if !ok {
		t.Fatal("SignRecoverable should succeed for valid inputs")
	}
	if len(pub) != 65 {
		t.Fatalf("unexpected pubkey length: %d", len(pub))
	}

	recovered, ok := RecoverUncompressed(hash[:], r, s, v)
	if !ok {
		t.Fatal("RecoverUncompressed should recover the public key")
	}
	if !bytes.Equal(recovered, pub) {
		t.Fatalf("unexpected recovered pubkey:\nwant %x\ngot  %x", pub, recovered)
	}

	pubPoint = mustPointFromUncompressed(t, pub)
	if !verify(hash[:], r, s, pubPoint) {
		t.Fatal("signature should verify against the recovered public key")
	}
	tamperedHash := sha256.Sum256([]byte("different message"))
	if verify(tamperedHash[:], r, s, pubPoint) {
		t.Fatal("signature should not verify for a different message")
	}

	if _, ok = RecoverUncompressed(hash[:], r, s, 4); ok {
		t.Fatal("invalid recovery id should fail")
	}
	if _, ok = RecoverUncompressed(hash[:], big.NewInt(0), s, v); ok {
		t.Fatal("invalid r should fail recovery")
	}
}
