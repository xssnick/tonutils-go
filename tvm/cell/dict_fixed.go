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

// DictItemView is a borrowed iterator item. Key and Value are valid only until
// the iterator's next Next or Reset call. They may be parsed during that window,
// and Key.ToCell or Key.BaseCell may be used to retain an owned key, but the
// views themselves must not be retained, nor read through RawCell: that hands
// out the iterator's scratch key cell, which the next advance overwrites.
type DictItemView struct {
	Key   Slice
	Value Slice
}

type DictIterator struct {
	root        *Cell
	keySz       uint
	rev         bool
	invertFirst bool
	// lenientForkShape relaxes fork-node validation to the augmented-tree
	// rules: extra bits after the label are legal. It is kept unpacked instead
	// of as a whole dictWalk: an iterator resolves special nodes through its
	// trace only, and a stored resolver would grow every iterator by a word.
	lenientForkShape bool
	trace            *Trace

	stack   []dictIteratorFrame
	prefix  Builder
	current DictItem
	view    DictItemView
	keyCell Cell
	hasView bool
	popNext bool
	err     error
}

type dictIteratorFrame struct {
	node             fixedDictNode
	remaining        uint
	prefixBefore     uint
	prefixAfterLabel uint
	first            int
	second           int
	nextChild        uint8
}

// dictIteratorInitialStackDepth keeps freshly built iterators cheap: a frame
// is ~128 bytes and short-lived iterators over small dicts are created in
// bulk on hot paths, so start small and let append grow the stack for the
// rare deep tree.
const dictIteratorInitialStackDepth = 4

type DictForeachFunc func(value *Slice, key *Cell) (bool, error)
type DictBorrowedForeachFunc func(item DictItemView) error

type DictFilterAction uint8

const (
	DictFilterKeep DictFilterAction = iota
	DictFilterRemove
	DictFilterKeepRest
	DictFilterRemoveRest
)

type DictFilterFunc func(value *Slice, key *Cell) (DictFilterAction, error)

func newDictIterator(root *Cell, keySz uint, rev, invertFirst bool, walk dictWalk) (*DictIterator, error) {
	it := &DictIterator{
		root:             root,
		keySz:            keySz,
		rev:              rev,
		invertFirst:      invertFirst,
		lenientForkShape: walk.lenient,
		trace:            CombineTraces(root.Trace(), walk.trace),
	}
	if root == nil {
		return it, nil
	}

	it.stack = make([]dictIteratorFrame, 0, min(int(keySz)+1, dictIteratorInitialStackDepth))
	if err := it.push(it.root, keySz, true, it.trace); err != nil {
		return nil, err
	}
	return it, nil
}

func (it *DictIterator) Next() bool {
	if it == nil || it.err != nil {
		return false
	}
	if it.popNext {
		// At the final leaf there is no need to mutate traversal state. Keeping
		// the prefix in place also preserves the historical Item-after-Next(false)
		// behavior without copying every yielded key.
		if !it.hasPendingChild() {
			return false
		}
		it.pop()
		it.popNext = false
	}

	for len(it.stack) > 0 {
		frame := &it.stack[len(it.stack)-1]
		if frame.node.isLeaf(frame.remaining) {
			it.current = DictItem{}
			it.keyCell = Cell{
				data:   it.prefix.data[:it.prefix.usedBytes()],
				bitsSz: uint16(it.prefix.bitsSz),
			}
			it.view = DictItemView{
				Key: Slice{
					cell:              &it.keyCell,
					bitEnd:            it.keyCell.bitsSz,
					forceCopyOnToCell: true,
				},
				Value: frame.node.loader,
			}
			it.hasView = true
			it.popNext = true
			return true
		}

		var childIdx int
		switch frame.nextChild {
		case 0:
			childIdx = frame.first
			frame.nextChild = 1
		case 1:
			it.prefix.truncateBits(frame.prefixAfterLabel)
			childIdx = frame.second
			frame.nextChild = 2
		default:
			it.pop()
			continue
		}

		ref, childTrace, err := frame.node.refAndTrace(childIdx)
		if err != nil {
			it.fail(err)
			return false
		}
		if err = it.prefix.StoreUInt(uint64(childIdx), 1); err != nil {
			it.fail(err)
			return false
		}
		if err = it.push(ref, frame.node.nextKeyBits(frame.remaining), false, childTrace); err != nil {
			it.fail(err)
			return false
		}
	}
	return false
}

func (it *DictIterator) hasPendingChild() bool {
	// The last frame is the current leaf. Every ancestor on its path has
	// already selected its first or second child; nextChild == 1 is the only
	// state with an unvisited sibling in iteration order.
	for i := len(it.stack) - 2; i >= 0; i-- {
		if it.stack[i].nextChild == 1 {
			return true
		}
	}
	return false
}

func (it *DictIterator) Item() DictItem {
	if it == nil || !it.hasView {
		return DictItem{}
	}
	it.materializeKey()
	it.materializeValue()
	return it.current
}

