package cell

import "fmt"

func cloneDictItems(items []DictItem) []DictItem {
	out := make([]DictItem, len(items))
	for i, item := range items {
		out[i] = DictItem{
			Key:   item.Key,
			Value: cloneSlice(item.Value),
		}
	}
	return out
}

func rebuildPlainDict(keySz uint, items []DictItem) (*Cell, error) {
	tmp := NewDict(keySz)
	for _, item := range items {
		if item.Key == nil || item.Key.BitsSize() != keySz {
			return nil, fmt.Errorf("invalid key size")
		}
		if item.Value == nil {
			return nil, fmt.Errorf("dict item value is nil")
		}
		if _, err := tmp.SetBuilderWithMode(item.Key, item.Value.ToBuilder(), DictSetModeSet); err != nil {
			return nil, err
		}
	}
	return tmp.root, nil
}

func (d *Dictionary) Range(rev bool, sgnd bool) ([]DictItem, error) {
	if d == nil {
		return []DictItem{}, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, rev, sgnd)
	if err != nil {
		return nil, err
	}
	return cloneDictItems(items), nil
}

func (d *Dictionary) Iterator(rev bool, sgnd bool) (*DictIterator, error) {
	items, err := d.Range(rev, sgnd)
	if err != nil {
		return nil, err
	}
	return newDictIterator(items), nil
}

func (d *Dictionary) LookupNearestKey(key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, error) {
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

func (d *Dictionary) HasCommonPrefix(prefix *Cell) (bool, error) {
	if d == nil {
		return true, nil
	}
	return fixedDictHasCommonPrefix(d.root, d.keySz, prefix)
}

func (d *Dictionary) GetCommonPrefix(limit ...uint) (*Cell, error) {
	if d == nil {
		return BeginCell().EndCell(), nil
	}
	maxLen := d.keySz
	if len(limit) > 0 && limit[0] < maxLen {
		maxLen = limit[0]
	}
	return fixedDictCommonPrefix(d.root, d.keySz, maxLen)
}

func (d *Dictionary) ExtractPrefixSubdictRoot(prefix *Cell, removePrefix bool) (*Cell, error) {
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

func (d *Dictionary) CutPrefixSubdict(prefix *Cell, removePrefix bool) (bool, error) {
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
		d.root = root
	}
	return true, nil
}

func (d *Dictionary) CheckForEach(fn DictForeachFunc, invertFirst bool, shuffle bool) (bool, error) {
	if d == nil {
		return true, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, false, invertFirst)
	if err != nil {
		return false, err
	}
	return fixedDictCheckForEach(items, fn, shuffle)
}

func (d *Dictionary) ValidateCheck(fn DictForeachFunc, invertFirst bool) (bool, error) {
	if d == nil {
		return true, nil
	}
	if err := validatePlainDictRoot(d.root, d.keySz); err != nil {
		return false, err
	}
	return d.CheckForEach(fn, invertFirst, false)
}

func (d *Dictionary) ValidateAll() bool {
	if d == nil {
		return true
	}
	return validatePlainDictRoot(d.root, d.keySz) == nil
}

func (d *Dictionary) Filter(fn DictFilterFunc) (int, error) {
	if d == nil || d.root == nil {
		return 0, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, false, false)
	if err != nil {
		return 0, err
	}
	filtered, changes, err := fixedDictFilterItems(items, fn)
	if err != nil {
		return 0, err
	}
	if changes == 0 {
		return 0, nil
	}
	root, err := rebuildPlainDict(d.keySz, filtered)
	if err != nil {
		return 0, err
	}
	d.root = root
	return changes, nil
}
