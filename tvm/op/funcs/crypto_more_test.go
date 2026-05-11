package funcs

import (
	"math/big"
	"testing"

	circlbls "github.com/cloudflare/circl/ecc/bls12381"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestCryptoMoreEdges(t *testing.T) {
	msg := []byte("crypto-more-message")
	sk1 := blsScalarFromInt(big.NewInt(3))
	sk2 := blsScalarFromInt(big.NewInt(7))
	hashPoint := blsHashToG2(msg)

	var pub1, pub2 circlbls.G1
	pub1.ScalarMult(sk1, circlbls.G1Generator())
	pub2.ScalarMult(sk2, circlbls.G1Generator())

	var sig1, sig2 circlbls.G2
	sig1.ScalarMult(sk1, hashPoint)
	sig2.ScalarMult(sk2, hashPoint)

	aggSig := sig1
	aggSig.Add(&aggSig, &sig2)

	t.Run("ristretto quiet and non-quiet ops reject malformed inputs", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_ADD().Interpret(st); err == nil {
			t.Fatal("RIST255_ADD should reject malformed points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RIST255_QADD().Interpret(st); err != nil {
			t.Fatalf("RIST255_QADD failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("RIST255_QADD = (%v, %v), want false", ok, err)
		}
	})

	t.Run("bls verify wrappers reject malformed public inputs", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_VERIFY().Interpret(st); err == nil {
			t.Fatal("BLS_VERIFY should reject short public keys")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(aggSig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_FASTAGGREGATEVERIFY().Interpret(st); err == nil {
			t.Fatal("BLS_FASTAGGREGATEVERIFY should reject short public keys")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(msg, uint(len(msg))*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(aggSig.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_AGGREGATEVERIFY().Interpret(st); err == nil {
			t.Fatal("BLS_AGGREGATEVERIFY should reject short public keys")
		}
	})

	t.Run("bls binary and group-check ops reject alternate malformed inputs", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(pub1.BytesCompressed(), 48*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_ADD().Interpret(st); err == nil {
			t.Fatal("BLS_G1_ADD should reject malformed second operands")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(sig1.BytesCompressed(), 96*8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_ADD().Interpret(st); err == nil {
			t.Fatal("BLS_G2_ADD should reject malformed second operands")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_INGROUP().Interpret(st); err == nil {
			t.Fatal("BLS_G1_INGROUP should reject short slices")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_INGROUP().Interpret(st); err == nil {
			t.Fatal("BLS_G2_INGROUP should reject short slices")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G1_ISZERO().Interpret(st); err == nil {
			t.Fatal("BLS_G1_ISZERO should reject short slices")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := BLS_G2_ISZERO().Interpret(st); err == nil {
			t.Fatal("BLS_G2_ISZERO should reject short slices")
		}
	})

	t.Run("bls multiexp wrappers reject malformed single-point inputs", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G1_MULTIEXP().Interpret(st); err == nil {
			t.Fatal("BLS_G1_MULTIEXP should reject malformed points")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := BLS_G2_MULTIEXP().Interpret(st); err == nil {
			t.Fatal("BLS_G2_MULTIEXP should reject malformed points")
		}
	})
}
