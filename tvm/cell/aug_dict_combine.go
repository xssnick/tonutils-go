package cell

import (
	"errors"
	"fmt"
)

var errAugmentedDictionaryConflict = errors.New("augmented dictionary has duplicate key")

type augmentedRootView struct {
	cell  *Cell
	keySz uint
	skip  uint
}

type augmentedCombineNode struct {
	view     augmentedRootView
	label    Slice
	labelLen uint
	payload  Slice
	extra    Slice
	left     *Cell
	right    *Cell
	leaf     bool
}

type augmentedCombineState struct {
	aug                   Augmentation
	loader, cursor        Slice
	leftExtra, rightExtra Slice
	extra, payload, node  Builder
}

// CombineWith structurally merges other into d.
//
// It returns false, nil when both dictionaries contain the same key.
// The receiver is mutated only after the whole merge succeeds.
func (d *AugmentedDictionary) CombineWith(other *AugmentedDictionary) (bool, error) {
	if d.keySz != other.keySz {
		return false, fmt.Errorf("cannot combine augmented dictionaries with different key sizes: %d != %d", d.keySz, other.keySz)
	}
	if d.aug == nil {
		return false, fmt.Errorf("augmentation is nil")
	}
	if err := d.ensureWritable(); err != nil {
		return false, err
	}

	if other.root == nil {
		if other.rootExtra != nil {
			if err := validateAugmentedExtraCellForCombine(other.rootExtra, d.aug); err != nil {
				return false, err
			}
		}
		return true, nil
	}

	if d.root == nil {
		extra, err := chooseAugmentedRootExtraForCombine(other.root, other.rootExtra, d.keySz, d.aug)
		if err != nil {
			return false, err
		}
		if err := d.setRootWithExtra(other.root, extra); err != nil {
			return false, err
		}
		return true, nil
	}

	root, extra, err := combineAugmentedRoots(d.root, other.root, d.keySz, d.aug)
	if errors.Is(err, errAugmentedDictionaryConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := d.setRootWithExtra(root, extra); err != nil {
		return false, err
	}

	return true, nil
}

func combineAugmentedRoots(left, right *Cell, keySz uint, aug Augmentation) (*Cell, *Cell, error) {
	state := augmentedCombineState{aug: aug}
	root, extra, err := combineAugmentedRootViews(
		augmentedRootView{cell: left, keySz: keySz},
		augmentedRootView{cell: right, keySz: keySz},
		&state,
	)
	if err != nil {
		return nil, nil, err
	}
	extraCell, err := extra.ToCell()
	if err != nil {
		return nil, nil, err
	}
	return root, extraCell, nil
}

func combineAugmentedRootViews(left, right augmentedRootView, state *augmentedCombineState) (*Cell, Slice, error) {
	if left.cell == nil {
		if right.cell == nil {
			state.extra = Builder{}
			if err := state.aug.EmptyExtra(&state.extra); err != nil {
				return nil, Slice{}, err
			}
			extraCell := state.extra.EndCell()
			return nil, Slice{cell: extraCell, bitEnd: extraCell.bitsSz, refEnd: uint8(extraCell.refsCount())}, nil
		}

		rightNode, err := parseAugmentedNodeForCombine(right, state)
		if err != nil {
			return nil, Slice{}, err
		}
		root, err := materializeAugmentedNodeForCombine(rightNode, rightNode.remainingKeyBits(), state)
		if err != nil {
			return nil, Slice{}, err
		}
		return root, rightNode.extra, nil
	}
	if right.cell == nil {
		leftNode, err := parseAugmentedNodeForCombine(left, state)
		if err != nil {
			return nil, Slice{}, err
		}
		root, err := materializeAugmentedNodeForCombine(leftNode, leftNode.remainingKeyBits(), state)
		if err != nil {
			return nil, Slice{}, err
		}
		return root, leftNode.extra, nil
	}

	return combineAugmentedNonEmptyRootViews(left, right, state)
}

func combineAugmentedNonEmptyRootViews(left, right augmentedRootView, state *augmentedCombineState) (*Cell, Slice, error) {
	leftNode, err := parseAugmentedNodeForCombine(left, state)
	if err != nil {
		return nil, Slice{}, err
	}
	rightNode, err := parseAugmentedNodeForCombine(right, state)
	if err != nil {
		return nil, Slice{}, err
	}
	if leftNode.remainingKeyBits() != rightNode.remainingKeyBits() {
		return nil, Slice{}, fmt.Errorf("cannot combine augmented dictionary views with different remaining key sizes: %d != %d", leftNode.remainingKeyBits(), rightNode.remainingKeyBits())
	}

	common := commonAugmentedLabelPrefix(&leftNode, &rightNode)
	switch {
	case common < leftNode.visibleLabelLen() && common < rightNode.visibleLabelLen():
		return combineAugmentedDivergedRoots(leftNode, rightNode, common, state)

	case common == leftNode.visibleLabelLen() && common == rightNode.visibleLabelLen():
		return combineAugmentedSameLabelRoots(leftNode, rightNode, state)

	case common == leftNode.visibleLabelLen():
		return combineAugmentedIntoLeftFork(leftNode, rightNode, common, state)

	default:
		return combineAugmentedIntoRightFork(leftNode, rightNode, common, state)
	}
}

func combineAugmentedDivergedRoots(leftNode, rightNode augmentedCombineNode, common uint, state *augmentedCombineState) (*Cell, Slice, error) {
	leftBit := leftNode.visibleLabelBit(common)
	rightBit := rightNode.visibleLabelBit(common)
	if leftBit == rightBit {
		return nil, Slice{}, fmt.Errorf("invalid augmented dictionary labels: divergent labels share branch bit")
	}

	remaining := leftNode.remainingKeyBits()
	prefix := leftNode.visibleLabelValue(0, common)
	childKeySz := remaining - common - 1

	leftChild, err := materializeAugmentedNodeSuffixForCombine(leftNode, common+1, childKeySz, state)
	if err != nil {
		return nil, Slice{}, err
	}
	rightChild, err := materializeAugmentedNodeSuffixForCombine(rightNode, common+1, childKeySz, state)
	if err != nil {
		return nil, Slice{}, err
	}

	var forkLeft, forkRight *Cell
	var forkLeftExtra, forkRightExtra Slice
	if leftBit == 0 {
		forkLeft, forkLeftExtra = leftChild, leftNode.extra
		forkRight, forkRightExtra = rightChild, rightNode.extra
	} else {
		forkLeft, forkLeftExtra = rightChild, rightNode.extra
		forkRight, forkRightExtra = leftChild, leftNode.extra
	}

	return storeAugmentedForkForCombine(&prefix, forkLeft, &forkLeftExtra, forkRight, &forkRightExtra, remaining, state)
}

func combineAugmentedSameLabelRoots(leftNode, rightNode augmentedCombineNode, state *augmentedCombineState) (*Cell, Slice, error) {
	if leftNode.leaf || rightNode.leaf {
		if leftNode.leaf && rightNode.leaf {
			return nil, Slice{}, errAugmentedDictionaryConflict
		}
		return nil, Slice{}, fmt.Errorf("invalid augmented dictionary node: leaf/fork mismatch on equal label")
	}

	remaining := leftNode.remainingKeyBits()
	if leftNode.visibleLabelLen() >= remaining {
		return nil, Slice{}, fmt.Errorf("invalid augmented dictionary fork label length %d for key size %d", leftNode.visibleLabelLen(), remaining)
	}

	childKeySz := remaining - leftNode.visibleLabelLen() - 1
	leftChild, leftExtra, err := combineAugmentedRootViews(leftNode.leftChildView(childKeySz), rightNode.leftChildView(childKeySz), state)
	if err != nil {
		return nil, Slice{}, err
	}
	rightChild, rightExtra, err := combineAugmentedRootViews(leftNode.rightChildView(childKeySz), rightNode.rightChildView(childKeySz), state)
	if err != nil {
		return nil, Slice{}, err
	}

	label := leftNode.visibleLabelValue(0, leftNode.visibleLabelLen())
	return storeAugmentedForkForCombine(&label, leftChild, &leftExtra, rightChild, &rightExtra, remaining, state)
}

func combineAugmentedIntoLeftFork(leftNode, rightNode augmentedCombineNode, common uint, state *augmentedCombineState) (*Cell, Slice, error) {
	if leftNode.leaf {
		return nil, Slice{}, fmt.Errorf("invalid augmented dictionary node: leaf cannot contain a longer key")
	}

	remaining := leftNode.remainingKeyBits()
	branchBit := rightNode.visibleLabelBit(common)
	childKeySz := remaining - common - 1
	longerView := rightNode.consumeVisibleLabelBits(common + 1)

	var leftChild, rightChild *Cell
	var leftExtra, rightExtra Slice
	var err error
	if branchBit == 0 {
		leftChild, leftExtra, err = combineAugmentedRootViews(leftNode.leftChildView(childKeySz), longerView, state)
		if err != nil {
			return nil, Slice{}, err
		}
		rightChild = leftNode.right
		rightExtra, err = extractAugmentedNodeExtraStrictView(rightChild, childKeySz, state)
		if err != nil {
			return nil, Slice{}, err
		}
	} else {
		leftChild = leftNode.left
		leftExtra, err = extractAugmentedNodeExtraStrictView(leftChild, childKeySz, state)
		if err != nil {
			return nil, Slice{}, err
		}
		rightChild, rightExtra, err = combineAugmentedRootViews(leftNode.rightChildView(childKeySz), longerView, state)
		if err != nil {
			return nil, Slice{}, err
		}
	}

	label := leftNode.visibleLabelValue(0, leftNode.visibleLabelLen())
	return storeAugmentedForkForCombine(&label, leftChild, &leftExtra, rightChild, &rightExtra, remaining, state)
}

func combineAugmentedIntoRightFork(leftNode, rightNode augmentedCombineNode, common uint, state *augmentedCombineState) (*Cell, Slice, error) {
	if rightNode.leaf {
		return nil, Slice{}, fmt.Errorf("invalid augmented dictionary node: leaf cannot contain a longer key")
	}

	remaining := rightNode.remainingKeyBits()
	branchBit := leftNode.visibleLabelBit(common)
	childKeySz := remaining - common - 1
	longerView := leftNode.consumeVisibleLabelBits(common + 1)

	var leftChild, rightChild *Cell
	var leftExtra, rightExtra Slice
	var err error
	if branchBit == 0 {
		leftChild, leftExtra, err = combineAugmentedRootViews(longerView, rightNode.leftChildView(childKeySz), state)
		if err != nil {
			return nil, Slice{}, err
		}
		rightChild = rightNode.right
		rightExtra, err = extractAugmentedNodeExtraStrictView(rightChild, childKeySz, state)
		if err != nil {
			return nil, Slice{}, err
		}
	} else {
		leftChild = rightNode.left
		leftExtra, err = extractAugmentedNodeExtraStrictView(leftChild, childKeySz, state)
		if err != nil {
			return nil, Slice{}, err
		}
		rightChild, rightExtra, err = combineAugmentedRootViews(longerView, rightNode.rightChildView(childKeySz), state)
		if err != nil {
			return nil, Slice{}, err
		}
	}

	label := rightNode.visibleLabelValue(0, rightNode.visibleLabelLen())
	return storeAugmentedForkForCombine(&label, leftChild, &leftExtra, rightChild, &rightExtra, remaining, state)
}

func parseAugmentedNodeForCombine(view augmentedRootView, state *augmentedCombineState) (augmentedCombineNode, error) {
	if view.cell.IsSpecial() && !view.cell.IsLazy() {
		return augmentedCombineNode{}, fmt.Errorf("augmented dictionary merge does not support special cells inside dict tree: %v", view.cell.GetType())
	}

	if err := view.cell.BeginParseInto(&state.loader); err != nil {
		return augmentedCombineNode{}, err
	}
	if state.loader.cell.IsSpecial() {
		return augmentedCombineNode{}, fmt.Errorf("augmented dictionary merge does not support special cells inside dict tree: %v", state.loader.cell.GetType())
	}
	labelLen, label, err := readLabelView(view.keySz, &state.loader)
	if err != nil {
		return augmentedCombineNode{}, fmt.Errorf("failed to load augmented dictionary label: %w", err)
	}
	if view.skip > labelLen {
		return augmentedCombineNode{}, fmt.Errorf("invalid augmented dictionary label skip %d for label length %d", view.skip, labelLen)
	}

	payload := state.loader
	node := augmentedCombineNode{
		view:     view,
		label:    label,
		labelLen: labelLen,
		payload:  payload,
		leaf:     labelLen == view.keySz,
	}

	if node.leaf {
		extra := payload
		state.cursor = payload
		if err := state.aug.SkipExtra(&state.cursor); err != nil {
			return augmentedCombineNode{}, fmt.Errorf("failed to load augmented dictionary leaf extra: %w", err)
		}
		extra.bitEnd = state.cursor.bitStart
		extra.refEnd = state.cursor.refStart
		node.extra = extra
		return node, nil
	}

	left, err := state.loader.LoadRefCell()
	if err != nil {
		return augmentedCombineNode{}, fmt.Errorf("failed to load augmented dictionary left fork: %w", err)
	}
	right, err := state.loader.LoadRefCell()
	if err != nil {
		return augmentedCombineNode{}, fmt.Errorf("failed to load augmented dictionary right fork: %w", err)
	}
	extra := state.loader
	if err = state.aug.SkipExtra(&state.loader); err != nil {
		return augmentedCombineNode{}, fmt.Errorf("failed to load augmented dictionary fork extra: %w", err)
	}
	extra.bitEnd = state.loader.bitStart
	extra.refEnd = state.loader.refStart
	if state.loader.BitsLeft() != 0 || state.loader.RefsNum() != 0 {
		return augmentedCombineNode{}, fmt.Errorf("augmented dictionary fork has trailing data")
	}

	node.left = left
	node.right = right
	node.extra = extra

	return node, nil
}

func chooseAugmentedRootExtraForCombine(root, rootExtra *Cell, keySz uint, aug Augmentation) (*Cell, error) {
	state := augmentedCombineState{aug: aug}
	nodeExtra, err := extractAugmentedNodeExtraStrictView(root, keySz, &state)
	if err != nil {
		return nil, err
	}

	if rootExtra == nil {
		return nodeExtra.ToCell()
	}
	if err := validateAugmentedExtraCellForCombine(rootExtra, aug); err != nil {
		return nil, err
	}
	var expected Builder
	nodeExtra.ToBuilderInto(&expected)
	if !expected.EqualsCell(rootExtra) {
		return nil, fmt.Errorf("augmented dictionary root extra does not match root node extra")
	}

	return rootExtra, nil
}

func extractAugmentedNodeExtraStrictView(c *Cell, keySz uint, state *augmentedCombineState) (Slice, error) {
	node, err := parseAugmentedNodeForCombine(augmentedRootView{cell: c, keySz: keySz}, state)
	if err != nil {
		return Slice{}, err
	}
	return node.extra, nil
}

func validateAugmentedExtraCellForCombine(extra *Cell, aug Augmentation) error {
	loader, err := extra.BeginParse()
	if err != nil {
		return fmt.Errorf("failed to load augmented dictionary extra: %w", err)
	}
	if err := aug.SkipExtra(loader); err != nil {
		return fmt.Errorf("failed to load augmented dictionary extra: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return fmt.Errorf("augmented dictionary extra has trailing data")
	}

	return nil
}

func storeAugmentedForkForCombine(label *Slice, left *Cell, leftExtra *Slice, right *Cell, rightExtra *Slice, keySz uint, state *augmentedCombineState) (*Cell, Slice, error) {
	state.leftExtra = *leftExtra
	state.rightExtra = *rightExtra
	state.extra = Builder{}
	if err := state.aug.CombineExtra(&state.leftExtra, &state.rightExtra, &state.extra); err != nil {
		return nil, Slice{}, err
	}

	state.node = Builder{}
	if err := storeDictLabel(&state.node, label, keySz); err != nil {
		return nil, Slice{}, err
	}
	if err := state.node.StoreRef(left); err != nil {
		return nil, Slice{}, err
	}
	if err := state.node.StoreRef(right); err != nil {
		return nil, Slice{}, err
	}
	extraBitStart, extraRefStart := state.node.bitsSz, state.node.refsNum
	if err := state.node.StoreBuilder(&state.extra); err != nil {
		return nil, Slice{}, err
	}

	root := state.node.EndCell()
	return root, Slice{
		cell:     root,
		bitStart: uint16(extraBitStart),
		bitEnd:   root.bitsSz,
		refStart: extraRefStart,
		refEnd:   uint8(root.refsCount()),
	}, nil
}

func materializeAugmentedNodeForCombine(node augmentedCombineNode, keySz uint, state *augmentedCombineState) (*Cell, error) {
	if node.view.skip == 0 && node.view.keySz == keySz {
		return node.view.cell, nil
	}

	return materializeAugmentedNodeSuffixForCombine(node, 0, keySz, state)
}

func materializeAugmentedNodeSuffixForCombine(node augmentedCombineNode, skip uint, keySz uint, state *augmentedCombineState) (*Cell, error) {
	state.payload = Builder{}
	node.payload.ToBuilderInto(&state.payload)
	for i, ref := range state.payload.rawRefs() {
		loaded, err := ref.load()
		if err != nil {
			return nil, err
		}
		state.payload.refs[i] = loaded
	}

	state.node = Builder{}
	label := node.visibleLabelValue(skip, node.visibleLabelLen()-skip)
	if err := storeDictLabel(&state.node, &label, keySz); err != nil {
		return nil, err
	}
	if err := state.node.StoreBuilderUncheckedDepth(&state.payload); err != nil {
		return nil, err
	}
	return state.node.EndCell(), nil
}

func commonAugmentedLabelPrefix(left, right *augmentedCombineNode) uint {
	limit := left.visibleLabelLen()
	if right.visibleLabelLen() < limit {
		limit = right.visibleLabelLen()
	}
	leftLabel := left.visibleLabelValue(0, limit)
	rightLabel := right.visibleLabelValue(0, limit)
	matched, err := commonSlicePrefix(&leftLabel, &rightLabel, limit)
	if err != nil {
		panic(err)
	}
	return matched
}

func (n *augmentedCombineNode) remainingKeyBits() uint {
	return n.view.keySz - n.view.skip
}

func (n *augmentedCombineNode) visibleLabelLen() uint {
	return n.labelLen - n.view.skip
}

func (n *augmentedCombineNode) visibleLabelBit(bit uint) uint64 {
	value, _ := n.label.BitAt(n.view.skip + bit)
	return uint64(value)
}

func (n *augmentedCombineNode) visibleLabelValue(start, length uint) Slice {
	label := n.label
	label.bitStart += uint16(n.view.skip + start)
	label.bitEnd = label.bitStart + uint16(length)
	label.refEnd = label.refStart
	return label
}

func (n *augmentedCombineNode) consumeVisibleLabelBits(bits uint) augmentedRootView {
	return augmentedRootView{
		cell:  n.view.cell,
		keySz: n.view.keySz,
		skip:  n.view.skip + bits,
	}
}

func (n *augmentedCombineNode) leftChildView(keySz uint) augmentedRootView {
	return augmentedRootView{cell: n.left, keySz: keySz}
}

func (n *augmentedCombineNode) rightChildView(keySz uint) augmentedRootView {
	return augmentedRootView{cell: n.right, keySz: keySz}
}
