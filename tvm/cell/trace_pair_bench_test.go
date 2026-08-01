package cell

import "testing"

var benchmarkTracePairSink *Trace

func BenchmarkCombineTracePair(b *testing.B) {
	left := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	right := NewTrace(TraceHooks{OnCreate: func() {}})

	b.Run("new_pair", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkTracePairSink = CombineTraces(left, right)
		}
	})

	pair := CombineTraces(left, right)
	b.Run("existing_pair_member", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkTracePairSink = CombineTraces(pair, right)
		}
	})
}
