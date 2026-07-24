package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type MerkleProofBuilder struct {
	usageTree *CellUsageTree
	origRoot  *Cell
	usageRoot *Cell
}

func NewMerkleProofBuilder(root *Cell) *MerkleProofBuilder {
	b := &MerkleProofBuilder{}
	b.Init(root)
	return b
}

func (b *MerkleProofBuilder) Init(root *Cell) *Cell {
	b.usageTree = NewCellUsageTree()
	b.origRoot = root
	if root == nil {
		b.usageRoot = nil
		return nil
	}
	b.usageRoot = root.WithTrace(b.usageTree.RootTrace())
	return b.usageRoot
}

func (b *MerkleProofBuilder) Clear() {
	b.usageTree = nil
	b.origRoot = nil
	b.usageRoot = nil
}

func (b *MerkleProofBuilder) Root() *Cell {
	if b == nil {
		return nil
	}
	return b.usageRoot
}

func (b *MerkleProofBuilder) OriginalRoot() *Cell {
	if b == nil {
		return nil
	}
	return b.origRoot
}

func (b *MerkleProofBuilder) UsageTree() *CellUsageTree {
	if b == nil {
		return nil
	}
	return b.usageTree
}

func (b *MerkleProofBuilder) CreateProof() (*Cell, error) {
	if b == nil || b.origRoot == nil || b.usageTree == nil {
		return nil, fmt.Errorf("failed to build usage proof: proof builder is not initialized")
	}
	return b.origRoot.CreateUsageProof(b.usageTree)
}

func (c *Cell) CreateUsageProof(usageTree *CellUsageTree) (*Cell, error) {
	if usageTree == nil {
		return nil, fmt.Errorf("failed to build usage proof: usage tree is nil")
	}

	state := newUsageProofBuildState(usageTree.NodeCount())
	if err := collectUsageProofHashes(c, usageTree, usageTree.RootNode(), state); err != nil {
		return nil, err
	}

	body, err := buildUsageProofBody(c, state, c.Level())
	if err != nil {
		return nil, fmt.Errorf("failed to build proof for cell: %w", err)
	}
	return CreateMerkleProof(body)
}

type merkleProofPruneFunc func(*Cell) (bool, error)

func buildMerkleProofBodyByPruneFunc(c *Cell, shouldPrune merkleProofPruneFunc, merkleDepth int) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	if shouldPrune != nil {
		pruned, err := shouldPrune(c)
		if err != nil {
			return nil, err
		}
		if pruned {
			return createPrunedBranchFromCell(c, merkleDepth+1)
		}
	}

	loaded, err := c.load()
	if err != nil {
		return nil, err
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		return loaded.WithoutTrace(), nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	refView := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref, err := merkleProofRef(c, refView, i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}
		next, err := buildMerkleProofBodyByPruneFunc(ref, shouldPrune, childDepth)
		if err != nil {
			return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
		}
		refs[i] = next
	}

	return cloneProofCellWithRefs(loaded, refView, refs)
}

type usageProofBuildKey struct {
	hash        Hash
	merkleDepth int
}

// usageProofBuildState tracks the cells a usage proof must include. Presence
// of a hash in cells marks it visited; a non-nil value also caches its loaded
// form for the build path.
type usageProofBuildState struct {
	cells map[Hash]*Cell
	built map[usageProofBuildKey]*Cell
}

func newUsageProofBuildState(sizeHint int) *usageProofBuildState {
	return &usageProofBuildState{
		cells: make(map[Hash]*Cell, sizeHint),
		built: make(map[usageProofBuildKey]*Cell, sizeHint),
	}
}

func (s *usageProofBuildState) markVisited(c *Cell) {
	hash := c.HashKey()
	if !c.IsLazy() {
		s.cells[hash] = c
		return
	}
	if _, ok := s.cells[hash]; !ok {
		s.cells[hash] = nil
	}
}

