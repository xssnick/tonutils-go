package cell

import "fmt"

// cellWithBuf* combine a finalized Cell and its data into one size-classed
// allocation, halving the allocation count of EndCell. Cells are immutable
// after finalization and nothing grows c.data (the slice is capped to its
// length, so any append reallocates), which makes the data aliasing a larger
// allocation safe — BoC parsing already aliases cell data into shared payload
// buffers the same way.
type cellWithBuf24 struct {
	c   Cell
	buf [24]byte
}

type cellWithBuf56 struct {
	c   Cell
	buf [56]byte
}

type cellWithBuf128 struct {
	c   Cell
	buf [maxCellDataBytes]byte
}

func finalizeCellFromBuilder(builder *Builder, special bool) (*Cell, error) {
	refs := builder.rawRefs()

	var c *Cell
	switch usedBytes := builder.usedBytes(); {
	case usedBytes == 0:
		c = &Cell{}
	case usedBytes <= 24:
		x := new(cellWithBuf24)
		copy(x.buf[:], builder.data[:usedBytes])
		x.c.data = x.buf[:usedBytes:usedBytes]
		c = &x.c
	case usedBytes <= 56:
		x := new(cellWithBuf56)
		copy(x.buf[:], builder.data[:usedBytes])
		x.c.data = x.buf[:usedBytes:usedBytes]
		c = &x.c
	default:
		x := new(cellWithBuf128)
		copy(x.buf[:], builder.data[:usedBytes])
		x.c.data = x.buf[:usedBytes:usedBytes]
		c = &x.c
	}

	c.bitsSz = uint16(builder.bitsSz)
	c.setSpecial(special)
	copy(c.refs[:], refs)
	c.setRefsCount(len(refs))
	if err := validateCellRefDepthLimit(refs); err != nil {
		return nil, err
	}

	if special {
		if err := refreshSpecialCellLevelMask(c); err != nil {
			return nil, err
		}
	} else {
		c.setLevelMask(ordinaryLevelMask(refs))
	}

	if err := validateBoundaryCell(c); err != nil {
		return nil, err
	}
	if err := c.calculateHashes(); err != nil {
		return nil, err
	}
	return c, nil
}

func refreshSpecialCellLevelMask(c *Cell) error {
	if c.bitsSz < 8 {
		return fmt.Errorf("not enough data for a special cell")
	}

	switch Type(c.data[0]) {
	case PrunedCellType:
		if _, err := specialCellRefs(c, PrunedCellType, false); err != nil {
			return err
		}
		if c.bitsSz < 16 {
			return fmt.Errorf("not enough data for a pruned branch special cell")
		}
		c.setLevelMask(LevelMask{c.data[1]})
	case LibraryCellType:
		if _, err := specialCellRefs(c, LibraryCellType, false); err != nil {
			return err
		}
		c.setLevelMask(LevelMask{})
	case MerkleProofCellType:
		refs, err := specialCellRefs(c, MerkleProofCellType, false)
		if err != nil {
			return err
		}
		c.setLevelMask(LevelMask{refs[0].getLevelMask().Mask >> 1})
	case MerkleUpdateCellType:
		refs, err := specialCellRefs(c, MerkleUpdateCellType, false)
		if err != nil {
			return err
		}
		left, right := refs[0], refs[1]
		c.setLevelMask(LevelMask{(left.getLevelMask().Mask | right.getLevelMask().Mask) >> 1})
	default:
		return fmt.Errorf("unknown special cell type")
	}
	return nil
}
