package cell

import (
	"bytes"
	"fmt"
)

// AugMinRank reads the ordering rank a node's augmentation carries. It must be
// monotone over the tree — a fork's rank is the minimum of its subtree's ranks
// — which is what lets the iterator skip a subtree without opening it. extra is
// a borrowed view bounded by the augmentation's own SkipExtra and is valid only
// for the duration of the call.
type AugMinRank func(extra *Slice) (uint64, error)

// AugMinIteratorOptions configures MinIterator.
type AugMinIteratorOptions struct {
	// Rank is required.
	Rank AugMinRank
	// Prefix restricts the stream to keys starting with these bits. The descent
	// follows the prefix before any rank is consulted, so a stream over an
	// absent prefix opens O(len(Prefix)) nodes and ends immediately. This is
	// OutputQueueMerger::add_root -> replace_by_prefix.
	Prefix *Cell
	// TieBreakFrom is the first key bit compared when two entries carry equal
	// rank. Must be a multiple of 8. Bits before it take no part in the order.
	TieBreakFrom uint
}

// AugMinIterator streams an augmented dictionary in (rank, key[TieBreakFrom:])
// ascending order, opening a fork only when its subtree minimum can still be
// next.
//
// The stream is the Go form of block::OutputQueueMerger built over a single
// root: same descent, same fork-before-leaf expansion at equal rank, same
// 256-bit key-suffix tie-break, and the same rejection when a fork's
// augmentation is not the minimum of its two children.
//
// The root is captured at construction. Mutating the dictionary afterwards does
// not disturb the stream: it keeps serving the snapshot, exactly as a C++
// OutputQueueMerger built over out_msg_queue_->get_root_cell() keeps serving it
// while lookup_delete rebuilds the dictionary underneath.
type AugMinIterator struct {
	dict  *AugmentedDictionary
	root  *Cell
	trace *Trace
	rank  AugMinRank
	tieAt int // bytes

	heap []augMinNode
	// keys is the key arena: every live heap node owns one stride-sized slot,
	// so a node costs no allocation of its own. Slots are recycled through
	// free, which keeps a long stream over a deep trie from growing the arena
	// past the live frontier.
	keys   []byte
	stride int
	free   []int32
	// pending is the slot backing the emitted view. It is returned to free at
	// the next advance, which is exactly when the view stops being valid.
	pending int32

	view    AugDictItemView
	keyCell Cell
	rankOut uint64
	hasView bool
	done    bool
	err     error
}

// augMinNode is one positioned dictionary node: its parsed label and payload,
// the rank its augmentation carries, and the key bits accumulated above it.
type augMinNode struct {
	node      fixedDictNode
	value     Slice // payload after the augmentation; the leaf value
	extra     Slice // the augmentation itself
	rank      uint64
	keyOff    int32
	keyLen    uint16
	remaining uint16
	leaf      bool
}

// MinIterator opens a lazy stream over d in ascending augmentation order. See
// AugMinIterator for the ordering contract.
func (d *AugmentedDictionary) MinIterator(opts AugMinIteratorOptions) (*AugMinIterator, error) {
	if d == nil {
		return nil, fmt.Errorf("dict is nil")
	}
	if d.aug == nil {
		return nil, ErrAugmentationSemanticsUnavailable
	}
	if opts.Rank == nil {
		return nil, fmt.Errorf("aug min iterator requires a rank function")
	}
	if opts.TieBreakFrom%8 != 0 {
		return nil, fmt.Errorf("aug min iterator tie break offset %d is not a whole number of bytes", opts.TieBreakFrom)
	}
	if opts.TieBreakFrom > d.keySz {
		return nil, fmt.Errorf("aug min iterator tie break offset %d exceeds the %d bit key", opts.TieBreakFrom, d.keySz)
	}
	it := &AugMinIterator{
		dict:    d,
		root:    d.root,
		rank:    opts.Rank,
		tieAt:   int(opts.TieBreakFrom / 8),
		stride:  int((d.keySz + 7) / 8),
		pending: -1,
	}
	if d.root == nil {
		it.done = true
		return it, nil
	}
	it.trace = CombineTraces(d.root.Trace(), d.trace)
	node, err := it.open(d.root, d.keySz, it.trace, nil, 0)
	if err != nil {
		return nil, err
	}
	if opts.Prefix != nil {
		node, err = it.descendToPrefix(node, opts.Prefix)
		if err != nil {
			return nil, err
		}
		if node == nil {
			it.done = true
			return it, nil
		}
	}
	it.push(*node)
	return it, nil
}

