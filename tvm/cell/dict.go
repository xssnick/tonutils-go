package cell

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

type Dictionary struct {
	keySz uint

	root         *Cell
	observer     Observer
	traceNode    TraceNode
	skipRootLoad bool
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
	root, err := c.ToCell()
	if err != nil {
		return nil, err
	}

	if err = validatePlainDictRoot(root, keySz); err != nil {
		return nil, fmt.Errorf("failed to validate dict: %w", err)
	}

	return (&Dictionary{
		keySz: keySz,
		root:  root,
	}).SetObserverNode(c.observer, c.traceNode), nil
}

func (c *Slice) MustLoadDict(keySz uint) *Dictionary {
	ld, err := c.LoadDict(keySz)
	if err != nil {
		panic(err)
	}
	return ld
}

func (c *Slice) LoadDict(keySz uint) (*Dictionary, error) {
	cl, err := c.LoadMaybeRef()
	if err != nil {
		return nil, fmt.Errorf("failed to load ref for dict, err: %w", err)
	}

	if cl == nil {
		return (&Dictionary{
			keySz: keySz,
		}).SetObserverNode(c.observer, c.traceNode), nil
	}

	return cl.ToDict(keySz)
}

func (d *Dictionary) GetKeySize() uint {
	return d.keySz
}

func (d *Dictionary) Copy() *Dictionary {
	return &Dictionary{
		keySz:        d.keySz,
		root:         d.root,
		observer:     d.observer,
		traceNode:    d.traceNode,
		skipRootLoad: d.skipRootLoad,
	}
}

func (d *Dictionary) SetObserver(observer Observer) *Dictionary {
	return d.SetObserverNode(observer, 0)
}

func (d *Dictionary) SetObserverNode(observer Observer, node TraceNode) *Dictionary {
	d.observer = observer
	d.traceNode = node
	d.skipRootLoad = observer != nil && node != 0 && d.root != nil
	return d
}

func (d *Dictionary) beginParse(c *Cell) *Slice {
	if d != nil {
		return d.beginParseNode(c, 0)
	}
	return c.BeginParse()
}

func beginParseObservedNode(c *Cell, observer Observer, node TraceNode, charge bool) *Slice {
	if observer != nil {
		if charge {
			notifyCellLoad(observer, c)
		}
		return c.BeginParse().SetObserverNode(observer, node)
	}
	return c.BeginParse()
}

func (d *Dictionary) beginParseNode(c *Cell, node TraceNode) *Slice {
	if d == nil {
		return c.BeginParse()
	}

	charge := true
	if d.skipRootLoad {
		d.skipRootLoad = false
		charge = false
	}

	return beginParseObservedNode(c, d.observer, node, charge)
}

func (d *Dictionary) SetIntKey(key *big.Int, value *Cell) error {
	return d.Set(BeginCell().MustStoreBigInt(key, d.keySz).EndCell(), value)
}

func (d *Dictionary) storeLeaf(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	if value == nil {
		return nil, nil
	}
	return storeDictNodeObserved(keyPfx, value, keyOffset, d.observer)
}

