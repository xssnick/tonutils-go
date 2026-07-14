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
	hashes, depths := collectMetadataHashesDepths(c, levelMask)

	refsCount := c.refsCount()
	refs := make([]RefMetadata, refsCount)
	refView := newCellRefView(c)
	for i := 0; i < refsCount; i++ {
		refs[i] = metadataForRef(refView.logicalBoundaryRef(i))
	}

	return Metadata{
		Hash:      c.HashKey(),
		LevelMask: levelMask,
		Hashes:    hashes,
		Depths:    depths,
		Refs:      refs,
	}
}

func metadataForRef(ref *Cell) RefMetadata {
	if ref == nil {
		return RefMetadata{Lazy: true}
	}

	levelMask := ref.getLevelMask()
	hashes, depths := collectMetadataHashesDepths(ref, levelMask)

	return RefMetadata{
		Hash:      ref.HashKey(),
		LevelMask: levelMask,
		Hashes:    hashes,
		Depths:    depths,
		Lazy:      ref.IsLazy(),
	}
}

func collectMetadataHashesDepths(c *Cell, levelMask LevelMask) ([]Hash, []uint16) {
	hashes := make([]Hash, levelMask.getHashesCount())
	depths := make([]uint16, len(hashes))
	idx := 0
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if !levelMask.IsSignificant(level) {
			continue
		}
		hashes[idx] = c.HashKey(level)
		depths[idx] = c.Depth(level)
		idx++
	}
	return hashes, depths
}
