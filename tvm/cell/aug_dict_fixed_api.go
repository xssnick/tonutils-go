package cell

import "fmt"

type AugDictItem struct {
	Key   *Cell
	Value *Slice
	Extra *Slice
}

// AugDictItemView is a borrowed decomposed iterator item. Its slices are valid
// only until the iterator's next Next or Reset call and must not be retained,
// nor read through RawCell: that hands out the iterator's scratch key cell.
// Key.ToCell and Key.BaseCell materialize an owned key that survives advance.
type AugDictItemView struct {
	Key   Slice
	Value Slice
	Extra Slice
}

type AugDictIterator struct {
	raw     *DictIterator
	dict    *AugmentedDictionary
	current AugDictItem
	view    AugDictItemView
	hasView bool
	err     error
}

type AugDictForeachFunc func(value, extra *Slice, key *Cell) (bool, error)
type AugDictBorrowedForeachFunc func(item AugDictItemView) error
type AugDictFilterFunc func(value, extra *Slice, key *Cell) (DictFilterAction, error)
type AugDictTraverseFunc func(keyPrefix *Cell, extra *Slice, value *Slice) (int, error)
type AugDictBorrowedTraverseFunc func(keyPrefix, extra, value *Slice) (int, error)

func newAugDictIterator(raw *DictIterator, dict *AugmentedDictionary) *AugDictIterator {
	return &AugDictIterator{raw: raw, dict: dict}
}

func (it *AugDictIterator) Next() bool {
	if it == nil || it.err != nil || it.raw == nil || !it.raw.Next() {
		if it != nil && it.raw != nil {
			it.err = it.raw.Err()
		}
		return false
	}
	it.current = AugDictItem{}
	it.hasView = false
	raw := it.raw.View()
	it.view = AugDictItemView{Key: raw.Key, Value: raw.Value, Extra: raw.Value}
	if err := it.dict.skipExtra(&it.view.Value); err != nil {
		it.err = err
		return false
	}
	it.view.Extra.bitEnd = it.view.Value.bitStart
	it.view.Extra.refEnd = it.view.Value.refStart
	it.hasView = true
	return true
}

func (it *AugDictIterator) Item() AugDictItem {
	if it == nil || !it.hasView {
		return AugDictItem{}
	}
	it.materializeKey()
	it.materializeValue()
	it.materializeExtra()
	return it.current
}

// View returns the current decomposed item without materializing owned key,
// value, or extra objects. The result is invalidated by the next Next or Reset
// call and must not be retained, nor read through RawCell: that hands out the
// iterator's scratch key cell. Key.ToCell and Key.BaseCell materialize an owned
// key that survives advance.
func (it *AugDictIterator) View() AugDictItemView {
	if it == nil || !it.hasView {
		return AugDictItemView{}
	}
	return it.view
}

func (it *AugDictIterator) Key() *Cell {
	if it == nil || !it.hasView {
		return nil
	}
	it.materializeKey()
	return it.current.Key
}

func (it *AugDictIterator) Value() *Slice {
	if it == nil || !it.hasView {
		return nil
	}
	it.materializeValue()
	return it.current.Value
}

func (it *AugDictIterator) Extra() *Slice {
	if it == nil || !it.hasView {
		return nil
	}
	it.materializeExtra()
	return it.current.Extra
}

func (it *AugDictIterator) materializeKey() {
	if it.current.Key == nil {
		it.current.Key = it.raw.Key()
	}
}

func (it *AugDictIterator) materializeValue() {
	if it.current.Value == nil {
		value := it.view.Value
		it.current.Value = &value
	}
}

func (it *AugDictIterator) materializeExtra() {
	if it.current.Extra == nil {
		extra := it.view.Extra
		it.current.Extra = &extra
	}
}

func (it *AugDictIterator) Reset() {
	if it == nil {
		return
	}
	it.current = AugDictItem{}
	it.view = AugDictItemView{}
	it.hasView = false
	it.err = nil
	if it.raw != nil {
		it.raw.Reset()
		it.err = it.raw.Err()
	}
}

func (it *AugDictIterator) Err() error {
	if it == nil {
		return nil
	}
	return it.err
}

func validateAugmentedDictionary(d *AugmentedDictionary) error {
	return validateAugmentedDictionaryWithTrace(d, false)
}

