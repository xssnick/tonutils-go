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

// merkleUpdateKnownCell keeps the depth needed to verify effective masks,
// hashes and depths when the same source boundary is encountered again.
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
	ready map[merkleUpdateVisitKey]*Cell
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

	return applyMerkleUpdateWithSourceIndex(from, updateFrom, updateTo)
}

func applyMerkleUpdateWithSourceIndex(from, updateFrom, updateTo *Cell) (*Cell, error) {
	source := newMerkleUpdateSourceIndex(32)
	if err := source.walkProof(from, updateFrom, 0); err != nil {
		return nil, err
	}
	return buildMerkleUpdateRoot(updateTo, source.known)
}

func newMerkleUpdateSourceIndex(capacity int) merkleUpdateSourceIndex {
	return merkleUpdateSourceIndex{
		known: make(map[Hash]*Cell, capacity),
		seen:  make(map[merkleUpdateVisitKey]struct{}, capacity),
	}
}

func (e merkleUpdateUnknownPrunedBranchError) Error() string {
	return fmt.Sprintf("unknown pruned branch %x", e.hash[:])
}

// buildMerkleUpdateRoot rebuilds the destination root, substituting the source
// subtree for every pruned boundary the update reaches.
func buildMerkleUpdateRoot(updateTo *Cell, known map[Hash]*Cell) (*Cell, error) {
	applier := merkleUpdateApplier{ready: make(map[merkleUpdateVisitKey]*Cell, len(known))}
	return buildMerkleUpdateCell(updateTo, 0, known, &applier)
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

	if existing, ok := known[source.HashKeyAt(merkleDepth)]; ok {
		// A repeated hash must describe the same effective cell, including its
		// mask and depth.
		if err := compareMerkleBoundaryCells(source, merkleDepth, existing.cell, existing.merkleDepth); err != nil {
			return fmt.Errorf("conflicting cells in merkle update source: %w", err)
		}
	} else {
		known[source.HashKeyAt(merkleDepth)] = merkleUpdateKnownCell{cell: source, merkleDepth: merkleDepth}
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

func buildMerkleUpdateCell(cell *Cell, merkleDepth int, known map[Hash]*Cell, reuse *merkleUpdateApplier) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	if hash, ok := merkleUpdatePrunedBoundaryHash(cell, merkleDepth); ok {
		ref := known[hash]
		if ref == nil {
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
	if ready, ok := reuse.ready[key]; ok {
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
		reuse.ready[key] = cell
		return cell, nil
	}
	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	if err != nil {
		return nil, err
	}
	reuse.ready[key] = rebuilt
	return rebuilt, nil
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
			return merkleUnknownPrunedError(hash)
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
