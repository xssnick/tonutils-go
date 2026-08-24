package cell

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const sourceGraphBoundaryShards = 32

// sourceGraph is the source tree as the reads reached it: the subtrees the
// source really holds, the edges the walk descended to find them, and the order
// it finished them in.
//
// Building an update asks the source two questions, and they used to be two
// walks over the same nodes. The first is membership — which subtrees the source
// holds — and it is the only evidence that lets the destination replace one with
// a boundary onto the predecessor. The second is shape — which cells the source
// proof must carry so those boundaries are exposed. The second walk re-hashed
// every cell, re-probed the read set and re-stripped traces the first walk had
// already stripped, all to rediscover a structure the first walk had in hand.
//
// Nodes are numbered as they finish, so the slice is in post-order: a node's
// children always precede it. That turns the second question into one forward
// loop over a slice — no recursion, no hashing, no lookups.
type sourceGraph struct {
	nodes []sourceNode
	edges []int32
	// index maps a hash to its node. Read cells ride the read set's own table:
	// readPos[shard][entryPos] holds the node index plus one, keyed by the
	// position lookupPos returns, so the graph never hashes a 32-byte key the
	// read set has already hashed. Only unread references — the fringe the walk
	// stops at — fall back to a map, and they are the small minority.
	rs       *ReadSet
	readPos  [readSetShards][]int32
	byUnread map[Hash]int32
	// boundaries counts the nodes the destination replaced, so a source with
	// none skips the fold entirely.
	boundaries int

	// lazyInstance records that the source handed this walk an unresolved
	// placeholder. It is not a property of the block: the same predecessor is
	// lazy or resident depending on what earlier work happened to materialize,
	// and cells rebuilt from a placeholder are not interchangeable with cells
	// rebuilt from the resolved instance. Only the source proof's builder reads
	// it, to stay on the one path whose output does not depend on which of the
	// two it arrived through. See buildRecordedProofBody's caller.
	lazyInstance       atomic.Bool
	parallel           bool
	parallelBoundaries atomic.Int64
	boundaryMu         [sourceGraphBoundaryShards]sync.Mutex
}

// sourceNode is one subtree the reads reached.
type sourceNode struct {
	// cell is what the source holds here: for a cell that was read, the recorded
	// instance; for one that was not read, the reference the walk arrived at.
	cell *Cell
	// applied is the trace-free form returned when the destination prunes onto
	// this subtree. Most graph cells are never boundaries, so it is built lazily
	// rather than copying every recorded source cell during the graph walk.
	applied *Cell
	// firstRef and refCount address this node's edges in the shared slice. Every
	// reference is an edge, including one to a subtree an earlier path already
	// finished: which position a shared subtree is reached through is decided
	// later, once the destination has said where the boundaries are, and that
	// decision needs to see all the candidates.
	firstRef int32
	refCount int32
	// claimedBy is the parent that took responsibility for reaching this subtree,
	// filled by the claim pass. A subtree sitting at several positions is reached
	// through exactly one of them.
	claimedBy int32
	read      bool
	// boundary is set by the destination walk when it replaces this subtree with
	// a pruned branch.
	boundary bool
	// reach says a boundary sits at or below this subtree, so a path to it has to
	// exist somewhere in the source proof.
	reach bool
	// covered says the claim pass has already found this subtree a way in.
	covered bool
	// kept is the answer: the source proof carries this cell as a body.
	kept bool
}