func validateAugmentedDictionaryWithTrace(d *AugmentedDictionary, preserveTrace bool) error {
	if d == nil {
		return nil
	}
	if !d.wrapped {
		return validateAugmentedDictRootWithTrace(d.root, d.keySz, d.aug, preserveTrace)
	}
	if err := validateAugmentedDictRootWithTrace(d.root, d.keySz, d.aug, preserveTrace); err != nil {
		return err
	}

	root, err := d.ToCell()
	if err != nil {
		return err
	}
	loader, err := root.BeginParse()
	if err != nil {
		return err
	}
	_, err = loader.LoadAugDict(d.keySz, d.aug, false)
	return err
}

// Range returns every leaf in key order with raw `extra ++ value` slices: the
// augmentation comes first and the caller must skip it. Use RangeExtra to get
// values and extras decomposed.
func (d *AugmentedDictionary) Range(rev bool, sgnd bool) ([]DictItem, error) {
	if d == nil {
		return []DictItem{}, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, rev, sgnd, dictWalk{lenient: true})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Count returns the number of dictionary leaves without materializing keys or values.
func (d *AugmentedDictionary) Count() (int, error) {
	if d == nil || d.root == nil {
		return 0, nil
	}
	return countFixedDictLeaves(d.root, d.keySz)
}

// ForEachValueExtra visits every leaf's decomposed value and extra in key
// order without materializing key cells — cheaper than IteratorExtra when
// keys are not needed. fn returns false to stop the walk early.
func (d *AugmentedDictionary) ForEachValueExtra(fn func(value, extra *Slice) (bool, error)) error {
	if d == nil || d.root == nil || fn == nil {
		return nil
	}
	root := d.root.withTraceCombined(d.trace)
	_, err := forEachDictLeafValue(root, d.keySz, func(leaf *Slice) (bool, error) {
		value, extra, err := d.decomposeValueExtra(leaf)
		if err != nil {
			return false, err
		}
		return fn(value, extra)
	})
	return err
}

// Iterator creates a lazy depth-first raw value+extra iterator. Inspect Err
// after Next returns false to catch failures discovered in deeper children.
func (d *AugmentedDictionary) Iterator(rev bool, sgnd bool) (*DictIterator, error) {
	if d == nil {
		return newDictIterator(nil, 0, rev, sgnd, dictWalk{})
	}
	return newDictIterator(d.root, d.keySz, rev, sgnd, dictWalk{trace: d.trace, lenient: true})
}

func (d *AugmentedDictionary) RangeExtra(rev bool, sgnd bool) ([]AugDictItem, error) {
	rawItems, err := d.Range(rev, sgnd)
	if err != nil {
		return nil, err
	}

	items := make([]AugDictItem, len(rawItems))
	for i, item := range rawItems {
		value, extra, err := d.decomposeValueExtra(item.Value)
		if err != nil {
			return nil, err
		}
		items[i] = AugDictItem{
			Key:   item.Key,
			Value: value,
			Extra: extra,
		}
	}
	return items, nil
}

// IteratorExtra creates a lazy depth-first decomposed iterator. Inspect Err
// after Next returns false to catch traversal or extra-decoding failures.
func (d *AugmentedDictionary) IteratorExtra(rev bool, sgnd bool) (*AugDictIterator, error) {
	raw, err := d.Iterator(rev, sgnd)
	if err != nil {
		return nil, err
	}
	return newAugDictIterator(raw, d), nil
}

// ForEachBorrowed visits every decomposed item in iterator order without
// materializing owned key, value, or extra objects per leaf. Each item is valid
// only for the duration of its callback; it must not be retained, nor read
// through RawCell: that hands out the iterator's scratch key cell.
func (d *AugmentedDictionary) ForEachBorrowed(rev bool, sgnd bool, fn AugDictBorrowedForeachFunc) error {
	if fn == nil {
		return nil
	}
	it, err := d.IteratorExtra(rev, sgnd)
	if err != nil {
		return err
	}
	for it.Next() {
		if err = fn(it.View()); err != nil {
			return err
		}
	}
	return it.Err()
}

// IteratorAt creates a lazy raw iterator positioned at the nearest key to
// `key` in iteration order: for rev=false the first item is the smallest key
// >= key (> key when allowEq is false), for rev=true the largest key <= key
// (< key). Reset rewinds to the full range, not to the seek position.
func (d *AugmentedDictionary) IteratorAt(key *Cell, rev bool, sgnd bool, allowEq bool) (*DictIterator, error) {
	if d == nil {
		return newDictIterator(nil, 0, rev, sgnd, dictWalk{})
	}
	return newDictIteratorAt(d.root, d.keySz, key, rev, sgnd, allowEq, dictWalk{trace: d.trace, lenient: true})
}

// IteratorExtraAt is IteratorAt with values decomposed into value and extra.
func (d *AugmentedDictionary) IteratorExtraAt(key *Cell, rev bool, sgnd bool, allowEq bool) (*AugDictIterator, error) {
	raw, err := d.IteratorAt(key, rev, sgnd, allowEq)
	if err != nil {
		return nil, err
	}
	return newAugDictIterator(raw, d), nil
}

// LookupNearestKey returns the raw leaf slice, which for an augmented
// dictionary is `extra ++ value`: per TL-B `ahmn_leaf#_ {X:Type} {Y:Type}
// extra:Y value:X` the augmentation comes FIRST and the caller must skip it.
// Use LookupNearestKeyExtra to get the value and the extra decomposed.
func (d *AugmentedDictionary) LookupNearestKey(key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}

	return fixedDictLookupNearest(d.root, d.keySz, key, fetchNext, allowEq, invertFirst, dictWalk{lenient: true})
}

