package address

// TODO add length checks and panic on errors

func setBit(n *byte, pos uint) {
	*n |= 1 << pos
}

func clearBit(n *byte, pos uint) {
	mask := ^(1 << pos)
	*n &= byte(mask)
}

func hasBit(n byte, pos uint) bool {
	val := n & (1 << pos)
	return val > 0
}
