package cell

import (
	"bytes"
	"fmt"
)

// CombineMerkleProof merges two wrapped Merkle proofs of the same root.
// A nil proof is treated as an empty input and the other proof is returned.
func CombineMerkleProof(left, right *Cell) (*Cell, error) {
	if left == nil {
		return right, nil
	}
	if right == nil {
		return left, nil
	}

	leftBody, err := unwrapMerkleProofForCombine(left)
	if err != nil {
		return nil, err
	}
	rightBody, err := unwrapMerkleProofForCombine(right)
	if err != nil {
		return nil, err
	}
	combined, err := CombineMerkleProofRaw(leftBody, rightBody)
	if err != nil {
		return nil, err
	}
	return CreateMerkleProof(combined)
}

// CombineMerkleProofFast merges two wrapped proofs with matching structure.
// CombineMerkleProof handles shared and differently-pruned DAG shapes too and
// should be preferred unless the caller already knows that the fast invariant
// holds.
func CombineMerkleProofFast(left, right *Cell) (*Cell, error) {
	if left == nil {
		return right, nil
	}
	if right == nil {
		return left, nil
	}

	leftBody, err := unwrapMerkleProofForCombine(left)
	if err != nil {
		return nil, err
	}
	rightBody, err := unwrapMerkleProofForCombine(right)
	if err != nil {
		return nil, err
	}
	combined, err := CombineMerkleProofFastRaw(leftBody, rightBody)
	if err != nil {
		return nil, err
	}
	return CreateMerkleProof(combined)
}

// CombineMerkleProofRaw merges two unwrapped Merkle proof bodies.
func CombineMerkleProofRaw(left, right *Cell) (*Cell, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("cannot combine nil Merkle proof body")
	}
	if left.HashKeyAt(0) != right.HashKeyAt(0) {
		return nil, fmt.Errorf("cannot combine Merkle proofs with different roots")
	}

	combiner := merkleProofCombiner{
		cells:   map[Hash]merkleProofCombineInfo{},
		visited: map[proofBodyKey]struct{}{},
		ready:   map[proofBodyKey]*Cell{},
	}
	if err := combiner.load(left, 0); err != nil {
		return nil, err
	}
	if err := combiner.load(right, 0); err != nil {
		return nil, err
	}
	return combiner.create(left, 0, 0)
}

// CombineMerkleProofFastRaw merges two unwrapped proofs with matching
// structure.
func CombineMerkleProofFastRaw(left, right *Cell) (*Cell, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("cannot combine nil Merkle proof body")
	}
	if left.HashKeyAt(0) != right.HashKeyAt(0) {
		return nil, fmt.Errorf("cannot combine Merkle proofs with different roots")
	}

	combiner := merkleProofFastCombiner{}
	return combiner.merge(left, right, 0)
}

func unwrapMerkleProofForCombine(proof *Cell) (*Cell, error) {
	if proof.Level() != 0 {
		return nil, fmt.Errorf("failed to unpack Merkle proof: level is not zero")
	}
	loaded, err := proof.load()
	if err != nil {
		return nil, fmt.Errorf("failed to unpack Merkle proof: %w", err)
	}
	loaded = loadedForBoundary(proof, loaded)
	if !loaded.IsSpecial() || loaded.RefsNum() != 1 || loaded.BitsSize() != 280 ||
		Type(loaded.data[0]) != MerkleProofCellType {
		return nil, fmt.Errorf("failed to unpack Merkle proof: not a MerkleProof cell")
	}
	return UnwrapProof(loaded, loaded.data[1:33])
}

type merkleProofCombineInfo struct {
	cell   *Cell
	pruned [_DataCellMaxLevel + 1]*Cell
}

func (i *merkleProofCombineInfo) putPruned(c *Cell) {
	idx := c.Level() - 1
	if idx >= 0 && idx < len(i.pruned) {
		i.pruned[idx] = c
	}
}

func (i *merkleProofCombineInfo) getPruned(depth int) *Cell {
	if depth < 0 || depth >= len(i.pruned) {
		return nil
	}
	return i.pruned[depth]
}

func (i *merkleProofCombineInfo) anyCell() *Cell {
	if i.cell != nil {
		return i.cell
	}
	for _, c := range i.pruned {
		if c != nil {
			return c
		}
	}
	return nil
}

type merkleProofCombiner struct {
	cells   map[Hash]merkleProofCombineInfo
	visited map[proofBodyKey]struct{}
	ready   map[proofBodyKey]*Cell
}