// LookupNearestKeyExtra is LookupNearestKey with the leaf decomposed into the
// value and its augmentation.
func (d *AugmentedDictionary) LookupNearestKeyExtra(key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, *Slice, error) {
	foundKey, leaf, err := d.LookupNearestKey(key, fetchNext, allowEq, invertFirst)
	if err != nil {
		return nil, nil, nil, err
	}
	value, extra, err := d.decomposeValueExtra(leaf)
	if err != nil {
		return nil, nil, nil, err
	}
	return foundKey, value, extra, nil
}

func (d *AugmentedDictionary) HasCommonPrefix(prefix *Cell) (bool, error) {
	if d == nil {
		return true, nil
	}
	return fixedDictHasCommonPrefix(d.root, d.keySz, prefix, dictWalk{lenient: true})
}

func (d *AugmentedDictionary) GetCommonPrefix(limit ...uint) (*Cell, error) {
	if d == nil {
		return BeginCell().EndCell(), nil
	}
	maxLen := d.keySz
	if len(limit) > 0 && limit[0] < maxLen {
		maxLen = limit[0]
	}
	return fixedDictCommonPrefix(d.root, d.keySz, maxLen, dictWalk{lenient: true})
}

func (d *AugmentedDictionary) ExtractPrefixSubdictRoot(prefix *Cell, removePrefix bool) (*Cell, error) {
	if d == nil {
		return nil, nil
	}
	root, changed, err := extractPrefixSubdictRoot(d.root, d.keySz, prefix, removePrefix, dictWalk{trace: d.trace, lenient: true})
	if err != nil {
		return nil, err
	}
	if !changed {
		return d.root, nil
	}
	return root, nil
}

func (d *AugmentedDictionary) CutPrefixSubdict(prefix *Cell, removePrefix bool) (bool, error) {
	if d == nil {
		return true, nil
	}
	if prefix != nil && prefix.BitsSize() > d.keySz && removePrefix {
		return false, nil
	}

	root, changed, err := extractPrefixSubdictRoot(d.root, d.keySz, prefix, removePrefix, dictWalk{trace: d.trace, lenient: true})
	if err != nil {
		return false, err
	}
	if removePrefix && prefix != nil && prefix.BitsSize() <= d.keySz {
		d.keySz -= prefix.BitsSize()
	}
	if changed {
		if err = d.setRootWithExtra(root, nil); err != nil {
			return false, err
		}
	}
	return true, nil
}

