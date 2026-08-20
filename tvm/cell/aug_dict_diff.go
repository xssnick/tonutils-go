package cell

import "fmt"

// AugDictDiffFunc receives changed leaves in key order. A nil oldValueExtra or
// newValueExtra means that the key is absent from that side of the diff. The
// slices contain the raw extra followed by the value.
type AugDictDiffFunc func(key *Cell, oldValueExtra, newValueExtra *Slice) error

// ScanDiff compares two HashmapAug tries structurally and visits their changed
// leaves in key order. Equal subtrees are skipped by hash. When
// checkOtherAugmentation is set, every changed node in other has its stored
// augmentation checked in the same traversal.
func (d *AugmentedDictionary) ScanDiff(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffFunc) error {
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}

	walk := augDictDiffWalk{
		keySz:    d.keySz,
		newAug:   other.aug,
		checkNew: checkOtherAugmentation,
		fn:       fn,
	}
	oldRoot := d.root.withTraceCombined(d.trace)
	newRoot := other.root.withTraceCombined(other.trace)
	if err := walk.node(oldRoot, newRoot, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	return nil
}

type augDictDiffWalk struct {
	keySz  uint
	key    Builder
	newAug Augmentation

	checkNew bool
	fn       AugDictDiffFunc

	checker augmentedNodeChecker
}

