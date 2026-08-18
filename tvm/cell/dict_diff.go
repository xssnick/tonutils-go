package cell

import "fmt"

// DictDiffFunc receives changed leaves in key order. A nil oldValue or newValue
// means that the key is absent from that side of the diff.
type DictDiffFunc func(key *Cell, oldValue, newValue *Slice) error

// ScanDiff compares two Hashmap tries structurally and visits their changed
// leaves in key order. Equal subtrees are skipped by hash, so the walk costs
// O(difference) rather than O(dictionary), and leaves whose values are byte
// identical are not reported even when the two tries reached them by different
// Patricia paths.
//
// It is the plain-dictionary counterpart of AugmentedDictionary.ScanDiff and
// mirrors vm::DictionaryFixed::scan_diff, which is what the reference validator
// uses to check a dictionary update without materializing either side.
//
// Both dictionaries are walked through their own traces. Special cells other
// than a shared subtree skipped by hash fail the walk: a pruned boundary facing
// a real subtree cannot be diffed.
func (d *Dictionary) ScanDiff(other *Dictionary, fn DictDiffFunc) error {
	if fn == nil {
		return fmt.Errorf("dictionary diff callback is required")
	}
	keySz := d.GetKeySize()
	if keySz != other.GetKeySize() {
		return fmt.Errorf("cannot compare dictionaries with different key sizes")
	}
	if err := validateDictKeySize(keySz); err != nil {
		return err
	}

	var oldRoot, newRoot *Cell
	if d != nil {
		oldRoot = d.tracedRoot()
	}
	if other != nil {
		newRoot = other.tracedRoot()
	}

	walk := dictDiffWalk{keySz: keySz, fn: fn}
	if err := walk.node(oldRoot, newRoot, keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan dictionary diff: %w", err)
	}
	return nil
}

type dictDiffWalk struct {
	keySz uint
	key   Builder
	fn    DictDiffFunc
}

// node compares one aligned pair of subtrees. skipOld and skipNew are the label
// bits already consumed on that side by an ancestor whose label was longer, so
// the two sides stay aligned on the same key prefix without materializing
// intermediate nodes — the same device C++ dict_scan_diff uses.
func (w *dictDiffWalk) node(old, new *Cell, remaining, skipOld, skipNew uint) error {
	if old == nil {
		if new == nil {
			return nil
		}
		if skipNew != 0 {
			return fmt.Errorf("invalid new dictionary alignment")
		}
		return w.oneSide(new, remaining, false)
	}
	if new == nil {
		if skipOld != 0 {
			return fmt.Errorf("invalid old dictionary alignment")
		}
		return w.oneSide(old, remaining, true)
	}
	// Skip equality matters: two logically identical subtrees reached with
	// different label skips are different cells, so comparing them by hash
	// would skip a real difference.
	if skipOld == skipNew && (old == new || old.HashKey() == new.HashKey()) {
		return nil
	}

	oldNode, err := parseDictDiffNode(old, remaining+skipOld)
	if err != nil {
		return fmt.Errorf("invalid old dictionary node: %w", err)
	}
	newNode, err := parseDictDiffNode(new, remaining+skipNew)
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
		// Disjoint key ranges: the two sides part company inside their own
		// labels, so neither shares a single key with the other. Emit them in
		// key order.
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
			if equalSliceContents(oldValue, newValue) {
				return nil
			}
			return w.emit(oldValue, newValue)
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
		// The old side forks where the new side is still inside its label, so
		// the whole new subtree lives under one old child; the sibling is an
		// old-only subtree.
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

// oneSide reports every leaf of a subtree that exists on only one side.
func (w *dictDiffWalk) oneSide(branch *Cell, remaining uint, oldOnly bool) error {
	node, err := parseDictDiffNode(branch, remaining)
	if err != nil {
		return err
	}

	depth := w.keySz - remaining
	label := node.labelSlice()
	w.storeLabel(&label, depth)
	if node.isLeaf(remaining) {
		if oldOnly {
			return w.emit(node.value(), nil)
		}
		return w.emit(nil, node.value())
	}

	nextRemaining := remaining - node.labelLen - 1
	for child := 0; child < 2; child++ {
		w.setKeyBit(depth+node.labelLen, byte(child))
		ref, err := node.ref(child)
		if err != nil {
			return fmt.Errorf("failed to load dictionary child: %w", err)
		}
		if err = w.oneSide(ref, nextRemaining, oldOnly); err != nil {
			return err
		}
	}
	return nil
}

func parseDictDiffNode(branch *Cell, remaining uint) (fixedDictNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return fixedDictNode{}, err
	}
	if err = node.rejectSpecial("dictionary"); err != nil {
		return fixedDictNode{}, err
	}
	if err = node.validateForkShape(remaining, false); err != nil {
		return fixedDictNode{}, err
	}
	return node, nil
}

func (w *dictDiffWalk) emit(oldValue, newValue *Slice) error {
	w.key.bitsSz = w.keySz
	return w.fn(w.key.EndCell(), oldValue, newValue)
}

func (w *dictDiffWalk) keyMatchesSkippedLabel(label *Slice, depth, skip uint) bool {
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

func (w *dictDiffWalk) storeLabel(label *Slice, start uint) {
	for bit := uint(0); bit < label.BitsLeft(); bit++ {
		w.setKeyBit(start+bit, label.bitAt(bit))
	}
}

func (w *dictDiffWalk) keyBit(bit uint) byte {
	return (w.key.data[bit/8] >> (7 - bit%8)) & 1
}

func (w *dictDiffWalk) setKeyBit(bit uint, value byte) {
	mask := byte(1 << (7 - bit%8))
	if value != 0 {
		w.key.data[bit/8] |= mask
		return
	}
	w.key.data[bit/8] &^= mask
}
