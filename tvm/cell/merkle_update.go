package cell

import (
	"bytes"
	"fmt"
)

type merkleUpdateVisitKey struct {
	hash        Hash
	merkleDepth int
}

// merkleUpdateHashTable is an exact Hash-keyed open-addressing table. The
// four-byte fingerprint only selects the probe sequence; a hit always compares
// the complete hash, so adversarial collisions cannot alias update boundaries.
type merkleUpdateHashTable[V any] struct {
	slots   []uint32
	entries []merkleUpdateHashEntry[V]
}

type merkleUpdateHashEntry[V any] struct {
	hash  Hash
	value V
}

// merkleUpdateVisitTable is the corresponding exact table for traversal
// identities. Merkle depth is part of the key because one physical cell may be
// visible through nested Merkle cells at more than one effective depth.
type merkleUpdateVisitTable[V any] struct {
	slots   []uint32
	entries []merkleUpdateVisitEntry[V]
}

type merkleUpdateVisitEntry[V any] struct {
	key   merkleUpdateVisitKey
	value V
}

type merkleUpdateCellVisitTable struct {
	slots   []uint32
	entries []merkleUpdateCellVisitEntry
}

type merkleUpdateCellVisitEntry struct {
	cell        *Cell
	merkleDepth int
}

func newMerkleUpdateHashTable[V any](hint int) merkleUpdateHashTable[V] {
	var table merkleUpdateHashTable[V]
	table.init(hint)
	return table
}

func (t *merkleUpdateHashTable[V]) init(hint int) {
	slots := 16
	for slots < 2*hint {
		slots *= 2
	}
	t.slots = make([]uint32, slots)
	t.entries = make([]merkleUpdateHashEntry[V], 0, hint)
}

func (t *merkleUpdateHashTable[V]) lookup(hash Hash) (V, bool) {
	if len(t.slots) == 0 {
		var zero V
		return zero, false
	}

	mask := len(t.slots) - 1
	pos := int(usageCellFingerprint(hash)) & mask
	for {
		slot := t.slots[pos]
		if slot == 0 {
			var zero V
			return zero, false
		}
		entry := &t.entries[slot-1]
		if entry.hash == hash {
			return entry.value, true
		}
		pos = (pos + 1) & mask
	}
}

func (t *merkleUpdateHashTable[V]) store(hash Hash, value V) {
	if len(t.slots) == 0 {
		t.init(16)
	}

	mask := len(t.slots) - 1
	pos := int(usageCellFingerprint(hash)) & mask
	for {
		slot := t.slots[pos]
		if slot == 0 {
			break
		}
		entry := &t.entries[slot-1]
		if entry.hash == hash {
			entry.value = value
			return
		}
		pos = (pos + 1) & mask
	}

	if (len(t.entries)+1)*2 > len(t.slots) {
		t.grow()
	}
	t.entries = append(t.entries, merkleUpdateHashEntry[V]{hash: hash, value: value})
	t.place(uint32(len(t.entries)))
}

func (t *merkleUpdateHashTable[V]) place(slot uint32) {
	entry := &t.entries[slot-1]
	mask := len(t.slots) - 1
	pos := int(usageCellFingerprint(entry.hash)) & mask
	for t.slots[pos] != 0 {
		pos = (pos + 1) & mask
	}
	t.slots[pos] = slot
}

func (t *merkleUpdateHashTable[V]) grow() {
	t.slots = make([]uint32, len(t.slots)*2)
	for i := range t.entries {
		t.place(uint32(i + 1))
	}
}

func newMerkleUpdateVisitTable[V any](hint int) merkleUpdateVisitTable[V] {
	var table merkleUpdateVisitTable[V]
	table.init(hint)
	return table
}

func (t *merkleUpdateVisitTable[V]) init(hint int) {
	slots := 16
	for slots < 2*hint {
		slots *= 2
	}
	t.slots = make([]uint32, slots)
	t.entries = make([]merkleUpdateVisitEntry[V], 0, hint)
}

func (t *merkleUpdateVisitTable[V]) lookup(key merkleUpdateVisitKey) (V, bool) {
	if len(t.slots) == 0 {
		var zero V
		return zero, false
	}

	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(key.hash, key.merkleDepth)) & mask
	for {
		slot := t.slots[pos]
		if slot == 0 {
			var zero V
			return zero, false
		}
		entry := &t.entries[slot-1]
		if entry.key.merkleDepth == key.merkleDepth && entry.key.hash == key.hash {
			return entry.value, true
		}
		pos = (pos + 1) & mask
	}
}

func (t *merkleUpdateVisitTable[V]) store(key merkleUpdateVisitKey, value V) {
	if len(t.slots) == 0 {
		t.init(16)
	}

	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(key.hash, key.merkleDepth)) & mask
	for {
		slot := t.slots[pos]
		if slot == 0 {
			break
		}
		entry := &t.entries[slot-1]
		if entry.key.merkleDepth == key.merkleDepth && entry.key.hash == key.hash {
			entry.value = value
			return
		}
		pos = (pos + 1) & mask
	}

	if (len(t.entries)+1)*2 > len(t.slots) {
		t.grow()
	}
	t.entries = append(t.entries, merkleUpdateVisitEntry[V]{key: key, value: value})
	t.place(uint32(len(t.entries)))
}

