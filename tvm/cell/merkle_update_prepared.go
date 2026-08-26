package cell

import (
	"errors"
	"fmt"
)

// A fused validated Merkle update.
//
// ValidateMerkleUpdate walks the update's source subtree and its destination
// subtree, building its own exact Hash-keyed indexes; ApplyMerkleUpdate then walks the
// source again in lockstep with the real parent and the destination again,
// building two more. Four subtree walks and five 32-byte-keyed indexes, where the
// second and third walks are re-deriving something the first two already knew:
// which hashes are boundaries, at which Merkle depth, in which DFS order, and
// which destination boundary resolves to which source occurrence.
//
// That part is a pure function of the update. What is NOT is the value behind
// each boundary — the parent's own cell — and the source materialization check
// that produces it. So a PreparedMerkleUpdate holds the update-side walks as
// two flat plans, and each ApplyTo replays them against one parent with two
// slices and no map at all.
//
// This is a DEPARTURE BEYOND THE REFERENCE, not a bug fix. C++ validates,
// may_applies and applies once per candidate
// (validator/impl/validate-query.cpp, ValidateQuery::compute_next_state) and
// keeps the same split in crypto/vm/cells/MerkleUpdate.cpp. The fusion is only
// admissible because the accept/reject behaviour is provably identical:
//
//   - PrepareMerkleUpdate runs merkleUpdateVerdict, which IS
//     ValidateMerkleUpdate — same two walks, same order, same errors, including
//     validateLoadedCell on every source and destination node. The verdict is
//     never folded into an apply, so no rejection moves between the two error
//     classes the consumer distinguishes ("invalid state update" versus "this
//     update does not apply to this parent").
//   - ApplyTo performs every per-node comparison walkProof performs, against
//     the same parent cells in the same order, and rebuilds through the same
//     buildMerkleUpdateCell decisions. What it drops is index lookups, not checks.
//
// See TestPreparedMerkleUpdateDifferential / FuzzPreparedMerkleUpdate, which
// compare the two paths on accept/reject, error class, output root hash and
// bytes, shared-subtree identity, per-hash loader-call multisets, determinism,
// concurrent applies over a shared lazy parent, and the whole of the above once
// more with the update tree itself lazy — the one shape where the capsule's
// cached update cells could diverge from a re-walk.
type PreparedMerkleUpdate struct {
	update      *Cell // the MerkleUpdate special cell, as supplied
	updateFrom  *Cell // ref 0, loaded once
	updateTo    *Cell // ref 1, loaded once
	fromHash    Hash  // updateFrom.HashKeyAt(0) — the applicability key
	sourceHints merkleUpdateSourceIndexHints

	src      []preparedSourceStep
	dst      []preparedDestStep
	srcSlots int
	memos    int

	// updateTraced records that the update carried a Trace at construction.
	// Caching its loaded cells would then swallow the reads a second apply is
	// supposed to record, so a traced update keeps the classic path.
	updateTraced bool
}

// preparedSourceStep is one node of walkMerkleUpdateSource, in DFS order.
//
// childPruned is computed from the UPDATE side only, so ApplyTo structurally
// cannot consult the parent to decide whether an edge is virtualized or loaded.
// That decision is the residency-invariance of the whole walk.
type preparedSourceStep struct {
	cell        *Cell
	hash        Hash // cell.HashKeyAt(merkleDepth)
	merkleDepth int32
	slot        int32 // parent slot this cell's hash@merkleDepth owns
	refs        int8
	descend     bool  // false on a revisit or a PrunedCellType node
	childPruned uint8 // bit i: source ref i is pruned ⇒ logicalBoundaryRef(i)
}

// preparedDestStep is one node of merkleUpdateValidator.dfsTo, in DFS order,
// carrying the decisions buildMerkleUpdateCell makes at that node.
//
//   - boundarySlot >= 0: substitute the parent cell in that slot.
//   - memo >= 0: a rebuildable node; all steps with an equal {hash@max,
//     merkleDepth} share the slot, which is what buildMerkleUpdateCell's ready
//     table does and what keeps shared destination subtrees shared.
//   - neither: hand the update's own cell back (pruned non-boundary, or
//     childless).
//
// next is the index just past this step's subtree, so a memo hit can skip the
// subtree the pointer-and-Merkle-depth traversal recorded under it.
type preparedDestStep struct {
	cell         *Cell
	merkleDepth  int32
	boundarySlot int32
	memo         int32
	next         int32
	refs         int8
	// boundary marks a step that substitutes a parent cell, so that a broken
	// slot is refused rather than read as one of the other two kinds. A
	// boundary handed back the update's own pruned cell would be a silently
	// wrong tree with a correct-looking root, which is the one failure mode
	// this file may not have.
	boundary bool
	// deferred marks a step whose children were not recorded because the same
	// pointer and Merkle depth had already been visited. Its memo slot is filled by an earlier
	// step by construction; if it is not, the plan is corrupt and says so
	// instead of reading a neighbour's steps as its children.
	deferred bool
}

