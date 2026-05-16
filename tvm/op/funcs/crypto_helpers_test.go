package funcs

import (
	"bytes"
	"errors"
	"math/big"
	"strings"
	"testing"

	ristretto "github.com/bwesterb/go-ristretto"
	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	circlgroup "github.com/cloudflare/circl/group"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/internal/blsmap"
)

func mustRistrettoInt(t *testing.T, point circlgroup.Element) *big.Int {
	t.Helper()

	data, err := point.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}
	return new(big.Int).SetBytes(data)
}

func mustBLSPointBytes(t *testing.T, sl *cell.Slice) []byte {
	t.Helper()

	data, err := sl.PreloadSlice(sl.BitsLeft())
	if err != nil {
		t.Fatalf("PreloadSlice failed: %v", err)
	}
	return data
}

func TestBLSAndRistrettoHelpers(t *testing.T) {
	st := newFuncTestState(t, nil)
	if err := pushSliceBytes(st, []byte{0xAA, 0xBB}); err != nil {
		t.Fatalf("pushSliceBytes failed: %v", err)
	}
	gotSlice, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	if got := mustSliceData(t, gotSlice); !bytes.Equal(got, []byte{0xAA, 0xBB}) {
		t.Fatalf("unexpected slice bytes: %x", got)
	}

	if err := blsUnknown("plain", nil); err == nil || !strings.Contains(err.Error(), "plain") {
		t.Fatalf("unexpected blsUnknown(nil) result: %v", err)
	}
	if err := blsUnknown("wrapped", errors.New("inner")); err == nil || !strings.Contains(err.Error(), "wrapped: inner") {
		t.Fatalf("unexpected blsUnknown(err) result: %v", err)
	}

	x := new(big.Int).Add(blsOrder, big.NewInt(5))
	if got := blsScalarMod(x); got.Int64() != 5 {
		t.Fatalf("blsScalarMod returned %v, want 5", got)
	}
	var wantPub, gotPub circlbls.G1
	wantPub.ScalarMult(blsScalarFromInt(big.NewInt(5)), circlbls.G1Generator())
	gotPub.ScalarMult(blsScalarFromInt(x), circlbls.G1Generator())
	if !wantPub.IsEqual(&gotPub) {
		t.Fatal("blsScalarFromInt should reduce scalars modulo the BLS order")
	}

	if got := blsCalculateMultiexpGas(16, 10, 2, 12); got != 10+16*2+16*12/4 {
		t.Fatalf("unexpected multiexp gas: %d", got)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice([]byte("abc"), 24).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	msg, err := popBLSMsg(st)
	if err != nil {
		t.Fatalf("popBLSMsg failed: %v", err)
	}
	if !bytes.Equal(msg, []byte("abc")) {
		t.Fatalf("unexpected message: %x", msg)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if _, err = popBLSMsg(st); err == nil {
		t.Fatal("popBLSMsg should reject non-byte-aligned messages")
	}

	pointBytes, err := preloadBLSPoint(cell.BeginCell().MustStoreSlice(blsG1ZeroCompressed, uint(len(blsG1ZeroCompressed))*8).ToSlice(), 48*8, "need 48 bytes")
	if err != nil || !bytes.Equal(pointBytes, blsG1ZeroCompressed) {
		t.Fatalf("preloadBLSPoint = (%x, %v)", pointBytes, err)
	}
	if _, err = preloadBLSPoint(cell.BeginCell().MustStoreUInt(0, 8).ToSlice(), 48*8, "need 48 bytes"); err == nil {
		t.Fatal("preloadBLSPoint should reject short slices")
	}

	if g1, err := parseBLSG1(blsG1ZeroCompressed); err != nil || !g1.IsIdentity() {
		t.Fatalf("parseBLSG1 = (%v, %v)", g1, err)
	}
	if _, err := parseBLSG1([]byte{1, 2, 3}); err == nil {
		t.Fatal("parseBLSG1 should reject invalid encodings")
	}
	if g2, err := parseBLSG2(blsG2ZeroCompressed); err != nil || !g2.IsIdentity() {
		t.Fatalf("parseBLSG2 = (%v, %v)", g2, err)
	}
	if _, err := parseBLSG2([]byte{1, 2, 3}); err == nil {
		t.Fatal("parseBLSG2 should reject invalid encodings")
	}

	msgA := []byte("message-a")
	msgB := []byte("message-b")
	hashA1 := blsHashToG2(msgA)
	hashA2 := blsHashToG2(msgA)
	hashB := blsHashToG2(msgB)
	if !hashA1.IsEqual(hashA2) || hashA1.IsEqual(hashB) {
		t.Fatal("blsHashToG2 should be deterministic and message-dependent")
	}

	sk1 := blsScalarFromInt(big.NewInt(3))
	sk2 := blsScalarFromInt(big.NewInt(7))
	var pub1, pub2 circlbls.G1
	pub1.ScalarMult(sk1, circlbls.G1Generator())
	pub2.ScalarMult(sk2, circlbls.G1Generator())

	hashPoint := blsHashToG2(msgA)
	var sig1, sig2 circlbls.G2
	sig1.ScalarMult(sk1, hashPoint)
	sig2.ScalarMult(sk2, hashPoint)

	if !blsVerify(pub1.BytesCompressed(), msgA, sig1.BytesCompressed()) {
		t.Fatal("blsVerify should accept a valid signature")
	}
	if blsVerify(blsG1ZeroCompressed, msgA, sig1.BytesCompressed()) {
		t.Fatal("blsVerify should reject identity pubkeys")
	}

	aggSigSameMsg := sig1
	aggSigSameMsg.Add(&aggSigSameMsg, &sig2)
	if !blsFastAggregateVerify([][]byte{pub1.BytesCompressed(), pub2.BytesCompressed()}, msgA, aggSigSameMsg.BytesCompressed()) {
		t.Fatal("blsFastAggregateVerify should accept a valid aggregate signature")
	}
	if blsFastAggregateVerify(nil, msgA, aggSigSameMsg.BytesCompressed()) {
		t.Fatal("blsFastAggregateVerify should reject empty pubkey sets")
	}

	hashBPoint := blsHashToG2(msgB)
	var sigB circlbls.G2
	sigB.ScalarMult(sk2, hashBPoint)
	aggSigDistinct := sig1
	aggSigDistinct.Add(&aggSigDistinct, &sigB)
	if !blsAggregateVerify(
		[][]byte{pub1.BytesCompressed(), pub2.BytesCompressed()},
		[][]byte{msgA, msgB},
		aggSigDistinct.BytesCompressed(),
	) {
		t.Fatal("blsAggregateVerify should accept valid distinct-message aggregates")
	}
	if blsAggregateVerify([][]byte{pub1.BytesCompressed()}, [][]byte{msgA, msgB}, aggSigDistinct.BytesCompressed()) {
		t.Fatal("blsAggregateVerify should reject mismatched pub/message counts")
	}

	identity := circlgroup.Ristretto255.NewElement()
	encodedIdentity, err := identity.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(identity) failed: %v", err)
	}
	point, err := parseRistrettoPoint(new(big.Int).SetBytes(encodedIdentity))
	if err != nil {
		t.Fatalf("parseRistrettoPoint(valid) failed: %v", err)
	}
	if encoded, err := point.MarshalBinary(); err != nil || !bytes.Equal(encoded, encodedIdentity) {
		t.Fatalf("unexpected parsed ristretto point: %x / %v", encoded, err)
	}
	if _, err = parseRistrettoPoint(new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32))); err == nil {
		t.Fatal("parseRistrettoPoint should reject invalid encodings")
	}

	st = newFuncTestState(t, nil)
	if err = pushRistrettoPoint(st, identity); err != nil {
		t.Fatalf("pushRistrettoPoint failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || !bytes.Equal(got.FillBytes(make([]byte, 32)), encodedIdentity) {
		t.Fatalf("pushRistrettoPoint = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushInt(new(big.Int).SetBytes(encodedIdentity)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = RIST255_VALIDATE().Interpret(st); err != nil {
		t.Fatalf("RIST255_VALIDATE failed: %v", err)
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("RIST255_VALIDATE should not leave stack values on success, len=%d", st.Stack.Len())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushInt(new(big.Int).SetBytes(encodedIdentity)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = RIST255_QVALIDATE().Interpret(st); err != nil {
		t.Fatalf("RIST255_QVALIDATE(valid) failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("RIST255_QVALIDATE(valid) = (%v, %v)", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushInt(new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32))); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = RIST255_QVALIDATE().Interpret(st); err != nil {
		t.Fatalf("RIST255_QVALIDATE(invalid) failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || ok {
		t.Fatalf("RIST255_QVALIDATE(invalid) = (%v, %v)", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = RIST255_MULBASE().Interpret(st); err != nil {
		t.Fatalf("RIST255_MULBASE failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
		t.Fatalf("RIST255_MULBASE(0) = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = RIST255_QMULBASE().Interpret(st); err != nil {
		t.Fatalf("RIST255_QMULBASE failed: %v", err)
	}
	ok, err := st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("RIST255_QMULBASE bool = (%v, %v)", ok, err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
		t.Fatalf("RIST255_QMULBASE value = (%v, %v)", got, err)
	}
}

func TestBLSMapAndZeroOps(t *testing.T) {
	g1Input := bytes.Repeat([]byte{0x42}, 48)
	g1Expected, err := blsmap.MapToG1(g1Input)
	if err != nil {
		t.Fatalf("MapToG1 failed: %v", err)
	}
	st := newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(g1Input, 48*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = BLS_MAP_TO_G1().Interpret(st); err != nil {
		t.Fatalf("BLS_MAP_TO_G1 failed: %v", err)
	}
	gotG1, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	if got := mustSliceData(t, gotG1); !bytes.Equal(got, g1Expected) {
		t.Fatalf("unexpected BLS_MAP_TO_G1 output:\nwant %x\ngot  %x", g1Expected, got)
	}

	g2Input := bytes.Repeat([]byte{0x24}, 96)
	g2Expected, err := blsmap.MapToG2(g2Input)
	if err != nil {
		t.Fatalf("MapToG2 failed: %v", err)
	}
	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(g2Input, 96*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = BLS_MAP_TO_G2().Interpret(st); err != nil {
		t.Fatalf("BLS_MAP_TO_G2 failed: %v", err)
	}
	gotG2, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	if got := mustSliceData(t, gotG2); !bytes.Equal(got, g2Expected) {
		t.Fatalf("unexpected BLS_MAP_TO_G2 output:\nwant %x\ngot  %x", g2Expected, got)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(blsG1ZeroCompressed, uint(len(blsG1ZeroCompressed))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = BLS_G1_ISZERO().Interpret(st); err != nil {
		t.Fatalf("BLS_G1_ISZERO failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("BLS_G1_ISZERO = (%v, %v)", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(blsG2ZeroCompressed, uint(len(blsG2ZeroCompressed))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = BLS_G2_ISZERO().Interpret(st); err != nil {
		t.Fatalf("BLS_G2_ISZERO failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("BLS_G2_ISZERO = (%v, %v)", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(blsG1ZeroCompressed, uint(len(blsG1ZeroCompressed))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = BLS_G1_INGROUP().Interpret(st); err != nil {
		t.Fatalf("BLS_G1_INGROUP failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("BLS_G1_INGROUP = (%v, %v)", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(blsG2ZeroCompressed, uint(len(blsG2ZeroCompressed))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = BLS_G2_INGROUP().Interpret(st); err != nil {
		t.Fatalf("BLS_G2_INGROUP failed: %v", err)
	}
	if ok, err := st.Stack.PopBool(); err != nil || !ok {
		t.Fatalf("BLS_G2_INGROUP = (%v, %v)", ok, err)
	}
}

func TestRistrettoArithmeticAndBLSGroupOps(t *testing.T) {
	s3 := circlgroup.Ristretto255.NewScalar().SetBigInt(big.NewInt(3))
	s5 := circlgroup.Ristretto255.NewScalar().SetBigInt(big.NewInt(5))
	s2 := circlgroup.Ristretto255.NewScalar().SetBigInt(big.NewInt(2))
	p3 := circlgroup.Ristretto255.NewElement()
	p3.MulGen(s3)
	p5 := circlgroup.Ristretto255.NewElement()
	p5.MulGen(s5)
	p3Int := mustRistrettoInt(t, p3)
	p5Int := mustRistrettoInt(t, p5)
	invalidRistretto := new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32))

	t.Run("RistrettoOps", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushInt(p3Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(p5Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_ADD().Interpret(st); err != nil {
			t.Fatalf("RIST255_ADD failed: %v", err)
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		wantAdd := circlgroup.Ristretto255.NewElement().Add(p3, p5)
		if !bytes.Equal(got.FillBytes(make([]byte, 32)), mustRistrettoInt(t, wantAdd).FillBytes(make([]byte, 32))) {
			t.Fatalf("unexpected RIST255_ADD result: %x", got.FillBytes(make([]byte, 32)))
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(p3Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(p5Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_QADD().Interpret(st); err != nil {
			t.Fatalf("RIST255_QADD failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("RIST255_QADD bool = (%v, %v)", ok, err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if !bytes.Equal(got.FillBytes(make([]byte, 32)), mustRistrettoInt(t, wantAdd).FillBytes(make([]byte, 32))) {
			t.Fatalf("unexpected RIST255_QADD result: %x", got.FillBytes(make([]byte, 32)))
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(p3Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(invalidRistretto); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_QADD().Interpret(st); err != nil {
			t.Fatalf("RIST255_QADD(invalid) failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || ok {
			t.Fatalf("RIST255_QADD(invalid) = (%v, %v)", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(p3Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(p5Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_SUB().Interpret(st); err != nil {
			t.Fatalf("RIST255_SUB failed: %v", err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		wantSub := circlgroup.Ristretto255.NewElement().Add(p3, circlgroup.Ristretto255.NewElement().Neg(p5))
		if !bytes.Equal(got.FillBytes(make([]byte, 32)), mustRistrettoInt(t, wantSub).FillBytes(make([]byte, 32))) {
			t.Fatalf("unexpected RIST255_SUB result: %x", got.FillBytes(make([]byte, 32)))
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(p3Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(p5Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_QSUB().Interpret(st); err != nil {
			t.Fatalf("RIST255_QSUB failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("RIST255_QSUB bool = (%v, %v)", ok, err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if !bytes.Equal(got.FillBytes(make([]byte, 32)), mustRistrettoInt(t, wantSub).FillBytes(make([]byte, 32))) {
			t.Fatalf("unexpected RIST255_QSUB result: %x", got.FillBytes(make([]byte, 32)))
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(p3Int); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_MUL().Interpret(st); err != nil {
			t.Fatalf("RIST255_MUL failed: %v", err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		wantMul := circlgroup.Ristretto255.NewElement().Mul(p3, s2)
		if !bytes.Equal(got.FillBytes(make([]byte, 32)), mustRistrettoInt(t, wantMul).FillBytes(make([]byte, 32))) {
			t.Fatalf("unexpected RIST255_MUL result: %x", got.FillBytes(make([]byte, 32)))
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(invalidRistretto); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_QMUL().Interpret(st); err != nil {
			t.Fatalf("RIST255_QMUL(invalid) failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || ok {
			t.Fatalf("RIST255_QMUL(invalid) = (%v, %v)", ok, err)
		}

		var u1, u2 [32]byte
		u1[31] = 1
		u2[31] = 2
		var wantHashP1, wantHashP2, wantHashOut ristretto.Point
		wantHashP1.SetElligator(&u1)
		wantHashP2.SetElligator(&u2)
		wantHashOut.Add(&wantHashP1, &wantHashP2)

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_FROMHASH().Interpret(st); err != nil {
			t.Fatalf("RIST255_FROMHASH failed: %v", err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if !bytes.Equal(got.FillBytes(make([]byte, 32)), wantHashOut.Bytes()) {
			t.Fatalf("unexpected RIST255_FROMHASH result: %x", got.FillBytes(make([]byte, 32)))
		}
	})

	t.Run("BLSOps", func(t *testing.T) {
		msgA := []byte("message-a")
		msgB := []byte("message-b")
		sk1 := blsScalarFromInt(big.NewInt(3))
		sk2 := blsScalarFromInt(big.NewInt(7))

		var pub1, pub2 circlbls.G1
		pub1.ScalarMult(sk1, circlbls.G1Generator())
		pub2.ScalarMult(sk2, circlbls.G1Generator())

		hashA := blsHashToG2(msgA)
		hashB := blsHashToG2(msgB)

		var sig1, sig2, sigB circlbls.G2
		sig1.ScalarMult(sk1, hashA)
		sig2.ScalarMult(sk2, hashA)
		sigB.ScalarMult(sk2, hashB)

		aggSameMsg := sig1
		aggSameMsg.Add(&aggSameMsg, &sig2)
		aggDistinct := sig1
		aggDistinct.Add(&aggDistinct, &sigB)

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgA, uint(len(msgA))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_VERIFY().Interpret(st); err != nil {
			t.Fatalf("BLS_VERIFY failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || !ok {
			t.Fatalf("BLS_VERIFY = (%v, %v)", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig2.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_AGGREGATE().Interpret(st); err != nil {
			t.Fatalf("BLS_AGGREGATE failed: %v", err)
		}
		gotAgg, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotAgg); !bytes.Equal(got, aggSameMsg.BytesCompressed()) {
			t.Fatalf("unexpected BLS_AGGREGATE output:\nwant %x\ngot  %x", aggSameMsg.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub2.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgA, uint(len(msgA))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(aggSameMsg.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_FASTAGGREGATEVERIFY().Interpret(st); err != nil {
			t.Fatalf("BLS_FASTAGGREGATEVERIFY failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || !ok {
			t.Fatalf("BLS_FASTAGGREGATEVERIFY = (%v, %v)", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgA, uint(len(msgA))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub2.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msgB, uint(len(msgB))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(aggDistinct.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_AGGREGATEVERIFY().Interpret(st); err != nil {
			t.Fatalf("BLS_AGGREGATEVERIFY failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || !ok {
			t.Fatalf("BLS_AGGREGATEVERIFY = (%v, %v)", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub2.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_ADD().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_ADD failed: %v", err)
		}
		gotG1, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		wantG1Add := pub1
		wantG1Add.Add(&wantG1Add, &pub2)
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, wantG1Add.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G1_ADD output:\nwant %x\ngot  %x", wantG1Add.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub2.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_SUB().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_SUB failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		wantG1Sub := pub2
		wantG1Sub.Neg()
		wantG1Sub.Add(&pub1, &wantG1Sub)
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, wantG1Sub.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G1_SUB output:\nwant %x\ngot  %x", wantG1Sub.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_NEG().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_NEG failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		wantG1Neg := pub1
		wantG1Neg.Neg()
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, wantG1Neg.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G1_NEG output:\nwant %x\ngot  %x", wantG1Neg.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MUL().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_MUL failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		var wantG1Mul circlbls.G1
		wantG1Mul.ScalarMult(blsScalarFromInt(big.NewInt(2)), &pub1)
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, wantG1Mul.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G1_MUL output:\nwant %x\ngot  %x", wantG1Mul.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MUL().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_MUL(0) failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, blsG1ZeroCompressed) {
			t.Fatalf("unexpected BLS_G1_MUL zero output: %x", got)
		}

		st = newFuncTestState(t, nil)
		if err := BLS_G1_ZERO().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_ZERO failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, blsG1ZeroCompressed) {
			t.Fatalf("unexpected BLS_G1_ZERO output: %x", got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub2.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MULTIEXP().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_MULTIEXP failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		var term1, term2, wantG1Multi circlbls.G1
		term1.ScalarMult(blsScalarFromInt(big.NewInt(2)), &pub1)
		term2.ScalarMult(blsScalarFromInt(big.NewInt(3)), &pub2)
		wantG1Multi.Add(&term1, &term2)
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, wantG1Multi.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G1_MULTIEXP output:\nwant %x\ngot  %x", wantG1Multi.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MULTIEXP().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_MULTIEXP(1) failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, wantG1Mul.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G1_MULTIEXP single output:\nwant %x\ngot  %x", wantG1Mul.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MULTIEXP().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_MULTIEXP(0) failed: %v", err)
		}
		gotG1, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG1); !bytes.Equal(got, blsG1ZeroCompressed) {
			t.Fatalf("unexpected BLS_G1_MULTIEXP zero output: %x", got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig2.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_ADD().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_ADD failed: %v", err)
		}
		gotG2, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		wantG2Add := sig1
		wantG2Add.Add(&wantG2Add, &sig2)
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, wantG2Add.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G2_ADD output:\nwant %x\ngot  %x", wantG2Add.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig2.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_SUB().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_SUB failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		wantG2Sub := sig2
		wantG2Sub.Neg()
		wantG2Sub.Add(&sig1, &wantG2Sub)
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, wantG2Sub.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G2_SUB output:\nwant %x\ngot  %x", wantG2Sub.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_NEG().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_NEG failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		wantG2Neg := sig1
		wantG2Neg.Neg()
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, wantG2Neg.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G2_NEG output:\nwant %x\ngot  %x", wantG2Neg.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MUL().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_MUL failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		var wantG2Mul circlbls.G2
		wantG2Mul.ScalarMult(blsScalarFromInt(big.NewInt(2)), &sig1)
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, wantG2Mul.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G2_MUL output:\nwant %x\ngot  %x", wantG2Mul.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MUL().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_MUL(0) failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, blsG2ZeroCompressed) {
			t.Fatalf("unexpected BLS_G2_MUL zero output: %x", got)
		}

		st = newFuncTestState(t, nil)
		if err := BLS_G2_ZERO().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_ZERO failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, blsG2ZeroCompressed) {
			t.Fatalf("unexpected BLS_G2_ZERO output: %x", got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sigB.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MULTIEXP().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_MULTIEXP failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		var g2Term1, g2Term2, wantG2Multi circlbls.G2
		g2Term1.ScalarMult(blsScalarFromInt(big.NewInt(2)), &sig1)
		g2Term2.ScalarMult(blsScalarFromInt(big.NewInt(3)), &sigB)
		wantG2Multi.Add(&g2Term1, &g2Term2)
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, wantG2Multi.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G2_MULTIEXP output:\nwant %x\ngot  %x", wantG2Multi.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MULTIEXP().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_MULTIEXP(1) failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, wantG2Mul.BytesCompressed()) {
			t.Fatalf("unexpected BLS_G2_MULTIEXP single output:\nwant %x\ngot  %x", wantG2Mul.BytesCompressed(), got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MULTIEXP().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_MULTIEXP(0) failed: %v", err)
		}
		gotG2, err = st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if got := mustBLSPointBytes(t, gotG2); !bytes.Equal(got, blsG2ZeroCompressed) {
			t.Fatalf("unexpected BLS_G2_MULTIEXP zero output: %x", got)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_PAIRING().Interpret(st); err != nil {
			t.Fatalf("BLS_PAIRING(0) failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || !ok {
			t.Fatalf("BLS_PAIRING(0) = (%v, %v)", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(blsG2ZeroCompressed, 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_PAIRING().Interpret(st); err != nil {
			t.Fatalf("BLS_PAIRING failed: %v", err)
		}
		if ok, err := st.Stack.PopBool(); err != nil || !ok {
			t.Fatalf("BLS_PAIRING = (%v, %v)", ok, err)
		}
	})
}