func (t *merkleUpdateVisitTable[V]) place(slot uint32) {
	entry := &t.entries[slot-1]
	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(entry.key.hash, entry.key.merkleDepth)) & mask
	for t.slots[pos] != 0 {
		pos = (pos + 1) & mask
	}
	t.slots[pos] = slot
}

func (t *merkleUpdateVisitTable[V]) grow() {
	t.slots = make([]uint32, len(t.slots)*2)
	for i := range t.entries {
		t.place(uint32(i + 1))
	}
}

func newMerkleUpdateCellVisitTable(hint int) merkleUpdateCellVisitTable {
	slots := 16
	for slots < 2*hint {
		slots *= 2
	}
	return merkleUpdateCellVisitTable{
		slots:   make([]uint32, slots),
		entries: make([]merkleUpdateCellVisitEntry, 0, hint),
	}
}

func (t *merkleUpdateCellVisitTable) contains(cell *Cell, merkleDepth int) bool {
	if len(t.slots) == 0 {
		return false
	}

	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(cell.HashKey(), merkleDepth)) & mask
	for {
		slot := t.slots[pos]
		if slot == 0 {
			return false
		}
		entry := &t.entries[slot-1]
		if entry.cell == cell && entry.merkleDepth == merkleDepth {
			return true
		}
		pos = (pos + 1) & mask
	}
}

func (t *merkleUpdateCellVisitTable) store(cell *Cell, merkleDepth int) {
	if len(t.slots) == 0 {
		*t = newMerkleUpdateCellVisitTable(16)
	}
	if (len(t.entries)+1)*2 > len(t.slots) {
		t.grow()
	}
	t.entries = append(t.entries, merkleUpdateCellVisitEntry{cell: cell, merkleDepth: merkleDepth})
	t.place(uint32(len(t.entries)))
}

func (t *merkleUpdateCellVisitTable) place(slot uint32) {
	entry := &t.entries[slot-1]
	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(entry.cell.HashKey(), entry.merkleDepth)) & mask
	for t.slots[pos] != 0 {
		pos = (pos + 1) & mask
	}
	t.slots[pos] = slot
}

func (t *merkleUpdateCellVisitTable) grow() {
	t.slots = make([]uint32, len(t.slots)*2)
	for i := range t.entries {
		t.place(uint32(i + 1))
	}
}

// merkleUpdateKnownCell keeps the depth needed to verify effective masks,
// hashes and depths when the same source boundary is encountered again.
//
// slot is the parent-side boundary slot this hash owns while a plan is being
// recorded (see merkle_update_prepared.go), and -1 otherwise. It rides here
// rather than in a second Hash-keyed map because the plan recording happens on
// the hot path that the plans exist to take Hash-keyed maps off.
type merkleUpdateKnownCell struct {
	cell        *Cell
	merkleDepth int
	slot        int32
}

type merkleUpdateValidator struct {
	known     merkleUpdateHashTable[merkleUpdateKnownCell]
	visitedTo merkleUpdateCellVisitTable
	// plan records the destination walk for a PreparedMerkleUpdate. Nil on the
	// plain ValidateMerkleUpdate path, where every method on it is a no-op, so
	// there is exactly one destination traversal in this package.
	plan *merkleUpdateDestPlan
}

type merkleUpdateSourceIndex struct {
	known merkleUpdateHashTable[*Cell]
	seen  merkleUpdateVisitTable[struct{}]
}

// merkleUpdateSourceIndexHints holds only the final table cardinalities learned
// while validating an update. It deliberately retains no cells or mutable
// traversal state from that walk.
type merkleUpdateSourceIndexHints struct {
	known int
	seen  int
}

type merkleUpdateApplier struct {
	ready merkleUpdateVisitTable[*Cell]
	arena merkleUpdateApplyArena
}

type merkleUpdateArenaCell struct {
	cell   Cell
	meta   cellMeta
	hashes [3]Hash
}

// merkleUpdateApplyArena is owned by one returned output DAG. It is deliberately
// allocated per Apply call and never pooled: pointers into its slabs become the
// published result and must remain stable for that result's whole lifetime.
type merkleUpdateApplyArena struct {
	free     []merkleUpdateArenaCell
	slabSize int
}

func (a *merkleUpdateApplyArena) take() *merkleUpdateArenaCell {
	if len(a.free) == 0 {
		switch {
		case a.slabSize == 0:
			a.slabSize = 8
		case a.slabSize < 64:
			a.slabSize *= 2
		}
		a.free = make([]merkleUpdateArenaCell, a.slabSize)
	}

	out := &a.free[0]
	a.free = a.free[1:]
	return out
}

type merkleUpdateUnknownPrunedBranchError struct {
	hash Hash
}

type merkleCombineNodeID uint32

const merkleCombineRootNode merkleCombineNodeID = 1

type merkleCombineNode struct {
	parent   merkleCombineNodeID
	children [4]merkleCombineNodeID
	marked   bool
}

type merkleCombineUsage struct {
	nodes []merkleCombineNode
}

