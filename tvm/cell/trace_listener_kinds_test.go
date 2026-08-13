package cell

import (
	"errors"
	"fmt"
	"testing"
)

var (
	errTraceProbePending = errors.New("trace probe pending")
	errTraceProbeSecond  = errors.New("trace probe second pending")
)

// traceKindProbe records everything a listener-backed trace asks of its
// listener, so the two listener kinds can be compared event for event.
type traceKindProbe struct {
	loads    int
	creates  int
	children int
	pending  error
	child    *Trace
}

func (p *traceKindProbe) OnLoad(*Cell) {
	p.loads++
}

func (p *traceKindProbe) OnCreate() {
	p.creates++
}

func (p *traceKindProbe) ChildTrace(int) *Trace {
	p.children++
	return p.child
}

func (p *traceKindProbe) PendingError() error {
	return p.pending
}

// ResolveDictNodeCell makes the probe a DictSpecialResolver, which is what
// Trace.dictSpecialResolver must find on both listener kinds.
func (p *traceKindProbe) ResolveDictNodeCell(c *Cell) (*Cell, error) {
	return c, nil
}

// plainTraceListener backs traceKindListener: it has no OnLoadError.
type plainTraceListener struct {
	*traceKindProbe
}

// loadErrorTraceListener backs traceKindLoadErrorListener and mirrors the VM
// cell manager, where OnLoad is the same charge with the error dropped.
type loadErrorTraceListener struct {
	*traceKindProbe
}

func (l *loadErrorTraceListener) OnLoadError(c *Cell) error {
	l.OnLoad(c)
	return l.pending
}

// traceKindObservations drives one listener through every Trace entry point,
// alone and inside pair and combined traces, and returns what was observed.
// Both listener kinds must produce the same log: the only case allowed to tell
// them apart is the load dispatch, and that difference is not observable from
// outside the listener.
func traceKindObservations(newListener func(*traceKindProbe) TraceListener) []string {
	cl := BeginCell().EndCell()

	var log []string
	record := func(format string, args ...any) {
		log = append(log, fmt.Sprintf(format, args...))
	}

	probe := &traceKindProbe{}
	listener := newListener(probe)
	leaf := NewTraceForListener(listener)
	probe.child = leaf

	secondProbe := &traceKindProbe{}
	second := NewTraceForListener(&plainTraceListener{secondProbe})
	secondProbe.child = second

	thirdProbe := &traceKindProbe{}
	third := NewTraceForListener(&plainTraceListener{thirdProbe})
	thirdProbe.child = third

	// Every entry point is recorded together with the callback counters it moved,
	// so a kind that silently stops dispatching shows up as a diverging log. The
	// call always runs before the counters are read, which argument evaluation
	// order alone does not guarantee.
	loads := func(name string, trace *Trace) {
		trace.NotifyLoad(cl)
		record("%s load: loads=%d/%d/%d", name, probe.loads, secondProbe.loads, thirdProbe.loads)

		err := trace.NotifyLoadError(cl)
		record("%s load err: err=%v loads=%d/%d/%d", name, err, probe.loads, secondProbe.loads, thirdProbe.loads)
	}
	creates := func(name string, trace *Trace) {
		err := trace.NotifyCreate()
		record("%s create: err=%v creates=%d/%d/%d", name, err, probe.creates, secondProbe.creates, thirdProbe.creates)
		record("%s pending: err=%v", name, trace.PendingError())
	}
	child := func(name string, trace *Trace, refIdx int) {
		got := trace.Child(refIdx)
		record("%s child: self=%t children=%d/%d/%d", name, got == trace, probe.children, secondProbe.children, thirdProbe.children)
	}

	loads("leaf", leaf)
	creates("leaf", leaf)
	child("leaf", leaf, 1)
	record("leaf resolver: self=%t", any(leaf.dictSpecialResolver()) == any(listener))

	probe.pending = errTraceProbePending
	loads("latched leaf", leaf)
	creates("latched leaf", leaf)
	probe.pending = nil

	pair := CombineTraces(leaf, second)
	record("pair: kind=%d", pair.kind)
	loads("pair", pair)
	creates("pair", pair)
	child("pair", pair, 0)
	record("pair resolver: leaf=%t", any(pair.dictSpecialResolver()) == any(listener))
	record("pair without leaf: second=%t", pair.WithoutTrace(leaf) == second)

	probe.pending = errTraceProbePending
	loads("latched pair", pair)
	creates("latched pair", pair)
	probe.pending = nil

	combined := CombineTraces(leaf, second, third)
	record("combined: kind=%d", combined.kind)
	loads("combined", combined)
	creates("combined", combined)
	child("combined", combined, 2)
	record("combined resolver: leaf=%t", any(combined.dictSpecialResolver()) == any(listener))
	record("combined without leaf: kind=%d", combined.WithoutTrace(leaf).kind)

	probe.pending = errTraceProbePending
	loads("latched combined", combined)
	creates("latched combined", combined)
	probe.pending = nil

	leaf.DetachListener()
	record("detached: kind=%d backendNil=%t", leaf.kind, leaf.backend == nil)
	loads("detached leaf", leaf)
	creates("detached leaf", leaf)
	detachedChild := leaf.Child(0)
	record("detached leaf child: nil=%t children=%d", detachedChild == nil, probe.children)
	record("detached leaf resolver: nil=%t", leaf.dictSpecialResolver() == nil)
	record("detached leaf combine: second=%t", CombineTraces(leaf, second) == second)

	loads("detached combined", combined)
	creates("detached combined", combined)

	return log
}

