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
	usageCellIndexMinSlots    = 8
)

const (
	usageNodeLoaded uint32 = 1 << iota
	usageNodeMarked
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
	trace Trace
	cell  atomic.Pointer[Cell]

	children [4]atomic.Uint32
	parent   TraceNode
	// state combines the concurrently updated loaded flag and the temporary
	// proof-build mark without adding padding to every arena node.
	state atomic.Uint32
}

// usageTreeCellIndex is built only if a proof/update needs to resolve a cell
// by hash. Each open-addressed slot packs a 32-bit hash fingerprint and a
// 32-bit arena node id; lookups verify the complete hash on the node's cell,
// so fingerprint collisions cannot return a wrong cell.
type usageTreeCellIndex struct {
	tree  *CellUsageTree
	slots []uint64
}

// CellUsageTree records which cells were loaded through traced wrappers so
// that Merkle proofs and updates can later be built from the visited paths.
//
// Concurrent cell loads through traces of one tree are safe: node creation,
// load marking and loaded-cell caching use atomics. Hash lookup state is built
// after tracking, only when a proof/update actually needs it.
// The proof/update building phase (CreateUsageProof, CreateMerkleUpdate,
// SetMark/MarkPath/SetUseMarkForIsLoaded) is not synchronized against
// concurrent loads and must run after all traced readers are done.
type CellUsageTree struct {
	initial  usageTreeInitialChunk
	chunks   atomic.Pointer[[]*usageTreeChunk]
	nextNode atomic.Uint32
	growMu   sync.Mutex

	useMark     bool
	ignoreLoads atomic.Int32
	onLoadFn    func(*Cell)
}

func NewCellUsageTree() *CellUsageTree {
	t := new(CellUsageTree)
	var chunks []*usageTreeChunk
	t.chunks.Store(&chunks)
	t.nextNode.Store(2)
	root := t.node(1)
	root.trace.initUsage(t, 1)
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

// NodeCount returns the number of arena nodes allocated so far. It includes
// unpublished slots lost to concurrent child-creation races, so it can be much
// larger than the set of loaded cells used by a proof.
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
	if c != nil && !c.IsLazy() && n.cell.Load() == nil {
		n.cell.CompareAndSwap(nil, c)
	}
	if n.state.Load()&usageNodeLoaded != 0 || n.state.Or(usageNodeLoaded)&usageNodeLoaded != 0 {
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

func newUsageTreeCellIndex(tree *CellUsageTree) usageTreeCellIndex {
	return usageTreeCellIndex{tree: tree}
}

func (i *usageTreeCellIndex) loadedCellByHash(hash Hash) (*Cell, bool) {
	if i == nil || i.tree == nil {
		return nil, false
	}
	if i.slots == nil {
		i.build()
	}
	if len(i.slots) == 0 {
		return nil, false
	}

	fingerprint := usageCellFingerprint(hash)
	mask := len(i.slots) - 1
	pos := int(fingerprint) & mask
	for range i.slots {
		entry := i.slots[pos]
		if entry == 0 {
			return nil, false
		}
		if uint32(entry>>32) == fingerprint {
			node := TraceNode(uint32(entry))
			if c := i.tree.node(node).cell.Load(); c != nil && c.HashKey() == hash {
				return c, true
			}
		}
		pos = (pos + 1) & mask
	}
	return nil, false
}

func (i *usageTreeCellIndex) build() {
	total := TraceNode(i.tree.nextNode.Load())
	used := 0
	for node := TraceNode(1); node < total; node++ {
		c := i.tree.node(node).cell.Load()
		if c == nil {
			continue
		}
		if i.slots == nil {
			i.slots = make([]uint64, usageCellIndexMinSlots)
		}

		hash := c.HashKey()
		fingerprint := usageCellFingerprint(hash)
		mask := len(i.slots) - 1
		pos := int(fingerprint) & mask
		duplicate := false
		for i.slots[pos] != 0 {
			entry := i.slots[pos]
			if uint32(entry>>32) == fingerprint {
				existing := i.tree.node(TraceNode(uint32(entry))).cell.Load()
				if existing.HashKey() == hash {
					duplicate = true
				}
			}
			if duplicate {
				break
			}
			pos = (pos + 1) & mask
		}
		if duplicate {
			continue
		}
		if used*2 >= len(i.slots) {
			i.grow()
			mask = len(i.slots) - 1
			pos = int(fingerprint) & mask
			for i.slots[pos] != 0 {
				pos = (pos + 1) & mask
			}
		}
		i.slots[pos] = uint64(fingerprint)<<32 | uint64(node)
		used++
	}
	if i.slots == nil {
		i.slots = []uint64{}
	}
}

func (i *usageTreeCellIndex) grow() {
	old := i.slots
	i.slots = make([]uint64, len(old)*2)
	mask := len(i.slots) - 1
	for _, entry := range old {
		if entry == 0 {
			continue
		}
		pos := int(uint32(entry>>32)) & mask
		for i.slots[pos] != 0 {
			pos = (pos + 1) & mask
		}
		i.slots[pos] = entry
	}
}

func usageCellFingerprint(hash Hash) uint32 {
	return uint32(hash[0]) |
		uint32(hash[1])<<8 |
		uint32(hash[2])<<16 |
		uint32(hash[3])<<24
}

func (t *CellUsageTree) IsLoaded(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	if t.useMark {
		return t.node(node).state.Load()&usageNodeMarked != 0
	}
	return t.node(node).state.Load()&usageNodeLoaded != 0
}

func (t *CellUsageTree) HasMark(node TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	return t.node(node).state.Load()&usageNodeMarked != 0
}

func (t *CellUsageTree) SetMark(node TraceNode, mark bool) {
	if t == nil || !t.validNode(node) {
		return
	}
	if mark {
		t.node(node).state.Or(usageNodeMarked)
		return
	}
	t.node(node).state.And(^usageNodeMarked)
}

func (t *CellUsageTree) MarkPath(node TraceNode) bool {
	return t.markPath(node, nil)
}

func (t *CellUsageTree) markPath(node TraceNode, journal *[]TraceNode) bool {
	if t == nil || !t.validNode(node) {
		return false
	}
	for cur := t.node(node).parent; cur != 0; cur = t.node(cur).parent {
		n := t.node(cur)
		if n.state.Or(usageNodeMarked)&usageNodeMarked != 0 {
			break
		}
		if journal != nil {
			*journal = append(*journal, cur)
		}
	}
	return true
}

func (t *CellUsageTree) clearMarkedJournal(journal []TraceNode) {
	for _, node := range journal {
		t.node(node).state.And(^usageNodeMarked)
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
	n.trace.initUsage(t, id)
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