// CheckForEach visits every leaf with a raw `extra ++ value` slice: the
// augmentation comes first and fn must skip it. Use CheckForEachExtra to
// receive the value and the extra decomposed.
func (d *AugmentedDictionary) CheckForEach(fn DictForeachFunc, invertFirst bool, shuffle bool) (bool, error) {
	if d == nil {
		return true, nil
	}
	if fn == nil {
		return true, nil
	}
	if !shuffle {
		it, err := d.Iterator(false, invertFirst)
		if err != nil {
			return false, err
		}
		for it.Next() {
			item := it.Item()
			ok, err := fn(item.Value, item.Key)
			if err != nil || !ok {
				return ok, err
			}
		}
		if err = it.Err(); err != nil {
			return false, err
		}
		return true, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, false, invertFirst, dictWalk{lenient: true})
	if err != nil {
		return false, err
	}
	return fixedDictCheckForEach(items, fn, shuffle)
}

// ValidateCheck visits every leaf with a raw `extra ++ value` slice, like
// CheckForEach. Use ValidateCheckExtra for decomposed values.
func (d *AugmentedDictionary) ValidateCheck(fn DictForeachFunc, invertFirst bool) (bool, error) {
	if err := validateAugmentedDictionary(d); err != nil {
		return false, err
	}
	return d.CheckForEach(fn, invertFirst, false)
}

func (d *AugmentedDictionary) ValidateAll() bool {
	return validateAugmentedDictionary(d) == nil
}

// Validate verifies the HashmapAugE wrapper and the root node extra while
// preserving attached usage traces. This matches the reference VM's
// AugmentedDictionary::validate: descendants are checked only by ValidateAll.
func (d *AugmentedDictionary) Validate() error {
	if d == nil {
		return nil
	}
	if err := validateDictKeySize(d.keySz); err != nil {
		return err
	}
	if d.aug == nil {
		return fmt.Errorf("augmentation is nil")
	}

	if d.root == nil {
		if !d.wrapped {
			return nil
		}
		if d.rootExtra == nil {
			return fmt.Errorf("augmented dict empty extra is absent")
		}

		var expected Builder
		if err := d.aug.EmptyExtra(&expected); err != nil {
			return err
		}
		stored, err := d.rootExtra.BeginParse()
		if err != nil {
			return err
		}
		var buf [maxCellDataBytes]byte
		if !expected.equalsSlice(stored, &buf) {
			return fmt.Errorf("augmented dict empty extra mismatch")
		}
		return nil
	}

	node, err := parseFixedDictNodeWithTrace(d.root, d.keySz, d.root.Trace())
	if err != nil {
		return fmt.Errorf("failed to load augmented dict root: %w", err)
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return err
	}
	var after Slice
	rootExtra, err := augmentedNodeExtraViewScratch(node, d.keySz, d.aug.SkipExtra, &after)
	if err != nil {
		return err
	}
	if !node.isLeaf(d.keySz) && (after.BitsLeft() != 0 || after.RefsNum() != 0) {
		return fmt.Errorf("invalid augmented dict root fork")
	}
	if !d.wrapped {
		return nil
	}
	if d.rootExtra == nil {
		return fmt.Errorf("augmented dict root extra is absent")
	}
	stored, err := d.rootExtra.BeginParse()
	if err != nil {
		return err
	}
	if !equalSliceContents(&rootExtra, stored) {
		return fmt.Errorf("augmented dict root extra mismatch")
	}
	return nil
}

func (d *AugmentedDictionary) CheckForEachExtra(fn AugDictForeachFunc, invertFirst bool) (bool, error) {
	if d == nil || fn == nil {
		return true, nil
	}
	it, err := d.IteratorExtra(false, invertFirst)
	if err != nil {
		return false, err
	}
	for it.Next() {
		item := it.Item()
		ok, err := fn(item.Value, item.Extra, item.Key)
		if err != nil || !ok {
			return ok, err
		}
	}
	if err = it.Err(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *AugmentedDictionary) ValidateCheckExtra(fn AugDictForeachFunc, invertFirst bool) (bool, error) {
	if err := validateAugmentedDictionary(d); err != nil {
		return false, err
	}
	return d.CheckForEachExtra(fn, invertFirst)
}

func (d *AugmentedDictionary) TraverseExtra(fn AugDictTraverseFunc) (*Slice, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, nil
	}
	if fn == nil {
		return nil, nil, nil
	}
	return d.traverseExtraNode(d.root, d.keySz, BeginCell(), fn)
}

