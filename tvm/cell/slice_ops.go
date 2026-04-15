package cell

func (c *Slice) PeekRefCellAt(i int) (*Cell, error) {
	if i < 0 || i >= c.RefsNum() {
		return nil, ErrNoMoreRefs
	}
	return c.refCellAt(i), nil
}

func (c *Slice) bitAt(offset uint) byte {
	abs := uint(c.bitStart) + offset
	return (c.cell.data[abs/8] >> (7 - (abs % 8))) & 1
}

func (c *Slice) OnlyFirst(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || c.RefsNum() < refs {
		return false
	}
	c.bitEnd = c.bitStart + uint16(bits)
	c.refEnd = c.refStart + uint8(refs)
	return true
}

func (c *Slice) SkipFirst(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || c.RefsNum() < refs {
		return false
	}
	return c.AdvanceExt(bits, refs) == nil
}

func (c *Slice) OnlyLast(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || c.RefsNum() < refs {
		return false
	}
	c.bitStart = c.bitEnd - uint16(bits)
	c.refStart = c.refEnd - uint8(refs)
	return true
}

func (c *Slice) SkipLast(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || c.RefsNum() < refs {
		return false
	}
	c.bitEnd -= uint16(bits)
	c.refEnd -= uint8(refs)
	return true
}

func (c *Slice) Subslice(offsetBits uint, offsetRefs int, bits uint, refs int) (*Slice, error) {
	cp := c.Copy()
	if !cp.SkipFirst(offsetBits, offsetRefs) {
		return nil, ErrNotEnoughData(int(c.BitsLeft()), int(offsetBits+bits))
	}
	if !cp.OnlyFirst(bits, refs) {
		return nil, ErrNotEnoughData(int(c.BitsLeft()), int(offsetBits+bits))
	}
	return cp, nil
}

func (c *Slice) HasPrefix(prefix *Slice) bool {
	if prefix == nil {
		return true
	}
	return prefix.IsPrefixOf(c)
}

func (c *Slice) bitsEqualAt(other *Slice, offset, otherOffset, bits uint) bool {
	for i := uint(0); i < bits; i++ {
		if c.bitAt(offset+i) != other.bitAt(otherOffset+i) {
			return false
		}
	}
	return true
}

func (c *Slice) LexCompare(other *Slice) int {
	if other == nil {
		if c.BitsLeft() == 0 {
			return 0
		}
		return 1
	}

	left := c.BitsLeft()
	right := other.BitsLeft()
	limit := left
	if right < limit {
		limit = right
	}

	for i := uint(0); i < limit; i++ {
		a := c.bitAt(i)
		b := other.bitAt(i)
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
	}

	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (c *Slice) BitsEqual(other *Slice) bool {
	if other == nil {
		return false
	}
	return c.BitsLeft() == other.BitsLeft() && c.bitsEqualAt(other, 0, 0, c.BitsLeft())
}

func (c *Slice) IsPrefixOf(other *Slice) bool {
	if other == nil || c.BitsLeft() > other.BitsLeft() {
		return false
	}
	return c.bitsEqualAt(other, 0, 0, c.BitsLeft())
}

func (c *Slice) IsProperPrefixOf(other *Slice) bool {
	return other != nil && c.BitsLeft() < other.BitsLeft() && c.bitsEqualAt(other, 0, 0, c.BitsLeft())
}

func (c *Slice) IsSuffixOf(other *Slice) bool {
	if other == nil || c.BitsLeft() > other.BitsLeft() {
		return false
	}
	return c.bitsEqualAt(other, 0, other.BitsLeft()-c.BitsLeft(), c.BitsLeft())
}

func (c *Slice) IsProperSuffixOf(other *Slice) bool {
	return other != nil && c.BitsLeft() < other.BitsLeft() &&
		c.bitsEqualAt(other, 0, other.BitsLeft()-c.BitsLeft(), c.BitsLeft())
}

func (c *Slice) CountLeading(bit bool) int {
	bits := c.BitsLeft()
	if bits == 0 {
		return 0
	}

	var want byte
	if bit {
		want = 1
	}

	for i := uint(0); i < bits; i++ {
		if c.bitAt(i) != want {
			return int(i)
		}
	}
	return int(bits)
}

func (c *Slice) CountTrailing(bit bool) int {
	bits := c.BitsLeft()
	if bits == 0 {
		return 0
	}

	var want byte
	if bit {
		want = 1
	}

	for i := int(bits) - 1; i >= 0; i-- {
		if c.bitAt(uint(i)) != want {
			return int(bits) - 1 - i
		}
	}
	return int(bits)
}

func (c *Slice) RemoveTrailing() int {
	if c.BitsLeft() == 0 {
		return 0
	}

	trailing := c.CountTrailing(false)
	if trailing >= int(c.BitsLeft()) {
		c.bitEnd -= uint16(trailing)
		return trailing
	}

	c.bitEnd -= uint16(trailing + 1)
	return trailing
}

func (c *Slice) Depth() uint16 {
	var depth uint16
	for i := 0; i < c.RefsNum(); i++ {
		childDepth := c.refCellAt(i).Depth() + 1
		if childDepth > depth {
			depth = childDepth
		}
	}
	return depth
}
