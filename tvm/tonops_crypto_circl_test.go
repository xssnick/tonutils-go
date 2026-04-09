package tvm

import (
	"bytes"
	"math/big"
	"testing"

	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	circlgroup "github.com/cloudflare/circl/group"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTonOpsCryptoCirclRistretto(t *testing.T) {
	t.Run("PushL", func(t *testing.T) {
		want, ok := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
		if !ok {
			t.Fatal("failed to parse expected ristretto order")
		}

		_, res, err := runRawCode(codeFromBuilders(t, funcsop.RIST255_PUSHL().Serialize()))
		if err != nil {
			t.Fatalf("unexpected pushl error: %v", err)
		}
		got, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop pushl result: %v", popErr)
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("unexpected ristretto pushl result")
		}
	})

	t.Run("FromHash", func(t *testing.T) {
		x1 := big.NewInt(1)
		x2 := big.NewInt(2)

		_, res, err := runRawCode(codeFromBuilders(t, funcsop.RIST255_FROMHASH().Serialize()), x1, x2)
		if err != nil {
			t.Fatalf("unexpected fromhash error: %v", err)
		}
		got, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop fromhash result: %v", popErr)
		}
		want := testRistrettoFromHashInt(t, x1, x2)
		if got.Cmp(want) != 0 {
			t.Fatalf("unexpected ristretto fromhash result")
		}

		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_FROMHASH().Serialize()), int64(-1), int64(0))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected fromhash x1 range exit code: %d", code)
		}
	})

	t.Run("ValidateAndQuiet", func(t *testing.T) {
		valid := testRistrettoMulBaseInt(t, 1)
		invalid := testInvalidRistrettoInt(t)

		stack, res, err := runRawCode(codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()), valid)
		if err != nil {
			t.Fatalf("unexpected validate error: %v", err)
		}
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("unexpected validate exit code: %d", code)
		}
		if stack.Len() != 0 {
			t.Fatalf("expected empty stack after validate, got %d items", stack.Len())
		}

		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()), invalid)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected invalid validate exit code: %d", code)
		}

		stack, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()), invalid)
		if err != nil {
			t.Fatalf("unexpected qvalidate error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop qvalidate result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected qvalidate(false) for invalid point")
		}
	})

	t.Run("AddSubMulAndMulBase", func(t *testing.T) {
		aInt := testRistrettoMulBaseInt(t, 5)
		bInt := testRistrettoMulBaseInt(t, 7)

		aEl, err := circlgroup.Ristretto255.NewElement(), error(nil)
		if err = aEl.UnmarshalBinary(aInt.FillBytes(make([]byte, 32))); err != nil {
			t.Fatalf("failed to parse point a: %v", err)
		}
		bEl := circlgroup.Ristretto255.NewElement()
		if err = bEl.UnmarshalBinary(bInt.FillBytes(make([]byte, 32))); err != nil {
			t.Fatalf("failed to parse point b: %v", err)
		}

		sumEl := circlgroup.Ristretto255.NewElement().Add(aEl, bEl)
		_, res, err := runRawCode(codeFromBuilders(t, funcsop.RIST255_ADD().Serialize()), aInt, bInt)
		if err != nil {
			t.Fatalf("unexpected add error: %v", err)
		}
		sum, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop add result: %v", popErr)
		}
		if sum.Cmp(testRistrettoElementInt(t, sumEl)) != 0 {
			t.Fatalf("unexpected ristretto add result")
		}

		diffEl := circlgroup.Ristretto255.NewElement().Add(aEl, circlgroup.Ristretto255.NewElement().Neg(bEl))
		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_SUB().Serialize()), aInt, bInt)
		if err != nil {
			t.Fatalf("unexpected sub error: %v", err)
		}
		diff, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop sub result: %v", popErr)
		}
		if diff.Cmp(testRistrettoElementInt(t, diffEl)) != 0 {
			t.Fatalf("unexpected ristretto sub result")
		}

		mulEl := circlgroup.Ristretto255.NewElement()
		mulEl.Mul(aEl, circlgroup.Ristretto255.NewScalar().SetBigInt(big.NewInt(3)))
		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_MUL().Serialize()), aInt, int64(3))
		if err != nil {
			t.Fatalf("unexpected mul error: %v", err)
		}
		mul, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop mul result: %v", popErr)
		}
		if mul.Cmp(testRistrettoElementInt(t, mulEl)) != 0 {
			t.Fatalf("unexpected ristretto mul result")
		}

		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_MULBASE().Serialize()), int64(11))
		if err != nil {
			t.Fatalf("unexpected mulbase error: %v", err)
		}
		mulBase, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop mulbase result: %v", popErr)
		}
		if mulBase.Cmp(testRistrettoMulBaseInt(t, 11)) != 0 {
			t.Fatalf("unexpected ristretto mulbase result")
		}

		invalid := testInvalidRistrettoInt(t)
		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_QADD().Serialize()), aInt, invalid)
		if err != nil {
			t.Fatalf("unexpected qadd error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop qadd result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected qadd(false) for invalid point")
		}

		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_QSUB().Serialize()), aInt, invalid)
		if err != nil {
			t.Fatalf("unexpected qsub error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop qsub result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected qsub(false) for invalid point")
		}

		_, res, err = runRawCode(codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()), invalid, int64(3))
		if err != nil {
			t.Fatalf("unexpected qmul error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop qmul result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected qmul(false) for invalid point")
		}
	})
}

