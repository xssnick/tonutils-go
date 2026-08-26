package cell

import (
	"fmt"
	"math/big"
)

func combinedCellTrace(cell *Cell, trace *Trace) *Trace {
	if cell == nil {
		return nil
	}
	return CombineTraces(cell.Trace(), trace)
}

func initIntKeyBuilder(key *big.Int, bits uint, builder *Builder) {
	if key == nil {
		panic(ErrNilBigInt)
	}
	if bits > 1023 {
		panic(ErrTooBigSize)
	}
	if bits == 0 {
		if key.Sign() != 0 {
			panic(ErrTooBigValue)
		}
		*builder = Builder{}
		return
	}

	bitLen := uint(key.BitLen())
	if key.Sign() >= 0 {
		if bitLen > bits {
			panic(ErrTooBigValue)
		}
	} else if bitLen > bits || bitLen == bits && key.TrailingZeroBits() != bits-1 {
		panic(ErrTooBigValue)
	}

	*builder = Builder{bitsSz: bits}
	data := builder.data[:builder.usedBytes()]
	key.FillBytes(data)
	if key.Sign() < 0 {
		carry := byte(1)
		for i := len(data) - 1; i >= 0; i-- {
			v := ^data[i] + carry
			if carry != 0 && v != 0 {
				carry = 0
			}
			data[i] = v
		}
	}

	if rem := bits % 8; rem != 0 {
		data[0] &= byte(1<<rem) - 1
		shift := 8 - rem
		var carry byte
		for i := len(data) - 1; i >= 0; i-- {
			nextCarry := data[i] >> rem
			data[i] = data[i]<<shift | carry
			carry = nextCarry
		}
	}
}

func initUintKeyBuilder(key uint64, bits uint, builder *Builder) error {
	if bits > 1023 {
		return ErrTooBigSize
	}

	*builder = Builder{}
	if bits <= 64 {
		return builder.StoreUInt(key, bits)
	}

	// StoreUInt accepts widths above 64 through big.Int. A uint key in a wider
	// dictionary is just zero-extended, so reserve those leading zero bits in
	// the already-zeroed inline buffer and write the only significant 64 bits.
	builder.bitsSz = bits - 64
	return builder.StoreUInt(key, 64)
}

func initFixedDictBytesKeySlice(key []byte, bits uint, cell *Cell, keySlice *Slice) error {
	if bits > maxDictKeyBits {
		return fmt.Errorf("dict key size exceeds %d bits", maxDictKeyBits)
	}
	if uint(len(key))*8 < bits {
		return fmt.Errorf("incorrect key size")
	}

	*cell = Cell{data: key, bitsSz: uint16(bits)}
	*keySlice = Slice{cell: cell, bitEnd: cell.bitsSz}
	return nil
}

func initFixedDictUintKeySlice(key uint64, bits uint, builder *Builder, cell *Cell, keySlice *Slice) error {
	if err := initUintKeyBuilder(key, bits, builder); err != nil {
		return err
	}

	*cell = Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	*keySlice = Slice{cell: cell, bitEnd: cell.bitsSz}
	return nil
}

func (c *Slice) loadMaybeRefCell() (*Cell, bool, error) {
	has, err := c.LoadBoolBit()
	if err != nil {
		return nil, false, err
	}
	if !has {
		return nil, false, nil
	}

	ref, err := c.LoadRefCell()
	return ref, true, err
}

type fixedDictNode struct {
	cell     *Cell
	refView  cellRefView
	loader   Slice
	label    Slice
	labelLen uint
}

// static bit sources for hml_same labels: a view over these cells behaves
// exactly like a materialized label without building one per node
var dictSameOnesCell, dictSameZerosCell = func() (*Cell, *Cell) {
	ones := make([]byte, maxCellDataBytes)
	for i := range ones {
		ones[i] = 0xFF
	}
	return &Cell{data: ones, bitsSz: 1023}, &Cell{data: make([]byte, maxCellDataBytes), bitsSz: 1023}
}()

