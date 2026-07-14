package cell

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

type Dictionary struct {
	keySz uint

	root *Cell

	trace *Trace
}

type HashmapKV struct {
	Key   *Cell
	Value *Cell
}

type DictKV struct {
	Key   *Slice
	Value *Slice
}

var ErrNoSuchKeyInDict = errors.New("no such key in dict")

func NewDict(keySz uint) *Dictionary {
	return &Dictionary{
		keySz: keySz,
	}
}

func (c *Cell) AsDict(keySz uint) *Dictionary {
	return &Dictionary{
		keySz: keySz,
		root:  c,
	}
}

// DictBulkKV is a key/value pair for NewDictFromItems. Only the first key-size
// bits of Key are used as the key.
type DictBulkKV struct {
	Key   []byte
	Value *Builder
}

// NewDictFromItems builds a dictionary from all key/value pairs at once,
// constructing the tree bottom-up so every node is finalized (hashed) exactly
// once instead of re-hashing the insert path per key. Dict serialization is
// canonical, so the result is bit-identical to sequential Set calls over the
// same items; on duplicate keys the last item wins, matching sequential Set.
// The items slice is reordered in place.
func NewDictFromItems(keySz uint, items []DictBulkKV) (*Dictionary, error) {
	if err := validateDictKeySize(keySz); err != nil {
		return nil, err
	}

	d := NewDict(keySz)
	if len(items) == 0 {
		return d, nil
	}

	keyBytes := int(keySz+7) / 8
	for i := range items {
		if len(items[i].Key) < keyBytes {
			return nil, fmt.Errorf("key of item %d is shorter than %d bits", i, keySz)
		}
		if items[i].Value == nil {
			return nil, fmt.Errorf("value builder of item %d is nil", i)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return compareDictBulkKeys(items[i].Key, items[j].Key, keySz) < 0
	})

	// keep the last item of each equal-key run, like sequential Set
	unique := items[:0]
	for i := 0; i < len(items); {
		j := i + 1
		for j < len(items) && compareDictBulkKeys(items[i].Key, items[j].Key, keySz) == 0 {
			j++
		}
		unique = append(unique, items[j-1])
		i = j
	}
	items = unique

	keyCells := make([]Cell, len(items))
	for i := range items {
		keyCells[i] = Cell{data: items[i].Key, bitsSz: uint16(keySz)}
	}

	root, err := d.buildFromSorted(items, keyCells, 0)
	if err != nil {
		return nil, err
	}
	d.root = root
	return d, nil
}

// buildFromSorted builds the subtree over items whose keys all share the
// first pos bits and returns its finalized node cell.
func (d *Dictionary) buildFromSorted(items []DictBulkKV, keyCells []Cell, pos uint) (*Cell, error) {
	remaining := d.keySz - pos
	if len(items) == 1 {
		label := Slice{cell: &keyCells[0], bitStart: uint16(pos), bitEnd: uint16(d.keySz)}
		return d.storeLeaf(&label, items[0].Value, remaining)
	}

	// with sorted distinct keys the common prefix of the whole run equals the
	// common prefix of its first and last keys, and they diverge before the end
	first := Slice{cell: &keyCells[0], bitStart: uint16(pos), bitEnd: uint16(d.keySz)}
	last := Slice{cell: &keyCells[len(items)-1], bitStart: uint16(pos), bitEnd: uint16(d.keySz)}
	common, err := commonSlicePrefix(&first, &last, remaining)
	if err != nil {
		return nil, err
	}

	split := pos + common
	mid := sort.Search(len(items), func(i int) bool {
		return items[i].Key[split/8]>>(7-split%8)&1 != 0
	})

	left, err := d.buildFromSorted(items[:mid], keyCells[:mid], split+1)
	if err != nil {
		return nil, err
	}
	right, err := d.buildFromSorted(items[mid:], keyCells[mid:], split+1)
	if err != nil {
		return nil, err
	}

	label := Slice{cell: &keyCells[0], bitStart: uint16(pos), bitEnd: uint16(split)}
	return d.storeFork(&label, left, right, remaining)
}

