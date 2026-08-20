package cell

import (
	"fmt"
	"sort"
)

// DeleteMany removes a batch of keys in a single descent.
//
// Repeated Delete walks a full root-to-leaf path per key and recombines the
// augmentation of every node on the way back up, so a node shared by k keys is
// rebuilt k times. DeleteMany visits the union of the paths once instead.
//
// The result is the dictionary repeated Delete would have produced — patricia
// tries are canonical in their key set, so the cells and augmentations come out
// identical. Keys must all be the dictionary's key size, must be distinct, and
// must all be present; a missing key fails the whole batch.
//
// parallelism is optional and has the same bounded branch-worker semantics as
// SetMany. It defaults to 1.
func (d *AugmentedDictionary) DeleteMany(keys []*Cell, parallelism ...int) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	workers, err := augmentedBulkParallelism(parallelism)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	if err := d.ensureWritable(); err != nil {
		return err
	}

	items := make([]Slice, len(keys))
	for i := range keys {
		if keys[i] == nil || keys[i].BitsSize() != d.keySz {
			return fmt.Errorf("invalid key size at entry %d", i)
		}
		if err := keys[i].BeginParseInto(&items[i]); err != nil {
			return fmt.Errorf("failed to load key at entry %d: %w", i, err)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return compareKeySlices(&items[i], &items[j]) < 0
	})
	for i := 1; i < len(items); i++ {
		if compareKeySlices(&items[i-1], &items[i]) == 0 {
			return fmt.Errorf("duplicate key in bulk delete")
		}
	}

	workers = min(workers, len(items))
	state := augmentedMutationState{parallelism: workers}
	root, rootExtra, err := d.deleteMany(d.root, items, d.keySz, &state)
	if err != nil {
		return err
	}
	if root == nil {
		return d.setRootWithExtra(nil, nil)
	}
	rootExtraCell, err := rootExtra.ToCell()
	if err != nil {
		return err
	}
	return d.setRootWithExtra(root, rootExtraCell)
}

