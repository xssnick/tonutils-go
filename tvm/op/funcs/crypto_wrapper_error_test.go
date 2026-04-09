package funcs

import (
	"bytes"
	"math/big"
	"testing"

	circlbls "github.com/cloudflare/circl/ecc/bls12381"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestCryptoWrapperAdditionalPaths(t *testing.T) {
	msg := []byte("crypto-wrapper-message")
	sk := blsScalarFromInt(big.NewInt(3))
	hashPoint := blsHashToG2(msg)

	var pub circlbls.G1
	pub.ScalarMult(sk, circlbls.G1Generator())

	var sig circlbls.G2
	sig.ScalarMult(sk, hashPoint)

	invalidG1 := bytes.Repeat([]byte{0xFF}, 48)
	if _, err := parseBLSG1(invalidG1); err == nil {
		t.Fatal("expected invalid compressed G1 test vector")
	}
	invalidG2 := bytes.Repeat([]byte{0xFF}, 96)
	if _, err := parseBLSG2(invalidG2); err == nil {
		t.Fatal("expected invalid compressed G2 test vector")
	}

	t.Run("VerifyAndAggregateFailures", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG1, 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_VERIFY().Interpret(st); err != nil {
			t.Fatalf("BLS_VERIFY(invalid pub) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_VERIFY(invalid pub) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_VERIFY().Interpret(st); err == nil {
			t.Fatal("BLS_VERIFY should reject short signatures")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG2, 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_AGGREGATE().Interpret(st); err == nil {
			t.Fatal("BLS_AGGREGATE should reject invalid signatures")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_FASTAGGREGATEVERIFY().Interpret(st); err != nil {
			t.Fatalf("BLS_FASTAGGREGATEVERIFY(empty) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_FASTAGGREGATEVERIFY(empty) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_FASTAGGREGATEVERIFY().Interpret(st); err == nil {
			t.Fatal("BLS_FASTAGGREGATEVERIFY should reject short signatures")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_AGGREGATEVERIFY().Interpret(st); err != nil {
			t.Fatalf("BLS_AGGREGATEVERIFY(empty) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_AGGREGATEVERIFY(empty) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_AGGREGATEVERIFY().Interpret(st); err == nil {
			t.Fatal("BLS_AGGREGATEVERIFY should reject short signatures")
		}
	})

	t.Run("G1Wrappers", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG1, 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_ADD().Interpret(st); err == nil {
			t.Fatal("BLS_G1_ADD should reject invalid points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG1, 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_NEG().Interpret(st); err == nil {
			t.Fatal("BLS_G1_NEG should reject invalid points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG1, 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MUL().Interpret(st); err == nil {
			t.Fatal("BLS_G1_MUL should reject invalid points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_MAP_TO_G1().Interpret(st); err == nil {
			t.Fatal("BLS_MAP_TO_G1 should reject short field inputs")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG1, 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_INGROUP().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_INGROUP(invalid) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_G1_INGROUP(invalid) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_ISZERO().Interpret(st); err != nil {
			t.Fatalf("BLS_G1_ISZERO(nonzero) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_G1_ISZERO(nonzero) = (%v, %v), want false", ok, err)
		}
	})

	t.Run("G2Wrappers", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG2, 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_ADD().Interpret(st); err == nil {
			t.Fatal("BLS_G2_ADD should reject invalid points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG2, 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_NEG().Interpret(st); err == nil {
			t.Fatal("BLS_G2_NEG should reject invalid points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG2, 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MUL().Interpret(st); err == nil {
			t.Fatal("BLS_G2_MUL should reject invalid points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_MAP_TO_G2().Interpret(st); err == nil {
			t.Fatal("BLS_MAP_TO_G2 should reject short field inputs")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG2, 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_INGROUP().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_INGROUP(invalid) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_G2_INGROUP(invalid) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_ISZERO().Interpret(st); err != nil {
			t.Fatalf("BLS_G2_ISZERO(nonzero) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_G2_ISZERO(nonzero) = (%v, %v), want false", ok, err)
		}
	})

	t.Run("PairingFalseAndError", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_PAIRING().Interpret(st); err != nil {
			t.Fatalf("BLS_PAIRING(non-identity) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("BLS_PAIRING(non-identity) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(invalidG1, 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_PAIRING().Interpret(st); err == nil {
			t.Fatal("BLS_PAIRING should reject invalid points")
		}
	})
}