// compareDictBulkKeys compares the first keySz bits of two keys.
func compareDictBulkKeys(a, b []byte, keySz uint) int {
	full := keySz / 8
	if c := bytes.Compare(a[:full], b[:full]); c != 0 {
		return c
	}

	rest := keySz % 8
	if rest == 0 {
		return 0
	}
	mask := byte(0xFF) << (8 - rest)
	av, bv := a[full]&mask, b[full]&mask
	switch {
	case av < bv:
		return -1
	case av > bv:
		return 1
	}
	return 0
}

func (c *Slice) ToDict(keySz uint) (*Dictionary, error) {
	root, err := c.WithoutTrace().ToCell()
	if err != nil {
		return nil, err
	}

	if err = validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate dict: %w", err)
	}

	return (&Dictionary{
		keySz: keySz,
		root:  root,
	}).SetTrace(c.trace), nil
}

func (c *Slice) MustLoadDict(keySz uint) *Dictionary {
	ld, err := c.LoadDict(keySz)
	if err != nil {
		panic(err)
	}
	return ld
}

func (c *Slice) LoadDict(keySz uint) (*Dictionary, error) {
	root, has, err := c.loadMaybeRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load ref for dict, err: %w", err)
	}

	if !has {
		return (&Dictionary{
			keySz: keySz,
		}).SetTrace(c.trace), nil
	}

	if err = validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate dict: %w", err)
	}

	return (&Dictionary{
		keySz: keySz,
		root:  root,
	}).SetTrace(root.Trace()), nil
}

func (d *Dictionary) GetKeySize() uint {
	return d.keySz
}

func (d *Dictionary) Copy() *Dictionary {
	return &Dictionary{
		keySz: d.keySz,
		root:  d.root,
		trace: d.trace,
	}
}

func (d *Dictionary) SetTrace(trace *Trace) *Dictionary {
	if d == nil {
		return nil
	}
	d.trace = trace
	d.root = d.root.withTraceCombined(trace)
	return d
}

func (d *Dictionary) setRoot(root *Cell) {
	d.root = root
}

func (d *Dictionary) SetIntKey(key *big.Int, value *Cell) error {
	var builder Builder
	var cell Cell
	initIntKeyCell(key, d.keySz, &builder, &cell)
	return d.Set(&cell, value)
}

func (d *Dictionary) storeLeaf(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	if value == nil {
		return nil, nil
	}
	return storeDictNodeTraced(keyPfx, value, keyOffset, d.trace)
}

func (d *Dictionary) storeFork(label *Slice, left, right *Cell, keyOffset uint) (*Cell, error) {
	b := BeginCell().SetTrace(d.trace)
	if err := b.StoreRefUncheckedDepth(left); err != nil {
		return nil, err
	}
	if err := b.StoreRefUncheckedDepth(right); err != nil {
		return nil, err
	}

	return storeDictNodeTraced(label, b, keyOffset, d.trace)
}

func (d *Dictionary) Set(key, value *Cell) error {
	if value == nil {
		return d.Delete(key)
	}

	_, err := d.SetWithMode(key, value, DictSetModeSet)
	return err
}

func (d *Dictionary) SetBuilder(key *Cell, value *Builder) error {
	_, err := d.SetBuilderWithMode(key, value, DictSetModeSet)
	return err
}

func (d *Dictionary) SetWithMode(key, value *Cell, mode DictSetMode) (bool, error) {
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}

	return d.SetBuilderWithMode(key, value.ToBuilder(), mode)
}

func (d *Dictionary) SetBuilderWithMode(key *Cell, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if key == nil || key.BitsSize() != d.keySz {
		return false, fmt.Errorf("invalid key size")
	}
	if value == nil {
		return false, fmt.Errorf("value builder is nil")
	}

	keySlice, err := key.BeginParse()
	if err != nil {
		return false, fmt.Errorf("failed to load key: %w", err)
	}

	newRoot, _, changed, err := d.set(d.root, keySlice, d.keySz, value, mode)
	if err != nil {
		return false, fmt.Errorf("failed to set value in dict, err: %w", err)
	}
	if changed {
		d.setRoot(newRoot)
	}
	return changed, nil
}