// buildSourceGraph walks the source once, descending only through cells that
// were read.
//
// It separates two things the recorder cannot tell apart on its own. A hash in
// the read set means "this content was parsed through the recording trace",
// which includes cells the transition rebuilt and then read back. A node here
// means "the source tree holds this subtree", which is what an update needs
// before it may prune a destination subtree onto the predecessor.
func (rs *ReadSet) buildSourceGraph(workers int) (*sourceGraph, error) {
	// The record's size is already an over-estimate of this graph, not an
	// under-estimate to be doubled: a node exists for a subtree the source holds,
	// while the record also carries cells the transition rebuilt and read back,
	// which the source never held. Measured on a mainnet block the graph is about
	// seven tenths of the record — so doubling it allocated a nodes array, an
	// edges array and a map for three times the population that arrives.
	g := newSourceGraphFor(rs)
	hint := rs.Size()
	if rs.source == nil {
		return g, nil
	}
	if workers > 1 && hint >= sourceGraphParallelMinCells {
		done, err := rs.buildSourceGraphParallel(g, workers)
		if err != nil {
			return nil, err
		}
		if done {
			return g, nil
		}
		// The parallel build declined — too few frontier subtrees, or a depth
		// race — and touched nothing, so the sequential walk starts clean.
		if len(g.nodes) != 0 {
			return nil, fmt.Errorf("declined parallel source walk left %d nodes behind", len(g.nodes))
		}
	}
	if _, _, err := g.visit(rs, rs.source, 0); err != nil {
		return nil, err
	}
	return g, nil
}

// newSourceGraphFor opens an empty graph keyed to the read set's current table
// generation.
func newSourceGraphFor(rs *ReadSet) *sourceGraph {
	hint := rs.Size()
	g := &sourceGraph{
		nodes:    make([]sourceNode, 0, hint),
		edges:    make([]int32, 0, hint),
		rs:       rs,
		byUnread: make(map[Hash]int32),
	}
	for i := range g.readPos {
		g.readPos[i] = make([]int32, rs.shards[i].entryCapacity())
	}
	return g
}

// visit returns the node standing for c and whether this call is the one that
// created it.
//
// A node is registered when it finishes rather than when it starts, which is
// what numbers the slice in post-order. Nothing is lost by the gap: cells form a
// directed acyclic graph, so a cell still on the stack cannot be reached from
// below it, and finding a hash absent from byHash therefore does mean the walk
// has not started that subtree.
func (g *sourceGraph) visit(rs *ReadSet, c *Cell, depth int) (int32, bool, error) {
	if depth > maxDepth {
		return 0, false, fmt.Errorf("source walk exceeded the cell depth limit")
	}
	g.noteInstance(c)
	hash := c.HashKey()
	source, pos, read := rs.shards[shardOf(hash)].lookupPos(hash)
	if read {
		if idx := g.readPos[shardOf(hash)][pos]; idx != 0 {
			return idx - 1, false, nil
		}
	} else {
		if idx, seen := g.byUnread[hash]; seen {
			return idx, false, nil
		}
		// An unread reference is still a legitimate boundary — the update may
		// prune onto it — but nothing below it was reached, so the walk stops.
		return g.add(hash, pos, c, false, nil), true, nil
	}

	// Descend the cell the walk arrived at, falling back to the recorded
	// instance only when the arriving one cannot answer. A lazy predecessor
	// hands out placeholders with no references, so those need the recorded
	// materialization; everywhere else the source tree's own cell is the one
	// whose children carry the hashes the recorder keyed by.
	descend := c
	if descend.IsLazy() || descend.refsCount() == 0 {
		descend = source
	}

	// A cell holds at most four references, so the children are gathered on the
	// stack and appended to the shared slice in one piece.
	var childBuf [4]int32
	children := 0
	refView := newCellRefView(descend)
	for i := 0; i < descend.refsCount(); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return 0, false, err
		}
		idx, _, err := g.visit(rs, ref, depth+1)
		if err != nil {
			return 0, false, err
		}
		childBuf[children] = idx
		children++
	}
	return g.add(hash, pos, source, true, childBuf[:children]), true, nil
}

// noteInstance records an unresolved arrival. The load guards the store so a
// store-backed predecessor, where nearly every arrival is a placeholder, pays
// one uncontended read per cell instead of a write per cell.
func (g *sourceGraph) noteInstance(c *Cell) {
	if c.IsLazy() && !g.lazyInstance.Load() {
		g.lazyInstance.Store(true)
	}
}

// sawLazyInstance reports whether any cell reached the walk unresolved.
func (g *sourceGraph) sawLazyInstance() bool {
	return g != nil && g.lazyInstance.Load()
}

