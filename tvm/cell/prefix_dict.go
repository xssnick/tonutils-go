package cell

import "fmt"

type PrefixDictionary struct {
	keySz uint

	root *Cell
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
	root, err := c.ToCell()
	if err != nil {
		return nil, err
	}
	if err = validatePrefixDictRoot(root, keySz); err != nil {
		return nil, fmt.Errorf("failed to validate prefix dict: %w", err)
	}
	return &PrefixDictionary{
		keySz: keySz,
		root:  root,
	}, nil
}

func (c *Slice) MustToPrefixDict(keySz uint) *PrefixDictionary {
	dict, err := c.ToPrefixDict(keySz)
	if err != nil {
		panic(err)
	}
	return dict
}

func (c *Slice) LoadPrefixDict(keySz uint) (*PrefixDictionary, error) {
	root, err := c.LoadMaybeRef()
	if err != nil {
		return nil, fmt.Errorf("failed to load ref for prefix dict, err: %w", err)
	}

	if root == nil {
		return &PrefixDictionary{
			keySz: keySz,
		}, nil
	}

	return root.ToPrefixDict(keySz)
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
	}
}

func (d *PrefixDictionary) IsEmpty() bool {
	return d == nil || d.root == nil
}

func (d *PrefixDictionary) LookupPrefix(key *Cell) (*Slice, uint, error) {
	if d == nil {
		return nil, 0, nil
	}
	if key.BitsSize() > d.keySz {
		return nil, 0, fmt.Errorf("incorrect key size")
	}
	if d.root == nil {
		return nil, 0, nil
	}

	branch := d.root
	remaining := d.keySz
	matched := uint(0)
	keySlice := key.BeginParse()

	for {
		if branch.special {
			return nil, matched, fmt.Errorf("prefix dict has special cells in tree structure")
		}

		branchSlice := branch.BeginParse()
		labelLen, commonPrefix, err := matchLabelPrefix(remaining, branchSlice, keySlice)
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
			return branchSlice, matched, nil
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

		branch, err = branch.PeekRef(int(idx))
		if err != nil {
			return nil, matched, err
		}
	}
}

func (d *PrefixDictionary) LoadValue(key *Cell) (*Slice, error) {
	value, matched, err := d.LookupPrefix(key)
	if err != nil {
		return nil, err
	}
	if matched != key.BitsSize() || value == nil {
		return nil, ErrNoSuchKeyInDict
	}
	return value, nil
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
	if key.BitsSize() > d.keySz {
		return false, fmt.Errorf("invalid key size")
	}
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}

	newRoot, changed, err := d.set(d.root, key.BeginParse(), d.keySz, value, mode)
	if err != nil {
		return false, err
	}
	if changed {
		d.root = newRoot
	}
	return changed, nil
}

func (d *PrefixDictionary) LoadValueAndDelete(key *Cell) (*Slice, error) {
	if d == nil {
		return nil, ErrNoSuchKeyInDict
	}
	if key.BitsSize() > d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	value, newRoot, changed, err := d.lookupDelete(d.root, key.BeginParse(), d.keySz)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrNoSuchKeyInDict
	}

	d.root = newRoot
	return value, nil
}

