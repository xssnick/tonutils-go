package cell

func (c *Slice) PeekRefCellAt(i int) (*Cell, error) {
	if i < 0 || i >= len(c.refs) {
		return nil, ErrNoMoreRefs
	}
	return c.refs[i], nil
}

func (c *Slice) bitAt(offset uint) byte {
	abs := c.loadedSz + offset
	return (c.data[abs/8] >> (7 - (abs % 8))) & 1
}

func (c *Slice) OnlyFirst(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || len(c.refs) < refs {
		return false
	}
	c.bitsSz = c.loadedSz + bits
	c.refs = c.refs[:refs]
	return true
}

func (c *Slice) SkipFirst(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || len(c.refs) < refs {
		return false
	}
	return c.AdvanceExt(bits, refs) == nil
}

func (c *Slice) OnlyLast(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || len(c.refs) < refs {
		return false
	}
	if err := c.Advance(c.BitsLeft() - bits); err != nil {
		return false
	}
	c.refs = c.refs[len(c.refs)-refs:]
	return true
}

func (c *Slice) SkipLast(bits uint, refs int) bool {
	if refs < 0 || c.BitsLeft() < bits || len(c.refs) < refs {
		return false
	}
	c.bitsSz -= bits
	c.refs = c.refs[:len(c.refs)-refs]
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
		switch {
		case c == nil:
			return 0
		case c.BitsLeft() == 0:
			return 0
		default:
			return 1
		}
	}
	if c == nil {
		if other.BitsLeft() == 0 {
			return 0
		}
		return -1
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
	if c == nil || other == nil {
		return c == other
	}
	return c.BitsLeft() == other.BitsLeft() && c.bitsEqualAt(other, 0, 0, c.BitsLeft())
}

func (c *Slice) IsPrefixOf(other *Slice) bool {
	if c == nil {
		return true
	}
	if other == nil || c.BitsLeft() > other.BitsLeft() {
		return false
	}
	return c.bitsEqualAt(other, 0, 0, c.BitsLeft())
}

func (c *Slice) IsProperPrefixOf(other *Slice) bool {
	if c == nil {
		return other != nil && other.BitsLeft() > 0
	}
	return other != nil && c.BitsLeft() < other.BitsLeft() && c.bitsEqualAt(other, 0, 0, c.BitsLeft())
}

func (c *Slice) IsSuffixOf(other *Slice) bool {
	if c == nil {
		return true
	}
	if other == nil || c.BitsLeft() > other.BitsLeft() {
		return false
	}
	return c.bitsEqualAt(other, 0, other.BitsLeft()-c.BitsLeft(), c.BitsLeft())
}

func (c *Slice) IsProperSuffixOf(other *Slice) bool {
	if c == nil {
		return other != nil && other.BitsLeft() > 0
	}
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
		c.bitsSz -= uint(trailing)
		return trailing
	}

	c.bitsSz -= uint(trailing + 1)
	return trailing
}

func (c *Slice) Depth() uint16 {
	var depth uint16
	for _, ref := range c.refs {
		childDepth := ref.Depth() + 1
		if childDepth > depth {
			depth = childDepth
		}
	}
	return depth
}
