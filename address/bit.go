package address

func setBit(n *byte, pos uint) {
	if pos > 7 {
		panic("bit position out of range [0..7]")
	}
	*n |= 1 << pos
}

func clearBit(n *byte, pos uint) {
	if pos > 7 {
		panic("bit position out of range [0..7]")
	}
	mask := ^(1 << pos)
	*n &= byte(mask)
}

func hasBit(n byte, pos uint) bool {
	if pos > 7 {
		panic("bit position out of range [0..7]")
	}
	val := n & (1 << pos)
	return val > 0
}
