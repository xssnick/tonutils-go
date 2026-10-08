package cell

import "testing"

type detachTestTraceListener struct {
	loads   int
	creates int
}

func (l *detachTestTraceListener) OnLoad(*Cell) {
	l.loads++
}

func (l *detachTestTraceListener) OnCreate() {
	l.creates++
}

func (l *detachTestTraceListener) ChildTrace(int) *Trace {
	return nil
}

func (l *detachTestTraceListener) PendingError() error {
	return nil
}

func TestTraceDetachListenerIsInertAndCanonical(t *testing.T) {
	listener := new(detachTestTraceListener)
	listenerTrace := NewTraceForListener(listener)
	hookLoads := 0
	hookCreates := 0
	hookTrace := NewTrace(TraceHooks{
		OnLoad: func(*Cell) {
			hookLoads++
		},
		OnCreate: func() {
			hookCreates++
		},
	})
	combined := CombineTraces(listenerTrace, hookTrace)
	cl := BeginCell().EndCell()

	combined.NotifyLoad(cl)
	if err := combined.NotifyCreate(); err != nil {
		t.Fatalf("notify create before detach: %v", err)
	}
	if listener.loads != 1 || listener.creates != 1 || hookLoads != 1 || hookCreates != 1 {
		t.Fatalf("events before detach = listener %d/%d hooks %d/%d, want all 1", listener.loads, listener.creates, hookLoads, hookCreates)
	}

	listenerTrace.DetachListener()
	if listenerTrace.backend != nil || listenerTrace.kind != traceKindEmpty {
		t.Fatalf("detached trace still owns listener: %+v", listenerTrace)
	}
	if child := listenerTrace.Child(0); child != nil {
		t.Fatalf("detached child trace = %p, want nil", child)
	}
	if got := CombineTraces(listenerTrace, hookTrace); got != hookTrace {
		t.Fatalf("combine detached trace = %p, want hook trace %p", got, hookTrace)
	}
	if got := CombineTraces(listenerTrace); got != nil {
		t.Fatalf("single detached trace canonicalized to %p, want nil", got)
	}

	combined.NotifyLoad(cl)
	if err := combined.NotifyCreate(); err != nil {
		t.Fatalf("notify create after detach: %v", err)
	}
	if listener.loads != 1 || listener.creates != 1 {
		t.Fatalf("detached listener received events: %d/%d", listener.loads, listener.creates)
	}
	if hookLoads != 2 || hookCreates != 2 {
		t.Fatalf("other combined trace lost events: %d/%d, want 2/2", hookLoads, hookCreates)
	}
}
