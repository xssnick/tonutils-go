package cell

const (
	cellFlagSpecial        uint8 = 1 << 0
	cellFlagLevelMaskShift uint8 = 1
	cellFlagRefsNumShift   uint8 = 4
)

const (
	cellFlagLevelMaskMask uint8 = 0b111 << cellFlagLevelMaskShift
	cellFlagRefsNumMask   uint8 = 0b111 << cellFlagRefsNumShift
)

type cellMeta struct {
	base *Cell

	extraHashes *[3][32]byte
	extraDepths [3]uint16

	effectiveLevel uint8
}

func cloneCellMeta(meta *cellMeta) *cellMeta {
	if meta == nil {
		return nil
	}

	cp := *meta
	if meta.extraHashes != nil {
		extra := *meta.extraHashes
		cp.extraHashes = &extra
	}
	return &cp
}

func (c *Cell) isSpecial() bool {
	return c.flags&cellFlagSpecial != 0
}

func (c *Cell) setSpecial(special bool) {
	if special {
		c.flags |= cellFlagSpecial
		return
	}
	c.flags &^= cellFlagSpecial
}

func (c *Cell) getLevelMask() LevelMask {
	return LevelMask{Mask: (c.flags & cellFlagLevelMaskMask) >> cellFlagLevelMaskShift}
}

func (c *Cell) setLevelMask(mask LevelMask) {
	c.flags &^= cellFlagLevelMaskMask
	c.flags |= (mask.Mask & 0b111) << cellFlagLevelMaskShift
}

func (c *Cell) refsCount() int {
	return int((c.flags & cellFlagRefsNumMask) >> cellFlagRefsNumShift)
}

func (c *Cell) setRefsCount(refs int) {
	if refs < 0 || refs > 4 {
		panic("invalid refs count")
	}

	c.flags &^= cellFlagRefsNumMask
	c.flags |= uint8(refs) << cellFlagRefsNumShift
}

func (c *Cell) rawRefs() []*Cell {
	refCnt := c.refsCount()
	if refCnt == 0 {
		return nil
	}
	return c.refs[:refCnt:refCnt]
}

func (c *Cell) setRefs(refs []*Cell) {
	if len(refs) > 4 {
		panic("too many refs")
	}

	for i := range c.refs {
		c.refs[i] = nil
	}
	copy(c.refs[:], refs)
	c.setRefsCount(len(refs))
}

func (c *Cell) ref(i int) *Cell {
	if i < 0 || i >= c.refsCount() {
		return nil
	}
	return c.refs[i]
}

func (c *Cell) setRef(i int, ref *Cell) {
	if i < 0 || i >= 4 {
		panic("invalid ref index")
	}
	c.refs[i] = ref
}

func (c *Cell) baseCell() *Cell {
	if c.meta == nil {
		return nil
	}
	return c.meta.base
}

func (c *Cell) effectiveLevelValue() uint8 {
	if c.meta == nil {
		return 0
	}
	return c.meta.effectiveLevel
}

func (c *Cell) ensureMeta() *cellMeta {
	if c.meta == nil {
		c.meta = &cellMeta{}
	}
	return c.meta
}

func (c *Cell) clearMetaIfEmpty() {
	if c.meta == nil {
		return
	}
	if c.meta.base != nil || c.meta.extraHashes != nil || c.meta.effectiveLevel != 0 {
		return
	}
	c.meta = nil
}

func (c *Cell) clearVirtualization() {
	if c.meta == nil {
		return
	}
	c.meta.base = nil
	c.meta.effectiveLevel = 0
	c.clearMetaIfEmpty()
}

func (c *Cell) clearExtraHashes() {
	if c.meta == nil {
		return
	}
	c.meta.extraHashes = nil
	c.meta.extraDepths = [3]uint16{}
	c.clearMetaIfEmpty()
}

func (c *Cell) hashAt(index int) []byte {
	if index == 0 {
		return c.hash0[:]
	}
	if c.meta == nil || c.meta.extraHashes == nil {
		return nil
	}
	return c.meta.extraHashes[index-1][:]
}

func (c *Cell) setHashAt(index int, sum []byte) {
	if index == 0 {
		copy(c.hash0[:], sum)
		return
	}

	meta := c.ensureMeta()
	if meta.extraHashes == nil {
		meta.extraHashes = new([3][32]byte)
	}
	copy(meta.extraHashes[index-1][:], sum)
}

func (c *Cell) depthAt(index int) uint16 {
	if index == 0 {
		return c.depth0
	}
	if c.meta == nil {
		return 0
	}
	return c.meta.extraDepths[index-1]
}

func (c *Cell) setDepthAt(index int, depth uint16) {
	if index == 0 {
		c.depth0 = depth
		return
	}
	meta := c.ensureMeta()
	meta.extraDepths[index-1] = depth
}