type merkleCombineInfo struct {
	cell   *Cell
	pruned [_DataCellMaxLevel + 1]*Cell
	aNode  merkleCombineNodeID
}

type merkleUpdateCombiner struct {
	cells        map[Hash]*merkleCombineInfo
	loadVisited  map[merkleUpdateVisitKey]struct{}
	createAReady map[merkleUpdateVisitKey]*Cell
	createDReady map[merkleUpdateVisitKey]*Cell
	usage        *merkleCombineUsage
}

func ValidateMerkleUpdate(update *Cell) error {
	_, _, _, err := merkleUpdateVerdict(update, nil, nil)
	return err
}

// merkleUpdateVerdict runs the two update-side walks that decide a Merkle
// update's verdict, optionally recording them into plans.
//
// The verdict is a pure function of update: neither walk is given a source
// root, which is why PrepareMerkleUpdate can decide it once for parents it has
// never seen. src and dst are nil for ValidateMerkleUpdate, so both callers go
// through the same traversal rather than through two copies of it.
func merkleUpdateVerdict(
	update *Cell,
	src *merkleUpdateSourcePlan,
	dst *merkleUpdateDestPlan,
) (*Cell, *Cell, merkleUpdateSourceIndexHints, error) {
	updateFrom, updateTo, err := merkleUpdateRootRefs(update, true)
	if err != nil {
		return nil, nil, merkleUpdateSourceIndexHints{}, err
	}

	validator := merkleUpdateValidator{
		known:     newMerkleUpdateHashTable[merkleUpdateKnownCell](32),
		visitedTo: newMerkleUpdateCellVisitTable(32),
		plan:      dst,
	}
	visitedFrom := newMerkleUpdateVisitTable[struct{}](32)

	if err := walkMerkleUpdateSource(
		updateFrom,
		0,
		&visitedFrom,
		true,
		&validator.known,
		src,
	); err != nil {
		return nil, nil, merkleUpdateSourceIndexHints{}, err
	}
	if err := validator.dfsTo(updateTo, 0); err != nil {
		return nil, nil, merkleUpdateSourceIndexHints{}, err
	}
	return updateFrom, updateTo, merkleUpdateSourceIndexHints{
		known: len(validator.known.entries),
		seen:  len(visitedFrom.entries),
	}, nil
}

func MayApplyMerkleUpdate(from, update *Cell) error {
	if from == nil {
		return fmt.Errorf("from cell is nil")
	}
	if from.Level() != 0 {
		return fmt.Errorf("level of update or from is not zero")
	}

	updateFrom, _, err := merkleUpdateRootRefs(update, false)
	if err != nil {
		return err
	}

	fromHash := from.HashKeyAt(0)
	updateFromHash := updateFrom.HashKeyAt(0)
	if fromHash != updateFromHash {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

// ApplyMerkleUpdate applies update to from and returns the new root. Subtrees the
// update did not touch are the source cells themselves, so the result shares
// memory with from rather than duplicating it.
func ApplyMerkleUpdate(from, update *Cell) (*Cell, error) {
	return applyMerkleUpdateWithHints(from, update, merkleUpdateSourceIndexHints{
		known: 32,
		seen:  32,
	})
}

func applyMerkleUpdateWithHints(from, update *Cell, hints merkleUpdateSourceIndexHints) (*Cell, error) {
	if from == nil {
		return nil, fmt.Errorf("from cell is nil")
	}
	if from.Level() != 0 {
		return nil, fmt.Errorf("roots have non-zero level")
	}

	updateFrom, updateTo, err := merkleUpdateRootRefs(update, true)
	if err != nil {
		return nil, err
	}

	fromHash := from.HashKeyAt(0)
	updateFromHash := updateFrom.HashKeyAt(0)
	if fromHash != updateFromHash {
		return nil, fmt.Errorf("invalid Merkle update: expected old value hash = %x, applied to value with hash = %x", updateFromHash, fromHash)
	}

	return applyMerkleUpdateWithSourceIndex(from, updateFrom, updateTo, hints)
}

func applyMerkleUpdateWithSourceIndex(from, updateFrom, updateTo *Cell, hints merkleUpdateSourceIndexHints) (*Cell, error) {
	source := newMerkleUpdateSourceIndex(hints)
	if err := source.walkProof(from, updateFrom, 0); err != nil {
		return nil, err
	}
	return buildMerkleUpdateRoot(updateTo, source.known)
}

func newMerkleUpdateSourceIndex(hints merkleUpdateSourceIndexHints) merkleUpdateSourceIndex {
	return merkleUpdateSourceIndex{
		known: newMerkleUpdateHashTable[*Cell](hints.known),
		seen:  newMerkleUpdateVisitTable[struct{}](hints.seen),
	}
}

func (e merkleUpdateUnknownPrunedBranchError) Error() string {
	return fmt.Sprintf("unknown pruned branch %x", e.hash[:])
}

// buildMerkleUpdateRoot rebuilds the destination root, substituting the source
// subtree for every pruned boundary the update reaches.
func buildMerkleUpdateRoot(updateTo *Cell, known merkleUpdateHashTable[*Cell]) (*Cell, error) {
	applier := merkleUpdateApplier{ready: newMerkleUpdateVisitTable[*Cell](len(known.entries))}
	return buildMerkleUpdateCell(updateTo, 0, &known, &applier)
}

func CombineMerkleUpdate(ab, bc *Cell) (*Cell, error) {
	a, b, err := merkleUpdateRootRefs(ab, true)
	if err != nil {
		return nil, err
	}
	c, d, err := merkleUpdateRootRefs(bc, true)
	if err != nil {
		return nil, err
	}
	if b.HashKeyAt(0) != c.HashKeyAt(0) {
		return nil, fmt.Errorf("impossible to combine merkle updates: intermediate hash mismatch")
	}

	combiner := merkleUpdateCombiner{
		cells:        map[Hash]*merkleCombineInfo{},
		loadVisited:  map[merkleUpdateVisitKey]struct{}{},
		createAReady: map[merkleUpdateVisitKey]*Cell{},
		createDReady: map[merkleUpdateVisitKey]*Cell{},
		usage:        newMerkleCombineUsage(),
	}

	for _, root := range []*Cell{a, b, c, d} {
		if err := combiner.loadCells(root, 0); err != nil {
			return nil, err
		}
	}
	if err := combiner.markA(a, 0, merkleCombineRootNode); err != nil {
		return nil, err
	}

	newD, err := combiner.createD(d, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to combine merkle updates: %w", err)
	}
	newA, err := combiner.createA(a, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to combine merkle updates: %w", err)
	}
	return CreateMerkleUpdate(newA, newD)
}

func merkleUpdateRootRefs(update *Cell, validate bool) (*Cell, *Cell, error) {
	if update == nil {
		return nil, nil, fmt.Errorf("merkle update cell is nil")
	}
	if update.Level() != 0 {
		return nil, nil, fmt.Errorf("merkle update has non-zero level")
	}
	if update.GetType() != MerkleUpdateCellType {
		return nil, nil, fmt.Errorf("not a MerkleUpdate cell")
	}
	if update.refsCount() != 2 {
		return nil, nil, fmt.Errorf("wrong references count for a merkle update special cell")
	}

	refView := newCellRefView(update)
	updateFrom, err := refView.boundaryRef(0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to peek merkle update first ref: %w", err)
	}
	if !validate {
		// may_apply only needs the source boundary metadata. In particular, it
		// must not materialize the destination subtree just to answer whether
		// the update can apply to a given source root.
		return updateFrom, nil, nil
	}
	updateFrom, err = updateFrom.load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load merkle update first ref: %w", err)
	}
	updateTo, err := refView.boundaryRef(1)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to peek merkle update second ref: %w", err)
	}
	updateTo, err = updateTo.load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load merkle update second ref: %w", err)
	}
	if err := validateLoadedCell(update); err != nil {
		return nil, nil, fmt.Errorf("invalid merkle update cell: %w", err)
	}
	return updateFrom, updateTo, nil
}

