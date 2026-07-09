package funcs

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func makeFeeHashMoreState(t *testing.T) *vm.State {
	t.Helper()

	storageCell := cell.BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreUInt(1, 32).
		MustStoreUInt(2, 64).
		MustStoreUInt(3, 64).
		MustStoreUInt(8, 64).
		MustStoreUInt(13, 64).
		EndCell()
	msgCellMC := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(1, 64).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreUInt(0, 16).
		MustStoreUInt(0, 16).
		EndCell()
	msgCellWC := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(7, 64).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(2<<16, 64).
		MustStoreUInt(11, 32).
		MustStoreUInt(3, 16).
		MustStoreUInt(4, 16).
		EndCell()

	st := newFuncTestState(t, map[int]any{
		9: makeConfigRootRefDict(t, map[uint32]*cell.Cell{
			18: makeStoragePricesConfigDict(t, map[uint32]*cell.Cell{1: storageCell}),
			24: msgCellMC,
			25: msgCellWC,
		}),
		paramIdxUnpackedConfig: func() tuple.Tuple {
			cfg := tuple.NewTupleSized(6)
			if err := cfg.Set(0, storageCell.MustBeginParse()); err != nil {
				t.Fatalf("failed to set unpacked storage config: %v", err)
			}
			if err := cfg.Set(4, msgCellMC.MustBeginParse()); err != nil {
				t.Fatalf("failed to set unpacked masterchain msg config: %v", err)
			}
			if err := cfg.Set(5, msgCellWC.MustBeginParse()); err != nil {
				t.Fatalf("failed to set unpacked workchain msg config: %v", err)
			}
			return cfg
		}(),
		7: tuple.NewTupleValue(big.NewInt(0), makeExtraBalanceDict(t, map[uint32]uint64{1: 9})),
	})
	st.InitForExecution()
	return st
}

func makeStoragePricesConfigDict(t *testing.T, entries map[uint32]*cell.Cell) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	for validSince, value := range entries {
		if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(validSince)), value); err != nil {
			t.Fatalf("failed to set storage prices entry %d: %v", validSince, err)
		}
	}

	return dict.AsCell()
}

func makeDeepExtraBalanceDict(t *testing.T, salt uint32) *cell.Cell {
	t.Helper()

	entries := map[uint32]uint64{
		0: uint64(1000 + salt),
	}
	for i := 0; i < 16; i++ {
		entries[uint32(1)<<uint(31-i)] = uint64(2000 + salt + uint32(i))
	}
	return makeExtraBalanceDict(t, entries)
}

func setExtraBalanceParam(t *testing.T, st *vm.State, dict *cell.Cell) {
	t.Helper()

	params := tuple.NewTupleSized(8)
	if err := params.Set(7, tuple.NewTupleValue(big.NewInt(0), dict)); err != nil {
		t.Fatalf("failed to set balance param: %v", err)
	}
	c7 := tuple.NewTupleSized(1)
	if err := c7.Set(0, params); err != nil {
		t.Fatalf("failed to set c7 params: %v", err)
	}
	if err := st.SetC7(c7); err != nil {
		t.Fatalf("failed to bind c7: %v", err)
	}
}

func runExtraBalanceGasDelta(t *testing.T, st *vm.State, salt uint32) int64 {
	t.Helper()

	setExtraBalanceParam(t, st, makeDeepExtraBalanceDict(t, salt))
	before := st.Gas.Used()
	if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt extra balance id failed: %v", err)
	}
	if err := GETEXTRABALANCE().Interpret(st); err != nil {
		t.Fatalf("GETEXTRABALANCE failed: %v", err)
	}
	got, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite extra balance failed: %v", err)
	}
	if got.Uint64() != uint64(1000+salt) {
		t.Fatalf("unexpected extra balance: got %s want %d", got.String(), 1000+salt)
	}
	return st.Gas.Used() - before
}

