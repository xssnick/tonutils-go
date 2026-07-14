package math

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

var benchmarkMathStackLenSink int

func BenchmarkPushSmallInt(b *testing.B) {
	st := vm.NewStack()
	state := &vm.State{Stack: st}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st.Clear()
		if err := pushSmallInt(state, 10); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkMathStackLenSink = st.Len()
}
