package vm

import "testing"

var (
	benchmarkStackSink      *Stack
	benchmarkStackValueSink any
)

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

func BenchmarkStackPopAnySmall(b *testing.B) {
	base := []any{stackIntOne}
	st := &Stack{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		base[0] = stackIntOne
		st.elems = base
		value, err := st.PopAny()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStackValueSink = value
	}
}

func BenchmarkStackDropOne(b *testing.B) {
	base := []any{stackIntOne}
	st := &Stack{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		base[0] = stackIntOne
		st.elems = base
		if err := st.Drop(1); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStackClearSmall(b *testing.B) {
	base := []any{stackIntOne, stackIntZero, stackIntMinusOne}
	st := &Stack{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		base[0], base[1], base[2] = stackIntOne, stackIntZero, stackIntMinusOne
		st.elems = base
		st.Clear()
	}
}