func (w *augDictDiffWalk) node(old, new *Cell, remaining, skipOld, skipNew uint) error {
	if old == nil {
		if new == nil {
			return nil
		}
		if skipNew != 0 {
			return fmt.Errorf("invalid new dictionary alignment")
		}
		return w.oneSide(nil, new, remaining, false)
	}
	if new == nil {
		if skipOld != 0 {
			return fmt.Errorf("invalid old dictionary alignment")
		}
		return w.oneSide(old, nil, remaining, true)
	}
	if skipOld == skipNew && old.HashKey() == new.HashKey() {
		return nil
	}

	oldNode, err := parseAugDictDiffNode(old, remaining+skipOld)
	if err != nil {
		return fmt.Errorf("invalid old dictionary node: %w", err)
	}
	newNode, err := parseAugDictDiffNode(new, remaining+skipNew)
	if err != nil {
		return fmt.Errorf("invalid new dictionary node: %w", err)
	}
	if oldNode.labelLen < skipOld || newNode.labelLen < skipNew {
		return fmt.Errorf("invalid dictionary diff alignment")
	}

	oldLabel := oldNode.labelSlice()
	newLabel := newNode.labelSlice()
	oldEffective := oldLabel
	newEffective := newLabel
	if err = oldEffective.SkipBits(skipOld); err != nil {
		return err
	}
	if err = newEffective.SkipBits(skipNew); err != nil {
		return err
	}

	depth := w.keySz - remaining
	if !w.keyMatchesSkippedLabel(&oldLabel, depth, skipOld) || !w.keyMatchesSkippedLabel(&newLabel, depth, skipNew) {
		return fmt.Errorf("invalid dictionary diff prefix")
	}
	w.storeLabel(&oldLabel, depth-skipOld)

	oldLen := oldNode.labelLen - skipOld
	newLen := newNode.labelLen - skipNew
	common, err := commonSlicePrefix(&oldEffective, &newEffective, min(oldLen, newLen))
	if err != nil {
		return err
	}

	if common < oldLen && common < newLen {
		w.storeLabel(&newLabel, depth-skipNew)
		if oldEffective.bitAt(common) == 0 {
			if err = w.node(old, nil, remaining+skipOld, 0, 0); err != nil {
				return err
			}
			return w.node(nil, new, remaining+skipNew, 0, 0)
		}
		if err = w.node(nil, new, remaining+skipNew, 0, 0); err != nil {
			return err
		}
		return w.node(old, nil, remaining+skipOld, 0, 0)
	}

	if common == oldLen && common == newLen {
		if common == remaining {
			oldValue, newValue := oldNode.value(), newNode.value()
			equal := equalSliceContents(oldValue, newValue)
			if w.checkNew {
				if equal {
					// A nearby insertion or deletion can rebuild the same logical
					// leaf under a different Patricia path. C++ then validates the
					// new leaf through references shared with the predecessor. Carry
					// both path identities so its LeafExtra reads retain that exact
					// predecessor closure in a path-based usage tree.
					newNode.loader.trace = CombineTraces(newNode.loader.trace, oldNode.loader.trace)
				}
				if err = w.checker.leaf(newNode, w.newAug); err != nil {
					return fmt.Errorf("invalid new dictionary leaf augmentation: %w", err)
				}
			}
			if equal {
				return nil
			}
			return w.emit(oldValue, newValue)
		}

		if w.checkNew {
			if err = w.checker.fork(newNode, remaining-common, w.newAug); err != nil {
				return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
			}
		}

		nextRemaining := remaining - common - 1
		for child := 0; child < 2; child++ {
			w.setKeyBit(depth+common, byte(child))
			oldChild, err := oldNode.ref(child)
			if err != nil {
				return fmt.Errorf("failed to load old dictionary child: %w", err)
			}
			newChild, err := newNode.ref(child)
			if err != nil {
				return fmt.Errorf("failed to load new dictionary child: %w", err)
			}
			if err = w.node(oldChild, newChild, nextRemaining, 0, 0); err != nil {
				return err
			}
		}
		return nil
	}

	if common == oldLen {
		w.storeLabel(&newLabel, depth-skipNew)
		oldLeft, err := oldNode.ref(0)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary left child: %w", err)
		}
		oldRight, err := oldNode.ref(1)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary right child: %w", err)
		}

		nextRemaining := remaining - common - 1
		branch := int(newEffective.bitAt(common))
		w.setKeyBit(depth+common, byte(branch))
		if branch == 0 {
			if err = w.node(oldLeft, new, nextRemaining, 0, skipNew+common+1); err != nil {
				return err
			}
			w.setKeyBit(depth+common, 1)
			return w.node(oldRight, nil, nextRemaining, 0, 0)
		}
		w.setKeyBit(depth+common, 0)
		if err = w.node(oldLeft, nil, nextRemaining, 0, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(oldRight, new, nextRemaining, 0, skipNew+common+1)
	}

	if w.checkNew {
		if err = w.checker.fork(newNode, remaining-common, w.newAug); err != nil {
			return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
		}
	}
	newLeft, err := newNode.ref(0)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary left child: %w", err)
	}
	newRight, err := newNode.ref(1)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary right child: %w", err)
	}

	nextRemaining := remaining - common - 1
	branch := int(oldEffective.bitAt(common))
	if branch == 0 {
		w.setKeyBit(depth+common, 0)
		if err = w.node(old, newLeft, nextRemaining, skipOld+common+1, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(nil, newRight, nextRemaining, 0, 0)
	}
	w.setKeyBit(depth+common, 0)
	if err = w.node(nil, newLeft, nextRemaining, 0, 0); err != nil {
		return err
	}
	w.setKeyBit(depth+common, 1)
	return w.node(old, newRight, nextRemaining, skipOld+common+1, 0)
}

