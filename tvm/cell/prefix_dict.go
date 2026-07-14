package cell

import (
	"fmt"
	"math/big"
)

type PrefixDictionary struct {
	keySz uint

	root *Cell

	trace *Trace
}

func NewPrefixDict(keySz uint) *PrefixDictionary {
	return &PrefixDictionary{
		keySz: keySz,
	}
}

func (c *Cell) AsPrefixDict(keySz uint) *PrefixDictionary {
	return &PrefixDictionary{
		keySz: keySz,
		root:  c,
	}
}

func (c *Slice) ToPrefixDict(keySz uint) (*PrefixDictionary, error) {
	root, err := c.WithoutTrace().ToCell()
	if err != nil {
		return nil, err
	}
	if err = validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate prefix dict: %w", err)
	}
	dict := &PrefixDictionary{
		keySz: keySz,
		root:  root,
	}
	return dict.SetTrace(c.trace), nil
}

func (c *Slice) MustToPrefixDict(keySz uint) *PrefixDictionary {
	dict, err := c.ToPrefixDict(keySz)
	if err != nil {
		panic(err)
	}
	return dict
}

func (c *Slice) LoadPrefixDict(keySz uint) (*PrefixDictionary, error) {
	root, has, err := c.loadMaybeRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load ref for prefix dict, err: %w", err)
	}

	if !has {
		return (&PrefixDictionary{
			keySz: keySz,
		}).SetTrace(c.trace), nil
	}

	if err = validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate prefix dict: %w", err)
	}

	return (&PrefixDictionary{
		keySz: keySz,
		root:  root,
	}).SetTrace(root.Trace()), nil
}

func (c *Slice) MustLoadPrefixDict(keySz uint) *PrefixDictionary {
	dict, err := c.LoadPrefixDict(keySz)
	if err != nil {
		panic(err)
	}
	return dict
}

func (d *PrefixDictionary) GetKeySize() uint {
	return d.keySz
}

func (d *PrefixDictionary) Copy() *PrefixDictionary {
	if d == nil {
		return nil
	}

	return &PrefixDictionary{
		keySz: d.keySz,
		root:  d.root,
		trace: d.trace,
	}
}

func (d *PrefixDictionary) SetTrace(trace *Trace) *PrefixDictionary {
	if d == nil {
		return nil
	}
	d.trace = trace
	d.root = d.root.withTraceCombined(trace)
	return d
}

func (d *PrefixDictionary) setRoot(root *Cell) {
	d.root = root
}

func (d *PrefixDictionary) IsEmpty() bool {
	return d == nil || d.root == nil
}

func (d *PrefixDictionary) LookupPrefix(key *Cell) (*Slice, uint, error) {
	if d == nil {
		return nil, 0, nil
	}
	if key == nil {
		return nil, 0, fmt.Errorf("key is nil")
	}
	if d.root == nil {
		return nil, 0, nil
	}

	branch := d.root
	remaining := d.keySz
	matched := uint(0)

	// the descent reuses value slices, only the found value escapes
	var keySlice, branchSlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, 0, fmt.Errorf("failed to load key: %w", err)
	}

	for {
		if err := branch.BeginParseInto(&branchSlice); err != nil {
			return nil, matched, fmt.Errorf("failed to load prefix dict branch: %w", err)
		}
		if branchSlice.cell.IsSpecial() {
			return nil, matched, fmt.Errorf("prefix dict %w", ErrDictHasSpecialCells)
		}

		labelLen, commonPrefix, err := matchLabelPrefix(remaining, &branchSlice, &keySlice)
		if err != nil {
			return nil, matched, err
		}

		if commonPrefix < labelLen {
			return nil, matched + commonPrefix, nil
		}

		matched += labelLen
		remaining -= labelLen

		isFork, err := branchSlice.LoadBoolBit()
		if err != nil {
			return nil, matched, fmt.Errorf("no node constructor in a prefix code dictionary")
		}

		if !isFork {
			value := branchSlice
			return &value, matched, nil
		}

		if remaining == 0 {
			return nil, matched, fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
		}
		if branchSlice.BitsLeft() != 0 || branchSlice.RefsNum() != 2 {
			return nil, matched, fmt.Errorf("invalid fork node in a prefix code dictionary")
		}
		if keySlice.BitsLeft() == 0 {
			return nil, matched, nil
		}

		idx, err := keySlice.LoadUInt(1)
		if err != nil {
			return nil, matched, err
		}

		matched++
		remaining--

		next, err := branchSlice.peekRefCellAt(int(idx))
		if err != nil {
			return nil, matched, err
		}
		branch = next
	}
}

