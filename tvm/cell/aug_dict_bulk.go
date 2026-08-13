package cell

import (
	"bytes"
	"fmt"
	"sort"
)

// AugmentedEntry is one update of a bulk dictionary write. The zero Mode means
// DictSetModeSet; Add and Replace are assertions here rather than silent
// no-ops, because a bulk write has no per-entry result to report.
type AugmentedEntry struct {
	Key   *Cell
	Value *Cell
	Mode  DictSetMode
}

// SetMany applies a batch of add-or-replace updates in a single descent.
//
// Repeated Set walks a full root-to-leaf path per key and recombines the
// augmentation of every node on the way back up, so a node shared by k keys is
// rebuilt k times and the root's extra is recomputed once per key. SetMany
// visits the union of the paths once instead: each node is rebuilt once and its
// augmentation combined once, no matter how many of the keys pass through it.
//
// The result is the dictionary repeated Set would have produced — same cells,
// same augmentations — so callers may pick either by cost alone. Keys must all
// be the dictionary's key size and must be distinct; the batch is sorted here,
// so the caller's order does not matter.
func (d *AugmentedDictionary) SetMany(entries []AugmentedEntry) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	if len(entries) == 0 {
		return nil
	}
	if err := d.ensureWritable(); err != nil {
		return err
	}

	items := make([]augBulkItem, len(entries))
	for i := range entries {
		entry := &entries[i]
		if entry.Key == nil || entry.Key.BitsSize() != d.keySz {
			return fmt.Errorf("invalid key size at entry %d", i)
		}
		if entry.Value == nil {
			return fmt.Errorf("value is nil at entry %d", i)
		}
		if err := entry.Key.BeginParseInto(&items[i].key); err != nil {
			return fmt.Errorf("failed to load key at entry %d: %w", i, err)
		}
		items[i].value = entry.Value.ToBuilder()
		items[i].mode = entry.Mode
		if items[i].mode == 0 {
			items[i].mode = DictSetModeSet
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return compareKeySlices(&items[i].key, &items[j].key) < 0
	})
	for i := 1; i < len(items); i++ {
		if compareKeySlices(&items[i-1].key, &items[i].key) == 0 {
			return fmt.Errorf("duplicate key in bulk update")
		}
	}

	var state augmentedMutationState
	root, rootExtra, err := d.setMany(d.root, d.root.Trace(), items, d.keySz, &state)
	if err != nil {
		return err
	}
	rootExtraCell, err := rootExtra.ToCell()
	if err != nil {
		return err
	}
	return d.setRootWithExtra(root, rootExtraCell)
}

type augBulkItem struct {
	// key is the not yet consumed suffix. Every item in a batch carries the
	// same number of remaining bits, which is the recursion's keyOffset.
	key   Slice
	value *Builder
	mode  DictSetMode
}

// checkMode enforces Add/Replace against what the descent found. The two call
// sites are the only places an entry can come to rest: replacing the leaf of an
// existing key, or landing in a subtree built from scratch, where by
// construction no existing key shares the path.
func (i *augBulkItem) checkMode(existed bool) error {
	if existed {
		if i.mode&DictSetModeReplace == 0 {
			return fmt.Errorf("add-mode entry already exists in the dictionary")
		}
		return nil
	}
	if i.mode&DictSetModeAdd == 0 {
		return fmt.Errorf("replace-mode entry is absent from the dictionary")
	}
	return nil
}

// compareKeySlices orders two unconsumed whole-key views by their backing
// bytes, which is only meaningful because both are views of a complete key
// cell. SetMany and DeleteMany are the only callers, and both reject an entry
// whose Key.BitsSize differs from the dictionary key size before handing it to
// Cell.BeginParseInto, which opens the Slice at bit 0 of that cell: hence
// bitStart is 0 on both sides — so the byte range starts on a byte boundary and
// covers the whole key — and bitEnd is the same key size on both sides.
//
// A trailing partial byte is compared masked to its key bits, because the
// padding under them is not canonical: a cell built here pads with zeros while
// one parsed from a BoC keeps its completion tag. Comparing that byte whole
// would order the same key differently depending on where its cell came from,
// and the callers' distinctness check would not recognize it as a duplicate.
func compareKeySlices(a, b *Slice) int {
	aKey := a.cell.data[a.bitStart/8 : (a.bitEnd+7)/8]
	bKey := b.cell.data[b.bitStart/8 : (b.bitEnd+7)/8]

	rem := a.bitEnd % 8
	if rem == 0 {
		return bytes.Compare(aKey, bKey)
	}

	last := len(aKey) - 1
	if cmp := bytes.Compare(aKey[:last], bKey[:last]); cmp != 0 {
		return cmp
	}

	mask := byte(0xFF << (8 - rem))
	switch {
	case aKey[last]&mask < bKey[last]&mask:
		return -1
	case aKey[last]&mask > bKey[last]&mask:
		return 1
	default:
		return 0
	}
}

