package cell

type TraceHooks struct {
	OnLoad       func(*Cell)
	OnCreate     func()
	OnChild      func(refIdx int) *Trace
	PendingError func() error
}

// TraceListener receives the same events as TraceHooks through interface
// dispatch, letting a single object back a Trace without allocating a closure
// per hook.
type TraceListener interface {
	OnLoad(*Cell)
	OnCreate()
	ChildTrace(refIdx int) *Trace
	PendingError() error
}

type traceKind uint8

const (
	traceKindEmpty traceKind = iota
	traceKindHooks
	traceKindListener
	traceKindUsage
	traceKindPair
	traceKindCombined
)

type traceParts []*Trace

// Trace keeps the dispatch tag and its backend in a compact fixed-size value.
// General hook and combined traces keep their larger state in the optional
// backend, while the common usage-tree trace stores only a tree pointer and
// node id. Usage traces are embedded in stable arena slots; callers may retain
// their exact addresses while the arena grows.
type Trace struct {
	backend   any
	usageNode TraceNode
	kind      traceKind
}

// These owners keep the uncommon large backends beside Trace in the same
// allocation. Storing the owner pointer in backend avoids a second heap box
// for TraceHooks or a slice header while Trace itself stays compact.
type traceHooksState struct {
	trace Trace
	hooks TraceHooks
}

type traceCombinedState struct {
	trace Trace
	parts traceParts
}

// tracePairState keeps the overwhelmingly common usage+gas combination in one
// allocation. General combinations still use traceCombinedState, but a pair
// does not need a separately allocated slice backing array.
type tracePairState struct {
	trace  Trace
	first  *Trace
	second *Trace
}

func NewTrace(hooks TraceHooks) *Trace {
	if hooks.OnLoad == nil && hooks.OnCreate == nil && hooks.OnChild == nil && hooks.PendingError == nil {
		return nil
	}
	state := &traceHooksState{hooks: hooks}
	state.trace.backend = state
	state.trace.kind = traceKindHooks
	return &state.trace
}

