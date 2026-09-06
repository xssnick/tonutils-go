package cell

import "fmt"

// DictDiffFunc receives changed leaves in key order. A nil oldValue or newValue
// means that the key is absent from that side of the diff.
type DictDiffFunc func(key *Cell, oldValue, newValue *Slice) error

// DictDiffView is one changed dictionary leaf. Key, OldValue and NewValue are
// borrowed Slice values backed by the walk's current cells. They are valid only
// until the callback returns; call ToCell or copy the data to retain it.
type DictDiffView struct {
	Key      Slice
	OldValue Slice
	NewValue Slice
	HasOld   bool
	HasNew   bool
}

// DictDiffViewFunc receives borrowed changed-leaf views.
type DictDiffViewFunc func(DictDiffView) error

// DictDiffRawView is one changed dictionary leaf with its key exposed as
// borrowed packed bits. Key, OldValue and NewValue are read-only, valid only
// until the callback returns, and must not be retained; the first KeyBits bits
// of Key are significant.
type DictDiffRawView struct {
	Key      []byte
	KeyBits  uint
	OldValue Slice
	NewValue Slice
	HasOld   bool
	HasNew   bool
}

// DictDiffRawViewFunc receives a borrowed raw-key diff view.
type DictDiffRawViewFunc func(DictDiffRawView) error

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
	return d.ScanDiffBorrowed(other, func(view DictDiffView) error {
		key, err := view.Key.ToCell()
		if err != nil {
			return err
		}

		var oldValue, newValue *Slice
		if view.HasOld {
			oldValue = &view.OldValue
		}
		if view.HasNew {
			newValue = &view.NewValue
		}
		return fn(key, oldValue, newValue)
	})
}

// ScanDiffBorrowed is ScanDiff without materializing a key Cell for every
// changed leaf. Every Slice in the view is borrowed and must not be retained
// after the callback returns.
func (d *Dictionary) ScanDiffBorrowed(other *Dictionary, fn DictDiffViewFunc) error {
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
	var oldTrace, newTrace *Trace
	if d != nil {
		oldRoot = d.root
		oldTrace = combinedCellTrace(d.root, d.trace)
	}
	if other != nil {
		newRoot = other.root
		newTrace = combinedCellTrace(other.root, other.trace)
	}

	walk := dictDiffWalk{keySz: keySz, fn: fn}
	if err := walk.node(oldRoot, oldTrace, newRoot, newTrace, keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan dictionary diff: %w", err)
	}
	return nil
}

// ScanDiffRaw is ScanDiffBorrowed without materializing or hashing a key Cell.
// The callback receives the key as borrowed packed bits.
func (d *Dictionary) ScanDiffRaw(other *Dictionary, fn DictDiffRawViewFunc) error {
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
	var oldTrace, newTrace *Trace
	if d != nil {
		oldRoot = d.root
		oldTrace = combinedCellTrace(d.root, d.trace)
	}
	if other != nil {
		newRoot = other.root
		newTrace = combinedCellTrace(other.root, other.trace)
	}

	walk := dictDiffWalk{keySz: keySz, rawFn: fn}
	if err := walk.node(oldRoot, oldTrace, newRoot, newTrace, keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan dictionary diff: %w", err)
	}
	return nil
}

type dictDiffWalk struct {
	keySz   uint
	key     Builder
	keyCell Cell
	fn      DictDiffViewFunc
	rawFn   DictDiffRawViewFunc
}