func (d *Dictionary) storeFork(label *Slice, left, right *Cell, keyOffset uint) (*Cell, error) {
	b := BeginCell().SetObserver(d.observer)
	if err := b.StoreRef(left); err != nil {
		return nil, fmt.Errorf("failed to store left branch: %w", err)
	}

	if err := b.StoreRef(right); err != nil {
		return nil, fmt.Errorf("failed to store right branch: %w", err)
	}

	return storeDictNodeObserved(label, b, keyOffset, d.observer)
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

	newRoot, changed, err := d.set(d.root, key.BeginParse(), d.keySz, value, mode)
	if err != nil {
		return false, fmt.Errorf("failed to set value in dict, err: %w", err)
	}
	if changed {
		d.root = newRoot
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

	s := d.beginParse(branch)
	sz, kPart, err := loadLabel(keyOffset, s, BeginCell())
	if err != nil {
		return nil, false, fmt.Errorf("failed to load label: %w", err)
	}

	isNewRight, matches := false, true
	kPartSlice := kPart.ToSlice()
	var bitsMatches uint
	for bitsMatches = 0; bitsMatches < sz; bitsMatches++ {
		vCurr, err := kPartSlice.LoadUInt(1)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load current key bit: %w", err)
		}

		vNew, err := pfx.LoadUInt(1)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load new key bit: %w", err)
		}

		if vCurr != vNew {
			isNewRight = vNew != 0
			matches = false
			break
		}
	}

	if matches {
		if pfx.BitsLeft() == 0 {
			if mode == DictSetModeAdd {
				return branch, false, nil
			}
			leaf, err := d.storeLeaf(kPart.ToSlice(), value, keyOffset)
			return leaf, err == nil, err
		}

		refIdx := int(pfx.MustLoadUInt(1))
		ref, err := branch.PeekRef(refIdx)
		if err != nil {
			return nil, false, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
		}

		ref, changed, err := d.set(ref, pfx, keyOffset-(bitsMatches+1), value, mode)
		if err != nil {
			return nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
		}
		if !changed {
			return branch, false, nil
		}
		if ref == nil {
			return nil, false, fmt.Errorf("set produced nil child")
		}

		return branch.cloneWithRefObserved(refIdx, ref, d.observer), true, nil
	}

	if mode == DictSetModeReplace {
		return branch, false, nil
	}

	prefixBits := kPart.ToSlice().MustLoadSlice(bitsMatches)
	prefixLabel := BeginCell().MustStoreSlice(prefixBits, bitsMatches).ToSlice()

	oldChild := BeginCell()
	if err = storeDictLabel(oldChild, kPartSlice, keyOffset-(bitsMatches+1)); err != nil {
		return nil, false, fmt.Errorf("failed to store old child label: %w", err)
	}
	if err = oldChild.StoreBuilder(s.ToBuilder()); err != nil {
		return nil, false, fmt.Errorf("failed to store old child payload: %w", err)
	}

	newChild, err := d.storeLeaf(pfx, value, keyOffset-(bitsMatches+1))
	if err != nil {
		return nil, false, fmt.Errorf("failed to store new child leaf: %w", err)
	}

	left, right := newChild, oldChild.EndCell()
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

	s := d.beginParse(branch)
	sz, kPart, err := loadLabel(keyOffset, s, BeginCell())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load label: %w", err)
	}

	bitsMatches, err := consumeCommonPrefix(kPart.ToSlice(), pfx, sz)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}
	if bitsMatches < sz {
		return nil, nil, false, nil
	}

	if pfx.BitsLeft() == 0 {
		return s, nil, true, nil
	}

	refIdx := int(pfx.MustLoadUInt(1))
	ref, err := branch.PeekRef(refIdx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
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
		otherRef, err := branch.PeekRef(otherIdx)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to peek neighbour ref %d: %w", otherIdx, err)
		}

		slc := d.beginParse(otherRef)
		_, otherLabel, err := loadLabel(nextKeyOffset, slc, BeginCell())
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load neighbour label: %w", err)
		}

		if err = kPart.StoreUInt(uint64(otherIdx), 1); err != nil {
			return nil, nil, false, fmt.Errorf("failed to append neighbour edge bit: %w", err)
		}
		if err = kPart.StoreBuilder(otherLabel); err != nil {
			return nil, nil, false, fmt.Errorf("failed to append neighbour label: %w", err)
		}

		merged, err := d.storeLeaf(kPart.ToSlice(), slc.ToBuilder(), keyOffset)
		if err != nil {
			return nil, nil, false, err
		}
		return removed, merged, true, nil
	}

	return removed, branch.cloneWithRefObserved(refIdx, newChild, d.observer), true, nil
}