// Next advances to the smallest remaining entry. It expands every fork whose
// rank ties the current minimum before emitting a leaf at that rank, which is
// what makes the suffix tie-break authoritative: no unopened subtree can then
// hold a smaller entry.
func (it *AugMinIterator) Next() bool {
	if it == nil || it.err != nil || it.done {
		return false
	}
	it.hasView = false
	if it.pending >= 0 {
		it.free = append(it.free, it.pending)
		it.pending = -1
	}
	for len(it.heap) > 0 {
		top := it.pop()
		if top.leaf {
			it.emit(top)
			return true
		}
		if err := it.split(top); err != nil {
			it.fail(err)
			return false
		}
	}
	it.done = true
	return false
}

// View returns the positioned entry. Key is the full key, Value starts after
// the augmentation and Extra covers it. All three are borrowed and invalidated
// by the next Next.
func (it *AugMinIterator) View() AugDictItemView {
	if it == nil || !it.hasView {
		return AugDictItemView{}
	}
	return it.view
}

// Rank is the augmentation rank of the positioned entry.
func (it *AugMinIterator) Rank() uint64 {
	if it == nil || !it.hasView {
		return 0
	}
	return it.rankOut
}

// Err reports the malformed-node or lazy-load error that ended the stream.
func (it *AugMinIterator) Err() error {
	if it == nil {
		return nil
	}
	return it.err
}

func (it *AugMinIterator) fail(err error) {
	it.err = err
	it.done = true
	it.hasView = false
}

func (it *AugMinIterator) emit(top augMinNode) {
	it.keyCell = Cell{
		data:   it.keys[top.keyOff : int(top.keyOff)+it.stride : int(top.keyOff)+it.stride],
		bitsSz: uint16(it.dict.keySz),
	}
	it.view = AugDictItemView{
		Key: Slice{
			cell:              &it.keyCell,
			bitEnd:            it.keyCell.bitsSz,
			forceCopyOnToCell: true,
		},
		Value: top.value,
		Extra: top.extra,
	}
	it.rankOut = top.rank
	it.pending = top.keyOff
	it.hasView = true
}

// open parses one node, decomposes its augmentation and reads its rank. The
// key slot is allocated here and carries parent ++ label ++ branch bit.
func (it *AugMinIterator) open(
	branch *Cell,
	remaining uint,
	trace *Trace,
	parent *augMinNode,
	branchBit int,
) (*augMinNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, trace)
	if err != nil {
		return nil, err
	}
	if err = node.resolveIfSpecial(remaining, trace, nil); err != nil {
		return nil, err
	}
	if err = node.rejectSpecial("aug dict"); err != nil {
		return nil, err
	}
	if err = node.validateForkShape(remaining, true); err != nil {
		return nil, err
	}
	out := augMinNode{node: node, remaining: uint16(remaining), leaf: node.isLeaf(remaining)}
	// The payload after the label is the augmentation in both node shapes:
	// ahmn_fork keeps its children in the ref array, ahmn_leaf stores
	// extra before value. This is the C++ prefetch_ulong(64)-after-label read.
	out.value = *node.value()
	out.extra = out.value
	if err = it.dict.skipExtra(&out.value); err != nil {
		return nil, err
	}
	out.extra.bitEnd = out.value.bitStart
	out.extra.refEnd = out.value.refStart
	if !out.leaf && (out.value.BitsLeft() != 0 || out.value.RefsNum() != 2) {
		// C++ invalidates a fork whose remainder after the augmentation is not
		// exactly two references and no data (size_ext() != 0x20000).
		return nil, ErrInvalidDictForkNode
	}
	rankView := out.extra
	if out.rank, err = it.rank(&rankView); err != nil {
		return nil, err
	}

	slot, err := it.allocKey()
	if err != nil {
		return nil, err
	}
	out.keyOff = slot
	dst := it.keys[slot : int(slot)+it.stride]
	for i := range dst {
		dst[i] = 0
	}
	var written uint
	if parent != nil {
		copyBits(dst, it.keys[parent.keyOff:int(parent.keyOff)+it.stride], uint(parent.keyLen))
		written = uint(parent.keyLen)
		setBit(dst, written, branchBit == 1)
		written++
	}
	label := node.labelSlice()
	if err = appendSliceBits(dst, written, &label, node.labelLen); err != nil {
		return nil, err
	}
	written += node.labelLen
	out.keyLen = uint16(written)

	return &out, nil
}

