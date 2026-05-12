package cell

import (
	"errors"
	"fmt"
	"math/big"
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
	return d.Set(BeginCell().MustStoreBigInt(key, d.keySz).EndCell(), value)
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

	newRoot, changed, err := d.set(d.root, key.MustBeginParse(), d.keySz, value, mode)
	if err != nil {
		return false, fmt.Errorf("failed to set value in dict, err: %w", err)
	}
	if changed {
		d.setRoot(newRoot)
	}
	return changed, nil
}

func (d *Dictionary) set(branch *Cell, pfx *Slice, keyOffset uint, value *Builder, mode DictSetMode) (*Cell, bool, error) {
	if branch == nil {
		if mode == DictSetModeReplace {
			return nil, false, nil
		}
		leaf, err := d.storeLeaf(pfx, value, keyOffset)
		return leaf, err == nil, err
	}

	node, err := parseFixedDictNode(branch, keyOffset)
	if err != nil {
		return nil, false, err
	}
	if err = node.rejectSpecial("dict"); err != nil {
		return nil, false, err
	}

	bitsMatches, isNewRight, _, err := matchBuilderLabel(node.label, node.labelLen, pfx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}

	if bitsMatches == node.labelLen {
		if pfx.BitsLeft() == 0 {
			if mode == DictSetModeAdd {
				return node.cell, false, nil
			}
			leaf, err := d.storeLeaf(node.label.ToSlice(), value, keyOffset)
			return leaf, err == nil, err
		}

		refIdx := int(pfx.MustLoadUInt(1))
		ref, err := node.ref(refIdx)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load %d ref: %w", refIdx, err)
		}

		ref, changed, err := d.set(ref, pfx, keyOffset-(bitsMatches+1), value, mode)
		if err != nil {
			return nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
		}
		if !changed {
			return node.cell, false, nil
		}

		return node.cloneWithRef(refIdx, ref, d.trace)
	}

	if mode == DictSetModeReplace {
		return node.cell, false, nil
	}

	prefixLabel, labelRemainder, err := node.splitLabel(bitsMatches)
	if err != nil {
		return nil, false, fmt.Errorf("failed to split old child label: %w", err)
	}

	oldChild := BeginCell().SetTrace(d.trace)
	if err = storeDictLabel(oldChild, labelRemainder, keyOffset-(bitsMatches+1)); err != nil {
		return nil, false, fmt.Errorf("failed to store old child label: %w", err)
	}
	if err = oldChild.StoreBuilderUncheckedDepth(node.loader.ToBuilder()); err != nil {
		return nil, false, fmt.Errorf("failed to store old child payload: %w", err)
	}

	newChild, err := d.storeLeaf(pfx, value, keyOffset-(bitsMatches+1))
	if err != nil {
		return nil, false, fmt.Errorf("failed to store new child leaf: %w", err)
	}

	oldChildCell, err := oldChild.EndCellSpecial(false)
	if err != nil {
		return nil, false, err
	}

	left, right := newChild, oldChildCell
	if isNewRight {
		left, right = right, left
	}

	newBranch, err := d.storeFork(prefixLabel, left, right, keyOffset)
	return newBranch, err == nil, err
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

	bitsMatches, err := consumeCommonPrefix(node.label.ToSlice(), pfx, node.labelLen)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}
	if bitsMatches < node.labelLen {
		return nil, nil, false, nil
	}

	if pfx.BitsLeft() == 0 {
		return node.loader, nil, true, nil
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

		slc := otherRef.MustBeginParse()
		_, otherLabel, err := loadLabel(nextKeyOffset, slc, BeginCell())
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour label: %w", err)
		}

		if err = node.appendEdgeLabel(uint64(otherIdx), otherLabel, "neighbour"); err != nil {
			return nil, nil, false, err
		}

		merged, err := d.storeLeaf(node.label.ToSlice(), slc.ToBuilder(), keyOffset)
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

	_, newRoot, changed, err := d.lookupDelete(d.root, key.MustBeginParse(), d.keySz)
	if err != nil {
		return err
	}
	if changed {
		d.setRoot(newRoot)
	}
	return nil
}

