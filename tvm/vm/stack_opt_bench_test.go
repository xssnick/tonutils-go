package vm

import "testing"

var benchmarkStackSink *Stack

func BenchmarkStackSplitTopEmpty(b *testing.B) {
	st := &Stack{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st.elems = nil
		next, err := st.SplitTop(0, 0)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStackSink = next
	}
}

func BenchmarkStackSplitTopSmall(b *testing.B) {
	base := []any{stackIntOne, stackIntZero}
	st := &Stack{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		st.elems = base[:2]
		next, err := st.SplitTop(1, 0)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStackSink = next
	}
}
