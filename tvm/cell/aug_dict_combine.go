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
	label    augmentedLabel
	labelLen uint
	payload  *Slice
	extra    *Cell
	left     *Cell
	right    *Cell
	leaf     bool
}

type augmentedLabel struct {
	bits   []byte
	bitLen uint
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
	return combineAugmentedRootViews(
		augmentedRootView{cell: left, keySz: keySz},
		augmentedRootView{cell: right, keySz: keySz},
		aug,
	)
}

func combineAugmentedRootViews(left, right augmentedRootView, aug Augmentation) (*Cell, *Cell, error) {
	if left.cell == nil {
		if right.cell == nil {
			extra, err := aug.EmptyExtra()
			if err != nil {
				return nil, nil, err
			}
			return nil, extra, nil
		}

		rightNode, err := parseAugmentedNodeForCombine(right, aug)
		if err != nil {
			return nil, nil, err
		}
		root, err := materializeAugmentedNodeForCombine(rightNode, rightNode.remainingKeyBits())
		if err != nil {
			return nil, nil, err
		}
		return root, rightNode.extra, nil
	}
	if right.cell == nil {
		leftNode, err := parseAugmentedNodeForCombine(left, aug)
		if err != nil {
			return nil, nil, err
		}
		root, err := materializeAugmentedNodeForCombine(leftNode, leftNode.remainingKeyBits())
		if err != nil {
			return nil, nil, err
		}
		return root, leftNode.extra, nil
	}

	return combineAugmentedNonEmptyRootViews(left, right, aug)
}

func combineAugmentedNonEmptyRootViews(left, right augmentedRootView, aug Augmentation) (*Cell, *Cell, error) {
	leftNode, err := parseAugmentedNodeForCombine(left, aug)
	if err != nil {
		return nil, nil, err
	}
	rightNode, err := parseAugmentedNodeForCombine(right, aug)
	if err != nil {
		return nil, nil, err
	}
	if leftNode.remainingKeyBits() != rightNode.remainingKeyBits() {
		return nil, nil, fmt.Errorf("cannot combine augmented dictionary views with different remaining key sizes: %d != %d", leftNode.remainingKeyBits(), rightNode.remainingKeyBits())
	}

	common := commonAugmentedLabelPrefix(leftNode, rightNode)
	switch {
	case common < leftNode.visibleLabelLen() && common < rightNode.visibleLabelLen():
		return combineAugmentedDivergedRoots(leftNode, rightNode, common, aug)

	case common == leftNode.visibleLabelLen() && common == rightNode.visibleLabelLen():
		return combineAugmentedSameLabelRoots(leftNode, rightNode, aug)

	case common == leftNode.visibleLabelLen():
		return combineAugmentedIntoLeftFork(leftNode, rightNode, common, aug)

	default:
		return combineAugmentedIntoRightFork(leftNode, rightNode, common, aug)
	}
}

func combineAugmentedDivergedRoots(leftNode, rightNode *augmentedCombineNode, common uint, aug Augmentation) (*Cell, *Cell, error) {
	leftBit := leftNode.visibleLabelBit(common)
	rightBit := rightNode.visibleLabelBit(common)
	if leftBit == rightBit {
		return nil, nil, fmt.Errorf("invalid augmented dictionary labels: divergent labels share branch bit")
	}

	remaining := leftNode.remainingKeyBits()
	prefix := leftNode.visibleLabelSlice(0, common)
	childKeySz := remaining - common - 1

	leftChild, err := materializeAugmentedNodeSuffixForCombine(leftNode, common+1, childKeySz)
	if err != nil {
		return nil, nil, err
	}
	rightChild, err := materializeAugmentedNodeSuffixForCombine(rightNode, common+1, childKeySz)
	if err != nil {
		return nil, nil, err
	}

	var forkLeft, forkRight *Cell
	var forkLeftExtra, forkRightExtra *Cell
	if leftBit == 0 {
		forkLeft, forkLeftExtra = leftChild, leftNode.extra
		forkRight, forkRightExtra = rightChild, rightNode.extra
	} else {
		forkLeft, forkLeftExtra = rightChild, rightNode.extra
		forkRight, forkRightExtra = leftChild, leftNode.extra
	}

	return storeAugmentedForkForCombine(aug, prefix, forkLeft, forkLeftExtra, forkRight, forkRightExtra, remaining)
}