// readLabelView decodes a dict edge label as a zero-copy slice view: for
// short/long forms it points into the node payload, for hml_same it points
// into the static all-ones/all-zeros cells.
func readLabelView(sz uint, loader *Slice) (uint, Slice, error) {
	first, err := loader.LoadUInt(1)
	if err != nil {
		return 0, Slice{}, err
	}

	// hml_short$0
	if first == 0 {
		ln, err := loadUnaryLength(loader)
		if err != nil {
			return 0, Slice{}, err
		}
		if ln > sz {
			return 0, Slice{}, ErrLabelExceedsKeyBits
		}
		return labelDataView(loader, ln)
	}

	second, err := loader.LoadUInt(1)
	if err != nil {
		return 0, Slice{}, err
	}

	bitsLen := dictLabelSizeBits(sz)

	// hml_long$10
	if second == 0 {
		ln, err := loader.LoadUInt(bitsLen)
		if err != nil {
			return 0, Slice{}, err
		}
		if ln > uint64(sz) {
			return 0, Slice{}, ErrLabelExceedsKeyBits
		}
		return labelDataView(loader, uint(ln))
	}

	// hml_same$11
	bitType, err := loader.LoadUInt(1)
	if err != nil {
		return 0, Slice{}, err
	}
	ln, err := loader.LoadUInt(bitsLen)
	if err != nil {
		return 0, Slice{}, err
	}
	if ln > uint64(sz) {
		return 0, Slice{}, ErrLabelExceedsKeyBits
	}

	src := dictSameZerosCell
	if bitType == 1 {
		src = dictSameOnesCell
	}
	return uint(ln), Slice{cell: src, bitEnd: uint16(ln)}, nil
}

func labelDataView(loader *Slice, ln uint) (uint, Slice, error) {
	if loader.BitsLeft() < ln {
		return 0, Slice{}, ErrNotEnoughData(int(loader.BitsLeft()), int(ln))
	}
	view := Slice{
		cell:     loader.cell,
		bitStart: loader.bitStart,
		bitEnd:   loader.bitStart + uint16(ln),
	}
	if err := loader.SkipBits(ln); err != nil {
		return 0, Slice{}, err
	}
	return ln, view, nil
}

func parseFixedDictNode(branch *Cell, remaining uint) (fixedDictNode, error) {
	return parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
}

// resolveIfSpecial re-parses a node whose cell turned out to be special
// (a library) through the walk's resolver, so a walk inside the VM sees the
// resolved ordinary node. It is a no-op for the ordinary nodes that make up
// the whole of a well-formed tree, so it stays out of the descent's way.
func (n *fixedDictNode) resolveIfSpecial(remaining uint, trace *Trace, resolver DictSpecialResolver) error {
	if !n.cell.IsSpecial() {
		return nil
	}
	return n.resolveSpecialNode(remaining, trace, resolver)
}

// resolveSpecialNode carries the actual resolution. The walk charges the node
// cell load first and resolves a library only afterwards — the parse that
// produced this node already performed that charge through the trace. Without
// a resolver the node is left as it is, for the caller's own special-cell
// rejection to report.
func (n *fixedDictNode) resolveSpecialNode(remaining uint, trace *Trace, resolver DictSpecialResolver) error {
	if resolver == nil {
		resolver = trace.dictSpecialResolver()
		if resolver == nil {
			return nil
		}
	}

	resolved, err := resolver.ResolveDictNodeCell(n.cell)
	if err != nil {
		return err
	}
	// The resolver already charged the resolved cell per version rules;
	// parse it charge-free and rebind the gas trace for its children.
	resolvedNode, err := parseFixedDictNodeWithTrace(resolved, remaining, nil)
	if err != nil {
		return err
	}
	resolvedNode.loader.SetTrace(CombineTraces(resolved.Trace(), trace))
	*n = resolvedNode
	return nil
}

func parseFixedDictNodeWithTrace(branch *Cell, remaining uint, trace *Trace) (fixedDictNode, error) {
	var loader Slice
	if err := branch.BeginParseIntoWithTrace(&loader, trace); err != nil {
		return fixedDictNode{}, err
	}
	refView := newCellRefView(loader.cell)
	if loader.cell.IsSpecial() {
		return fixedDictNode{cell: loader.cell, refView: refView, loader: loader}, nil
	}
	labelLen, label, err := readLabelView(remaining, &loader)
	if err != nil {
		return fixedDictNode{}, err
	}

	return fixedDictNode{
		cell:     loader.cell,
		refView:  refView,
		loader:   loader,
		label:    label,
		labelLen: labelLen,
	}, nil
}

// labelSlice returns a fresh consumable copy of the label view.
func (n *fixedDictNode) labelSlice() Slice {
	return n.label
}

func (c Slice) slice(start, length uint) *Slice {
	c.bitStart += uint16(start)
	c.bitEnd = c.bitStart + uint16(length)
	c.refEnd = c.refStart
	return &c
}

