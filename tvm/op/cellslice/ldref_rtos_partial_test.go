package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLDREFRTOSPushesRemainderBeforeChildLoad(t *testing.T) {
	assertRemainder := func(t *testing.T, st *vm.State, bits uint64, bitLen uint) {
		t.Helper()
		if st.Stack.Len() != 1 {
			t.Fatalf("LDREFRTOS failed child load left %d stack values, want remainder slice", st.Stack.Len())
		}
		remainder := popCellSliceSlice(t, st)
		if remainder.BitsLeft() != bitLen || remainder.RefsNum() != 0 {
			t.Fatalf("LDREFRTOS remainder = (%d bits, %d refs), want (%d, 0)", remainder.BitsLeft(), remainder.RefsNum(), bitLen)
		}
		if got := remainder.MustLoadUInt(bitLen); got != bits {
			t.Fatalf("LDREFRTOS remainder bits = %#x, want %#x", got, bits)
		}
	}

	t.Run("MissingLibrary", func(t *testing.T) {
		parent := cell.BeginCell().
			MustStoreUInt(0xA, 4).
			MustStoreRef(mustLibraryCell(t)).
			EndCell()
		st := newCellSliceState()
		pushCellSliceSlice(t, st, parent.MustBeginParse())

		assertCellSliceVMErrorCode(t, LDREFRTOS().Interpret(st), vmerr.CodeCellUnderflow)
		assertRemainder(t, st, 0xA, 4)
	})

	t.Run("VirtualizedPrunedChild", func(t *testing.T) {
		parent, _ := mustVirtualizedProofBodyAndPrunedRef(t)
		st := newCellSliceState()
		pushCellSliceSlice(t, st, parent.MustBeginParse())

		assertCellSliceVMErrorCode(t, LDREFRTOS().Interpret(st), vmerr.CodeVirtualization)
		assertRemainder(t, st, 0, 1)
	})

	t.Run("ChildLoadOutOfGas", func(t *testing.T) {
		child := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		parent := cell.BeginCell().MustStoreUInt(0xC, 4).MustStoreRef(child).EndCell()
		st := newCellSliceState()
		st.Gas = vm.GasWithLimit(0)
		pushCellSliceSlice(t, st, parent.MustBeginParse())

		assertCellSliceVMErrorCode(t, LDREFRTOS().Interpret(st), vmerr.CodeOutOfGas)
		assertRemainder(t, st, 0xC, 4)
	})

	t.Run("PendingGasError", func(t *testing.T) {
		child := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
		parent := cell.BeginCell().MustStoreUInt(0xD, 4).MustStoreRef(child).EndCell()
		st := newCellSliceState()
		st.Gas = vm.GasWithLimit(0)
		assertCellSliceVMErrorCode(t, st.Cells.OnLoadError(parent), vmerr.CodeOutOfGas)
		pushCellSliceSlice(t, st, parent.MustBeginParse())

		assertCellSliceVMErrorCode(t, LDREFRTOS().Interpret(st), vmerr.CodeOutOfGas)
		assertRemainder(t, st, 0xD, 4)
		if st.Cells.IsCellLoaded(child) {
			t.Fatal("LDREFRTOS parsed the child after a pending gas error")
		}
	})
}

func TestLDREFRTOSSuccessPreservesChildTraceAndGas(t *testing.T) {
	loads := 0
	childTrace := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) { loads++ }})
	parentTrace := cell.NewTrace(cell.TraceHooks{
		OnChild: func(int) *cell.Trace { return childTrace },
	})
	child := cell.BeginCell().MustStoreUInt(0xEE, 8).EndCell()
	parent := cell.BeginCell().MustStoreUInt(0xF, 4).MustStoreRef(child).EndCell().WithTrace(parentTrace)
	st := newCellSliceState()
	st.Gas = vm.GasWithLimit(1_000)
	pushCellSliceSlice(t, st, parent.MustBeginParse())

	if err := LDREFRTOS().Interpret(st); err != nil {
		t.Fatalf("LDREFRTOS failed: %v", err)
	}
	loaded := popCellSliceSlice(t, st)
	remainder := popCellSliceSlice(t, st)
	if got := loaded.MustLoadUInt(8); got != 0xEE {
		t.Fatalf("LDREFRTOS child bits = %#x, want 0xEE", got)
	}
	if remainder.BitsLeft() != 4 || remainder.RefsNum() != 0 {
		t.Fatalf("LDREFRTOS remainder = (%d bits, %d refs), want (4, 0)", remainder.BitsLeft(), remainder.RefsNum())
	}
	if loads != 1 {
		t.Fatalf("LDREFRTOS child trace loads = %d, want 1", loads)
	}
	if got := st.Gas.Used(); got != vm.CellLoadGasPrice {
		t.Fatalf("LDREFRTOS gas used = %d, want %d", got, vm.CellLoadGasPrice)
	}
}
