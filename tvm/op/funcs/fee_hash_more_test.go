package funcs

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func makeFeeHashMoreState(t *testing.T) *vm.State {
	t.Helper()

	cfg := tuple.NewTupleSized(7)
	storageSlice := cell.BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreUInt(1, 32).
		MustStoreUInt(2, 64).
		MustStoreUInt(3, 64).
		MustStoreUInt(8, 64).
		MustStoreUInt(13, 64).
		ToSlice()
	msgSliceMC := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(1, 64).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreUInt(0, 16).
		MustStoreUInt(0, 16).
		ToSlice()
	msgSliceWC := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(7, 64).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(2<<16, 64).
		MustStoreUInt(11, 32).
		MustStoreUInt(3, 16).
		MustStoreUInt(4, 16).
		ToSlice()

	if err := cfg.Set(0, storageSlice); err != nil {
		t.Fatalf("failed to set storage config: %v", err)
	}
	if err := cfg.Set(4, msgSliceMC); err != nil {
		t.Fatalf("failed to set masterchain msg config: %v", err)
	}
	if err := cfg.Set(5, msgSliceWC); err != nil {
		t.Fatalf("failed to set workchain msg config: %v", err)
	}

	st := newFuncTestState(t, map[int]any{
		paramIdxUnpackedConfig: cfg,
		7:                      *tuple.NewTuple(big.NewInt(0), makeExtraBalanceDict(t, map[uint32]uint64{1: 9})),
	})
	st.InitForExecution()
	return st
}

func TestFeeHashMoreBranches(t *testing.T) {
	t.Run("hashext serialization and stack discipline", func(t *testing.T) {
		op := HASHEXT(1<<9 | 1<<8 | 255)
		if got := op.SerializeText(); got != "HASHEXTAR -1" {
			t.Fatalf("unexpected HASHEXT text: %q", got)
		}

		decoded := HASHEXT(0)
		if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
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
		wantStorage := (&tonStoragePrices{
			ValidSince:  1,
			BitPrice:    2,
			CellPrice:   3,
			MCBitPrice:  8,
			MCCellPrice: 13,
		}).computeStorageFee(true, 7, 5, 3)
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
		wantForward := (&tonMsgPrices{
			LumpPrice: 1,
			BitPrice:  0,
			CellPrice: 0,
		}).computeForwardFee(2, 9)
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
