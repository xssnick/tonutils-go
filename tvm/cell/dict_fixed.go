package cell

import (
	"fmt"
	"math/rand"
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

	aSlice := a.MustBeginParse()
	bSlice := b.MustBeginParse()
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

	keySlice := key.MustBeginParse()
	prefixSlice := prefix.MustBeginParse()
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
	return BeginCell().MustStoreSlice(key.MustBeginParse().MustLoadSlice(bits), bits).EndCell(), nil
}

func fixedDictCommonPrefix(root *Cell, keySz uint, limit uint) (*Cell, error) {
	if root == nil || limit == 0 {
		return BeginCell().EndCell(), nil
	}
	node, err := parseFixedDictNode(root, keySz)
	if err != nil {
		return nil, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, err
	}

	labelLen := node.labelLen
	if labelLen > limit {
		labelLen = limit
	}
	return cellPrefix(node.label.EndCell(), labelLen)
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

func appendFixedDictEntries(items *[]DictItem, root *Cell, remaining uint, prefix *Builder, rev bool, invertFirst bool, rootLevel bool) error {
	if root == nil {
		return nil
	}

	node, err := parseFixedDictNode(root, remaining)
	if err != nil {
		return err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return err
	}

	keyPrefix := prefix.Copy()
	if err = keyPrefix.StoreBuilder(node.label); err != nil {
		return err
	}

	if node.isLeaf(remaining) {
		*items = append(*items, DictItem{
			Key:   keyPrefix.EndCell(),
			Value: node.loader,
		})
		return nil
	}

	first, second := 0, 1
	if rootLevel && invertFirst && node.labelLen == 0 {
		first, second = 1, 0
		if rev {
			first, second = 0, 1
		}
	} else if rev {
		first, second = 1, 0
	}

	nextRemaining := node.nextKeyBits(remaining)
	firstRef, err := node.ref(first)
	if err != nil {
		return err
	}
	if err = appendFixedDictEntries(items, firstRef, nextRemaining, keyPrefix.Copy().MustStoreUInt(uint64(first), 1), rev, invertFirst, false); err != nil {
		return err
	}

	secondRef, err := node.ref(second)
	if err != nil {
		return err
	}
	return appendFixedDictEntries(items, secondRef, nextRemaining, keyPrefix.Copy().MustStoreUInt(uint64(second), 1), rev, invertFirst, false)
}

func fixedDictRange(root *Cell, keySz uint, rev bool, invertFirst bool) ([]DictItem, error) {
	if root == nil {
		return []DictItem{}, nil
	}

	items := make([]DictItem, 0)
	if err := appendFixedDictEntries(&items, root, keySz, BeginCell(), rev, invertFirst, true); err != nil {
		return nil, err
	}
	return items, nil
}

func fixedDictLookupNearest(root *Cell, keySz uint, key *Cell, fetchNext bool, allowEq bool, invertFirst bool) (*Cell, *Slice, error) {
	return fixedDictLookupNearestTraced(root, keySz, key, fetchNext, allowEq, invertFirst, nil)
}

func fixedDictLookupNearestTraced(root *Cell, keySz uint, key *Cell, fetchNext bool, allowEq bool, invertFirst bool, trace *Trace) (*Cell, *Slice, error) {
	if root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	root = root.withTraceCombined(trace)

	target, err := fixedDictKeyBits(key, keySz)
	if err != nil {
		return nil, nil, err
	}

	item, ok, err := fixedDictLookupNearestNode(root, keySz, BeginCell(), target, fetchNext, allowEq, invertFirst)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrNoSuchKeyInDict
	}
	return item.Key, item.Value, nil
}

func fixedDictKeyBits(key *Cell, keySz uint) ([]uint8, error) {
	if key == nil || key.BitsSize() != keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	loader := key.MustBeginParse()
	bits := make([]uint8, keySz)
	for i := uint(0); i < keySz; i++ {
		bit, err := loader.LoadUInt(1)
		if err != nil {
			return nil, err
		}
		bits[i] = uint8(bit)
	}
	return bits, nil
}

