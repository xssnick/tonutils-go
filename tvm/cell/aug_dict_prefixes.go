package cell

import "fmt"

// KeyPrefixes returns every distinct prefix of bits bits that at least one key
// of the dictionary begins with, in key order, or nil with an error when there
// are more than limit of them.
//
// It is the split a parallel walk of a large dictionary needs: a prefix names
// a non-empty subtree, CutPrefixSubdict on a copy isolates it with its full
// keys, and the subtrees can be iterated independently. Taking the prefixes
// from the trie instead of enumerating a key space keeps the split generic —
// a queue keyed by workchain and address prefix has two live workchains and
// four billion possible ones — and guarantees no task is empty.
//
// Only the top of the trie is read: descent stops as soon as a node's
// accumulated label reaches bits. The reads go through the dictionary's trace
// like any other walk.
func (d *AugmentedDictionary) KeyPrefixes(bits uint, limit int) ([]*Cell, error) {
	if d == nil || d.root == nil || bits == 0 {
		return nil, nil
	}
	if bits > d.keySz {
		bits = d.keySz
	}
	var out []*Cell
	var prefix Builder
	err := d.keyPrefixesFrom(d.root.withTraceCombined(d.trace), d.keySz, &prefix, bits, limit, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (d *AugmentedDictionary) keyPrefixesFrom(branch *Cell, remaining uint, prefix *Builder, bits uint, limit int, out *[]*Cell) error {
	if branch == nil {
		return nil
	}
	prefixBits := prefix.BitsUsed()
	defer prefix.truncateBits(prefixBits)

	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return err
	}
	if err = node.resolveIfSpecial(remaining, branch.Trace(), d.trace.dictSpecialResolver()); err != nil {
		return err
	}
	if err = node.rejectSpecial("augmented dictionary"); err != nil {
		return err
	}
	if err = node.validateForkShape(remaining, true); err != nil {
		return err
	}
	consumed := uint(prefix.BitsUsed())
	label := node.labelSlice()
	// The label completes the prefix: emit the first bits of prefix+label and
	// stop, whatever hangs below.
	if consumed+node.labelLen >= bits {
		if err = prefix.storeSliceFromSlice(&label, bits-consumed); err != nil {
			return err
		}
		if len(*out) >= limit {
			return fmt.Errorf("dictionary has more than %d key prefixes of %d bits", limit, bits)
		}
		*out = append(*out, prefix.EndCell())
		return nil
	}
	if node.isLeaf(remaining) {
		// A leaf whose whole key is shorter than bits cannot happen in a fixed
		// dictionary: remaining == labelLen here, and remaining+consumed == keySz >= bits.
		return fmt.Errorf("dictionary leaf shorter than its key")
	}
	if err = prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return err
	}
	branchBits := prefix.BitsUsed()
	childRemaining := remaining - node.labelLen - 1
	for bit := 0; bit < 2; bit++ {
		child, err := node.ref(bit)
		if err != nil {
			return err
		}
		if err = prefix.StoreUInt(uint64(bit), 1); err != nil {
			return err
		}
		if err = d.keyPrefixesFrom(child, childRemaining, prefix, bits, limit, out); err != nil {
			return err
		}
		prefix.truncateBits(branchBits)
	}
	return nil
}
