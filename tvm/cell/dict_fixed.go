package cell

import (
	"fmt"
	"math/rand"
	"sort"
)

type DictSetMode uint8

const (
	DictSetModeReplace DictSetMode = 1
	DictSetModeAdd     DictSetMode = 2
	DictSetModeSet     DictSetMode = DictSetModeReplace | DictSetModeAdd
)

type DictItem struct {
	Key   *Cell
	Value *Slice
}

type DictIterator struct {
	items []DictItem
	idx   int
}

type DictForeachFunc func(value *Slice, key *Cell) (bool, error)

type DictFilterAction uint8

const (
	DictFilterKeep DictFilterAction = iota
	DictFilterRemove
	DictFilterKeepRest
	DictFilterRemoveRest
)

type DictFilterFunc func(value *Slice, key *Cell) (DictFilterAction, error)

func newDictIterator(items []DictItem) *DictIterator {
	return &DictIterator{
		items: items,
		idx:   -1,
	}
}

func (it *DictIterator) Next() bool {
	if it == nil || it.idx+1 >= len(it.items) {
		return false
	}
	it.idx++
	return true
}

func (it *DictIterator) Item() DictItem {
	if it == nil || it.idx < 0 || it.idx >= len(it.items) {
		return DictItem{}
	}
	return it.items[it.idx]
}

func (it *DictIterator) Key() *Cell {
	return it.Item().Key
}

func (it *DictIterator) Value() *Slice {
	return it.Item().Value
}

func (it *DictIterator) Reset() {
	if it == nil {
		return
	}
	it.idx = -1
}

func cloneSlice(s *Slice) *Slice {
	if s == nil {
		return nil
	}
	return s.Copy()
}

func compareKeyCells(a, b *Cell, invertFirst bool) int {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return -1
		default:
			return 1
		}
	}

	aSlice := a.BeginParse()
	bSlice := b.BeginParse()
	limit := aSlice.BitsLeft()
	if bSlice.BitsLeft() < limit {
		limit = bSlice.BitsLeft()
	}

	for i := uint(0); i < limit; i++ {
		av := aSlice.MustLoadUInt(1) != 0
		bv := bSlice.MustLoadUInt(1) != 0
		if invertFirst && i == 0 {
			av = !av
			bv = !bv
		}
		if av == bv {
			continue
		}
		if av {
			return 1
		}
		return -1
	}

	switch {
	case aSlice.BitsLeft() == bSlice.BitsLeft():
		return 0
	case aSlice.BitsLeft() < bSlice.BitsLeft():
		return -1
	default:
		return 1
	}
}

func cellHasPrefix(key, prefix *Cell) bool {
	if prefix == nil {
		return true
	}
	if key == nil || prefix.BitsSize() > key.BitsSize() {
		return false
	}

	keySlice := key.BeginParse()
	prefixSlice := prefix.BeginParse()
	for prefixSlice.BitsLeft() > 0 {
		if keySlice.MustLoadUInt(1) != prefixSlice.MustLoadUInt(1) {
			return false
		}
	}
	return true
}

func cellPrefix(key *Cell, bits uint) (*Cell, error) {
	if key == nil {
		return nil, fmt.Errorf("key is nil")
	}
	if bits > key.BitsSize() {
		return nil, fmt.Errorf("prefix length is too large")
	}
	return BeginCell().MustStoreSlice(key.BeginParse().MustLoadSlice(bits), bits).EndCell(), nil
}

func fixedDictCommonPrefix(root *Cell, keySz uint, limit uint) (*Cell, error) {
	if root == nil || limit == 0 {
		return BeginCell().EndCell(), nil
	}
	if root.special {
		return nil, fmt.Errorf("dict has special cells in tree structure")
	}

	loader := root.BeginParse()
	labelLen, label, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return nil, err
	}
	if labelLen > limit {
		labelLen = limit
	}
	return cellPrefix(label.EndCell(), labelLen)
}

func fixedDictHasCommonPrefix(root *Cell, keySz uint, prefix *Cell) (bool, error) {
	if root == nil || prefix == nil || prefix.BitsSize() == 0 {
		return true, nil
	}
	if prefix.BitsSize() > keySz {
		return false, nil
	}

	common, err := fixedDictCommonPrefix(root, keySz, prefix.BitsSize())
	if err != nil {
		return false, err
	}
	return cellHasPrefix(common, prefix), nil
}

func collectFixedDictEntries(root *Cell, keySz uint) ([]DictItem, error) {
	if root == nil {
		return []DictItem{}, nil
	}
	return collectFixedDictEntriesInner(root, keySz, BeginCell())
}

func collectFixedDictEntriesInner(root *Cell, remaining uint, prefix *Builder) ([]DictItem, error) {
	if root == nil {
		return []DictItem{}, nil
	}
	if root.special {
		return nil, fmt.Errorf("dict has special cells in tree structure")
	}

	loader := root.BeginParse()
	labelLen, keyPrefix, err := loadLabel(remaining, loader, prefix)
	if err != nil {
		return nil, err
	}

	if labelLen == remaining {
		return []DictItem{{
			Key:   keyPrefix.EndCell(),
			Value: loader,
		}}, nil
	}

	nextRemaining := remaining - (labelLen + 1)
	left, err := loader.LoadRefCell()
	if err != nil {
		return nil, err
	}
	leftItems, err := collectFixedDictEntriesInner(left, nextRemaining, keyPrefix.Copy().MustStoreUInt(0, 1))
	if err != nil {
		return nil, err
	}

	right, err := loader.LoadRefCell()
	if err != nil {
		return nil, err
	}
	rightItems, err := collectFixedDictEntriesInner(right, nextRemaining, keyPrefix.Copy().MustStoreUInt(1, 1))
	if err != nil {
		return nil, err
	}

	return append(leftItems, rightItems...), nil
}