// TraverseExtraBorrowed walks the same depth-first search as TraverseExtra
// without materializing a key cell, extra cell, or traced child cell at every
// visited node. keyPrefix, extra, and value are synchronous borrowed views:
// they may be parsed during fn but must not be retained after it returns. A
// retained key must be materialized through keyPrefix.ToCell or BaseCell;
// RawCell exposes traversal scratch and must not be used.
//
// Fork callbacks receive a nil value and accept the same directives as
// TraverseExtra: 0 stops the subtree, 1/2 descend left/right, and 5/6 visit
// right-first/left-first. At a leaf, a positive result stops the whole walk.
func (d *AugmentedDictionary) TraverseExtraBorrowed(fn AugDictBorrowedTraverseFunc) error {
	if d == nil || d.root == nil || fn == nil {
		return nil
	}

	state := augmentedDictBorrowedTraverseState{
		dict: d,
		fn:   fn,
	}
	_, err := traverseAugmentedDictBorrowedNode(
		d.root,
		CombineTraces(d.root.Trace(), d.trace),
		d.keySz,
		&state,
	)
	return err
}

type augmentedDictBorrowedTraverseState struct {
	dict *AugmentedDictionary
	fn   AugDictBorrowedTraverseFunc

	prefix  Builder
	key     Slice
	keyCell Cell
	extra   Slice
	value   Slice

	// skipScratch is the sole destination whose address reaches the opaque
	// augmentation skipper. Keeping it in traversal state prevents one escape
	// for every fork visited by the walk.
	skipScratch Slice
}

func traverseAugmentedDictBorrowedNode(branch *Cell, trace *Trace, remaining uint, state *augmentedDictBorrowedTraverseState) (bool, error) {
	if branch == nil {
		return false, nil
	}

	node, err := parseFixedDictNodeWithTrace(branch, remaining, trace)
	if err != nil {
		return false, err
	}
	if activeTrace := node.loader.Trace(); activeTrace != nil {
		if err = activeTrace.PendingError(); err != nil {
			return false, err
		}
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return false, err
	}

	savedPrefix := state.prefix.BitsUsed()
	label := node.labelSlice()
	if err = state.prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return false, err
	}
	defer state.prefix.truncateBits(savedPrefix)

	if node.isLeaf(remaining) {
		state.value = node.loader
		state.extra = node.loader
		if err = state.dict.skipExtra(&state.value); err != nil {
			return false, err
		}
		state.extra.bitEnd = state.value.bitStart
		state.extra.refEnd = state.value.refStart

		directive, err := state.fn(state.keyView(), &state.extra, &state.value)
		if err != nil {
			return false, err
		}
		return directive > 0, nil
	}

	left, leftTrace, err := node.refAndTrace(0)
	if err != nil {
		return false, err
	}
	right, rightTrace, err := node.refAndTrace(1)
	if err != nil {
		return false, err
	}
	state.extra, err = augmentedNodeExtraViewScratch(node, remaining, state.dict.aug.SkipExtra, &state.skipScratch)
	if err != nil {
		return false, err
	}

	directive, err := state.fn(state.keyView(), &state.extra, nil)
	if err != nil {
		return false, err
	}
	if directive < 0 || directive&3 == 3 {
		return false, fmt.Errorf("invalid traverse directive")
	}
	if directive&3 == 0 {
		return false, nil
	}

	afterLabel := state.prefix.BitsUsed()
	childRemaining := node.nextKeyBits(remaining)

	switch directive {
	case 1:
		return traverseAugmentedDictBorrowedChild(left, leftTrace, childRemaining, afterLabel, 0, state)
	case 2:
		return traverseAugmentedDictBorrowedChild(right, rightTrace, childRemaining, afterLabel, 1, state)
	case 5:
		found, err := traverseAugmentedDictBorrowedChild(right, rightTrace, childRemaining, afterLabel, 1, state)
		if err != nil || found {
			return found, err
		}
		return traverseAugmentedDictBorrowedChild(left, leftTrace, childRemaining, afterLabel, 0, state)
	case 6:
		found, err := traverseAugmentedDictBorrowedChild(left, leftTrace, childRemaining, afterLabel, 0, state)
		if err != nil || found {
			return found, err
		}
		return traverseAugmentedDictBorrowedChild(right, rightTrace, childRemaining, afterLabel, 1, state)
	default:
		return false, fmt.Errorf("invalid traverse directive")
	}
}