func (d *PrefixDictionary) Delete(key *Cell) error {
	_, err := d.LoadValueAndDelete(key)
	return err
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

	if branch.special {
		return nil, false, fmt.Errorf("prefix dict has special cells in tree structure")
	}

	s := branch.BeginParse()
	labelLen, label, err := loadLabel(remaining, s, BeginCell())
	if err != nil {
		return nil, false, fmt.Errorf("failed to load label: %w", err)
	}

	labelSlice := label.ToSlice()
	bitsMatches := uint(0)
	diverged := false
	isNewRight := false

	for bitsMatches < labelLen && key.BitsLeft() > 0 {
		vCurr, err := labelSlice.LoadUInt(1)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load current key bit: %w", err)
		}

		vNew, err := key.LoadUInt(1)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load new key bit: %w", err)
		}

		if vCurr != vNew {
			diverged = true
			isNewRight = vNew != 0
			break
		}
		bitsMatches++
	}

	if bitsMatches < labelLen {
		if !diverged || mode == DictSetModeReplace {
			return branch, false, nil
		}

		prefixBits := label.ToSlice().MustLoadSlice(bitsMatches)
		prefixLabel := BeginCell().MustStoreSlice(prefixBits, bitsMatches).ToSlice()

		newLeaf, err := d.storePrefixLeaf(key, value, remaining-(bitsMatches+1))
		if err != nil {
			return nil, false, fmt.Errorf("failed to build new leaf: %w", err)
		}

		oldChild, err := d.storePrefixNode(labelSlice, s.ToBuilder(), remaining-(bitsMatches+1))
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

	isFork, err := s.LoadBoolBit()
	if err != nil {
		return nil, false, fmt.Errorf("no node constructor in a prefix code dictionary")
	}

	if !isFork {
		if key.BitsLeft() != 0 || mode == DictSetModeAdd {
			return branch, false, nil
		}
		leaf, err := d.storePrefixLeaf(label.ToSlice(), value, remaining)
		if err != nil {
			return nil, false, fmt.Errorf("failed to replace leaf: %w", err)
		}
		return leaf, true, nil
	}

	if remaining == labelLen {
		return nil, false, fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
	}
	if s.BitsLeft() != 0 || s.RefsNum() != 2 {
		return nil, false, fmt.Errorf("invalid fork node in a prefix code dictionary")
	}
	if key.BitsLeft() == 0 {
		return branch, false, nil
	}

	idx, err := key.LoadUInt(1)
	if err != nil {
		return nil, false, err
	}

	child, err := branch.PeekRef(int(idx))
	if err != nil {
		return nil, false, err
	}

	child, changed, err := d.set(child, key, remaining-(labelLen+1), value, mode)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return branch, false, nil
	}

	return branch.cloneWithRef(int(idx), child), true, nil
}

func (d *PrefixDictionary) lookupDelete(branch *Cell, key *Slice, remaining uint) (*Slice, *Cell, bool, error) {
	if key.BitsLeft() > remaining {
		return nil, nil, false, fmt.Errorf("incorrect key size")
	}
	if branch == nil {
		return nil, nil, false, nil
	}
	if branch.special {
		return nil, nil, false, fmt.Errorf("prefix dict has special cells in tree structure")
	}

	s := branch.BeginParse()
	labelLen, label, err := loadLabel(remaining, s, BeginCell())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load label: %w", err)
	}

	bitsMatches, err := consumeCommonPrefix(label.ToSlice(), key, labelLen)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}

	if bitsMatches < labelLen {
		return nil, nil, false, nil
	}

	isFork, err := s.LoadBoolBit()
	if err != nil {
		return nil, nil, false, fmt.Errorf("no node constructor in a prefix code dictionary")
	}

	if !isFork {
		if key.BitsLeft() != 0 {
			return nil, nil, false, nil
		}
		return s, nil, true, nil
	}

	if remaining == labelLen {
		return nil, nil, false, fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
	}
	if s.BitsLeft() != 0 || s.RefsNum() != 2 {
		return nil, nil, false, fmt.Errorf("invalid fork node in a prefix code dictionary")
	}
	if key.BitsLeft() == 0 {
		return nil, nil, false, nil
	}

	idx, err := key.LoadUInt(1)
	if err != nil {
		return nil, nil, false, err
	}

	child, err := branch.PeekRef(int(idx))
	if err != nil {
		return nil, nil, false, err
	}

	oldValue, newChild, changed, err := d.lookupDelete(child, key, remaining-(labelLen+1))
	if err != nil {
		return nil, nil, false, err
	}
	if !changed {
		return nil, nil, false, nil
	}

	otherIdx := idx ^ 1
	otherChild, err := branch.PeekRef(int(otherIdx))
	if err != nil {
		return nil, nil, false, err
	}

	if newChild != nil && otherChild != nil {
		return oldValue, branch.cloneWithRef(int(idx), newChild), true, nil
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

	survivorSlice := survivor.BeginParse()
	_, survivorLabel, err := loadLabel(remaining-(labelLen+1), survivorSlice, BeginCell())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load survivor label: %w", err)
	}

	if err = label.StoreUInt(uint64(survivorBit), 1); err != nil {
		return nil, nil, false, fmt.Errorf("failed to append survivor branch bit: %w", err)
	}
	if err = label.StoreBuilder(survivorLabel); err != nil {
		return nil, nil, false, fmt.Errorf("failed to append survivor label: %w", err)
	}

	merged, err := d.storePrefixNode(label.ToSlice(), survivorSlice.ToBuilder(), remaining)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to merge prefix edges: %w", err)
	}
	return oldValue, merged, true, nil
}

