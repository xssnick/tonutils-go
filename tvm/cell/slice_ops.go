package cell

import (
	"bytes"
	mathbits "math/bits"
)

func (c *Slice) PeekRefCellAt(i int) (*Cell, error) {
	return c.peekRefCellAt(i)
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
	c.bitStart += uint16(bits)
	c.refStart += uint8(refs)
	return true
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
	start := uint(c.bitStart) + offset
	otherStart := uint(other.bitStart) + otherOffset
	if start%8 == otherStart%8 {
		return bitsEqualSameOffset(
			c.cell.data[start/8:],
			other.cell.data[otherStart/8:],
			start%8,
			bits,
		)
	}

	for done := uint(0); done < bits; {
		chunk := min(uint(64), bits-done)
		a, err := preloadSliceUIntAt(c, offset+done, chunk)
		if err != nil {
			return false
		}
		b, err := preloadSliceUIntAt(other, otherOffset+done, chunk)
		if err != nil {
			return false
		}
		if a != b {
			return false
		}
		done += chunk
	}
	return true
}

func bitsEqualSameOffset(left, right []byte, offset, bits uint) bool {
	if bits == 0 {
		return true
	}

	if offset != 0 {
		headBits := min(bits, 8-offset)
		mask := byte(0xFF>>offset) & byte(0xFF<<(8-offset-headBits))
		if left[0]&mask != right[0]&mask {
			return false
		}

		bits -= headBits
		if bits == 0 {
			return true
		}
		left = left[1:]
		right = right[1:]
	}

	wholeBytes := bits / 8
	if !bytes.Equal(left[:wholeBytes], right[:wholeBytes]) {
		return false
	}

	tailBits := bits % 8
	if tailBits == 0 {
		return true
	}
	mask := byte(0xFF << (8 - tailBits))
	return left[wholeBytes]&mask == right[wholeBytes]&mask
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

	// big-endian chunks compare the same way the bit stream does
	for done := uint(0); done < limit; {
		chunk := min(uint(64), limit-done)
		a, errA := preloadSliceUIntAt(c, done, chunk)
		b, errB := preloadSliceUIntAt(other, done, chunk)
		if errA != nil || errB != nil {
			break
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
		done += chunk
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
	bits := c.BitsLeft()
	return bits == other.BitsLeft() && c.bitsEqualAt(other, 0, 0, bits)
}

func (c *Slice) IsPrefixOf(other *Slice) bool {
	bits := c.BitsLeft()
	if other == nil || bits > other.BitsLeft() {
		return false
	}
	return c.bitsEqualAt(other, 0, 0, bits)
}

func (c *Slice) IsProperPrefixOf(other *Slice) bool {
	if other == nil {
		return false
	}
	bits := c.BitsLeft()
	return bits < other.BitsLeft() && c.bitsEqualAt(other, 0, 0, bits)
}

func (c *Slice) IsSuffixOf(other *Slice) bool {
	bits := c.BitsLeft()
	if other == nil || bits > other.BitsLeft() {
		return false
	}
	return c.bitsEqualAt(other, 0, other.BitsLeft()-bits, bits)
}

func (c *Slice) IsProperSuffixOf(other *Slice) bool {
	if other == nil {
		return false
	}
	bits := c.BitsLeft()
	otherBits := other.BitsLeft()
	return bits < otherBits && c.bitsEqualAt(other, 0, otherBits-bits, bits)
}

func (c *Slice) CountLeading(bit bool) int {
	bits := c.BitsLeft()
	if bits == 0 {
		return 0
	}

	for done := uint(0); done < bits; {
		chunk := min(uint(64), bits-done)
		v, err := preloadSliceUIntAt(c, done, chunk)
		if err != nil {
			return int(done)
		}
		want := sameBitsValue(bit, chunk)
		if v != want {
			// first mismatching bit within the chunk window
			return int(done) + mathbits.LeadingZeros64((v^want)<<(64-chunk))
		}
		done += chunk
	}
	return int(bits)
}

func (c *Slice) CountTrailing(bit bool) int {
	bits := c.BitsLeft()
	if bits == 0 {
		return 0
	}

	for done := uint(0); done < bits; {
		chunk := min(uint(64), bits-done)
		v, err := preloadSliceUIntAt(c, bits-done-chunk, chunk)
		if err != nil {
			return int(done)
		}
		want := sameBitsValue(bit, chunk)
		if v != want {
			// last mismatching bit is the lowest xor bit of the chunk
			return int(done) + mathbits.TrailingZeros64(v^want)
		}
		done += chunk
	}
	return int(bits)
}

func (c *Slice) RemoveTrailing() int {
	bits := c.BitsLeft()
	if bits == 0 {
		return 0
	}

	trailing := c.CountTrailing(false)
	if trailing >= int(bits) {
		c.bitEnd -= uint16(trailing)
		return trailing
	}

	c.bitEnd -= uint16(trailing + 1)
	return trailing
}

func (c *Slice) Depth() uint16 {
	var depth uint16
	refs := c.RefsNum()
	for i := 0; i < refs; i++ {
		childDepth := c.boundaryRefCellAt(i).Depth() + 1
		if childDepth > depth {
			depth = childDepth
		}
	}
	return depth
}