func (d *PrefixDictionary) LoadValue(key *Cell) (*Slice, error) {
	if key == nil {
		return nil, fmt.Errorf("key is nil")
	}

	value, matched, err := d.LookupPrefix(key)
	if err != nil {
		return nil, err
	}
	if matched != key.BitsSize() || value == nil {
		return nil, ErrNoSuchKeyInDict
	}
	return value, nil
}

// LoadValueByIntKey loads a full-width prefix-dictionary key without
// finalizing and hashing an intermediate key cell.
func (d *PrefixDictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	var builder Builder
	var cell Cell
	initIntKeyCell(key, d.keySz, &builder, &cell)
	return d.LoadValue(&cell)
}

func (d *PrefixDictionary) Get(key *Cell) *Cell {
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

func (d *PrefixDictionary) Set(key, value *Cell) error {
	if value == nil {
		return d.Delete(key)
	}
	_, err := d.SetWithMode(key, value, DictSetModeSet)
	return err
}

// SetIntKey stores a full-width prefix-dictionary key.
func (d *PrefixDictionary) SetIntKey(key *big.Int, value *Cell) error {
	var builder Builder
	var cell Cell
	initIntKeyCell(key, d.keySz, &builder, &cell)
	return d.Set(&cell, value)
}

func (d *PrefixDictionary) SetBuilder(key *Cell, value *Builder) error {
	_, err := d.SetBuilderWithMode(key, value, DictSetModeSet)
	return err
}

func (d *PrefixDictionary) SetWithMode(key, value *Cell, mode DictSetMode) (bool, error) {
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}
	return d.SetBuilderWithMode(key, value.ToBuilder(), mode)
}

func (d *PrefixDictionary) SetBuilderWithMode(key *Cell, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("prefix dict is nil")
	}
	if key == nil || key.BitsSize() > d.keySz {
		return false, fmt.Errorf("invalid key size")
	}
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}

	keySlice, err := key.BeginParse()
	if err != nil {
		return false, fmt.Errorf("failed to load key: %w", err)
	}

	newRoot, changed, err := d.set(d.root, keySlice, d.keySz, value, mode)
	if err != nil {
		return false, err
	}
	if changed {
		d.setRoot(newRoot)
	}
	return changed, nil
}

func (d *PrefixDictionary) LoadValueAndDelete(key *Cell) (*Slice, error) {
	if d == nil {
		return nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsSize() > d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	keySlice, err := key.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}

	value, newRoot, changed, err := d.lookupDelete(d.root, keySlice, d.keySz)
	if err != nil {
		return nil, err
	}
	if !changed || value == nil || sameDictRoot(d.root, newRoot) {
		return nil, ErrNoSuchKeyInDict
	}

	d.setRoot(newRoot)
	return value, nil
}

func (d *PrefixDictionary) Delete(key *Cell) error {
	_, err := d.LoadValueAndDelete(key)
	return err
}

// DeleteIntKey removes a full-width prefix-dictionary key.
func (d *PrefixDictionary) DeleteIntKey(key *big.Int) error {
	var builder Builder
	var cell Cell
	initIntKeyCell(key, d.keySz, &builder, &cell)
	return d.Delete(&cell)
}

func (d *PrefixDictionary) MustToCell() *Cell {
	return d.AsCell()
}

func (d *PrefixDictionary) AsCell() *Cell {
	if d == nil {
		return nil
	}
	return d.root
}

func (d *PrefixDictionary) ToCell() (*Cell, error) {
	if d == nil {
		return nil, nil
	}
	return d.root, nil
}

