package cell

import "fmt"

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
	nodes  []sourceNode
	edges  []int32
	byHash map[Hash]int32
	// boundaries counts the nodes the destination replaced, so a source with
	// none skips the fold entirely.
	boundaries int
}

// sourceNode is one subtree the reads reached.
type sourceNode struct {
	// cell is what the source holds here: for a cell that was read, the recorded
	// instance with its trace stripped, which is also what the destination gets
	// back when it prunes onto this subtree; for one that was not read, the
	// reference the walk arrived at.
	cell *Cell
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
func (rs *ReadSet) buildSourceGraph() (*sourceGraph, error) {
	// The record's size is already an over-estimate of this graph, not an
	// under-estimate to be doubled: a node exists for a subtree the source holds,
	// while the record also carries cells the transition rebuilt and read back,
	// which the source never held. Measured on a mainnet block the graph is about
	// seven tenths of the record — so doubling it allocated a nodes array, an
	// edges array and a map for three times the population that arrives.
	hint := rs.Size()
	g := &sourceGraph{
		nodes:  make([]sourceNode, 0, hint),
		edges:  make([]int32, 0, hint),
		byHash: make(map[Hash]int32, hint),
	}
	if rs.source == nil {
		return g, nil
	}
	if _, _, err := g.visit(rs, rs.source, 0); err != nil {
		return nil, err
	}
	return g, nil
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
	hash := c.HashKey()
	if idx, seen := g.byHash[hash]; seen {
		return idx, false, nil
	}

	source, read := rs.Contains(hash)
	if !read {
		// An unread reference is still a legitimate boundary — the update may
		// prune onto it — but nothing below it was reached, so the walk stops.
		return g.add(hash, c, false, nil), true, nil
	}

	// The recorder keeps the instance that was parsed, which carries the
	// recording trace. Anything handed back from here can end up in the applied
	// destination root and outlive the collation, so the trace is stripped once,
	// here; for the untraced cells this normally yields, it is a no-op.
	held := source.WithoutTrace()

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
	return g.add(hash, held, true, childBuf[:children]), true, nil
}

func (g *sourceGraph) add(hash Hash, held *Cell, read bool, children []int32) int32 {
	node := sourceNode{cell: held, read: read, claimedBy: -1, firstRef: int32(len(g.edges))}
	if len(children) > 0 {
		g.edges = append(g.edges, children...)
		node.refCount = int32(len(children))
	}
	idx := int32(len(g.nodes))
	g.nodes = append(g.nodes, node)
	g.byHash[hash] = idx
	return idx
}

// held returns the subtree the source holds under this hash, if it holds one.
func (g *sourceGraph) held(hash Hash) (*Cell, int32, bool) {
	idx, ok := g.byHash[hash]
	if !ok {
		return nil, 0, false
	}
	return g.nodes[idx].cell, idx, true
}

// markBoundary records that the destination replaced this subtree with a pruned
// branch, which the source proof now has to expose.
func (g *sourceGraph) markBoundary(idx int32) {
	n := &g.nodes[idx]
	if n.boundary {
		return
	}
	n.boundary = true
	g.boundaries++
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
	if g.boundaries == 0 || len(g.nodes) == 0 {
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

	kept := make([]int32, 0, g.boundaries*2)
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