// TestTraceListenerKindsDispatchIdentically guards the split between
// traceKindListener and traceKindLoadErrorListener: every switch over trace
// kinds except the two load dispatches must handle both tags in one case, and
// a forgotten case shows up here as a diverging event log.
func TestTraceListenerKindsDispatchIdentically(t *testing.T) {
	cases := []struct {
		name        string
		wantKind    traceKind
		newListener func(*traceKindProbe) TraceListener
	}{
		{
			name:     "listener",
			wantKind: traceKindListener,
			newListener: func(p *traceKindProbe) TraceListener {
				return &plainTraceListener{p}
			},
		},
		{
			name:     "load error listener",
			wantKind: traceKindLoadErrorListener,
			newListener: func(p *traceKindProbe) TraceListener {
				return &loadErrorTraceListener{p}
			},
		},
	}

	cl := BeginCell().EndCell()
	var reference []string
	referenceName := ""

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &traceKindProbe{}
			listener := tc.newListener(probe)
			leaf := NewTraceForListener(listener)
			if leaf.kind != tc.wantKind {
				t.Fatalf("trace kind = %d, want %d", leaf.kind, tc.wantKind)
			}

			leaf.NotifyLoad(cl)
			if probe.loads != 1 {
				t.Fatalf("load deliveries after NotifyLoad = %d, want 1", probe.loads)
			}

			probe.pending = errTraceProbePending
			if err := leaf.NotifyLoadError(cl); err != errTraceProbePending {
				t.Fatalf("NotifyLoadError err = %v, want %v", err, errTraceProbePending)
			}
			if probe.loads != 2 {
				t.Fatalf("load deliveries after NotifyLoadError = %d, want 2", probe.loads)
			}
			if err := leaf.NotifyCreate(); err != errTraceProbePending {
				t.Fatalf("NotifyCreate err = %v, want %v", err, errTraceProbePending)
			}
			if err := leaf.PendingError(); err != errTraceProbePending {
				t.Fatalf("PendingError err = %v, want %v", err, errTraceProbePending)
			}
			if resolver := leaf.dictSpecialResolver(); any(resolver) != any(listener) {
				t.Fatalf("dictSpecialResolver = %v, want the listener itself", resolver)
			}

			leaf.DetachListener()
			if leaf.kind != traceKindEmpty || leaf.backend != nil {
				t.Fatalf("detached trace = %+v, want an empty inert trace", leaf)
			}

			log := traceKindObservations(tc.newListener)
			if len(log) == 0 {
				t.Fatal("observed no trace events")
			}
			if reference == nil {
				reference, referenceName = log, tc.name
				return
			}
			if len(log) != len(reference) {
				t.Fatalf("observed %d events, %q observed %d", len(log), referenceName, len(reference))
			}
			for i := range log {
				if log[i] != reference[i] {
					t.Fatalf("event %d = %q, %q observed %q", i, log[i], referenceName, reference[i])
				}
			}
		})
	}
}