var errPreparedPlanCorrupt = errors.New("merkle update plan is inconsistent")

// PrepareMerkleUpdate decides the update's verdict. Its error is exactly
// ValidateMerkleUpdate's error, produced by the same checks in the same order.
// It records no plans, so it costs a handful of pointers over the walk it
// already performs; a caller that applies the update more than once, or that
// wants the map-free apply at all, wants PrepareMerkleUpdatePlanned.
func PrepareMerkleUpdate(update *Cell) (*PreparedMerkleUpdate, error) {
	return prepareMerkleUpdate(update, false)
}

// PrepareMerkleUpdatePlanned is PrepareMerkleUpdate plus the walk plans. The
// plans pin one entry per update node — order 32 B per source node and 32 B per
// destination node, half a megabyte on a real mainnet block — for as long as
// the capsule lives, so it is for a caller that will actually replay them: one
// that applies the update more than once, or that wants the map-free apply at
// all. A caller applying once wants PrepareMerkleUpdate.
func PrepareMerkleUpdatePlanned(update *Cell) (*PreparedMerkleUpdate, error) {
	return prepareMerkleUpdate(update, true)
}

func prepareMerkleUpdate(update *Cell, planned bool) (*PreparedMerkleUpdate, error) {
	var srcPlan *merkleUpdateSourcePlan
	var dstPlan *merkleUpdateDestPlan
	// A traced update is validated exactly as today and then never planned: the
	// plan would cache cells loaded under that trace and silence the reads a
	// later apply is supposed to record.
	if planned && update.Trace() == nil {
		srcPlan = &merkleUpdateSourcePlan{}
		dstPlan = &merkleUpdateDestPlan{memos: newMerkleUpdateVisitTable[int32](32)}
	}

	updateFrom, updateTo, sourceHints, err := merkleUpdateVerdict(update, srcPlan, dstPlan)
	if err != nil {
		return nil, err
	}

	p := &PreparedMerkleUpdate{
		update:       update,
		updateFrom:   updateFrom,
		updateTo:     updateTo,
		fromHash:     updateFrom.HashKeyAt(0),
		sourceHints:  sourceHints,
		updateTraced: update.Trace() != nil,
	}
	if srcPlan != nil {
		p.src = srcPlan.steps
		p.srcSlots = srcPlan.slots
		p.dst = dstPlan.steps
		p.memos = len(dstPlan.memos.entries)
	}
	return p, nil
}

// Cell is the update this capsule was prepared from.
func (p *PreparedMerkleUpdate) Cell() *Cell { return p.update }

// Planned reports whether ApplyTo takes the map-free replay.
func (p *PreparedMerkleUpdate) Planned() bool { return p.src != nil }

// ApplyTo applies the prepared update to from and returns the new root.
//
// The result is built from from's own cells, so two hash-equal parents with
// different materialization legitimately produce differently materialized
// results — a proof-virtualized parent yields a root standing on pruned
// boundaries, a storage-backed parent yields one standing on lazy cells. That
// is the case this type exists for.
//
// from must carry no Trace unless the caller intends the walk's reads to be
// recorded into it. ApplyTo does not strip one: silently dropping a caller's
// trace would hide the mistake, and reads recorded during an apply widen a
// ReadSet, which changes produced block bytes. Every call site in this
// repository passes WithoutTrace, and that stays the caller's contract —
// checked under the cellassert build tag, see assertUntracedApplyParent.
//
// Safe to call concurrently, including on one shared parent: the plans are
// read-only, all per-apply state is allocated here, and a lazy reference
// materializes into a fresh cell rather than into the tree being walked.
func (p *PreparedMerkleUpdate) ApplyTo(from *Cell) (*Cell, error) {
	if from == nil {
		return nil, fmt.Errorf("from cell is nil")
	}
	if err := assertUntracedApplyParent(from); err != nil {
		return nil, err
	}
	if from.Level() != 0 {
		return nil, fmt.Errorf("roots have non-zero level")
	}
	if p.updateTraced {
		// Re-enter through the update root so its trace still observes every
		// classic-path read. Only the exact-table capacities are reused.
		return applyMerkleUpdateWithHints(from, p.update, p.sourceHints)
	}

	fromHash := from.HashKeyAt(0)
	if fromHash != p.fromHash {
		return nil, fmt.Errorf("invalid Merkle update: expected old value hash = %x, applied to value with hash = %x", p.fromHash, fromHash)
	}
	if p.src == nil {
		return applyMerkleUpdateWithSourceIndex(from, p.updateFrom, p.updateTo, p.sourceHints)
	}

	slots := make([]*Cell, p.srcSlots)
	consumed, err := p.replaySource(from, 0, slots)
	if err != nil {
		return nil, err
	}
	if consumed != len(p.src) {
		return nil, errPreparedPlanCorrupt
	}
	arena := merkleUpdateApplyArena{}
	root, consumed, err := p.replayDest(0, slots, make([]*Cell, p.memos), &arena)
	if err != nil {
		return nil, err
	}
	if consumed != len(p.dst) {
		return nil, errPreparedPlanCorrupt
	}
	return root, nil
}