func sortDictItems(items []DictItem, rev bool, invertFirst bool) {
	sort.Slice(items, func(i, j int) bool {
		return compareKeyCells(items[i].Key, items[j].Key, invertFirst) < 0
	})
	if !rev {
		return
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func fixedDictRange(root *Cell, keySz uint, rev bool, invertFirst bool) ([]DictItem, error) {
	items, err := collectFixedDictEntries(root, keySz)
	if err != nil {
		return nil, err
	}
	sortDictItems(items, rev, invertFirst)
	return items, nil
}

func fixedDictNearest(items []DictItem, key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice) {
	if len(items) == 0 || key == nil {
		return nil, nil
	}

	pos := sort.Search(len(items), func(i int) bool {
		return compareKeyCells(items[i].Key, key, invertFirst) >= 0
	})

	if pos < len(items) && compareKeyCells(items[pos].Key, key, invertFirst) == 0 {
		if allowEq {
			return items[pos].Key, cloneSlice(items[pos].Value)
		}
		if fetchNext {
			pos++
		} else {
			pos--
		}
	} else if !fetchNext {
		pos--
	}

	if pos < 0 || pos >= len(items) {
		return nil, nil
	}
	return items[pos].Key, cloneSlice(items[pos].Value)
}

func fixedDictCheckForEach(items []DictItem, fn DictForeachFunc, shuffle bool) (bool, error) {
	if fn == nil {
		return true, nil
	}

	if shuffle {
		rand.Shuffle(len(items), func(i, j int) {
			items[i], items[j] = items[j], items[i]
		})
	}

	for _, item := range items {
		ok, err := fn(cloneSlice(item.Value), item.Key)
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func fixedDictFilterItems(items []DictItem, fn DictFilterFunc) ([]DictItem, int, error) {
	if fn == nil {
		cp := append([]DictItem{}, items...)
		return cp, 0, nil
	}

	out := make([]DictItem, 0, len(items))
	changes := 0
	for i, item := range items {
		action, err := fn(cloneSlice(item.Value), item.Key)
		if err != nil {
			return nil, 0, err
		}

		switch action {
		case DictFilterKeep:
			out = append(out, item)
		case DictFilterRemove:
			changes++
		case DictFilterKeepRest:
			out = append(out, item)
			out = append(out, items[i+1:]...)
			return out, changes, nil
		case DictFilterRemoveRest:
			changes += len(items) - i
			return out, changes, nil
		default:
			return nil, 0, fmt.Errorf("unknown dict filter action")
		}
	}
	return out, changes, nil
}

func extractPrefixSubdictRoot(root *Cell, keySz uint, prefix *Cell, removePrefix bool) (*Cell, bool, error) {
	if root == nil {
		return nil, false, nil
	}

	prefixLen := uint(0)
	var prefixSlice *Slice
	if prefix != nil {
		prefixLen = prefix.BitsSize()
		prefixSlice = prefix.BeginParse()
	} else {
		prefixSlice = BeginCell().EndCell().BeginParse()
	}

	if prefixLen == 0 {
		return root, false, nil
	}

	if prefixLen > keySz {
		return nil, true, nil
	}

	consumed := uint(0)
	branch := root
	for {
		if branch == nil {
			return nil, true, nil
		}
		if branch.special {
			return nil, false, fmt.Errorf("dict has special cells in tree structure")
		}

		loader := branch.BeginParse()
		labelLen, label, err := loadLabel(keySz-consumed, loader, BeginCell())
		if err != nil {
			return nil, false, err
		}

		toMatch := prefixLen - consumed
		if toMatch > labelLen {
			toMatch = labelLen
		}

		matched, err := consumeCommonPrefix(label.ToSlice(), prefixSlice, toMatch)
		if err != nil {
			return nil, false, err
		}
		if matched < toMatch {
			return nil, true, nil
		}

		if consumed+labelLen < prefixLen {
			idx, err := prefixSlice.LoadUInt(1)
			if err != nil {
				return nil, false, err
			}
			consumed += labelLen + 1
			branch, err = branch.PeekRef(int(idx))
			if err != nil {
				return nil, false, err
			}
			continue
		}

		if !removePrefix {
			if consumed == 0 {
				return branch, false, nil
			}

			prefixBits, err := prefix.BeginParse().LoadSlice(consumed)
			if err != nil {
				return nil, false, err
			}

			combined := BeginCell()
			if consumed > 0 {
				if err = combined.StoreSlice(prefixBits, consumed); err != nil {
					return nil, false, err
				}
			}
			if err = combined.StoreBuilder(label); err != nil {
				return nil, false, err
			}
			node, err := storeDictNode(combined.ToSlice(), loader.ToBuilder(), keySz)
			return node, true, err
		}

		suffix := label.ToSlice()
		if prefixLen > consumed {
			if _, err := suffix.LoadSlice(prefixLen - consumed); err != nil {
				return nil, false, err
			}
		}
		node, err := storeDictNode(suffix, loader.ToBuilder(), keySz-prefixLen)
		return node, true, err
	}
}