func combineAugmentedSameLabelRoots(leftNode, rightNode *augmentedCombineNode, aug Augmentation) (*Cell, *Cell, error) {
	if leftNode.leaf || rightNode.leaf {
		if leftNode.leaf && rightNode.leaf {
			return nil, nil, errAugmentedDictionaryConflict
		}
		return nil, nil, fmt.Errorf("invalid augmented dictionary node: leaf/fork mismatch on equal label")
	}

	remaining := leftNode.remainingKeyBits()
	if leftNode.visibleLabelLen() >= remaining {
		return nil, nil, fmt.Errorf("invalid augmented dictionary fork label length %d for key size %d", leftNode.visibleLabelLen(), remaining)
	}

	childKeySz := remaining - leftNode.visibleLabelLen() - 1
	leftChild, leftExtra, err := combineAugmentedRootViews(leftNode.leftChildView(childKeySz), rightNode.leftChildView(childKeySz), aug)
	if err != nil {
		return nil, nil, err
	}
	rightChild, rightExtra, err := combineAugmentedRootViews(leftNode.rightChildView(childKeySz), rightNode.rightChildView(childKeySz), aug)
	if err != nil {
		return nil, nil, err
	}

	return storeAugmentedForkForCombine(aug, leftNode.visibleLabelSlice(0, leftNode.visibleLabelLen()), leftChild, leftExtra, rightChild, rightExtra, remaining)
}

func combineAugmentedIntoLeftFork(leftNode, rightNode *augmentedCombineNode, common uint, aug Augmentation) (*Cell, *Cell, error) {
	if leftNode.leaf {
		return nil, nil, fmt.Errorf("invalid augmented dictionary node: leaf cannot contain a longer key")
	}

	remaining := leftNode.remainingKeyBits()
	branchBit := rightNode.visibleLabelBit(common)
	childKeySz := remaining - common - 1
	longerView := rightNode.consumeVisibleLabelBits(common + 1)

	var leftChild, rightChild, leftExtra, rightExtra *Cell
	var err error
	if branchBit == 0 {
		leftChild, leftExtra, err = combineAugmentedRootViews(leftNode.leftChildView(childKeySz), longerView, aug)
		if err != nil {
			return nil, nil, err
		}
		rightChild = leftNode.right
		rightExtra, err = extractAugmentedNodeExtraStrict(rightChild, childKeySz, aug)
		if err != nil {
			return nil, nil, err
		}
	} else {
		leftChild = leftNode.left
		leftExtra, err = extractAugmentedNodeExtraStrict(leftChild, childKeySz, aug)
		if err != nil {
			return nil, nil, err
		}
		rightChild, rightExtra, err = combineAugmentedRootViews(leftNode.rightChildView(childKeySz), longerView, aug)
		if err != nil {
			return nil, nil, err
		}
	}

	return storeAugmentedForkForCombine(aug, leftNode.visibleLabelSlice(0, leftNode.visibleLabelLen()), leftChild, leftExtra, rightChild, rightExtra, remaining)
}

