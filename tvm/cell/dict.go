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

// AsDictWithTrace creates a traced dictionary view without copying the root
// cell. Dictionary traversals carry trace separately through child edges.
func (c *Cell) AsDictWithTrace(keySz uint, trace *Trace) *Dictionary {
	return &Dictionary{
		keySz: keySz,
		root:  c,
		trace: trace,
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

	sortDictBulkItems(items, keySz)

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

	arena := newDictBuildArena(2*len(items)-1, dictBulkArenaDataBytes(items, keySz))
	root, err := d.buildFromSorted(items, keyCells, 0, arena)
	if err != nil {
		return nil, err
	}
	d.root = root
	return d, nil
}

// buildFromSorted builds the subtree over items whose keys all share the
// first pos bits and returns its finalized node cell.
func (d *Dictionary) buildFromSorted(items []DictBulkKV, keyCells []Cell, pos uint, arena *dictBuildArena) (*Cell, error) {
	remaining := d.keySz - pos
	if len(items) == 1 {
		label := Slice{cell: &keyCells[0], bitStart: uint16(pos), bitEnd: uint16(d.keySz)}
		return d.storeLeafArena(arena, &label, items[0].Value, remaining)
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

	left, err := d.buildFromSorted(items[:mid], keyCells[:mid], split+1, arena)
	if err != nil {
		return nil, err
	}
	right, err := d.buildFromSorted(items[mid:], keyCells[mid:], split+1, arena)
	if err != nil {
		return nil, err
	}

	label := Slice{cell: &keyCells[0], bitStart: uint16(pos), bitEnd: uint16(split)}
	return d.storeForkArena(arena, &label, left, right, remaining)
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

// LoadOptionalDict loads a HashmapE and returns nil for its empty encoding.
// Use this when nil and an allocated empty Dictionary have the same domain
// meaning; LoadDict retains its historical always-non-nil result.
func (c *Slice) LoadOptionalDict(keySz uint) (*Dictionary, error) {
	if err := validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate dict: %w", err)
	}

	root, has, err := c.loadMaybeRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load ref for dict, err: %w", err)
	}
	if !has {
		return nil, nil
	}

	return (&Dictionary{
		keySz: keySz,
		root:  root,
	}).SetTrace(root.Trace()), nil
}

func (d *Dictionary) GetKeySize() uint {
	if d == nil {
		return 0
	}
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

func (d *Dictionary) tracedRoot() *Cell {
	return d.root.withTraceCombined(d.trace)
}

func (d *Dictionary) setRoot(root *Cell) {
	d.root = root
}

func (d *Dictionary) SetIntKey(key *big.Int, value *Cell) error {
	if value == nil {
		return d.DeleteIntKey(key)
	}
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	var keyBuilder Builder
	initIntKeyBuilder(key, d.keySz, &keyBuilder)
	keyCell := Cell{data: keyBuilder.data[:keyBuilder.usedBytes()], bitsSz: uint16(keyBuilder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	_, err := d.setValueWithModeSlice(&keySlice, dictSetValue{cell: value}, DictSetModeSet)
	return err
}

// dictSetValue keeps Cell-based setters from allocating a 192-byte Builder at
// the API boundary. The cell is converted into a stack-local builder only at
// the leaf that stores it; builder-based APIs retain their existing path.
type dictSetValue struct {
	builder *Builder
	cell    *Cell
}

func (d *Dictionary) storeLeaf(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	if value == nil {
		return nil, nil
	}
	return storeDictNodeTraced(keyPfx, value, keyOffset, d.trace)
}

func (d *Dictionary) storeSetLeaf(keyPfx *Slice, value dictSetValue, keyOffset uint) (*Cell, error) {
	if value.cell == nil {
		return d.storeLeaf(keyPfx, value.builder, keyOffset)
	}

	var builder Builder
	value.cell.ToBuilderInto(&builder)
	return d.storeLeaf(keyPfx, &builder, keyOffset)
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
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if key == nil || key.BitsSize() != d.keySz {
		return false, fmt.Errorf("invalid key size")
	}

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.setValueWithModeSlice(&keySlice, dictSetValue{cell: value}, mode)
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

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

// SetBuilderBySliceKeyWithMode stores a value using the first key-size
// remaining bits of key without materializing a key cell.
func (d *Dictionary) SetBuilderBySliceKeyWithMode(key *Slice, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if value == nil {
		return false, fmt.Errorf("value builder is nil")
	}
	keySlice, err := fixedDictKeySlice(key, d.keySz)
	if err != nil {
		return false, fmt.Errorf("invalid key size")
	}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

// SetBuilderByIntKeyWithMode stores a value using a stack-local integer key
// representation, avoiding key cell finalization and hashing.
func (d *Dictionary) SetBuilderByIntKeyWithMode(key *big.Int, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if value == nil {
		return false, fmt.Errorf("value builder is nil")
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	keyCell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

func (d *Dictionary) setBuilderWithModeSlice(keySlice *Slice, value *Builder, mode DictSetMode) (bool, error) {
	return d.setValueWithModeSlice(keySlice, dictSetValue{builder: value}, mode)
}

func (d *Dictionary) setValueWithModeSlice(keySlice *Slice, value dictSetValue, mode DictSetMode) (bool, error) {
	newRoot, _, changed, err := d.set(d.tracedRoot(), keySlice, d.keySz, value, mode)
	if err != nil {
		return false, fmt.Errorf("failed to set value in dict, err: %w", err)
	}
	if changed {
		d.setRoot(newRoot)
	}
	return changed, nil
}

func (d *Dictionary) set(branch *Cell, pfx *Slice, keyOffset uint, value dictSetValue, mode DictSetMode) (*Cell, *Slice, bool, error) {
	if branch == nil {
		if mode == DictSetModeReplace {
			return nil, nil, false, nil
		}
		leaf, err := d.storeSetLeaf(pfx, value, keyOffset)
		return leaf, nil, err == nil, err
	}

	node, err := parseFixedDictNodeWithTrace(branch, keyOffset, branch.Trace())
	if err != nil {
		return nil, nil, false, err
	}
	if err = node.resolveIfSpecial(keyOffset, branch.Trace(), nil); err != nil {
		return nil, nil, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, nil, false, err
	}
	if err = node.validateForkShape(keyOffset, false); err != nil {
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
			leaf, err := d.storeSetLeaf(&nodeLabel, value, keyOffset)
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

		canonical, decided := node.canonicalLabelFast(keyOffset)
		if !decided {
			canonical, err = node.hasCanonicalLabel(keyOffset)
			if err != nil {
				return nil, nil, false, err
			}
		}
		if canonical {
			cloned, changed, err := node.cloneWithRef(refIdx, ref, d.trace)
			return cloned, oldValue, changed, err
		}
		cloned, changed, err := node.rebuildNonCanonicalFixedForkWithRef(refIdx, ref, keyOffset, d.trace)
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
	if err = storeDictLabel(oldChild, &labelRemainder, keyOffset-(bitsMatches+1)); err != nil {
		return nil, nil, false, fmt.Errorf("failed to store old child label: %w", err)
	}
	var oldPayload Builder
	node.loader.ToBuilderInto(&oldPayload)
	if err = oldChild.StoreBuilderUncheckedDepth(&oldPayload); err != nil {
		return nil, nil, false, fmt.Errorf("failed to store old child payload: %w", err)
	}

	newChild, err := d.storeSetLeaf(pfx, value, keyOffset-(bitsMatches+1))
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

	newBranch, err := d.storeFork(&prefixLabel, left, right, keyOffset)
	return newBranch, nil, err == nil, err
}

func (d *Dictionary) lookupDelete(branch *Cell, pfx *Slice, keyOffset uint) (*Slice, *Cell, bool, error) {
	if branch == nil {
		return nil, nil, false, nil
	}

	node, err := parseFixedDictNodeWithTrace(branch, keyOffset, branch.Trace())
	if err != nil {
		return nil, nil, false, err
	}
	if err = node.resolveIfSpecial(keyOffset, branch.Trace(), nil); err != nil {
		return nil, nil, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, nil, false, err
	}
	if err = node.validateForkShape(keyOffset, false); err != nil {
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

		// The merge reads the surviving sibling's content — a node the descent
		// never visited. Parse through the same boundary-aware path as ordinary
		// descent: a lazy CellDB placeholder must materialize before special-cell
		// classification, while a real library/pruned node keeps the VM resolver
		// and error semantics attached to the walk trace.
		otherNode, err := parseFixedDictNodeWithTrace(otherRef, nextKeyOffset, otherRef.Trace())
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour ref %d: %w", otherIdx, err)
		}
		if err = otherNode.resolveIfSpecial(nextKeyOffset, otherRef.Trace(), nil); err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour ref %d: %w", otherIdx, err)
		}
		if err = otherNode.rejectSpecial("dict"); err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour ref %d: %w", otherIdx, err)
		}
		if trace := otherNode.loader.Trace(); trace != nil {
			if err = trace.PendingError(); err != nil {
				return nil, nil, false, err
			}
		}

		otherLabel := otherNode.labelSlice()
		mergedLabel, err := node.mergedEdgeLabel(uint64(otherIdx), &otherLabel, "neighbour")
		if err != nil {
			return nil, nil, false, err
		}

		var survivorPayload Builder
		otherNode.loader.ToBuilderInto(&survivorPayload)
		merged, err := d.storeLeaf(mergedLabel, &survivorPayload, keyOffset)
		if err != nil {
			return nil, nil, false, err
		}
		return removed, merged, true, nil
	}

	canonical, decided := node.canonicalLabelFast(keyOffset)
	if !decided {
		canonical, err = node.hasCanonicalLabel(keyOffset)
		if err != nil {
			return nil, nil, false, err
		}
	}
	if canonical {
		cloned, changed, err := node.cloneWithRef(refIdx, newChild, d.trace)
		return removed, cloned, changed, err
	}
	cloned, changed, err := node.rebuildNonCanonicalFixedForkWithRef(refIdx, newChild, keyOffset, d.trace)
	return removed, cloned, changed, err
}

func (d *Dictionary) Delete(key *Cell) error {
	if d == nil {
		return nil
	}
	if key == nil || key.BitsSize() != d.keySz {
		return fmt.Errorf("incorrect key size")
	}

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return fmt.Errorf("failed to load key: %w", err)
	}
	_, err := d.deleteBySliceKey(&keySlice)
	return err
}

// DeleteBySliceKey removes the entry selected by the first key-size remaining
// bits of key without materializing a key cell. It reports whether an entry was
// removed.
func (d *Dictionary) DeleteBySliceKey(key *Slice) (bool, error) {
	if d == nil {
		return false, nil
	}
	keySlice, err := fixedDictKeySlice(key, d.keySz)
	if err != nil {
		return false, err
	}
	return d.deleteBySliceKey(&keySlice)
}

// DeleteByIntKey removes an integer key using stack-local key storage.
func (d *Dictionary) DeleteByIntKey(key *big.Int) (bool, error) {
	if d == nil {
		return false, nil
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	keyCell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	return d.deleteBySliceKey(&keySlice)
}

func (d *Dictionary) deleteBySliceKey(keySlice *Slice) (bool, error) {

	_, newRoot, changed, err := d.lookupDelete(d.tracedRoot(), keySlice, d.keySz)
	if err != nil {
		return false, err
	}
	if changed {
		d.setRoot(newRoot)
	}
	return changed, nil
}

func (d *Dictionary) DeleteIntKey(key *big.Int) error {
	_, err := d.DeleteByIntKey(key)
	return err
}

// LoadValueByIntKey is the same as LoadValue, but constructs the key cell from int.
func (d *Dictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueByIntKeyInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueByIntKeyInto is LoadValueByIntKey with caller-owned result storage.
func (d *Dictionary) LoadValueByIntKeyInto(key *big.Int, value *Slice) error {
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.findKeySliceInto(&keySlice, value, dictWalk{})
}

// LoadValueByUintKey is the same as LoadValue, but constructs the key cell from uint,
// without a big.Int allocation.
func (d *Dictionary) LoadValueByUintKey(key uint64) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueByUintKeyInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueByUintKeyInto is LoadValueByUintKey with caller-owned result storage.
func (d *Dictionary) LoadValueByUintKeyInto(key uint64, value *Slice) error {
	var builder Builder
	if err := initUintKeyBuilder(key, d.keySz, &builder); err != nil {
		panic(err)
	}
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.findKeySliceInto(&keySlice, value, dictWalk{})
}

// LoadValueByBytesKey is the same as LoadValue, but takes the key as raw
// big-endian bytes (the first key-size bits), without building and hashing
// a key cell.
func (d *Dictionary) LoadValueByBytesKey(key []byte) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueByBytesKeyInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueByBytesKeyInto is LoadValueByBytesKey with caller-owned result storage.
func (d *Dictionary) LoadValueByBytesKeyInto(key []byte, value *Slice) error {
	if uint(len(key))*8 < d.keySz {
		return fmt.Errorf("incorrect key size")
	}

	cell := Cell{data: key, bitsSz: uint16(d.keySz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.findKeySliceInto(&keySlice, value, dictWalk{})
}

// LoadValueBySliceKeyInto loads a value using the first key-size remaining
// bits of key without materializing and hashing a key cell.
func (d *Dictionary) LoadValueBySliceKeyInto(key *Slice, value *Slice) error {
	keySlice, err := fixedDictKeySlice(key, d.keySz)
	if err != nil {
		return err
	}
	return d.findKeySliceInto(&keySlice, value, dictWalk{})
}

func fixedDictKeySlice(key *Slice, bits uint) (Slice, error) {
	if key == nil || key.BitsLeft() < bits {
		return Slice{}, fmt.Errorf("incorrect key size")
	}

	keySlice := *key
	keySlice.bitEnd = keySlice.bitStart + uint16(bits)
	keySlice.refEnd = keySlice.refStart
	keySlice.trace = nil
	return keySlice, nil
}

// SetBuilderByBytesKey is the same as SetBuilder, but takes the key as raw
// big-endian bytes (the first key-size bits), without building and hashing
// a key cell.
func (d *Dictionary) SetBuilderByBytesKey(key []byte, value *Builder) error {
	_, err := d.SetBuilderByBytesKeyWithMode(key, value, DictSetModeSet)
	return err
}

// SetBuilderByBytesKeyWithMode is SetBuilderWithMode without materializing a
// key cell.
func (d *Dictionary) SetBuilderByBytesKeyWithMode(key []byte, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if uint(len(key))*8 < d.keySz {
		return false, fmt.Errorf("incorrect key size")
	}
	if value == nil {
		return false, fmt.Errorf("value builder is nil")
	}

	cell := Cell{data: key, bitsSz: uint16(d.keySz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

// SetBuilderByUintKey stores a zero-extended unsigned key without a big.Int or
// materialized key cell.
func (d *Dictionary) SetBuilderByUintKey(key uint64, value *Builder) error {
	_, err := d.SetBuilderByUintKeyWithMode(key, value, DictSetModeSet)
	return err
}

// SetBuilderByUintKeyWithMode is SetBuilderWithMode for a zero-extended
// unsigned key.
func (d *Dictionary) SetBuilderByUintKeyWithMode(key uint64, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if value == nil {
		return false, fmt.Errorf("value builder is nil")
	}

	var keyBuilder Builder
	if err := initUintKeyBuilder(key, d.keySz, &keyBuilder); err != nil {
		return false, err
	}
	keyCell := Cell{data: keyBuilder.data[:keyBuilder.usedBytes()], bitsSz: uint16(keyBuilder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
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
	_, err := d.deleteBySliceKey(&keySlice)
	return err
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
	value := new(Slice)
	if err := d.LoadValueInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueWithResolver performs one lookup with an explicit special-node
// resolver. It is used by unmetered VM library lookup, where no execution
// trace is installed and the resolver must not become dictionary state.
func (d *Dictionary) LoadValueWithResolver(key *Cell, resolver DictSpecialResolver) (*Slice, error) {
	value := new(Slice)
	if key == nil || key.BitsSize() != d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, fmt.Errorf("failed to load lookup key: %w", err)
	}
	if err := d.findKeySliceInto(&keySlice, value, dictWalk{resolver: resolver}); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueInto is LoadValue with caller-owned result storage.
func (d *Dictionary) LoadValueInto(key *Cell, value *Slice) error {
	if key == nil || key.BitsSize() != d.keySz {
		return fmt.Errorf("incorrect key size")
	}

	return d.findKeyInto(key, value)
}

func (d *Dictionary) LoadMinMax(fetchMax bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}

	key := BeginCell()
	branch := d.root
	branchTrace := CombineTraces(branch.Trace(), d.trace)
	remaining := d.keySz

	// the descent reuses a value slice, only the found value escapes
	var loader Slice
	for {
		if err := branch.BeginParseIntoWithTrace(&loader, branchTrace); err != nil {
			return nil, nil, err
		}
		if loader.cell.IsSpecial() {
			resolved, err := resolveDictNodeCell(loader.cell, branchTrace, nil, "dict")
			if err != nil {
				return nil, nil, err
			}
			branchTrace = CombineTraces(resolved.Trace(), branchTrace)
			if err = resolved.BeginParseIntoWithTrace(&loader, nil); err != nil {
				return nil, nil, err
			}
			loader.SetTrace(branchTrace)
		}

		labelLen, keyBuilder, err := loadLabel(remaining, &loader, key)
		if err != nil {
			return nil, nil, err
		}
		key = keyBuilder

		// a fork must hold nothing but its label and exactly two references;
		// an augmented tree walked as a plain dictionary keeps its extras
		if labelLen < remaining {
			if loader.BitsLeft() != 0 || loader.RefsNum() != 2 {
				return nil, nil, ErrInvalidDictForkNode
			}
		}
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
		next, nextTrace, err := loader.refAndTraceAt(refIdx)
		if err != nil {
			return nil, nil, err
		}
		branch = next
		branchTrace = nextTrace
		remaining--
	}
}

// LoadMinMaxAndDelete runs a full boundary lookup (charging every node load)
// followed by a separate delete walk over the now-loaded path (reload prices,
// then new-cell creation on the way back up), as the reference does. Keeping
// the two walks distinct keeps the point at which an exhausted gas limit
// interrupts the operation — and so the reported gas usage — identical.
func (d *Dictionary) LoadMinMaxAndDelete(fetchMax bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}

	key, value, err := d.LoadMinMax(fetchMax, invertFirst)
	if err != nil {
		return nil, nil, err
	}

	var keySlice Slice
	if err = key.BeginParseInto(&keySlice); err != nil {
		return nil, nil, fmt.Errorf("failed to load boundary key: %w", err)
	}
	_, newRoot, changed, err := d.lookupDelete(d.tracedRoot(), &keySlice, d.keySz)
	if err != nil {
		return nil, nil, err
	}
	if !changed {
		return nil, nil, ErrNoSuchKeyInDict
	}
	d.setRoot(newRoot)
	return key, value, nil
}

func (d *Dictionary) LoadValueAndSet(key, value *Cell) (*Slice, bool, error) {
	return d.LoadValueAndSetWithMode(key, value, DictSetModeSet)
}

func (d *Dictionary) LoadValueAndSetWithMode(key, value *Cell, mode DictSetMode) (*Slice, bool, error) {
	if value == nil {
		return nil, false, fmt.Errorf("value is nil")
	}
	if d == nil {
		return nil, false, fmt.Errorf("dict is nil")
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, false, fmt.Errorf("incorrect key size")
	}

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.loadValueAndSetBySliceKey(&keySlice, dictSetValue{cell: value}, mode)
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

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.loadValueAndSetBuilderBySliceKey(&keySlice, value, mode)
}

// LoadValueAndSetBuilderBySliceKeyWithMode replaces a value using a slice key
// without materializing a key cell and returns the previous value when present.
func (d *Dictionary) LoadValueAndSetBuilderBySliceKeyWithMode(key *Slice, value *Builder, mode DictSetMode) (*Slice, bool, error) {
	if d == nil {
		return nil, false, fmt.Errorf("dict is nil")
	}
	if value == nil {
		return nil, false, fmt.Errorf("value builder is nil")
	}
	keySlice, err := fixedDictKeySlice(key, d.keySz)
	if err != nil {
		return nil, false, err
	}
	return d.loadValueAndSetBuilderBySliceKey(&keySlice, value, mode)
}

// LoadValueAndSetBuilderByIntKeyWithMode is the integer-key variant of
// LoadValueAndSetBuilderBySliceKeyWithMode.
func (d *Dictionary) LoadValueAndSetBuilderByIntKeyWithMode(key *big.Int, value *Builder, mode DictSetMode) (*Slice, bool, error) {
	if d == nil {
		return nil, false, fmt.Errorf("dict is nil")
	}
	if value == nil {
		return nil, false, fmt.Errorf("value builder is nil")
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	keyCell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	return d.loadValueAndSetBuilderBySliceKey(&keySlice, value, mode)
}

func (d *Dictionary) loadValueAndSetBuilderBySliceKey(keySlice *Slice, value *Builder, mode DictSetMode) (*Slice, bool, error) {
	return d.loadValueAndSetBySliceKey(keySlice, dictSetValue{builder: value}, mode)
}

func (d *Dictionary) loadValueAndSetBySliceKey(keySlice *Slice, value dictSetValue, mode DictSetMode) (*Slice, bool, error) {
	newRoot, oldValue, changed, err := d.set(d.tracedRoot(), keySlice, d.keySz, value, mode)
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

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}
	return d.loadValueAndDeleteBySliceKey(&keySlice)
}

// LoadValueAndDeleteBySliceKey removes and returns a value using the first
// key-size remaining bits of key without materializing a key cell.
func (d *Dictionary) LoadValueAndDeleteBySliceKey(key *Slice) (*Slice, error) {
	if d == nil {
		return nil, ErrNoSuchKeyInDict
	}
	keySlice, err := fixedDictKeySlice(key, d.keySz)
	if err != nil {
		return nil, err
	}
	return d.loadValueAndDeleteBySliceKey(&keySlice)
}

// LoadValueAndDeleteBySliceKeyInto removes a slice-backed key and writes the
// removed value into caller-owned storage without materializing a key cell.
func (d *Dictionary) LoadValueAndDeleteBySliceKeyInto(key, value *Slice) error {
	if d == nil {
		return ErrNoSuchKeyInDict
	}
	keySlice, err := fixedDictKeySlice(key, d.keySz)
	if err != nil {
		return err
	}
	return d.loadValueAndDeleteBySliceKeyInto(&keySlice, value)
}

// LoadValueAndDeleteByBytesKeyInto is the byte-backed form of
// LoadValueAndDeleteBySliceKeyInto.
func (d *Dictionary) LoadValueAndDeleteByBytesKeyInto(key []byte, value *Slice) error {
	if d == nil {
		return ErrNoSuchKeyInDict
	}
	var keyCell Cell
	var keySlice Slice
	if err := initFixedDictBytesKeySlice(key, d.keySz, &keyCell, &keySlice); err != nil {
		return err
	}
	return d.loadValueAndDeleteBySliceKeyInto(&keySlice, value)
}

// LoadValueAndDeleteByUintKeyInto removes a zero-extended unsigned key without
// a big.Int or materialized key cell.
func (d *Dictionary) LoadValueAndDeleteByUintKeyInto(key uint64, value *Slice) error {
	if d == nil {
		return ErrNoSuchKeyInDict
	}
	var keyBuilder Builder
	if err := initUintKeyBuilder(key, d.keySz, &keyBuilder); err != nil {
		return err
	}
	keyCell := Cell{data: keyBuilder.data[:keyBuilder.usedBytes()], bitsSz: uint16(keyBuilder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	return d.loadValueAndDeleteBySliceKeyInto(&keySlice, value)
}

// LoadValueAndDeleteByIntKey is the integer-key variant of
// LoadValueAndDeleteBySliceKey.
func (d *Dictionary) LoadValueAndDeleteByIntKey(key *big.Int) (*Slice, error) {
	if d == nil {
		return nil, ErrNoSuchKeyInDict
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	keyCell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &keyCell, bitEnd: keyCell.bitsSz}
	return d.loadValueAndDeleteBySliceKey(&keySlice)
}

func (d *Dictionary) loadValueAndDeleteBySliceKey(keySlice *Slice) (*Slice, error) {

	removed, newRoot, changed, err := d.lookupDelete(d.tracedRoot(), keySlice, d.keySz)
	if err != nil {
		return nil, err
	}
	if !changed || removed == nil || sameDictRoot(d.root, newRoot) {
		return nil, ErrNoSuchKeyInDict
	}

	d.setRoot(newRoot)
	return removed, nil
}

func (d *Dictionary) loadValueAndDeleteBySliceKeyInto(keySlice, value *Slice) error {
	removed, err := d.loadValueAndDeleteBySliceKey(keySlice)
	if err != nil {
		return err
	}
	*value = *removed
	return nil
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
	return d.mapInner(nil, d.keySz, d.keySz, d.tracedRoot(), BeginCell(), len(skipPruned) > 0 && skipPruned[0])
}

func (d *Dictionary) mapInner(out []DictKV, keySz, leftKeySz uint, c *Cell, keyPrefix *Builder, skipPruned bool) ([]DictKV, error) {
	var err error
	var sz uint

	if c.IsSpecial() && (!c.IsLazy() || skipPruned) {
		if skipPruned && c.GetType() == PrunedCellType {
			// ignore pruned keys
			return out, nil
		}
		if c.Trace().dictSpecialResolver() == nil {
			return nil, fmt.Errorf("dict %w, cannot load some values", ErrDictHasSpecialCells)
		}
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
		resolved, err := resolveDictNodeCell(loader.cell, loader.Trace(), nil, "dict")
		if err != nil {
			return nil, err
		}
		trace := CombineTraces(resolved.Trace(), loader.Trace())
		if err = resolved.BeginParseIntoWithTrace(&loader, nil); err != nil {
			return nil, err
		}
		loader.SetTrace(trace)
	}

	sz, keyPrefix, err = loadLabel(leftKeySz, &loader, keyPrefix)
	if err != nil {
		return nil, err
	}
	// LoadAll is also the raw HashmapAug walker used by block parsers: a fork
	// may carry augmentation bits after its label, while its first two refs are
	// still the dictionary children.

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

func (d *Dictionary) findKeyInto(lookupKey *Cell, value *Slice) error {
	var lKey Slice
	if err := lookupKey.BeginParseInto(&lKey); err != nil {
		return fmt.Errorf("failed to load lookup key: %w", err)
	}
	return d.findKeySliceInto(&lKey, value, dictWalk{})
}

func (d *Dictionary) findKeySliceInto(lKey *Slice, value *Slice, walk dictWalk) error {
	branch := d.root
	if branch == nil {
		return ErrNoSuchKeyInDict
	}
	branchTrace := CombineTraces(branch.Trace(), d.trace)

	// The descent and result both use caller-owned Slice values.
	var branchSlice Slice
	for {
		if err := branch.BeginParseIntoWithTrace(&branchSlice, branchTrace); err != nil {
			return err
		}
		if branchSlice.cell.IsSpecial() {
			resolved, err := resolveDictNodeCell(branchSlice.cell, branchTrace, walk.resolver, "dict")
			if err != nil {
				return err
			}
			// the resolver already charged the resolved cell: parse it
			// charge-free, children stay metered through the walk trace
			branchTrace = CombineTraces(resolved.Trace(), branchTrace)
			if err = resolved.BeginParseIntoWithTrace(&branchSlice, nil); err != nil {
				return err
			}
			branchSlice.SetTrace(branchTrace)
		}
		remaining := lKey.BitsLeft()
		sz, matched, err := matchLabelPrefix(remaining, &branchSlice, lKey)
		if err != nil {
			return err
		}
		if matched != sz {
			// The matcher stops at the first differing bit. Re-read only this
			// uncommon path to validate the complete node before reporting a
			// missing key; successful lookups keep the old tight descent.
			if sz < remaining {
				check := Slice{
					cell:   branchSlice.cell,
					bitEnd: branchSlice.cell.bitsSz,
					refEnd: uint8(branchSlice.cell.refsCount()),
				}
				if _, _, err = readLabelView(remaining, &check); err != nil {
					return err
				}
				refsLeft := check.refEnd - check.refStart
				if check.bitStart != check.bitEnd || refsLeft != 2 {
					if !walk.lenient || refsLeft < 2 {
						return ErrInvalidDictForkNode
					}
				}
			}
			return ErrNoSuchKeyInDict
		}

		if remaining == sz {
			*value = branchSlice
			return nil
		}

		// A matched non-leaf is necessarily a fork. Validate it without an
		// extra label-length branch on the successful lookup path.
		refsLeft := branchSlice.refEnd - branchSlice.refStart
		if branchSlice.bitStart != branchSlice.bitEnd || refsLeft != 2 {
			if !walk.lenient || refsLeft < 2 {
				return ErrInvalidDictForkNode
			}
		}

		idx, err := lKey.LoadUInt(1)
		if err != nil {
			return err
		}
		next, nextTrace, err := branchSlice.refAndTraceAt(int(idx))
		if err != nil {
			return err
		}
		branch = next
		branchTrace = nextTrace
	}
}

func (d *Dictionary) AsCell() *Cell {
	return d.tracedRoot()
}

func (d *Dictionary) ToCell() (*Cell, error) {
	if d == nil {
		return nil, nil
	}
	return d.tracedRoot(), nil
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
