package cell

type TraceNode uint32

type usageTreeNode struct {
	parent   TraceNode
	children [4]TraceNode
	trace    *Trace
	loaded   bool
	marked   bool
	cell     *Cell
}

type CellUsageTree struct {
	nodes       []usageTreeNode
	loadedCells map[Hash]*Cell
	useMark     bool
	ignoreLoads int
	onLoadFn    func(*Cell)
}

func NewCellUsageTree() *CellUsageTree {
	return &CellUsageTree{
		nodes:       make([]usageTreeNode, 2),
		loadedCells: map[Hash]*Cell{},
	}
}

func (t *CellUsageTree) RootNode() TraceNode {
	return 1
}

func (t *CellUsageTree) RootTrace() *Trace {
	return t.Trace(t.RootNode())
}

func (t *CellUsageTree) Trace(node TraceNode) *Trace {
	if t == nil || !t.validNode(node) {
		return nil
	}
	if t.nodes[node].trace != nil {
		return t.nodes[node].trace
	}

	trace := NewTrace(TraceHooks{
		OnLoad: func(c *Cell) {
			t.OnLoad(node, c)
		},
		OnChild: func(refIdx int) *Trace {
			child := t.CreateChild(node, refIdx)
			return t.Trace(child)
		},
	})
	trace.usageTree = t
	trace.usageNode = node
	t.nodes[node].trace = trace
	return trace
}

func (t *CellUsageTree) SetCellLoadCallback(fn func(*Cell)) {
	t.onLoadFn = fn
}

func (t *CellUsageTree) SetIgnoreLoads(ignore bool) {
	if ignore {
		t.ignoreLoads++
	} else if t.ignoreLoads > 0 {
		t.ignoreLoads--
	}
}

func (t *CellUsageTree) SetUseMarkForIsLoaded(useMark bool) {
	t.useMark = useMark
}

func (t *CellUsageTree) NodeForCell(c *Cell) (TraceNode, bool) {
	if c == nil {
		return 0, false
	}
	return t.NodeForTrace(c.Trace())
}

func (t *CellUsageTree) NodeForTrace(trace *Trace) (TraceNode, bool) {
	if t == nil {
		return 0, false
	}
	return trace.usageNodeFor(t)
}

func (t *CellUsageTree) OnLoad(node TraceNode, c *Cell) {
	if t == nil || t.ignoreLoads > 0 || !t.validNode(node) {
		return
	}

	wasLoaded := t.nodes[node].loaded
	t.storeLoadedCell(node, c)
	if wasLoaded {
		return
	}

	t.nodes[node].loaded = true
	if t.onLoadFn != nil {
		t.onLoadFn(c)
	}
}

func (t *CellUsageTree) loadedCell(node TraceNode) (*Cell, bool) {
	if t == nil || !t.validNode(node) {
		return nil, false
	}
	c := t.nodes[node].cell
	return c, c != nil
}

func (t *CellUsageTree) loadedCellByHash(hash Hash) (*Cell, bool) {
	if t == nil || t.loadedCells == nil {
		return nil, false
	}
	c := t.loadedCells[hash]
	return c, c != nil
}

func (t *CellUsageTree) storeLoadedCell(node TraceNode, c *Cell) {
	if c == nil || c.IsLazy() {
		return
	}
	t.nodes[node].cell = c
	if t.loadedCells == nil {
		t.loadedCells = map[Hash]*Cell{}
	}
	t.loadedCells[c.HashKey()] = c
}

func (t *CellUsageTree) IsLoaded(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	if t.useMark {
		return t.nodes[node].marked
	}
	return t.nodes[node].loaded
}

func (t *CellUsageTree) HasMark(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	return t.nodes[node].marked
}

func (t *CellUsageTree) SetMark(node TraceNode, mark bool) {
	if t == nil || !t.validNode(node) {
		return
	}
	t.nodes[node].marked = mark
}

func (t *CellUsageTree) MarkPath(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	for cur := t.nodes[node].parent; cur != 0; cur = t.nodes[cur].parent {
		if t.nodes[cur].marked {
			break
		}
		t.nodes[cur].marked = true
	}
	return true
}

func (t *CellUsageTree) Parent(node TraceNode) TraceNode {
	if t == nil || !t.validNode(node) {
		return 0
	}
	return t.nodes[node].parent
}

func (t *CellUsageTree) GetChild(node TraceNode, refIdx int) TraceNode {
	if t == nil || !t.validNode(node) || refIdx < 0 || refIdx >= 4 {
		return 0
	}
	return t.nodes[node].children[refIdx]
}

func (t *CellUsageTree) CreateChild(node TraceNode, refIdx int) TraceNode {
	if t == nil || !t.validNode(node) || refIdx < 0 || refIdx >= 4 {
		return 0
	}
	if child := t.nodes[node].children[refIdx]; child != 0 {
		return child
	}
	child := TraceNode(len(t.nodes))
	t.nodes = append(t.nodes, usageTreeNode{parent: node})
	t.nodes[node].children[refIdx] = child
	return child
}

func (t *CellUsageTree) validNode(node TraceNode) bool {
	return node != 0 && int(node) < len(t.nodes)
}