func (d *PrefixDictionary) storePrefixNode(label *Slice, payload *Builder, remaining uint) (*Cell, error) {
	node, err := storeDictNode(label, payload, remaining)
	if err != nil {
		return nil, fmt.Errorf("failed to store prefix node: %w", err)
	}
	return node, nil
}

func (d *PrefixDictionary) storePrefixLeaf(label *Slice, value *Builder, remaining uint) (*Cell, error) {
	b := BeginCell()
	if err := storeDictLabel(b, label, remaining); err != nil {
		return nil, fmt.Errorf("failed to store label: %w", err)
	}
	if err := b.StoreBoolBit(false); err != nil {
		return nil, fmt.Errorf("failed to store leaf constructor: %w", err)
	}
	if err := b.StoreBuilder(value); err != nil {
		return nil, fmt.Errorf("failed to store value: %w", err)
	}
	return b.EndCell(), nil
}

func (d *PrefixDictionary) storePrefixFork(label *Slice, left, right *Cell, remaining uint) (*Cell, error) {
	b := BeginCell()
	if err := storeDictLabel(b, label, remaining); err != nil {
		return nil, fmt.Errorf("failed to store label: %w", err)
	}
	if err := b.StoreBoolBit(true); err != nil {
		return nil, fmt.Errorf("failed to store fork constructor: %w", err)
	}
	if err := b.StoreRef(left); err != nil {
		return nil, fmt.Errorf("failed to store left branch: %w", err)
	}
	if err := b.StoreRef(right); err != nil {
		return nil, fmt.Errorf("failed to store right branch: %w", err)
	}
	return b.EndCell(), nil
}

func validatePrefixDictRoot(root *Cell, keySz uint) error {
	if root == nil {
		return nil
	}
	return validatePrefixDictNode(root, keySz)
}

func validatePrefixDictNode(c *Cell, keySz uint) error {
	if c == nil {
		return fmt.Errorf("prefix dict branch is nil")
	}

	if c.special {
		if c.GetType() == PrunedCellType {
			return nil
		}
		return fmt.Errorf("prefix dict has unsupported special cell in tree structure")
	}

	loader := c.BeginParse()
	labelLen, _, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return fmt.Errorf("failed to parse prefix dict label: %w", err)
	}

	isFork, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("no node constructor in a prefix code dictionary")
	}
	if !isFork {
		return nil
	}

	if labelLen == keySz {
		return fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 2 {
		return fmt.Errorf("invalid fork node in a prefix code dictionary")
	}

	nextKeySz := keySz - labelLen - 1

	left, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load left branch: %w", err)
	}
	if err = validatePrefixDictNode(left, nextKeySz); err != nil {
		return fmt.Errorf("invalid left branch: %w", err)
	}

	right, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load right branch: %w", err)
	}
	if err = validatePrefixDictNode(right, nextKeySz); err != nil {
		return fmt.Errorf("invalid right branch: %w", err)
	}

	return nil
}
