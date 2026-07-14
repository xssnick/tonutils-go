package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var (
	benchmarkStackIndexSink int
	benchmarkStackLenSink   int
)

func benchmarkSliceValue(v uint64) *cell.Slice {
	return cell.BeginCell().MustStoreUInt(v, 32).ToSlice()
}

func BenchmarkPopSmallIndex(b *testing.B) {
	st := vm.NewStack()
	state := &vm.State{Stack: st}
	value := big.NewInt(1)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st.Clear()
		if err := st.PushInt(value); err != nil {
			b.Fatal(err)
		}
		idx, err := popSmallIndex(state)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStackIndexSink = idx
	}
}

func BenchmarkROTSlices(b *testing.B) {
	st := vm.NewStack()
	if err := st.PushOwnedSlice(benchmarkSliceValue(1)); err != nil {
		b.Fatal(err)
	}
	if err := st.PushOwnedSlice(benchmarkSliceValue(2)); err != nil {
		b.Fatal(err)
	}
	if err := st.PushOwnedSlice(benchmarkSliceValue(3)); err != nil {
		b.Fatal(err)
	}
	state := &vm.State{Stack: st}
	op := ROT()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkStackLenSink = st.Len()
}

func BenchmarkSWAP2Slices(b *testing.B) {
	st := vm.NewStack()
	for i := uint64(1); i <= 4; i++ {
		if err := st.PushOwnedSlice(benchmarkSliceValue(i)); err != nil {
			b.Fatal(err)
		}
	}
	state := &vm.State{Stack: st}
	op := SWAP2()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkStackLenSink = st.Len()
}
