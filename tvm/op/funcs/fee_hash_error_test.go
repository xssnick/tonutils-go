package funcs

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestFeeHashAdditionalPaths(t *testing.T) {
	t.Run("GasPriceParsingAndLookup", func(t *testing.T) {
		ddGas := cell.BeginCell().
			MustStoreUInt(0xDD, 8).
			MustStoreUInt(1<<16, 64).
			MustStoreUInt(100, 64).
			MustStoreUInt(7, 64).
			MustStoreUInt(1000, 64).
			MustStoreUInt(11, 64).
			MustStoreUInt(12, 64).
			ToSlice()
		parsed, err := parseTonGasPrices(ddGas)
		if err != nil {
			t.Fatalf("parseTonGasPrices(DD) failed: %v", err)
		}
		if parsed.GasLimit != 100 || parsed.SpecialGasLimit != 100 || parsed.GasCredit != 7 {
			t.Fatalf("unexpected DD gas config: %+v", parsed)
		}
		if got := parsed.computeGasPrice(3); got.Int64() != 3 {
			t.Fatalf("unexpected DD gas price: %v", got)
		}

		if _, err := parseTonGasPrices(cell.BeginCell().MustStoreUInt(0xD1, 8).MustStoreUInt(1, 64).ToSlice()); err == nil {
			t.Fatal("truncated D1 gas config should fail")
		}
		if _, err := parseTonGasPrices(cell.BeginCell().
			MustStoreUInt(0xD1, 8).
			MustStoreUInt(1, 64).
			MustStoreUInt(2, 64).
			MustStoreUInt(0xAA, 8).
			ToSlice()); err == nil {
			t.Fatal("invalid main gas tag after D1 should fail")
		}

		cfg := tuple.NewTupleSized(4)
		if err := cfg.Set(3, ddGas); err != nil {
			t.Fatalf("failed to set workchain gas config: %v", err)
		}
		st := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})
		got, err := getTonGasPrices(st, false)
		if err != nil || got.GasLimit != 100 {
			t.Fatalf("getTonGasPrices(workchain) = (%+v, %v)", got, err)
		}
	})

	t.Run("StorageAndExtraBalance", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: *tuple.NewTuple(nil)})
		if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
			t.Fatalf("PushInt(cells) failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(20)); err != nil {
			t.Fatalf("PushInt(bits) failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("PushInt(delta) failed: %v", err)
		}
		if err := st.Stack.PushBool(false); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETSTORAGEFEE().Interpret(st); err != nil {
			t.Fatalf("GETSTORAGEFEE(nil prices) failed: %v", err)
		}
		if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
			t.Fatalf("GETSTORAGEFEE(nil prices) = (%v, %v)", got, err)
		}

		st = newFuncTestState(t, map[int]any{
			7: *tuple.NewTuple(big.NewInt(1000), nil),
		})
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err != nil {
			t.Fatalf("GETEXTRABALANCE(nil dict) failed: %v", err)
		}
		if got, err := st.Stack.PopIntFinite(); err != nil || got.Sign() != 0 {
			t.Fatalf("GETEXTRABALANCE(nil dict) = (%v, %v)", got, err)
		}

		st = newFuncTestState(t, map[int]any{7: big.NewInt(1)})
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err == nil {
			t.Fatal("GETEXTRABALANCE should reject non-tuple balances")
		}

		st = newFuncTestState(t, map[int]any{
			7: *tuple.NewTuple(big.NewInt(1000), big.NewInt(2)),
		})
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err == nil {
			t.Fatal("GETEXTRABALANCE should reject non-cell dict roots")
		}

		if _, err := parseTonStoragePrices(cell.BeginCell().MustStoreUInt(0xCC, 8).ToSlice()); err == nil {
			t.Fatal("truncated storage config should fail")
		}
		if _, err := parseTonMsgPrices(cell.BeginCell().MustStoreUInt(0xEA, 8).ToSlice()); err == nil {
			t.Fatal("truncated msg config should fail")
		}

		cfg := tuple.NewTupleSized(5)
		msgCfg := cell.BeginCell().
			MustStoreUInt(0xEA, 8).
			MustStoreUInt(11, 64).
			MustStoreUInt(12, 64).
			MustStoreUInt(13, 64).
			MustStoreUInt(14, 32).
			MustStoreUInt(15, 16).
			MustStoreUInt(16, 16).
			ToSlice()
		if err := cfg.Set(4, msgCfg); err != nil {
			t.Fatalf("failed to set masterchain msg config: %v", err)
		}
		st = newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})
		gotMsg, err := getTonMsgPrices(st, true)
		if err != nil || gotMsg.LumpPrice != 11 {
			t.Fatalf("getTonMsgPrices(masterchain) = (%+v, %v)", gotMsg, err)
		}

		st = newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: *tuple.NewTuple(big.NewInt(1))})
		if _, err := getTonStoragePrices(st); err == nil {
			t.Fatal("getTonStoragePrices should reject non-slice config entries")
		}
	})

	t.Run("HashExtBranches", func(t *testing.T) {
		itemA := cell.BeginCell().MustStoreSlice([]byte("ab"), 16).ToSlice()
		itemB := cell.BeginCell().MustStoreSlice([]byte("cd"), 16).ToSlice()

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(itemA); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(itemB); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := HASHEXT(1 << 8).Interpret(st); err != nil {
			t.Fatalf("HASHEXTR failed: %v", err)
		}
		got, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		want := sha256.Sum256([]byte("cdab"))
		if got.Cmp(new(big.Int).SetBytes(want[:])) != 0 {
			t.Fatalf("unexpected reverse HASHEXT result: want %x, got %x", want[:], got.Bytes())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := HASHEXT(0).Interpret(st); err != nil {
			t.Fatalf("HASHEXT(empty) failed: %v", err)
		}
		got, err = st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		emptyHash := sha256.Sum256(nil)
		if got.Cmp(new(big.Int).SetBytes(emptyHash[:])) != 0 {
			t.Fatalf("unexpected empty HASHEXT result: want %x, got %x", emptyHash[:], got.Bytes())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(itemA); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := HASHEXT(255).Interpret(st); err == nil {
			t.Fatal("HASHEXT should reject unknown dynamic hash ids")
		}

		st = newFuncTestState(t, nil)
		fullBuilder := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)
		if err := st.Stack.PushBuilder(fullBuilder); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushSlice(itemA); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := HASHEXT(1 << 9).Interpret(st); err == nil {
			t.Fatal("HASHEXTA should reject builders without room for the hash")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := HASHEXT(0).Interpret(st); err == nil {
			t.Fatal("HASHEXT should reject non-slice inputs")
		}
	})

	t.Run("FeeOpErrors", func(t *testing.T) {
		st := makeFeeState(t)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushBool(false); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETGASFEE().Interpret(st); err == nil {
			t.Fatal("GETGASFEE should reject negative gas values")
		}

		st = makeFeeState(t)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushBool(false); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETFORWARDFEE().Interpret(st); err == nil {
			t.Fatal("GETFORWARDFEE should reject negative bit counts")
		}

		st = makeFeeState(t)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushBool(false); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETORIGINALFWDFEE().Interpret(st); err == nil {
			t.Fatal("GETORIGINALFWDFEE should reject negative fwd_fee")
		}

		st = makeFeeState(t)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushBool(false); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETGASFEESIMPLE().Interpret(st); err == nil {
			t.Fatal("GETGASFEESIMPLE should reject negative gas values")
		}

		st = makeFeeState(t)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushBool(false); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETFORWARDFEESIMPLE().Interpret(st); err == nil {
			t.Fatal("GETFORWARDFEESIMPLE should reject negative bit counts")
		}
	})
}
