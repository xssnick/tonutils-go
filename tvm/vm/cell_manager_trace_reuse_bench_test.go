package vm_test

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func BenchmarkCellManagerTracePairParseInto(b *testing.B) {
	root := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	state := vm.State{Gas: vm.GasWithLimit(vm.GasInfinite)}
	state.Cells.Init(&state)
	reads := cell.NewReadSet(root)
	trace := cell.CombineTraces(reads.Trace(), state.Cells.Trace())
	var dst cell.Slice
	if err := state.Cells.BeginParseIntoWithTrace(root, trace, &dst); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := state.Cells.BeginParseIntoWithTrace(root, trace, &dst); err != nil {
			b.Fatal(err)
		}
	}
}
