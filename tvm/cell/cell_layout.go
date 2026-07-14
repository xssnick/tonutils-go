package cell

const (
	cellFlagSpecial        uint8 = 1 << 0
	cellFlagLevelMaskShift uint8 = 1
	cellFlagRefsNumShift   uint8 = 4
	cellFlagLazy           uint8 = 1 << 7
)

const (
	cellFlagLevelMaskMask uint8 = 0b111 << cellFlagLevelMaskShift
	cellFlagRefsNumMask   uint8 = 0b111 << cellFlagRefsNumShift
)

// Cell.typ cache encoding: 0 means "not resolved yet" (the zero value), other
// values store the resolved Type shifted by one, with UnknownCellType mapped
// to a dedicated slot since Type(0xFF)+1 would wrap to the unresolved marker.
const (
	cellTypeCacheNone    uint8 = 0
	cellTypeCacheUnknown uint8 = 6
)

func encodeCellTypeCache(t Type) uint8 {
	if t == UnknownCellType {
		return cellTypeCacheUnknown
	}
	return uint8(t) + 1
}

func decodeCellTypeCache(v uint8) Type {
	if v == cellTypeCacheUnknown {
		return UnknownCellType
	}
	return Type(v - 1)
}

type cellMeta struct {
	extraHashes *[3]Hash
	viewOf      *Cell
	lazyLoader  LazyCellLoader
	trace       *Trace

	extraDepths [3]uint16
	viewLevel   uint8 // effectiveLevel + 1
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
	cp.trace = nil
	if cp.extraHashes == nil && cp.viewOf == nil && cp.lazyLoader == nil && cp.viewLevel == 0 {
		return nil
	}
	return &cp
}

func (c *Cell) IsSpecial() bool {
	return c.flags&cellFlagSpecial != 0
}

func (c *Cell) setSpecial(special bool) {
	c.typ = cellTypeCacheNone
	if special {
		c.flags |= cellFlagSpecial
		return
	}
	c.flags &^= cellFlagSpecial
}

func (c *Cell) IsLazy() bool {
	return c.flags&cellFlagLazy != 0
}

func (c *Cell) setLazy(lazy bool) {
	c.typ = cellTypeCacheNone
	if lazy {
		c.flags |= cellFlagLazy
		return
	}
	c.flags &^= cellFlagLazy
}

func (c *Cell) getLevelMask() LevelMask {
	return LevelMask{Mask: (c.flags & cellFlagLevelMaskMask) >> cellFlagLevelMaskShift}
}

func (c *Cell) setLevelMask(mask LevelMask) {
	c.typ = cellTypeCacheNone
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

	c.typ = cellTypeCacheNone
	c.flags &^= cellFlagRefsNumMask
	c.flags |= uint8(refs) << cellFlagRefsNumShift
}

func (c *Cell) rawRefs() []*Cell {
	refCnt := c.refsCount()
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
	if c.meta.extraHashes != nil || c.meta.viewOf != nil || c.meta.lazyLoader != nil || c.meta.trace != nil || c.meta.viewLevel != 0 {
		return
	}
	c.meta = nil
}

func (c *Cell) clearVirtualization() {
	if c.meta == nil {
		return
	}
	if c.meta.viewLevel != 0 {
		c.meta.viewOf = nil
		c.meta.viewLevel = 0
	}
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
		meta.extraHashes = new([3]Hash)
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