func (d *PrefixDictionary) set(branch *Cell, key *Slice, remaining uint, value *Builder, mode DictSetMode) (*Cell, bool, error) {
	if key.BitsLeft() > remaining {
		return nil, false, fmt.Errorf("invalid key size")
	}

	if branch == nil {
		if mode == DictSetModeReplace {
			return nil, false, nil
		}
		leaf, err := d.storePrefixLeaf(key, value, remaining)
		return leaf, err == nil, err
	}

	node, err := parseFixedDictNode(branch, remaining)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load label: %w", err)
	}
	if err = node.rejectSpecial("prefix dict"); err != nil {
		return nil, false, err
	}

	bitsMatches, isNewRight, diverged, err := matchLabelView(node.label, node.labelLen, key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}

	if bitsMatches < node.labelLen {
		if !diverged || mode == DictSetModeReplace {
			return node.cell, false, nil
		}

		prefixLabel, labelRemainder, err := node.splitLabel(bitsMatches)
		if err != nil {
			return nil, false, fmt.Errorf("failed to split old child label: %w", err)
		}

		newLeaf, err := d.storePrefixLeaf(key, value, remaining-(bitsMatches+1))
		if err != nil {
			return nil, false, fmt.Errorf("failed to build new leaf: %w", err)
		}

		oldChild, err := d.storePrefixNode(labelRemainder, node.loader.ToBuilder(), remaining-(bitsMatches+1))
		if err != nil {
			return nil, false, fmt.Errorf("failed to rebuild old child: %w", err)
		}

		var left, right *Cell
		if isNewRight {
			left, right = oldChild, newLeaf
		} else {
			left, right = newLeaf, oldChild
		}

		fork, err := d.storePrefixFork(prefixLabel, left, right, remaining)
		if err != nil {
			return nil, false, fmt.Errorf("failed to build new fork: %w", err)
		}
		return fork, true, nil
	}

	isFork, err := node.loader.LoadBoolBit()
	if err != nil {
		return nil, false, fmt.Errorf("no node constructor in a prefix code dictionary")
	}

	if !isFork {
		if key.BitsLeft() != 0 || mode == DictSetModeAdd {
			return node.cell, false, nil
		}
		nodeLabel := node.labelSlice()
		leaf, err := d.storePrefixLeaf(&nodeLabel, value, remaining)
		if err != nil {
			return nil, false, fmt.Errorf("failed to replace leaf: %w", err)
		}
		return leaf, true, nil
	}

	if remaining == node.labelLen {
		return nil, false, fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
	}
	if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
		return nil, false, fmt.Errorf("invalid fork node in a prefix code dictionary")
	}
	if key.BitsLeft() == 0 {
		return node.cell, false, nil
	}

	idx, err := key.LoadUInt(1)
	if err != nil {
		return nil, false, err
	}

	child, err := node.ref(int(idx))
	if err != nil {
		return nil, false, err
	}

	child, changed, err := d.set(child, key, remaining-(node.labelLen+1), value, mode)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return node.cell, false, nil
	}

	return node.cloneWithRef(int(idx), child, d.trace)
}

func (d *PrefixDictionary) lookupDelete(branch *Cell, key *Slice, remaining uint) (*Slice, *Cell, bool, error) {
	if key.BitsLeft() > remaining {
		return nil, nil, false, fmt.Errorf("incorrect key size")
	}
	if branch == nil {
		return nil, nil, false, nil
	}

	node, err := parseFixedDictNode(branch, remaining)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load label: %w", err)
	}
	if err = node.rejectSpecial("prefix dict"); err != nil {
		return nil, nil, false, err
	}

	nodeLabel := node.labelSlice()
	bitsMatches, err := consumeCommonPrefix(&nodeLabel, key, node.labelLen)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}

	if bitsMatches < node.labelLen {
		return nil, nil, false, nil
	}

	isFork, err := node.loader.LoadBoolBit()
	if err != nil {
		return nil, nil, false, fmt.Errorf("no node constructor in a prefix code dictionary")
	}

	if !isFork {
		if key.BitsLeft() != 0 {
			return nil, nil, false, nil
		}
		return node.value(), nil, true, nil
	}

	if remaining == node.labelLen {
		return nil, nil, false, fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
	}
	if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
		return nil, nil, false, fmt.Errorf("invalid fork node in a prefix code dictionary")
	}
	if key.BitsLeft() == 0 {
		return nil, nil, false, nil
	}

	idx, err := key.LoadUInt(1)
	if err != nil {
		return nil, nil, false, err
	}

	child, err := node.ref(int(idx))
	if err != nil {
		return nil, nil, false, err
	}

	oldValue, newChild, changed, err := d.lookupDelete(child, key, remaining-(node.labelLen+1))
	if err != nil {
		return nil, nil, false, err
	}
	if !changed {
		return nil, nil, false, nil
	}

	otherIdx := idx ^ 1
	otherChild, err := node.ref(int(otherIdx))
	if err != nil {
		return nil, nil, false, err
	}

	if newChild != nil && otherChild != nil {
		cloned, changed, err := node.cloneWithRef(int(idx), newChild, d.trace)
		return oldValue, cloned, changed, err
	}

	survivor := otherChild
	survivorBit := otherIdx
	if newChild != nil {
		survivor = newChild
		survivorBit = idx
	}

	if survivor == nil {
		return oldValue, nil, true, nil
	}

	survivorSlice, err := survivor.BeginParse()
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load survivor branch: %w", err)
	}

	_, survivorLabel, err := loadLabel(remaining-(node.labelLen+1), survivorSlice, BeginCell())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load survivor label: %w", err)
	}

	mergedLabel, err := node.mergedEdgeLabel(uint64(survivorBit), survivorLabel, "survivor")
	if err != nil {
		return nil, nil, false, err
	}

	merged, err := d.storePrefixNode(mergedLabel, survivorSlice.ToBuilder(), remaining)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to merge prefix edges: %w", err)
	}
	return oldValue, merged, true, nil
}