func TestTonOpsCryptoCirclBLS(t *testing.T) {
	msg := []byte("ton-circl-bls")
	pub1 := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig1 := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	aggSig := testBLSAggregateSigBytes(t, sig1, sig2)

	t.Run("PushR", func(t *testing.T) {
		want := new(big.Int).SetBytes(circlbls.Order())

		_, res, err := runRawCode(codeFromBuilders(t, funcsop.BLS_PUSHR().Serialize()))
		if err != nil {
			t.Fatalf("unexpected pushr error: %v", err)
		}
		got, popErr := res.Stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop pushr result: %v", popErr)
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("unexpected bls pushr result")
		}
	})

	t.Run("Verify", func(t *testing.T) {
		stack, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			testSliceFromBytes(pub1),
			testSliceFromBytes(msg),
			testSliceFromBytes(sig1),
		)
		if err != nil {
			t.Fatalf("unexpected verify error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop verify result: %v", popErr)
		}
		if !ok {
			t.Fatalf("expected verify to succeed")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			testSliceFromBytes(msg),
			testSliceFromBytes(sig1),
		)
		if err != nil {
			t.Fatalf("unexpected invalid verify error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop invalid verify result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected invalid verify to return false")
		}
		_ = stack
	})

	t.Run("AggregateAndFastAggregateVerify", func(t *testing.T) {
		stack, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			testSliceFromBytes(sig1),
			testSliceFromBytes(sig2),
			int64(2),
		)
		if err != nil {
			t.Fatalf("unexpected aggregate error: %v", err)
		}
		got, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop aggregate result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, got), aggSig) {
			t.Fatalf("unexpected aggregate result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			testSliceFromBytes(pub1),
			testSliceFromBytes(pub2),
			int64(2),
			testSliceFromBytes(msg),
			testSliceFromBytes(aggSig),
		)
		if err != nil {
			t.Fatalf("unexpected fast aggregate verify error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop fast aggregate verify result: %v", popErr)
		}
		if !ok {
			t.Fatalf("expected fast aggregate verify to succeed")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			int64(0),
			testSliceFromBytes(msg),
			testSliceFromBytes(aggSig),
		)
		if err != nil {
			t.Fatalf("unexpected empty fast aggregate verify error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop empty fast aggregate verify result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected empty fast aggregate verify to return false")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			testSliceFromBytes(pub2),
			int64(2),
			testSliceFromBytes(msg),
			testSliceFromBytes(aggSig),
		)
		if err != nil {
			t.Fatalf("unexpected invalid fast aggregate verify error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop invalid fast aggregate verify result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected invalid fast aggregate verify to return false")
		}
		_ = stack
	})

	t.Run("AggregateVerifyAllowsDuplicateMessages", func(t *testing.T) {
		stack, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			testSliceFromBytes(pub1),
			testSliceFromBytes(msg),
			testSliceFromBytes(pub2),
			testSliceFromBytes(msg),
			int64(2),
			testSliceFromBytes(aggSig),
		)
		if err != nil {
			t.Fatalf("unexpected aggregate verify error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop aggregate verify result: %v", popErr)
		}
		if !ok {
			t.Fatalf("expected aggregate verify with duplicate messages to succeed")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			testSliceFromBytes(msg),
			int64(1),
			testSliceFromBytes(sig1),
		)
		if err != nil {
			t.Fatalf("unexpected invalid aggregate verify error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop invalid aggregate verify result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected invalid aggregate verify to return false")
		}
		_ = stack
	})

	t.Run("G1AndG2Arithmetic", func(t *testing.T) {
		g1a := testBLSG1BytesForScalar(2)
		g1b := testBLSG1BytesForScalar(3)
		g2a := testBLSG2BytesForScalar(2)
		g2b := testBLSG2BytesForScalar(3)

		var wantG1Add circlbls.G1
		var g1aPoint, g1bPoint circlbls.G1
		if err := g1aPoint.SetBytes(g1a); err != nil {
			t.Fatalf("failed to parse g1a: %v", err)
		}
		if err := g1bPoint.SetBytes(g1b); err != nil {
			t.Fatalf("failed to parse g1b: %v", err)
		}
		wantG1Add.Add(&g1aPoint, &g1bPoint)

		stack, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			testSliceFromBytes(g1a),
			testSliceFromBytes(g1b),
		)
		if err != nil {
			t.Fatalf("unexpected g1 add error: %v", err)
		}
		gotG1Add, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g1 add result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG1Add), wantG1Add.BytesCompressed()) {
			t.Fatalf("unexpected g1 add result")
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_SUB().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			testSliceFromBytes(g1b),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g1 sub exit code: %d", code)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_NEG().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g1 neg exit code: %d", code)
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			testSliceFromBytes(g1a),
			int64(0),
		)
		if err != nil {
			t.Fatalf("unexpected g1 mul-by-zero error: %v", err)
		}
		gotG1Zero, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g1 mul zero result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG1Zero), testBLSG1ZeroBytes()) {
			t.Fatalf("unexpected g1 mul zero result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			int64(0),
		)
		if err != nil {
			t.Fatalf("unexpected g1 mul-by-zero invalid-point error: %v", err)
		}
		gotG1Zero, popErr = res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g1 mul zero invalid-point result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG1Zero), testBLSG1ZeroBytes()) {
			t.Fatalf("unexpected g1 mul zero invalid-point result")
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			int64(3),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g1 mul exit code: %d", code)
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			int64(0),
		)
		if err != nil {
			t.Fatalf("unexpected g1 multiexp zero error: %v", err)
		}
		gotG1Zero, popErr = res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g1 multiexp zero result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG1Zero), testBLSG1ZeroBytes()) {
			t.Fatalf("unexpected g1 multiexp zero result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			int64(0),
			int64(1),
		)
		if err != nil {
			t.Fatalf("unexpected g1 multiexp zero invalid-point error: %v", err)
		}
		gotG1Zero, popErr = res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g1 multiexp zero invalid-point result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG1Zero), testBLSG1ZeroBytes()) {
			t.Fatalf("unexpected g1 multiexp zero invalid-point result")
		}

		var wantG1Multi circlbls.G1
		wantG1Multi.SetIdentity()
		var g1m1, g1m2 circlbls.G1
		if err := g1m1.SetBytes(g1a); err != nil {
			t.Fatalf("failed to parse g1 multiexp point 1: %v", err)
		}
		if err := g1m2.SetBytes(g1b); err != nil {
			t.Fatalf("failed to parse g1 multiexp point 2: %v", err)
		}
		var g1term1, g1term2 circlbls.G1
		g1term1.ScalarMult(testBLSScalar(5), &g1m1)
		g1term2.ScalarMult(testBLSScalar(7), &g1m2)
		wantG1Multi.Add(&g1term1, &g1term2)

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			testSliceFromBytes(g1a),
			int64(5),
			testSliceFromBytes(g1b),
			int64(7),
			int64(2),
		)
		if err != nil {
			t.Fatalf("unexpected g1 multiexp error: %v", err)
		}
		gotG1Multi, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g1 multiexp result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG1Multi), wantG1Multi.BytesCompressed()) {
			t.Fatalf("unexpected g1 multiexp result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_ISZERO().Serialize()),
			testSliceFromBytes(testBLSG1ZeroBytes()),
		)
		if err != nil {
			t.Fatalf("unexpected g1 iszero error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop g1 iszero result: %v", popErr)
		}
		if !ok {
			t.Fatalf("expected g1 iszero to return true")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
		)
		if err != nil {
			t.Fatalf("unexpected g1 ingroup error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop g1 ingroup result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected invalid g1 encoding to be out of group")
		}

		var wantG2Add circlbls.G2
		var g2aPoint, g2bPoint circlbls.G2
		if err := g2aPoint.SetBytes(g2a); err != nil {
			t.Fatalf("failed to parse g2a: %v", err)
		}
		if err := g2bPoint.SetBytes(g2b); err != nil {
			t.Fatalf("failed to parse g2b: %v", err)
		}
		wantG2Add.Add(&g2aPoint, &g2bPoint)

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			testSliceFromBytes(g2a),
			testSliceFromBytes(g2b),
		)
		if err != nil {
			t.Fatalf("unexpected g2 add error: %v", err)
		}
		gotG2Add, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g2 add result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG2Add), wantG2Add.BytesCompressed()) {
			t.Fatalf("unexpected g2 add result")
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_SUB().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
			testSliceFromBytes(g2b),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g2 sub exit code: %d", code)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_NEG().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g2 neg exit code: %d", code)
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			testSliceFromBytes(g2a),
			int64(0),
		)
		if err != nil {
			t.Fatalf("unexpected g2 mul-by-zero error: %v", err)
		}
		gotG2Zero, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g2 mul zero result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG2Zero), testBLSG2ZeroBytes()) {
			t.Fatalf("unexpected g2 mul zero result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
			int64(0),
		)
		if err != nil {
			t.Fatalf("unexpected g2 mul-by-zero invalid-point error: %v", err)
		}
		gotG2Zero, popErr = res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g2 mul zero invalid-point result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG2Zero), testBLSG2ZeroBytes()) {
			t.Fatalf("unexpected g2 mul zero invalid-point result")
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
			int64(3),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g2 mul exit code: %d", code)
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			int64(0),
		)
		if err != nil {
			t.Fatalf("unexpected g2 multiexp zero error: %v", err)
		}
		gotG2Zero, popErr = res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g2 multiexp zero result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG2Zero), testBLSG2ZeroBytes()) {
			t.Fatalf("unexpected g2 multiexp zero result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
			int64(0),
			int64(1),
		)
		if err != nil {
			t.Fatalf("unexpected g2 multiexp zero invalid-point error: %v", err)
		}
		gotG2Zero, popErr = res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g2 multiexp zero invalid-point result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG2Zero), testBLSG2ZeroBytes()) {
			t.Fatalf("unexpected g2 multiexp zero invalid-point result")
		}

		var wantG2Multi circlbls.G2
		wantG2Multi.SetIdentity()
		var g2m1, g2m2 circlbls.G2
		if err := g2m1.SetBytes(g2a); err != nil {
			t.Fatalf("failed to parse g2 multiexp point 1: %v", err)
		}
		if err := g2m2.SetBytes(g2b); err != nil {
			t.Fatalf("failed to parse g2 multiexp point 2: %v", err)
		}
		var g2term1, g2term2 circlbls.G2
		g2term1.ScalarMult(testBLSScalar(5), &g2m1)
		g2term2.ScalarMult(testBLSScalar(7), &g2m2)
		wantG2Multi.Add(&g2term1, &g2term2)

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			testSliceFromBytes(g2a),
			int64(5),
			testSliceFromBytes(g2b),
			int64(7),
			int64(2),
		)
		if err != nil {
			t.Fatalf("unexpected g2 multiexp error: %v", err)
		}
		gotG2Multi, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop g2 multiexp result: %v", popErr)
		}
		if !bytes.Equal(testSliceToBytes(t, gotG2Multi), wantG2Multi.BytesCompressed()) {
			t.Fatalf("unexpected g2 multiexp result")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_ISZERO().Serialize()),
			testSliceFromBytes(testBLSG2ZeroBytes()),
		)
		if err != nil {
			t.Fatalf("unexpected g2 iszero error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop g2 iszero result: %v", popErr)
		}
		if !ok {
			t.Fatalf("expected g2 iszero to return true")
		}

		stack, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
		)
		if err != nil {
			t.Fatalf("unexpected g2 ingroup error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop g2 ingroup result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected invalid g2 encoding to be out of group")
		}
		_ = stack
	})

	t.Run("MapToG1AndG2", func(t *testing.T) {
		fp := testBLSFPBytes(7)
		fp2 := testBLSFP2Bytes(11)

		_, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			testSliceFromBytes(fp),
		)
		if err != nil {
			t.Fatalf("unexpected map_to_g1 error: %v", err)
		}
		g1Sl, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop map_to_g1 result: %v", popErr)
		}
		g1Bytes := testSliceToBytes(t, g1Sl)
		var g1 circlbls.G1
		if err := g1.SetBytes(g1Bytes); err != nil {
			t.Fatalf("map_to_g1 returned invalid point: %v", err)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			testSliceFromBytes(fp2),
		)
		if err != nil {
			t.Fatalf("unexpected map_to_g2 error: %v", err)
		}
		g2Sl, popErr := res.Stack.PopSlice()
		if popErr != nil {
			t.Fatalf("failed to pop map_to_g2 result: %v", popErr)
		}
		g2Bytes := testSliceToBytes(t, g2Sl)
		var g2 circlbls.G2
		if err := g2.SetBytes(g2Bytes); err != nil {
			t.Fatalf("map_to_g2 returned invalid point: %v", err)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			testSliceFromBytes(fp[:47]),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("unexpected map_to_g1 underflow exit code: %d", code)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			testSliceFromBytes(fp2[:95]),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("unexpected map_to_g2 underflow exit code: %d", code)
		}
	})

	t.Run("Pairing", func(t *testing.T) {
		g1 := testBLSG1BytesForScalar(1)
		g2 := testBLSG2BytesForScalar(1)

		_, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			testSliceFromBytes(g1),
			testSliceFromBytes(g2),
			int64(1),
		)
		if err != nil {
			t.Fatalf("unexpected pairing error: %v", err)
		}
		ok, popErr := res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop single pairing result: %v", popErr)
		}
		if ok {
			t.Fatalf("expected single non-trivial pairing to be false")
		}

		var negG2 circlbls.G2
		if err := negG2.SetBytes(g2); err != nil {
			t.Fatalf("failed to parse g2 for negation: %v", err)
		}
		negG2.Neg()

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			testSliceFromBytes(g1),
			testSliceFromBytes(g2),
			testSliceFromBytes(g1),
			testSliceFromBytes(negG2.BytesCompressed()),
			int64(2),
		)
		if err != nil {
			t.Fatalf("unexpected pairing cancellation error: %v", err)
		}
		ok, popErr = res.Stack.PopBool()
		if popErr != nil {
			t.Fatalf("failed to pop pairing cancellation result: %v", popErr)
		}
		if !ok {
			t.Fatalf("expected cancelling pairings to evaluate to true")
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			testSliceFromBytes(g2),
			int64(1),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected invalid pairing exit code: %d", code)
		}
	})

	t.Run("MalformedArithmeticReturnsUnknown", func(t *testing.T) {
		_, res, err := runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			testSliceFromBytes(testBLSG1BytesForScalar(1)),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g1 add exit code: %d", code)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
			int64(1),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed aggregate exit code: %d", code)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			testSliceFromBytes(testInvalidBLSG1Bytes(t)),
			int64(0),
			testSliceFromBytes(testBLSG1BytesForScalar(1)),
			int64(1),
			int64(2),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g1 multiexp exit code: %d", code)
		}

		_, res, err = runRawCode(
			codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			testSliceFromBytes(testInvalidBLSG2Bytes(t)),
			int64(0),
			testSliceFromBytes(testBLSG2BytesForScalar(1)),
			int64(1),
			int64(2),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected malformed g2 multiexp exit code: %d", code)
		}
	})
}
