package cell

import (
	"errors"
	"testing"
)

type augmentedTraceCounters struct {
	loads   int
	creates int
}

type orderedLeafAugmentation struct {
	testMetricAugmentation
	leafCalls int
}

func (a *orderedLeafAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	a.leafCalls++
	return a.testMetricAugmentation.LeafExtra(value, dst)
}

func newAugmentedTraceCounters() (*Trace, *augmentedTraceCounters) {
	counters := new(augmentedTraceCounters)
	var trace *Trace
	trace = NewTrace(TraceHooks{
		OnLoad: func(*Cell) {
			counters.loads++
		},
		OnCreate: func() {
			counters.creates++
		},
		OnChild: func(int) *Trace {
			return trace
		},
	})
	return trace, counters
}

func (c *augmentedTraceCounters) reset() {
	c.loads = 0
	c.creates = 0
}

func TestAugmentedDictionaryTraverseExtraStopsAtPendingLoadError(t *testing.T) {
	aug := testMetricAugmentation{}
	dict := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, dict, 8, 0x12, 0xaa, 8)

	pendingErr := errors.New("pending cell load error")
	trace, loads := pendingErrorAfterLoads(1, pendingErr)
	dict.SetTrace(trace)

	called := false
	value, extra, err := dict.TraverseExtra(func(*Cell, *Slice, *Slice) (int, error) {
		called = true
		return 1, nil
	})
	if !errors.Is(err, pendingErr) {
		t.Fatalf("TraverseExtra error = %v, want pending load error", err)
	}
	if value != nil || extra != nil {
		t.Fatalf("TraverseExtra returned value=%v extra=%v after pending load error", value, extra)
	}
	if called {
		t.Fatal("TraverseExtra invoked callback after pending load error")
	}
	if got := loads(); got != 1 {
		t.Fatalf("load notifications = %d, want 1", got)
	}
}

func TestExtractAugmentedNodeExtraStopsAtPendingLoadError(t *testing.T) {
	aug := testMetricAugmentation{}
	dict := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, dict, 8, 0x12, 0xaa, 8)

	pendingErr := errors.New("pending cell load error")

	t.Run("direct", func(t *testing.T) {
		trace, loads := pendingErrorAfterLoads(1, pendingErr)
		extra, err := extractAugmentedNodeExtra(dict.root.WithTrace(trace), 8, aug.SkipExtra)
		if !errors.Is(err, pendingErr) {
			t.Fatalf("extractAugmentedNodeExtra error = %v, want pending load error", err)
		}
		if extra != nil {
			t.Fatal("extractAugmentedNodeExtra returned an extra after pending load error")
		}
		if got := loads(); got != 1 {
			t.Fatalf("load notifications = %d, want 1", got)
		}
	})

	t.Run("LoadRootExtra", func(t *testing.T) {
		trace, loads := pendingErrorAfterLoads(1, pendingErr)
		view := dict.Copy()
		view.rootExtra = nil
		view.SetTrace(trace)

		extra, err := view.LoadRootExtra()
		if !errors.Is(err, pendingErr) {
			t.Fatalf("LoadRootExtra error = %v, want pending load error", err)
		}
		if extra != nil {
			t.Fatal("LoadRootExtra returned an extra after pending load error")
		}
		if got := loads(); got != 1 {
			t.Fatalf("load notifications = %d, want 1", got)
		}
	})
}

func TestParseAugmentedNodeForCombineStopsAtPendingLoadError(t *testing.T) {
	aug := testMetricAugmentation{}
	dict := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, dict, 8, 0x12, 0xaa, 8)

	pendingErr := errors.New("pending cell load error")
	trace, loads := pendingErrorAfterLoads(1, pendingErr)
	state := augmentedCombineState{aug: aug}
	_, err := parseAugmentedNodeForCombine(augmentedRootView{
		cell:  dict.root.WithTrace(trace),
		keySz: 8,
	}, &state)
	if !errors.Is(err, pendingErr) {
		t.Fatalf("parseAugmentedNodeForCombine error = %v, want pending load error", err)
	}
	if got := loads(); got != 1 {
		t.Fatalf("load notifications = %d, want 1", got)
	}
}

