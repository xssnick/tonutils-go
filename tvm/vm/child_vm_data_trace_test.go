package vm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestRunChildVMDataDoesNotChargeParent(t *testing.T) {
	for _, version := range []int{4, 5, 9, 10, 11, 15, MaxSupportedGlobalVersion} {
		for _, loaded := range []bool{false, true} {
			for _, isolated := range []bool{false, true} {
				t.Run(fmt.Sprintf("v%d/loaded=%t/isolated=%t", version, loaded, isolated), func(t *testing.T) {
					parent := NewExecutionState(version, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
					parent.InitForExecution()
					root := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
					if loaded {
						if err := parent.Cells.RegisterCellLoad(root); err != nil {
							t.Fatal(err)
						}
					}

					loads := 0
					usage := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) { loads++ }})
					trace := cell.CombineTraces(usage, parent.Cells.Trace())
					data := root.WithTrace(trace)
					before := parent.Gas.Used()
					want := int64(CellLoadGasPrice)
					if loaded && !isolated {
						want = CellReloadGasPrice
					}

					parent.SetChildRunner(func(child *State) (int64, error) {
						slice, err := child.Cells.BeginParse(child.Reg.D[0])
						if err != nil {
							return 0, err
						}

						value, err := slice.LoadUInt(8)
						if err != nil || value != 0xA5 {
							t.Fatal("child data changed", value, err)
						}
						if parent.Gas.Used() != before {
							t.Errorf("child load charged parent before settlement: delta=%d", parent.Gas.Used()-before)
						}
						if child.Gas.Used() != want {
							t.Errorf("child load gas=%d, want %d", child.Gas.Used(), want)
						}
						return 0, nil
					})

					if err := parent.RunChildVM(ChildVMConfig{
						Code:       cell.BeginCell().EndCell().MustBeginParse(),
						Data:       data,
						Gas:        GasWithLimit(10_000),
						IsolateGas: isolated,
						ReturnGas:  true,
					}); err != nil {
						t.Fatal(err)
					}
					if got := parent.Gas.Used() - before; got != want {
						t.Errorf("settled gas=%d, want %d", got, want)
					}
					if got := mustPopInt64(t, parent.Stack); got != want {
						t.Errorf("returned gas=%d, want %d", got, want)
					}
					if loads != 1 {
						t.Errorf("usage trace load count=%d, want 1", loads)
					}
					if data.Trace() != trace || data.HashKey() != root.HashKey() {
						t.Fatal("caller data or trace mutated")
					}
				})
			}
		}
	}
}

func TestRunChildVMDataReferenceAtGasLimit(t *testing.T) {
	const wantGas = 2 * CellLoadGasPrice
	parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(wantGas), nil, tuple.Tuple{}, NewStack())
	parent.InitForExecution()
	leaf := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	root := cell.BeginCell().MustStoreRef(leaf).EndCell()

	var loads []cell.Hash
	var usage *cell.Trace
	usage = cell.NewTrace(cell.TraceHooks{
		OnLoad:  func(cl *cell.Cell) { loads = append(loads, cl.HashKey()) },
		OnChild: func(int) *cell.Trace { return usage },
	})
	trace := cell.CombineTraces(usage, parent.Cells.Trace())
	data := root.WithTrace(trace)
	parent.SetChildRunner(func(child *State) (int64, error) {
		slice, err := child.Cells.BeginParse(child.Reg.D[0])
		if err != nil {
			return 0, err
		}
		ref, err := child.Cells.LoadRef(slice)
		if err != nil {
			return 0, err
		}
		value, err := ref.LoadUInt(8)
		if err != nil || value != 0xA5 {
			t.Fatalf("child reference value = %d, error = %v", value, err)
		}
		if got := parent.Gas.Used(); got != 0 {
			t.Errorf("child traversal charged parent before settlement: gas = %d", got)
		}
		return 0, nil
	})

	if err := parent.RunChildVM(ChildVMConfig{
		Code:      cell.BeginCell().EndCell().MustBeginParse(),
		Data:      data,
		Gas:       GasWithLimit(wantGas),
		ReturnGas: true,
	}); err != nil {
		t.Fatalf("run child VM with exactly enough gas: %v", err)
	}
	if got := parent.Gas.Used(); got != wantGas {
		t.Errorf("settled gas = %d, want %d", got, wantGas)
	}
	if got := mustPopInt64(t, parent.Stack); got != wantGas {
		t.Errorf("returned gas = %d, want %d", got, wantGas)
	}
	if got := mustPopInt64(t, parent.Stack); got != 0 {
		t.Errorf("child exit code = %d, want 0", got)
	}
	if len(loads) != 2 || loads[0] != root.HashKey() || loads[1] != leaf.HashKey() {
		t.Fatalf("usage trace did not observe root and reference exactly once: %v", loads)
	}
	if data.Trace() != trace || data.HashKey() != root.HashKey() {
		t.Fatal("caller data or trace mutated")
	}
}
