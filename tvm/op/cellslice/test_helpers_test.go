package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func newCellSliceState() *vm.State {
	st := &vm.State{
		Stack: vm.NewStack(),
		Gas:   vm.NewGas(),
	}
	st.InitForExecution()
	return st
}

func pushCellSliceInt(t *testing.T, st *vm.State, v int64) {
	t.Helper()
	if err := st.Stack.PushInt(big.NewInt(v)); err != nil {
		t.Fatalf("failed to push int %d: %v", v, err)
	}
}

func pushCellSliceBool(t *testing.T, st *vm.State, v bool) {
	t.Helper()
	if err := st.Stack.PushBool(v); err != nil {
		t.Fatalf("failed to push bool %v: %v", v, err)
	}
}

func pushCellSliceCell(t *testing.T, st *vm.State, cl *cell.Cell) {
	t.Helper()
	if err := st.Stack.PushCell(cl); err != nil {
		t.Fatalf("failed to push cell: %v", err)
	}
}

func pushCellSliceBuilder(t *testing.T, st *vm.State, b *cell.Builder) {
	t.Helper()
	if err := st.Stack.PushBuilder(b); err != nil {
		t.Fatalf("failed to push builder: %v", err)
	}
}

func pushCellSliceSlice(t *testing.T, st *vm.State, sl *cell.Slice) {
	t.Helper()
	if err := st.Stack.PushSlice(sl); err != nil {
		t.Fatalf("failed to push slice: %v", err)
	}
}

func popCellSliceInt(t *testing.T, st *vm.State) int64 {
	t.Helper()
	v, err := st.Stack.PopInt()
	if err != nil {
		t.Fatalf("failed to pop int: %v", err)
	}
	return v.Int64()
}

func popCellSliceBool(t *testing.T, st *vm.State) bool {
	t.Helper()
	v, err := st.Stack.PopBool()
	if err != nil {
		t.Fatalf("failed to pop bool: %v", err)
	}
	return v
}

func popCellSliceCell(t *testing.T, st *vm.State) *cell.Cell {
	t.Helper()
	v, err := st.Stack.PopCell()
	if err != nil {
		t.Fatalf("failed to pop cell: %v", err)
	}
	return v
}

func popCellSliceBuilder(t *testing.T, st *vm.State) *cell.Builder {
	t.Helper()
	v, err := st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("failed to pop builder: %v", err)
	}
	return v
}

func popCellSliceSlice(t *testing.T, st *vm.State) *cell.Slice {
	t.Helper()
	v, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("failed to pop slice: %v", err)
	}
	return v
}

func sameCellHash(a, b *cell.Cell) bool {
	if a == nil || b == nil {
		return a == b
	}
	return string(a.Hash()) == string(b.Hash())
}

func sameSliceHash(a, b *cell.Slice) bool {
	if a == nil || b == nil {
		return a == b
	}
	return sameCellHash(a.MustToCell(), b.MustToCell())
}

func mustLibraryCell(t *testing.T) *cell.Cell {
	t.Helper()
	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func mustPrunedCell(t *testing.T) *cell.Cell {
	t.Helper()
	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.PrunedCellType), 8).
		MustStoreUInt(1, 8).
		MustStoreSlice(make([]byte, 32), 256).
		MustStoreUInt(0, 16).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build pruned cell: %v", err)
	}
	return cl
}

func mustUnknownSpecialCell(t *testing.T) *cell.Cell {
	t.Helper()
	cl := cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell()
	cl.UnsafeModify(cell.LevelMask{}, true)
	return cl
}

func mustFullBuilder(t *testing.T) *cell.Builder {
	t.Helper()
	b := cell.BeginCell()
	for i := 0; i < 4; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b
}
