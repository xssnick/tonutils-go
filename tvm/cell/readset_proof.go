package cell

import "fmt"

// Proof serializes the recorded reads as a Merkle proof of the source tree: a cell
// that was read contributes its body, a reference the reads stopped at contributes
// a pruned branch.
//
// It replaces Cell.CreateUsageProof and produces the same bytes. That the two
// agree is not a coincidence of this implementation: the old builder collected the
// visited cells out of the arena into a hash set and then pruned the source tree by
// membership in that set, so the arena was only ever a way of arriving at the set
// this recorder holds directly.
func (rs *ReadSet) Proof() (*Cell, error) {
	if rs == nil || rs.source == nil {
		return nil, fmt.Errorf("failed to build read set proof: no source tree")
	}
	if rs.source.Level() != 0 {
		return nil, fmt.Errorf("failed to build read set proof: level is not 0")
	}

	state := merkleProofPruneBuildState{
		readSet: rs,
		arena:   &proofCellArena{},
		// The proof keeps exactly the recorded cells, so the record's own size
		// is not an estimate of the memo's population — it is the population.
		memoHint: rs.Size(),
	}
	body, _, err := state.build(rs.source, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build read set proof: %w", err)
	}
	return CreateMerkleProof(body)
}

// recordedCell hands the proof builder the cell the recorder already loaded, so a
// lazy source cell is not resolved through its loader a second time.
func (rs *ReadSet) recordedCell(hash Hash) *Cell {
	c, _ := rs.Contains(hash)
	return c
}

// RecordedCell exposes that same lookup to a proof built outside this recorder
// over the same source tree — Cell.CreateHashUsageProofResolved, whose selection
// is a hash predicate rather than the record itself. A hash the recorder never
// saw simply comes back nil and the build resolves it.
func (rs *ReadSet) RecordedCell(hash Hash) *Cell {
	if rs == nil {
		return nil
	}
	return rs.recordedCell(hash)
}

// ProofOf serializes the reads of a subtree of the source, for callers that record
// several roots through one set — a state and the neighbour queues it walked.
func (rs *ReadSet) ProofOf(root *Cell) (*Cell, error) {
	if rs == nil {
		return nil, fmt.Errorf("failed to build read set proof: read set is nil")
	}
	if root == nil {
		return nil, fmt.Errorf("failed to build read set proof: root is nil")
	}
	source := root.WithoutTrace()
	if source.Level() != 0 {
		return nil, fmt.Errorf("failed to build read set proof: level is not 0")
	}

	state := merkleProofPruneBuildState{
		readSet: rs,
		arena:   &proofCellArena{},
		// No estimate here, unlike Proof: this walk covers one subtree of the
		// source, so the record's size says nothing about how much of it the
		// walk will reach and would size the memo for the whole set.
	}
	body, _, err := state.build(source, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build read set proof: %w", err)
	}
	return CreateMerkleProof(body)
}
