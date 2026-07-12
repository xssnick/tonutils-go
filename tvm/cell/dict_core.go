package cell

import (
	"fmt"
	"math/big"
)

func initIntKeyCell(key *big.Int, bits uint, builder *Builder, cell *Cell) {
	if err := builder.StoreBigInt(key, bits); err != nil {
		panic(err)
	}
	*cell = Cell{
		data:   builder.data[:builder.usedBytes()],
		bitsSz: uint16(builder.bitsSz),
	}
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
	var loader Slice
	if err := branch.BeginParseInto(&loader); err != nil {
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

func (n fixedDictNode) rejectSpecial(kind string) error {
	if n.cell.IsSpecial() {
		return fmt.Errorf("%s %w", kind, ErrDictHasSpecialCells)
	}
	return nil
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
	return n.refView.cloneWithRef(i, ref, trace)
}

func (n fixedDictNode) splitLabel(matched uint) (*Slice, *Slice, error) {
	remainder := n.label
	if err := remainder.SkipBits(matched + 1); err != nil {
		return nil, nil, fmt.Errorf("failed to skip label edge bit: %w", err)
	}

	prefix := n.label
	prefix.bitEnd = prefix.bitStart + uint16(matched)
	return &prefix, &remainder, nil
}

// mergedEdgeLabel builds label + edge bit + neighbour label for delete-merge.
func (n *fixedDictNode) mergedEdgeLabel(bit uint64, label *Builder, name string) (*Slice, error) {
	merged := BeginCell()
	own := n.labelSlice()
	if err := merged.storeSliceFromSlice(&own, n.labelLen); err != nil {
		return nil, fmt.Errorf("failed to append %s base label: %w", name, err)
	}
	if err := merged.StoreUInt(bit, 1); err != nil {
		return nil, fmt.Errorf("failed to append %s edge bit: %w", name, err)
	}
	if err := merged.StoreBuilder(label); err != nil {
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
