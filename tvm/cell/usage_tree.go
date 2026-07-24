package cell

import (
	"sync"
	"sync/atomic"
)

type TraceNode uint32

const (
	usageTreeInitialChunkSize = 64
	usageTreeChunkBits        = 8
	usageTreeChunkSize        = 1 << usageTreeChunkBits
	usageTreeChunkMask        = usageTreeChunkSize - 1
)

type usageTreeInitialChunk [usageTreeInitialChunkSize]usageTreeNode
type usageTreeChunk [usageTreeChunkSize]usageTreeNode

// usageTreeNode is a slot in the tree arena. Nodes are published to other
// goroutines either as the root or through a CompareAndSwap on the parent's
// children entry, which orders the plain field writes done at allocation time
// (trace, parent) before any concurrent reader can obtain the node id.
type usageTreeNode struct {
	// trace is the usage-tree trace of this node, embedded to avoid a
	// per-node allocation. Its address is stable: chunks are never moved
	// once allocated, only the chunk directory is copied on growth.
	trace    Trace
	parent   TraceNode
	children [4]atomic.Uint32
	cell     atomic.Pointer[Cell]
	loaded   atomic.Bool
	// marked is only read/written by the single-threaded proof/update build
	// phases (SetMark, MarkPath, IsLoaded with useMark), never during
	// concurrent load tracking.
	marked bool
}

// CellUsageTree records which cells were loaded through traced wrappers so
// that Merkle proofs and updates can later be built from the visited paths.
//
// Concurrent cell loads through traces of one tree are safe: node creation,
// load marking and loaded-cell caching are lock-free or briefly locked.
// The proof/update building phase (CreateUsageProof, CreateMerkleUpdate,
// SetMark/MarkPath/SetUseMarkForIsLoaded) is not synchronized against
// concurrent loads and must run after all traced readers are done.
type CellUsageTree struct {
	initial  usageTreeInitialChunk
	chunks   atomic.Pointer[[]*usageTreeChunk]
	nextNode atomic.Uint32
	growMu   sync.Mutex

	loadedMu    sync.Mutex
	loadedCells map[Hash]*Cell

	useMark     bool
	ignoreLoads atomic.Int32
	onLoadFn    func(*Cell)
}

func NewCellUsageTree() *CellUsageTree {
	t := &CellUsageTree{
		loadedCells: map[Hash]*Cell{},
	}
	var chunks []*usageTreeChunk
	t.chunks.Store(&chunks)
	t.nextNode.Store(2)
	root := t.node(1)
	root.trace.usageTree = t
	root.trace.usageNode = 1
	return t
}

func (t *CellUsageTree) node(id TraceNode) *usageTreeNode {
	if id < usageTreeInitialChunkSize {
		return &t.initial[id]
	}
	id -= usageTreeInitialChunkSize
	chunks := *t.chunks.Load()
	return &chunks[id>>usageTreeChunkBits][id&usageTreeChunkMask]
}

func (t *CellUsageTree) RootNode() TraceNode {
	return 1
}

// NodeCount returns the number of arena nodes allocated so far — an upper
// bound on the distinct cells a proof built from this tree can include,
// useful for presizing build state.
func (t *CellUsageTree) NodeCount() int {
	if t == nil {
		return 0
	}
	return int(t.nextNode.Load()) - 1
}

func (t *CellUsageTree) RootTrace() *Trace {
	return t.Trace(t.RootNode())
}

func (t *CellUsageTree) Trace(node TraceNode) *Trace {
	if t == nil || !t.validNode(node) {
		return nil
	}
	return &t.node(node).trace
}

// SetCellLoadCallback registers fn to be called on the first load of every
// node. It must be set before loads start; fn may be called concurrently when
// traced readers run on multiple goroutines.
func (t *CellUsageTree) SetCellLoadCallback(fn func(*Cell)) {
	t.onLoadFn = fn
}