// replaySource is merkleUpdateSourceIndex.walkProof driven by the recorded
// update-side walk instead of by a re-traversal of it. Every comparison
// walkProof makes is made here, in the same order, against the same parent
// cells; the two Hash-keyed maps become one slice write per node.
func (p *PreparedMerkleUpdate) replaySource(original *Cell, i int, slots []*Cell) (int, error) {
	step := &p.src[i]
	merkleDepth := int(step.merkleDepth)

	if step.slot < 0 {
		return 0, errPreparedPlanCorrupt
	}

	originalHash := original.HashKeyAt(merkleDepth)
	if originalHash != step.hash {
		return 0, merkleHashMismatchError(originalHash, step.hash)
	}
	// A matching hash alone is insufficient because masks and depths are part
	// of the boundary identity. On a virtualized parent the effective mask is
	// rawMask.Apply(viewLevel-1) rather than the raw one, and it is the
	// effective mask that refreshLevelMaskForRefs folds into every rebuilt
	// ancestor — so dropping this compare lets a wrong mask reach the result
	// root's hash. A pruned boundary's depth comes from its payload and is not
	// implied by any hash this walk compares.
	if err := compareMerkleBoundaryCells(original, merkleDepth, step.cell, merkleDepth); err != nil {
		return 0, fmt.Errorf("merkle update source mismatch: %w", err)
	}

	// Last write wins, exactly as walkProof's known map does: the plan gives
	// every occurrence of one hash the same slot.
	slots[step.slot] = original

	i++
	if !step.descend {
		return i, nil
	}

	refsCount := int(step.refs)
	originalRefsCount := original.refsCount()
	if originalRefsCount != refsCount {
		return 0, fmt.Errorf("merkle update source refs mismatch: got=%d want=%d", originalRefsCount, refsCount)
	}

	originalRefs := newCellRefView(original)
	for k := 0; k < refsCount; k++ {
		originalRef, err := merkleUpdateSourceTreeRefAt(&originalRefs, step.childPruned&(1<<uint(k)) != 0, k)
		if err != nil {
			return 0, err
		}
		if i, err = p.replaySource(originalRef, i, slots); err != nil {
			return 0, err
		}
	}
	return i, nil
}

// replayDest is buildMerkleUpdateCell driven by the recorded destination walk.
// Boundary substitution reads a slot instead of a Hash-keyed map, and the
// rebuild memo is a slice indexed by the slot the plan assigned per
// {hash@max, merkleDepth} — the same identity the ready map used.
func (p *PreparedMerkleUpdate) replayDest(i int, slots, memo []*Cell, arena *merkleUpdateApplyArena) (*Cell, int, error) {
	step := &p.dst[i]

	if step.boundary {
		if step.boundarySlot < 0 || int(step.boundarySlot) >= len(slots) {
			return nil, 0, errPreparedPlanCorrupt
		}
		ref := slots[step.boundarySlot]
		if ref == nil {
			// Unreachable after a successful verdict: dfsTo rejected every
			// destination boundary the source walk did not reach, and both
			// walks key on the same hashes.
			return nil, 0, merkleUpdateUnknownPrunedBranchError{hash: step.cell.HashKeyAt(int(step.merkleDepth))}
		}
		// Verify the complete boundary identity before reusing the source cell.
		// The parent side normalizes by its own level, the update side by the
		// recorded merkleDepth; that asymmetry is the check.
		if err := compareMerkleBoundaryCells(ref, ref.Level(), step.cell, int(step.merkleDepth)); err != nil {
			return nil, 0, fmt.Errorf("invalid pruned branch in merkle update: %w", err)
		}
		return ref, int(step.next), nil
	}
	if step.memo < 0 {
		return step.cell, int(step.next), nil
	}
	if ready := memo[step.memo]; ready != nil {
		return ready, int(step.next), nil
	}
	if step.deferred {
		return nil, 0, errPreparedPlanCorrupt
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:step.refs]
	child := i + 1
	changed := false
	for k := range refs {
		recorded := p.dst[child].cell
		rebuilt, next, err := p.replayDest(child, slots, memo, arena)
		if err != nil {
			return nil, 0, err
		}
		refs[k] = rebuilt
		changed = changed || rebuilt != recorded
		child = next
	}
	if child != int(step.next) {
		return nil, 0, errPreparedPlanCorrupt
	}
	if !changed {
		memo[step.memo] = step.cell
		return step.cell, int(step.next), nil
	}

	refView := newCellRefView(step.cell)
	rebuilt, _, err := cloneMerkleUpdateCellWithRefs(&refView, refs, arena)
	if err != nil {
		return nil, 0, err
	}
	memo[step.memo] = rebuilt
	return rebuilt, int(step.next), nil
}