// View returns the current item without materializing a key Cell or an owned
// value Slice. The returned views are invalidated by the next Next or Reset
// call and must not be retained, nor read through RawCell: that hands out the
// iterator's scratch key cell. Key.ToCell and Key.BaseCell materialize a
// finalized owned key that remains valid after iterator advance.
func (it *DictIterator) View() DictItemView {
	if it == nil || !it.hasView {
		return DictItemView{}
	}
	return it.view
}

func (it *DictIterator) Key() *Cell {
	if it == nil || !it.hasView {
		return nil
	}
	it.materializeKey()
	return it.current.Key
}

func (it *DictIterator) Value() *Slice {
	if it == nil || !it.hasView {
		return nil
	}
	it.materializeValue()
	return it.current.Value

}

func (it *DictIterator) materializeKey() {
	if it.current.Key != nil {
		return
	}
	var key Builder
	it.view.Key.ToBuilderInto(&key)
	it.current.Key = key.EndCell()
}

func (it *DictIterator) materializeValue() {
	if it.current.Value != nil {
		return
	}
	value := it.view.Value
	it.current.Value = &value
}

func (it *DictIterator) Reset() {
	if it == nil {
		return
	}
	it.stack = it.stack[:0]
	it.prefix = Builder{}
	it.current = DictItem{}
	it.view = DictItemView{}
	it.keyCell = Cell{}
	it.hasView = false
	it.popNext = false
	it.err = nil
	if it.root != nil {
		if err := it.push(it.root, it.keySz, true, it.trace); err != nil {
			it.err = err
		}
	}
}

// Err reports a malformed/lazy-cell error discovered after iterator
// construction. Next remains source compatible with the historical bool-only
// API; callers that consume untrusted dictionaries can inspect Err afterwards.
func (it *DictIterator) Err() error {
	if it == nil {
		return nil
	}
	return it.err
}

func (it *DictIterator) push(root *Cell, remaining uint, rootLevel bool, trace *Trace) error {
	node, err := parseFixedDictNodeWithTrace(root, remaining, trace)
	if err != nil {
		return err
	}
	if err = node.resolveIfSpecial(remaining, trace, nil); err != nil {
		return err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return err
	}
	if err = node.validateForkShape(remaining, it.lenientForkShape); err != nil {
		return err
	}
	return it.pushNode(node, remaining, rootLevel)
}

func (it *DictIterator) pushNode(node fixedDictNode, remaining uint, rootLevel bool) error {
	before := it.prefix.BitsUsed()
	label := node.labelSlice()
	if err := it.prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return err
	}

	first, second := 0, 1
	if rootLevel && it.invertFirst && node.labelLen == 0 {
		first, second = 1, 0
		if it.rev {
			first, second = 0, 1
		}
	} else if it.rev {
		first, second = 1, 0
	}

	it.stack = append(it.stack, dictIteratorFrame{
		node:             node,
		remaining:        remaining,
		prefixBefore:     before,
		prefixAfterLabel: it.prefix.BitsUsed(),
		first:            first,
		second:           second,
	})
	return nil
}

// newDictIteratorAt builds an iterator positioned at the nearest key to target
// in iteration order: for rev=false the first yielded key is the smallest key
// >= target (> target when allowEq is false), for rev=true the largest key
// <= target (< target). Keys preceding the position are skipped without their
// subtrees being visited, so a positioned iterator loads the same cells a
// LookupNearestKey descent would.
func newDictIteratorAt(root *Cell, keySz uint, key *Cell, rev, invertFirst, allowEq bool, walk dictWalk) (*DictIterator, error) {
	it := &DictIterator{
		root:             root,
		keySz:            keySz,
		rev:              rev,
		invertFirst:      invertFirst,
		lenientForkShape: walk.lenient,
		trace:            CombineTraces(root.Trace(), walk.trace),
	}
	if root == nil {
		return it, nil
	}
	if key == nil || key.BitsSize() != keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	it.stack = make([]dictIteratorFrame, 0, min(int(keySz)+1, dictIteratorInitialStackDepth))
	var target Slice
	if err := key.BeginParseInto(&target); err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}
	if err := it.push(it.root, keySz, true, it.trace); err != nil {
		return nil, err
	}
	if err := it.seekFromTop(&target, allowEq); err != nil {
		return nil, err
	}
	return it, nil
}