func traverseAugmentedDictBorrowedChild(child *Cell, trace *Trace, remaining, prefixBits uint, bit uint64, state *augmentedDictBorrowedTraverseState) (bool, error) {
	state.prefix.truncateBits(prefixBits)
	if err := state.prefix.StoreUInt(bit, 1); err != nil {
		return false, err
	}
	return traverseAugmentedDictBorrowedNode(child, trace, remaining, state)
}

func (s *augmentedDictBorrowedTraverseState) keyView() *Slice {
	s.keyCell = Cell{
		data:   s.prefix.data[:s.prefix.usedBytes()],
		bitsSz: uint16(s.prefix.bitsSz),
	}
	s.key = Slice{
		cell:              &s.keyCell,
		bitEnd:            s.keyCell.bitsSz,
		forceCopyOnToCell: true,
	}
	return &s.key
}

func (d *AugmentedDictionary) traverseExtraNode(branch *Cell, remaining uint, prefix *Builder, fn AugDictTraverseFunc) (*Slice, *Slice, error) {
	if branch == nil {
		return nil, nil, nil
	}

	loader, err := branch.BeginParse()
	if err != nil {
		return nil, nil, err
	}
	if trace := loader.Trace(); trace != nil {
		if err = trace.PendingError(); err != nil {
			return nil, nil, err
		}
	}
	if loader.cell.IsSpecial() {
		return nil, nil, fmt.Errorf("augmented dict %w", ErrDictHasSpecialCells)
	}
	labelLen, prefixKey, err := loadLabel(remaining, loader, prefix)
	if err != nil {
		return nil, nil, err
	}

	if labelLen == remaining {
		value, extra, err := d.decomposeValueExtra(loader)
		if err != nil {
			return nil, nil, err
		}
		r, err := fn(prefixKey.EndCell(), cloneSlice(extra), cloneSlice(value))
		if err != nil {
			return nil, nil, err
		}
		if r > 0 {
			return value, extra, nil
		}
		return nil, nil, nil
	}

	left, err := loader.LoadRefCell()
	if err != nil {
		return nil, nil, err
	}
	right, err := loader.LoadRefCell()
	if err != nil {
		return nil, nil, err
	}
	extra, err := captureConsumedPrefix(loader, d.aug.SkipExtra)
	if err != nil {
		return nil, nil, err
	}

	extraSlice, err := extra.BeginParse()
	if err != nil {
		return nil, nil, err
	}

	r, err := fn(prefixKey.EndCell(), extraSlice, nil)
	if err != nil {
		return nil, nil, err
	}
	if r < 0 || (r&3) == 3 {
		return nil, nil, fmt.Errorf("invalid traverse directive")
	}
	if (r & 3) == 0 {
		return nil, nil, nil
	}

	nextRemaining := remaining - labelLen - 1
	switch r {
	case 1:
		return d.traverseExtraNode(left, nextRemaining, prefixKey.Copy().MustStoreUInt(0, 1), fn)
	case 2:
		return d.traverseExtraNode(right, nextRemaining, prefixKey.Copy().MustStoreUInt(1, 1), fn)
	case 5:
		value, extra, err := d.traverseExtraNode(right, nextRemaining, prefixKey.Copy().MustStoreUInt(1, 1), fn)
		if err != nil || value != nil {
			return value, extra, err
		}
		return d.traverseExtraNode(left, nextRemaining, prefixKey.Copy().MustStoreUInt(0, 1), fn)
	case 6:
		value, extra, err := d.traverseExtraNode(left, nextRemaining, prefixKey.Copy().MustStoreUInt(0, 1), fn)
		if err != nil || value != nil {
			return value, extra, err
		}
		return d.traverseExtraNode(right, nextRemaining, prefixKey.Copy().MustStoreUInt(1, 1), fn)
	default:
		return nil, nil, fmt.Errorf("invalid traverse directive")
	}
}

func (d *AugmentedDictionary) Filter(fn AugDictFilterFunc) (int, error) {
	if d == nil || d.root == nil {
		return 0, nil
	}
	if fn == nil {
		return 0, nil
	}
	if err := d.ensureWritable(); err != nil {
		return 0, err
	}

	state := augmentedDictFilterState{dict: d, fn: fn}
	result, err := filterAugmentedDictNode(d.root, d.keySz, &state)
	if err != nil {
		return 0, err
	}
	if state.changes == 0 {
		return 0, nil
	}

	var rootExtra *Cell
	if result.root != nil {
		rootExtra, err = result.extra.ToCell()
		if err != nil {
			return 0, err
		}
	}
	if err = d.setRootWithExtra(result.root, rootExtra); err != nil {
		return 0, err
	}
	return state.changes, nil
}