func (d *Dictionary) DeleteIntKey(key *big.Int) error {
	return d.Delete(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

// LoadValueByIntKey is the same as LoadValue, but constructs the key cell from int.
func (d *Dictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	return d.LoadValue(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
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

	for {
		loader, err := branch.BeginParse()
		if err != nil {
			return nil, nil, err
		}
		if loader.cell.IsSpecial() {
			return nil, nil, fmt.Errorf("dict has special cells in tree structure")
		}

		labelLen, keyBuilder, err := loadLabel(remaining, loader, key)
		if err != nil {
			return nil, nil, err
		}
		key = keyBuilder
		remaining -= labelLen

		if remaining == 0 {
			return key.EndCell(), loader, nil
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
	key, _, err := d.LoadMinMax(fetchMax, invertFirst)
	if err != nil {
		return nil, nil, err
	}

	value, err := d.LoadValueAndDelete(key)
	if err != nil {
		return nil, nil, err
	}
	return key, value, nil
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

	oldValue, err := d.LoadValue(key)
	if err != nil {
		if !errors.Is(err, ErrNoSuchKeyInDict) {
			return nil, false, err
		}
		oldValue = nil
	}

	changed, err := d.SetBuilderWithMode(key, value, mode)
	if err != nil {
		return nil, false, err
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

	removed, newRoot, changed, err := d.lookupDelete(d.root, key.MustBeginParse(), d.keySz)
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
		return nil, fmt.Errorf("dict has special cells in tree structure, cannot load some values")
	}

	loader, err := c.BeginParse()
	if err != nil {
		return nil, err
	}
	if loader.cell.IsSpecial() {
		if skipPruned && loader.cell.GetType() == PrunedCellType {
			return out, nil
		}
		return nil, fmt.Errorf("dict has special cells in tree structure, cannot load some values")
	}

	sz, keyPrefix, err = loadLabel(leftKeySz, loader, keyPrefix)
	if err != nil {
		return nil, err
	}

	// until key size is not equals we go deeper
	if keyPrefix.BitsUsed() < keySz {
		// 0 bit branch
		left, err := loadDictMapRef(loader, skipPruned)
		if err != nil {
			return nil, err
		}

		out, err = d.mapInner(out, keySz, leftKeySz-(1+sz), left, keyPrefix.Copy().MustStoreUInt(0, 1), skipPruned)
		if err != nil {
			return nil, err
		}

		// 1 bit branch
		right, err := loadDictMapRef(loader, skipPruned)
		if err != nil {
			return nil, err
		}
		return d.mapInner(out, keySz, leftKeySz-(1+sz), right, keyPrefix.Copy().MustStoreUInt(1, 1), skipPruned)
	}

	return append(out, DictKV{
		Key:   keyPrefix.ToSlice(),
		Value: loader,
	}), nil
}

func loadDictMapRef(loader *Slice, skipPruned bool) (*Cell, error) {
	if skipPruned {
		return loader.loadBoundaryRefCell()
	}
	return loader.LoadRefCell()
}

func (d *Dictionary) findKey(lookupKey *Cell) (*Slice, error) {
	branch := d.root
	if branch == nil {
		// empty dict
		return nil, ErrNoSuchKeyInDict
	}

	lKey := lookupKey.MustBeginParse()

	// until key size is not equals we go deeper
	for {
		branchSlice, err := branch.BeginParse()
		if err != nil {
			return nil, err
		}
		if branchSlice.cell.IsSpecial() {
			return nil, fmt.Errorf("dict has special cells in tree structure")
		}
		sz, matched, err := matchLabelPrefix(lKey.BitsLeft(), branchSlice, lKey)
		if err != nil {
			return nil, err
		}

		if matched != sz {
			return nil, ErrNoSuchKeyInDict
		}

		if lKey.BitsLeft() == 0 {
			return branchSlice, nil
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

	var list []string
	for _, dictKV := range kv {
		list = append(list, fmt.Sprintf("Key %s: Value %d bits, %d refs", dictKV.Key.String(), dictKV.Value.BitsLeft(), dictKV.Value.RefsNum()))
	}

	if len(list) == 0 {
		return "{}"
	}

	return "{\n\t" + strings.Join(list, "\n\t") + "\n}"
}
