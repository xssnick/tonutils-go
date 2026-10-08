package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func BenchmarkSTSLICEInterpret(b *testing.B) {
	value := cell.BeginCell().MustStoreSlice(make([]byte, 64), 511).EndCell().MustBeginParse()
	op := STSLICE()
	state := &vm.State{}

	b.ReportAllocs()
	for b.Loop() {
		state.Stack = vm.NewStack()
		if err := state.Stack.PushSlice(value); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushOwnedBuilder(cell.BeginCell()); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHASHSUInterpret(b *testing.B) {
	value := cell.BeginCell().MustStoreSlice(make([]byte, 64), 511).EndCell().MustBeginParse()
	op := HASHSU()
	state := &vm.State{}

	b.ReportAllocs()
	for b.Loop() {
		state.Stack = vm.NewStack()
		if err := state.Stack.PushSlice(value); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
}
