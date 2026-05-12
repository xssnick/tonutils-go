package cell

import "fmt"

type refMode uint8

const (
	refStored refMode = iota
	refBoundary
	refResolved
)

type cellRefView struct {
	cell       *Cell
	refs       [4]*Cell
	loaded     uint8
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

func (v *cellRefView) refAt(i int, mode refMode) (*Cell, error) {
	if i < 0 {
		return nil, ErrNegative
	}
	if i >= int(v.refCnt) {
		return nil, ErrNoMoreRefs
	}

	ref := v.cell.refs[i]
	switch mode {
	case refStored:
		return ref, nil
	case refBoundary:
		return v.viewRef(ref), nil
	case refResolved:
	default:
		return nil, fmt.Errorf("unknown ref mode")
	}

	mask := uint8(1 << i)
	if v.loaded&mask == 0 {
		if ref != nil && ref.IsLazy() {
			loaded, err := loadLazyPrunedRef(ref)
			if err != nil {
				return nil, err
			}
			ref = loaded
		}
		v.refs[i] = v.viewRef(ref)
		v.loaded |= mask
	}
	return v.refs[i], nil
}

func (v *cellRefView) resolvedRef(i int) (*Cell, error) {
	return v.refAt(i, refResolved)
}

func (v *cellRefView) boundaryRef(i int) (*Cell, error) {
	return v.refAt(i, refBoundary)
}

func (v *cellRefView) logicalBoundaryRef(i int) *Cell {
	return v.effectiveRef(v.cell.refs[i])
}

func (v *cellRefView) cloneWithRef(i int, ref *Cell, observer Observer) (*Cell, bool, error) {
	if ref == nil {
		return nil, false, ErrRefCannotBeNil
	}

	if v.virtual {
		var refsBuf [4]*Cell
		refs := refsBuf[:v.refCnt]
		var err error
		for j := range refs {
			refs[j], err = v.resolvedRef(j)
			if err != nil {
				return nil, false, err
			}
		}
		refs[i] = ref
		return v.cloneWithRefs(refs, observer)
	}

	oldRef, err := v.resolvedRef(i)
	if err != nil {
		return nil, false, err
	}
	if ref == oldRef {
		return v.cell, false, nil
	}

	cloned, err := v.cell.cloneWithRefObserved(i, ref, observer)
	return cloned, err == nil, err
}

func (v *cellRefView) cloneWithRefs(refs []*Cell, observer Observer) (*Cell, bool, error) {
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
		oldRef, err := v.resolvedRef(i)
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

	if err := notifyCellCreate(observer); err != nil {
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