func TestAugmentedDictionaryCombineRetainsReceiverTrace(t *testing.T) {
	aug := testMetricAugmentation{}

	t.Run("adopted root", func(t *testing.T) {
		left := mustNewCombineTestAugDict(t, 8, aug)
		right := mustNewCombineTestAugDict(t, 8, aug)
		mustSetCombineTestValue(t, right, 8, 0x12, 0xaa, 8)

		trace, counters := newAugmentedTraceCounters()
		left.SetTrace(trace)
		ok, err := left.CombineWith(right)
		if err != nil || !ok {
			t.Fatalf("CombineWith returned ok=%v err=%v", ok, err)
		}
		if counters.loads == 0 {
			t.Fatal("CombineWith did not account for adopted-root validation loads")
		}

		counters.reset()
		_, _, err = left.TraverseExtra(func(*Cell, *Slice, *Slice) (int, error) {
			return 0, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if counters.loads == 0 {
			t.Fatal("TraverseExtra did not retain receiver load trace after root adoption")
		}

		counters.reset()
		mustSetCombineTestValue(t, left, 8, 0x34, 0xbb, 8)
		if counters.loads == 0 || counters.creates == 0 {
			t.Fatalf("subsequent mutation accounting: loads=%d creates=%d", counters.loads, counters.creates)
		}
	})

	t.Run("rebuilt root", func(t *testing.T) {
		left := mustNewCombineTestAugDict(t, 8, aug)
		right := mustNewCombineTestAugDict(t, 8, aug)
		mustSetCombineTestValue(t, left, 8, 0x10, 0xaa, 8)
		mustSetCombineTestValue(t, right, 8, 0x80, 0xbb, 8)

		trace, counters := newAugmentedTraceCounters()
		left.SetTrace(trace)
		ok, err := left.CombineWith(right)
		if err != nil || !ok {
			t.Fatalf("CombineWith returned ok=%v err=%v", ok, err)
		}
		if counters.loads < 2 {
			t.Fatalf("CombineWith load accounting = %d, want both roots", counters.loads)
		}
		if counters.creates == 0 {
			t.Fatal("CombineWith did not account for rebuilt cells")
		}

		counters.reset()
		_, _, err = left.TraverseExtra(func(*Cell, *Slice, *Slice) (int, error) {
			return 0, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if counters.loads == 0 {
			t.Fatal("TraverseExtra did not retain receiver load trace after root rebuild")
		}
	})
}

func TestAugmentedDictionaryDivergentSetChargesOldChildCreateFirst(t *testing.T) {
	aug := new(orderedLeafAugmentation)
	dict := mustNewCombineTestAugDict(t, 8, aug)
	mustSetCombineTestValue(t, dict, 8, 0x10, 0xaa, 8)
	aug.leafCalls = 0

	pendingErr := errors.New("pending cell create error")
	creates := 0
	trace := NewTrace(TraceHooks{
		OnCreate: func() {
			creates++
		},
		PendingError: func() error {
			if creates > 0 {
				return pendingErr
			}
			return nil
		},
	})
	dict.SetTrace(trace)
	originalRoot := dict.root

	var gotErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				var ok bool
				gotErr, ok = recovered.(error)
				if !ok {
					t.Fatalf("set panic = %v, want error", recovered)
				}
			}
		}()
		gotErr = dict.Set(mustCombineTestKey(t, 8, 0x80), mustCombineTestValue(0xbb, 8))
	}()

	if !errors.Is(gotErr, pendingErr) {
		t.Fatalf("divergent set error = %v, want pending create error", gotErr)
	}
	if creates != 1 {
		t.Fatalf("create notifications = %d, want 1", creates)
	}
	if aug.leafCalls != 0 {
		t.Fatalf("new leaf extra calls = %d, want 0 before old-child create succeeds", aug.leafCalls)
	}
	if dict.root != originalRoot {
		t.Fatal("divergent set mutated root after pending create error")
	}
}

var benchmarkAugmentedDivergentRootSink *Cell

func BenchmarkAugmentedDictionarySetDivergent(b *testing.B) {
	aug := testMetricAugmentation{}
	base := mustNewCombineTestAugDict(b, 8, aug)
	mustSetCombineTestValue(b, base, 8, 0x10, 0xaa, 8)
	key := mustCombineTestKey(b, 8, 0x80)
	value := mustCombineTestValue(0xbb, 8)

	b.ReportAllocs()
	for b.Loop() {
		dict := base.Copy()
		if err := dict.Set(key, value); err != nil {
			b.Fatal(err)
		}
		benchmarkAugmentedDivergentRootSink = dict.root
	}
}
