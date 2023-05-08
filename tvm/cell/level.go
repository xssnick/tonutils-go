package cell

import "math/bits"

type LevelMask struct {
	mask byte
}

func (m LevelMask) getLevel() int {
	return bits.Len8(m.mask)
}

func (m LevelMask) getHashIndex() int {
	return bits.OnesCount8(m.mask)
}

func (m LevelMask) apply(level int) LevelMask {
	return LevelMask{m.mask & ((1 << level) - 1)}
}

func (m LevelMask) isSignificant(level int) bool {
	return level == 0 || ((m.mask>>(level-1))%2 != 0)
}