func (d *Dictionary) set(branch *Cell, pfx *Slice, keyOffset uint, value *Builder, mode DictSetMode) (*Cell, *Slice, bool, error) {
	if branch == nil {
		if mode == DictSetModeReplace {
			return nil, nil, false, nil
		}
		leaf, err := d.storeLeaf(pfx, value, keyOffset)
		return leaf, nil, err == nil, err
	}

	node, err := parseFixedDictNode(branch, keyOffset)
	if err != nil {
		return nil, nil, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, nil, false, err
	}

	bitsMatches, isNewRight, _, err := matchLabelView(node.label, node.labelLen, pfx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}

	if bitsMatches == node.labelLen {
		if pfx.BitsLeft() == 0 {
			if mode == DictSetModeAdd {
				return node.cell, node.value(), false, nil
			}
			nodeLabel := node.labelSlice()
			leaf, err := d.storeLeaf(&nodeLabel, value, keyOffset)
			return leaf, node.value(), err == nil, err
		}

		refIdx := int(pfx.MustLoadUInt(1))
		ref, err := node.ref(refIdx)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load %d ref: %w", refIdx, err)
		}

		ref, oldValue, changed, err := d.set(ref, pfx, keyOffset-(bitsMatches+1), value, mode)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
		}
		if !changed {
			return node.cell, oldValue, false, nil
		}

		cloned, changed, err := node.cloneWithRef(refIdx, ref, d.trace)
		return cloned, oldValue, changed, err
	}

	if mode == DictSetModeReplace {
		return node.cell, nil, false, nil
	}

	prefixLabel, labelRemainder, err := node.splitLabel(bitsMatches)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to split old child label: %w", err)
	}

	oldChild := BeginCell().SetTrace(d.trace)
	if err = storeDictLabel(oldChild, labelRemainder, keyOffset-(bitsMatches+1)); err != nil {
		return nil, nil, false, fmt.Errorf("failed to store old child label: %w", err)
	}
	if err = oldChild.StoreBuilderUncheckedDepth(node.loader.ToBuilder()); err != nil {
		return nil, nil, false, fmt.Errorf("failed to store old child payload: %w", err)
	}

	newChild, err := d.storeLeaf(pfx, value, keyOffset-(bitsMatches+1))
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to store new child leaf: %w", err)
	}

	oldChildCell, err := oldChild.EndCellSpecial(false)
	if err != nil {
		return nil, nil, false, err
	}

	left, right := newChild, oldChildCell
	if isNewRight {
		left, right = right, left
	}

	newBranch, err := d.storeFork(prefixLabel, left, right, keyOffset)
	return newBranch, nil, err == nil, err
}

func (d *Dictionary) lookupDelete(branch *Cell, pfx *Slice, keyOffset uint) (*Slice, *Cell, bool, error) {
	if branch == nil {
		return nil, nil, false, nil
	}

	node, err := parseFixedDictNode(branch, keyOffset)
	if err != nil {
		return nil, nil, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, nil, false, err
	}

	nodeLabel := node.labelSlice()
	bitsMatches, err := consumeCommonPrefix(&nodeLabel, pfx, node.labelLen)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}
	if bitsMatches < node.labelLen {
		return nil, nil, false, nil
	}

	if pfx.BitsLeft() == 0 {
		return node.value(), nil, true, nil
	}

	refIdx := int(pfx.MustLoadUInt(1))
	ref, err := node.ref(refIdx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load %d ref: %w", refIdx, err)
	}

	nextKeyOffset := keyOffset - (bitsMatches + 1)
	removed, newChild, changed, err := d.lookupDelete(ref, pfx, nextKeyOffset)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
	}
	if !changed {
		return nil, nil, false, nil
	}

	if newChild == nil {
		otherIdx := refIdx ^ 1
		otherRef, err := node.ref(otherIdx)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour ref %d: %w", otherIdx, err)
		}

		slc, err := otherRef.BeginParse()
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour ref %d: %w", otherIdx, err)
		}

		_, otherLabel, err := loadLabel(nextKeyOffset, slc, BeginCell())
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour label: %w", err)
		}

		mergedLabel, err := node.mergedEdgeLabel(uint64(otherIdx), otherLabel, "neighbour")
		if err != nil {
			return nil, nil, false, err
		}

		merged, err := d.storeLeaf(mergedLabel, slc.ToBuilder(), keyOffset)
		if err != nil {
			return nil, nil, false, err
		}
		return removed, merged, true, nil
	}

	cloned, changed, err := node.cloneWithRef(refIdx, newChild, d.trace)
	return removed, cloned, changed, err
}

