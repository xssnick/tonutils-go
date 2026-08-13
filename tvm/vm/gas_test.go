package vm

import (
	"errors"
	"math/big"
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

	tup := tuple.NewTupleValue(big.NewInt(1), big.NewInt(2), big.NewInt(3))
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

func TestGasWithLimitPreservesExplicitZero(t *testing.T) {
	tests := []struct {
		name      string
		gas       Gas
		wantMax   int64
		wantLimit int64
	}{
		{
			name:      "zero limit",
			gas:       GasWithLimit(0),
			wantMax:   GasInfinite,
			wantLimit: 0,
		},
		{
			name:      "zero limit and zero max sentinel",
			gas:       GasWithLimit(0, 0),
			wantMax:   GasInfinite,
			wantLimit: 0,
		},
		{
			name:      "limit clamped to max",
			gas:       GasWithLimit(5, 3),
			wantMax:   3,
			wantLimit: 3,
		},
		{
			name:      "negative limit retained",
			gas:       GasWithLimit(-1),
			wantMax:   GasInfinite,
			wantLimit: -1,
		},
		{
			name:      "negative max retains existing clamp",
			gas:       GasWithLimit(0, -1),
			wantMax:   -1,
			wantLimit: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.gas.Max != tt.wantMax {
				t.Fatalf("Max = %d, want %d", tt.gas.Max, tt.wantMax)
			}
			if tt.gas.Limit != tt.wantLimit || tt.gas.Base != tt.wantLimit || tt.gas.Remaining != tt.wantLimit {
				t.Fatalf("limit/base/remaining = %d/%d/%d, want %d", tt.gas.Limit, tt.gas.Base, tt.gas.Remaining, tt.wantLimit)
			}
			if tt.gas.Credit != 0 || tt.gas.FreeConsumed != 0 {
				t.Fatalf("unexpected auxiliary gas fields: %+v", tt.gas)
			}
		})
	}

	defaults := NewGas()
	if defaults.Max != GasInfinite || defaults.Limit != GasInfinite || defaults.Base != GasInfinite || defaults.Remaining != GasInfinite {
		t.Fatalf("NewGas zero-value sentinel changed: %+v", defaults)
	}
}

// the stop happens whenever the stop-on-accept flag is armed, even when no
// gas credit was outstanding.
func TestSetGasLimitStopsOnAcceptWithoutCredit(t *testing.T) {
	s := NewExecutionState(13, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	s.StopOnAccept = true
	if err := s.SetGasLimit(500); !errors.Is(err, ErrStopOnAccept) {
		t.Fatalf("SetGasLimit with zero credit = %v, want ErrStopOnAccept", err)
	}

	s2 := NewExecutionState(13, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	if err := s2.SetGasLimit(500); err != nil {
		t.Fatalf("SetGasLimit without the flag = %v, want nil", err)
	}
}