// setMany writes a non-empty, sorted, distinct batch into branch. Every item
// holds exactly keyOffset key bits. It returns the new subtree and a view of
// its augmentation, which lives inside the returned cell and so survives the
// sibling recursion that follows.
// The trace travels beside branch instead of on it: the descent reaches a
// child through its parent's Slice, which already holds the child trace, and
// attaching that trace to the child cell would allocate a copy of the cell per
// visited node for nothing else.
func (d *AugmentedDictionary) setMany(
	branch *Cell,
	trace *Trace,
	items []augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
) (*Cell, Slice, error) {
	if branch == nil {
		return d.buildMany(items, keyOffset, state)
	}

	node, err := parseFixedDictNodeWithTrace(branch, keyOffset, trace)
	if err != nil {
		return nil, Slice{}, fmt.Errorf("failed to load branch: %w", err)
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return nil, Slice{}, err
	}
	if err = node.validateForkShape(keyOffset, true); err != nil {
		return nil, Slice{}, err
	}
	sz, label := node.labelLen, node.label

	// The batch follows the label only as far as its least-matching member:
	// past that bit the node has to fork, and the rest of the label moves down
	// into the old child.
	matched := sz
	for i := range items {
		labelView, keyView := label, items[i].key
		shared, err := commonSlicePrefix(&labelView, &keyView, sz)
		if err != nil {
			return nil, Slice{}, fmt.Errorf("failed to match key prefix: %w", err)
		}
		if shared < matched {
			matched = shared
			if matched == 0 {
				break
			}
		}
	}

	if matched == sz {
		for i := range items {
			if err = items[i].key.SkipBits(sz); err != nil {
				return nil, Slice{}, err
			}
		}
		if keyOffset == sz {
			// The whole key is spent, so the batch is the single key living here.
			if err = items[0].checkMode(true); err != nil {
				return nil, Slice{}, err
			}
			labelView := label
			return d.storeLeafWithExtra(&labelView, items[0].value, keyOffset, state)
		}

		left, right, err := splitBulkOnNextBit(items, augBulkItemKey)
		if err != nil {
			return nil, Slice{}, err
		}
		childOffset := keyOffset - sz - 1
		leftCell, leftExtra, err := d.setManyChild(&node, 0, left, childOffset, state)
		if err != nil {
			return nil, Slice{}, err
		}
		rightCell, rightExtra, err := d.setManyChild(&node, 1, right, childOffset, state)
		if err != nil {
			return nil, Slice{}, err
		}
		labelView := label
		fork, forkExtra, err := d.storeForkWithExtraSlices(
			&labelView, leftCell, &leftExtra, rightCell, &rightExtra, keyOffset, state)
		return fork, forkExtra, err
	}

	// Divergence: the node keeps its first matched bits as the fork label and
	// moves down one level, carrying the rest of its own label. At least one
	// item took the other edge, so that side is always newly built.
	prefixLabel, labelRemainder, err := node.splitLabel(matched)
	if err != nil {
		return nil, Slice{}, fmt.Errorf("failed to split old child label: %w", err)
	}
	labelBit, err := label.BitAt(matched)
	if err != nil {
		return nil, Slice{}, err
	}
	childOffset := keyOffset - matched - 1

	oldChild := BeginCell().SetTrace(d.trace)
	if err = storeDictLabel(oldChild, labelRemainder, childOffset); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store old child label: %w", err)
	}
	node.loader.ToBuilderInto(&state.extra)
	if err = oldChild.StoreBuilderUncheckedDepth(&state.extra); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store old child payload: %w", err)
	}
	oldExtra, err := augmentedNodeExtraViewScratch(node, keyOffset, d.aug.SkipExtra, &state.skipScratch)
	if err != nil {
		return nil, Slice{}, fmt.Errorf("failed to extract old child extra: %w", err)
	}
	oldChildCell := oldChild.EndCell()

	for i := range items {
		if err = items[i].key.SkipBits(matched); err != nil {
			return nil, Slice{}, err
		}
	}
	left, right, err := splitBulkOnNextBit(items, augBulkItemKey)
	if err != nil {
		return nil, Slice{}, err
	}
	sides := [2][]augBulkItem{left, right}

	var children [2]*Cell
	var extras [2]Slice
	for bit := range children {
		batch := sides[bit]
		if uint8(bit) == labelBit {
			if len(batch) == 0 {
				children[bit], extras[bit] = oldChildCell, oldExtra
				continue
			}
			children[bit], extras[bit], err = d.setMany(oldChildCell, oldChildCell.Trace(), batch, childOffset, state)
		} else {
			children[bit], extras[bit], err = d.buildMany(batch, childOffset, state)
		}
		if err != nil {
			return nil, Slice{}, err
		}
	}

	fork, forkExtra, err := d.storeForkWithExtraSlices(
		prefixLabel, children[0], &extras[0], children[1], &extras[1], keyOffset, state)
	return fork, forkExtra, err
}