// seekFromTop repositions an iterator whose stack holds exactly the root frame
// so Next yields the first key at-or-after target in iteration order (an exact
// match is skipped when allowEq is false). The frames left on the stack mirror
// what a plain walk would hold at that position, so Next continues seamlessly.
func (it *DictIterator) seekFromTop(target *Slice, allowEq bool) error {
	for {
		frame := &it.stack[len(it.stack)-1]
		node := &frame.node

		targetLabel := *target
		if err := targetLabel.SkipBits(frame.prefixBefore); err != nil {
			return err
		}
		matched, err := commonSlicePrefix(&node.label, &targetLabel, node.labelLen)
		if err != nil {
			return err
		}
		if matched < node.labelLen {
			pos := frame.prefixBefore + matched
			bit, err := node.label.BitAt(matched)
			if err != nil {
				return err
			}
			targetBit, err := target.BitAt(pos)
			if err != nil {
				return err
			}
			bitOrder := fixedDictOrderBit(uint8(bit), pos, it.invertFirst)
			targetOrder := fixedDictOrderBit(uint8(targetBit), pos, it.invertFirst)
			subtreeAfter := bitOrder > targetOrder
			if it.rev {
				subtreeAfter = bitOrder < targetOrder
			}
			if !subtreeAfter {
				// every key of this subtree precedes target in iteration order
				it.pop()
			}
			// otherwise the whole subtree follows target: enumerate it fully
			return nil
		}

		if node.isLeaf(frame.remaining) {
			// full label match at leaf depth means the exact target key
			if !allowEq {
				it.pop()
			}
			return nil
		}

		targetBit, err := target.BitAt(frame.prefixAfterLabel)
		if err != nil {
			return err
		}
		child := int(targetBit)
		if child == frame.first {
			frame.nextChild = 1
		} else {
			frame.nextChild = 2
		}
		ref, childTrace, err := node.refAndTrace(child)
		if err != nil {
			return err
		}
		if err = it.prefix.StoreUInt(uint64(child), 1); err != nil {
			return err
		}
		if err = it.push(ref, node.nextKeyBits(frame.remaining), false, childTrace); err != nil {
			return err
		}
	}
}

func (it *DictIterator) pop() {
	last := len(it.stack) - 1
	it.prefix.truncateBits(it.stack[last].prefixBefore)
	it.stack = it.stack[:last]
}