// descendToPrefix is OutputQueueMerger::MsgKeyValue::replace_by_prefix: follow
// the requested prefix from the captured root, opening exactly the nodes on it,
// and report an empty stream when the trie leaves the prefix. Nothing below the
// prefix is opened and no rank is consulted along the way.
func (it *AugMinIterator) descendToPrefix(node *augMinNode, prefix *Cell) (*augMinNode, error) {
	prefixLen := uint(prefix.BitsSize())
	if prefixLen == 0 {
		return node, nil
	}
	if prefixLen > it.dict.keySz {
		return nil, fmt.Errorf("aug min iterator prefix is longer than the key")
	}
	loader, err := prefix.BeginParse()
	if err != nil {
		return nil, err
	}
	prefixBits := make([]byte, it.stride)
	if err = loader.LoadSliceInto(prefixBits, prefixLen); err != nil {
		return nil, err
	}
	for {
		common := min(prefixLen, uint(node.keyLen))
		if !equalBits(it.keys[node.keyOff:int(node.keyOff)+it.stride], prefixBits, common) {
			it.releaseKey(node.keyOff)
			return nil, nil
		}
		if uint(node.keyLen) >= prefixLen {
			return node, nil
		}
		if node.leaf {
			// The key ends before the prefix does, so no key under the prefix
			// exists. C++ reaches the same conclusion through replace_with_child
			// failing on a non-fork.
			it.releaseKey(node.keyOff)
			return nil, nil
		}
		branch := 0
		if bitAt(prefixBits, uint(node.keyLen)) {
			branch = 1
		}
		child, childTrace, refErr := node.node.refAndTrace(branch)
		if refErr != nil {
			return nil, refErr
		}
		next, openErr := it.open(child, node.node.nextKeyBits(uint(node.remaining)), childTrace, node, branch)
		if openErr != nil {
			return nil, openErr
		}
		it.releaseKey(node.keyOff)
		node = next
	}
}

// split opens both children of a fork and pushes them. It enforces the C++
// invariant that a fork's augmentation is the minimum of its children's: the
// only thing that stops a forged augmentation from making the stream skip a
// subtree it should have opened.
func (it *AugMinIterator) split(parent augMinNode) error {
	remaining := parent.node.nextKeyBits(uint(parent.remaining))
	var children [2]*augMinNode
	for i := 0; i < 2; i++ {
		child, childTrace, err := parent.node.refAndTrace(i)
		if err != nil {
			return err
		}
		opened, err := it.open(child, remaining, childTrace, &parent, i)
		if err != nil {
			return err
		}
		children[i] = opened
	}
	it.releaseKey(parent.keyOff)
	low := min(children[0].rank, children[1].rank)
	if low != parent.rank {
		return fmt.Errorf("aug min iterator: fork rank %d is not the minimum %d of its children", parent.rank, low)
	}
	it.push(*children[0])
	it.push(*children[1])
	return nil
}