func (g *sourceGraph) add(hash Hash, pos int32, held *Cell, read bool, children []int32) int32 {
	node := sourceNode{cell: held, read: read, claimedBy: -1, firstRef: int32(len(g.edges))}
	if len(children) > 0 {
		g.edges = append(g.edges, children...)
		node.refCount = int32(len(children))
	}
	idx := int32(len(g.nodes))
	g.nodes = append(g.nodes, node)
	if read {
		g.readPos[shardOf(hash)][pos] = idx + 1
	} else {
		g.byUnread[hash] = idx
	}
	return idx
}

// held returns the subtree the source holds under this hash, if it holds one.
func (g *sourceGraph) held(hash Hash) (*Cell, int32, bool) {
	if _, pos, read := g.rs.shards[shardOf(hash)].lookupPos(hash); read {
		if idx := g.readPos[shardOf(hash)][pos]; idx != 0 {
			return g.nodes[idx-1].cell, idx - 1, true
		}
		return nil, 0, false
	}
	idx, ok := g.byUnread[hash]
	if !ok {
		return nil, 0, false
	}
	return g.nodes[idx].cell, idx, true
}

// markBoundary records that the destination replaced this subtree with a pruned
// branch, which the source proof now has to expose. When the caller is also
// assembling the applied state, it returns the one trace-free source cell that
// state may retain beyond this proof build.
func (g *sourceGraph) markBoundary(idx int32, wantApplied bool) *Cell {
	if g.parallel {
		mu := &g.boundaryMu[uint32(idx)&(sourceGraphBoundaryShards-1)]
		mu.Lock()
		applied := g.markBoundaryNode(idx, wantApplied, true)
		mu.Unlock()
		return applied
	}
	return g.markBoundaryNode(idx, wantApplied, false)
}

func (g *sourceGraph) markBoundaryNode(idx int32, wantApplied, parallel bool) *Cell {
	n := &g.nodes[idx]
	if !n.boundary {
		n.boundary = true
		if parallel {
			g.parallelBoundaries.Add(1)
		} else {
			g.boundaries++
		}
	}
	if !wantApplied {
		return nil
	}
	if n.applied == nil {
		n.applied = n.cell.WithoutTrace()
	}

	return n.applied
}

// boundaryAncestors returns the nodes the source proof must carry: the strict
// ancestors of every boundary, and nothing else. The boundaries stay out so they
// become pruned branches on the source side too, which is what pairs the two
// halves of the update.
//
// A boundary is exposed at ONE place. Boundaries are named by hash, and the same
// subtree can sit at several positions in the source; opening a path to every one
// of them would carry ancestors that prove nothing the first path did not already
// prove. Applying an update looks a boundary up by hash and takes whichever
// occurrence the proof carries, so one path is enough — and the difference is not
// academic: exposing them all is what made this side of the update roughly a
// quarter larger than the reference implementation's.
//
// Which one is not free to choose badly. A path opened for a boundary that some
// other path also reaches costs every cell along it. So the choice is made here,
// with the boundaries known, rather than during the walk, which could only ever
// pick whichever position it happened to reach first.
//
// Three linear passes over the post-ordered slice, no recursion and no lookups:
//
//	reach  — ascending, children first: is there a boundary at or below this node
//	claim  — descending, parents first: give every reachable subtree one way in
//	kept   — ascending: a node stays in the proof if a subtree it claimed needs it
//
// Descending index is a topological order because the slice is post-ordered, and
// it visits the root's last child first. For a shard state that child is
// accounts, which is where all but a handful of boundaries live, so a subtree
// shared between it and the out-message queue is claimed on the accounts side —
// the side whose path the proof is carrying anyway.
func (g *sourceGraph) boundaryAncestors() []int32 {
	boundaries := g.boundaries
	if g.parallel {
		boundaries = int(g.parallelBoundaries.Load())
	}
	if boundaries == 0 || len(g.nodes) == 0 {
		return nil
	}
	for i := range g.nodes {
		n := &g.nodes[i]
		reach := n.boundary
		if !reach {
			for _, child := range g.edges[n.firstRef : n.firstRef+n.refCount] {
				if g.nodes[child].reach {
					reach = true
					break
				}
			}
		}
		n.reach = reach
	}

	// The root is reached by being the root; everything else has to be claimed.
	// A boundary is not a stopping point: one can sit inside another — the
	// destination reuses a subtree and, elsewhere, something nested in it — so
	// the claim descends through boundaries too.
	g.nodes[len(g.nodes)-1].covered = true
	for i := len(g.nodes) - 1; i >= 0; i-- {
		n := &g.nodes[i]
		if !n.covered {
			continue
		}
		for _, child := range g.edges[n.firstRef : n.firstRef+n.refCount] {
			c := &g.nodes[child]
			if !c.reach || c.covered {
				continue
			}
			c.covered = true
			c.claimedBy = int32(i)
		}
	}

	kept := make([]int32, 0, boundaries*2)
	for i := range g.nodes {
		n := &g.nodes[i]
		for _, child := range g.edges[n.firstRef : n.firstRef+n.refCount] {
			c := &g.nodes[child]
			// Kept for being an ancestor of a boundary, not for being one: a
			// boundary with nothing reused underneath stays pruned on the source
			// side too, which is what pairs the two halves of the update. And a
			// subtree this node did not claim proves nothing here — some other
			// node is carrying it.
			if c.claimedBy == int32(i) && (c.boundary || c.kept) {
				n.kept = true
				break
			}
		}
		if n.kept {
			kept = append(kept, int32(i))
		}
	}
	return kept
}