func (s *usageProofBuildState) loadForUsage(c *Cell, usageTree *CellUsageTree, node TraceNode) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	hash := c.HashKey()
	if loaded := s.cells[hash]; loaded != nil {
		return loaded, nil
	}
	if loaded, ok := usageTree.loadedCell(node); ok {
		loaded = loadedForBoundary(c, loaded)
		if loaded.HashKey() == hash {
			s.cells[hash] = loaded
			return loaded, nil
		}
	}
	if loaded, ok := usageTree.loadedCellByHash(hash); ok {
		s.cells[hash] = loaded
		return loaded, nil
	}
	if !c.IsLazy() {
		s.cells[hash] = c
		return c, nil
	}

	loaded, err := c.load()
	if err != nil {
		return nil, err
	}
	s.cells[hash] = loaded
	return loaded, nil
}

func loadedForBoundary(boundary, loaded *Cell) *Cell {
	if boundary == nil || loaded == nil || boundary.HashKey() == loaded.HashKey() {
		return loaded
	}

	raw := loaded.rawCell()
	if trace := loaded.Trace(); trace != nil && raw.Trace() != trace {
		raw = raw.WithTrace(trace)
	}
	return raw.Virtualize(uint8(boundary.currentEffectiveLevel()))
}

func collectUsageProofHashes(c *Cell, usageTree *CellUsageTree, node TraceNode, state *usageProofBuildState) error {
	if c == nil || !usageTree.IsLoaded(node) {
		return nil
	}
	state.markVisited(c)

	loaded, err := state.loadForUsage(c, usageTree, node)
	if err != nil {
		return err
	}
	if loaded != c {
		state.markVisited(loaded)
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		return nil
	}

	refView := newCellRefView(loaded)
	for i := 0; i < refCnt; i++ {
		childNode := usageTree.GetChild(node, i)
		if !usageTree.IsLoaded(childNode) {
			continue
		}

		ref, err := merkleProofRef(loaded, refView, i)
		if err != nil {
			return fmt.Errorf("failed to peek %d ref: %w", i, err)
		}
		ref, err = state.loadForUsage(ref, usageTree, childNode)
		if err != nil {
			return fmt.Errorf("failed to load %d ref: %w", i, err)
		}
		if err = collectUsageProofHashes(ref, usageTree, childNode, state); err != nil {
			return err
		}
	}
	return nil
}

func buildUsageProofBody(c *Cell, state *usageProofBuildState, merkleDepth int) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	hash := c.HashKey()
	key := usageProofBuildKey{hash: hash, merkleDepth: merkleDepth}
	if built := state.built[key]; built != nil {
		return built, nil
	}

	cached, visited := state.cells[hash]
	if !visited {
		pruned, err := createPrunedBranchFromCell(c, merkleDepth+1)
		if err != nil {
			return nil, err
		}
		state.built[key] = pruned
		return pruned, nil
	}

	loaded := c
	if cached != nil {
		loaded = cached
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		built := loaded.WithoutTrace()
		state.built[key] = built
		return built, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	refView := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref, err := merkleProofRef(loaded, refView, i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}

		var next *Cell
		refHash := ref.HashKey()
		if loadedRef, refVisited := state.cells[refHash]; refVisited {
			if loadedRef == nil {
				loadedRef, err = ref.load()
				if err != nil {
					return nil, fmt.Errorf("failed to load %d ref: %w", i, err)
				}
				state.cells[refHash] = loadedRef
			}
			next, err = buildUsageProofBody(loadedRef, state, childDepth)
			if err != nil {
				return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
		} else {
			next, err = buildUsageProofBody(ref, state, childDepth)
			if err != nil {
				return nil, fmt.Errorf("failed to prune %d ref: %w", i, err)
			}
		}
		refs[i] = next
	}

	rebuilt, err := cloneProofCellWithRefs(loaded, refView, refs)
	if err != nil {
		return nil, err
	}
	state.built[key] = rebuilt
	return rebuilt, nil
}