func (it *DictIterator) fail(err error) {
	it.err = err
	it.current = DictItem{}
	it.view = DictItemView{}
	it.keyCell = Cell{}
	it.hasView = false
	it.popNext = false
	it.stack = it.stack[:0]
	it.prefix.truncateBits(0)
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

	var aSlice, bSlice Slice
	if err := a.BeginParseInto(&aSlice); err != nil {
		panic(err)
	}
	if err := b.BeginParseInto(&bSlice); err != nil {
		panic(err)
	}
	limit := min(aSlice.BitsLeft(), bSlice.BitsLeft())

	matched, err := commonSlicePrefix(&aSlice, &bSlice, limit)
	if err != nil {
		panic(err)
	}

	if matched < limit {
		av, err := aSlice.BitAt(matched)
		if err != nil {
			panic(err)
		}
		if invertFirst && matched == 0 {
			// inverting both first bits flips the comparison outcome
			av ^= 1
		}
		if av != 0 {
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

func cellHasPrefix(key, prefix *Cell) (bool, error) {
	if prefix == nil {
		return true, nil
	}
	if key == nil || prefix.BitsSize() > key.BitsSize() {
		return false, nil
	}

	var keySlice, prefixSlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return false, err
	}
	if err := prefix.BeginParseInto(&prefixSlice); err != nil {
		return false, err
	}

	limit := prefixSlice.BitsLeft()
	matched, err := commonSlicePrefix(&keySlice, &prefixSlice, limit)
	if err != nil {
		return false, err
	}
	return matched == limit, nil
}

func cellPrefix(key *Cell, bits uint) (*Cell, error) {
	if key == nil {
		return nil, fmt.Errorf("key is nil")
	}
	if bits > key.BitsSize() {
		return nil, fmt.Errorf("prefix length is too large")
	}
	keySlice, err := key.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}
	prefix := BeginCell()
	if err = keySlice.loadSliceInto(prefix.data[:], bits, false); err != nil {
		return nil, fmt.Errorf("failed to load key prefix: %w", err)
	}
	prefix.bitsSz = bits
	return prefix.EndCell(), nil
}

func fixedDictCommonPrefix(root *Cell, keySz uint, limit uint, walk dictWalk) (*Cell, error) {
	if root == nil || limit == 0 {
		return BeginCell().EndCell(), nil
	}
	node, err := parseFixedDictNodeWithTrace(root, keySz, root.Trace())
	if err != nil {
		return nil, err
	}
	if err = node.resolveIfSpecial(keySz, root.Trace(), walk.resolver); err != nil {
		return nil, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, err
	}
	if err = node.validateForkShape(keySz, walk.lenient); err != nil {
		return nil, err
	}

	labelLen := node.labelLen
	if labelLen > limit {
		labelLen = limit
	}
	ls := node.labelSlice()
	pb := BeginCell()
	if err = pb.storeSliceFromSlice(&ls, labelLen); err != nil {
		return nil, err
	}
	return pb.EndCell(), nil
}

func fixedDictHasCommonPrefix(root *Cell, keySz uint, prefix *Cell, walk dictWalk) (bool, error) {
	if root == nil || prefix == nil || prefix.BitsSize() == 0 {
		return true, nil
	}
	if prefix.BitsSize() > keySz {
		return false, nil
	}

	common, err := fixedDictCommonPrefix(root, keySz, prefix.BitsSize(), walk)
	if err != nil {
		return false, err
	}
	return cellHasPrefix(common, prefix)
}

func appendFixedDictEntries(items *[]DictItem, root *Cell, remaining uint, prefix *Builder, rev bool, invertFirst bool, rootLevel bool, walk dictWalk) error {
	if root == nil {
		return nil
	}

	node, err := parseFixedDictNodeWithTrace(root, remaining, root.Trace())
	if err != nil {
		return err
	}
	if err = node.resolveIfSpecial(remaining, root.Trace(), walk.resolver); err != nil {
		return err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return err
	}
	if err = node.validateForkShape(remaining, walk.lenient); err != nil {
		return err
	}

	// the walk shares one key builder: append bits, descend, roll back
	saved := prefix.BitsUsed()
	defer prefix.truncateBits(saved)

	nodeLabel := node.labelSlice()
	if err = prefix.storeSliceFromSlice(&nodeLabel, node.labelLen); err != nil {
		return err
	}

	if node.isLeaf(remaining) {
		*items = append(*items, DictItem{
			Key:   prefix.EndCell(),
			Value: node.value(),
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

	afterLabel := prefix.BitsUsed()
	nextRemaining := node.nextKeyBits(remaining)
	firstRef, err := node.ref(first)
	if err != nil {
		return err
	}
	if err = appendFixedDictEntries(items, firstRef, nextRemaining, prefix.MustStoreUInt(uint64(first), 1), rev, invertFirst, false, walk); err != nil {
		return err
	}
	prefix.truncateBits(afterLabel)

	secondRef, err := node.ref(second)
	if err != nil {
		return err
	}
	return appendFixedDictEntries(items, secondRef, nextRemaining, prefix.MustStoreUInt(uint64(second), 1), rev, invertFirst, false, walk)
}

func fixedDictRange(root *Cell, keySz uint, rev bool, invertFirst bool, walk dictWalk) ([]DictItem, error) {
	if root == nil {
		return []DictItem{}, nil
	}

	items := make([]DictItem, 0)
	if err := appendFixedDictEntries(&items, root, keySz, BeginCell(), rev, invertFirst, true, walk); err != nil {
		return nil, err
	}
	return items, nil
}

func fixedDictLookupNearest(root *Cell, keySz uint, key *Cell, fetchNext bool, allowEq bool, invertFirst bool, walk dictWalk) (*Cell, *Slice, error) {
	if root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() != keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}

	var target Slice
	if err := key.BeginParseInto(&target); err != nil {
		return nil, nil, fmt.Errorf("failed to load key: %w", err)
	}
	return fixedDictLookupNearestSlice(root, keySz, &target, fetchNext, allowEq, invertFirst, walk)
}

func fixedDictLookupNearestSlice(root *Cell, keySz uint, target *Slice, fetchNext bool, allowEq bool, invertFirst bool, walk dictWalk) (*Cell, *Slice, error) {
	if root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}
	if target == nil || target.BitsLeft() != keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}

	walk.trace = CombineTraces(root.Trace(), walk.trace)
	item, ok, err := fixedDictLookupNearestNode(root, keySz, BeginCell(), target, fetchNext, allowEq, invertFirst, walk)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, ErrNoSuchKeyInDict
	}
	return item.Key, item.Value, nil
}

