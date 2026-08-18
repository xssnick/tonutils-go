package cell

import "fmt"

// CreateMerkleUpdate builds the state update from the recorded source tree to the
// destination root the caller assembled.
//
// It replaces CellUsageTree.CreateMerkleUpdate. The destination walk decides
// whether a subtree may be replaced by a boundary onto the predecessor, and that
// decision is the one place the two implementations differ. The old one asked a
// path-keyed arena and then spent three indexes and a mark mode establishing
// whether the answer could be trusted, because a rebuilt cell inherits the trace of
// the path it came from. This one asks whether the subtree's hash was read. A hash
// is not inherited, so there is nothing left to disambiguate.
func (rs *ReadSet) CreateMerkleUpdate(to *Cell) (*Cell, error) {
	updateFrom, updateTo, _, _, err := rs.createMerkleUpdateRaw(to, false, 0)
	if err != nil {
		return nil, err
	}
	return CreateMerkleUpdate(updateFrom, updateTo)
}

// CreateMerkleUpdateApplied returns the update together with the destination root
// as ApplyMerkleUpdate would rebuild it: every pruned subtree is the predecessor's
// own cell, so the new root shares memory with the resident state instead of
// duplicating it.
//
// Producing both here is what makes it cheap. Applying an update afterwards has to
// rediscover which source subtree stands behind each boundary by walking the whole
// source proof; the walk that decided to prune them already knew.
func (rs *ReadSet) CreateMerkleUpdateApplied(to *Cell) (*Cell, *Cell, error) {
	update, applied, _, err := rs.CreateMerkleUpdateAppliedSized(to, 0)
	return update, applied, err
}

// CreateMerkleUpdateAppliedSized is CreateMerkleUpdateApplied for a caller that
// can estimate how many cells the destination walk will memoise, and reports
// what it actually memoised so the caller can estimate the next one.
//
// The count is worth carrying because nothing else at hand predicts it. It is
// the size of the destination proof, which tracks how much of the state the
// block rewrote — across mainnet blocks it moved by an eighth while the read set
// moved by a factor of two — so sizing the memo from the record's size, the only
// figure this call used to have, asks for twice the table the walk fills.
//
// Capacity only. The memo holds cells for the duration of one walk and is
// dropped with it; the estimate cannot reach the produced update.
func (rs *ReadSet) CreateMerkleUpdateAppliedSized(to *Cell, memoHint int) (*Cell, *Cell, int, error) {
	updateFrom, updateTo, applied, memoUsed, err := rs.createMerkleUpdateRaw(to, true, memoHint)
	if err != nil {
		return nil, nil, 0, err
	}
	update, err := CreateMerkleUpdate(updateFrom, updateTo)
	if err != nil {
		return nil, nil, 0, err
	}
	return update, applied, memoUsed, nil
}

