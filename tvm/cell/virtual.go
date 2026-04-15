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
	if base := c.baseCell(); base != nil {
		return base
	}
	return c
}

func (c *Cell) currentEffectiveLevel() int {
	if c.baseCell() != nil {
		return int(c.effectiveLevelValue())
	}
	return c.getLevelMask().GetLevel()
}

func (c *Cell) IsVirtualized() bool {
	return c.baseCell() != nil
}

func (c *Cell) EffectiveLevel() int {
	return c.currentEffectiveLevel()
}

func (c *Cell) ActualLevel() int {
	raw := c.rawCell()
	return raw.getLevelMask().GetLevel()
}

func (c *Cell) Virtualize(effectiveLevel uint8) *Cell {
	if c.Level() <= int(effectiveLevel) {
		return c
	}

	raw := c.rawCell()
	if raw.getLevelMask().GetLevel() <= int(effectiveLevel) {
		return raw
	}

	vc := &Cell{
		data:   raw.data,
		bitsSz: raw.bitsSz,
		flags:  raw.flags,
	}
	copy(vc.refs[:], raw.rawRefs())
	vc.setLevelMask(raw.getLevelMask().Apply(int(effectiveLevel)))
	vc.meta = &cellMeta{
		base:           raw,
		effectiveLevel: effectiveLevel,
	}
	return vc
}

func (c *Cell) childEffectiveLevel() uint8 {
	return childEffectiveLevelFor(c, c.effectiveLevelValue())
}

func (c *Cell) visibleRef(i int) *Cell {
	if i < 0 || i >= c.refsCount() {
		return nil
	}
	ref := c.refs[i]
	if ref == nil || !c.IsVirtualized() {
		return ref
	}
	return ref.Virtualize(c.childEffectiveLevel())
}

func (c *Cell) visibleRefs() []*Cell {
	if c.refsCount() == 0 {
		return nil
	}
	if !c.IsVirtualized() {
		return c.rawRefs()
	}
	refs := make([]*Cell, c.refsCount())
	for i := range refs {
		refs[i] = c.visibleRef(i)
	}
	return refs
}
