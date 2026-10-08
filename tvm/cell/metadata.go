package cell

// Metadata is the storage-visible representation of a cell.
//
// It exposes hashes and depths exactly as they are visible from the cell's
// current effective level. Lazy pruned references are represented as metadata
// boundaries: their hash/depth data is available, while Lazy is true.
type Metadata struct {
	Hash      Hash
	LevelMask LevelMask
	Hashes    []Hash
	Depths    []uint16
	Refs      []RefMetadata
}

type RefMetadata struct {
	Hash      Hash
	LevelMask LevelMask
	Hashes    []Hash
	Depths    []uint16
	Lazy      bool
}

func (c *Cell) GetMetadata() Metadata {
	levelMask := c.getLevelMask()
	rootHashes := levelMask.getHashesCount()
	totalHashes := rootHashes
	refsCount := c.refsCount()
	refs := make([]RefMetadata, refsCount)
	var refCells [4]*Cell
	refView := newCellRefView(c)
	for i := 0; i < refsCount; i++ {
		ref := refView.logicalBoundaryRef(i)
		refCells[i] = ref
		if ref == nil {
			refs[i].Lazy = true
			continue
		}
		refs[i] = RefMetadata{
			Hash:      ref.HashKey(),
			LevelMask: ref.getLevelMask(),
			Lazy:      ref.IsLazy(),
		}
		totalHashes += refs[i].LevelMask.getHashesCount()
	}

	// The result owns two flat buffers instead of a pair of allocations per
	// reference. Cap each window so appending cannot overwrite another entry.
	hashes := make([]Hash, totalHashes)
	depths := make([]uint16, totalHashes)
	fillMetadataHashesDepths(c, levelMask, hashes[:rootHashes], depths[:rootHashes])
	pos := rootHashes
	for i := range refs {
		if refCells[i] == nil {
			continue
		}
		end := pos + refs[i].LevelMask.getHashesCount()
		refs[i].Hashes = hashes[pos:end:end]
		refs[i].Depths = depths[pos:end:end]
		fillMetadataHashesDepths(refCells[i], refs[i].LevelMask, refs[i].Hashes, refs[i].Depths)
		pos = end
	}

	return Metadata{
		Hash:      c.HashKey(),
		LevelMask: levelMask,
		Hashes:    hashes[:rootHashes:rootHashes],
		Depths:    depths[:rootHashes:rootHashes],
		Refs:      refs,
	}
}

func fillMetadataHashesDepths(c *Cell, levelMask LevelMask, hashes []Hash, depths []uint16) {
	idx := 0
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if !levelMask.IsSignificant(level) {
			continue
		}
		hashes[idx] = c.HashKeyAt(level)
		depths[idx] = c.getDepth(level)
		idx++
	}
}