func fixedDictLookupNearestNode(root *Cell, remaining uint, prefix *Builder, target *Slice, fetchNext bool, allowEq bool, invertFirst bool, walk dictWalk) (DictItem, bool, error) {
	saved := prefix.BitsUsed()
	defer prefix.truncateBits(saved)

	node, err := parseFixedDictNodeWithTrace(root, remaining, walk.trace)
	if err != nil {
		return DictItem{}, false, err
	}
	if err = node.resolveIfSpecial(remaining, walk.trace, walk.resolver); err != nil {
		return DictItem{}, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return DictItem{}, false, err
	}
	if err = node.validateForkShape(remaining, walk.lenient); err != nil {
		return DictItem{}, false, err
	}
	targetLabel := *target
	if err = targetLabel.SkipBits(saved); err != nil {
		return DictItem{}, false, err
	}
	matched, err := commonSlicePrefix(&node.label, &targetLabel, node.labelLen)
	if err != nil {
		return DictItem{}, false, err
	}
	if matched < node.labelLen {
		pos := saved + matched
		bit, err := node.label.BitAt(matched)
		if err != nil {
			return DictItem{}, false, err
		}
		targetBit, err := target.BitAt(pos)
		if err != nil {
			return DictItem{}, false, err
		}

		bitOrder := fixedDictOrderBit(uint8(bit), pos, invertFirst)
		targetOrder := fixedDictOrderBit(uint8(targetBit), pos, invertFirst)
		if fetchNext && bitOrder > targetOrder {
			return fixedDictBoundary(root, remaining, prefix, false, invertFirst, walk)
		}
		if !fetchNext && bitOrder < targetOrder {
			return fixedDictBoundary(root, remaining, prefix, true, invertFirst, walk)
		}
		return DictItem{}, false, nil
	}

	label := node.labelSlice()
	if err = prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return DictItem{}, false, err
	}
	if node.isLeaf(remaining) {
		if allowEq {
			return DictItem{Key: prefix.EndCell(), Value: node.value()}, true, nil
		}
		return DictItem{}, false, nil
	}

	pos := prefix.BitsUsed()
	if pos >= target.BitsLeft() {
		return DictItem{}, false, fmt.Errorf("target key is shorter than dictionary key")
	}
	targetBitValue, err := target.BitAt(pos)
	if err != nil {
		return DictItem{}, false, err
	}
	targetBit := int(targetBitValue)
	nextRemaining := node.nextKeyBits(remaining)
	firstRef, firstTrace, err := node.refAndTrace(targetBit)
	if err != nil {
		return DictItem{}, false, err
	}
	if err = prefix.StoreUInt(uint64(targetBit), 1); err != nil {
		return DictItem{}, false, err
	}
	walk.trace = firstTrace
	item, ok, err := fixedDictLookupNearestNode(firstRef, nextRemaining, prefix, target, fetchNext, allowEq, invertFirst, walk)
	if err != nil || ok {
		return item, ok, err
	}
	prefix.truncateBits(pos)

	second := targetBit ^ 1
	firstOrder := fixedDictOrderBit(uint8(targetBit), pos, invertFirst)
	secondOrder := fixedDictOrderBit(uint8(second), pos, invertFirst)
	if (fetchNext && secondOrder > firstOrder) || (!fetchNext && secondOrder < firstOrder) {
		secondRef, secondTrace, err := node.refAndTrace(second)
		if err != nil {
			return DictItem{}, false, err
		}
		if err = prefix.StoreUInt(uint64(second), 1); err != nil {
			return DictItem{}, false, err
		}
		walk.trace = secondTrace
		return fixedDictBoundary(secondRef, nextRemaining, prefix, !fetchNext, invertFirst, walk)
	}
	return DictItem{}, false, nil
}

func fixedDictBoundary(root *Cell, remaining uint, prefix *Builder, max bool, invertFirst bool, walk dictWalk) (DictItem, bool, error) {
	saved := prefix.BitsUsed()
	defer prefix.truncateBits(saved)

	node, err := parseFixedDictNodeWithTrace(root, remaining, walk.trace)
	if err != nil {
		return DictItem{}, false, err
	}
	if err = node.resolveIfSpecial(remaining, walk.trace, walk.resolver); err != nil {
		return DictItem{}, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return DictItem{}, false, err
	}
	if err = node.validateForkShape(remaining, walk.lenient); err != nil {
		return DictItem{}, false, err
	}

	nodeLabel := node.labelSlice()
	if err = prefix.storeSliceFromSlice(&nodeLabel, node.labelLen); err != nil {
		return DictItem{}, false, err
	}
	if node.isLeaf(remaining) {
		return DictItem{Key: prefix.EndCell(), Value: node.value()}, true, nil
	}

	refIdx := fixedDictBoundaryChild(prefix.BitsUsed(), max, invertFirst)
	ref, childTrace, err := node.refAndTrace(refIdx)
	if err != nil {
		return DictItem{}, false, err
	}
	if err = prefix.StoreUInt(uint64(refIdx), 1); err != nil {
		return DictItem{}, false, err
	}
	walk.trace = childTrace
	return fixedDictBoundary(ref, node.nextKeyBits(remaining), prefix, max, invertFirst, walk)
}

