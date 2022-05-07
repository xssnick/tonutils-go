package utils

//TODO add length checks and panic on errors

func SetBit(n *byte, pos uint) {
	*n |= 1 << pos
}

func ClearBit(n *byte, pos uint) {
	mask := ^(1 << pos)
	*n &= byte(mask)
}

func HasBit(n byte, pos uint) bool {
	val := n & (1 << pos)
	return val > 0
}