func (d *PrefixDictionary) storePrefixNode(label *Slice, payload *Builder, remaining uint) (*Cell, error) {
	node, err := storeDictNodeTraced(label, payload, remaining, d.trace)
	if err != nil {
		return nil, fmt.Errorf("failed to store prefix node: %w", err)
	}
	return node, nil
}

func (d *PrefixDictionary) storePrefixLeaf(label *Slice, value *Builder, remaining uint) (*Cell, error) {
	b := BeginCell().SetTrace(d.trace)
	if err := storeDictLabel(b, label, remaining); err != nil {
		return nil, fmt.Errorf("failed to store label: %w", err)
	}
	if err := b.StoreBoolBit(false); err != nil {
		return nil, fmt.Errorf("failed to store leaf constructor: %w", err)
	}
	if err := b.StoreBuilderUncheckedDepth(value); err != nil {
		return nil, fmt.Errorf("failed to store value: %w", err)
	}
	return b.EndCellSpecial(false)
}

func (d *PrefixDictionary) storePrefixFork(label *Slice, left, right *Cell, remaining uint) (*Cell, error) {
	b := BeginCell().SetTrace(d.trace)
	if err := storeDictLabel(b, label, remaining); err != nil {
		return nil, fmt.Errorf("failed to store label: %w", err)
	}
	if err := b.StoreBoolBit(true); err != nil {
		return nil, fmt.Errorf("failed to store fork constructor: %w", err)
	}
	if err := b.StoreRefUncheckedDepth(left); err != nil {
		return nil, fmt.Errorf("failed to store left branch: %w", err)
	}
	if err := b.StoreRefUncheckedDepth(right); err != nil {
		return nil, fmt.Errorf("failed to store right branch: %w", err)
	}
	return b.EndCellSpecial(false)
}

func validatePrefixDictRoot(root *Cell, keySz uint) error {
	if root == nil {
		return validateDictKeySize(keySz)
	}
	if err := validateDictKeySize(keySz); err != nil {
		return err
	}
	return validatePrefixDictNode(root.WithTrace(nil), keySz)
}

func validatePrefixDictNode(c *Cell, keySz uint) error {
	if c == nil {
		return fmt.Errorf("prefix dict branch is nil")
	}

	node, err := parseFixedDictNode(c, keySz)
	if err != nil {
		return err
	}

	if pruned, err := node.prunedBoundary("prefix dict"); err != nil || pruned {
		return err
	}

	isFork, err := node.loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("no node constructor in a prefix code dictionary")
	}
	if !isFork {
		return nil
	}

	if node.labelLen == keySz {
		return fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
	}
	if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
		return fmt.Errorf("invalid fork node in a prefix code dictionary")
	}

	nextKeySz := node.nextKeyBits(keySz)

	left, err := node.boundaryRef(0)
	if err != nil {
		return fmt.Errorf("failed to load left branch: %w", err)
	}
	if err = validatePrefixDictNode(left, nextKeySz); err != nil {
		return fmt.Errorf("invalid left branch: %w", err)
	}

	right, err := node.boundaryRef(1)
	if err != nil {
		return fmt.Errorf("failed to load right branch: %w", err)
	}
	if err = validatePrefixDictNode(right, nextKeySz); err != nil {
		return fmt.Errorf("invalid right branch: %w", err)
	}

	return nil
}