// cloneProofCellWithRefs rebuilds a proof-body cell in one copy with the same
// bits, replaced references and no traversal trace. It returns the source when
// neither its references nor its view changed.
func cloneProofCellWithRefs(src *Cell, view cellRefView, refs []*Cell) (*Cell, error) {
	if src.Trace() == nil && !view.virtual {
		unchanged := true
		for i, ref := range refs {
			if ref != src.refs[i] {
				unchanged = false
				break
			}
		}
		if unchanged {
			return src, nil
		}
	}

	if !src.IsSpecial() {
		// an ordinary rebuilt cell needs nothing from the source meta: the
		// trace and lazy loader must not carry over, virtualization is
		// resolved by the explicit refs, and hashes are reseeded below
		cloned := new(Cell)
		*cloned = *src
		cloned.meta = nil
		for i, ref := range refs {
			cloned.setRef(i, ref)
		}
		if err := cloned.refreshLevelMaskForRefs(); err != nil {
			return nil, err
		}
		if err := calculateSeededProofHashes(cloned, src); err != nil {
			return nil, err
		}
		return cloned, nil
	}

	cloned := src.copy()
	cloned.clearVirtualization()
	if cloned.meta != nil {
		cloned.meta.trace = nil
		cloned.clearMetaIfEmpty()
	}
	for i, ref := range refs {
		cloned.setRef(i, ref)
	}
	if err := cloned.refreshLevelMaskForRefs(); err != nil {
		return nil, err
	}
	if err := cloned.calculateHashes(); err != nil {
		return nil, err
	}
	return cloned, nil
}

// calculateSeededProofHashes finalizes a rebuilt ordinary proof-body cell
// reusing the Merkle invariant that its level-0 hash and depth equal the
// source cell's: the body bits are identical and every child is the source
// subtree either included (same level-0 hash by induction) or replaced by a
// pruned branch that preserves it. Only the higher significant levels
// introduced by pruned children are hashed, mirroring the corresponding
// levels of calculateHashes.
func calculateSeededProofHashes(cloned, src *Cell) error {
	levelMask := cloned.getLevelMask()
	if levelMask.getHashIndex() == 0 {
		cloned.clearExtraHashes()
		cloned.setHashAt(0, src.getHash(0))
		cloned.setDepthAt(0, src.getDepth(0))
		return nil
	}

	meta := cloned.ensureMeta()
	if meta.extraHashes == nil {
		meta.extraHashes = new([3]Hash)
	}
	meta.extraDepths = [3]uint16{}

	cloned.setHashAt(0, src.getHash(0))
	cloned.setDepthAt(0, src.getDepth(0))

	refCnt := cloned.refsCount()
	level := levelMask.GetLevel()
	var hashBuf [2 + maxCellDataBytes + (4 * depthSize) + (4 * hashSize)]byte
	hashIndex := 1
	for levelIndex := 1; levelIndex <= level; levelIndex++ {
		if !levelMask.IsSignificant(levelIndex) {
			continue
		}

		dsc1, dsc2 := cloned.descriptors(levelMask.Apply(levelIndex))
		hashBuf[0], hashBuf[1] = dsc1, dsc2
		bufPos := 2
		bufPos += copy(hashBuf[bufPos:], cloned.hashAt(hashIndex-1))

		var depth uint16
		for i := 0; i < refCnt; i++ {
			childDepth := cloned.refs[i].getDepth(levelIndex)
			binary.BigEndian.PutUint16(hashBuf[bufPos:bufPos+depthSize], childDepth)
			bufPos += depthSize

			if childDepth > depth {
				depth = childDepth
			}
		}
		if refCnt > 0 {
			depth++
			if depth > maxDepth {
				return ErrCellDepthLimit
			}
		}

		for i := 0; i < refCnt; i++ {
			bufPos += copy(hashBuf[bufPos:], cloned.refs[i].getHash(levelIndex))
		}

		cloned.setDepthAt(hashIndex, depth)
		sum := sha256.Sum256(hashBuf[:bufPos])
		cloned.setHashAt(hashIndex, sum[:])
		hashIndex++
	}
	return nil
}

func merkleProofRef(parent *Cell, refView cellRefView, i int) (*Cell, error) {
	ref, err := refView.boundaryRef(i)
	if err != nil {
		return nil, err
	}
	return ref.Virtualize(childEffectiveLevelFor(parent, uint8(parent.currentEffectiveLevel()))), nil
}
