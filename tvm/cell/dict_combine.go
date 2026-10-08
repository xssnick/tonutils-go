package cell

import "fmt"

// DictCombineFunc combines the values of a key present in both dictionaries.
// The inputs and dst are borrowed for the duration of the call; the resulting
// value must be stored in dst.
type DictCombineFunc func(left, right *Slice, dst *Builder) error

type dictCombineRootView struct {
	cell  *Cell
	keySz uint
	skip  uint
}

type dictCombineNode struct {
	view     dictCombineRootView
	label    Slice
	labelLen uint
	payload  Slice
	left     *Cell
	right    *Cell
	leaf     bool
}

type dictCombineState struct {
	combine       DictCombineFunc
	trace         *Trace
	loader        Slice
	payload, node Builder
}

// CombineWith structurally merges other into d. Values present in both
// dictionaries are passed to combine in key order. Subtrees outside the
// intersecting Patricia paths are reused without walking them. The receiver
// changes only after the entire merge succeeds.
func (d *Dictionary) CombineWith(other *Dictionary, combine DictCombineFunc) error {
	if combine == nil {
		return fmt.Errorf("dictionary combine callback is required")
	}
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot combine dictionaries with different key sizes: %d != %d", d.keySz, other.keySz)
	}
	if err := validateDictKeySize(d.keySz); err != nil {
		return err
	}
	if other.root == nil {
		return nil
	}
	if d.root == nil {
		d.setRoot(other.root.withTraceCombined(d.trace))
		return nil
	}

	state := dictCombineState{
		combine: combine,
		trace:   d.trace,
	}
	root, err := combineDictRootViews(
		dictCombineRootView{cell: d.root.withTraceCombined(d.trace), keySz: d.keySz},
		dictCombineRootView{cell: other.root.withTraceCombined(d.trace), keySz: d.keySz},
		&state,
	)
	if err != nil {
		return err
	}

	d.setRoot(root)
	return nil
}

func combineDictRootViews(left, right dictCombineRootView, state *dictCombineState) (*Cell, error) {
	leftNode, err := parseDictNodeForCombine(left, state)
	if err != nil {
		return nil, err
	}
	rightNode, err := parseDictNodeForCombine(right, state)
	if err != nil {
		return nil, err
	}
	if leftNode.remainingKeyBits() != rightNode.remainingKeyBits() {
		return nil, fmt.Errorf("cannot combine dictionary views with different remaining key sizes: %d != %d", leftNode.remainingKeyBits(), rightNode.remainingKeyBits())
	}

	common, err := commonDictCombineLabelPrefix(&leftNode, &rightNode)
	if err != nil {
		return nil, err
	}
	switch {
	case common < leftNode.visibleLabelLen() && common < rightNode.visibleLabelLen():
		return combineDivergedDictRoots(leftNode, rightNode, common, state)

	case common == leftNode.visibleLabelLen() && common == rightNode.visibleLabelLen():
		return combineSameLabelDictRoots(leftNode, rightNode, state)

	case common == leftNode.visibleLabelLen():
		return combineDictIntoLeftFork(leftNode, rightNode, common, state)

	default:
		return combineDictIntoRightFork(leftNode, rightNode, common, state)
	}
}

func combineDivergedDictRoots(leftNode, rightNode dictCombineNode, common uint, state *dictCombineState) (*Cell, error) {
	leftBit := leftNode.visibleLabelBit(common)
	rightBit := rightNode.visibleLabelBit(common)
	if leftBit == rightBit {
		return nil, fmt.Errorf("invalid dictionary labels: divergent labels share branch bit")
	}

	remaining := leftNode.remainingKeyBits()
	prefix := leftNode.visibleLabelValue(0, common)
	childKeySz := remaining - common - 1

	leftChild, err := materializeDictNodeSuffixForCombine(leftNode, common+1, childKeySz, state)
	if err != nil {
		return nil, err
	}
	rightChild, err := materializeDictNodeSuffixForCombine(rightNode, common+1, childKeySz, state)
	if err != nil {
		return nil, err
	}

	if leftBit == 0 {
		return storeDictForkForCombine(&prefix, leftChild, rightChild, remaining, state)
	}
	return storeDictForkForCombine(&prefix, rightChild, leftChild, remaining, state)
}

