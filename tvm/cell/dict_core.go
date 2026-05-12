package cell

import "fmt"

func (c *Slice) loadMaybeRefCell() (*Cell, bool, error) {
	has, err := c.LoadBoolBit()
	if err != nil {
		return nil, false, err
	}
	if !has {
		return nil, false, nil
	}

	ref, err := c.LoadRefCell()
	return ref, true, err
}

type fixedDictNode struct {
	cell     *Cell
	refView  cellRefView
	loader   *Slice
	label    *Builder
	labelLen uint
}

func parseFixedDictNode(branch *Cell, remaining uint) (fixedDictNode, error) {
	loader, err := branch.BeginParse()
	if err != nil {
		return fixedDictNode{}, err
	}
	refView := newCellRefView(loader.cell)
	if loader.cell.IsSpecial() {
		return fixedDictNode{cell: loader.cell, refView: refView, loader: loader}, nil
	}
	labelLen, label, err := loadLabel(remaining, loader, BeginCell())
	if err != nil {
		return fixedDictNode{}, err
	}

	return fixedDictNode{
		cell:     loader.cell,
		refView:  refView,
		loader:   loader,
		label:    label,
		labelLen: labelLen,
	}, nil
}

func (n fixedDictNode) isLeaf(remaining uint) bool {
	return n.labelLen == remaining
}

func (n fixedDictNode) nextKeyBits(remaining uint) uint {
	return remaining - n.labelLen - 1
}

func (n *fixedDictNode) ref(i int) (*Cell, error) {
	if n.loader != nil {
		return n.loader.peekRefCellAt(i)
	}
	return n.refView.boundaryRef(i)
}

func (n *fixedDictNode) boundaryRef(i int) (*Cell, error) {
	if n.loader != nil {
		if i < 0 {
			return nil, ErrNegative
		}
		if i >= n.loader.RefsNum() {
			return nil, ErrNoMoreRefs
		}
		return n.loader.withChildTrace(n.loader.boundaryRefCellAt(i), int(n.loader.refStart)+i), nil
	}
	return n.refView.boundaryRef(i)
}

func (n *fixedDictNode) refs() (*Cell, *Cell, error) {
	left, err := n.ref(0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load left ref: %w", err)
	}

	right, err := n.ref(1)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load right ref: %w", err)
	}

	return left, right, nil
}

func (n fixedDictNode) rejectSpecial(kind string) error {
	if n.cell.IsSpecial() {
		return fmt.Errorf("%s has special cells in tree structure", kind)
	}
	return nil
}

func (n fixedDictNode) prunedBoundary(kind string) (bool, error) {
	if !n.cell.IsSpecial() {
		return false, nil
	}
	if n.cell.GetType() == PrunedCellType {
		return true, nil
	}
	return false, fmt.Errorf("%s has unsupported special cell in tree structure", kind)
}

func (n fixedDictNode) requireAugmentedBody(kind string) error {
	pruned, err := n.prunedBoundary(kind)
	if err != nil {
		return err
	}
	if pruned {
		return ErrAugmentationSemanticsUnavailable
	}
	return nil
}

func (n *fixedDictNode) cloneWithRef(i int, ref *Cell, trace *Trace) (*Cell, bool, error) {
	return n.refView.cloneWithRef(i, ref, trace)
}

func (n fixedDictNode) splitLabel(matched uint) (*Slice, *Slice, error) {
	label := n.label.ToSlice()
	prefixBits, err := label.LoadSlice(matched)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load label prefix: %w", err)
	}
	if err = label.SkipBits(1); err != nil {
		return nil, nil, fmt.Errorf("failed to skip label edge bit: %w", err)
	}

	prefix := BeginCell().MustStoreSlice(prefixBits, matched).ToSlice()
	return prefix, label, nil
}

func (n fixedDictNode) appendEdgeLabel(bit uint64, label *Builder, name string) error {
	if err := n.label.StoreUInt(bit, 1); err != nil {
		return fmt.Errorf("failed to append %s edge bit: %w", name, err)
	}
	if err := n.label.StoreBuilder(label); err != nil {
		return fmt.Errorf("failed to append %s label: %w", name, err)
	}
	return nil
}

func matchBuilderLabel(label *Builder, labelLen uint, key *Slice) (matched uint, newRight bool, diverged bool, err error) {
	labelSlice := label.ToSlice()
	for matched < labelLen && key.BitsLeft() > 0 {
		curr, err := labelSlice.LoadUInt(1)
		if err != nil {
			return 0, false, false, err
		}

		next, err := key.LoadUInt(1)
		if err != nil {
			return 0, false, false, err
		}

		if curr != next {
			return matched, next != 0, true, nil
		}
		matched++
	}
	return matched, false, false, nil
}