// setManyChild descends into one side of an existing fork, leaving it untouched
// when no key of the batch goes that way.
func (d *AugmentedDictionary) setManyChild(
	node *fixedDictNode,
	refIdx int,
	items []augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
) (*Cell, Slice, error) {
	ref, trace, err := node.refAndTrace(refIdx)
	if err != nil {
		return nil, Slice{}, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
	}
	if len(items) == 0 {
		// This child is kept as it stands and becomes a ref of the rebuilt
		// fork, so here the trace has to ride on the cell: the merkle update
		// recognizes an untouched subtree by the usage node its trace resolves
		// to, and a child stored without one is written out in full instead of
		// being pruned against the source.
		kept := ref.WithTrace(trace)
		extra, err := extractAugmentedNodeExtraViewWithTraceScratch(kept, trace, keyOffset, d.aug.SkipExtra, &state.skipScratch)
		if err != nil {
			return nil, Slice{}, fmt.Errorf("failed to extract %d ref extra: %w", refIdx, err)
		}
		return kept, extra, nil
	}
	// The descent rebuilds this child, so nothing that survives the call refers
	// to the cell and the trace can travel beside it.
	return d.setMany(ref, trace, items, keyOffset, state)
}

// buildMany creates a fresh subtree holding exactly the batch.
func (d *AugmentedDictionary) buildMany(
	items []augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
) (*Cell, Slice, error) {
	if len(items) == 1 {
		if err := items[0].checkMode(false); err != nil {
			return nil, Slice{}, err
		}
		return d.storeLeafWithExtra(&items[0].key, items[0].value, keyOffset, state)
	}

	// Distinct keys share less than the whole remaining space, so the common
	// prefix always leaves a bit to fork on and both sides come out non-empty.
	shared := keyOffset
	for i := 1; i < len(items); i++ {
		first, other := items[0].key, items[i].key
		common, err := commonSlicePrefix(&first, &other, keyOffset)
		if err != nil {
			return nil, Slice{}, fmt.Errorf("failed to match batch prefix: %w", err)
		}
		if common < shared {
			shared = common
			if shared == 0 {
				break
			}
		}
	}

	label := items[0].key
	label.bitEnd = label.bitStart + uint16(shared)
	for i := range items {
		if err := items[i].key.SkipBits(shared); err != nil {
			return nil, Slice{}, err
		}
	}
	left, right, err := splitBulkOnNextBit(items, augBulkItemKey)
	if err != nil {
		return nil, Slice{}, err
	}

	childOffset := keyOffset - shared - 1
	leftCell, leftExtra, err := d.buildMany(left, childOffset, state)
	if err != nil {
		return nil, Slice{}, err
	}
	rightCell, rightExtra, err := d.buildMany(right, childOffset, state)
	if err != nil {
		return nil, Slice{}, err
	}
	fork, forkExtra, err := d.storeForkWithExtraSlices(
		&label, leftCell, &leftExtra, rightCell, &rightExtra, keyOffset, state)
	return fork, forkExtra, err
}

// splitBulkOnNextBit consumes the edge bit of every item and partitions the
// batch. Sorted input keeps the zero side a prefix of the slice, so the split
// is a single index and neither side is copied.
func splitBulkOnNextBit[T any](items []T, key func(*T) *Slice) ([]T, []T, error) {
	split := len(items)
	for i := range items {
		bit, err := key(&items[i]).LoadUInt(1)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to consume edge bit: %w", err)
		}
		if bit != 0 && i < split {
			split = i
		}
	}
	return items[:split], items[split:], nil
}

func augBulkItemKey(item *augBulkItem) *Slice {
	return &item.key
}
