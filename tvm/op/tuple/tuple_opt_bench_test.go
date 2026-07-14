package tuple

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

var benchmarkTupleStackLenSink int

func BenchmarkExecMakeTuple8(b *testing.B) {
	values := make([]*big.Int, 8)
	for i := range values {
		values[i] = big.NewInt(int64(i))
	}
	st := vm.NewStack()
	state := &vm.State{Stack: st, Gas: vm.NewGas()}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st.Clear()
		for _, value := range values {
			if err := st.PushInt(value); err != nil {
				b.Fatal(err)
			}
		}
		if err := execMakeTuple(state, len(values)); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkTupleStackLenSink = st.Len()
}