func merkleUpdateSeenKey(cell *Cell, merkleDepth int) merkleUpdateVisitKey {
	return merkleUpdateVisitKey{hash: cell.HashKey(), merkleDepth: merkleDepth}
}

// compareMerkleBoundaryCells compares cells at possibly different Merkle
// depths. Effective level masks and every visible hash and depth must match.
func compareMerkleBoundaryCells(a *Cell, merkleDepthA int, b *Cell, merkleDepthB int) error {
	if a == nil || b == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}
	if a.getLevelMask().Apply(merkleDepthA) != b.getLevelMask().Apply(merkleDepthB) {
		return fmt.Errorf("level mask mismatch")
	}
	for i := 0; i <= max(merkleDepthA, merkleDepthB); i++ {
		levelA, levelB := min(i, merkleDepthA), min(i, merkleDepthB)
		if !bytes.Equal(a.getHash(levelA), b.getHash(levelB)) {
			return fmt.Errorf("cell hash mismatch")
		}
		if a.getDepth(levelA) != b.getDepth(levelB) {
			return fmt.Errorf("cell depth mismatch")
		}
	}
	return nil
}

func merkleChildDepth(cell *Cell, merkleDepth int) int {
	return int(childEffectiveLevelFor(cell, uint8(merkleDepth)))
}

func normalizeMerkleDepth(cell *Cell, merkleDepth int) int {
	return cell.getLevelMask().Apply(merkleDepth).GetLevel()
}