func (rs *ReadSet) createMerkleUpdateRaw(to *Cell, wantApplied bool, memoHint int) (*Cell, *Cell, *Cell, int, error) {
	if rs == nil || rs.source == nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to build merkle update: no source tree")
	}
	if to == nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to build merkle update: destination is nil")
	}
	from := rs.source
	if from.Level() != 0 || to.Level() != 0 {
		return nil, nil, nil, 0, fmt.Errorf("roots have non-zero level")
	}

	// Building an update must not widen the record. The destination walk parses
	// whatever it does not prune, and a cell recorded there would grow the proof
	// taken afterwards past the size the block was already admitted against.
	rs.IgnoreReads(true)
	defer rs.IgnoreReads(false)

	// Being recorded is not evidence that a subtree may be pruned onto. A cell the
	// transition rebuilt inherits the trace of the cell it was derived from and is
	// therefore recorded, even though the source tree never held it; pruning a
	// destination subtree onto such a hash emits a boundary the source proof cannot
	// expose, and applying the update then fails on an unknown pruned branch. So
	// the source is walked once, and only what it really contains may be pruned
	// onto — the same walk that later says which cells the source proof carries.
	graph, graphErr := rs.buildSourceGraph()
	if graphErr != nil {
		return nil, nil, nil, 0, graphErr
	}

	prune := func(_ *Cell, _ int, hash Hash) (*Cell, bool, error) {
		source, idx, known := graph.held(hash)
		if !known {
			return nil, false, nil
		}
		// A childless cell is never replaced by a boundary: a pruned branch carries
		// a hash and a depth, which is more than such a cell weighs, so leaving it
		// ordinary is the smaller update. That is a size trade and nothing more,
		// and it is only on offer for a cell whose children are already known.
		//
		// An unresolved predecessor reference is not such a cell. It reports no
		// references because a placeholder stands in for the subtree rather than
		// holding it — not because the subtree behind it is childless. So an
		// unresolved reference is pruned on the spot, which is precisely what a
		// boundary is for.
		//
		// The price of that is measured, not estimated. On the mainnet fixture a
		// store-shaped predecessor turns 213 childless cells into pruned
		// boundaries, and the block grows by 21,642 bytes — 30,816 B (+11.40% of
		// the block) with no filler accounts, 22,209 B at 10,000 accounts,
		// 21,642 B at 100,000. Three orders of magnitude above the couple of
		// dozen bytes this comment used to claim, and still the right trade:
		// believing the placeholder's report would spend 213 celldb reads on the
		// collation hot path to save that, and the read is the expensive half by
		// a wide margin.
		//
		// The reference decides identically, and for the same reason
		// (MerkleUpdate.cpp:228-240): its prune predicate gates the childless
		// check behind cell->is_loaded(), so only a loaded cell with
		// size_refs() == 0 suppresses the boundary; an unloaded ExtCell falls to
		// get_tree_node() and is pruned without ever being loaded.
		//
		// The count is the invariant, not the byte delta — the delta moves 42%
		// along the filler axis above while the 213 does not move at all — and
		// the count is what the collator's regression test pins. One consequence
		// has to be stated because it is easy to build a broken gate on: a block
		// collated over a lazy parent has a different RootHash and FileHash from
		// the same block collated over a resident one, since MerkleUpdate hashes
		// its children at level 1, i.e. by their pruned representation. Both
		// blocks are valid and each validates under either parent shape, so
		// anything that recollates and compares hashes must fix the parent shape
		// first.
		if !source.IsLazy() && source.refsCount() == 0 {
			return nil, false, nil
		}
		graph.markBoundary(idx)
		if !wantApplied {
			return nil, true, nil
		}
		return source, true, nil
	}

	// Both proofs end up inside the same update, so they are built from one arena
	// and released together with it.
	arena := &proofCellArena{}
	buildState := merkleProofPruneBuildState{
		shouldPrune:   prune,
		wantApplied:   wantApplied,
		resolveLoaded: rs.recordedCell,
		arena:         arena,
	}
	// The caller's estimate when there is one; the record's size otherwise, which
	// is the ceiling this walk used to be sized at unconditionally — it cannot
	// visit more cells than were read, but on a mainnet block it visits about a
	// third of them.
	memoSize := rs.Size()
	if memoHint > 0 {
		memoSize = memoHint
	}
	buildState.memoHint = memoSize
	buildState.built.init(memoSize)
	// Both proofs prune the same subtree roots — that is what makes them a pair —
	// so the boundaries the destination walk builds are handed to the source walk
	// instead of being built a second time.
	//
	// Sized against the memo rather than the record: these are the boundaries of
	// the destination proof, a subset of what that walk memoises, and measured
	// over four block shapes they were 0.36-0.40 of it while the record was three
	// times the size of either. Half the memo covers the measured spread with room
	// left and is never reached, so the map is never rehashed.
	buildState.prunedShared = make(map[proofBodyKey]*Cell, memoSize/2+1)
	updateTo, applied, err := buildState.build(to, to.Level())
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to build merkle update destination proof: %w", err)
	}

	kept := graph.boundaryAncestors()
	sourceState := &proofBodyBuildState{arena: arena, prunedShared: buildState.prunedShared}
	if len(kept) > 0 {
		sourceState.cells = make(map[Hash]*Cell, len(kept))
		for _, idx := range kept {
			held := graph.nodes[idx].cell
			sourceState.cacheLoaded(held.HashKey(), held)
		}
	}
	sourceState.prepareBuiltCache()
	updateFrom, err := buildRecordedProofBody(from, sourceState, from.Level())
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to build merkle update source proof: %w", err)
	}
	return updateFrom, updateTo, applied, buildState.memoSize(), nil
}