func (n fixedDictNode) isLeaf(remaining uint) bool {
	return n.labelLen == remaining
}

func (n fixedDictNode) nextKeyBits(remaining uint) uint {
	return remaining - n.labelLen - 1
}

func (n *fixedDictNode) ref(i int) (*Cell, error) {
	return n.loader.peekRefCellAt(i)
}

func (n *fixedDictNode) refAndTrace(i int) (*Cell, *Trace, error) {
	return n.loader.refAndTraceAt(i)
}

func (n *fixedDictNode) boundaryRef(i int) (*Cell, error) {
	if i < 0 {
		return nil, ErrNegative
	}
	if i >= n.loader.RefsNum() {
		return nil, ErrNoMoreRefs
	}
	return n.loader.withChildTrace(n.loader.boundaryRefCellAt(i), int(n.loader.refStart)+i), nil
}

// value returns the only heap-owned slice created while walking a node. The
// parser and fork descent keep Slice values inline; a copy escapes only when a
// leaf value is handed to the caller.
func (n fixedDictNode) value() *Slice {
	value := n.loader
	return &value
}

// DictSpecialResolver resolves a special (library) cell met as a dictionary
// node into its ordinary content, as a cell load inside the VM does. When
// nil, special nodes fail the walk. It is an interface rather than a function
// so that installing it on a dictionary costs no allocation.
type DictSpecialResolver interface {
	ResolveDictNodeCell(*Cell) (*Cell, error)
}

func resolveDictNodeCell(branch *Cell, trace *Trace, resolver DictSpecialResolver, kind string) (*Cell, error) {
	if !branch.IsSpecial() {
		return branch, nil
	}
	if resolver == nil {
		resolver = trace.dictSpecialResolver()
		if resolver == nil {
			return nil, fmt.Errorf("%s %w", kind, ErrDictHasSpecialCells)
		}
	}
	return resolver.ResolveDictNodeCell(branch)
}

func (n fixedDictNode) rejectSpecial(kind string) error {
	if n.cell.IsSpecial() {
		return fmt.Errorf("%s %w", kind, ErrDictHasSpecialCells)
	}
	return nil
}

// validateForkShape enforces the dictionary node shape: a fork carries its
// label and exactly two references. lenient relaxes it to the augmented-tree
// rules, where payload after the label is legal because it carries the
// augmentation. The check fails even when the looked-up key does not match the
// node's label.
func (n *fixedDictNode) validateForkShape(remaining uint, lenient bool) error {
	if n.labelLen >= remaining {
		return nil
	}
	if lenient {
		if n.loader.RefsNum() < 2 {
			return ErrInvalidDictForkNode
		}
		return nil
	}
	if n.loader.BitsLeft() != 0 || n.loader.RefsNum() != 2 {
		return ErrInvalidDictForkNode
	}
	return nil
}

// dictWalk carries the settings that travel together through a fixed-dictionary
// walk: the trace node loads are charged to, the resolver consulted when the
// walk meets a special node, and whether fork nodes follow the lenient
// augmented-dictionary shape rules.
type dictWalk struct {
	trace    *Trace
	resolver DictSpecialResolver
	lenient  bool
}

func (n fixedDictNode) prunedBoundary(kind string) (bool, error) {
	if !n.cell.IsSpecial() {
		return false, nil
	}
	if n.cell.GetType() == PrunedCellType {
		return true, nil
	}
	return false, fmt.Errorf("%s has unsupported special cell in tree structure", kind)
}

func (n *fixedDictNode) cloneWithRef(i int, ref *Cell, trace *Trace) (*Cell, bool, error) {
	if ref == nil {
		return nil, false, ErrRefCannotBeNil
	}
	if i < 0 {
		return nil, false, ErrNegative
	}
	if i >= int(n.refView.refCnt) {
		return nil, false, ErrNoMoreRefs
	}
	if n.loader.trace == nil {
		return n.refView.cloneWithRef(i, ref, trace)
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:n.refView.refCnt]
	for j := range refs {
		var err error
		refs[j], err = n.ref(j)
		if err != nil {
			return nil, false, err
		}
	}
	refs[i] = ref

	// Unchanged siblings must be copied through the parsed node view. Its child
	// traces retain predecessor proof edges even when the dictionary also has a
	// detached VM trace; copying raw refs would make rebuilt Patricia ancestors
	// lose the only path that can materialize a reused predecessor subtree.
	return n.refView.cloneWithRefs(refs, trace)
}