func (d *Dictionary) Delete(key *Cell) error {
	if d == nil {
		return nil
	}
	if key == nil || key.BitsSize() != d.keySz {
		return fmt.Errorf("incorrect key size")
	}

	keySlice, err := key.BeginParse()
	if err != nil {
		return fmt.Errorf("failed to load key: %w", err)
	}

	_, newRoot, changed, err := d.lookupDelete(d.root, keySlice, d.keySz)
	if err != nil {
		return err
	}
	if changed {
		d.setRoot(newRoot)
	}
	return nil
}

func (d *Dictionary) DeleteIntKey(key *big.Int) error {
	var builder Builder
	var cell Cell
	initIntKeyCell(key, d.keySz, &builder, &cell)
	return d.Delete(&cell)
}

// LoadValueByIntKey is the same as LoadValue, but constructs the key cell from int.
func (d *Dictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	var builder Builder
	if err := builder.StoreBigInt(key, d.keySz); err != nil {
		panic(err)
	}
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.findKeySlice(&keySlice)
}

// LoadValueByUintKey is the same as LoadValue, but constructs the key cell from uint,
// without a big.Int allocation.
func (d *Dictionary) LoadValueByUintKey(key uint64) (*Slice, error) {
	var builder Builder
	if err := builder.StoreUInt(key, d.keySz); err != nil {
		panic(err)
	}
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.findKeySlice(&keySlice)
}