func combineSameLabelDictRoots(leftNode, rightNode dictCombineNode, state *dictCombineState) (*Cell, error) {
	remaining := leftNode.remainingKeyBits()
	label := leftNode.visibleLabelValue(0, leftNode.visibleLabelLen())

	if leftNode.leaf || rightNode.leaf {
		if !leftNode.leaf || !rightNode.leaf {
			return nil, fmt.Errorf("invalid dictionary node: leaf/fork mismatch on equal label")
		}

		state.payload = Builder{trace: state.trace}
		leftValue, rightValue := leftNode.payload, rightNode.payload
		if err := state.combine(&leftValue, &rightValue, &state.payload); err != nil {
			return nil, err
		}
		return storeDictNodeTraced(&label, &state.payload, remaining, state.trace)
	}

	if leftNode.visibleLabelLen() >= remaining {
		return nil, fmt.Errorf("invalid dictionary fork label length %d for key size %d", leftNode.visibleLabelLen(), remaining)
	}

	childKeySz := remaining - leftNode.visibleLabelLen() - 1
	leftChild, err := combineDictRootViews(leftNode.leftChildView(childKeySz), rightNode.leftChildView(childKeySz), state)
	if err != nil {
		return nil, err
	}
	rightChild, err := combineDictRootViews(leftNode.rightChildView(childKeySz), rightNode.rightChildView(childKeySz), state)
	if err != nil {
		return nil, err
	}

	return storeDictForkForCombine(&label, leftChild, rightChild, remaining, state)
}

func combineDictIntoLeftFork(leftNode, rightNode dictCombineNode, common uint, state *dictCombineState) (*Cell, error) {
	if leftNode.leaf {
		return nil, fmt.Errorf("invalid dictionary node: leaf cannot contain a longer key")
	}

	remaining := leftNode.remainingKeyBits()
	branchBit := rightNode.visibleLabelBit(common)
	childKeySz := remaining - common - 1
	longerView := rightNode.consumeVisibleLabelBits(common + 1)

	leftChild, rightChild := leftNode.left, leftNode.right
	var err error
	if branchBit == 0 {
		leftChild, err = combineDictRootViews(leftNode.leftChildView(childKeySz), longerView, state)
	} else {
		rightChild, err = combineDictRootViews(leftNode.rightChildView(childKeySz), longerView, state)
	}
	if err != nil {
		return nil, err
	}

	label := leftNode.visibleLabelValue(0, leftNode.visibleLabelLen())
	return storeDictForkForCombine(&label, leftChild, rightChild, remaining, state)
}

func combineDictIntoRightFork(leftNode, rightNode dictCombineNode, common uint, state *dictCombineState) (*Cell, error) {
	if rightNode.leaf {
		return nil, fmt.Errorf("invalid dictionary node: leaf cannot contain a longer key")
	}

	remaining := rightNode.remainingKeyBits()
	branchBit := leftNode.visibleLabelBit(common)
	childKeySz := remaining - common - 1
	longerView := leftNode.consumeVisibleLabelBits(common + 1)

	leftChild, rightChild := rightNode.left, rightNode.right
	var err error
	if branchBit == 0 {
		leftChild, err = combineDictRootViews(longerView, rightNode.leftChildView(childKeySz), state)
	} else {
		rightChild, err = combineDictRootViews(longerView, rightNode.rightChildView(childKeySz), state)
	}
	if err != nil {
		return nil, err
	}

	label := rightNode.visibleLabelValue(0, rightNode.visibleLabelLen())
	return storeDictForkForCombine(&label, leftChild, rightChild, remaining, state)
}

func parseDictNodeForCombine(view dictCombineRootView, state *dictCombineState) (dictCombineNode, error) {
	if view.cell.IsSpecial() && !view.cell.IsLazy() {
		return dictCombineNode{}, fmt.Errorf("dictionary merge does not support special cells inside dict tree: %v", view.cell.GetType())
	}

	if err := view.cell.BeginParseInto(&state.loader); err != nil {
		return dictCombineNode{}, err
	}
	if trace := state.loader.Trace(); trace != nil {
		if err := trace.PendingError(); err != nil {
			return dictCombineNode{}, err
		}
	}
	if state.loader.cell.IsSpecial() {
		return dictCombineNode{}, fmt.Errorf("dictionary merge does not support special cells inside dict tree: %v", state.loader.cell.GetType())
	}

	labelLen, label, err := readLabelView(view.keySz, &state.loader)
	if err != nil {
		return dictCombineNode{}, fmt.Errorf("failed to load dictionary label: %w", err)
	}
	if view.skip > labelLen {
		return dictCombineNode{}, fmt.Errorf("invalid dictionary label skip %d for label length %d", view.skip, labelLen)
	}

	node := dictCombineNode{
		view:     view,
		label:    label,
		labelLen: labelLen,
		payload:  state.loader,
		leaf:     labelLen == view.keySz,
	}
	if node.leaf {
		return node, nil
	}

	left, err := state.loader.LoadRefCell()
	if err != nil {
		return dictCombineNode{}, fmt.Errorf("failed to load dictionary left fork: %w", err)
	}
	right, err := state.loader.LoadRefCell()
	if err != nil {
		return dictCombineNode{}, fmt.Errorf("failed to load dictionary right fork: %w", err)
	}
	if state.loader.BitsLeft() != 0 || state.loader.RefsNum() != 0 {
		return dictCombineNode{}, fmt.Errorf("dictionary fork has trailing data")
	}

	node.left = left
	node.right = right
	return node, nil
}

