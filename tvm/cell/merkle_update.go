package cell

import (
	"bytes"
	"fmt"
)

type merkleUpdateVisitKey struct {
	hash        Hash
	merkleDepth int
}

type merkleUpdateCellKey struct {
	cell        *Cell
	merkleDepth int
}

// merkleUpdateKnownCell remembers a source subtree together with the merkle
// depth it was visited at, so destination boundaries can be compared against
// it the same way the reference VM does.
type merkleUpdateKnownCell struct {
	cell        *Cell
	merkleDepth int
}

type merkleUpdateValidator struct {
	known     map[Hash]merkleUpdateKnownCell
	visitedTo map[merkleUpdateCellKey]struct{}
}

type merkleUpdateSourceIndex struct {
	known map[Hash]*Cell
	seen  map[merkleUpdateVisitKey]struct{}
}

type merkleUpdateApplier struct {
	ready      map[merkleUpdateVisitKey]*Cell
	reused     map[Hash]struct{}
	reusedRefs map[merkleUpdateReusedRefKey]struct{}
	reusedList []MerkleUpdateReusedCell
	refList    []MerkleUpdateReusedRef
}

type merkleUpdateUnknownPrunedBranchError struct {
	hash Hash
}

// MerkleUpdateReusedCell describes an old-state subtree reused through a
// pruned boundary while applying a Merkle update.
type MerkleUpdateReusedCell struct {
	// Hash is the logical hash of the reused subtree as it appears in the
	// returned root. It can be a virtual hash.
	Hash Hash
	// Cell carries the reference identity for storage and serialization. It may
	// be a full source cell or a pruned placeholder with the same hashes/depths.
	Cell *Cell
}

// MerkleUpdateReusedRef describes a parent->child edge where the child was
// reused through a pruned boundary while applying a Merkle update.
type MerkleUpdateReusedRef struct {
	ParentHash  Hash
	RefIndex    int
	LogicalHash Hash
	RawCell     *Cell
}

type MerkleUpdateReuse struct {
	Cells []MerkleUpdateReusedCell
	Refs  []MerkleUpdateReusedRef
}

type merkleUpdateReusedRefKey struct {
	parentHash  Hash
	refIndex    int
	logicalHash Hash
}

