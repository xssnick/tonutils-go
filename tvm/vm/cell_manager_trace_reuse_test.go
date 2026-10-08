package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestCellManagerReusesOrderedTracePair(t *testing.T) {
	root := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	state := State{Gas: GasWithLimit(GasInfinite)}
	state.Cells.Init(&state)
	reads := cell.NewReadSet(root)

	for _, tc := range []struct {
		name  string
		trace *cell.Trace
		parse func(*cell.Cell, *cell.Trace, *cell.Slice) error
	}{
		{"gas", cell.CombineTraces(reads.Trace(), state.Cells.Trace()), state.Cells.BeginParseIntoWithTrace},
		{"already loaded", cell.CombineTraces(reads.Trace(), state.Cells.Trace()), state.Cells.BeginParseAlreadyLoadedIntoWithTrace},
		{"no create", cell.CombineTraces(reads.Trace(), state.Cells.LoadTrace()), state.Cells.BeginParseAlreadyLoadedNoCreateIntoWithTrace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dst cell.Slice
			if err := tc.parse(root, tc.trace, &dst); err != nil {
				t.Fatal(err)
			}
			if dst.Trace() != tc.trace {
				t.Fatal("parse replaced an already ordered trace pair")
			}
			if allocs := testing.AllocsPerRun(1000, func() {
				if err := tc.parse(root, tc.trace, &dst); err != nil {
					panic(err)
				}
			}); allocs != 0 {
				t.Fatalf("parse allocated %.0f times, want 0", allocs)
			}
		})
	}
}

func TestCellManagerReordersGasTraceAfterUsage(t *testing.T) {
	root := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	for _, alreadyLoaded := range []bool{false, true} {
		name := "load"
		if alreadyLoaded {
			name = "already loaded"
		}
		t.Run(name, func(t *testing.T) {
			state := State{Gas: GasWithLimit(10_000)}
			state.Cells.Init(&state)
			calls := 0
			observer := cell.NewTrace(cell.TraceHooks{OnLoad: func(*cell.Cell) {
				calls++
				if state.Gas.Used() != 0 {
					t.Fatal("gas was charged before the usage listener")
				}
			}})
			trace := cell.CombineTraces(state.Cells.Trace(), observer)
			var dst cell.Slice
			parse := state.Cells.BeginParseIntoWithTrace
			expectedGas := int64(CellLoadGasPrice)
			if alreadyLoaded {
				parse = state.Cells.BeginParseAlreadyLoadedIntoWithTrace
				expectedGas = 0
			}
			if err := parse(root, trace, &dst); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || state.Gas.Used() != expectedGas {
				t.Fatalf("calls=%d gas=%d, want 1 and %d", calls, state.Gas.Used(), expectedGas)
			}
		})
	}
}
