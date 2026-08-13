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
	updateFrom, updateTo, _, err := rs.createMerkleUpdateRaw(to, false)
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
	updateFrom, updateTo, applied, err := rs.createMerkleUpdateRaw(to, true)
	if err != nil {
		return nil, nil, err
	}
	update, err := CreateMerkleUpdate(updateFrom, updateTo)
	if err != nil {
		return nil, nil, err
	}
	return update, applied, nil
}

func (rs *ReadSet) createMerkleUpdateRaw(to *Cell, wantApplied bool) (*Cell, *Cell, *Cell, error) {
	if rs == nil || rs.source == nil {
		return nil, nil, nil, fmt.Errorf("failed to build merkle update: no source tree")
	}
	if to == nil {
		return nil, nil, nil, fmt.Errorf("failed to build merkle update: destination is nil")
	}
	from := rs.source
	if from.Level() != 0 || to.Level() != 0 {
		return nil, nil, nil, fmt.Errorf("roots have non-zero level")
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
		return nil, nil, nil, graphErr
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
		// holding it — not because the subtree behind it is childless. Believing
		// that report spends a celldb read to find out whether a boundary would
		// have cost a few dozen bytes too many, which is the wrong way round: the
		// read is the expensive half. So an unresolved reference is pruned on the
		// spot, which is precisely what a boundary is for.
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
	buildState.built.init(rs.Size())
	// Both proofs prune the same subtree roots — that is what makes them a pair —
	// so the boundaries the destination walk builds are handed to the source walk
	// instead of being built a second time.
	buildState.prunedShared = make(map[proofBodyKey]*Cell, rs.Size()/2+1)
	updateTo, applied, err := buildState.build(to, to.Level())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to build merkle update destination proof: %w", err)
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
		return nil, nil, nil, fmt.Errorf("failed to build merkle update source proof: %w", err)
	}
	return updateFrom, updateTo, applied, nil
}
