package cell

import "fmt"

func finalizeCellFromBuilder(builder *Builder, special bool) (*Cell, error) {
	refs := builder.rawRefs()
	var data []byte
	if usedBytes := builder.usedBytes(); usedBytes > 0 {
		data = make([]byte, usedBytes)
		copy(data, builder.data[:usedBytes])
	}

	c := &Cell{
		bitsSz: uint16(builder.bitsSz),
		data:   data,
	}
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
