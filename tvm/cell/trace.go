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
	var flat []*Trace
	seen := map[*Trace]struct{}{}
	var appendTrace func(*Trace)
	appendTrace = func(trace *Trace) {
		if trace == nil {
			return
		}
		if len(trace.parts) > 0 {
			for _, part := range trace.parts {
				appendTrace(part)
			}
			return
		}
		if _, ok := seen[trace]; ok {
			return
		}
		seen[trace] = struct{}{}
		flat = append(flat, trace)
	}

	for _, trace := range traces {
		appendTrace(trace)
	}
	if len(flat) == 0 {
		return nil
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return &Trace{parts: flat}
}

func (t *Trace) WithoutTrace(trace *Trace) *Trace {
	if t == nil || trace == nil {
		return t
	}

	excluded := map[*Trace]struct{}{}
	for _, part := range trace.flatten() {
		excluded[part] = struct{}{}
	}

	var out []*Trace
	for _, part := range t.flatten() {
		if _, ok := excluded[part]; ok {
			continue
		}
		out = append(out, part)
	}
	return CombineTraces(out...)
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
		children := make([]*Trace, 0, len(t.parts))
		for _, part := range t.parts {
			children = append(children, part.Child(refIdx))
		}
		return CombineTraces(children...)
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

func (t *Trace) flatten() []*Trace {
	if t == nil {
		return nil
	}
	if len(t.parts) == 0 {
		return []*Trace{t}
	}
	var out []*Trace
	for _, part := range t.parts {
		out = append(out, part.flatten()...)
	}
	return out
}

func (t *Trace) usageNodeFor(tree *CellUsageTree) (TraceNode, bool) {
	if t == nil || tree == nil {
		return 0, false
	}
	for _, part := range t.flatten() {
		if part.usageTree == tree && part.usageNode != 0 {
			return part.usageNode, true
		}
	}
	return 0, false
}