func (c *merkleProofCombiner) load(boundary *Cell, merkleDepth int) error {
	if boundary == nil {
		return fmt.Errorf("Merkle proof contains nil reference")
	}

	visitKey := proofBodyKey{hash: boundary.HashKey(), merkleDepth: merkleDepth}
	if _, ok := c.visited[visitKey]; ok {
		return nil
	}
	c.visited[visitKey] = struct{}{}

	loaded, err := boundary.load()
	if err != nil {
		return fmt.Errorf("failed to load Merkle proof cell: %w", err)
	}
	loaded = loadedForBoundary(boundary, loaded)
	hash := loaded.HashKeyAt(merkleDepth)
	info := c.cells[hash]
	if loaded.GetType() == PrunedCellType && loaded.Level() > merkleDepth {
		info.putPruned(loaded)
		c.cells[hash] = info
		return nil
	}

	info.cell = loaded
	c.cells[hash] = info
	view := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	for i := 0; i < loaded.refsCount(); i++ {
		ref, err := view.boundaryRef(i)
		if err != nil {
			return fmt.Errorf("failed to peek Merkle proof ref %d: %w", i, err)
		}
		if err = c.load(ref, childDepth); err != nil {
			return err
		}
	}
	return nil
}

func (c *merkleProofCombiner) create(boundary *Cell, merkleDepth, proofDepth int) (*Cell, error) {
	merkleDepth = normalizeMerkleDepth(boundary, merkleDepth)
	hash := boundary.HashKeyAt(merkleDepth)
	key := proofBodyKey{hash: hash, merkleDepth: proofDepth}
	if ready := c.ready[key]; ready != nil {
		return ready, nil
	}

	info, ok := c.cells[hash]
	if !ok {
		return nil, fmt.Errorf("missing cached Merkle proof subtree %x", hash)
	}
	if info.cell == nil {
		if pruned := info.getPruned(proofDepth); pruned != nil {
			c.ready[key] = pruned
			return pruned, nil
		}
		pruned, err := createPrunedBranchForCombine(info.anyCell(), proofDepth+1, merkleDepth)
		if err != nil {
			return nil, err
		}
		c.ready[key] = pruned
		return pruned, nil
	}
	if info.cell.refsCount() == 0 {
		c.ready[key] = info.cell
		return info.cell, nil
	}

	view := newCellRefView(info.cell)
	childDepth := merkleChildDepth(info.cell, merkleDepth)
	childProofDepth := merkleChildDepth(info.cell, proofDepth)
	var refsBuf [4]*Cell
	refs := refsBuf[:info.cell.refsCount()]
	for i := range refs {
		ref, err := view.boundaryRef(i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek Merkle proof ref %d: %w", i, err)
		}
		ref, err = c.create(ref, childDepth, childProofDepth)
		if err != nil {
			return nil, err
		}
		refs[i] = ref
	}
	rebuilt, err := cloneProofCellWithRefs(info.cell, view, refs, nil)
	if err != nil {
		return nil, err
	}
	c.ready[key] = rebuilt
	return rebuilt, nil
}

type merkleProofFastKey struct {
	left, right Hash
	merkleDepth int
}

type merkleProofFastCombiner struct {
	ready map[merkleProofFastKey]*Cell
}

func (c *merkleProofFastCombiner) merge(leftBoundary, rightBoundary *Cell, merkleDepth int) (*Cell, error) {
	// Match the reference's ExtCell shortcuts: hashes and levels are available
	// on a lazy boundary, so equal or already-bounded inputs need no load.
	if leftBoundary.HashKey() == rightBoundary.HashKey() {
		return leftBoundary, nil
	}
	if leftBoundary.Level() == merkleDepth {
		return leftBoundary, nil
	}
	if rightBoundary.Level() == merkleDepth {
		return rightBoundary, nil
	}

	left, err := leftBoundary.load()
	if err != nil {
		return nil, err
	}
	left = loadedForBoundary(leftBoundary, left)
	right, err := rightBoundary.load()
	if err != nil {
		return nil, err
	}
	right = loadedForBoundary(rightBoundary, right)

	if left.GetType() == PrunedCellType {
		return right, nil
	}
	if right.GetType() == PrunedCellType {
		return left, nil
	}

	key := merkleProofFastKey{left: left.HashKey(), right: right.HashKey(), merkleDepth: merkleDepth}
	if ready := c.ready[key]; ready != nil {
		return ready, nil
	}
	if left.BitsSize() != right.BitsSize() || !bytes.Equal(left.data, right.data) || left.refsCount() != right.refsCount() {
		return nil, fmt.Errorf("cannot fast-combine differently shaped Merkle proofs")
	}
	if left.refsCount() == 0 {
		return nil, fmt.Errorf("cannot fast-combine inconsistent Merkle proof leaves")
	}

	leftView := newCellRefView(left)
	rightView := newCellRefView(right)
	childDepth := merkleChildDepth(left, merkleDepth)
	var refsBuf [4]*Cell
	refs := refsBuf[:left.refsCount()]
	for i := range refs {
		leftRef, err := leftView.boundaryRef(i)
		if err != nil {
			return nil, err
		}
		rightRef, err := rightView.boundaryRef(i)
		if err != nil {
			return nil, err
		}
		refs[i], err = c.merge(leftRef, rightRef, childDepth)
		if err != nil {
			return nil, err
		}
	}
	rebuilt, err := cloneProofCellWithRefs(left, leftView, refs, nil)
	if err != nil {
		return nil, err
	}
	if c.ready == nil {
		c.ready = make(map[merkleProofFastKey]*Cell)
	}
	c.ready[key] = rebuilt
	return rebuilt, nil
}
