package cell

import "fmt"

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

	state := newUsageProofBuildState()
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
		return loaded, nil
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

	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	if err != nil {
		return nil, err
	}
	return rebuilt, nil
}

type usageProofBuildKey struct {
	hash        Hash
	merkleDepth int
}

type usageProofBuildState struct {
	visited map[Hash]struct{}
	loaded  map[Hash]*Cell
	built   map[usageProofBuildKey]*Cell
}

func newUsageProofBuildState() *usageProofBuildState {
	return &usageProofBuildState{
		visited: map[Hash]struct{}{},
		loaded:  map[Hash]*Cell{},
		built:   map[usageProofBuildKey]*Cell{},
	}
}

func (s *usageProofBuildState) markVisited(c *Cell) {
	hash := c.HashKey()
	s.visited[hash] = struct{}{}
	if !c.IsLazy() {
		s.loaded[hash] = c
	}
}

func (s *usageProofBuildState) rememberLoaded(boundary, loaded *Cell) {
	if boundary == nil || loaded == nil {
		return
	}
	s.loaded[boundary.HashKey()] = loaded
	s.loaded[loaded.HashKey()] = loaded
}

func (s *usageProofBuildState) isVisited(c *Cell) bool {
	_, ok := s.visited[c.HashKey()]
	return ok
}

func (s *usageProofBuildState) loadedCell(c *Cell) (*Cell, bool) {
	loaded, ok := s.loaded[c.HashKey()]
	return loaded, ok
}

func (s *usageProofBuildState) loadForUsage(c *Cell, usageTree *CellUsageTree, node TraceNode) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	if loaded, ok := s.loadedCell(c); ok {
		return loaded, nil
	}
	if loaded, ok := usageTree.loadedCell(node); ok {
		loaded = loadedForBoundary(c, loaded)
		if loaded.HashKey() == c.HashKey() {
			s.rememberLoaded(c, loaded)
			return loaded, nil
		}
	}
	if loaded, ok := usageTree.loadedCellByHash(c.HashKey()); ok {
		s.rememberLoaded(c, loaded)
		return loaded, nil
	}
	if !c.IsLazy() {
		s.rememberLoaded(c, c)
		return c, nil
	}

	loaded, err := c.load()
	if err != nil {
		return nil, err
	}
	s.rememberLoaded(c, loaded)
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
	key := usageProofBuildKey{hash: c.HashKey(), merkleDepth: merkleDepth}
	if built := state.built[key]; built != nil {
		return built, nil
	}

	if !state.isVisited(c) {
		pruned, err := createPrunedBranchFromCell(c, merkleDepth+1)
		if err != nil {
			return nil, err
		}
		state.built[key] = pruned
		return pruned, nil
	}

	loaded := c
	if cached, ok := state.loadedCell(c); ok {
		loaded = cached
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		state.built[key] = loaded
		return loaded, nil
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

		next := ref
		if state.isVisited(ref) {
			loadedRef, ok := state.loadedCell(ref)
			if !ok {
				loadedRef, err = ref.load()
				if err != nil {
					return nil, fmt.Errorf("failed to load %d ref: %w", i, err)
				}
				state.rememberLoaded(ref, loadedRef)
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

	rebuilt, _, err := refView.cloneWithRefs(refs, nil)
	if err != nil {
		return nil, err
	}
	state.built[key] = rebuilt
	return rebuilt, nil
}

func merkleProofRef(parent *Cell, refView cellRefView, i int) (*Cell, error) {
	ref, err := refView.boundaryRef(i)
	if err != nil {
		return nil, err
	}
	return ref.Virtualize(childEffectiveLevelFor(parent, uint8(parent.currentEffectiveLevel()))), nil
}
