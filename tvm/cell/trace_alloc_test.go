package cell

import "testing"

var traceAllocationSink *Trace

func TestTraceCompactBackendsAvoidExtraBoxing(t *testing.T) {
	hooks := TraceHooks{OnLoad: func(*Cell) {}}
	if allocs := testing.AllocsPerRun(1000, func() {
		traceAllocationSink = NewTrace(hooks)
	}); allocs != 1 {
		t.Fatalf("hook trace allocations: got=%v want=1", allocs)
	}

	left := NewTrace(hooks)
	right := NewTrace(hooks)
	if allocs := testing.AllocsPerRun(1000, func() {
		traceAllocationSink = CombineTraces(left, right)
	}); allocs != 1 {
		t.Fatalf("combined trace allocations: got=%v want=1", allocs)
	}
}

func TestCombineTracesReusesEquivalentComposite(t *testing.T) {
	left := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	right := NewTrace(TraceHooks{OnCreate: func() {}})
	pair := CombineTraces(left, right)

	for name, traces := range map[string][]*Trace{
		"pair then member": {pair, right},
		"first then pair":  {left, pair},
		"pair repeated":    {pair, pair},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CombineTraces(traces...); got != pair {
				t.Fatalf("equivalent combination was rebuilt: got=%p want=%p", got, pair)
			}
		})
	}

	if got := CombineTraces(right, pair); got == pair {
		t.Fatal("combination with a different notification order reused the pair")
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		traceAllocationSink = CombineTraces(pair, right)
	}); allocs != 0 {
		t.Fatalf("idempotent combination allocations: got=%v want=0", allocs)
	}
}
