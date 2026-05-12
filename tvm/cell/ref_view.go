package cell

import "fmt"

type cellRefView struct {
	cell       *Cell
	refCnt     uint8
	childLevel uint8
	virtual    bool
}

func newCellRefView(c *Cell) cellRefView {
	v := cellRefView{
		cell:   c,
		refCnt: (c.flags & cellFlagRefsNumMask) >> cellFlagRefsNumShift,
	}
	if meta := c.meta; meta != nil && meta.viewLevel != 0 {
		v.childLevel = childEffectiveLevelFor(c, meta.viewLevel-1)
		v.virtual = true
	}
	return v
}

func (v *cellRefView) viewRef(ref *Cell) *Cell {
	if v.virtual && ref != nil {
		return ref.Virtualize(v.childLevel)
	}
	return ref
}

func (v *cellRefView) effectiveRef(ref *Cell) *Cell {
	if ref == nil {
		return nil
	}
	if v.virtual {
		return ref.Virtualize(v.childLevel)
	}
	return ref.Virtualize(childEffectiveLevelFor(v.cell, 0))
}

func (v *cellRefView) boundaryRef(i int) (*Cell, error) {
	if i < 0 {
		return nil, ErrNegative
	}
	if i >= int(v.refCnt) {
		return nil, ErrNoMoreRefs
	}

	return v.viewRef(v.cell.refs[i]), nil
}

func (v *cellRefView) logicalBoundaryRef(i int) *Cell {
	return v.effectiveRef(v.cell.refs[i])
}

func (v *cellRefView) cloneWithRef(i int, ref *Cell, trace *Trace) (*Cell, bool, error) {
	if ref == nil {
		return nil, false, ErrRefCannotBeNil
	}

	if v.virtual {
		var refsBuf [4]*Cell
		refs := refsBuf[:v.refCnt]
		var err error
		for j := range refs {
			refs[j], err = v.boundaryRef(j)
			if err != nil {
				return nil, false, err
			}
		}
		refs[i] = ref
		return v.cloneWithRefs(refs, trace)
	}

	oldRef, err := v.boundaryRef(i)
	if err != nil {
		return nil, false, err
	}
	if ref == oldRef {
		return v.cell, false, nil
	}

	cloned := v.cell.copy()
	cloned.setRef(i, ref)
	if err := trace.NotifyCreate(); err != nil {
		return nil, false, err
	}
	if err := cloned.refreshLevelMaskForRefs(); err != nil {
		return nil, false, err
	}
	if err := cloned.calculateHashes(); err != nil {
		return nil, false, err
	}
	return cloned, true, nil
}

func (v *cellRefView) cloneWithRefs(refs []*Cell, trace *Trace) (*Cell, bool, error) {
	refCnt := int(v.refCnt)
	if len(refs) != refCnt {
		return nil, false, fmt.Errorf("unexpected refs count: got %d want %d", len(refs), refCnt)
	}

	var cloned *Cell
	materialize := v.virtual
	changed := materialize
	if materialize {
		cloned = v.cell.copy()
	}
	for i, ref := range refs {
		var oldRef *Cell
		var err error
		oldRef, err = v.boundaryRef(i)
		if err != nil {
			return nil, false, err
		}
		if !materialize && ref == oldRef {
			continue
		}

		if cloned == nil {
			cloned = v.cell.copy()
		}
		cloned.setRef(i, ref)
		changed = true
	}

	if !changed {
		return v.cell, false, nil
	}

	if err := trace.NotifyCreate(); err != nil {
		return nil, false, err
	}
	if err := cloned.refreshLevelMaskForRefs(); err != nil {
		return nil, false, err
	}
	if err := cloned.calculateHashes(); err != nil {
		return nil, false, err
	}
	return cloned, true, nil
}

func (c *Cell) refreshLevelMaskForRefs() error {
	refs := c.rawRefs()
	mask := byte(0)
	for _, ref := range refs {
		if ref == nil {
			return ErrRefCannotBeNil
		}
		if ref.Depth() >= maxDepth {
			return ErrCellDepthLimit
		}
		mask |= ref.getLevelMask().Mask
	}

	if !c.IsSpecial() {
		c.setLevelMask(LevelMask{Mask: mask})
		return nil
	}

	switch c.GetType() {
	case MerkleProofCellType:
		if len(refs) == 1 {
			c.setLevelMask(LevelMask{Mask: refs[0].getLevelMask().Mask >> 1})
		}
	case MerkleUpdateCellType:
		if len(refs) == 2 {
			c.setLevelMask(LevelMask{Mask: mask >> 1})
		}
	}

	return validateBoundaryCell(c)
}