type augmentedDictFilterState struct {
	dict       *AugmentedDictionary
	fn         AugDictFilterFunc
	prefix     Builder
	mutation   augmentedMutationState
	changes    int
	keepRest   bool
	removeRest bool
}

type augmentedDictFilterResult struct {
	root    *Cell
	extra   Slice
	changed bool
}

func filterAugmentedDictNode(root *Cell, remaining uint, state *augmentedDictFilterState) (augmentedDictFilterResult, error) {
	if state.removeRest {
		count, err := countFixedDictLeaves(root, remaining)
		if err != nil {
			return augmentedDictFilterResult{}, err
		}
		state.changes += count
		return augmentedDictFilterResult{changed: count != 0}, nil
	}

	node, err := parseFixedDictNode(root, remaining)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return augmentedDictFilterResult{}, err
	}
	nodeExtra, err := augmentedNodeExtraView(node, remaining, state.dict.aug.SkipExtra)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}
	if state.keepRest {
		return augmentedDictFilterResult{root: root, extra: nodeExtra}, nil
	}

	saved := state.prefix.BitsUsed()
	defer state.prefix.truncateBits(saved)
	label := node.labelSlice()
	if err = state.prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return augmentedDictFilterResult{}, err
	}

	if node.isLeaf(remaining) {
		value, extra, err := state.dict.decomposeValueExtra(node.value())
		if err != nil {
			return augmentedDictFilterResult{}, err
		}
		action, err := state.fn(value, extra, state.prefix.EndCell())
		if err != nil {
			return augmentedDictFilterResult{}, err
		}
		switch action {
		case DictFilterKeep:
			return augmentedDictFilterResult{root: root, extra: nodeExtra}, nil
		case DictFilterRemove:
			state.changes++
			return augmentedDictFilterResult{changed: true}, nil
		case DictFilterKeepRest:
			state.keepRest = true
			return augmentedDictFilterResult{root: root, extra: nodeExtra}, nil
		case DictFilterRemoveRest:
			state.removeRest = true
			state.changes++
			return augmentedDictFilterResult{changed: true}, nil
		default:
			return augmentedDictFilterResult{}, fmt.Errorf("unknown dict filter action")
		}
	}

	afterLabel := state.prefix.BitsUsed()
	childRemaining := node.nextKeyBits(remaining)
	left, err := node.ref(0)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}
	if err = state.prefix.StoreUInt(0, 1); err != nil {
		return augmentedDictFilterResult{}, err
	}
	newLeft, err := filterAugmentedDictNode(left, childRemaining, state)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}

	state.prefix.truncateBits(afterLabel)
	right, err := node.ref(1)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}
	if err = state.prefix.StoreUInt(1, 1); err != nil {
		return augmentedDictFilterResult{}, err
	}
	newRight, err := filterAugmentedDictNode(right, childRemaining, state)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}

	if !newLeft.changed && !newRight.changed {
		return augmentedDictFilterResult{root: root, extra: nodeExtra}, nil
	}
	if newLeft.root == nil && newRight.root == nil {
		return augmentedDictFilterResult{changed: true}, nil
	}
	if newLeft.root == nil {
		merged, err := mergeFixedDictSurvivor(node, 1, newRight.root, remaining, state.dict.trace)
		return augmentedDictFilterResult{root: merged, extra: newRight.extra, changed: true}, err
	}
	if newRight.root == nil {
		merged, err := mergeFixedDictSurvivor(node, 0, newLeft.root, remaining, state.dict.trace)
		return augmentedDictFilterResult{root: merged, extra: newLeft.extra, changed: true}, err
	}

	parentLabel := node.labelSlice()
	rebuilt, extra, err := state.dict.storeForkWithExtraSlices(&parentLabel, newLeft.root, &newLeft.extra, newRight.root, &newRight.extra, remaining, &state.mutation)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}
	return augmentedDictFilterResult{root: rebuilt, extra: extra, changed: true}, nil
}