// NewTraceForListener wires a Trace directly to the listener, allocating only
// the Trace itself when the listener is already pointer-backed.
func NewTraceForListener(l TraceListener) *Trace {
	if l == nil {
		return nil
	}
	return &Trace{
		backend: l,
		kind:    traceKindListener,
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
	for _, trace := range traces {
		if tracePartsEqualTrace(flat, trace) {
			return trace
		}
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

	switch t.kind {
	case traceKindHooks:
		if fn := t.backend.(*traceHooksState).hooks.OnLoad; fn != nil {
			fn(c)
		}
	case traceKindListener:
		t.backend.(TraceListener).OnLoad(c)
	case traceKindUsage:
		t.backend.(*CellUsageTree).OnLoad(t.usageNode, c)
	case traceKindPair:
		pair := t.backend.(*tracePairState)
		pair.first.NotifyLoad(c)
		pair.second.NotifyLoad(c)
	case traceKindCombined:
		for _, part := range t.backend.(*traceCombinedState).parts {
			part.NotifyLoad(c)
		}
	}
}

func (t *Trace) NotifyCreate() error {
	if t == nil {
		return nil
	}

	switch t.kind {
	case traceKindHooks:
		hooks := &t.backend.(*traceHooksState).hooks
		if hooks.OnCreate != nil {
			hooks.OnCreate()
		}
		if hooks.PendingError != nil {
			return hooks.PendingError()
		}
	case traceKindListener:
		listener := t.backend.(TraceListener)
		listener.OnCreate()
		return listener.PendingError()
	case traceKindPair:
		pair := t.backend.(*tracePairState)
		if err := pair.first.NotifyCreate(); err != nil {
			return err
		}
		return pair.second.NotifyCreate()
	case traceKindCombined:
		for _, part := range t.backend.(*traceCombinedState).parts {
			if err := part.NotifyCreate(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Trace) Child(refIdx int) *Trace {
	if t == nil {
		return nil
	}

	switch t.kind {
	case traceKindHooks:
		if fn := t.backend.(*traceHooksState).hooks.OnChild; fn != nil {
			return fn(refIdx)
		}
	case traceKindListener:
		return t.backend.(TraceListener).ChildTrace(refIdx)
	case traceKindUsage:
		tree := t.backend.(*CellUsageTree)
		return tree.Trace(tree.CreateChild(t.usageNode, refIdx))
	case traceKindPair:
		pair := t.backend.(*tracePairState)
		var buf [4]*Trace
		children := appendTraceUnique(buf[:0], pair.first.Child(refIdx))
		children = appendTraceUnique(children, pair.second.Child(refIdx))
		if tracePartsEqualTrace(children, t) {
			return t
		}
		return combinedTraceFromFlat(children)
	case traceKindCombined:
		parts := t.backend.(*traceCombinedState).parts
		var buf [4]*Trace
		children := buf[:0]
		for _, part := range parts {
			children = appendTraceUnique(children, part.Child(refIdx))
		}
		if tracePartsEqual(children, parts) {
			return t
		}
		return combinedTraceFromFlat(children)
	}
	return t
}

func (t *Trace) PendingError() error {
	if t == nil {
		return nil
	}

	switch t.kind {
	case traceKindHooks:
		if fn := t.backend.(*traceHooksState).hooks.PendingError; fn != nil {
			return fn()
		}
	case traceKindListener:
		return t.backend.(TraceListener).PendingError()
	case traceKindPair:
		pair := t.backend.(*tracePairState)
		if err := pair.first.PendingError(); err != nil {
			return err
		}
		return pair.second.PendingError()
	case traceKindCombined:
		for _, part := range t.backend.(*traceCombinedState).parts {
			if err := part.PendingError(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Trace) initUsage(tree *CellUsageTree, node TraceNode) {
	t.backend = tree
	t.usageNode = node
	t.kind = traceKindUsage
}

func (t *Trace) usageNodeFor(tree *CellUsageTree) (TraceNode, bool) {
	if t == nil || tree == nil {
		return 0, false
	}
	if t.kind == traceKindPair {
		pair := t.backend.(*tracePairState)
		if node, ok := pair.first.usageNodeFor(tree); ok {
			return node, true
		}
		return pair.second.usageNodeFor(tree)
	}
	if t.kind == traceKindCombined {
		for _, part := range t.backend.(*traceCombinedState).parts {
			if node, ok := part.usageNodeFor(tree); ok {
				return node, true
			}
		}
		return 0, false
	}
	if t.kind == traceKindUsage && t.backend == tree && t.usageNode != 0 {
		return t.usageNode, true
	}
	return 0, false
}

func appendTraceUnique(out []*Trace, trace *Trace) []*Trace {
	if trace == nil {
		return out
	}
	if trace.kind == traceKindPair {
		pair := trace.backend.(*tracePairState)
		out = appendTraceUnique(out, pair.first)
		return appendTraceUnique(out, pair.second)
	}
	if trace.kind == traceKindCombined {
		for _, part := range trace.backend.(*traceCombinedState).parts {
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
	if trace.kind == traceKindPair {
		pair := trace.backend.(*tracePairState)
		out = appendTraceWithout(out, pair.first, excluded)
		return appendTraceWithout(out, pair.second, excluded)
	}
	if trace.kind == traceKindCombined {
		for _, part := range trace.backend.(*traceCombinedState).parts {
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
	case 2:
		state := &tracePairState{
			first:  flat[0],
			second: flat[1],
		}
		state.trace.backend = state
		state.trace.kind = traceKindPair
		return &state.trace
	default:
		parts := make(traceParts, len(flat))
		copy(parts, flat)
		state := &traceCombinedState{parts: parts}
		state.trace.backend = state
		state.trace.kind = traceKindCombined
		return &state.trace
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
	if trace.kind == traceKindPair {
		pair := trace.backend.(*tracePairState)
		return traceContainsLeaf(pair.first, leaf) || traceContainsLeaf(pair.second, leaf)
	}
	if trace.kind != traceKindCombined {
		return trace == leaf
	}
	for _, part := range trace.backend.(*traceCombinedState).parts {
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
	if trace.kind == traceKindPair {
		pair := trace.backend.(*tracePairState)
		return traceContainsAny(pair.first, excluded) || traceContainsAny(pair.second, excluded)
	}
	if trace.kind != traceKindCombined {
		return traceContainsLeaf(excluded, trace)
	}
	for _, part := range trace.backend.(*traceCombinedState).parts {
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

func tracePartsEqualTrace(parts []*Trace, trace *Trace) bool {
	if trace == nil {
		return len(parts) == 0
	}
	switch trace.kind {
	case traceKindPair:
		if len(parts) != 2 {
			return false
		}
		pair := trace.backend.(*tracePairState)
		return parts[0] == pair.first && parts[1] == pair.second
	case traceKindCombined:
		return tracePartsEqual(parts, trace.backend.(*traceCombinedState).parts)
	default:
		return len(parts) == 1 && parts[0] == trace
	}
}
