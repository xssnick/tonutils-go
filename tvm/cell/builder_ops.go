package cell

func (b *Builder) CanExtendBy(bits uint, refs uint) bool {
	if b.bitsSz >= 1024 {
		return false
	}
	refsLeft := 4 - int(b.refsNum)
	return refsLeft >= 0 && bits < 1024-b.bitsSz && refs <= uint(refsLeft)
}

func (b *Builder) Depth() uint16 {
	var depth uint16
	for i := uint8(0); i < b.refsNum; i++ {
		ref := b.refs[i]
		childDepth := ref.Depth() + 1
		if childDepth > depth {
			depth = childDepth
		}
	}
	return depth
}

func (b *Builder) EndCellSpecial(special bool) (*Cell, error) {
	if err := b.trace.NotifyCreate(); err != nil {
		return nil, err
	}
	cl, err := finalizeCellFromBuilder(b, special)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

func (b *Builder) StoreSameBit(bit bool, bits uint) error {
	if !b.CanExtendBy(bits, 0) {
		return ErrNotFit1023
	}
	if bits == 0 {
		return nil
	}

	start := b.bitsSz
	byteIdx := int(start / 8)
	bitOffset := start % 8
	left := bits

	if bitOffset != 0 {
		n := min(left, 8-bitOffset)
		mask := byte(((uint16(1) << n) - 1) << (8 - bitOffset - n))
		if bit {
			b.data[byteIdx] |= mask
		} else {
			b.data[byteIdx] &^= mask
		}
		left -= n
		byteIdx++
	}

	fullBytes := int(left / 8)
	if bit {
		for i := 0; i < fullBytes; i++ {
			b.data[byteIdx+i] = 0xFF
		}
	} else {
		clear(b.data[byteIdx : byteIdx+fullBytes])
	}
	byteIdx += fullBytes
	left %= 8

	if left != 0 {
		mask := byte(0xFF << (8 - left))
		if bit {
			b.data[byteIdx] |= mask
		} else {
			b.data[byteIdx] &^= mask
		}
	}

	b.bitsSz += bits
	if rem := b.bitsSz % 8; rem != 0 {
		b.data[b.bitsSz/8] &= byte(0xFF << (8 - rem))
	}
	return nil
}