func (d *Dictionary) Delete(key *Cell) error {
	if d == nil {
		return nil
	}
	if key == nil || key.BitsSize() != d.keySz {
		return fmt.Errorf("incorrect key size")
	}

	_, newRoot, changed, err := d.lookupDelete(d.root, key.BeginParse(), d.keySz)
	if err != nil {
		return err
	}
	if changed {
		d.root = newRoot
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

	skipInitialLoad := d.skipRootLoad
	d.skipRootLoad = false
	return findKeyInDict(d.root, key, d.observer, d.traceNode, skipInitialLoad)
}

func (d *Dictionary) LoadMinMax(fetchMax bool, invertFirst bool) (*Cell, *Slice, error) {
	if d == nil || d.root == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}

	key := BeginCell().SetObserver(d.observer)
	branch := d.root
	node := d.traceNode
	remaining := d.keySz

	for {
		if branch.IsSpecial() {
			return nil, nil, fmt.Errorf("dict has special cells in tree structure")
		}

		loader := d.beginParseNode(branch, node)
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

		if err = key.StoreBoolBit(bit); err != nil {
			return nil, nil, err
		}

		refIdx := 0
		if bit {
			refIdx = 1
		}
		if d.observer != nil {
			node = d.observer.OnRef(node, refIdx)
		}
		branch, err = branch.PeekRef(refIdx)
		if err != nil {
			return nil, nil, err
		}
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

	removed, newRoot, changed, err := d.lookupDelete(d.root, key.BeginParse(), d.keySz)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrNoSuchKeyInDict
	}

	d.root = newRoot
	return removed, nil
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
	return d.mapInner(d.keySz, d.keySz, d.root, BeginCell(), len(skipPruned) > 0 && skipPruned[0])
}

func (d *Dictionary) mapInner(keySz, leftKeySz uint, c *Cell, keyPrefix *Builder, skipPruned bool) ([]DictKV, error) {
	var err error
	var sz uint

	if c.IsSpecial() {
		if skipPruned && c.GetType() == PrunedCellType {
			// ignore pruned keys
			return []DictKV{}, nil
		}
		return nil, fmt.Errorf("dict has special cells in tree structure, cannot load some values")
	}

	loader := d.beginParse(c)

	sz, keyPrefix, err = loadLabel(leftKeySz, loader, keyPrefix)
	if err != nil {
		return nil, err
	}

	// until key size is not equals we go deeper
	if keyPrefix.BitsUsed() < keySz {
		// 0 bit branch
		left, err := loader.LoadRefCell()
		if err != nil {
			return nil, err
		}

		keysL, err := d.mapInner(keySz, leftKeySz-(1+sz), left, keyPrefix.Copy().MustStoreUInt(0, 1), skipPruned)
		if err != nil {
			return nil, err
		}

		// 1 bit branch
		right, err := loader.LoadRefCell()
		if err != nil {
			return nil, err
		}
		keysR, err := d.mapInner(keySz, leftKeySz-(1+sz), right, keyPrefix.Copy().MustStoreUInt(1, 1), skipPruned)
		if err != nil {
			return nil, err
		}

		return append(keysL, keysR...), nil
	}

	return []DictKV{{
		Key:   keyPrefix.ToSlice(),
		Value: loader,
	}}, nil
}

func findKeyInDict(branch *Cell, lookupKey *Cell, observer Observer, observerNode TraceNode, skipInitialLoad bool) (*Slice, error) {
	if branch == nil {
		// empty dict
		return nil, ErrNoSuchKeyInDict
	}

	lKey := lookupKey.BeginParse()

	// until key size is not equals we go deeper
	for {
		branchSlice := beginParseObservedNode(branch, observer, observerNode, !skipInitialLoad)
		skipInitialLoad = false
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

		refIdx := int(idx)
		if observer != nil {
			observerNode = observer.OnRef(observerNode, refIdx)
		}

		branch, err = branch.PeekRef(int(idx))
		if err != nil {
			return nil, err
		}
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
