package cell

import (
	"fmt"
	"sort"
)

// Multiset applies a whole batch of updates in one merged pass over the tree.
// The batch is sorted by key and descended together with the existing nodes,
// so every node on the union of the update paths is parsed once and finalized
// once, where a loop of Set and Delete calls walks and re-hashes one root-to-
// leaf path per key.
//
// A nil Value deletes its key, which must be present: deleting an absent key
// fails the whole batch, as it does for the reference implementation. So do
// duplicate keys. The updates slice is reordered in place.
//
// The batch shape is load bearing beyond the saved work. A subtree no update
// reaches is carried over by reference and never parsed, so it stays out of
// the read set and out of any Merkle proof built from it; a validator that
// replays the same batch against such a proof needs exactly the cells this
// pass read. Sequential updates read strictly more — a delete that empties one
// side of a fork merges the surviving sibling in, even when a later insert of
// the same batch puts a key straight back under it — which is why a proof
// shipped by a batch producer cannot serve a sequential consumer.
func (d *Dictionary) Multiset(updates []DictBulkKV) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	if len(updates) == 0 {
		return nil
	}

	keyBytes := int(d.keySz+7) / 8
	for i := range updates {
		if len(updates[i].Key) < keyBytes {
			return fmt.Errorf("key of update %d is shorter than %d bits", i, d.keySz)
		}
	}

	sortDictBulkItems(updates, d.keySz)
	for i := 0; i+1 < len(updates); i++ {
		if compareDictBulkKeys(updates[i].Key, updates[i+1].Key, d.keySz) == 0 {
			return fmt.Errorf("key %x appears twice in one dictionary update batch", updates[i].Key[:keyBytes])
		}
	}

	root, err := d.multisetNode(d.tracedRoot(), updates, 0, 0)
	if err != nil {
		return err
	}
	d.setRoot(root)
	return nil
}

