package cell

func makeManualCellForTest(isSpecial bool, levelMask LevelMask, bitsSz uint, data []byte, refs []*Cell) *Cell {
	cl := &Cell{
		data:   append([]byte(nil), data...),
		bitsSz: uint16(bitsSz),
	}
	cl.setSpecial(isSpecial)
	cl.setLevelMask(levelMask)
	cl.setRefs(refs)
	return cl
}