// errSourceGraphDepthRace marks a parallel subtree walk that ran past the cell
// depth limit from its own starting point. The same subtree can still be legal
// through the path the sequential order arrives by, so the walk's result is
// discarded and the replay covers the subtree itself, at the honest depth.
var errSourceGraphDepthRace = fmt.Errorf("parallel source walk exceeded the cell depth limit")

// sourceGraphFrontierDepth is where the parallel build hands subtrees to
// workers. It is a fixed property of the walk, not of the worker count, which
// is what keeps the graph — and every byte derived from it — identical however
// many workers run: which subtrees are precomputed depends only on the tree,
// and a precomputed subtree merges to exactly what walking it in place builds.
//
// Eight trades distribution against task weight: a mostly-binary state fans
// out to at most a couple hundred subtrees here, big enough that a worker's
// setup and the sequential merge stay small beside the walk itself.
const sourceGraphFrontierDepth = 8

// sourceGraphParallelMinCells gates the parallel build by recorded size. Under
// it the whole graph is a few milliseconds and the parallel form measurably
// loses to the plain walk on task setup; the server-scale graphs it exists
// for sit far above.
const sourceGraphParallelMinCells = 24 << 10

// sourceGraphLocalNode is one node of a worker-local subtree walk, carrying
// what the merge needs to place it in the shared graph: the identity to claim,
// the read-set position that keys the claim, and locally-numbered edges.
type sourceGraphLocalNode struct {
	hash     Hash
	cell     *Cell
	pos      int32
	firstRef int32
	refCount int32
	read     bool
}

type sourceGraphTask struct {
	cell  *Cell
	local []sourceGraphLocalNode
	edges []int32
	// lazy carries the worker's own unresolved arrivals up to the graph, which
	// the worker cannot touch: it is folded in by mergeSourceSubtree, and a task
	// the replay never reaches contributes no nodes and so needs no fold.
	lazy bool
	err  error
}