func combineAugmentedIntoRightFork(leftNode, rightNode *augmentedCombineNode, common uint, aug Augmentation) (*Cell, *Cell, error) {
	if rightNode.leaf {
		return nil, nil, fmt.Errorf("invalid augmented dictionary node: leaf cannot contain a longer key")
	}

	remaining := rightNode.remainingKeyBits()
	branchBit := leftNode.visibleLabelBit(common)
	childKeySz := remaining - common - 1
	longerView := leftNode.consumeVisibleLabelBits(common + 1)

	var leftChild, rightChild, leftExtra, rightExtra *Cell
	var err error
	if branchBit == 0 {
		leftChild, leftExtra, err = combineAugmentedRootViews(longerView, rightNode.leftChildView(childKeySz), aug)
		if err != nil {
			return nil, nil, err
		}
		rightChild = rightNode.right
		rightExtra, err = extractAugmentedNodeExtraStrict(rightChild, childKeySz, aug)
		if err != nil {
			return nil, nil, err
		}
	} else {
		leftChild = rightNode.left
		leftExtra, err = extractAugmentedNodeExtraStrict(leftChild, childKeySz, aug)
		if err != nil {
			return nil, nil, err
		}
		rightChild, rightExtra, err = combineAugmentedRootViews(longerView, rightNode.rightChildView(childKeySz), aug)
		if err != nil {
			return nil, nil, err
		}
	}

	return storeAugmentedForkForCombine(aug, rightNode.visibleLabelSlice(0, rightNode.visibleLabelLen()), leftChild, leftExtra, rightChild, rightExtra, remaining)
}

func parseAugmentedNodeForCombine(view augmentedRootView, aug Augmentation) (*augmentedCombineNode, error) {
	if view.cell.IsSpecial() && !view.cell.IsLazy() {
		return nil, fmt.Errorf("augmented dictionary merge does not support special cells inside dict tree: %v", view.cell.GetType())
	}

	loader, err := view.cell.BeginParse()
	if err != nil {
		return nil, err
	}
	if loader.cell.IsSpecial() {
		return nil, fmt.Errorf("augmented dictionary merge does not support special cells inside dict tree: %v", loader.cell.GetType())
	}
	labelLen, labelBuilder, err := loadLabel(view.keySz, loader, BeginCell())
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dictionary label: %w", err)
	}
	if view.skip > labelLen {
		return nil, fmt.Errorf("invalid augmented dictionary label skip %d for label length %d", view.skip, labelLen)
	}

	label, err := augmentedLabelFromBuilder(labelBuilder, labelLen)
	if err != nil {
		return nil, err
	}

	payload := loader.Copy()
	node := &augmentedCombineNode{
		view:     view,
		label:    label,
		labelLen: labelLen,
		payload:  payload,
		leaf:     labelLen == view.keySz,
	}

	if node.leaf {
		extra, err := captureConsumedPrefix(payload.Copy(), aug.SkipExtra)
		if err != nil {
			return nil, fmt.Errorf("failed to load augmented dictionary leaf extra: %w", err)
		}
		node.extra = extra
		return node, nil
	}

	left, err := loader.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dictionary left fork: %w", err)
	}
	right, err := loader.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dictionary right fork: %w", err)
	}
	extra, err := captureConsumedPrefix(loader, aug.SkipExtra)
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dictionary fork extra: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return nil, fmt.Errorf("augmented dictionary fork has trailing data")
	}

	node.left = left
	node.right = right
	node.extra = extra

	return node, nil
}

func chooseAugmentedRootExtraForCombine(root, rootExtra *Cell, keySz uint, aug Augmentation) (*Cell, error) {
	nodeExtra, err := extractAugmentedNodeExtraStrict(root, keySz, aug)
	if err != nil {
		return nil, err
	}

	if rootExtra == nil {
		return nodeExtra, nil
	}
	if err := validateAugmentedExtraCellForCombine(rootExtra, aug); err != nil {
		return nil, err
	}
	if !equalCellContents(rootExtra, nodeExtra) {
		return nil, fmt.Errorf("augmented dictionary root extra does not match root node extra")
	}

	return rootExtra, nil
}

func extractAugmentedNodeExtraStrict(c *Cell, keySz uint, aug Augmentation) (*Cell, error) {
	node, err := parseAugmentedNodeForCombine(augmentedRootView{cell: c, keySz: keySz}, aug)
	if err != nil {
		return nil, err
	}
	return node.extra, nil
}

