package cell

import "fmt"

func (d *Dictionary) Range(rev bool, sgnd bool) ([]DictItem, error) {
	if d == nil {
		return []DictItem{}, nil
	}
	items, err := fixedDictRange(d.root, d.keySz, rev, sgnd)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Iterator creates a lazy depth-first iterator. If a lazy or malformed child
// fails after construction, Next returns false and Err reports that failure.
func (d *Dictionary) Iterator(rev bool, sgnd bool) (*DictIterator, error) {
	if d == nil {
		return newDictIterator(nil, 0, rev, sgnd, nil)
	}
	return newDictIterator(d.root, d.keySz, rev, sgnd, d.trace)
}

// IteratorAt creates a lazy iterator positioned at the nearest key to `key`
// in iteration order: for rev=false the first item is the smallest key >= key
// (> key when allowEq is false), for rev=true the largest key <= key (< key).
// Reset rewinds to the full range, not to the seek position.
func (d *Dictionary) IteratorAt(key *Cell, rev bool, sgnd bool, allowEq bool) (*DictIterator, error) {
	if d == nil {
		return newDictIterator(nil, 0, rev, sgnd, nil)
	}
	return newDictIteratorAt(d.root, d.keySz, key, rev, sgnd, allowEq, d.trace)
}

// ForEachRefValue visits every leaf of a dictionary whose values are exactly
// one reference. It validates the complete dictionary shape and returns the
// number of visited leaves without materializing keys or value slices.
func (d *Dictionary) ForEachRefValue(fn func(value *Cell) error) (int, error) {
	if d == nil {
		return 0, nil
	}
	if err := validateDictKeySize(d.keySz); err != nil {
		return 0, err
	}
	if d.root == nil {
		return 0, nil
	}
	return forEachPlainDictRefValue(d.root.withTraceCombined(d.trace), d.keySz, fn)
}

func (d *Dictionary) LookupNearestKey(key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}

	return fixedDictLookupNearestTraced(d.root, d.keySz, key, fetchNext, allowEq, invertFirst, d.trace)
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
	root, changed, err := extractPrefixSubdictRootTraced(d.root, d.keySz, prefix, removePrefix, d.trace)
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

	root, changed, err := extractPrefixSubdictRootTraced(d.root, d.keySz, prefix, removePrefix, d.trace)
	if err != nil {
		return false, err
	}
	if removePrefix && prefix != nil && prefix.BitsSize() <= d.keySz {
		d.keySz -= prefix.BitsSize()
	}
	if changed {
		d.setRoot(root)
	}
	return true, nil
}

func (d *Dictionary) CheckForEach(fn DictForeachFunc, invertFirst bool, shuffle bool) (bool, error) {
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
	root, changes, err := fixedDictFilter(d.root, d.keySz, fn, d.trace)
	if err != nil {
		return 0, err
	}
	if changes == 0 {
		return 0, nil
	}
	d.setRoot(root)
	return changes, nil
}
