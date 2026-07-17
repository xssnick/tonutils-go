package cell

func childEffectiveLevelFor(parent *Cell, effectiveLevel uint8) uint8 {
	if parent == nil {
		return 0
	}
	switch parent.GetType() {
	case MerkleProofCellType, MerkleUpdateCellType:
		return effectiveLevel + 1
	default:
		return effectiveLevel
	}
}

func (c *Cell) rawCell() *Cell {
	if c.meta != nil && c.meta.viewLevel != 0 {
		return c.meta.viewOf
	}
	return c
}

func (c *Cell) currentEffectiveLevel() int {
	if c.meta != nil && c.meta.viewLevel != 0 {
		return int(c.meta.viewLevel - 1)
	}
	return c.getLevelMask().GetLevel()
}

func (c *Cell) IsVirtualized() bool {
	return c.meta != nil && c.meta.viewLevel != 0
}

func (c *Cell) EffectiveLevel() int {
	return c.currentEffectiveLevel()
}

func (c *Cell) ActualLevel() int {
	raw := c.rawCell()
	return raw.getLevelMask().GetLevel()
}

func (c *Cell) Virtualize(effectiveLevel uint8) *Cell {
	trace := c.Trace()
	level := c.Level()
	if level <= int(effectiveLevel) {
		return c
	}

	raw := c.rawCell()
	rawLevelMask := raw.getLevelMask()
	if rawLevelMask.GetLevel() <= int(effectiveLevel) {
		if trace != nil && raw.Trace() != trace {
			return raw.WithTrace(trace)
		}
		return raw
	}

	vc := &Cell{
		data:   raw.data,
		bitsSz: raw.bitsSz,
		flags:  raw.flags,
	}
	copy(vc.refs[:], raw.rawRefs())
	vc.setLevelMask(rawLevelMask.Apply(int(effectiveLevel)))
	vc.meta = &cellMeta{
		viewOf:                raw,
		lazyLoader:            raw.cellLazyLoader(),
		trace:                 trace,
		viewLevel:             effectiveLevel + 1,
		skipLazyRefValidation: raw.meta != nil && raw.meta.skipLazyRefValidation,
	}
	return vc
}

func (c *Cell) boundaryRefs() []*Cell {
	refCnt := c.refsCount()
	if refCnt == 0 {
		return nil
	}

	refs := make([]*Cell, refCnt)
	refView := newCellRefView(c)
	for i := range refs {
		refs[i], _ = refView.boundaryRef(i)
	}
	return refs
}