func walkMerkleUpdateSource(source *Cell, merkleDepth int, visited *merkleUpdateVisitTable[struct{}], validateSource bool, known *merkleUpdateHashTable[merkleUpdateKnownCell], plan *merkleUpdateSourcePlan) error {
	if source == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateSeenKey(source, merkleDepth)
	if _, ok := visited.lookup(key); ok {
		// walkProof does NOT stop here: it compares the boundary and rewrites
		// its parent index before its own seen check. A recorded revisit is
		// therefore a real step, carrying the same slot the first visit
		// assigned, and only its descent is suppressed.
		plan.repeat(source, merkleDepth, known)
		return nil
	}
	visited.store(key, struct{}{})

	if validateSource {
		if err := validateLoadedCell(source); err != nil {
			return fmt.Errorf("invalid merkle update source subtree: %w", err)
		}
	}

	hash := source.HashKeyAt(merkleDepth)
	slot := int32(-1)
	if existing, ok := known.lookup(hash); ok {
		// A repeated hash must describe the same effective cell, including its
		// mask and depth.
		if err := compareMerkleBoundaryCells(source, merkleDepth, existing.cell, existing.merkleDepth); err != nil {
			return fmt.Errorf("conflicting cells in merkle update source: %w", err)
		}
		slot = existing.slot
	} else {
		slot = plan.newSlot()
		known.store(hash, merkleUpdateKnownCell{cell: source, merkleDepth: merkleDepth, slot: slot})
	}
	if source.GetType() == PrunedCellType {
		plan.add(source, hash, merkleDepth, slot, 0, false)
		return nil
	}

	sourceRefs := newCellRefView(source)
	childDepth := merkleChildDepth(source, merkleDepth)
	refsCount := source.refsCount()
	step := plan.add(source, hash, merkleDepth, slot, refsCount, true)
	for i := 0; i < refsCount; i++ {
		sourceRef, err := sourceRefs.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek source ref %d: %w", i, err)
		}
		sourceRef, err = sourceRef.load()
		if err != nil {
			return fmt.Errorf("failed to load source ref %d: %w", i, err)
		}
		// The parent-side load/virtualize choice for this edge, decided from the
		// update-source shape and recorded now so that no replay can ever
		// consult the parent for it. See merkleUpdateSourceTreeRef.
		plan.markPrunedChild(step, i, sourceRef.GetType() == PrunedCellType)
		if err := walkMerkleUpdateSource(sourceRef, childDepth, visited, validateSource, known, plan); err != nil {
			return err
		}
	}
	return nil
}

func (s *merkleUpdateSourceIndex) walkProof(original, source *Cell, merkleDepth int) error {
	originalHash := original.HashKeyAt(merkleDepth)
	sourceHash := source.HashKeyAt(merkleDepth)
	if originalHash != sourceHash {
		return merkleHashMismatchError(originalHash, sourceHash)
	}
	// A matching hash alone is insufficient because masks and depths are part
	// of the boundary identity.
	if err := compareMerkleBoundaryCells(original, merkleDepth, source, merkleDepth); err != nil {
		return fmt.Errorf("merkle update source mismatch: %w", err)
	}

	s.known.store(originalHash, original)

	key := merkleUpdateSeenKey(source, merkleDepth)
	if _, ok := s.seen.lookup(key); ok {
		return nil
	}
	s.seen.store(key, struct{}{})

	if source.GetType() == PrunedCellType {
		return nil
	}

	refsCount := source.refsCount()
	originalRefsCount := original.refsCount()
	if originalRefsCount != refsCount {
		return fmt.Errorf("merkle update source refs mismatch: got=%d want=%d", originalRefsCount, refsCount)
	}

	sourceRefs := newCellRefView(source)
	originalRefs := newCellRefView(original)
	childDepth := merkleChildDepth(source, merkleDepth)
	for i := 0; i < refsCount; i++ {
		sourceRef, err := sourceRefs.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek source ref %d: %w", i, err)
		}
		sourceRef, err = sourceRef.load()
		if err != nil {
			return fmt.Errorf("failed to load source ref %d: %w", i, err)
		}

		originalRef, err := merkleUpdateSourceTreeRef(&originalRefs, sourceRef, i)
		if err != nil {
			return err
		}
		if err := s.walkProof(originalRef, sourceRef, childDepth); err != nil {
			return err
		}
	}
	return nil
}

func buildMerkleUpdateCell(cell *Cell, merkleDepth int, known *merkleUpdateHashTable[*Cell], reuse *merkleUpdateApplier) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		ref, found := known.lookup(hash)
		if !found || ref == nil {
			return nil, merkleUpdateUnknownPrunedBranchError{hash: hash}
		}
		// Verify the complete boundary identity before reusing the source cell.
		if err := compareMerkleBoundaryCells(ref, ref.Level(), cell, merkleDepth); err != nil {
			return nil, fmt.Errorf("invalid pruned branch in merkle update: %w", err)
		}
		return ref, nil
	}
	if cell.GetType() == PrunedCellType {
		return cell, nil
	}

	refsCount := cell.refsCount()
	if refsCount == 0 {
		return cell, nil
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if ready, ok := reuse.ready.lookup(key); ok {
		return ready, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refsCount]
	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	changed := false
	for i := 0; i < len(refs); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek destination ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return nil, fmt.Errorf("failed to load destination ref %d: %w", i, err)
		}
		rebuilt, err := buildMerkleUpdateCell(ref, childDepth, known, reuse)
		if err != nil {
			return nil, err
		}
		refs[i] = rebuilt
		changed = changed || rebuilt != ref
	}
	if !changed {
		reuse.ready.store(key, cell)
		return cell, nil
	}
	rebuilt, _, err := cloneMerkleUpdateCellWithRefs(&refView, refs, &reuse.arena)
	if err != nil {
		return nil, err
	}
	reuse.ready.store(key, rebuilt)
	return rebuilt, nil
}