type merkleUpdateReusedChild struct {
	hash Hash
	raw  *Cell
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
	updateFrom, updateTo, err := merkleUpdateRootRefs(update, true)
	if err != nil {
		return err
	}

	validator := merkleUpdateValidator{
		known:     map[Hash]merkleUpdateKnownCell{},
		visitedTo: map[merkleUpdateCellKey]struct{}{},
	}

	if err := walkMerkleUpdateSource(
		updateFrom,
		0,
		map[merkleUpdateVisitKey]struct{}{},
		true,
		validator.known,
	); err != nil {
		return err
	}
	return validator.dfsTo(updateTo, 0)
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

	fromHash := from.HashKey(0)
	updateFromHash := updateFrom.HashKey(0)
	if fromHash != updateFromHash {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

// ApplyMerkleUpdate applies update to from and returns the new root with
// hashes of old-state subtrees reused through pruned boundaries.
func ApplyMerkleUpdate(from, update *Cell) (*Cell, MerkleUpdateReuse, error) {
	if from == nil {
		return nil, MerkleUpdateReuse{}, fmt.Errorf("from cell is nil")
	}
	if from.Level() != 0 {
		return nil, MerkleUpdateReuse{}, fmt.Errorf("roots have non-zero level")
	}

	updateFrom, updateTo, err := merkleUpdateRootRefs(update, true)
	if err != nil {
		return nil, MerkleUpdateReuse{}, err
	}

	fromHash := from.HashKey(0)
	updateFromHash := updateFrom.HashKey(0)
	if fromHash != updateFromHash {
		return nil, MerkleUpdateReuse{}, fmt.Errorf("invalid Merkle update: expected old value hash = %x, applied to value with hash = %x", updateFromHash, fromHash)
	}

	return applyMerkleUpdateWithSourceIndex(from, updateFrom, updateTo)
}

// CreateMerkleUpdate creates a MerkleUpdate using the same usage-tree flow as
// the reference VM: the destination proof marks reused source paths, then the
// source proof is generated from these marks.
func (t *CellUsageTree) CreateMerkleUpdate(from, to *Cell) (*Cell, error) {
	if from.Level() != 0 || to.Level() != 0 {
		return nil, fmt.Errorf("roots have non-zero level")
	}

	updateFrom, updateTo, err := t.createMerkleUpdateRaw(from, to)
	if err != nil {
		return nil, err
	}
	return CreateMerkleUpdate(updateFrom, updateTo)
}

func (t *CellUsageTree) createMerkleUpdateRaw(from, to *Cell) (*Cell, *Cell, error) {
	prevUseMark := t.useMark
	prevMarks := t.marksSnapshot()
	defer func() {
		t.useMark = prevUseMark
		t.restoreMarks(prevMarks)
	}()

	updateTo, err := buildMerkleProofBodyByPruneFunc(to, func(c *Cell) (bool, error) {
		loaded := c
		hash := c.HashKey()
		if cached, ok := t.loadedCellByHash(hash); ok {
			cached = loadedForBoundary(c, cached)
			if cached.HashKey() == hash {
				loaded = cached
			}
		}
		if loaded == c && c.IsLazy() {
			var err error
			loaded, err = c.load()
			if err != nil {
				return false, err
			}
		}
		if loaded.refsCount() == 0 {
			return false, nil
		}
		node, ok := t.NodeForCell(loaded)
		if !ok {
			return false, nil
		}
		return t.MarkPath(node), nil
	}, to.Level())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build merkle update destination proof: %w", err)
	}

	t.SetUseMarkForIsLoaded(true)
	state := newUsageProofBuildState(t.NodeCount())
	if err = collectUsageProofHashes(from, t, t.RootNode(), state); err != nil {
		return nil, nil, fmt.Errorf("failed to collect merkle update source proof: %w", err)
	}
	updateFrom, err := buildUsageProofBody(from, state, from.Level())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build merkle update source proof: %w", err)
	}
	return updateFrom, updateTo, nil
}

func applyMerkleUpdateWithSourceIndex(from, updateFrom, updateTo *Cell) (*Cell, MerkleUpdateReuse, error) {
	source := newMerkleUpdateSourceIndex(512)
	if err := source.walkProof(from, updateFrom, 0); err != nil {
		return nil, MerkleUpdateReuse{}, err
	}
	return collectMerkleUpdateRoot(updateTo, source.known, merkleUpdateReuseCap(source.known))
}

func newMerkleUpdateSourceIndex(capacity int) merkleUpdateSourceIndex {
	return merkleUpdateSourceIndex{
		known: make(map[Hash]*Cell, capacity),
		seen:  make(map[merkleUpdateVisitKey]struct{}, capacity),
	}
}

func merkleUpdateReuseCap(known map[Hash]*Cell) int {
	capacity := len(known) / 2
	if capacity < 1 {
		return 1
	}
	return capacity
}

func (e merkleUpdateUnknownPrunedBranchError) Error() string {
	return fmt.Sprintf("unknown pruned branch %x", e.hash[:])
}