// hasCanonicalLabel reports whether the source node uses the same HmLabel
// representation append_dict_label would choose. Mutating dictionary walks in
// the reference rebuild every changed ancestor and therefore canonicalize a
// valid but non-minimal label on that path.
func (n *fixedDictNode) hasCanonicalLabel(maxLen uint) (bool, error) {
	canonical, decided := n.canonicalLabelFast(maxLen)
	if decided {
		return canonical, nil
	}

	label := n.labelSlice()
	_, same, err := dictLabelSliceSameBit(&label, n.labelLen)
	if err != nil {
		return false, err
	}
	return !same, nil
}

// canonicalLabelFast settles all canonical generated labels except an
// explicit short/long label for which hml_same would be shorter. Only that
// adversarial case needs to scan the label bits.
func (n *fixedDictNode) canonicalLabelFast(maxLen uint) (canonical, decided bool) {
	ln := n.labelLen
	lengthBits := dictLabelSizeBits(maxLen)
	sameOptimal := ln > 1 && lengthBits < 2*ln-1
	if n.label.cell != n.cell {
		return sameOptimal, true
	}

	isLong := n.cell.data[0]&0x80 != 0
	if isLong != (lengthBits < ln) {
		return false, true
	}
	if sameOptimal {
		return false, false
	}
	return true, true
}

func (n *fixedDictNode) rebuildNonCanonicalFixedForkWithRef(i int, ref *Cell, remaining uint, trace *Trace) (*Cell, bool, error) {
	left, err := n.ref(0)
	if err != nil {
		return nil, false, err
	}
	right, err := n.ref(1)
	if err != nil {
		return nil, false, err
	}
	if i == 0 {
		left = ref
	} else {
		right = ref
	}

	var payload Builder
	payload.SetTrace(trace)
	if err = payload.StoreRefUncheckedDepth(left); err != nil {
		return nil, false, err
	}
	if err = payload.StoreRefUncheckedDepth(right); err != nil {
		return nil, false, err
	}
	label := n.labelSlice()
	rebuilt, err := storeDictNodeTraced(&label, &payload, remaining, trace)
	return rebuilt, err == nil, err
}

func (n fixedDictNode) splitLabel(matched uint) (Slice, Slice, error) {
	remainder := n.label
	if err := remainder.SkipBits(matched + 1); err != nil {
		return Slice{}, Slice{}, fmt.Errorf("failed to skip label edge bit: %w", err)
	}

	prefix := n.label
	prefix.bitEnd = prefix.bitStart + uint16(matched)
	return prefix, remainder, nil
}

// mergedEdgeLabel builds label + edge bit + neighbour label for delete-merge.
func (n *fixedDictNode) mergedEdgeLabel(bit uint64, label *Slice, name string) (*Slice, error) {
	merged := BeginCell()
	own := n.labelSlice()
	if err := merged.storeSliceFromSlice(&own, n.labelLen); err != nil {
		return nil, fmt.Errorf("failed to append %s base label: %w", name, err)
	}
	if err := merged.StoreUInt(bit, 1); err != nil {
		return nil, fmt.Errorf("failed to append %s edge bit: %w", name, err)
	}
	if err := merged.storeSliceFromSlice(label, label.BitsLeft()); err != nil {
		return nil, fmt.Errorf("failed to append %s label: %w", name, err)
	}
	return builderSliceView(merged), nil
}

func builderSliceView(b *Builder) *Slice {
	return &Slice{
		cell:   &Cell{data: b.data[:b.usedBytes()], bitsSz: uint16(b.bitsSz)},
		bitEnd: uint16(b.bitsSz),
	}
}

func matchLabelView(label Slice, labelLen uint, key *Slice) (matched uint, newRight bool, diverged bool, err error) {
	labelSlice := &label
	limit := min(labelLen, key.BitsLeft())
	if limit == 0 {
		return 0, false, false, nil
	}

	matched, err = commonSlicePrefix(labelSlice, key, limit)
	if err != nil {
		return 0, false, false, err
	}

	consumed := matched
	if matched < limit {
		next, err := key.BitAt(matched)
		if err != nil {
			return 0, false, false, err
		}
		consumed++
		if err = key.SkipBits(consumed); err != nil {
			return 0, false, false, err
		}
		return matched, next != 0, true, nil
	}

	if err = key.SkipBits(consumed); err != nil {
		return 0, false, false, err
	}
	return matched, false, false, nil
}
