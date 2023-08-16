package cell

import "math/bits"

type LevelMask struct {
	Mask byte
}

func (m LevelMask) GetLevel() int {
	return bits.Len8(m.Mask)
}

func (m LevelMask) getHashIndex() int {
	return bits.OnesCount8(m.Mask)
}

func (m LevelMask) Apply(level int) LevelMask {
	return LevelMask{m.Mask & ((1 << level) - 1)}
}

func (m LevelMask) IsSignificant(level int) bool {
	return level == 0 || ((m.Mask>>(level-1))%2 != 0)
}