func (t *CellUsageTree) SetIgnoreLoads(ignore bool) {
	if ignore {
		t.ignoreLoads.Add(1)
		return
	}
	for {
		cur := t.ignoreLoads.Load()
		if cur <= 0 {
			return
		}
		if t.ignoreLoads.CompareAndSwap(cur, cur-1) {
			return
		}
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
	if t == nil || t.ignoreLoads.Load() > 0 || !t.validNode(node) {
		return
	}

	n := t.node(node)
	t.storeLoadedCell(n, c)
	if n.loaded.Load() || n.loaded.Swap(true) {
		return
	}
	if t.onLoadFn != nil {
		t.onLoadFn(c)
	}
}

func (t *CellUsageTree) loadedCell(node TraceNode) (*Cell, bool) {
	if t == nil || !t.validNode(node) {
		return nil, false
	}
	c := t.node(node).cell.Load()
	return c, c != nil
}

func (t *CellUsageTree) loadedCellByHash(hash Hash) (*Cell, bool) {
	if t == nil {
		return nil, false
	}
	t.loadedMu.Lock()
	c := t.loadedCells[hash]
	t.loadedMu.Unlock()
	return c, c != nil
}

func (t *CellUsageTree) storeLoadedCell(n *usageTreeNode, c *Cell) {
	if c == nil || c.IsLazy() || n.cell.Load() != nil {
		return
	}
	if !n.cell.CompareAndSwap(nil, c) {
		return
	}
	t.loadedMu.Lock()
	if t.loadedCells == nil {
		t.loadedCells = map[Hash]*Cell{}
	}
	t.loadedCells[c.HashKey()] = c
	t.loadedMu.Unlock()
}

func (t *CellUsageTree) IsLoaded(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	if t.useMark {
		return t.node(node).marked
	}
	return t.node(node).loaded.Load()
}

func (t *CellUsageTree) HasMark(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	return t.node(node).marked
}

func (t *CellUsageTree) SetMark(node TraceNode, mark bool) {
	if t == nil || !t.validNode(node) {
		return
	}
	t.node(node).marked = mark
}

func (t *CellUsageTree) MarkPath(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	for cur := t.node(node).parent; cur != 0; cur = t.node(cur).parent {
		n := t.node(cur)
		if n.marked {
			break
		}
		n.marked = true
	}
	return true
}

func (t *CellUsageTree) marksSnapshot() []bool {
	total := t.nextNode.Load()
	out := make([]bool, total)
	for id := TraceNode(1); id < TraceNode(total); id++ {
		out[id] = t.node(id).marked
	}
	return out
}

func (t *CellUsageTree) restoreMarks(prev []bool) {
	total := t.nextNode.Load()
	for id := TraceNode(1); id < TraceNode(total); id++ {
		t.node(id).marked = int(id) < len(prev) && prev[id]
	}
}

func (t *CellUsageTree) Parent(node TraceNode) TraceNode {
	if t == nil || !t.validNode(node) {
		return 0
	}
	return t.node(node).parent
}

func (t *CellUsageTree) GetChild(node TraceNode, refIdx int) TraceNode {
	if t == nil || !t.validNode(node) || refIdx < 0 || refIdx >= 4 {
		return 0
	}
	return TraceNode(t.node(node).children[refIdx].Load())
}

func (t *CellUsageTree) CreateChild(node TraceNode, refIdx int) TraceNode {
	if t == nil || !t.validNode(node) || refIdx < 0 || refIdx >= 4 {
		return 0
	}
	n := t.node(node)
	if child := n.children[refIdx].Load(); child != 0 {
		return TraceNode(child)
	}

	child := t.allocNode(node)
	if n.children[refIdx].CompareAndSwap(0, uint32(child)) {
		return child
	}
	// Another goroutine created this child concurrently; the slot allocated
	// above stays unused in the arena.
	return TraceNode(n.children[refIdx].Load())
}

func (t *CellUsageTree) allocNode(parent TraceNode) TraceNode {
	id := TraceNode(t.nextNode.Add(1) - 1)
	t.ensureChunk(id)
	n := t.node(id)
	n.parent = parent
	n.trace.usageTree = t
	n.trace.usageNode = id
	return id
}

func (t *CellUsageTree) ensureChunk(id TraceNode) {
	if id < usageTreeInitialChunkSize {
		return
	}
	chunkIdx := int((id - usageTreeInitialChunkSize) >> usageTreeChunkBits)
	if chunkIdx < len(*t.chunks.Load()) {
		return
	}
	t.growMu.Lock()
	defer t.growMu.Unlock()
	cur := *t.chunks.Load()
	if chunkIdx < len(cur) {
		return
	}
	nextLen := chunkIdx + 1
	var next []*usageTreeChunk
	if nextLen <= cap(cur) {
		next = cur[:nextLen]
	} else {
		next = make([]*usageTreeChunk, nextLen, max(nextLen, 4, cap(cur)*2))
		copy(next, cur)
	}
	for i := len(cur); i < nextLen; i++ {
		next[i] = new(usageTreeChunk)
	}
	t.chunks.Store(&next)
}

func (t *CellUsageTree) validNode(node TraceNode) bool {
	return node != 0 && uint32(node) < t.nextNode.Load()
}