func storeDictForkForCombine(label *Slice, left, right *Cell, keySz uint, state *dictCombineState) (*Cell, error) {
	state.node = Builder{trace: state.trace}
	if err := storeDictLabel(&state.node, label, keySz); err != nil {
		return nil, err
	}
	if err := state.node.StoreRefUncheckedDepth(left); err != nil {
		return nil, err
	}
	if err := state.node.StoreRefUncheckedDepth(right); err != nil {
		return nil, err
	}
	return state.node.EndCellSpecial(false)
}

// materializeDictNodeSuffixForCombine re-emits one side of a diverged pair as
// the child of the fork being built, with the shared prefix and the branch bit
// dropped from its label.
//
// It never reuses the node's own cell. Both callers reach it from
// combineDivergedDictRoots with skip = common+1, so at least the branch bit is
// always being consumed and the label always has to be rewritten; a reuse fast
// path here would be unreachable by construction, not merely unlikely.
func materializeDictNodeSuffixForCombine(node dictCombineNode, skip, keySz uint, state *dictCombineState) (*Cell, error) {
	node.payload.ToBuilderInto(&state.payload)
	label := node.visibleLabelValue(skip, node.visibleLabelLen()-skip)
	return storeDictNodeTraced(&label, &state.payload, keySz, state.trace)
}

// commonDictCombineLabelPrefix reports how many leading label bits the two
// nodes agree on. The error is returned rather than raised: this runs on parsed
// chain data — extra-currency arithmetic over message values reaches it — where
// a malformed dictionary must reject the value it came from, not stop the node.
func commonDictCombineLabelPrefix(left, right *dictCombineNode) (uint, error) {
	limit := left.visibleLabelLen()
	if right.visibleLabelLen() < limit {
		limit = right.visibleLabelLen()
	}
	leftLabel := left.visibleLabelValue(0, limit)
	rightLabel := right.visibleLabelValue(0, limit)
	matched, err := commonSlicePrefix(&leftLabel, &rightLabel, limit)
	if err != nil {
		return 0, fmt.Errorf("failed to match dictionary labels: %w", err)
	}
	return matched, nil
}

func (n *dictCombineNode) remainingKeyBits() uint {
	return n.view.keySz - n.view.skip
}

func (n *dictCombineNode) visibleLabelLen() uint {
	return n.labelLen - n.view.skip
}

func (n *dictCombineNode) visibleLabelBit(bit uint) uint64 {
	value, _ := n.label.BitAt(n.view.skip + bit)
	return uint64(value)
}

func (n *dictCombineNode) visibleLabelValue(start, length uint) Slice {
	label := n.label
	label.bitStart += uint16(n.view.skip + start)
	label.bitEnd = label.bitStart + uint16(length)
	return label
}

func (n *dictCombineNode) consumeVisibleLabelBits(bits uint) dictCombineRootView {
	view := n.view
	if view.cell.IsLazy() {
		// Only a consumed lazy root needs a resident handoff. The parsed payload
		// already carries its cell and path trace, so ordinary views stay small.
		view.cell = n.payload.cell.WithTrace(n.payload.trace)
	}
	view.skip += bits
	return view
}

func (n *dictCombineNode) leftChildView(keySz uint) dictCombineRootView {
	return dictCombineRootView{cell: n.left, keySz: keySz}
}

func (n *dictCombineNode) rightChildView(keySz uint) dictCombineRootView {
	return dictCombineRootView{cell: n.right, keySz: keySz}
}