// cloneMerkleUpdateCellWithRefs mirrors cellRefView.cloneWithRefs but places
// the Cell and all mutable metadata it may need in the output-owned arena.
// Source data bytes are immutable and remain shared, just as Cell.copy does.
func cloneMerkleUpdateCellWithRefs(view *cellRefView, refs []*Cell, arena *merkleUpdateApplyArena) (*Cell, bool, error) {
	refCnt := int(view.refCnt)
	if len(refs) != refCnt {
		return nil, false, fmt.Errorf("unexpected refs count: got %d want %d", len(refs), refCnt)
	}

	materialize := view.virtual
	changed := materialize
	for i, ref := range refs {
		oldRef, err := view.boundaryRef(i)
		if err != nil {
			return nil, false, err
		}
		if ref != oldRef {
			changed = true
		}
	}
	if !changed {
		return view.cell, false, nil
	}

	storage := arena.take()
	cloned := &storage.cell
	*cloned = *view.cell
	if sourceMeta := view.cell.meta; sourceMeta != nil {
		storage.meta = *sourceMeta
		storage.meta.trace = nil
		if sourceMeta.extraHashes != nil {
			storage.hashes = *sourceMeta.extraHashes
			storage.meta.extraHashes = &storage.hashes
		}
		cloned.meta = &storage.meta
		cloned.clearMetaIfEmpty()
	} else {
		cloned.meta = nil
	}
	if materialize {
		cloned.clearVirtualization()
	}
	for i, ref := range refs {
		cloned.setRef(i, ref)
	}

	if err := cloned.refreshLevelMaskForRefs(); err != nil {
		return nil, false, err
	}
	// calculateHashes needs an extra-hash array only for a multi-hash cell.
	// Seed that storage from the same slab so ensureMeta cannot allocate it on
	// the heap independently of the returned cell.
	levelMask := cloned.getLevelMask()
	typ := cloned.resolveType()
	hashCount := levelMask.getHashIndex() + 1
	if typ == PrunedCellType {
		hashCount = 1
	}
	if hashCount > 1 {
		if cloned.meta == nil {
			storage.meta = cellMeta{}
			cloned.meta = &storage.meta
		}
		cloned.meta.extraHashes = &storage.hashes
	}
	if err := cloned.calculateHashes(); err != nil {
		return nil, false, err
	}
	return cloned, true, nil
}

func merkleUpdateSourceTreeRef(refs *cellRefView, shapeRef *Cell, i int) (*Cell, error) {
	return merkleUpdateSourceTreeRefAt(refs, shapeRef.GetType() == PrunedCellType, i)
}

// merkleUpdateSourceTreeRefAt is the single place where a source-tree edge is
// either virtualized without touching storage (a pruned shape ref) or actually
// loaded. shapePruned is read from the UPDATE, never from the parent: choosing
// from the parent is how a walk starts loading subtrees the reference node
// never loads, or virtualizes where it must load.
func merkleUpdateSourceTreeRefAt(refs *cellRefView, shapePruned bool, i int) (*Cell, error) {
	if shapePruned {
		return refs.logicalBoundaryRef(i), nil
	}

	ref, err := refs.boundaryRef(i)
	if err != nil {
		return nil, fmt.Errorf("failed to peek source tree ref %d: %w", i, err)
	}
	ref, err = ref.load()
	if err != nil {
		return nil, fmt.Errorf("failed to load source tree ref %d: %w", i, err)
	}
	return ref, nil
}

// merkleHashMismatchError and merkleUnknownPrunedError keep the %x formatting
// of never-taken branches out of the hot walks: hash arrays passed to
// fmt.Errorf inline escape to the heap once per visited node even though the
// error path never runs.
func merkleHashMismatchError(got, want Hash) error {
	return fmt.Errorf("merkle update source hash mismatch: got=%x want=%x", got[:], want[:])
}

func merkleUnknownPrunedError(hash Hash) error {
	return fmt.Errorf("unknown pruned cell in merkle update: %x", hash[:])
}

func merkleUpdatePrunedBoundaryHash(cell *Cell, merkleDepth int) (Hash, bool) {
	if cell.GetType() != PrunedCellType || cell.Level() != merkleDepth+1 {
		return Hash{}, false
	}
	return cell.HashKeyAt(merkleDepth), true
}

func (v *merkleUpdateValidator) dfsTo(cell *Cell, merkleDepth int) error {
	if cell == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	// Validation is pointer-keyed on purpose. Two distinct cells may expose the
	// same hash while carrying different lazy descendants or traces; both must
	// still be descended so the second load/error/read is observable. The build
	// memo remains hash-keyed because it only deduplicates an already-validated
	// output identity.
	repeat := v.visitedTo.contains(cell, merkleDepth)
	if repeat && v.plan == nil {
		return nil
	}
	if !repeat {
		v.visitedTo.store(cell, merkleDepth)

		if err := validateLoadedCell(cell); err != nil {
			return fmt.Errorf("invalid merkle update destination subtree: %w", err)
		}
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		knownCell, found := v.known.lookup(hash)
		if !found {
			return merkleUnknownPrunedError(hash)
		}
		if !repeat {
			if err := compareMerkleBoundaryCells(cell, merkleDepth, knownCell.cell, knownCell.merkleDepth); err != nil {
				return fmt.Errorf("invalid pruned cell in merkle update: %w", err)
			}
		}
		v.plan.addBoundary(cell, merkleDepth, knownCell.slot)
		return nil
	}
	if cell.GetType() == PrunedCellType {
		v.plan.addPassthrough(cell, merkleDepth)
		return nil
	}

	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	refsNum := cell.refsCount()
	if refsNum == 0 {
		// buildMerkleUpdateCell hands a childless destination cell straight
		// back, without memoizing it, so the plan records the same.
		v.plan.addPassthrough(cell, merkleDepth)
		return nil
	}
	step := v.plan.addRebuild(cell, merkleDepth, refsNum, repeat)
	if repeat {
		// The first visit of this pointer already recorded the subtree, and the
		// replay reaches this step only through a filled memo slot.
		v.plan.close(step)
		return nil
	}
	for i := 0; i < refsNum; i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek destination ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return fmt.Errorf("failed to load destination ref %d: %w", i, err)
		}
		if err := v.dfsTo(ref, childDepth); err != nil {
			return err
		}
	}
	v.plan.close(step)
	return nil
}