func (w *augDictDiffWalk) oneSide(old, new *Cell, remaining uint, oldOnly bool) error {
	branch := new
	if oldOnly {
		branch = old
	}
	node, err := parseAugDictDiffNode(branch, remaining)
	if err != nil {
		return err
	}

	depth := w.keySz - remaining
	label := node.labelSlice()
	w.storeLabel(&label, depth)
	if node.isLeaf(remaining) {
		if !oldOnly && w.checkNew {
			if err = w.checker.leaf(node, w.newAug); err != nil {
				return fmt.Errorf("invalid new dictionary leaf augmentation: %w", err)
			}
		}
		if oldOnly {
			return w.emit(node.value(), nil)
		}
		return w.emit(nil, node.value())
	}

	if !oldOnly && w.checkNew {
		if err = w.checker.fork(node, remaining-node.labelLen, w.newAug); err != nil {
			return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
		}
	}

	nextRemaining := remaining - node.labelLen - 1
	for child := 0; child < 2; child++ {
		w.setKeyBit(depth+node.labelLen, byte(child))
		ref, err := node.ref(child)
		if err != nil {
			return fmt.Errorf("failed to load dictionary child: %w", err)
		}
		if oldOnly {
			err = w.oneSide(ref, nil, nextRemaining, true)
		} else {
			err = w.oneSide(nil, ref, nextRemaining, false)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func parseAugDictDiffNode(branch *Cell, remaining uint) (fixedDictNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return fixedDictNode{}, err
	}
	if err = node.rejectSpecial("augmented dictionary"); err != nil {
		return fixedDictNode{}, err
	}
	if err = node.validateForkShape(remaining, true); err != nil {
		return fixedDictNode{}, err
	}
	return node, nil
}

type augmentedNodeChecker struct {
	computed Builder
	left     Slice
	right    Slice
	// scratch backs the fork-extra probes; see augmentedNodeExtraViewScratch.
	scratch Slice
	buf     [maxCellDataBytes]byte
}

func (c *augmentedNodeChecker) leaf(node fixedDictNode, aug Augmentation) error {
	stored := node.loader
	value := node.loader
	if err := aug.SkipExtra(&value); err != nil {
		return err
	}
	stored.bitEnd, stored.refEnd = value.bitStart, value.refStart

	c.computed = Builder{}
	if err := aug.LeafExtra(&value, &c.computed); err != nil {
		return err
	}
	if !c.computed.equalsSlice(&stored, &c.buf) {
		return fmt.Errorf("augmented dictionary leaf extra mismatch")
	}
	return nil
}

func (c *augmentedNodeChecker) fork(node fixedDictNode, remaining uint, aug Augmentation) error {
	left, err := node.ref(0)
	if err != nil {
		return err
	}
	right, err := node.ref(1)
	if err != nil {
		return err
	}
	childRemaining := remaining - 1
	c.left, err = extractAugmentedNodeExtraViewScratch(left, childRemaining, aug.SkipExtra, &c.scratch)
	if err != nil {
		return err
	}
	c.right, err = extractAugmentedNodeExtraViewScratch(right, childRemaining, aug.SkipExtra, &c.scratch)
	if err != nil {
		return err
	}

	stored := node.loader
	if err = stored.SkipBitsAndRefs(0, 2); err != nil {
		return err
	}
	c.computed = Builder{}
	if err = aug.CombineExtra(&c.left, &c.right, &c.computed); err != nil {
		return err
	}
	if !c.computed.equalsSlice(&stored, &c.buf) {
		return fmt.Errorf("augmented dictionary fork extra mismatch")
	}
	return nil
}

func (w *augDictDiffWalk) emit(oldValueExtra, newValueExtra *Slice) error {
	w.key.bitsSz = w.keySz
	return w.fn(w.key.EndCell(), oldValueExtra, newValueExtra)
}

func (w *augDictDiffWalk) keyMatchesSkippedLabel(label *Slice, depth, skip uint) bool {
	if skip == 0 {
		return true
	}
	start := depth - skip
	for bit := uint(0); bit < skip; bit++ {
		if w.keyBit(start+bit) != label.bitAt(bit) {
			return false
		}
	}
	return true
}

func (w *augDictDiffWalk) storeLabel(label *Slice, start uint) {
	for bit := uint(0); bit < label.BitsLeft(); bit++ {
		w.setKeyBit(start+bit, label.bitAt(bit))
	}
}

func (w *augDictDiffWalk) keyBit(bit uint) byte {
	return (w.key.data[bit/8] >> (7 - bit%8)) & 1
}

func (w *augDictDiffWalk) setKeyBit(bit uint, value byte) {
	mask := byte(1 << (7 - bit%8))
	if value != 0 {
		w.key.data[bit/8] |= mask
		return
	}
	w.key.data[bit/8] &^= mask
}

func equalSliceContents(a, b *Slice) bool {
	if a.BitsLeft() != b.BitsLeft() || a.RefsNum() != b.RefsNum() || !a.BitsEqual(b) {
		return false
	}
	for i := 0; i < a.RefsNum(); i++ {
		if a.boundaryRefCellAt(i).HashKey() != b.boundaryRefCellAt(i).HashKey() {
			return false
		}
	}
	return true
}