// buildSourceGraphParallel builds the same graph the sequential walk builds,
// byte for byte.
//
// The claim pass downstream breaks ties by node index, so the update's shape
// depends on the exact post-order numbering — a merely valid post-order is not
// enough. So the parallel form does not schedule the graph build; it
// precomputes. A collection pass lists the distinct subtrees hanging at a
// fixed depth, workers walk each into a worker-local graph, and then one
// sequential replay — the ordinary walk, making the ordinary decisions in the
// ordinary order — splices a precomputed subtree in wherever it reaches one,
// instead of descending it.
//
// The splice is exact by an invariant the graph already keeps: a node is
// registered after its children, so a hash present in the graph stands for a
// whole finished subtree. Merging a local post-order and dropping every node
// the graph already holds therefore drops whole local subtrees — exactly the
// descents the in-place walk would have skipped — and appends the rest in the
// same order the in-place walk would have appended them.
//
// The tasks are a cache, not an obligation. A subtree the replay never reaches
// again (an earlier merge covered it) leaves its result unused; a subtree
// whose worker failed — the depth limit looks nearer from a longer path — is
// simply walked in place. Both cost duplicated work and change nothing.
func (rs *ReadSet) buildSourceGraphParallel(g *sourceGraph, workers int) (bool, error) {
	var tasks []*sourceGraphTask
	taskByHash := make(map[Hash]*sourceGraphTask)
	collected := make(map[Hash]struct{})
	var collect func(c *Cell, depth int) error
	collect = func(c *Cell, depth int) error {
		g.noteInstance(c)
		hash := c.HashKey()
		if _, seen := collected[hash]; seen {
			return nil
		}
		source, _, read := rs.shards[shardOf(hash)].lookupPos(hash)
		if !read {
			collected[hash] = struct{}{}
			return nil
		}
		descend := c
		if descend.IsLazy() || descend.refsCount() == 0 {
			descend = source
		}
		if depth == sourceGraphFrontierDepth {
			if descend.refsCount() > 0 {
				if _, known := taskByHash[hash]; !known {
					task := &sourceGraphTask{cell: c}
					taskByHash[hash] = task
					tasks = append(tasks, task)
				}
			}
			return nil
		}
		refView := newCellRefView(descend)
		for i := 0; i < descend.refsCount(); i++ {
			ref, err := refView.boundaryRef(i)
			if err != nil {
				return err
			}
			if err = collect(ref, depth+1); err != nil {
				return err
			}
		}
		collected[hash] = struct{}{}
		return nil
	}
	if err := collect(rs.source, 0); err != nil {
		return false, err
	}
	if len(tasks) < 2 {
		return false, nil
	}

	var cursor atomic.Int64
	var wg sync.WaitGroup
	for range min(workers, len(tasks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scratch := newSourceGraphScratch(rs)
			for {
				i := cursor.Add(1) - 1
				if i >= int64(len(tasks)) {
					return
				}
				task := tasks[i]
				task.err = rs.walkSourceSubtree(task, scratch)
			}
		}()
	}
	wg.Wait()

	var replay func(c *Cell, depth int) (int32, error)
	replay = func(c *Cell, depth int) (int32, error) {
		if depth > maxDepth {
			return 0, fmt.Errorf("source walk exceeded the cell depth limit")
		}
		g.noteInstance(c)
		hash := c.HashKey()
		source, pos, read := rs.shards[shardOf(hash)].lookupPos(hash)
		if read {
			if idx := g.readPos[shardOf(hash)][pos]; idx != 0 {
				return idx - 1, nil
			}
		} else {
			if idx, seen := g.byUnread[hash]; seen {
				return idx, nil
			}
			return g.add(hash, pos, c, false, nil), nil
		}
		descend := c
		if descend.IsLazy() || descend.refsCount() == 0 {
			descend = source
		}
		if depth == sourceGraphFrontierDepth {
			if task := taskByHash[hash]; task != nil && task.err == nil {
				return g.mergeSourceSubtree(task), nil
			}
		}
		var childBuf [4]int32
		children := 0
		refView := newCellRefView(descend)
		for i := 0; i < descend.refsCount(); i++ {
			ref, err := refView.boundaryRef(i)
			if err != nil {
				return 0, err
			}
			idx, err := replay(ref, depth+1)
			if err != nil {
				return 0, err
			}
			childBuf[children] = idx
			children++
		}
		return g.add(hash, pos, source, true, childBuf[:children]), nil
	}
	if _, err := replay(rs.source, 0); err != nil {
		return false, err
	}
	return true, nil
}