func fixedDictLookupNearestNode(root *Cell, remaining uint, prefix *Builder, target []uint8, fetchNext bool, allowEq bool, invertFirst bool) (DictItem, bool, error) {
	node, err := parseFixedDictNode(root, remaining)
	if err != nil {
		return DictItem{}, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return DictItem{}, false, err
	}

	basePrefix := prefix.Copy()
	prefix = prefix.Copy()
	label := node.label.ToSlice()
	for i := uint(0); i < node.labelLen; i++ {
		bit, err := label.LoadUInt(1)
		if err != nil {
			return DictItem{}, false, err
		}

		pos := prefix.BitsUsed()
		prefix.MustStoreUInt(bit, 1)
		if pos >= uint(len(target)) || uint8(bit) == target[pos] {
			continue
		}

		bitOrder := fixedDictOrderBit(uint8(bit), pos, invertFirst)
		targetOrder := fixedDictOrderBit(target[pos], pos, invertFirst)
		if fetchNext && bitOrder > targetOrder {
			return fixedDictBoundary(root, remaining, basePrefix, false, invertFirst)
		}
		if !fetchNext && bitOrder < targetOrder {
			return fixedDictBoundary(root, remaining, basePrefix, true, invertFirst)
		}
		return DictItem{}, false, nil
	}

	if node.isLeaf(remaining) {
		if allowEq {
			return DictItem{Key: prefix.EndCell(), Value: cloneSlice(node.loader)}, true, nil
		}
		return DictItem{}, false, nil
	}

	pos := prefix.BitsUsed()
	if pos >= uint(len(target)) {
		return DictItem{}, false, fmt.Errorf("target key is shorter than dictionary key")
	}

	targetBit := int(target[pos])
	nextRemaining := node.nextKeyBits(remaining)
	if fetchNext {
		first, second := fixedDictOrderedChildPair(pos, targetBit, invertFirst)
		firstRef, err := node.ref(first)
		if err != nil {
			return DictItem{}, false, err
		}
		item, ok, err := fixedDictLookupNearestNode(firstRef, nextRemaining, prefix.Copy().MustStoreUInt(uint64(first), 1), target, fetchNext, allowEq, invertFirst)
		if err != nil || ok {
			return item, ok, err
		}
		if fixedDictOrderBit(uint8(first), pos, invertFirst) < fixedDictOrderBit(uint8(second), pos, invertFirst) {
			secondRef, err := node.ref(second)
			if err != nil {
				return DictItem{}, false, err
			}
			return fixedDictBoundary(secondRef, nextRemaining, prefix.Copy().MustStoreUInt(uint64(second), 1), false, invertFirst)
		}
		return DictItem{}, false, nil
	}

	first, second := fixedDictOrderedChildPair(pos, targetBit, invertFirst)
	firstRef, err := node.ref(first)
	if err != nil {
		return DictItem{}, false, err
	}
	item, ok, err := fixedDictLookupNearestNode(firstRef, nextRemaining, prefix.Copy().MustStoreUInt(uint64(first), 1), target, fetchNext, allowEq, invertFirst)
	if err != nil || ok {
		return item, ok, err
	}
	if fixedDictOrderBit(uint8(first), pos, invertFirst) > fixedDictOrderBit(uint8(second), pos, invertFirst) {
		secondRef, err := node.ref(second)
		if err != nil {
			return DictItem{}, false, err
		}
		return fixedDictBoundary(secondRef, nextRemaining, prefix.Copy().MustStoreUInt(uint64(second), 1), true, invertFirst)
	}
	return DictItem{}, false, nil
}