func TestFeeHashMoreBranches(t *testing.T) {
	t.Run("hashext serialization and stack discipline", func(t *testing.T) {
		op := HASHEXT(1<<9 | 1<<8 | 255)
		if got := op.SerializeText(); got != "HASHEXTAR -1" {
			t.Fatalf("unexpected HASHEXT text: %q", got)
		}

		decoded := HASHEXT(0)
		if err := decoded.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("HASHEXT deserialize failed: %v", err)
		}
		if got := decoded.SerializeText(); got != "HASHEXTAR -1" {
			t.Fatalf("unexpected decoded HASHEXT text: %q", got)
		}

		st := newFuncTestState(t, nil)
		if err := HASHEXT(255).Interpret(st); err == nil {
			t.Fatal("dynamic HASHEXT should fail when the hash id is missing")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt hash id failed: %v", err)
		}
		if err := HASHEXT(255).Interpret(st); err == nil {
			t.Fatal("dynamic HASHEXT should fail when the count is missing")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		if err := HASHEXT(1 << 9).Interpret(st); err == nil {
			t.Fatal("HASHEXTA should fail when the output builder is missing")
		}

		st = newFuncTestState(t, nil)
		st.Gas = vm.GasWithLimit(1, 1)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice[0] failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice[1] failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		if err := HASHEXT(0).Interpret(st); err == nil {
			t.Fatal("HASHEXT should propagate gas exhaustion")
		}
	})

	t.Run("hashext error timing preserves operands like cpp", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(77)); err != nil {
			t.Fatalf("PushInt marker failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(123)); err != nil {
			t.Fatalf("PushInt bad item failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		beforeGas := st.Gas.Used()
		if err := HASHEXT(1 << 9).Interpret(st); err == nil {
			t.Fatal("HASHEXTA should reject a non-slice item")
		}
		if delta := st.Gas.Used() - beforeGas; delta != vm.HashExtEntryGasPrice {
			t.Fatalf("unexpected gas before type error: got %d want %d", delta, vm.HashExtEntryGasPrice)
		}
		if st.Stack.Len() != 2 {
			t.Fatalf("expected marker and append builder after type error, got %d values", st.Stack.Len())
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("append builder should remain on type error: %v", err)
		}
		if marker, err := st.Stack.PopIntFinite(); err != nil || marker.Int64() != 77 {
			t.Fatalf("unexpected marker after type error: %v, %v", marker, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(88)); err != nil {
			t.Fatalf("PushInt marker failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		if err := HASHEXT(1 << 9).Interpret(st); err == nil {
			t.Fatal("HASHEXTA should reject non-byte-aligned total input")
		}
		if st.Stack.Len() != 2 {
			t.Fatalf("expected marker and append builder after byte error, got %d values", st.Stack.Len())
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("append builder should remain on byte error: %v", err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt dynamic hash id failed: %v", err)
		}
		if err := HASHEXT(255).Interpret(st); err == nil {
			t.Fatal("dynamic HASHEXT should fail before popping a lone hash id")
		}
		if st.Stack.Len() != 1 {
			t.Fatalf("dynamic HASHEXT underflow popped hash id, stack len %d", st.Stack.Len())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("PushInt marker failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(42)); err != nil {
			t.Fatalf("PushInt hash id failed: %v", err)
		}
		if err := HASHEXT(255).Interpret(st); err == nil {
			t.Fatal("dynamic HASHEXT should reject unknown hash ids before popping items")
		}
		if st.Stack.Len() != 2 {
			t.Fatalf("expected marker and item after hash-id error, got %d values", st.Stack.Len())
		}
		if _, err := st.Stack.PopSlice(); err != nil {
			t.Fatalf("hash-id error should leave item on stack: %v", err)
		}
	})

	t.Run("getextrabalance cheap gas and child counter", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		for i := uint32(0); i < vm.GetExtraBalanceCheapCount; i++ {
			if delta := runExtraBalanceGasDelta(t, st, i); delta != vm.GetExtraBalanceCheapMaxGas {
				t.Fatalf("cheap call %d gas = %d, want %d", i+1, delta, vm.GetExtraBalanceCheapMaxGas)
			}
		}
		if st.Gas.FreeConsumed == 0 {
			t.Fatal("expected cheap GETEXTRABALANCE to defer excess cell-load gas")
		}
		if delta := runExtraBalanceGasDelta(t, st, 100); delta <= vm.GetExtraBalanceCheapMaxGas {
			t.Fatalf("sixth GETEXTRABALANCE should use normal gas, got %d", delta)
		}

		parent := newFuncTestState(t, nil)
		parent.SetChildRunner(func(child *vm.State) (int64, error) {
			setExtraBalanceParam(t, child, makeDeepExtraBalanceDict(t, 200))
			if err := child.Stack.PushInt(big.NewInt(0)); err != nil {
				return 0, err
			}
			if err := GETEXTRABALANCE().Interpret(child); err != nil {
				return 0, err
			}
			_, err := child.Stack.PopIntFinite()
			return 0, err
		})
		child := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.NewGas(), nil, tuple.Tuple{}, vm.NewStack())
		exitCode, err := parent.RunChild(child)
		if err != nil || exitCode != 0 {
			t.Fatalf("child GETEXTRABALANCE run = (%d, %v)", exitCode, err)
		}
		for i := uint32(0); i < vm.GetExtraBalanceCheapCount-1; i++ {
			if delta := runExtraBalanceGasDelta(t, parent, 300+i); delta != vm.GetExtraBalanceCheapMaxGas {
				t.Fatalf("post-child cheap call %d gas = %d, want %d", i+1, delta, vm.GetExtraBalanceCheapMaxGas)
			}
		}
		if delta := runExtraBalanceGasDelta(t, parent, 400); delta <= vm.GetExtraBalanceCheapMaxGas {
			t.Fatalf("child call did not advance parent cheap counter, next gas %d", delta)
		}
	})

	t.Run("masterchain fee ops use the right config entries", func(t *testing.T) {
		st := makeFeeHashMoreState(t)
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("PushInt cells failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(5)); err != nil {
			t.Fatalf("PushInt bits failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("PushInt delta failed: %v", err)
		}
		if err := st.Stack.PushBool(true); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETSTORAGEFEE().Interpret(st); err != nil {
			t.Fatalf("GETSTORAGEFEE(masterchain) failed: %v", err)
		}
		gotStorage, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		wantStorage := (&tlb.ConfigStoragePrices{
			ValidSince:  1,
			BitPrice:    2,
			CellPrice:   3,
			MCBitPrice:  8,
			MCCellPrice: 13,
		}).ComputeStorageFee(true, 7, 5, 3)
		if gotStorage.Cmp(wantStorage) != 0 {
			t.Fatalf("unexpected masterchain storage fee: want %v, got %v", wantStorage, gotStorage)
		}

		st = makeFeeHashMoreState(t)
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt cells failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(9)); err != nil {
			t.Fatalf("PushInt bits failed: %v", err)
		}
		if err := st.Stack.PushBool(true); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETFORWARDFEE().Interpret(st); err != nil {
			t.Fatalf("GETFORWARDFEE(masterchain) failed: %v", err)
		}
		gotForward, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		wantForward := (&tlb.ConfigMsgForwardPrices{
			LumpPrice: 1,
			BitPrice:  0,
			CellPrice: 0,
		}).ComputeForwardFee(2, 9)
		if gotForward.Cmp(wantForward) != 0 {
			t.Fatalf("unexpected masterchain forward fee: want %v, got %v", wantForward, gotForward)
		}

		st = makeFeeHashMoreState(t)
		if err := st.Stack.PushInt(big.NewInt(1234)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushBool(true); err != nil {
			t.Fatalf("PushBool failed: %v", err)
		}
		if err := GETORIGINALFWDFEE().Interpret(st); err != nil {
			t.Fatalf("GETORIGINALFWDFEE(masterchain) failed: %v", err)
		}
		gotOriginal, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if gotOriginal.Cmp(big.NewInt(1234)) != 0 {
			t.Fatalf("unexpected original forward fee: want 1234, got %v", gotOriginal)
		}
	})

	t.Run("hashext append mode handles dynamic ids and misaligned inputs", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		if err := HASHEXT(1 << 9).Interpret(st); err == nil {
			t.Fatal("HASHEXTA should reject non-byte-aligned input")
		}

		outBuilder := cell.BeginCell().MustStoreUInt(0x11, 8)
		inBuilder := cell.BeginCell().MustStoreUInt(0xABCD, 16)
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(outBuilder); err != nil {
			t.Fatalf("PushBuilder(out) failed: %v", err)
		}
		if err := st.Stack.PushBuilder(inBuilder); err != nil {
			t.Fatalf("PushBuilder(in) failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt count failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt hash id failed: %v", err)
		}
		if err := HASHEXT(1<<9 | 255).Interpret(st); err != nil {
			t.Fatalf("dynamic HASHEXTA failed: %v", err)
		}
		gotBuilder, err := st.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}
		wantHash := sha256.Sum256([]byte{0xAB, 0xCD})
		wantBytes := append([]byte{0x11}, wantHash[:]...)
		if got := mustSliceData(t, gotBuilder.ToSlice()); !bytes.Equal(got, wantBytes) {
			t.Fatalf("unexpected dynamic HASHEXTA output:\nwant %x\ngot  %x", wantBytes, got)
		}
	})

	t.Run("range helpers reject invalid inputs", func(t *testing.T) {
		if parsed, err := parseTonGasPrices(nil); err == nil || parsed != nil {
			t.Fatalf("parseTonGasPrices(nil) = (%v, %v), want error", parsed, err)
		}

		st := newFuncTestState(t, nil)
		if err := GETSTORAGEFEE().Interpret(st); err == nil {
			t.Fatal("GETSTORAGEFEE should fail when the stack is empty")
		}

		st = makeFeeHashMoreState(t)
		if err := st.Stack.PushInt(new(big.Int).Lsh(big.NewInt(1), 40)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETEXTRABALANCE().Interpret(st); err == nil {
			t.Fatal("GETEXTRABALANCE should reject 32-bit overflows")
		}

		stack := vm.NewStack()
		if err := stack.PushInt(new(big.Int).Lsh(big.NewInt(1), 63)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if _, err := popUint64NonNegative(stack); err == nil {
			t.Fatal("popUint64NonNegative should reject values outside uint63")
		}
	})
}