// multisetNode applies items to branch and returns the resulting subtree, or
// nil when the batch empties it.
//
// pos is the key bit at which the RESULT node's label starts, so the result
// resolves d.keySz-pos bits. skip is how many leading bits of branch's own
// label the caller has already consumed: branch resolves d.keySz-pos+skip
// bits and the first skip of them are known to match the path walked so far.
// A non-zero skip appears when the old node's label outlives the batch's
// common prefix and the node is pushed down under a new fork.
func (d *Dictionary) multisetNode(branch *Cell, items []DictBulkKV, pos, skip uint) (*Cell, error) {
	remaining := d.keySz - pos
	if branch == nil {
		return d.multisetBuild(items, pos)
	}
	if len(items) == 0 {
		// Carried over whole: not parsed, so not read.
		return branch, nil
	}

	node, err := d.parseMultisetNode(branch, remaining+skip, &branch)
	if err != nil {
		return nil, err
	}
	if node.labelLen < skip {
		return nil, fmt.Errorf("dictionary node label is shorter than its consumed prefix")
	}

	label := node.labelSlice()
	if skip > 0 {
		if err = label.SkipBits(skip); err != nil {
			return nil, err
		}
	}
	labelLen := node.labelLen - skip

	firstCell := Cell{data: items[0].Key, bitsSz: uint16(d.keySz)}
	first := Slice{cell: &firstCell, bitStart: uint16(pos), bitEnd: uint16(d.keySz)}
	batchLen := remaining
	if len(items) > 1 {
		lastCell := Cell{data: items[len(items)-1].Key, bitsSz: uint16(d.keySz)}
		last := Slice{cell: &lastCell, bitStart: uint16(pos), bitEnd: uint16(d.keySz)}
		batchLen, err = commonSlicePrefix(&first, &last, remaining)
		if err != nil {
			return nil, err
		}
	}

	matched := uint(0)
	if limit := min(labelLen, batchLen); limit > 0 {
		if matched, err = commonSlicePrefix(&label, &first, limit); err != nil {
			return nil, err
		}
	}

	// The old subtree and the batch diverge inside both labels: neither
	// contains the other, so they become the two sides of a new fork.
	if matched < labelLen && matched < batchLen {
		var payload Builder
		node.loader.ToBuilderInto(&payload)
		oldSide, err := storeDictNodeTraced(
			label.slice(matched+1, labelLen-matched-1), &payload, remaining-matched-1, d.trace,
		)
		if err != nil {
			return nil, err
		}
		newSide, err := d.multisetBuild(items, pos+matched+1)
		if err != nil {
			return nil, err
		}
		left, right := oldSide, newSide
		if keyBitAt(items[0].Key, pos+matched) == 0 {
			left, right = newSide, oldSide
		}
		return d.storeFork(label.slice(0, matched), left, right, remaining)
	}

	// The batch's common prefix outlives the node's label: every key of the
	// batch belongs under one child, and the other child is untouched.
	if matched == labelLen && matched < batchLen {
		descend := keyBitAt(items[0].Key, pos+matched)
		kept, err := node.ref(int(descend ^ 1))
		if err != nil {
			return nil, err
		}
		target, err := node.ref(int(descend))
		if err != nil {
			return nil, err
		}
		updated, err := d.multisetNode(target, items, pos+matched+1, 0)
		if err != nil {
			return nil, err
		}
		if updated == target {
			if reuse, err := d.canReuseMultisetNode(&node, branch, remaining, skip); err != nil || reuse {
				return branch, err
			}
		}
		left, right := kept, updated
		if descend == 0 {
			left, right = updated, kept
		}
		return d.joinMultisetChildren(&label, matched, left, right, remaining)
	}

	// The node's label outlives the batch's common prefix: the node is pushed
	// down under a new fork whose other side the batch builds from scratch.
	if matched == batchLen && matched < labelLen {
		side, err := label.BitAt(matched)
		if err != nil {
			return nil, err
		}
		mid := splitBatchAt(items, pos+matched)
		own, fresh := items[:mid], items[mid:]
		if side != 0 {
			own, fresh = fresh, own
		}
		moved, err := d.multisetNode(branch, own, pos+matched+1, skip+matched+1)
		if err != nil {
			return nil, err
		}
		built, err := d.multisetBuild(fresh, pos+matched+1)
		if err != nil {
			return nil, err
		}
		left, right := moved, built
		if side != 0 {
			left, right = built, moved
		}
		return d.joinMultisetChildren(&label, matched, left, right, remaining)
	}

	// The labels agree over their whole length.
	if matched == remaining {
		// Both describe the same key: the batch value replaces the leaf.
		if len(items) != 1 {
			return nil, fmt.Errorf("dictionary update batch resolves %d values for one key", len(items))
		}
		if items[0].Value == nil {
			return nil, nil
		}
		if d.trace == nil && node.loader.trace == nil && multisetValueUnchanged(&node.loader, items[0].Value) {
			if reuse, err := d.canReuseMultisetNode(&node, branch, remaining, skip); err != nil || reuse {
				return branch, err
			}
		}
		return d.storeLeaf(label.slice(0, matched), items[0].Value, remaining)
	}

	mid := splitBatchAt(items, pos+matched)
	leftRef, err := node.ref(0)
	if err != nil {
		return nil, err
	}
	rightRef, err := node.ref(1)
	if err != nil {
		return nil, err
	}
	left, err := d.multisetNode(leftRef, items[:mid], pos+matched+1, 0)
	if err != nil {
		return nil, err
	}
	right, err := d.multisetNode(rightRef, items[mid:], pos+matched+1, 0)
	if err != nil {
		return nil, err
	}
	if left == leftRef && right == rightRef {
		if reuse, err := d.canReuseMultisetNode(&node, branch, remaining, skip); err != nil || reuse {
			return branch, err
		}
	}
	return d.joinMultisetChildren(&label, matched, left, right, remaining)
}

// Only complete, ordinary, untraced nodes can survive a replacement unchanged.
// Traced writes still rebuild so creation events and errors retain their order;
// virtual views and resolved special nodes must materialize as ordinary cells.
// Even identical values canonicalize non-minimal labels on the updated path.
func (d *Dictionary) canReuseMultisetNode(node *fixedDictNode, branch *Cell, remaining, skip uint) (bool, error) {
	if skip != 0 || d.trace != nil || node.loader.trace != nil || node.cell != branch || branch.IsVirtualized() {
		return false, nil
	}
	return node.hasCanonicalLabel(remaining)
}