// less orders the frontier. Forks sort before leaves at equal rank so that
// every subtree that could still hold the next entry is expanded before any
// leaf of that rank is emitted; without it a leaf could be emitted ahead of a
// smaller-suffix sibling still folded inside a fork.
func (it *AugMinIterator) less(a, b *augMinNode) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	if a.leaf != b.leaf {
		return !a.leaf
	}
	return bytes.Compare(
		it.keys[int(a.keyOff)+it.tieAt:int(a.keyOff)+it.stride],
		it.keys[int(b.keyOff)+it.tieAt:int(b.keyOff)+it.stride],
	) < 0
}

func (it *AugMinIterator) push(n augMinNode) {
	it.heap = append(it.heap, n)
	i := len(it.heap) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !it.less(&it.heap[i], &it.heap[parent]) {
			break
		}
		it.heap[i], it.heap[parent] = it.heap[parent], it.heap[i]
		i = parent
	}
}

func (it *AugMinIterator) pop() augMinNode {
	top := it.heap[0]
	last := len(it.heap) - 1
	it.heap[0] = it.heap[last]
	it.heap[last] = augMinNode{}
	it.heap = it.heap[:last]
	i := 0
	for {
		left, right := 2*i+1, 2*i+2
		small := i
		if left < len(it.heap) && it.less(&it.heap[left], &it.heap[small]) {
			small = left
		}
		if right < len(it.heap) && it.less(&it.heap[right], &it.heap[small]) {
			small = right
		}
		if small == i {
			break
		}
		it.heap[i], it.heap[small] = it.heap[small], it.heap[i]
		i = small
	}
	return top
}

func (it *AugMinIterator) allocKey() (int32, error) {
	if n := len(it.free); n > 0 {
		slot := it.free[n-1]
		it.free = it.free[:n-1]
		return slot, nil
	}
	slot := len(it.keys)
	if slot > maxAugMinKeyArena {
		return 0, fmt.Errorf("aug min iterator key arena exceeded %d bytes", maxAugMinKeyArena)
	}
	it.keys = append(it.keys, make([]byte, it.stride)...)
	return int32(slot), nil
}

func (it *AugMinIterator) releaseKey(slot int32) {
	it.free = append(it.free, slot)
}

// maxAugMinKeyArena bounds the frontier a single stream may materialize. The
// heap only grows while forks tie the current minimum rank, so reaching this
// means the augmentation is degenerate; failing beats spending the block's
// memory on it.
const maxAugMinKeyArena = 1 << 26

func bitAt(buf []byte, i uint) bool {
	return buf[i/8]&(0x80>>(i%8)) != 0
}

func setBit(buf []byte, i uint, on bool) {
	if on {
		buf[i/8] |= 0x80 >> (i % 8)
	}
}

func copyBits(dst, src []byte, bits uint) {
	full := bits / 8
	copy(dst[:full], src[:full])
	if rest := bits % 8; rest != 0 {
		mask := byte(0xFF) << (8 - rest)
		dst[full] = (dst[full] &^ mask) | (src[full] & mask)
	}
}

func equalBits(a, b []byte, bits uint) bool {
	full := bits / 8
	if !bytes.Equal(a[:full], b[:full]) {
		return false
	}
	if rest := bits % 8; rest != 0 {
		mask := byte(0xFF) << (8 - rest)
		return a[full]&mask == b[full]&mask
	}
	return true
}

// appendSliceBits writes a label view into the key buffer at a bit offset. It
// mirrors Builder.storeSliceFromSlice without needing a Builder per node.
func appendSliceBits(dst []byte, at uint, label *Slice, bits uint) error {
	for bits > 0 {
		chunk := min(bits, 8)
		value, err := label.LoadUInt(chunk)
		if err != nil {
			return err
		}
		for i := uint(0); i < chunk; i++ {
			if value&(1<<(chunk-1-i)) != 0 {
				setBit(dst, at+i, true)
			}
		}
		at += chunk
		bits -= chunk
	}
	return nil
}
