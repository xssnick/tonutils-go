package funcs

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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
		if got := parsed.ComputeGasPrice(3); got.Int64() != 3 {
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
		if err := cfg.Set(3, ddGas.Copy()); err != nil {
			t.Fatalf("failed to set workchain gas config: %v", err)
		}
		st := newFuncTestState(t, map[int]any{
			9:                      "bad-config-root",
			paramIdxUnpackedConfig: cfg,
		})
		got, err := getTonGasPrices(st, false)
		if err != nil || got.GasLimit != 100 {
			t.Fatalf("getTonGasPrices(workchain) = (%+v, %v)", got, err)
		}
	})

	t.Run("StorageAndExtraBalance", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{
			paramIdxUnpackedConfig: tuple.NewTupleValue(nil),
		})
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
			7: tuple.NewTupleValue(big.NewInt(1000), nil),
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
			7: tuple.NewTupleValue(big.NewInt(1000), big.NewInt(2)),
		})
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err == nil {
			t.Fatal("GETEXTRABALANCE should reject non-cell dict roots")
		}

		largeDictEntries := make(map[uint32]uint64, 128)
		for i := uint32(0); i < 128; i++ {
			largeDictEntries[i] = uint64(i + 1000)
		}
		st = newFuncTestState(t, map[int]any{
			7: tuple.NewTupleValue(big.NewInt(1000), makeExtraBalanceDict(t, largeDictEntries)),
		})
		st.Gas = vm.GasWithLimit(vm.GetExtraBalanceCheapMaxGas + 50)
		if err := st.Stack.PushInt(big.NewInt(127)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err != nil {
			t.Fatalf("GETEXTRABALANCE cheap lookup failed: %v", err)
		}
		if st.Gas.FreeConsumed == 0 {
			t.Fatal("GETEXTRABALANCE cheap lookup should move excess lookup gas to free gas")
		}
		if got, err := st.Stack.PopIntFinite(); err != nil || got.Uint64() != 1127 {
			t.Fatalf("GETEXTRABALANCE cheap lookup = (%v, %v)", got, err)
		}

		malformedDict := cell.NewDict(32)
		if err := malformedDict.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(1, 5).EndCell()); err != nil {
			t.Fatalf("failed to build malformed extra balance dict: %v", err)
		}
		st = newFuncTestState(t, map[int]any{
			7: tuple.NewTupleValue(big.NewInt(1000), malformedDict.AsCell()),
		})
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		err := GETEXTRABALANCE().Interpret(st)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
			t.Fatalf("GETEXTRABALANCE malformed value should be cell underflow, got %v", err)
		}

		st = newFuncTestState(t, map[int]any{
			7: tuple.NewTupleValue(big.NewInt(1000), makeExtraBalanceDict(t, largeDictEntries)),
		})
		st.Gas = vm.GasWithLimit(vm.GetExtraBalanceCheapMaxGas + 50)
		st.GetExtraBalanceCounter = vm.GetExtraBalanceCheapCount
		if err := st.Stack.PushInt(big.NewInt(127)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err == nil {
			t.Fatal("GETEXTRABALANCE non-cheap lookup should exhaust the small gas limit")
		}

		if _, err := parseTonStoragePrices(cell.BeginCell().MustStoreUInt(0xCC, 8).ToSlice()); err == nil {
			t.Fatal("truncated storage config should fail")
		}
		if _, err := parseTonMsgPrices(cell.BeginCell().MustStoreUInt(0xEA, 8).ToSlice()); err == nil {
			t.Fatal("truncated msg config should fail")
		}

		msgCfg := cell.BeginCell().
			MustStoreUInt(0xEA, 8).
			MustStoreUInt(11, 64).
			MustStoreUInt(12, 64).
			MustStoreUInt(13, 64).
			MustStoreUInt(14, 32).
			MustStoreUInt(15, 16).
			MustStoreUInt(16, 16).
			EndCell()
		cfg := tuple.NewTupleSized(5)
		if err := cfg.Set(4, msgCfg.MustBeginParse()); err != nil {
			t.Fatalf("failed to set masterchain msg config: %v", err)
		}
		st = newFuncTestState(t, map[int]any{
			9:                      "bad-config-root",
			paramIdxUnpackedConfig: cfg,
		})
		gotMsg, err := getTonMsgPrices(st, true)
		if err != nil || gotMsg.LumpPrice != 11 {
			t.Fatalf("getTonMsgPrices(masterchain) = (%+v, %v)", gotMsg, err)
		}

		storageCfg := cell.BeginCell().
			MustStoreUInt(0xCC, 8).
			MustStoreUInt(17, 32).
			MustStoreUInt(18, 64).
			MustStoreUInt(19, 64).
			MustStoreUInt(20, 64).
			MustStoreUInt(21, 64).
			EndCell()
		st = newFuncTestState(t, map[int]any{
			9:                      "bad-config-root",
			paramIdxUnpackedConfig: tuple.NewTupleValue(storageCfg.MustBeginParse()),
		})
		gotStorage, err := getTonStoragePrices(st)
		if err != nil || gotStorage.ValidSince != 17 {
			t.Fatalf("getTonStoragePrices = (%+v, %v)", gotStorage, err)
		}

		st = newFuncTestState(t, map[int]any{
			paramIdxUnpackedConfig: tuple.NewTupleValue(cell.BeginCell().MustStoreUInt(0x03, 8).ToSlice()),
		})
		if _, err := getTonStoragePrices(st); err == nil {
			t.Fatal("getTonStoragePrices should reject invalid unpacked config")
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
		if st.Stack.Len() != 1 {
			t.Fatalf("invalid dynamic HASHEXT should leave items on stack after popping hash id and count, got depth %d", st.Stack.Len())
		}
		if _, err := st.Stack.PopSlice(); err != nil {
			t.Fatalf("invalid dynamic HASHEXT left unexpected stack item: %v", err)
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
		if st.Stack.Len() != 0 {
			t.Fatalf("HASHEXT type error should pop all hash items, got depth %d", st.Stack.Len())
		}

		st = newFuncTestState(t, nil)
		builder := cell.BeginCell()
		unaligned := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
		if err := st.Stack.PushBuilder(builder); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushSlice(unaligned); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := HASHEXT(1 << 9).Interpret(st); err == nil {
			t.Fatal("HASHEXTA should reject unaligned input")
		}
		if st.Stack.Len() != 1 {
			t.Fatalf("HASHEXTA byte-alignment error should leave append builder, got depth %d", st.Stack.Len())
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("HASHEXTA byte-alignment error left unexpected stack item: %v", err)
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

func TestFeeOpsUnderflowPrecheckStartsAtV9(t *testing.T) {
	st := &vm.State{
		GlobalVersion: 8,
		Stack:         vm.NewStack(),
	}
	if err := st.Stack.PushBool(false); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	err := GETFORWARDFEESIMPLE().Interpret(st)
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeStackUnderflow {
		t.Fatalf("v8 GETFORWARDFEESIMPLE error = %v, want stack underflow", err)
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("v8 GETFORWARDFEESIMPLE stack len = %d, want partial pop", st.Stack.Len())
	}

	st = &vm.State{
		GlobalVersion: 9,
		Stack:         vm.NewStack(),
	}
	if err = st.Stack.PushBool(false); err != nil {
		t.Fatalf("PushBool failed: %v", err)
	}
	err = GETFORWARDFEESIMPLE().Interpret(st)
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeStackUnderflow {
		t.Fatalf("v9 GETFORWARDFEESIMPLE error = %v, want stack underflow", err)
	}
	if st.Stack.Len() != 1 {
		t.Fatalf("v9 GETFORWARDFEESIMPLE stack len = %d, want precheck to keep stack", st.Stack.Len())
	}
}

func TestHashExtUnderflowPrecheckStartsAtV9(t *testing.T) {
	st := &vm.State{
		GlobalVersion: 8,
		Stack:         vm.NewStack(),
	}
	if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt hash id failed: %v", err)
	}
	err := HASHEXT(255).Interpret(st)
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeStackUnderflow {
		t.Fatalf("v8 dynamic HASHEXT error = %v, want stack underflow", err)
	}
	if st.Stack.Len() != 0 {
		t.Fatalf("v8 dynamic HASHEXT stack len = %d, want partial pop", st.Stack.Len())
	}

	st = &vm.State{
		GlobalVersion: 9,
		Stack:         vm.NewStack(),
	}
	if err = st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt hash id failed: %v", err)
	}
	err = HASHEXT(255).Interpret(st)
	if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeStackUnderflow {
		t.Fatalf("v9 dynamic HASHEXT error = %v, want stack underflow", err)
	}
	if st.Stack.Len() != 1 {
		t.Fatalf("v9 dynamic HASHEXT stack len = %d, want precheck to keep stack", st.Stack.Len())
	}
}