// deleteMany removes a non-empty, sorted, distinct batch from branch. Each item
// is the not yet consumed key suffix and holds exactly keyOffset bits. It
// returns the new subtree — nil when the batch consumed it whole — and a view
// of its augmentation, which lives inside the returned cell and so survives the
// sibling recursion that follows.
func (d *AugmentedDictionary) deleteMany(
	branch *Cell,
	items []Slice,
	keyOffset uint,
	state *augmentedMutationState,
) (*Cell, Slice, error) {
	if branch == nil {
		return nil, Slice{}, fmt.Errorf("key is absent from the dictionary")
	}

	node, err := parseFixedDictNode(branch, keyOffset)
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

	// Deletion never forks a label: a key that diverges from it is a key the
	// dictionary does not hold.
	for i := range items {
		labelView, keyView := label, items[i]
		shared, err := commonSlicePrefix(&labelView, &keyView, sz)
		if err != nil {
			return nil, Slice{}, fmt.Errorf("failed to match key prefix: %w", err)
		}
		if shared < sz {
			return nil, Slice{}, fmt.Errorf("key is absent from the dictionary")
		}
		if err = items[i].SkipBits(sz); err != nil {
			return nil, Slice{}, err
		}
	}

	if keyOffset == sz {
		// The whole key is spent: this leaf is the single key living here, and
		// distinctness makes the batch that one key.
		return nil, Slice{}, nil
	}

	left, right, err := splitBulkOnNextBit(items, func(key *Slice) *Slice { return key })
	if err != nil {
		return nil, Slice{}, err
	}
	childOffset := keyOffset - sz - 1

	var children [2]*Cell
	var extras [2]Slice
	var touched [2]bool
	sides := [2][]Slice{left, right}
	if len(right) != 0 && state.shouldFork(len(left), len(items)) {
		deleteSide := func(bit int, batch []Slice, branchState *augmentedMutationState) (*Cell, Slice, error) {
			ref, err := node.ref(bit)
			if err != nil {
				return nil, Slice{}, fmt.Errorf("failed to peek %d ref: %w", bit, err)
			}
			if len(batch) == 0 {
				return ref, Slice{}, nil
			}
			return d.deleteMany(ref, batch, childOffset, branchState)
		}
		leftResult, rightResult := runAugmentedBranches(
			state,
			len(left),
			len(items),
			func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
				cell, extra, err := deleteSide(0, left, branchState)
				return cell, extra, nil, err
			},
			func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
				cell, extra, err := deleteSide(1, right, branchState)
				return cell, extra, nil, err
			},
		)
		if leftResult.err != nil {
			return nil, Slice{}, leftResult.err
		}
		if rightResult.err != nil {
			return nil, Slice{}, rightResult.err
		}
		children[0], extras[0] = leftResult.cell, leftResult.extra
		children[1], extras[1] = rightResult.cell, rightResult.extra
		for bit, batch := range sides {
			touched[bit] = len(batch) != 0
		}
	} else {
		for bit, batch := range sides {
			ref, err := node.ref(bit)
			if err != nil {
				return nil, Slice{}, fmt.Errorf("failed to peek %d ref: %w", bit, err)
			}
			if len(batch) == 0 {
				children[bit] = ref
				continue
			}
			touched[bit] = true
			children[bit], extras[bit], err = d.deleteMany(ref, batch, childOffset, state)
			if err != nil {
				return nil, Slice{}, err
			}
		}
	}

	switch {
	case children[0] != nil && children[1] != nil:
		for bit := range extras {
			if touched[bit] {
				continue
			}
			extras[bit], err = extractAugmentedNodeExtraViewScratch(children[bit], childOffset, d.aug.SkipExtra, &state.skipScratch)
			if err != nil {
				return nil, Slice{}, fmt.Errorf("failed to extract %d ref extra: %w", bit, err)
			}
		}
		labelView := label
		return d.storeForkWithExtraSlices(&labelView, children[0], &extras[0], children[1], &extras[1], keyOffset, state)

	case children[0] == nil && children[1] == nil:
		return nil, Slice{}, nil

	default:
		// One side is gone: the survivor moves up, its label absorbing this
		// node's label and the edge bit — exactly the merge a single delete
		// performs, whether the survivor is the untouched old child or a
		// subtree the recursion just rebuilt.
		survivorIdx := 0
		if children[0] == nil {
			survivorIdx = 1
		}
		survivor := children[survivorIdx]
		survivorNode, err := parseFixedDictNode(survivor, childOffset)
		if err != nil {
			return nil, Slice{}, fmt.Errorf("failed to load surviving child: %w", err)
		}
		if err = survivorNode.rejectSpecial("augmented dict"); err != nil {
			return nil, Slice{}, err
		}
		survivorExtra, err := augmentedNodeExtraViewScratch(survivorNode, childOffset, d.aug.SkipExtra, &state.skipScratch)
		if err != nil {
			return nil, Slice{}, fmt.Errorf("failed to extract surviving child extra: %w", err)
		}

		var mergedLabel Builder
		labelView := label
		if err = mergedLabel.storeSliceFromSlice(&labelView, sz); err != nil {
			return nil, Slice{}, fmt.Errorf("failed to append base label: %w", err)
		}
		if err = mergedLabel.StoreUInt(uint64(survivorIdx), 1); err != nil {
			return nil, Slice{}, fmt.Errorf("failed to append survivor edge bit: %w", err)
		}
		survivorLabel := survivorNode.labelSlice()
		if err = mergedLabel.storeSliceFromSlice(&survivorLabel, survivorNode.labelLen); err != nil {
			return nil, Slice{}, fmt.Errorf("failed to append survivor label: %w", err)
		}
		state.extra = Builder{}
		survivorNode.loader.ToBuilderInto(&state.extra)
		merged, err := d.storeNode(builderSliceView(&mergedLabel), &state.extra, keyOffset)
		if err != nil {
			return nil, Slice{}, err
		}
		return merged, survivorExtra, nil
	}
}
