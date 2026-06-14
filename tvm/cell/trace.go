package cell

type TraceHooks struct {
	OnLoad       func(*Cell)
	OnCreate     func()
	OnChild      func(refIdx int) *Trace
	PendingError func() error
}

type Trace struct {
	onLoad       func(*Cell)
	onCreate     func()
	onChild      func(refIdx int) *Trace
	pendingError func() error
	usageTree    *CellUsageTree
	usageNode    TraceNode
	parts        []*Trace
}

func NewTrace(hooks TraceHooks) *Trace {
	if hooks.OnLoad == nil && hooks.OnCreate == nil && hooks.OnChild == nil && hooks.PendingError == nil {
		return nil
	}
	return &Trace{
		onLoad:       hooks.OnLoad,
		onCreate:     hooks.OnCreate,
		onChild:      hooks.OnChild,
		pendingError: hooks.PendingError,
	}
}

func CombineTraces(traces ...*Trace) *Trace {
	if len(traces) == 0 {
		return nil
	}
	if len(traces) == 1 {
		return traces[0]
	}

	var buf [4]*Trace
	flat := buf[:0]
	for _, trace := range traces {
		flat = appendTraceUnique(flat, trace)
	}
	return combinedTraceFromFlat(flat)
}

func (t *Trace) WithoutTrace(trace *Trace) *Trace {
	if t == nil || trace == nil {
		return t
	}
	if !traceContainsAny(t, trace) {
		return t
	}

	var buf [4]*Trace
	out := appendTraceWithout(buf[:0], t, trace)
	return combinedTraceFromFlat(out)
}

func (t *Trace) NotifyLoad(c *Cell) {
	if t == nil || c == nil {
		return
	}
	if len(t.parts) > 0 {
		for _, part := range t.parts {
			part.NotifyLoad(c)
		}
		return
	}
	if t.onLoad != nil {
		t.onLoad(c)
	}
}

func (t *Trace) NotifyCreate() error {
	if t == nil {
		return nil
	}
	if len(t.parts) > 0 {
		for _, part := range t.parts {
			if err := part.NotifyCreate(); err != nil {
				return err
			}
		}
		return nil
	}
	if t.onCreate != nil {
		t.onCreate()
	}
	return t.PendingError()
}

func (t *Trace) Child(refIdx int) *Trace {
	if t == nil {
		return nil
	}
	if len(t.parts) > 0 {
		var buf [4]*Trace
		children := buf[:0]
		for _, part := range t.parts {
			children = appendTraceUnique(children, part.Child(refIdx))
		}
		if tracePartsEqual(children, t.parts) {
			return t
		}
		return combinedTraceFromFlat(children)
	}
	if t.onChild != nil {
		return t.onChild(refIdx)
	}
	return t
}

func (t *Trace) PendingError() error {
	if t == nil {
		return nil
	}
	if len(t.parts) > 0 {
		for _, part := range t.parts {
			if err := part.PendingError(); err != nil {
				return err
			}
		}
		return nil
	}
	if t.pendingError != nil {
		return t.pendingError()
	}
	return nil
}

func (t *Trace) usageNodeFor(tree *CellUsageTree) (TraceNode, bool) {
	if t == nil || tree == nil {
		return 0, false
	}
	if len(t.parts) > 0 {
		for _, part := range t.parts {
			if node, ok := part.usageNodeFor(tree); ok {
				return node, true
			}
		}
		return 0, false
	}
	if t.usageTree == tree && t.usageNode != 0 {
		return t.usageNode, true
	}
	return 0, false
}

func appendTraceUnique(out []*Trace, trace *Trace) []*Trace {
	if trace == nil {
		return out
	}
	if len(trace.parts) > 0 {
		for _, part := range trace.parts {
			out = appendTraceUnique(out, part)
		}
		return out
	}
	if traceInList(out, trace) {
		return out
	}
	return append(out, trace)
}

func appendTraceWithout(out []*Trace, trace, excluded *Trace) []*Trace {
	if trace == nil {
		return out
	}
	if len(trace.parts) > 0 {
		for _, part := range trace.parts {
			out = appendTraceWithout(out, part, excluded)
		}
		return out
	}
	if traceContainsLeaf(excluded, trace) || traceInList(out, trace) {
		return out
	}
	return append(out, trace)
}

func combinedTraceFromFlat(flat []*Trace) *Trace {
	switch len(flat) {
	case 0:
		return nil
	case 1:
		return flat[0]
	default:
		parts := make([]*Trace, len(flat))
		copy(parts, flat)
		return &Trace{parts: parts}
	}
}

func traceInList(list []*Trace, trace *Trace) bool {
	for _, part := range list {
		if part == trace {
			return true
		}
	}
	return false
}

func traceContainsLeaf(trace, leaf *Trace) bool {
	if trace == nil || leaf == nil {
		return false
	}
	if len(trace.parts) == 0 {
		return trace == leaf
	}
	for _, part := range trace.parts {
		if traceContainsLeaf(part, leaf) {
			return true
		}
	}
	return false
}

func traceContainsAny(trace, excluded *Trace) bool {
	if trace == nil || excluded == nil {
		return false
	}
	if len(trace.parts) == 0 {
		return traceContainsLeaf(excluded, trace)
	}
	for _, part := range trace.parts {
		if traceContainsAny(part, excluded) {
			return true
		}
	}
	return false
}

func tracePartsEqual(a, b []*Trace) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
