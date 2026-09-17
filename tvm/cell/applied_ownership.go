package cell

// copyWithOwnedData separates a long-lived cell from a temporary BOC or proof
// arena. Cell and payload share one small allocation, not a slab containing
// unrelated cells: retaining one state leaf must not retain an entire block.
func (c *Cell) copyWithOwnedData() *Cell {
	cp := newCellWithData(len(c.data))
	data := cp.data
	*cp = *c
	cp.data = data
	copy(cp.data, c.data)
	cp.meta = cloneCellMeta(c.meta)
	return cp
}

// copyLeafWithOwnedData detaches a loaded childless destination cell without
// changing its runtime view. A virtualized pruned leaf still needs its raw
// representation, so the view is rebuilt over a private raw leaf instead of
// retaining the temporary graph through viewOf.
func (c *Cell) copyLeafWithOwnedData() *Cell {
	source := c.rawCell()
	owned := source.copyWithOwnedData()
	if owned.meta != nil {
		owned.meta.lazyLoader = nil
		owned.meta.lazyFlags = 0
		owned.clearMetaIfEmpty()
	}
	if c.IsVirtualized() {
		return owned.Virtualize(uint8(c.EffectiveLevel()))
	}
	return owned
}