func newMerkleCombineUsage() *merkleCombineUsage {
	return &merkleCombineUsage{
		nodes: make([]merkleCombineNode, 2),
	}
}

func (u *merkleCombineUsage) child(parent merkleCombineNodeID, refIdx int) merkleCombineNodeID {
	if parent == 0 || int(parent) >= len(u.nodes) || refIdx < 0 || refIdx >= 4 {
		return 0
	}
	if child := u.nodes[parent].children[refIdx]; child != 0 {
		return child
	}

	child := merkleCombineNodeID(len(u.nodes))
	u.nodes = append(u.nodes, merkleCombineNode{parent: parent})
	u.nodes[parent].children[refIdx] = child
	return child
}

func (u *merkleCombineUsage) markPath(node merkleCombineNodeID) {
	if node == 0 || int(node) >= len(u.nodes) {
		return
	}
	for node = u.nodes[node].parent; node != 0; node = u.nodes[node].parent {
		if u.nodes[node].marked {
			return
		}
		u.nodes[node].marked = true
	}
}

func (u *merkleCombineUsage) hasMark(node merkleCombineNodeID) bool {
	return node != 0 && int(node) < len(u.nodes) && u.nodes[node].marked
}

func (i *merkleCombineInfo) getPruned(depth int) *Cell {
	if depth < 0 || depth >= len(i.pruned) {
		return nil
	}
	return i.pruned[depth]
}

func (i *merkleCombineInfo) putPruned(cell *Cell) {
	if cell == nil {
		return
	}
	idx := cell.Level() - 1
	if idx < 0 || idx >= len(i.pruned) || i.pruned[idx] != nil {
		return
	}
	i.pruned[idx] = cell
}

func (i *merkleCombineInfo) getAnyCell() *Cell {
	if i.cell != nil {
		return i.cell
	}
	for _, pruned := range i.pruned {
		if pruned != nil {
			return pruned
		}
	}
	return nil
}

func (c *merkleUpdateCombiner) loadCells(cell *Cell, merkleDepth int) error {
	if cell == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if _, ok := c.loadVisited[key]; ok {
		return nil
	}
	c.loadVisited[key] = struct{}{}

	hash := cell.HashKeyAt(merkleDepth)
	info := c.cells[hash]
	if info == nil {
		info = &merkleCombineInfo{}
		c.cells[hash] = info
	}
	if cell.GetType() == PrunedCellType && cell.Level() > merkleDepth {
		info.putPruned(cell)
		return nil
	}

	info.cell = cell
	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	refsCount := cell.refsCount()
	for i := 0; i < refsCount; i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek combine ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return fmt.Errorf("failed to load combine ref %d: %w", i, err)
		}
		if err := c.loadCells(ref, childDepth); err != nil {
			return err
		}
	}
	return nil
}

func (c *merkleUpdateCombiner) markA(cell *Cell, merkleDepth int, node merkleCombineNodeID) error {
	if cell == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}
	if node == 0 {
		return fmt.Errorf("invalid merkle update combine node")
	}

	merkleDepth = normalizeMerkleDepth(cell, merkleDepth)
	hash := cell.HashKeyAt(merkleDepth)
	info := c.cells[hash]
	if info == nil {
		return fmt.Errorf("missing cached A subtree %x", hash)
	}
	if info.aNode != 0 {
		return nil
	}
	info.aNode = node
	if info.cell == nil {
		return nil
	}

	refView := newCellRefView(info.cell)
	childDepth := merkleChildDepth(info.cell, merkleDepth)
	refsCount := info.cell.refsCount()
	for i := 0; i < refsCount; i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek source ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return fmt.Errorf("failed to load source ref %d: %w", i, err)
		}
		if err := c.markA(ref, childDepth, c.usage.child(node, i)); err != nil {
			return err
		}
	}
	return nil
}