func validateAugmentedExtraCellForCombine(extra *Cell, aug Augmentation) error {
	loader := extra.MustBeginParse()
	if err := aug.SkipExtra(loader); err != nil {
		return fmt.Errorf("failed to load augmented dictionary extra: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return fmt.Errorf("augmented dictionary extra has trailing data")
	}

	return nil
}

func storeAugmentedForkForCombine(aug Augmentation, label *Slice, left, leftExtra, right, rightExtra *Cell, keySz uint) (*Cell, *Cell, error) {
	extra, err := aug.CombineExtra(leftExtra.MustBeginParse(), rightExtra.MustBeginParse())
	if err != nil {
		return nil, nil, err
	}

	fork := BeginCell().
		MustStoreRef(left).
		MustStoreRef(right).
		MustStoreBuilder(extra.MustBeginParse().ToBuilder())

	root, err := storeDictNode(label, fork, keySz)
	if err != nil {
		return nil, nil, err
	}

	return root, extra, nil
}

func materializeAugmentedNodeForCombine(node *augmentedCombineNode, keySz uint) (*Cell, error) {
	if node.view.skip == 0 && node.view.keySz == keySz {
		return node.view.cell, nil
	}

	return materializeAugmentedNodeSuffixForCombine(node, 0, keySz)
}

func materializeAugmentedNodeSuffixForCombine(node *augmentedCombineNode, skip uint, keySz uint) (*Cell, error) {
	payload, err := augmentedPayloadBuilderForCombine(node.payload)
	if err != nil {
		return nil, err
	}
	return storeDictNode(node.visibleLabelSlice(skip, node.visibleLabelLen()-skip), payload, keySz)
}

func augmentedPayloadBuilderForCombine(payload *Slice) (*Builder, error) {
	b := BeginCell()

	bits := payload.BitsLeft()
	data, err := payload.PreloadSlice(bits)
	if err != nil {
		return nil, err
	}
	if err = b.StoreSlice(data, bits); err != nil {
		return nil, err
	}

	refs := payload.Copy()
	refsCount := refs.RefsNum()
	for i := 0; i < refsCount; i++ {
		ref, err := refs.LoadRefCell()
		if err != nil {
			return nil, err
		}
		ref, err = ref.load()
		if err != nil {
			return nil, err
		}
		if err = b.StoreRef(ref); err != nil {
			return nil, err
		}
	}

	return b, nil
}

func commonAugmentedLabelPrefix(left, right *augmentedCombineNode) uint {
	limit := left.visibleLabelLen()
	if right.visibleLabelLen() < limit {
		limit = right.visibleLabelLen()
	}

	for i := uint(0); i < limit; i++ {
		if left.visibleLabelBit(i) != right.visibleLabelBit(i) {
			return i
		}
	}

	return limit
}

func (n *augmentedCombineNode) remainingKeyBits() uint {
	return n.view.keySz - n.view.skip
}

func (n *augmentedCombineNode) visibleLabelLen() uint {
	return n.labelLen - n.view.skip
}

func (n *augmentedCombineNode) visibleLabelBit(bit uint) uint64 {
	return n.label.bit(n.view.skip + bit)
}

func (n *augmentedCombineNode) visibleLabelSlice(start, length uint) *Slice {
	return n.label.slice(n.view.skip+start, length)
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

func augmentedLabelFromBuilder(label *Builder, bitLen uint) (augmentedLabel, error) {
	if bitLen == 0 {
		return augmentedLabel{}, nil
	}

	bits, err := label.ToSlice().LoadSlice(bitLen)
	if err != nil {
		return augmentedLabel{}, fmt.Errorf("failed to load augmented dictionary label bits: %w", err)
	}

	return augmentedLabel{
		bits:   append([]byte(nil), bits...),
		bitLen: bitLen,
	}, nil
}

func (l augmentedLabel) bit(bit uint) uint64 {
	if l.bits[bit/8]&(1<<(7-bit%8)) != 0 {
		return 1
	}
	return 0
}

func (l augmentedLabel) slice(start, length uint) *Slice {
	if length == 0 {
		return BeginCell().ToSlice()
	}

	b := BeginCell()
	for i := uint(0); i < length; i++ {
		b.MustStoreUInt(l.bit(start+i), 1)
	}

	return b.ToSlice()
}