func multisetValueUnchanged(stored *Slice, value *Builder) bool {
	if stored.BitsLeft() != value.bitsSz || stored.RefsNum() != int(value.refsNum) {
		return false
	}
	for i, ref := range value.rawRefs() {
		// Equal hashes can hide different lazy loaders or trace metadata.
		if stored.cell.refs[int(stored.refStart)+i] != ref {
			return false
		}
	}
	valueCell := Cell{data: value.data[:value.usedBytes()], bitsSz: uint16(value.bitsSz)}
	valueSlice := Slice{cell: &valueCell, bitEnd: valueCell.bitsSz}
	return stored.BitsEqual(&valueSlice)
}

// joinMultisetChildren rebuilds a node from the two subtrees the batch left
// behind: a fork while both survive, nothing when neither does, and the
// surviving child pulled up under a merged edge label when one is gone.
func (d *Dictionary) joinMultisetChildren(label *Slice, labelLen uint, left, right *Cell, remaining uint) (*Cell, error) {
	if left != nil && right != nil {
		return d.storeFork(label.slice(0, labelLen), left, right, remaining)
	}
	if left == nil && right == nil {
		return nil, nil
	}

	survivor, edge := left, uint64(0)
	if left == nil {
		survivor, edge = right, 1
	}
	// The survivor is the one node this shape has to open that the descent did
	// not: its label becomes part of the merged edge. It gets the same
	// treatment a descent gives a node, so a pruned branch here is reported as
	// one instead of being read as a label.
	survivorNode, err := d.parseMultisetNode(survivor, remaining-labelLen-1, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load surviving %d ref: %w", edge, err)
	}

	merged := BeginCell()
	base := label.slice(0, labelLen)
	if err = merged.storeSliceFromSlice(base, labelLen); err != nil {
		return nil, fmt.Errorf("failed to append base label: %w", err)
	}
	if err = merged.StoreUInt(edge, 1); err != nil {
		return nil, fmt.Errorf("failed to append edge bit: %w", err)
	}
	survivorLabel := survivorNode.labelSlice()
	if err = merged.storeSliceFromSlice(&survivorLabel, survivorNode.labelLen); err != nil {
		return nil, fmt.Errorf("failed to append surviving label: %w", err)
	}

	var payload Builder
	survivorNode.loader.ToBuilderInto(&payload)
	return storeDictNodeTraced(builderSliceView(merged), &payload, remaining, d.trace)
}

// parseMultisetNode parses one node under the walk's rules: a special cell is
// resolved when the walk carries a resolver and reported otherwise, and the
// node shape is validated, which is what the reference's chk_all label parse
// does at the same points.
func (d *Dictionary) parseMultisetNode(branch *Cell, remaining uint, loadedBranch **Cell) (fixedDictNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return fixedDictNode{}, err
	}
	if loadedBranch != nil && branch.IsLazy() {
		// Label realignment revisits this node with a longer consumed prefix.
		// Keep the validated body, before special resolution, so those visits
		// preserve their logical load/resolver events without repeating I/O.
		*loadedBranch = node.cell.WithTrace(branch.Trace())
	}
	if err = node.resolveIfSpecial(remaining, branch.Trace(), nil); err != nil {
		return fixedDictNode{}, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return fixedDictNode{}, err
	}
	if err = node.validateForkShape(remaining, false); err != nil {
		return fixedDictNode{}, err
	}
	return node, nil
}

// multisetBuild builds a subtree from batch entries that reach no existing
// node. Every entry has to carry a value there: a deletion that arrives here
// names a key the dictionary does not hold.
func (d *Dictionary) multisetBuild(items []DictBulkKV, pos uint) (*Cell, error) {
	if len(items) == 0 {
		return nil, nil
	}
	keyBytes := int(d.keySz+7) / 8
	for i := range items {
		if items[i].Value == nil {
			return nil, fmt.Errorf("cannot delete key %x: %w", items[i].Key[:keyBytes], ErrNoSuchKeyInDict)
		}
	}
	return d.buildFromSorted(items, pos, nil)
}

// splitBatchAt returns the index of the first entry whose key bit at is set,
// which for a sorted batch splits it into the two sides of a fork.
func splitBatchAt(items []DictBulkKV, at uint) int {
	return sort.Search(len(items), func(i int) bool {
		return keyBitAt(items[i].Key, at) != 0
	})
}

func keyBitAt(key []byte, at uint) uint8 {
	return key[at/8] >> (7 - at%8) & 1
}
