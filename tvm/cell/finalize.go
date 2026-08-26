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

// cellWithMeta fuses a cell with the metadata it is known to need, on the same
// reasoning as the buffers above. Attaching a trace to an untraced cell is the
// single most repeated cell operation a traced collation performs, and it
// otherwise costs two objects where one will do — the collector charges by
// object count, not by bytes.
type cellWithMeta struct {
	c Cell
	m cellMeta
}

// newCellWithData allocates a Cell and its immutable payload in one object.
// The returned data slice has no spare capacity, so callers cannot append into
// the fused allocation after the cell has been finalized.
func newCellWithData(usedBytes int) *Cell {
	switch {
	case usedBytes == 0:
		return &Cell{}
	case usedBytes <= 24:
		x := new(cellWithBuf24)
		x.c.data = x.buf[:usedBytes:usedBytes]
		return &x.c
	case usedBytes <= 56:
		x := new(cellWithBuf56)
		x.c.data = x.buf[:usedBytes:usedBytes]
		return &x.c
	default:
		x := new(cellWithBuf128)
		x.c.data = x.buf[:usedBytes:usedBytes]
		return &x.c
	}
}

func finalizeCellFromBuilder(builder *Builder, special bool) (*Cell, error) {
	c, err := buildCellShellFromBuilder(builder, special)
	if err != nil {
		return nil, err
	}
	if err := c.calculateHashes(); err != nil {
		return nil, err
	}
	return c, nil
}

// buildCellShellFromBuilder performs every finalization step except hash
// computation: the returned cell has data, refs, level mask and boundary
// validation done, but no hashes yet. Callers must compute hashes before the
// cell is shared.
func buildCellShellFromBuilder(builder *Builder, special bool) (*Cell, error) {
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

	if err := fillCellShell(c, refs, builder, special); err != nil {
		return nil, err
	}
	return c, nil
}

// fillCellShell completes a cell whose data is already in place. It is shared
// by the allocating path above and by the arena path below, so the two cannot
// drift apart in what they validate.
func fillCellShell(c *Cell, refs []*Cell, builder *Builder, special bool) error {
	c.bitsSz = uint16(builder.bitsSz)
	c.setSpecial(special)
	copy(c.refs[:], refs)
	c.setRefsCount(len(refs))
	if err := validateCellRefDepthLimit(refs); err != nil {
		return err
	}

	if special {
		if err := refreshSpecialCellLevelMask(c); err != nil {
			return err
		}
	} else {
		c.setLevelMask(ordinaryLevelMask(refs))
	}

	return validateBoundaryCell(c)
}

// finalizeCellInto finalizes a builder into a cell and a data window the
// caller owns, instead of the size-classed allocation buildCellShellFromBuilder
// makes. It is what lets a bulk dictionary build carve all of its nodes out of
// two slabs; the aliasing is safe for the same reason the cellWithBuf* types
// are, since a finalized cell is immutable and its data slice is capped to its
// length.
func finalizeCellInto(dst *Cell, buf []byte, builder *Builder, special bool) error {
	usedBytes := builder.usedBytes()
	if usedBytes > len(buf) {
		return fmt.Errorf("cell data of %d bytes does not fit the %d byte arena window", usedBytes, len(buf))
	}

	*dst = Cell{}
	if usedBytes > 0 {
		copy(buf, builder.data[:usedBytes])
		dst.data = buf[:usedBytes:usedBytes]
	}
	if err := fillCellShell(dst, builder.rawRefs(), builder, special); err != nil {
		return err
	}
	return dst.calculateHashes()
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