// TestTraceNotifyLoadAgreesWithNotifyLoadError covers the one pair of switches
// that repeats every trace kind: both must fire the same listener callbacks and
// NotifyLoadError must return the error NotifyLoad drops.
func TestTraceNotifyLoadAgreesWithNotifyLoadError(t *testing.T) {
	cl := BeginCell().EndCell()

	cases := []struct {
		name    string
		wantErr error
		build   func() (*Trace, func() string)
	}{
		{
			name: "empty",
			build: func() (*Trace, func() string) {
				probe := &traceKindProbe{pending: errTraceProbePending}
				trace := NewTraceForListener(&plainTraceListener{probe})
				trace.DetachListener()
				return trace, func() string { return fmt.Sprintf("loads=%d", probe.loads) }
			},
		},
		{
			name:    "hooks",
			wantErr: errTraceProbePending,
			build: func() (*Trace, func() string) {
				loads := 0
				trace := NewTrace(TraceHooks{
					OnLoad:       func(*Cell) { loads++ },
					PendingError: func() error { return errTraceProbePending },
				})
				return trace, func() string { return fmt.Sprintf("loads=%d", loads) }
			},
		},
		{
			name:    "listener",
			wantErr: errTraceProbePending,
			build: func() (*Trace, func() string) {
				probe := &traceKindProbe{pending: errTraceProbePending}
				trace := NewTraceForListener(&plainTraceListener{probe})
				return trace, func() string { return fmt.Sprintf("loads=%d", probe.loads) }
			},
		},
		{
			name:    "load error listener",
			wantErr: errTraceProbePending,
			build: func() (*Trace, func() string) {
				probe := &traceKindProbe{pending: errTraceProbePending}
				trace := NewTraceForListener(&loadErrorTraceListener{probe})
				return trace, func() string { return fmt.Sprintf("loads=%d", probe.loads) }
			},
		},
		{
			name: "read set",
			build: func() (*Trace, func() string) {
				rs := NewReadSet(cl)
				return rs.Trace(), func() string {
					// The recorder has no node to ask, so the observation is
					// membership of the very cell both notifications carry.
					_, recorded := rs.Contains(cl.HashKey())
					return fmt.Sprintf("recorded=%t", recorded)
				}
			},
		},
		{
			name:    "pair",
			wantErr: errTraceProbePending,
			build: func() (*Trace, func() string) {
				first := &traceKindProbe{pending: errTraceProbePending}
				second := &traceKindProbe{pending: errTraceProbeSecond}
				trace := CombineTraces(
					NewTraceForListener(&plainTraceListener{first}),
					NewTraceForListener(&loadErrorTraceListener{second}),
				)
				return trace, func() string {
					return fmt.Sprintf("loads=%d/%d", first.loads, second.loads)
				}
			},
		},
		{
			name:    "combined",
			wantErr: errTraceProbeSecond,
			build: func() (*Trace, func() string) {
				first := &traceKindProbe{}
				second := &traceKindProbe{pending: errTraceProbeSecond}
				third := &traceKindProbe{pending: errTraceProbePending}
				trace := CombineTraces(
					NewTraceForListener(&plainTraceListener{first}),
					NewTraceForListener(&loadErrorTraceListener{second}),
					NewTraceForListener(&plainTraceListener{third}),
				)
				return trace, func() string {
					return fmt.Sprintf("loads=%d/%d/%d", first.loads, second.loads, third.loads)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loadTrace, loadObserved := tc.build()
			loadTrace.NotifyLoad(cl)

			errTrace, errObserved := tc.build()
			gotErr := errTrace.NotifyLoadError(cl)

			if loadObserved() != errObserved() {
				t.Fatalf("NotifyLoad observed %s, NotifyLoadError observed %s", loadObserved(), errObserved())
			}
			if gotErr != tc.wantErr {
				t.Fatalf("NotifyLoadError err = %v, want %v", gotErr, tc.wantErr)
			}
		})
	}
}
