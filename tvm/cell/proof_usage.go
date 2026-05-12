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
	if c.Level() != 0 {
		return nil, fmt.Errorf("failed to build usage proof: level is not 0")
	}
	if usageTree == nil {
		return nil, fmt.Errorf("failed to build usage proof: usage tree is nil")
	}

	visited := map[Hash]struct{}{}
	if err := collectUsageProofHashes(c, usageTree, usageTree.RootNode(), visited); err != nil {
		return nil, err
	}

	body, err := buildUsageProofBody(c, visited, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build proof for cell: %w", err)
	}
	return createMerkleProofCell(body)
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
			return createUsageProofPrunedBranch(c, merkleDepth+1)
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
		ref, err := refView.boundaryRef(i)
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

func collectUsageProofHashes(c *Cell, usageTree *CellUsageTree, node TraceNode, visited map[Hash]struct{}) error {
	if c == nil || !usageTree.IsLoaded(node) {
		return nil
	}
	visited[c.HashKey()] = struct{}{}

	refCnt := c.refsCount()
	if refCnt == 0 {
		return nil
	}

	refView := newCellRefView(c)
	for i := 0; i < refCnt; i++ {
		childNode := usageTree.GetChild(node, i)
		if !usageTree.IsLoaded(childNode) {
			continue
		}

		ref, err := refView.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek %d ref: %w", i, err)
		}
		ref, err = ref.load()
		if err != nil {
			return fmt.Errorf("failed to load %d ref: %w", i, err)
		}
		if err = collectUsageProofHashes(ref, usageTree, childNode, visited); err != nil {
			return err
		}
	}
	return nil
}

func buildUsageProofBody(c *Cell, visited map[Hash]struct{}, merkleDepth int) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	if _, ok := visited[c.HashKey()]; !ok {
		return createUsageProofPrunedBranch(c, merkleDepth+1)
	}

	refCnt := c.refsCount()
	if refCnt == 0 {
		return c, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	refView := newCellRefView(c)
	childDepth := merkleChildDepth(c, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}

		next := ref
		if _, ok := visited[ref.HashKey()]; ok {
			ref, err = ref.load()
			if err != nil {
				return nil, fmt.Errorf("failed to load %d ref: %w", i, err)
			}
			next, err = buildUsageProofBody(ref, visited, childDepth)
			if err != nil {
				return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
		} else {
			next, err = createUsageProofPrunedBranch(ref, childDepth+1)
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
	return rebuilt, nil
}

func createUsageProofPrunedBranch(source *Cell, newLevel int) (*Cell, error) {
	level := source.getLevelMask().Apply(_DataCellMaxLevel).GetLevel()
	if newLevel < level+1 {
		newLevel = level + 1
	}
	return createPrunedBranchFromCell(source, newLevel)
}