func fixedDictOrderBit(bit uint8, pos uint, invertFirst bool) uint8 {
	if invertFirst && pos == 0 {
		return bit ^ 1
	}
	return bit
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

type fixedDictFilterState struct {
	fn         DictFilterFunc
	prefix     Builder
	changes    int
	keepRest   bool
	removeRest bool
	walk       dictWalk
}

func fixedDictFilter(root *Cell, keySz uint, fn DictFilterFunc, walk dictWalk) (*Cell, int, error) {
	if root == nil || fn == nil {
		return root, 0, nil
	}

	state := fixedDictFilterState{fn: fn, walk: walk}
	filtered, _, err := filterFixedDictNode(root.withTraceCombined(walk.trace), keySz, &state)
	if err != nil {
		return nil, 0, err
	}
	return filtered, state.changes, nil
}

func filterFixedDictNode(root *Cell, remaining uint, state *fixedDictFilterState) (*Cell, bool, error) {
	if state.keepRest {
		return root, false, nil
	}
	if state.removeRest {
		count, err := countFixedDictLeavesResolved(root, remaining, state.walk)
		if err != nil {
			return nil, false, err
		}
		state.changes += count
		return nil, count != 0, nil
	}

	node, err := parseFixedDictNodeWithTrace(root, remaining, root.Trace())
	if err != nil {
		return nil, false, err
	}
	if err = node.resolveIfSpecial(remaining, root.Trace(), state.walk.resolver); err != nil {
		return nil, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, false, err
	}
	if err = node.validateForkShape(remaining, state.walk.lenient); err != nil {
		return nil, false, err
	}

	saved := state.prefix.BitsUsed()
	defer state.prefix.truncateBits(saved)
	label := node.labelSlice()
	if err = state.prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return nil, false, err
	}

	if node.isLeaf(remaining) {
		action, err := state.fn(node.value(), state.prefix.EndCell())
		if err != nil {
			return nil, false, err
		}
		switch action {
		case DictFilterKeep:
			return root, false, nil
		case DictFilterRemove:
			state.changes++
			return nil, true, nil
		case DictFilterKeepRest:
			state.keepRest = true
			return root, false, nil
		case DictFilterRemoveRest:
			state.removeRest = true
			state.changes++
			return nil, true, nil
		default:
			return nil, false, fmt.Errorf("unknown dict filter action")
		}
	}

	afterLabel := state.prefix.BitsUsed()
	childRemaining := node.nextKeyBits(remaining)
	left, err := node.ref(0)
	if err != nil {
		return nil, false, err
	}
	if err = state.prefix.StoreUInt(0, 1); err != nil {
		return nil, false, err
	}
	newLeft, leftChanged, err := filterFixedDictNode(left, childRemaining, state)
	if err != nil {
		return nil, false, err
	}

	state.prefix.truncateBits(afterLabel)
	right, err := node.ref(1)
	if err != nil {
		return nil, false, err
	}
	if err = state.prefix.StoreUInt(1, 1); err != nil {
		return nil, false, err
	}
	newRight, rightChanged, err := filterFixedDictNode(right, childRemaining, state)
	if err != nil {
		return nil, false, err
	}

	if !leftChanged && !rightChanged {
		return root, false, nil
	}
	if newLeft == nil && newRight == nil {
		return nil, true, nil
	}
	if newLeft == nil {
		merged, err := mergeFixedDictSurvivorResolved(node, 1, newRight, remaining, state.walk)
		return merged, true, err
	}
	if newRight == nil {
		merged, err := mergeFixedDictSurvivorResolved(node, 0, newLeft, remaining, state.walk)
		return merged, true, err
	}

	if leftChanged != rightChanged {
		idx := 0
		child := newLeft
		if rightChanged {
			idx = 1
			child = newRight
		}
		cloned, _, err := node.cloneWithRef(idx, child, state.walk.trace)
		return cloned, true, err
	}

	payload := BeginCell().SetTrace(state.walk.trace)
	if err = payload.StoreRefUncheckedDepth(newLeft); err != nil {
		return nil, false, err
	}
	if err = payload.StoreRefUncheckedDepth(newRight); err != nil {
		return nil, false, err
	}
	parentLabel := node.labelSlice()
	rebuilt, err := storeDictNodeTraced(&parentLabel, payload, remaining, state.walk.trace)
	return rebuilt, true, err
}

// CountInlineDictLeaves counts the leaves of a non-empty Hashmap/HashmapAug
// whose root node is serialized inline at the slice's current position, like
// the transactions dict of an AccountBlock. Neither keys nor values are
// materialized and the slice is not advanced. Fork children occupy the first
// two refs in both plain and augmented dictionaries, so no augmentation
// knowledge is needed.
func (c *Slice) CountInlineDictLeaves(keySz uint) (int, error) {
	if err := validateDictKeySize(keySz); err != nil {
		return 0, fmt.Errorf("failed to validate dict: %w", err)
	}
	if c.cell.IsSpecial() {
		return 0, fmt.Errorf("dict %w", ErrDictHasSpecialCells)
	}

	view := *c
	labelLen, _, err := readLabelView(keySz, &view)
	if err != nil {
		return 0, err
	}
	if labelLen == keySz {
		return 1, nil
	}

	childRemaining := keySz - labelLen - 1
	left, err := view.peekRefCellAt(0)
	if err != nil {
		return 0, err
	}
	leftCount, err := countFixedDictLeaves(left, childRemaining)
	if err != nil {
		return 0, err
	}
	right, err := view.peekRefCellAt(1)
	if err != nil {
		return 0, err
	}
	rightCount, err := countFixedDictLeaves(right, childRemaining)
	if err != nil {
		return 0, err
	}
	return leftCount + rightCount, nil
}