func collectMerkleUpdateRoot(updateTo *Cell, known map[Hash]*Cell, reuseCap int) (*Cell, MerkleUpdateReuse, error) {
	applier := merkleUpdateApplier{
		ready:      make(map[merkleUpdateVisitKey]*Cell, len(known)),
		reused:     make(map[Hash]struct{}, reuseCap),
		reusedRefs: make(map[merkleUpdateReusedRefKey]struct{}, reuseCap),
		reusedList: make([]MerkleUpdateReusedCell, 0, reuseCap),
		refList:    make([]MerkleUpdateReusedRef, 0, reuseCap),
	}
	root, _, err := collectMerkleUpdateReuse(updateTo, 0, known, &applier)
	if err != nil {
		return nil, MerkleUpdateReuse{}, err
	}
	return root, MerkleUpdateReuse{
		Cells: applier.reusedList,
		Refs:  applier.refList,
	}, nil
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
	if b.HashKey(0) != c.HashKey(0) {
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

// compareMerkleBoundaryCells mirrors vm::detail::compare_cells from the
// reference MerkleUpdate.cpp: two cells observed at (possibly different)
// merkle depths must agree on the effective level mask and on both hash and
// depth for every level up to max(merkleDepthA, merkleDepthB), clamping each
// side to its own depth.
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

func walkMerkleUpdateSource(source *Cell, merkleDepth int, visited map[merkleUpdateVisitKey]struct{}, validateSource bool, known map[Hash]merkleUpdateKnownCell) error {
	if source == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateSeenKey(source, merkleDepth)
	if _, ok := visited[key]; ok {
		return nil
	}
	visited[key] = struct{}{}

	if validateSource {
		if err := validateLoadedCell(source); err != nil {
			return fmt.Errorf("invalid merkle update source subtree: %w", err)
		}
	}

	if existing, ok := known[source.HashKey(merkleDepth)]; ok {
		// same as the reference validator: a repeated hash must describe the
		// same cell, so masks and depths have to match too
		if err := compareMerkleBoundaryCells(source, merkleDepth, existing.cell, existing.merkleDepth); err != nil {
			return fmt.Errorf("conflicting cells in merkle update source: %w", err)
		}
	} else {
		known[source.HashKey(merkleDepth)] = merkleUpdateKnownCell{cell: source, merkleDepth: merkleDepth}
	}
	if source.GetType() == PrunedCellType {
		return nil
	}

	sourceRefs := newCellRefView(source)
	childDepth := merkleChildDepth(source, merkleDepth)
	refsCount := source.refsCount()
	for i := 0; i < refsCount; i++ {
		sourceRef, err := sourceRefs.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek source ref %d: %w", i, err)
		}
		sourceRef, err = sourceRef.load()
		if err != nil {
			return fmt.Errorf("failed to load source ref %d: %w", i, err)
		}
		if err := walkMerkleUpdateSource(sourceRef, childDepth, visited, validateSource, known); err != nil {
			return err
		}
	}
	return nil
}

func (s *merkleUpdateSourceIndex) walkProof(original, source *Cell, merkleDepth int) error {
	originalHash := original.HashKey(merkleDepth)
	sourceHash := source.HashKey(merkleDepth)
	if originalHash != sourceHash {
		return fmt.Errorf("merkle update source hash mismatch: got=%x want=%x", originalHash[:], sourceHash[:])
	}
	// reference dfs_both also requires matching level masks and depths, not
	// only the hash at the current merkle depth
	if err := compareMerkleBoundaryCells(original, merkleDepth, source, merkleDepth); err != nil {
		return fmt.Errorf("merkle update source mismatch: %w", err)
	}

	s.known[originalHash] = original

	key := merkleUpdateSeenKey(source, merkleDepth)
	if _, ok := s.seen[key]; ok {
		return nil
	}
	s.seen[key] = struct{}{}

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

func collectMerkleUpdateReuse(cell *Cell, merkleDepth int, known map[Hash]*Cell, reuse *merkleUpdateApplier) (*Cell, *merkleUpdateReusedChild, error) {
	if cell == nil {
		return nil, nil, fmt.Errorf("merkle update contains nil reference")
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		ref := known[hash]
		if ref == nil {
			return nil, nil, merkleUpdateUnknownPrunedBranchError{hash: hash}
		}
		// reference apply dfs compares the known cell at its own level with
		// the pruned boundary before reusing it
		if err := compareMerkleBoundaryCells(ref, ref.Level(), cell, merkleDepth); err != nil {
			return nil, nil, fmt.Errorf("invalid pruned branch in merkle update: %w", err)
		}
		reuse.addReusedCell(hash, ref)
		return ref, &merkleUpdateReusedChild{hash: hash, raw: ref}, nil
	}
	if cell.GetType() == PrunedCellType {
		return cell, nil, nil
	}

	refsCount := cell.refsCount()
	if refsCount == 0 {
		return cell, nil, nil
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if ready, ok := reuse.ready[key]; ok {
		return ready, nil, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refsCount]
	var reusedRefsBuf [4]*merkleUpdateReusedChild
	reusedRefs := reusedRefsBuf[:0]
	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	changed := false
	hasReused := false
	for i := 0; i < len(refs); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to peek destination ref %d: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load destination ref %d: %w", i, err)
		}
		rebuilt, reusedRef, err := collectMerkleUpdateReuse(ref, childDepth, known, reuse)
		if err != nil {
			return nil, nil, err
		}
		refs[i] = rebuilt
		reusedRefs = append(reusedRefs, reusedRef)
		changed = changed || rebuilt != ref
		hasReused = hasReused || reusedRef != nil
	}
	if !changed {
		reuse.ready[key] = cell
		return cell, nil, nil
	}
	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	if err != nil {
		return nil, nil, err
	}
	if hasReused {
		parentHash := rebuilt.HashKey()
		for i, reusedRef := range reusedRefs {
			if reusedRef != nil {
				reuse.addReusedRef(parentHash, i, reusedRef.hash, reusedRef.raw)
			}
		}
	}
	reuse.ready[key] = rebuilt
	return rebuilt, nil, nil
}

func merkleUpdateSourceTreeRef(refs *cellRefView, shapeRef *Cell, i int) (*Cell, error) {
	if shapeRef.GetType() == PrunedCellType {
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

func merkleUpdatePrunedBoundaryHash(cell *Cell, merkleDepth int) (Hash, bool) {
	if cell.GetType() != PrunedCellType || cell.Level() != merkleDepth+1 {
		return Hash{}, false
	}
	return cell.HashKey(merkleDepth), true
}

func (v *merkleUpdateValidator) dfsTo(cell *Cell, merkleDepth int) error {
	if cell == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateCellKey{cell: cell, merkleDepth: merkleDepth}
	if _, ok := v.visitedTo[key]; ok {
		return nil
	}
	v.visitedTo[key] = struct{}{}

	if err := validateLoadedCell(cell); err != nil {
		return fmt.Errorf("invalid merkle update destination subtree: %w", err)
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		knownCell, found := v.known[hash]
		if !found {
			return fmt.Errorf("unknown pruned cell in merkle update: %x", hash[:])
		}
		if err := compareMerkleBoundaryCells(cell, merkleDepth, knownCell.cell, knownCell.merkleDepth); err != nil {
			return fmt.Errorf("invalid pruned cell in merkle update: %w", err)
		}
		return nil
	}
	if cell.GetType() == PrunedCellType {
		return nil
	}

	refView := newCellRefView(cell)
	childDepth := merkleChildDepth(cell, merkleDepth)
	refsNum := cell.refsCount()
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
	return nil
}

func (a *merkleUpdateApplier) addReusedCell(hash Hash, raw *Cell) {
	if _, ok := a.reused[hash]; ok {
		return
	}
	a.reused[hash] = struct{}{}
	a.reusedList = append(a.reusedList, MerkleUpdateReusedCell{
		Hash: hash,
		Cell: raw,
	})
}

func (a *merkleUpdateApplier) addReusedRef(parentHash Hash, refIndex int, logicalHash Hash, raw *Cell) {
	key := merkleUpdateReusedRefKey{parentHash: parentHash, refIndex: refIndex, logicalHash: logicalHash}
	if _, ok := a.reusedRefs[key]; ok {
		return
	}
	a.reusedRefs[key] = struct{}{}
	a.refList = append(a.refList, MerkleUpdateReusedRef{
		ParentHash:  parentHash,
		RefIndex:    refIndex,
		LogicalHash: logicalHash,
		RawCell:     raw,
	})
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

	hash := cell.HashKey(merkleDepth)
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
	hash := cell.HashKey(merkleDepth)
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
	hash := cell.HashKey(merkleDepth)
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
	hash := cell.HashKey(merkleDepth)
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