func (c *merkleUpdateCombiner) rebuildRefs(cell *Cell, merkleDepth, cacheDepth int, rebuild func(*Cell, int, int) (*Cell, error)) (*Cell, error) {
	var refsBuf [4]*Cell
	refs := refsBuf[:cell.refsCount()]
	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	childCacheDepth := merkleChildDepth(cell, cacheDepth)
	for i := 0; i < len(refs); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek combine ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return nil, fmt.Errorf("failed to load combine ref %d: %w", i, err)
		}
		rebuilt, err := rebuild(ref, childDepth, childCacheDepth)
		if err != nil {
			return nil, err
		}
		refs[i] = rebuilt
	}
	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	return rebuilt, err
}

func (c *merkleUpdateCombiner) createD(cell *Cell, merkleDepth, dMerkleDepth int) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	merkleDepth = normalizeMerkleDepth(cell, merkleDepth)
	hash := cell.HashKeyAt(merkleDepth)
	key := merkleUpdateVisitKey{hash: hash, merkleDepth: dMerkleDepth}
	if ready, ok := c.createDReady[key]; ok {
		return ready, nil
	}

	rebuilt, err := c.doCreateD(merkleDepth, dMerkleDepth, hash)
	if err != nil {
		return nil, err
	}
	c.createDReady[key] = rebuilt
	return rebuilt, nil
}

func (c *merkleUpdateCombiner) doCreateD(merkleDepth, dMerkleDepth int, hash Hash) (*Cell, error) {
	info := c.cells[hash]
	if info == nil {
		return nil, fmt.Errorf("missing cached combine subtree %x", hash)
	}
	if info.aNode != 0 {
		c.usage.markPath(info.aNode)

		if pruned := info.getPruned(dMerkleDepth); pruned != nil {
			return pruned, nil
		}
		return createPrunedBranchForCombine(info.getAnyCell(), dMerkleDepth+1, merkleDepth)
	}
	if info.cell == nil {
		return nil, fmt.Errorf("missing concrete destination subtree %x", hash)
	}
	if info.cell.refsCount() == 0 {
		return info.cell, nil
	}
	return c.rebuildRefs(info.cell, merkleDepth, dMerkleDepth, c.createD)
}

func (c *merkleUpdateCombiner) createA(cell *Cell, merkleDepth, aMerkleDepth int) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	merkleDepth = normalizeMerkleDepth(cell, merkleDepth)
	hash := cell.HashKeyAt(merkleDepth)
	key := merkleUpdateVisitKey{hash: hash, merkleDepth: aMerkleDepth}
	if ready, ok := c.createAReady[key]; ok {
		return ready, nil
	}

	rebuilt, err := c.doCreateA(merkleDepth, aMerkleDepth, hash)
	if err != nil {
		return nil, err
	}
	c.createAReady[key] = rebuilt
	return rebuilt, nil
}

func (c *merkleUpdateCombiner) doCreateA(merkleDepth, aMerkleDepth int, hash Hash) (*Cell, error) {
	info := c.cells[hash]
	if info == nil {
		return nil, fmt.Errorf("missing cached combine subtree %x", hash)
	}
	if info.aNode == 0 {
		return nil, fmt.Errorf("missing A usage node for subtree %x", hash)
	}
	if !c.usage.hasMark(info.aNode) {
		if pruned := info.getPruned(aMerkleDepth); pruned != nil {
			return pruned, nil
		}
		return createPrunedBranchForCombine(info.getAnyCell(), aMerkleDepth+1, merkleDepth)
	}
	if info.cell == nil {
		return nil, fmt.Errorf("missing concrete source subtree %x", hash)
	}
	if info.cell.refsCount() == 0 {
		return info.cell, nil
	}
	return c.rebuildRefs(info.cell, merkleDepth, aMerkleDepth, c.createA)
}

func createPrunedBranchForCombine(source *Cell, newLevel, virtLevel int) (*Cell, error) {
	if !source.IsVirtualized() && source.Level() <= virtLevel && source.refsCount() == 0 {
		return source, nil
	}
	return CreatePrunedBranch(source, newLevel, virtLevel)
}

// CreateMerkleUpdate wraps already-built old and new update bodies into a
// MerkleUpdate special cell.
func CreateMerkleUpdate(left, right *Cell) (*Cell, error) {
	builder := BeginCell()
	if err := builder.StoreUInt(uint64(MerkleUpdateCellType), 8); err != nil {
		return nil, fmt.Errorf("failed to store merkle update type: %w", err)
	}
	if err := builder.StoreSlice(left.getHash(0), hashSize*8); err != nil {
		return nil, fmt.Errorf("failed to store left hash: %w", err)
	}
	if err := builder.StoreSlice(right.getHash(0), hashSize*8); err != nil {
		return nil, fmt.Errorf("failed to store right hash: %w", err)
	}
	if err := builder.StoreUInt(uint64(left.getDepth(0)), depthSize*8); err != nil {
		return nil, fmt.Errorf("failed to store left depth: %w", err)
	}
	if err := builder.StoreUInt(uint64(right.getDepth(0)), depthSize*8); err != nil {
		return nil, fmt.Errorf("failed to store right depth: %w", err)
	}
	if err := builder.StoreRef(left); err != nil {
		return nil, fmt.Errorf("failed to store left ref: %w", err)
	}
	if err := builder.StoreRef(right); err != nil {
		return nil, fmt.Errorf("failed to store right ref: %w", err)
	}
	return finalizeCellFromBuilder(builder, true)
}