// forEachDictLeafValue walks the leaves in key order handing each raw leaf
// value (extra included for augmented dicts) to fn, without building key
// cells. fn returns false to stop the walk early.
func forEachDictLeafValue(branch *Cell, remaining uint, fn func(value *Slice) (bool, error)) (bool, error) {
	node, err := parseFixedDictNode(branch, remaining)
	if err != nil {
		return false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return false, err
	}
	if node.isLeaf(remaining) {
		return fn(node.value())
	}

	childRemaining := node.nextKeyBits(remaining)
	left, err := node.ref(0)
	if err != nil {
		return false, err
	}
	cont, err := forEachDictLeafValue(left, childRemaining, fn)
	if err != nil || !cont {
		return cont, err
	}
	right, err := node.ref(1)
	if err != nil {
		return false, err
	}
	return forEachDictLeafValue(right, childRemaining, fn)
}

func countFixedDictLeaves(root *Cell, remaining uint) (int, error) {
	node, err := parseFixedDictNode(root, remaining)
	if err != nil {
		return 0, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return 0, err
	}
	if node.isLeaf(remaining) {
		return 1, nil
	}

	childRemaining := node.nextKeyBits(remaining)
	left, err := node.ref(0)
	if err != nil {
		return 0, err
	}
	leftCount, err := countFixedDictLeaves(left, childRemaining)
	if err != nil {
		return 0, err
	}
	right, err := node.ref(1)
	if err != nil {
		return 0, err
	}
	rightCount, err := countFixedDictLeaves(right, childRemaining)
	if err != nil {
		return 0, err
	}
	return leftCount + rightCount, nil
}

// countFixedDictLeavesResolved counts leaves like countFixedDictLeaves, but
// resolves special nodes through the walk and validates every fork shape.
func countFixedDictLeavesResolved(root *Cell, remaining uint, walk dictWalk) (int, error) {
	node, err := parseFixedDictNodeWithTrace(root, remaining, root.Trace())
	if err != nil {
		return 0, err
	}
	if err = node.resolveIfSpecial(remaining, root.Trace(), walk.resolver); err != nil {
		return 0, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return 0, err
	}
	if err = node.validateForkShape(remaining, walk.lenient); err != nil {
		return 0, err
	}
	if node.isLeaf(remaining) {
		return 1, nil
	}

	childRemaining := node.nextKeyBits(remaining)
	left, err := node.ref(0)
	if err != nil {
		return 0, err
	}
	leftCount, err := countFixedDictLeavesResolved(left, childRemaining, walk)
	if err != nil {
		return 0, err
	}
	right, err := node.ref(1)
	if err != nil {
		return 0, err
	}
	rightCount, err := countFixedDictLeavesResolved(right, childRemaining, walk)
	if err != nil {
		return 0, err
	}
	return leftCount + rightCount, nil
}

func forEachPlainDictRefValue(branch *Cell, remaining uint, fn func(value *Cell) error, resolver DictSpecialResolver) (int, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return 0, err
	}
	if err = node.resolveIfSpecial(remaining, branch.Trace(), resolver); err != nil {
		return 0, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return 0, err
	}
	if err = node.validateForkShape(remaining, false); err != nil {
		return 0, err
	}
	if node.isLeaf(remaining) {
		if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 1 {
			return 0, fmt.Errorf("invalid dict ref value: %d data bits, %d refs", node.loader.BitsLeft(), node.loader.RefsNum())
		}
		ref, err := node.ref(0)
		if err != nil {
			return 0, err
		}
		if fn != nil {
			if err = fn(ref); err != nil {
				return 0, err
			}
		}
		return 1, nil
	}

	if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
		return 0, fmt.Errorf("invalid dict fork node: %d data bits, %d refs", node.loader.BitsLeft(), node.loader.RefsNum())
	}

	childRemaining := node.nextKeyBits(remaining)
	left, err := node.ref(0)
	if err != nil {
		return 0, err
	}
	leftCount, err := forEachPlainDictRefValue(left, childRemaining, fn, resolver)
	if err != nil {
		return 0, err
	}
	right, err := node.ref(1)
	if err != nil {
		return 0, err
	}
	rightCount, err := forEachPlainDictRefValue(right, childRemaining, fn, resolver)
	if err != nil {
		return 0, err
	}
	return leftCount + rightCount, nil
}

func mergeFixedDictSurvivor(parent fixedDictNode, edge uint64, survivor *Cell, remaining uint, trace *Trace) (*Cell, error) {
	childRemaining := parent.nextKeyBits(remaining)
	child, err := parseFixedDictNode(survivor, childRemaining)
	if err != nil {
		return nil, err
	}
	if err = child.rejectSpecial("dict"); err != nil {
		return nil, err
	}
	return mergeFixedDictSurvivorNode(parent, edge, child, remaining, trace)
}

