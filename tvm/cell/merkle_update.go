package cell

import "fmt"

type merkleUpdateVisitKey struct {
	hash        Hash
	merkleDepth int
}

type merkleUpdateValidator struct {
	known     map[Hash]struct{}
	visitedTo map[merkleUpdateVisitKey]struct{}
}

type merkleUpdateApplier struct {
	known map[Hash]*Cell
	ready map[merkleUpdateVisitKey]*Cell
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
		known:     map[Hash]struct{}{},
		visitedTo: map[merkleUpdateVisitKey]struct{}{},
	}

	if err := walkMerkleUpdateSource(
		updateFrom,
		updateFrom,
		0,
		map[merkleUpdateVisitKey]struct{}{},
		true,
		func(source *Cell, merkleDepth int) {
			validator.known[source.HashKey(merkleDepth)] = struct{}{}
		},
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

	if from.HashKey(0) != updateFrom.HashKey(0) {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

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

	if from.HashKey(0) != updateFrom.HashKey(0) {
		return nil, fmt.Errorf("invalid Merkle update: expected old value hash = %x, applied to value with hash = %x", updateFrom.HashKey(0), from.HashKey(0))
	}

	applier := merkleUpdateApplier{
		known: map[Hash]*Cell{},
		ready: map[merkleUpdateVisitKey]*Cell{},
	}

	if err := walkMerkleUpdateSource(
		from,
		updateFrom,
		0,
		map[merkleUpdateVisitKey]struct{}{},
		false,
		func(source *Cell, merkleDepth int) {
			applier.known[source.HashKey(merkleDepth)] = source
		},
	); err != nil {
		return nil, err
	}
	return applier.rebuild(updateTo, 0)
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
	return createMerkleUpdateCell(newA, newD)
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

	updateFrom := update.ref(0)
	updateTo := update.ref(1)
	if updateFrom == nil || updateTo == nil {
		return nil, nil, fmt.Errorf("merkle update cell has nil references")
	}
	if validate {
		if err := validateLoadedCell(update); err != nil {
			return nil, nil, fmt.Errorf("invalid merkle update cell: %w", err)
		}
	}
	return updateFrom, updateTo, nil
}

func merkleUpdateSeenKey(cell *Cell, merkleDepth int) merkleUpdateVisitKey {
	return merkleUpdateVisitKey{hash: cell.HashKey(), merkleDepth: merkleDepth}
}

func merkleChildDepth(cell *Cell, merkleDepth int) int {
	return int(childEffectiveLevelFor(cell, uint8(merkleDepth)))
}

func normalizeMerkleDepth(cell *Cell, merkleDepth int) int {
	return cell.getLevelMask().Apply(merkleDepth).GetLevel()
}

func walkMerkleUpdateSource(source, shape *Cell, merkleDepth int, visited map[merkleUpdateVisitKey]struct{}, validateShape bool, onKnown func(source *Cell, merkleDepth int)) error {
	if source == nil || shape == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateSeenKey(shape, merkleDepth)
	if _, ok := visited[key]; ok {
		return nil
	}
	visited[key] = struct{}{}

	if validateShape {
		if err := validateLoadedCell(shape); err != nil {
			return fmt.Errorf("invalid merkle update source subtree: %w", err)
		}
	}

	onKnown(source, merkleDepth)
	if shape.GetType() == PrunedCellType {
		return nil
	}
	if source.refsCount() != shape.refsCount() {
		return fmt.Errorf("invalid merkle update: source subtree refs mismatch")
	}

	childDepth := merkleChildDepth(shape, merkleDepth)
	for i := 0; i < source.refsCount(); i++ {
		if err := walkMerkleUpdateSource(source.ref(i), shape.ref(i), childDepth, visited, validateShape, onKnown); err != nil {
			return err
		}
	}
	return nil
}

func merkleUpdateBoundaryPrunedHash(cell *Cell, merkleDepth int) (Hash, bool) {
	if cell.GetType() != PrunedCellType || cell.Level() != merkleDepth+1 {
		return Hash{}, false
	}
	return cell.HashKey(merkleDepth), true
}

func (v *merkleUpdateValidator) dfsTo(cell *Cell, merkleDepth int) error {
	if cell == nil {
		return fmt.Errorf("merkle update contains nil reference")
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if _, ok := v.visitedTo[key]; ok {
		return nil
	}
	v.visitedTo[key] = struct{}{}

	if err := validateLoadedCell(cell); err != nil {
		return fmt.Errorf("invalid merkle update destination subtree: %w", err)
	}

	if hash, ok := merkleUpdateBoundaryPrunedHash(cell, merkleDepth); ok {
		if _, ok := v.known[hash]; !ok {
			return fmt.Errorf("unknown pruned cell in merkle update: %x", hash[:])
		}
		return nil
	}
	if cell.GetType() == PrunedCellType {
		return nil
	}

	childDepth := merkleChildDepth(cell, merkleDepth)
	for i := 0; i < cell.refsCount(); i++ {
		if err := v.dfsTo(cell.ref(i), childDepth); err != nil {
			return err
		}
	}
	return nil
}

func (a *merkleUpdateApplier) rebuild(cell *Cell, merkleDepth int) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	if hash, ok := merkleUpdateBoundaryPrunedHash(cell, merkleDepth); ok {
		if original, ok := a.known[hash]; ok {
			return original, nil
		}
		return nil, fmt.Errorf("unknown pruned branch %x", hash[:])
	}
	if cell.GetType() == PrunedCellType {
		return cell, nil
	}

	key := merkleUpdateSeenKey(cell, merkleDepth)
	if ready, ok := a.ready[key]; ok {
		return ready, nil
	}
	if cell.refsCount() == 0 {
		a.ready[key] = cell
		return cell, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:cell.refsCount()]
	childDepth := merkleChildDepth(cell, merkleDepth)
	for i := 0; i < len(refs); i++ {
		ref, err := a.rebuild(cell.ref(i), childDepth)
		if err != nil {
			return nil, err
		}
		refs[i] = ref
	}
	rebuilt, err := rebuildCellWithRefs(cell, refs)
	if err != nil {
		return nil, err
	}
	a.ready[key] = rebuilt
	return rebuilt, nil
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
	childDepth := merkleChildDepth(cell, merkleDepth)
	for i := 0; i < cell.refsCount(); i++ {
		if err := c.loadCells(cell.ref(i), childDepth); err != nil {
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
	info := c.cells[cell.HashKey(merkleDepth)]
	if info == nil {
		return fmt.Errorf("missing cached A subtree %x", cell.HashKey(merkleDepth))
	}
	if info.aNode != 0 {
		return nil
	}
	info.aNode = node
	if info.cell == nil {
		return nil
	}

	childDepth := merkleChildDepth(info.cell, merkleDepth)
	for i := 0; i < info.cell.refsCount(); i++ {
		if err := c.markA(info.cell.ref(i), childDepth, c.usage.child(node, i)); err != nil {
			return err
		}
	}
	return nil
}

func (c *merkleUpdateCombiner) rebuildRefs(cell *Cell, merkleDepth, cacheDepth int, rebuild func(*Cell, int, int) (*Cell, error)) (*Cell, error) {
	var refsBuf [4]*Cell
	refs := refsBuf[:cell.refsCount()]
	childDepth := merkleChildDepth(cell, merkleDepth)
	childCacheDepth := merkleChildDepth(cell, cacheDepth)
	for i := 0; i < len(refs); i++ {
		ref, err := rebuild(cell.ref(i), childDepth, childCacheDepth)
		if err != nil {
			return nil, err
		}
		refs[i] = ref
	}
	return rebuildCellWithRefs(cell, refs)
}

func (c *merkleUpdateCombiner) createD(cell *Cell, merkleDepth, dMerkleDepth int) (*Cell, error) {
	if cell == nil {
		return nil, fmt.Errorf("merkle update contains nil reference")
	}

	merkleDepth = normalizeMerkleDepth(cell, merkleDepth)
	key := merkleUpdateVisitKey{hash: cell.HashKey(merkleDepth), merkleDepth: dMerkleDepth}
	if ready, ok := c.createDReady[key]; ok {
		return ready, nil
	}

	rebuilt, err := c.doCreateD(cell, merkleDepth, dMerkleDepth)
	if err != nil {
		return nil, err
	}
	c.createDReady[key] = rebuilt
	return rebuilt, nil
}

func (c *merkleUpdateCombiner) doCreateD(cell *Cell, merkleDepth, dMerkleDepth int) (*Cell, error) {
	info := c.cells[cell.HashKey(merkleDepth)]
	if info == nil {
		return nil, fmt.Errorf("missing cached combine subtree %x", cell.HashKey(merkleDepth))
	}
	if info.aNode != 0 {
		c.usage.markPath(info.aNode)

		if pruned := info.getPruned(dMerkleDepth); pruned != nil {
			return pruned, nil
		}
		return createPrunedBranchForCombine(info.getAnyCell(), dMerkleDepth+1, merkleDepth)
	}
	if info.cell == nil {
		return nil, fmt.Errorf("missing concrete destination subtree %x", cell.HashKey(merkleDepth))
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
	key := merkleUpdateVisitKey{hash: cell.HashKey(merkleDepth), merkleDepth: aMerkleDepth}
	if ready, ok := c.createAReady[key]; ok {
		return ready, nil
	}

	rebuilt, err := c.doCreateA(cell, merkleDepth, aMerkleDepth)
	if err != nil {
		return nil, err
	}
	c.createAReady[key] = rebuilt
	return rebuilt, nil
}

func (c *merkleUpdateCombiner) doCreateA(cell *Cell, merkleDepth, aMerkleDepth int) (*Cell, error) {
	info := c.cells[cell.HashKey(merkleDepth)]
	if info == nil {
		return nil, fmt.Errorf("missing cached combine subtree %x", cell.HashKey(merkleDepth))
	}
	if info.aNode == 0 {
		return nil, fmt.Errorf("missing A usage node for subtree %x", cell.HashKey(merkleDepth))
	}
	if !c.usage.hasMark(info.aNode) {
		if pruned := info.getPruned(aMerkleDepth); pruned != nil {
			return pruned, nil
		}
		return createPrunedBranchForCombine(info.getAnyCell(), aMerkleDepth+1, merkleDepth)
	}
	if info.cell == nil {
		return nil, fmt.Errorf("missing concrete source subtree %x", cell.HashKey(merkleDepth))
	}
	if info.cell.refsCount() == 0 {
		return info.cell, nil
	}
	return c.rebuildRefs(info.cell, merkleDepth, aMerkleDepth, c.createA)
}

func createPrunedBranchForCombine(source *Cell, newLevel, virtLevel int) (*Cell, error) {
	if source == nil {
		return nil, fmt.Errorf("source cell is nil")
	}
	if !source.IsVirtualized() && source.Level() <= virtLevel && source.refsCount() == 0 {
		return source, nil
	}
	return createPrunedBranchFromCellAtDepth(source, newLevel, virtLevel)
}

func createMerkleUpdateCell(left, right *Cell) (*Cell, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("merkle update cell has nil references")
	}

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

func rebuildCellWithRefs(src *Cell, refs []*Cell) (*Cell, error) {
	if src == nil {
		return nil, fmt.Errorf("source cell is nil")
	}
	if len(refs) != src.refsCount() {
		return nil, fmt.Errorf("unexpected refs count: got %d want %d", len(refs), src.refsCount())
	}
	for i, ref := range refs {
		if ref != src.ref(i) {
			return copyCellWithRefs(src, refs)
		}
	}
	return src, nil
}

func copyCellWithRefs(src *Cell, refs []*Cell) (*Cell, error) {
	if src == nil {
		return nil, fmt.Errorf("source cell is nil")
	}
	if len(refs) != src.refsCount() {
		return nil, fmt.Errorf("unexpected refs count: got %d want %d", len(refs), src.refsCount())
	}

	builder := BeginCell()
	if err := storeBitSpan(builder, cellBits(src)); err != nil {
		return nil, fmt.Errorf("failed to copy cell bits: %w", err)
	}
	for i, ref := range refs {
		if err := builder.StoreRef(ref); err != nil {
			return nil, fmt.Errorf("failed to store ref %d: %w", i, err)
		}
	}
	rebuilt, err := finalizeCellFromBuilder(builder, src.isSpecial())
	if err != nil {
		return nil, fmt.Errorf("failed to finalize rebuilt cell: %w", err)
	}
	return rebuilt, nil
}