// node compares one aligned pair of subtrees. skipOld and skipNew are the label
// bits already consumed on that side by an ancestor whose label was longer, so
// the two sides stay aligned on the same key prefix without materializing
// intermediate nodes — the same device C++ dict_scan_diff uses.
func (w *dictDiffWalk) node(old *Cell, oldTrace *Trace, new *Cell, newTrace *Trace, remaining, skipOld, skipNew uint) error {
	if old == nil {
		if new == nil {
			return nil
		}
		if skipNew != 0 {
			return fmt.Errorf("invalid new dictionary alignment")
		}
		return w.oneSide(new, newTrace, remaining, false)
	}
	if new == nil {
		if skipOld != 0 {
			return fmt.Errorf("invalid old dictionary alignment")
		}
		return w.oneSide(old, oldTrace, remaining, true)
	}
	// Skip equality matters: two logically identical subtrees reached with
	// different label skips are different cells, so comparing them by hash
	// would skip a real difference.
	if skipOld == skipNew && (old == new || old.HashKey() == new.HashKey()) {
		return nil
	}

	oldNode, err := parseDictDiffNode(old, oldTrace, remaining+skipOld)
	if err != nil {
		return fmt.Errorf("invalid old dictionary node: %w", err)
	}
	newNode, err := parseDictDiffNode(new, newTrace, remaining+skipNew)
	if err != nil {
		return fmt.Errorf("invalid new dictionary node: %w", err)
	}
	if oldNode.labelLen < skipOld || newNode.labelLen < skipNew {
		return fmt.Errorf("invalid dictionary diff alignment")
	}

	// Alignment may revisit this root while consuming its label. Keep the
	// validated resident cell, with the path trace still carried separately,
	// so those logical loads do not resolve the same lazy boundary again.
	old, new = oldNode.cell, newNode.cell

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
			if err = w.node(old, oldTrace, nil, nil, remaining+skipOld, 0, 0); err != nil {
				return err
			}
			return w.node(nil, nil, new, newTrace, remaining+skipNew, 0, 0)
		}
		if err = w.node(nil, nil, new, newTrace, remaining+skipNew, 0, 0); err != nil {
			return err
		}
		return w.node(old, oldTrace, nil, nil, remaining+skipOld, 0, 0)
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
			oldChild, oldChildTrace, err := oldNode.refAndTrace(child)
			if err != nil {
				return fmt.Errorf("failed to load old dictionary child: %w", err)
			}
			newChild, newChildTrace, err := newNode.refAndTrace(child)
			if err != nil {
				return fmt.Errorf("failed to load new dictionary child: %w", err)
			}
			if err = w.node(oldChild, oldChildTrace, newChild, newChildTrace, nextRemaining, 0, 0); err != nil {
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
		oldLeft, oldLeftTrace, err := oldNode.refAndTrace(0)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary left child: %w", err)
		}
		oldRight, oldRightTrace, err := oldNode.refAndTrace(1)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary right child: %w", err)
		}

		nextRemaining := remaining - common - 1
		branch := int(newEffective.bitAt(common))
		w.setKeyBit(depth+common, byte(branch))
		if branch == 0 {
			if err = w.node(oldLeft, oldLeftTrace, new, newTrace, nextRemaining, 0, skipNew+common+1); err != nil {
				return err
			}
			w.setKeyBit(depth+common, 1)
			return w.node(oldRight, oldRightTrace, nil, nil, nextRemaining, 0, 0)
		}
		w.setKeyBit(depth+common, 0)
		if err = w.node(oldLeft, oldLeftTrace, nil, nil, nextRemaining, 0, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(oldRight, oldRightTrace, new, newTrace, nextRemaining, 0, skipNew+common+1)
	}

	newLeft, newLeftTrace, err := newNode.refAndTrace(0)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary left child: %w", err)
	}
	newRight, newRightTrace, err := newNode.refAndTrace(1)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary right child: %w", err)
	}

	nextRemaining := remaining - common - 1
	branch := int(oldEffective.bitAt(common))
	if branch == 0 {
		w.setKeyBit(depth+common, 0)
		if err = w.node(old, oldTrace, newLeft, newLeftTrace, nextRemaining, skipOld+common+1, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(nil, nil, newRight, newRightTrace, nextRemaining, 0, 0)
	}
	w.setKeyBit(depth+common, 0)
	if err = w.node(nil, nil, newLeft, newLeftTrace, nextRemaining, 0, 0); err != nil {
		return err
	}
	w.setKeyBit(depth+common, 1)
	return w.node(old, oldTrace, newRight, newRightTrace, nextRemaining, skipOld+common+1, 0)
}

// oneSide reports every leaf of a subtree that exists on only one side.
func (w *dictDiffWalk) oneSide(branch *Cell, trace *Trace, remaining uint, oldOnly bool) error {
	node, err := parseDictDiffNode(branch, trace, remaining)
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
		ref, childTrace, err := node.refAndTrace(child)
		if err != nil {
			return fmt.Errorf("failed to load dictionary child: %w", err)
		}
		if err = w.oneSide(ref, childTrace, nextRemaining, oldOnly); err != nil {
			return err
		}
	}
	return nil
}

func parseDictDiffNode(branch *Cell, trace *Trace, remaining uint) (fixedDictNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, trace)
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
	if w.rawFn != nil {
		view := DictDiffRawView{
			Key:     w.key.data[:w.key.usedBytes()],
			KeyBits: w.keySz,
			HasOld:  oldValue != nil,
			HasNew:  newValue != nil,
		}
		if oldValue != nil {
			view.OldValue = *oldValue
		}
		if newValue != nil {
			view.NewValue = *newValue
		}
		return w.rawFn(view)
	}
	w.keyCell = Cell{
		data:   w.key.data[:w.key.usedBytes()],
		bitsSz: uint16(w.keySz),
	}
	if err := w.keyCell.calculateHashesOrdinary(); err != nil {
		return err
	}
	view := DictDiffView{
		Key: Slice{
			cell:              &w.keyCell,
			bitEnd:            uint16(w.keySz),
			forceCopyOnToCell: true,
		},
		HasOld: oldValue != nil,
		HasNew: newValue != nil,
	}
	if oldValue != nil {
		view.OldValue = *oldValue
	}
	if newValue != nil {
		view.NewValue = *newValue
	}
	return w.fn(view)
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