func fixedDictBoundary(root *Cell, remaining uint, prefix *Builder, max bool, invertFirst bool) (DictItem, bool, error) {
	node, err := parseFixedDictNode(root, remaining)
	if err != nil {
		return DictItem{}, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return DictItem{}, false, err
	}

	prefix = prefix.Copy().MustStoreBuilder(node.label)
	if node.isLeaf(remaining) {
		return DictItem{Key: prefix.EndCell(), Value: cloneSlice(node.loader)}, true, nil
	}

	refIdx := fixedDictBoundaryChild(prefix.BitsUsed(), max, invertFirst)

	ref, err := node.ref(refIdx)
	if err != nil {
		return DictItem{}, false, err
	}
	return fixedDictBoundary(ref, node.nextKeyBits(remaining), prefix.MustStoreUInt(uint64(refIdx), 1), max, invertFirst)
}

func fixedDictOrderBit(bit uint8, pos uint, invertFirst bool) uint8 {
	if invertFirst && pos == 0 {
		return bit ^ 1
	}
	return bit
}

func fixedDictOrderedChildPair(pos uint, targetBit int, invertFirst bool) (int, int) {
	targetOrder := fixedDictOrderBit(uint8(targetBit), pos, invertFirst)
	if targetOrder == 0 {
		first := targetBit
		return first, first ^ 1
	}
	second := targetBit ^ 1
	return targetBit, second
}

func fixedDictBoundaryChild(pos uint, max bool, invertFirst bool) int {
	orderBit := uint8(0)
	if max {
		orderBit = 1
	}
	if invertFirst && pos == 0 {
		orderBit ^= 1
	}
	return int(orderBit)
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
	return extractPrefixSubdictRootTraced(root, keySz, prefix, removePrefix, nil)
}

func extractPrefixSubdictRootTraced(root *Cell, keySz uint, prefix *Cell, removePrefix bool, trace *Trace) (*Cell, bool, error) {
	if root == nil {
		return nil, false, nil
	}
	root = root.withTraceCombined(trace)

	prefixLen := uint(0)
	var prefixSlice *Slice
	if prefix != nil {
		prefixLen = prefix.BitsSize()
		prefixSlice = prefix.MustBeginParse()
	} else {
		prefixSlice = BeginCell().EndCell().MustBeginParse()
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
		node, err := parseFixedDictNode(branch, keySz-consumed)
		if err != nil {
			return nil, false, err
		}
		if err = node.rejectSpecial("dict"); err != nil {
			return nil, false, err
		}

		toMatch := prefixLen - consumed
		if toMatch > node.labelLen {
			toMatch = node.labelLen
		}

		matched, err := consumeCommonPrefix(node.label.ToSlice(), prefixSlice, toMatch)
		if err != nil {
			return nil, false, err
		}
		if matched < toMatch {
			return nil, true, nil
		}

		if consumed+node.labelLen < prefixLen {
			idx, err := prefixSlice.LoadUInt(1)
			if err != nil {
				return nil, false, err
			}
			consumed += node.labelLen + 1
			branch, err = node.ref(int(idx))
			if err != nil {
				return nil, false, err
			}
			continue
		}

		if !removePrefix {
			if consumed == 0 {
				return branch, false, nil
			}

			prefixBits, err := prefix.MustBeginParse().LoadSlice(consumed)
			if err != nil {
				return nil, false, err
			}

			combined := BeginCell()
			if consumed > 0 {
				if err = combined.StoreSlice(prefixBits, consumed); err != nil {
					return nil, false, err
				}
			}
			if err = combined.StoreBuilder(node.label); err != nil {
				return nil, false, err
			}
			subdict, err := storeDictNodeTraced(combined.ToSlice(), node.loader.ToBuilder(), keySz, trace)
			return subdict, true, err
		}

		suffix := node.label.ToSlice()
		if prefixLen > consumed {
			if _, err := suffix.LoadSlice(prefixLen - consumed); err != nil {
				return nil, false, err
			}
		}
		subdict, err := storeDictNodeTraced(suffix, node.loader.ToBuilder(), keySz-prefixLen, trace)
		return subdict, true, err
	}
}
