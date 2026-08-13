package cell

import "testing"

// TestSourceGraphIsPostOrdered pins the invariant the boundary fold rests on:
// a node's edges always point at lower indices. The fold reads each child's
// answer as final, so if registration ever moved from the end of visit to its
// start the slice would stop being post-ordered and the fold would read answers
// that had not been computed yet — silently, since a zero value is a legal one.
func TestSourceGraphIsPostOrdered(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xEE, 8).EndCell()
	shared := BeginCell().MustStoreUInt(0x5A, 8).MustStoreRef(leaf).EndCell()
	parent0 := BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(shared).EndCell()
	parent1 := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	root := BeginCell().MustStoreUInt(0xA0, 8).MustStoreRef(parent0).MustStoreRef(parent1).EndCell()

	rs := NewReadSet(root)
	rootSlice := rs.Root().MustBeginParse()
	for i := 0; i < 2; i++ {
		branch, err := rootSlice.LoadRef()
		if err != nil {
			t.Fatal(err)
		}
		sharedRef, err := branch.LoadRef()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = sharedRef.LoadRef(); err != nil {
			t.Fatal(err)
		}
	}

	g, err := rs.buildSourceGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.nodes) != 5 {
		t.Fatalf("expected the five distinct cells as nodes, got %d", len(g.nodes))
	}
	for i := range g.nodes {
		n := &g.nodes[i]
		for _, child := range g.edges[n.firstRef : n.firstRef+n.refCount] {
			if int(child) >= i {
				t.Fatalf("node %d has an edge to %d: the slice is not post-ordered", i, child)
			}
		}
	}
}

// TestSourceGraphClaimsASharedSubtreeOnce is the other half of the same
// contract. A subtree reachable from two parents keeps an edge from both, and
// the claim pass gives it exactly one way in — which is how a boundary under it
// is exposed at one place instead of every place, the difference that once made
// this side of the update roughly a quarter larger than the reference
// implementation's.
func TestSourceGraphClaimsASharedSubtreeOnce(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0x5A, 8).MustStoreRef(BeginCell().MustStoreUInt(0xEE, 8).EndCell()).EndCell()
	parent0 := BeginCell().MustStoreUInt(0x10, 8).MustStoreRef(shared).EndCell()
	parent1 := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	root := BeginCell().MustStoreUInt(0xA0, 8).MustStoreRef(parent0).MustStoreRef(parent1).EndCell()

	rs := NewReadSet(root)
	rootSlice := rs.Root().MustBeginParse()
	for i := 0; i < 2; i++ {
		branch, err := rootSlice.LoadRef()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = branch.LoadRef(); err != nil {
			t.Fatal(err)
		}
	}

	g, err := rs.buildSourceGraph()
	if err != nil {
		t.Fatal(err)
	}
	sharedIdx, ok := g.byHash[shared.HashKey()]
	if !ok {
		t.Fatal("the shared subtree is missing from the graph")
	}
	incoming := 0
	for i := range g.nodes {
		n := &g.nodes[i]
		for _, child := range g.edges[n.firstRef : n.firstRef+n.refCount] {
			if child == sharedIdx {
				incoming++
			}
		}
	}
	if incoming != 2 {
		t.Fatalf("the shared subtree has %d edges into it, want both parents", incoming)
	}

	// With the shared subtree standing in for a boundary, exactly one of the two
	// parents may end up carrying it.
	g.markBoundary(sharedIdx)
	kept := g.boundaryAncestors()
	if g.nodes[sharedIdx].claimedBy < 0 {
		t.Fatal("the boundary was never claimed")
	}
	carriers := 0
	for _, idx := range kept {
		if idx == g.nodes[sharedIdx].claimedBy {
			carriers++
		}
	}
	if carriers != 1 {
		t.Fatalf("the claiming parent appears %d times among the kept cells, want once", carriers)
	}
	parent0Idx, ok0 := g.byHash[parent0.HashKey()]
	parent1Idx, ok1 := g.byHash[parent1.HashKey()]
	if !ok0 || !ok1 {
		t.Fatal("a parent is missing from the graph")
	}
	both := 0
	for _, idx := range kept {
		if idx == parent0Idx || idx == parent1Idx {
			both++
		}
	}
	if both != 1 {
		t.Fatalf("%d of the two parents are kept, want exactly one", both)
	}
}
