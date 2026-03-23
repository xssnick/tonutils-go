package cell

import "fmt"

type AugDictItem struct {
	Key   *Cell
	Value *Slice
	Extra *Slice
}

type AugDictIterator struct {
	items []AugDictItem
	idx   int
}

type AugDictForeachFunc func(value, extra *Slice, key *Cell) (bool, error)
type AugDictFilterFunc func(value, extra *Slice, key *Cell) (DictFilterAction, error)
type AugDictTraverseFunc func(keyPrefix *Cell, extra *Slice, value *Slice) (int, error)

func newAugDictIterator(items []AugDictItem) *AugDictIterator {
	return &AugDictIterator{
		items: items,
		idx:   -1,
	}
}

func (it *AugDictIterator) Next() bool {
	if it == nil || it.idx+1 >= len(it.items) {
		return false
	}
	it.idx++
	return true
}

func (it *AugDictIterator) Item() AugDictItem {
	if it == nil || it.idx < 0 || it.idx >= len(it.items) {
		return AugDictItem{}
	}
	return it.items[it.idx]
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
	it.idx = -1
}

func cloneAugDictItems(items []AugDictItem) []AugDictItem {
	out := make([]AugDictItem, len(items))
	for i, item := range items {
		out[i] = AugDictItem{
			Key:   item.Key,
			Value: cloneSlice(item.Value),
			Extra: cloneSlice(item.Extra),
		}
	}
	return out
}

func validateAugmentedDictionary(d *AugmentedDictionary) error {
	if d == nil {
		return nil
	}
	if !d.wrapped {
		return validateAugmentedDictRoot(d.root, d.keySz, d.aug)
	}

	root, err := d.ToCell()
	if err != nil {
		return err
	}
	_, err = root.BeginParse().LoadAugDictWithAugmentation(d.keySz, d.aug)
	return err
}

func rebuildAugmentedDict(d *AugmentedDictionary, items []DictItem) (*Cell, *Cell, error) {
	if err := d.ensureWritable(); err != nil {
		return nil, nil, err
	}

	tmp := d.Copy()
	tmp.root = nil
	tmp.rootExtra = nil

	for _, item := range items {
		if item.Key == nil || item.Key.BitsSize() != d.keySz {
			return nil, nil, fmt.Errorf("invalid key size")
		}
		value, _, err := d.decomposeValueExtra(item.Value)
		if err != nil {
			return nil, nil, err
		}
		if _, err = tmp.SetBuilderWithMode(item.Key, value.ToBuilder(), DictSetModeSet); err != nil {
			return nil, nil, err
		}
	}

	if tmp.root == nil && tmp.wrapped {
		extra, err := d.aug.EmptyExtra()
		if err != nil {
			return nil, nil, err
		}
		tmp.rootExtra = extra
	}

	return tmp.root, tmp.rootExtra, nil
}

func (d *AugmentedDictionary) Range(rev bool, sgnd bool) ([]DictItem, error) {
	if d == nil {
		return []DictItem{}, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, rev, sgnd)
	if err != nil {
		return nil, err
	}
	return cloneDictItems(items), nil
}

func (d *AugmentedDictionary) Iterator(rev bool, sgnd bool) (*DictIterator, error) {
	items, err := d.Range(rev, sgnd)
	if err != nil {
		return nil, err
	}
	return newDictIterator(items), nil
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

func (d *AugmentedDictionary) IteratorExtra(rev bool, sgnd bool) (*AugDictIterator, error) {
	items, err := d.RangeExtra(rev, sgnd)
	if err != nil {
		return nil, err
	}
	return newAugDictIterator(items), nil
}

func (d *AugmentedDictionary) LookupNearestKey(key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}

	items, err := fixedDictRange(d.root, d.keySz, false, invertFirst)
	if err != nil {
		return nil, nil, err
	}
	nearestKey, nearestValue := fixedDictNearest(items, key, fetchNext, allowEq, invertFirst)
	if nearestKey == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	return nearestKey, nearestValue, nil
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
	root, changed, err := extractPrefixSubdictRoot(d.root, d.keySz, prefix, removePrefix)
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

	root, changed, err := extractPrefixSubdictRoot(d.root, d.keySz, prefix, removePrefix)
	if err != nil {
		return false, err
	}
	if removePrefix && prefix != nil && prefix.BitsSize() <= d.keySz {
		d.keySz -= prefix.BitsSize()
	}
	if changed {
		if err = d.setRoot(root); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (d *AugmentedDictionary) CheckForEach(fn DictForeachFunc, invertFirst bool, shuffle bool) (bool, error) {
	if d == nil {
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
	items, err := d.RangeExtra(false, invertFirst)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		ok, err := fn(cloneSlice(item.Value), cloneSlice(item.Extra), item.Key)
		if err != nil || !ok {
			return ok, err
		}
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
	if branch.special {
		return nil, nil, fmt.Errorf("augmented dict has special cells in tree structure")
	}

	loader := branch.BeginParse()
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

	r, err := fn(prefixKey.EndCell(), extra.BeginParse(), nil)
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
	if err := d.ensureWritable(); err != nil {
		return 0, err
	}

	items, err := d.Range(false, false)
	if err != nil {
		return 0, err
	}

	out := make([]DictItem, 0, len(items))
	changes := 0
	for i, item := range items {
		value, extra, err := d.decomposeValueExtra(item.Value)
		if err != nil {
			return 0, err
		}
		action, err := fn(cloneSlice(value), cloneSlice(extra), item.Key)
		if err != nil {
			return 0, err
		}
		switch action {
		case DictFilterKeep:
			out = append(out, item)
		case DictFilterRemove:
			changes++
		case DictFilterKeepRest:
			out = append(out, item)
			out = append(out, items[i+1:]...)
			root, rootExtra, err := rebuildAugmentedDict(d, out)
			if err != nil {
				return 0, err
			}
			if err = d.setRootWithExtra(root, rootExtra); err != nil {
				return 0, err
			}
			return changes, nil
		case DictFilterRemoveRest:
			changes += len(items) - i
			root, rootExtra, err := rebuildAugmentedDict(d, out)
			if err != nil {
				return 0, err
			}
			if err = d.setRootWithExtra(root, rootExtra); err != nil {
				return 0, err
			}
			return changes, nil
		default:
			return 0, fmt.Errorf("unknown dict filter action")
		}
	}

	if changes == 0 {
		return 0, nil
	}

	root, rootExtra, err := rebuildAugmentedDict(d, out)
	if err != nil {
		return 0, err
	}
	if err = d.setRootWithExtra(root, rootExtra); err != nil {
		return 0, err
	}
	return changes, nil
}