// mergeFixedDictSurvivorResolved merges like mergeFixedDictSurvivor, but
// resolves a special survivor through the walk and validates its fork shape.
func mergeFixedDictSurvivorResolved(parent fixedDictNode, edge uint64, survivor *Cell, remaining uint, walk dictWalk) (*Cell, error) {
	childRemaining := parent.nextKeyBits(remaining)
	child, err := parseFixedDictNodeWithTrace(survivor, childRemaining, survivor.Trace())
	if err != nil {
		return nil, err
	}
	if err = child.resolveIfSpecial(childRemaining, survivor.Trace(), walk.resolver); err != nil {
		return nil, err
	}
	if err = child.rejectSpecial("dict"); err != nil {
		return nil, err
	}
	if err = child.validateForkShape(childRemaining, walk.lenient); err != nil {
		return nil, err
	}
	return mergeFixedDictSurvivorNode(parent, edge, child, remaining, walk.trace)
}

func mergeFixedDictSurvivorNode(parent fixedDictNode, edge uint64, child fixedDictNode, remaining uint, trace *Trace) (*Cell, error) {
	label := BeginCell()
	parentLabel := parent.labelSlice()
	var err error
	if err = label.storeSliceFromSlice(&parentLabel, parent.labelLen); err != nil {
		return nil, err
	}
	if err = label.StoreUInt(edge, 1); err != nil {
		return nil, err
	}
	childLabel := child.labelSlice()
	if err = label.storeSliceFromSlice(&childLabel, child.labelLen); err != nil {
		return nil, err
	}

	var payload Builder
	child.loader.ToBuilderInto(&payload)
	return storeDictNodeTraced(builderSliceView(label), &payload, remaining, trace)
}

func extractPrefixSubdictRoot(root *Cell, keySz uint, prefix *Cell, removePrefix bool, walk dictWalk) (*Cell, bool, error) {
	if root == nil {
		return nil, false, nil
	}

	var prefixSlice Slice
	if prefix == nil {
		return extractPrefixSubdictRootSlice(root, keySz, prefixSlice, removePrefix, nil, walk)
	}
	if err := prefix.BeginParseInto(&prefixSlice); err != nil {
		return nil, false, fmt.Errorf("failed to load prefix: %w", err)
	}
	return extractPrefixSubdictRootSlice(root, keySz, prefixSlice, removePrefix, prefix, walk)
}

func extractPrefixSubdictRootSlice(root *Cell, keySz uint, prefix Slice, removePrefix bool, reloadPrefix *Cell, walk dictWalk) (*Cell, bool, error) {
	if root == nil {
		return nil, false, nil
	}
	root = root.withTraceCombined(walk.trace)

	prefixLen := prefix.BitsLeft()
	if prefixLen == 0 {
		return root, false, nil
	}

	if prefixLen > keySz {
		return nil, true, nil
	}

	prefixStart := prefix
	consumed := uint(0)
	branch := root
	for {
		if branch == nil {
			return nil, true, nil
		}
		node, err := parseFixedDictNodeWithTrace(branch, keySz-consumed, branch.Trace())
		if err != nil {
			return nil, false, err
		}
		if err = node.resolveIfSpecial(keySz-consumed, branch.Trace(), walk.resolver); err != nil {
			return nil, false, err
		}
		if err = node.rejectSpecial("dict"); err != nil {
			return nil, false, err
		}
		if err = node.validateForkShape(keySz-consumed, walk.lenient); err != nil {
			return nil, false, err
		}

		toMatch := prefixLen - consumed
		if toMatch > node.labelLen {
			toMatch = node.labelLen
		}

		nodeLabel := node.labelSlice()
		matched, err := consumeCommonPrefix(&nodeLabel, &prefix, toMatch)
		if err != nil {
			return nil, false, err
		}
		if matched < toMatch {
			return nil, true, nil
		}

		if consumed+node.labelLen < prefixLen {
			idx, err := prefix.LoadUInt(1)
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

			prefixLoader := prefixStart
			if reloadPrefix != nil {
				if err := reloadPrefix.BeginParseInto(&prefixLoader); err != nil {
					return nil, false, fmt.Errorf("failed to load prefix: %w", err)
				}
			}

			combined := BeginCell()
			if err = prefixLoader.loadSliceInto(combined.data[:], consumed, false); err != nil {
				return nil, false, err
			}
			combined.bitsSz = consumed

			combinedLabel := node.labelSlice()
			if err = combined.storeSliceFromSlice(&combinedLabel, node.labelLen); err != nil {
				return nil, false, err
			}
			var payload Builder
			node.loader.ToBuilderInto(&payload)
			subdict, err := storeDictNodeTraced(builderSliceView(combined), &payload, keySz, walk.trace)
			return subdict, true, err
		}

		suffixLabel := node.labelSlice()
		suffix := &suffixLabel
		if prefixLen > consumed {
			if err := suffix.SkipBits(prefixLen - consumed); err != nil {
				return nil, false, err
			}
		}
		var payload Builder
		node.loader.ToBuilderInto(&payload)
		subdict, err := storeDictNodeTraced(suffix, &payload, keySz-prefixLen, walk.trace)
		return subdict, true, err
	}
}
