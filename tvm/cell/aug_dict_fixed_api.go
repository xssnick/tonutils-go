package cell

import "fmt"

type AugDictItem struct {
	Key   *Cell
	Value *Slice
	Extra *Slice
}

type AugDictIterator struct {
	raw     *DictIterator
	dict    *AugmentedDictionary
	current AugDictItem
	err     error
}

type AugDictForeachFunc func(value, extra *Slice, key *Cell) (bool, error)
type AugDictFilterFunc func(value, extra *Slice, key *Cell) (DictFilterAction, error)
type AugDictTraverseFunc func(keyPrefix *Cell, extra *Slice, value *Slice) (int, error)

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
	raw := it.raw.Item()
	value, extra, err := it.dict.decomposeValueExtra(raw.Value)
	if err != nil {
		it.err = err
		return false
	}
	it.current = AugDictItem{Key: raw.Key, Value: value, Extra: extra}
	return true
}

func (it *AugDictIterator) Item() AugDictItem {
	if it == nil {
		return AugDictItem{}
	}
	return it.current
}

func (it *AugDictIterator) Key() *Cell {
	return it.Item().Key
}

func (it *AugDictIterator) Value() *Slice {
	return it.Item().Value
}

func (it *AugDictIterator) Extra() *Slice {
	return it.Item().Extra
}

func (it *AugDictIterator) Reset() {
	if it == nil {
		return
	}
	it.current = AugDictItem{}
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
	if d == nil {
		return nil
	}
	if !d.wrapped {
		return validateAugmentedDictRoot(d.root, d.keySz, d.aug)
	}
	if err := validateAugmentedDictRoot(d.root, d.keySz, d.aug); err != nil {
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

func (d *AugmentedDictionary) Range(rev bool, sgnd bool) ([]DictItem, error) {
	if d == nil {
		return []DictItem{}, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, rev, sgnd)
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

// Iterator creates a lazy depth-first raw value+extra iterator. Inspect Err
// after Next returns false to catch failures discovered in deeper children.
func (d *AugmentedDictionary) Iterator(rev bool, sgnd bool) (*DictIterator, error) {
	if d == nil {
		return newDictIterator(nil, 0, rev, sgnd, nil)
	}
	return newDictIterator(d.root, d.keySz, rev, sgnd, d.trace)
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

func (d *AugmentedDictionary) LookupNearestKey(key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}

	return fixedDictLookupNearest(d.root, d.keySz, key, fetchNext, allowEq, invertFirst)
}

func (d *AugmentedDictionary) HasCommonPrefix(prefix *Cell) (bool, error) {
	if d == nil {
		return true, nil
	}
	return fixedDictHasCommonPrefix(d.root, d.keySz, prefix)
}

func (d *AugmentedDictionary) GetCommonPrefix(limit ...uint) (*Cell, error) {
	if d == nil {
		return BeginCell().EndCell(), nil
	}
	maxLen := d.keySz
	if len(limit) > 0 && limit[0] < maxLen {
		maxLen = limit[0]
	}
	return fixedDictCommonPrefix(d.root, d.keySz, maxLen)
}

func (d *AugmentedDictionary) ExtractPrefixSubdictRoot(prefix *Cell, removePrefix bool) (*Cell, error) {
	if d == nil {
		return nil, nil
	}
	root, changed, err := extractPrefixSubdictRootTraced(d.root, d.keySz, prefix, removePrefix, d.trace)
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

	root, changed, err := extractPrefixSubdictRootTraced(d.root, d.keySz, prefix, removePrefix, d.trace)
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
	items, err := fixedDictRange(d.root, d.keySz, false, invertFirst)
	if err != nil {
		return false, err
	}
	return fixedDictCheckForEach(items, fn, shuffle)
}

func (d *AugmentedDictionary) ValidateCheck(fn DictForeachFunc, invertFirst bool) (bool, error) {
	if err := validateAugmentedDictionary(d); err != nil {
		return false, err
	}
	return d.CheckForEach(fn, invertFirst, false)
}

func (d *AugmentedDictionary) ValidateAll() bool {
	return validateAugmentedDictionary(d) == nil
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

func (d *AugmentedDictionary) traverseExtraNode(branch *Cell, remaining uint, prefix *Builder, fn AugDictTraverseFunc) (*Slice, *Slice, error) {
	if branch == nil {
		return nil, nil, nil
	}

	loader, err := branch.BeginParse()
	if err != nil {
		return nil, nil, err
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
	rebuilt, extraCell, err := state.dict.storeForkWithExtraSlices(&parentLabel, newLeft.root, &newLeft.extra, newRight.root, &newRight.extra, remaining)
	if err != nil {
		return augmentedDictFilterResult{}, err
	}
	var extra Slice
	if err = extraCell.BeginParseInto(&extra); err != nil {
		return augmentedDictFilterResult{}, err
	}
	return augmentedDictFilterResult{root: rebuilt, extra: extra, changed: true}, nil
}