// ---- plan recorders, driven from the walks in merkle_update.go ----

type merkleUpdateSourcePlan struct {
	steps []preparedSourceStep
	slots int
}

// newSlot returns -1 when nothing is being recorded, which is the slot every
// merkleUpdateKnownCell carries on the plain ValidateMerkleUpdate path.
func (p *merkleUpdateSourcePlan) newSlot() int32 {
	if p == nil {
		return -1
	}
	slot := int32(p.slots)
	p.slots++
	return slot
}

func (p *merkleUpdateSourcePlan) add(source *Cell, hash Hash, merkleDepth int, slot int32, refs int, descend bool) int {
	if p == nil {
		return -1
	}
	p.steps = append(p.steps, preparedSourceStep{
		cell:        source,
		hash:        hash,
		merkleDepth: int32(merkleDepth),
		slot:        slot,
		refs:        int8(refs),
		descend:     descend,
	})
	return len(p.steps) - 1
}

func (p *merkleUpdateSourcePlan) repeat(source *Cell, merkleDepth int, known *merkleUpdateHashTable[merkleUpdateKnownCell]) {
	if p == nil {
		return
	}
	// The first visit of this {hash@max, merkleDepth} inserted the hash@depth
	// entry, so the slot is always there. A -1 would mean it is not, and
	// replaySource refuses the plan rather than writing to a wrong slot.
	hash := source.HashKeyAt(merkleDepth)
	slot := int32(-1)
	if existing, ok := known.lookup(hash); ok {
		slot = existing.slot
	}
	p.add(source, hash, merkleDepth, slot, 0, false)
}

func (p *merkleUpdateSourcePlan) markPrunedChild(step, i int, pruned bool) {
	if p == nil || !pruned {
		return
	}
	p.steps[step].childPruned |= 1 << uint(i)
}

type merkleUpdateDestPlan struct {
	steps []preparedDestStep
	memos merkleUpdateVisitTable[int32]
}

func (p *merkleUpdateDestPlan) addBoundary(c *Cell, merkleDepth int, slot int32) {
	if p == nil {
		return
	}
	p.leaf(c, merkleDepth, slot, true)
}

func (p *merkleUpdateDestPlan) addPassthrough(c *Cell, merkleDepth int) {
	if p == nil {
		return
	}
	p.leaf(c, merkleDepth, -1, false)
}

func (p *merkleUpdateDestPlan) leaf(c *Cell, merkleDepth int, boundarySlot int32, boundary bool) {
	p.steps = append(p.steps, preparedDestStep{
		cell:         c,
		merkleDepth:  int32(merkleDepth),
		boundarySlot: boundarySlot,
		memo:         -1,
		next:         int32(len(p.steps) + 1),
		boundary:     boundary,
	})
}

func (p *merkleUpdateDestPlan) addRebuild(c *Cell, merkleDepth, refs int, deferred bool) int {
	if p == nil {
		return -1
	}
	key := merkleUpdateSeenKey(c, merkleDepth)
	memo, ok := p.memos.lookup(key)
	if !ok {
		memo = int32(len(p.memos.entries))
		p.memos.store(key, memo)
	}
	p.steps = append(p.steps, preparedDestStep{
		cell:         c,
		merkleDepth:  int32(merkleDepth),
		boundarySlot: -1,
		memo:         memo,
		refs:         int8(refs),
		deferred:     deferred,
	})
	return len(p.steps) - 1
}

func (p *merkleUpdateDestPlan) close(step int) {
	if p == nil || step < 0 {
		return
	}
	p.steps[step].next = int32(len(p.steps))
}
