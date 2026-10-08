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

func (m LevelMask) getHashesCount() int {
	return m.getHashIndex() + 1
}

// A negative level means the top level here, as it does in Cell.Hash,
// Cell.Depth and BOCCellMeta.HashAtLevel, so it is clamped to the maximum.
func (m LevelMask) Apply(level int) LevelMask {
	if level < 0 {
		level = _DataCellMaxLevel
	}
	return LevelMask{m.Mask & ((1 << uint(level)) - 1)}
}

func (m LevelMask) IsSignificant(level int) bool {
	if level < 0 {
		level = _DataCellMaxLevel
	}
	return level == 0 || ((m.Mask>>uint(level-1))%2 != 0)
}