// sourceGraphScratch is one worker's reusable dedup table: the same read-set
// positions the shared graph is keyed by, tagged with a per-task epoch so that
// moving to the next task costs a counter increment instead of a clear. Only
// unread references fall back to a map, and that map is cleared per task —
// unread nodes are the fringe, a handful per subtree.
type sourceGraphScratch struct {
	readPos  [readSetShards][]uint64 // epoch<<32 | local index+1
	byUnread map[Hash]int32
	epoch    uint64
}

func newSourceGraphScratch(rs *ReadSet) *sourceGraphScratch {
	s := &sourceGraphScratch{byUnread: make(map[Hash]int32)}
	for i := range s.readPos {
		s.readPos[i] = make([]uint64, rs.shards[i].entryCapacity())
	}
	return s
}

func (s *sourceGraphScratch) next() {
	s.epoch++
	clear(s.byUnread)
}

// walkSourceSubtree is the sequential visit confined to one frontier subtree,
// writing a worker-local graph in local post-order.
func (rs *ReadSet) walkSourceSubtree(task *sourceGraphTask, scratch *sourceGraphScratch) error {
	scratch.next()
	epoch := scratch.epoch << 32
	var visit func(c *Cell, depth int) (int32, error)
	visit = func(c *Cell, depth int) (int32, error) {
		if depth > maxDepth {
			return 0, errSourceGraphDepthRace
		}
		if c.IsLazy() {
			task.lazy = true
		}
		hash := c.HashKey()
		source, pos, read := rs.shards[shardOf(hash)].lookupPos(hash)
		if read {
			if slot := scratch.readPos[shardOf(hash)][pos]; slot>>32 == scratch.epoch {
				return int32(uint32(slot)) - 1, nil
			}
		} else if idx, seen := scratch.byUnread[hash]; seen {
			return idx, nil
		}
		add := func(held *Cell, children []int32) int32 {
			node := sourceGraphLocalNode{
				hash: hash, cell: held, pos: pos, read: read,
				firstRef: int32(len(task.edges)), refCount: int32(len(children)),
			}
			task.edges = append(task.edges, children...)
			idx := int32(len(task.local))
			task.local = append(task.local, node)
			if read {
				scratch.readPos[shardOf(hash)][pos] = epoch | uint64(uint32(idx+1))
			} else {
				scratch.byUnread[hash] = idx
			}
			return idx
		}
		if !read {
			return add(c, nil), nil
		}
		descend := c
		if descend.IsLazy() || descend.refsCount() == 0 {
			descend = source
		}
		var childBuf [4]int32
		children := 0
		refView := newCellRefView(descend)
		for i := 0; i < descend.refsCount(); i++ {
			ref, err := refView.boundaryRef(i)
			if err != nil {
				return 0, err
			}
			idx, err := visit(ref, depth+1)
			if err != nil {
				return 0, err
			}
			childBuf[children] = idx
			children++
		}
		return add(source, childBuf[:children]), nil
	}
	_, err := visit(task.cell, sourceGraphFrontierDepth)
	return err
}

// mergeSourceSubtree folds one worker's local graph into the shared one,
// dropping every node the graph already claims — whole local subtrees, by the
// post-order invariant — and appending the rest in local order. Local edges
// reference only earlier local nodes, so one forward pass with a remap table
// settles every edge.
func (g *sourceGraph) mergeSourceSubtree(task *sourceGraphTask) int32 {
	if task.lazy && !g.lazyInstance.Load() {
		g.lazyInstance.Store(true)
	}
	remap := make([]int32, len(task.local))
	var childBuf [4]int32
	for i := range task.local {
		n := &task.local[i]
		if n.read {
			if idx := g.readPos[shardOf(n.hash)][n.pos]; idx != 0 {
				remap[i] = idx - 1
				continue
			}
		} else if idx, seen := g.byUnread[n.hash]; seen {
			remap[i] = idx
			continue
		}
		children := childBuf[:0]
		for _, child := range task.edges[n.firstRef : n.firstRef+n.refCount] {
			children = append(children, remap[child])
		}
		remap[i] = g.add(n.hash, n.pos, n.cell, n.read, children)
	}
	return remap[len(task.local)-1]
}