// LoadValueByBytesKey is the same as LoadValue, but takes the key as raw
// big-endian bytes (the first key-size bits), without building and hashing
// a key cell.
func (d *Dictionary) LoadValueByBytesKey(key []byte) (*Slice, error) {
	if uint(len(key))*8 < d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	cell := Cell{data: key, bitsSz: uint16(d.keySz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.findKeySlice(&keySlice)
}

// SetBuilderByBytesKey is the same as SetBuilder, but takes the key as raw
// big-endian bytes (the first key-size bits), without building and hashing
// a key cell.
func (d *Dictionary) SetBuilderByBytesKey(key []byte, value *Builder) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	if uint(len(key))*8 < d.keySz {
		return fmt.Errorf("incorrect key size")
	}
	if value == nil {
		return fmt.Errorf("value builder is nil")
	}

	cell := Cell{data: key, bitsSz: uint16(d.keySz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	newRoot, _, changed, err := d.set(d.root, &keySlice, d.keySz, value, DictSetModeSet)
	if err != nil {
		return fmt.Errorf("failed to set value in dict, err: %w", err)
	}
	if changed {
		d.setRoot(newRoot)
	}
	return nil
}

// DeleteByBytesKey is the same as Delete, but takes the key as raw big-endian
// bytes (the first key-size bits), without building and hashing a key cell.
func (d *Dictionary) DeleteByBytesKey(key []byte) error {
	if d == nil {
		return nil
	}
	if uint(len(key))*8 < d.keySz {
		return fmt.Errorf("incorrect key size")
	}

	cell := Cell{data: key, bitsSz: uint16(d.keySz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	_, newRoot, changed, err := d.lookupDelete(d.root, &keySlice, d.keySz)
	if err != nil {
		return err
	}
	if changed {
		d.setRoot(newRoot)
	}
	return nil
}

func (d *Dictionary) LoadMin() (*Cell, *Slice, error) {
	return d.LoadMinMax(false, false)
}

func (d *Dictionary) LoadMax() (*Cell, *Slice, error) {
	return d.LoadMinMax(true, false)
}

func (d *Dictionary) LoadMinAndDelete() (*Cell, *Slice, error) {
	return d.LoadMinMaxAndDelete(false, false)
}

func (d *Dictionary) LoadMaxAndDelete() (*Cell, *Slice, error) {
	return d.LoadMinMaxAndDelete(true, false)
}

// LoadValue - searches key in the underline dict cell and returns its value
//
//	If key is not found ErrNoSuchKeyInDict will be returned
func (d *Dictionary) LoadValue(key *Cell) (*Slice, error) {
	if key == nil || key.BitsSize() != d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	return d.findKey(key)
}

func (d *Dictionary) LoadMinMax(fetchMax bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}

	key := BeginCell()
	branch := d.root
	remaining := d.keySz

	// the descent reuses a value slice, only the found value escapes
	var loader Slice
	for {
		if err := branch.BeginParseInto(&loader); err != nil {
			return nil, nil, err
		}
		if loader.cell.IsSpecial() {
			return nil, nil, fmt.Errorf("dict %w", ErrDictHasSpecialCells)
		}

		labelLen, keyBuilder, err := loadLabel(remaining, &loader, key)
		if err != nil {
			return nil, nil, err
		}
		key = keyBuilder
		remaining -= labelLen

		if remaining == 0 {
			value := loader
			return key.EndCell(), &value, nil
		}

		bit := fetchMax
		if key.BitsUsed() == 0 && invertFirst {
			bit = !bit
		}

		if err := key.StoreBoolBit(bit); err != nil {
			return nil, nil, err
		}

		refIdx := 0
		if bit {
			refIdx = 1
		}
		next, err := loader.peekRefCellAt(refIdx)
		if err != nil {
			return nil, nil, err
		}
		branch = next
		remaining--
	}
}

func (d *Dictionary) LoadMinMaxAndDelete(fetchMax bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}

	root, key, value, err := deleteFixedDictBoundary(d.root, d.keySz, BeginCell(), fetchMax, invertFirst, d.trace)
	if err != nil {
		return nil, nil, err
	}
	d.setRoot(root)
	return key, value, nil
}

func deleteFixedDictBoundary(root *Cell, remaining uint, prefix *Builder, fetchMax, invertFirst bool, trace *Trace) (*Cell, *Cell, *Slice, error) {
	node, err := parseFixedDictNode(root, remaining)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, nil, nil, err
	}

	saved := prefix.BitsUsed()
	defer prefix.truncateBits(saved)
	label := node.labelSlice()
	if err = prefix.storeSliceFromSlice(&label, node.labelLen); err != nil {
		return nil, nil, nil, err
	}
	if node.isLeaf(remaining) {
		// The reference VM implements REMMIN/REMMAX as a lookup walk followed
		// by a delete walk, loading every node on the path twice. This fused
		// walk visits each node once, so it replays the second-walk load
		// notifications (on success only) to keep cell-load gas identical.
		root.Trace().NotifyLoad(node.cell)
		return nil, prefix.EndCell(), node.value(), nil
	}

	refIdx := 0
	if fetchMax {
		refIdx = 1
	}
	if prefix.BitsUsed() == 0 && invertFirst {
		refIdx ^= 1
	}
	if err = prefix.StoreUInt(uint64(refIdx), 1); err != nil {
		return nil, nil, nil, err
	}

	childRemaining := node.nextKeyBits(remaining)
	child, err := node.ref(refIdx)
	if err != nil {
		return nil, nil, nil, err
	}
	newChild, key, value, err := deleteFixedDictBoundary(child, childRemaining, prefix, fetchMax, invertFirst, trace)
	if err != nil {
		return nil, nil, nil, err
	}
	root.Trace().NotifyLoad(node.cell)
	if newChild == nil {
		otherIdx := refIdx ^ 1
		other, err := node.ref(otherIdx)
		if err != nil {
			return nil, nil, nil, err
		}
		merged, err := mergeFixedDictSurvivor(node, uint64(otherIdx), other, remaining, trace)
		return merged, key, value, err
	}

	cloned, _, err := node.cloneWithRef(refIdx, newChild, trace)
	return cloned, key, value, err
}

func (d *Dictionary) LoadValueAndSet(key, value *Cell) (*Slice, bool, error) {
	return d.LoadValueAndSetWithMode(key, value, DictSetModeSet)
}

func (d *Dictionary) LoadValueAndSetWithMode(key, value *Cell, mode DictSetMode) (*Slice, bool, error) {
	if value == nil {
		return nil, false, fmt.Errorf("value is nil")
	}
	return d.LoadValueAndSetBuilderWithMode(key, value.ToBuilder(), mode)
}

func (d *Dictionary) LoadValueAndSetBuilder(key *Cell, value *Builder) (*Slice, bool, error) {
	return d.LoadValueAndSetBuilderWithMode(key, value, DictSetModeSet)
}

func (d *Dictionary) LoadValueAndSetBuilderWithMode(key *Cell, value *Builder, mode DictSetMode) (*Slice, bool, error) {
	if d == nil {
		return nil, false, fmt.Errorf("dict is nil")
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, false, fmt.Errorf("incorrect key size")
	}
	if value == nil {
		return nil, false, fmt.Errorf("value builder is nil")
	}

	keySlice, err := key.BeginParse()
	if err != nil {
		return nil, false, fmt.Errorf("failed to load key: %w", err)
	}

	newRoot, oldValue, changed, err := d.set(d.root, keySlice, d.keySz, value, mode)
	if err != nil {
		return nil, false, fmt.Errorf("failed to set value in dict, err: %w", err)
	}
	if changed {
		d.setRoot(newRoot)
	}
	return oldValue, changed, nil
}

func (d *Dictionary) LoadValueAndDelete(key *Cell) (*Slice, error) {
	if d == nil {
		return nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	keySlice, err := key.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}

	removed, newRoot, changed, err := d.lookupDelete(d.root, keySlice, d.keySz)
	if err != nil {
		return nil, err
	}
	if !changed || removed == nil || sameDictRoot(d.root, newRoot) {
		return nil, ErrNoSuchKeyInDict
	}

	d.setRoot(newRoot)
	return removed, nil
}

func sameDictRoot(a, b *Cell) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.HashKey() == b.HashKey()
}

// Deprecated: use LoadValue
func (d *Dictionary) Get(key *Cell) *Cell {
	slc, err := d.LoadValue(key)
	if err != nil {
		return nil
	}

	c, err := slc.ToCell()
	if err != nil {
		return nil
	}

	return c
}

func (d *Dictionary) IsEmpty() bool {
	return d == nil || d.root == nil || (d.root.BitsSize() == 0 && d.root.RefsNum() == 0)
}

func (d *Dictionary) LoadAll(skipPruned ...bool) ([]DictKV, error) {
	if d == nil || d.root == nil {
		return []DictKV{}, nil
	}
	return d.mapInner(nil, d.keySz, d.keySz, d.root, BeginCell(), len(skipPruned) > 0 && skipPruned[0])
}

func (d *Dictionary) mapInner(out []DictKV, keySz, leftKeySz uint, c *Cell, keyPrefix *Builder, skipPruned bool) ([]DictKV, error) {
	var err error
	var sz uint

	if c.IsSpecial() && (!c.IsLazy() || skipPruned) {
		if skipPruned && c.GetType() == PrunedCellType {
			// ignore pruned keys
			return out, nil
		}
		return nil, fmt.Errorf("dict %w, cannot load some values", ErrDictHasSpecialCells)
	}

	// the walk reuses a value slice, only leaf values escape
	var loader Slice
	if err = c.BeginParseInto(&loader); err != nil {
		return nil, err
	}
	if loader.cell.IsSpecial() {
		if skipPruned && loader.cell.GetType() == PrunedCellType {
			return out, nil
		}
		return nil, fmt.Errorf("dict %w, cannot load some values", ErrDictHasSpecialCells)
	}

	sz, keyPrefix, err = loadLabel(leftKeySz, &loader, keyPrefix)
	if err != nil {
		return nil, err
	}

	// until key size is not equals we go deeper
	if keyPrefix.BitsUsed() < keySz {
		// the walk shares one key builder: append a bit, descend, roll back
		saved := keyPrefix.BitsUsed()

		// 0 bit branch
		left, err := loadDictMapRef(&loader, skipPruned)
		if err != nil {
			return nil, err
		}

		out, err = d.mapInner(out, keySz, leftKeySz-(1+sz), left, keyPrefix.MustStoreUInt(0, 1), skipPruned)
		if err != nil {
			return nil, err
		}
		keyPrefix.truncateBits(saved)

		// 1 bit branch
		right, err := loadDictMapRef(&loader, skipPruned)
		if err != nil {
			return nil, err
		}
		out, err = d.mapInner(out, keySz, leftKeySz-(1+sz), right, keyPrefix.MustStoreUInt(1, 1), skipPruned)
		keyPrefix.truncateBits(saved)
		return out, err
	}

	value := loader
	return append(out, DictKV{
		Key:   keyPrefix.ToSlice(),
		Value: &value,
	}), nil
}

func loadDictMapRef(loader *Slice, skipPruned bool) (*Cell, error) {
	if skipPruned {
		return loader.loadRefCell(true)
	}
	return loader.LoadRefCell()
}

func (d *Dictionary) findKey(lookupKey *Cell) (*Slice, error) {
	var lKey Slice
	if err := lookupKey.BeginParseInto(&lKey); err != nil {
		return nil, fmt.Errorf("failed to load lookup key: %w", err)
	}
	return d.findKeySlice(&lKey)
}

func (d *Dictionary) findKeySlice(lKey *Slice) (*Slice, error) {
	branch := d.root
	if branch == nil {
		return nil, ErrNoSuchKeyInDict
	}

	// The descent reuses value slices, only the found value escapes.
	var branchSlice Slice
	for {
		if err := branch.BeginParseInto(&branchSlice); err != nil {
			return nil, err
		}
		if branchSlice.cell.IsSpecial() {
			return nil, fmt.Errorf("dict %w", ErrDictHasSpecialCells)
		}
		sz, matched, err := matchLabelPrefix(lKey.BitsLeft(), &branchSlice, lKey)
		if err != nil {
			return nil, err
		}

		if matched != sz {
			return nil, ErrNoSuchKeyInDict
		}

		if lKey.BitsLeft() == 0 {
			value := branchSlice
			return &value, nil
		}

		idx, err := lKey.LoadUInt(1)
		if err != nil {
			return nil, err
		}

		next, err := branchSlice.peekRefCellAt(int(idx))
		if err != nil {
			return nil, err
		}
		branch = next
	}
}

func (d *Dictionary) AsCell() *Cell {
	return d.root
}

func (d *Dictionary) ToCell() (*Cell, error) {
	if d == nil {
		return nil, nil
	}
	return d.root, nil
}

func (d *Dictionary) String() string {
	kv, err := d.LoadAll(true)
	if err != nil {
		return "{Corrupted Dict}"
	}

	list := make([]string, 0, len(kv))
	for _, dictKV := range kv {
		list = append(list, fmt.Sprintf("Key %s: Value %d bits, %d refs", dictKV.Key.String(), dictKV.Value.BitsLeft(), dictKV.Value.RefsNum()))
	}

	if len(list) == 0 {
		return "{}"
	}

	return "{\n\t" + strings.Join(list, "\n\t") + "\n}"
}
