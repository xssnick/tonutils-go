package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestCellManagerRegisterCellLoadTracksReloads(t *testing.T) {
	st := &State{
		Gas:   GasWithLimit(10_000),
		Stack: NewStack(),
	}
	st.InitForExecution()

	cl := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	if err := st.Cells.RegisterCellLoad(cl); err != nil {
		t.Fatal(err)
	}
	if err := st.Cells.RegisterCellLoad(cl); err != nil {
		t.Fatal(err)
	}

	if got := st.Gas.Used(); got != CellLoadGasPrice+CellReloadGasPrice {
		t.Fatalf("unexpected gas usage: got=%d want=%d", got, CellLoadGasPrice+CellReloadGasPrice)
	}
}

func TestStatePushTupleChargedConsumesTupleGas(t *testing.T) {
	st := &State{
		Gas:   GasWithLimit(10_000),
		Stack: NewStack(),
	}
	st.InitForExecution()

	tup := *tuple.NewTuple(int64(1), int64(2), int64(3))
	if err := st.PushTupleCharged(tup); err != nil {
		t.Fatal(err)
	}

	if got := st.Gas.Used(); got != 3*TupleEntryGasPrice {
		t.Fatalf("unexpected tuple gas usage: got=%d want=%d", got, 3*TupleEntryGasPrice)
	}

	if st.Stack.Len() != 1 {
		t.Fatalf("unexpected stack size: got=%d want=1", st.Stack.Len())
	}
}
