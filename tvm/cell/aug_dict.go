package cell

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
)

var ErrAugmentationSemanticsUnavailable = errors.New("augmented dict was loaded without augmentation semantics; use the *WithAugmentation loader to enable mutation and extra validation")

type AugmentedExtraSkipper func(*Slice) error

type Augmentation interface {
	SkipExtra(*Slice) error
	EmptyExtra() (*Cell, error)
	LeafExtra(value *Slice) (*Cell, error)
	CombineExtra(leftExtra, rightExtra *Slice) (*Cell, error)
}

type readOnlyAugmentation struct {
	skip AugmentedExtraSkipper
}

func (a readOnlyAugmentation) SkipExtra(loader *Slice) error {
	if a.skip == nil {
		return fmt.Errorf("augmented dict extra skipper is nil")
	}
	return a.skip(loader)
}

func (a readOnlyAugmentation) EmptyExtra() (*Cell, error) {
	return nil, ErrAugmentationSemanticsUnavailable
}

func (a readOnlyAugmentation) LeafExtra(*Slice) (*Cell, error) {
	return nil, ErrAugmentationSemanticsUnavailable
}

func (a readOnlyAugmentation) CombineExtra(*Slice, *Slice) (*Cell, error) {
	return nil, ErrAugmentationSemanticsUnavailable
}

type AugmentedDictionary struct {
	keySz uint

	root      *Cell
	rootExtra *Cell
	wrapped   bool

	aug Augmentation
}

func NewAugDict(keySz uint, aug Augmentation) (*AugmentedDictionary, error) {
	if aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}

	rootExtra, err := aug.EmptyExtra()
	if err != nil {
		return nil, fmt.Errorf("failed to compute empty extra: %w", err)
	}

	return &AugmentedDictionary{
		keySz:     keySz,
		rootExtra: rootExtra,
		wrapped:   true,
		aug:       aug,
	}, nil
}

func (c *Cell) AsAugDict(keySz uint, aug Augmentation) *AugmentedDictionary {
	return &AugmentedDictionary{
		keySz: keySz,
		root:  c,
		aug:   aug,
	}
}

func (c *Slice) ToAugDict(keySz uint, skipExtra AugmentedExtraSkipper) (*AugmentedDictionary, error) {
	return c.ToAugDictWithValue(keySz, skipExtra, nil)
}

func (c *Slice) ToAugDictWithAugmentation(keySz uint, aug Augmentation) (*AugmentedDictionary, error) {
	return c.ToAugDictWithValueAndAugmentation(keySz, aug, nil)
}

// ToAugDictWithValue is the safe inline HashmapAug loader when the augmented
// leaf value does not consume the whole remainder of the current slice.
// Pass nil skipValue only when the augmented dict occupies the rest of the slice.
func (c *Slice) ToAugDictWithValue(keySz uint, skipExtra AugmentedExtraSkipper, skipValue AugmentedExtraSkipper) (*AugmentedDictionary, error) {
	return c.ToAugDictWithValueAndAugmentation(keySz, readOnlyAugmentation{skip: skipExtra}, skipValue)
}

func (c *Slice) ToAugDictWithValueAndAugmentation(keySz uint, aug Augmentation, skipValue AugmentedExtraSkipper) (*AugmentedDictionary, error) {
	if aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}

	var (
		root *Cell
		err  error
	)

	if skipValue == nil {
		root, err = c.ToCell()
		if err != nil {
			return nil, err
		}
	} else {
		root, err = captureConsumedPrefix(c, func(loader *Slice) error {
			labelLen, _, err := loadLabel(keySz, loader, BeginCell())
			if err != nil {
				return err
			}

			if labelLen == keySz {
				if err = aug.SkipExtra(loader); err != nil {
					return err
				}
				return skipValue(loader)
			}

			if _, err = loader.LoadRefCell(); err != nil {
				return err
			}
			if _, err = loader.LoadRefCell(); err != nil {
				return err
			}
			return aug.SkipExtra(loader)
		})
		if err != nil {
			return nil, err
		}
	}

	if err = validateAugmentedDictRoot(root, keySz, aug); err != nil {
		return nil, fmt.Errorf("failed to validate augmented dict: %w", err)
	}

	return &AugmentedDictionary{
		keySz: keySz,
		root:  root,
		aug:   aug,
	}, nil
}

func (c *Slice) LoadAugDict(keySz uint, skipExtra AugmentedExtraSkipper) (*AugmentedDictionary, error) {
	return c.LoadAugDictWithAugmentation(keySz, readOnlyAugmentation{skip: skipExtra})
}

func (c *Slice) LoadAugDictWithAugmentation(keySz uint, aug Augmentation) (*AugmentedDictionary, error) {
	if aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}

	hasSemantics, err := augmentationSupportsSemantics(aug)
	if err != nil {
		return nil, err
	}

	hasRoot, err := c.LoadBoolBit()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dict root flag: %w", err)
	}

	if !hasRoot {
		rootExtra, err := captureConsumedPrefix(c, aug.SkipExtra)
		if err != nil {
			return nil, fmt.Errorf("failed to load augmented dict empty extra: %w", err)
		}

		if hasSemantics {
			expected, expectedErr := aug.EmptyExtra()
			if expectedErr != nil {
				return nil, fmt.Errorf("failed to compute augmented dict empty extra: %w", expectedErr)
			}
			if !equalCellContents(rootExtra, expected) {
				return nil, fmt.Errorf("augmented dict empty extra mismatch")
			}
		}

		return &AugmentedDictionary{
			keySz:     keySz,
			rootExtra: rootExtra,
			wrapped:   true,
			aug:       aug,
		}, nil
	}

	root, err := c.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dict root ref: %w", err)
	}

	rootExtra, err := captureConsumedPrefix(c, aug.SkipExtra)
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dict root extra: %w", err)
	}

	if err = validateAugmentedDictRoot(root, keySz, aug); err != nil {
		return nil, fmt.Errorf("failed to validate augmented dict root: %w", err)
	}

	if hasSemantics {
		expected, expectedErr := computeAugmentedNodeExtra(root, keySz, aug)
		if expectedErr != nil {
			return nil, fmt.Errorf("failed to compute augmented dict root extra: %w", expectedErr)
		}
		if !equalCellContents(rootExtra, expected) {
			return nil, fmt.Errorf("augmented dict root extra mismatch")
		}
	} else {
		nodeExtra, err := extractAugmentedNodeExtra(root, keySz, aug.SkipExtra)
		if err != nil {
			return nil, fmt.Errorf("failed to extract augmented dict node extra: %w", err)
		}
		if !equalCellContents(rootExtra, nodeExtra) {
			return nil, fmt.Errorf("augmented dict root extra mismatch")
		}
	}

	return &AugmentedDictionary{
		keySz:     keySz,
		root:      root,
		rootExtra: rootExtra,
		wrapped:   true,
		aug:       aug,
	}, nil
}

func (c *Slice) MustLoadAugDict(keySz uint, skipExtra AugmentedExtraSkipper) *AugmentedDictionary {
	dict, err := c.LoadAugDict(keySz, skipExtra)
	if err != nil {
		panic(err)
	}
	return dict
}

func (c *Slice) MustLoadAugDictWithAugmentation(keySz uint, aug Augmentation) *AugmentedDictionary {
	dict, err := c.LoadAugDictWithAugmentation(keySz, aug)
	if err != nil {
		panic(err)
	}
	return dict
}

func (d *AugmentedDictionary) GetKeySize() uint {
	return d.keySz
}

func (d *AugmentedDictionary) Copy() *AugmentedDictionary {
	if d == nil {
		return nil
	}

	return &AugmentedDictionary{
		keySz:     d.keySz,
		root:      d.root,
		rootExtra: d.rootExtra,
		wrapped:   d.wrapped,
		aug:       d.aug,
	}
}

func (d *AugmentedDictionary) IsEmpty() bool {
	return d == nil || d.root == nil
}

func (d *AugmentedDictionary) GetRootExtra() *Cell {
	s, err := d.LoadRootExtra()
	if err != nil || s == nil {
		return nil
	}
	c, err := s.ToCell()
	if err != nil {
		return nil
	}
	return c
}

func (d *AugmentedDictionary) LoadRootExtra() (*Slice, error) {
	if d == nil {
		return nil, nil
	}

	if d.rootExtra != nil {
		return d.rootExtra.BeginParse(), nil
	}

	if d.root == nil {
		if d.aug == nil {
			return nil, fmt.Errorf("augmentation is nil")
		}
		extra, err := d.aug.EmptyExtra()
		if err != nil {
			return nil, err
		}
		return extra.BeginParse(), nil
	}

	if d.aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}

	extra, err := extractAugmentedNodeExtra(d.root, d.keySz, d.aug.SkipExtra)
	if err != nil {
		return nil, err
	}
	return extra.BeginParse(), nil
}

func (d *AugmentedDictionary) SetIntKey(key *big.Int, value *Cell) error {
	_, err := d.SetWithMode(BeginCell().MustStoreBigInt(key, d.keySz).EndCell(), value, DictSetModeSet)
	return err
}

func (d *AugmentedDictionary) DeleteIntKey(key *big.Int) error {
	return d.Delete(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

func (d *AugmentedDictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	return d.LoadValue(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

func (d *AugmentedDictionary) LoadValueWithExtraByIntKey(key *big.Int) (*Slice, error) {
	return d.LoadValueWithExtra(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

func (d *AugmentedDictionary) LoadValueWithExtra(key *Cell) (*Slice, error) {
	return d.loadValueExtraSlice(key)
}

func (d *AugmentedDictionary) loadValueExtraSlice(key *Cell) (*Slice, error) {
	if key == nil || key.BitsSize() != d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}
	return findKeyInDict(d.root, key, nil, 0, false)
}

func (d *AugmentedDictionary) LoadValue(key *Cell) (*Slice, error) {
	valueExtra, err := d.loadValueExtraSlice(key)
	if err != nil {
		return nil, err
	}

	value, _, err := d.decomposeValueExtra(valueExtra)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (d *AugmentedDictionary) LoadValueExtra(key *Cell) (*Slice, *Slice, error) {
	valueExtra, err := d.loadValueExtraSlice(key)
	if err != nil {
		return nil, nil, err
	}
	return d.decomposeValueExtra(valueExtra)
}

func (d *AugmentedDictionary) LoadValueExtraByIntKey(key *big.Int) (*Slice, *Slice, error) {
	return d.LoadValueExtra(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

func (d *AugmentedDictionary) GetWithExtra(key *Cell) *Cell {
	slc, err := d.LoadValueWithExtra(key)
	if err != nil {
		return nil
	}

	c, err := slc.ToCell()
	if err != nil {
		return nil
	}
	return c
}

func (d *AugmentedDictionary) Get(key *Cell) *Cell {
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

func (d *AugmentedDictionary) LoadValueWithExtraAndDelete(key *Cell) (*Slice, error) {
	valueExtra, changed, err := d.lookupDeleteWithExtra(key)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrNoSuchKeyInDict
	}
	return valueExtra, nil
}

func (d *AugmentedDictionary) LoadValueAndDelete(key *Cell) (*Slice, error) {
	valueExtra, err := d.LoadValueWithExtraAndDelete(key)
	if err != nil {
		return nil, err
	}
	value, _, err := d.decomposeValueExtra(valueExtra)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (d *AugmentedDictionary) LoadValueExtraAndDelete(key *Cell) (*Slice, *Slice, error) {
	valueExtra, err := d.LoadValueWithExtraAndDelete(key)
	if err != nil {
		return nil, nil, err
	}
	return d.decomposeValueExtra(valueExtra)
}

func (d *AugmentedDictionary) Delete(key *Cell) error {
	_, _, err := d.lookupDeleteWithExtra(key)
	return err
}

func (d *AugmentedDictionary) Set(key, value *Cell) error {
	_, err := d.SetWithMode(key, value, DictSetModeSet)
	return err
}

func (d *AugmentedDictionary) SetWithMode(key, value *Cell, mode DictSetMode) (bool, error) {
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}
	return d.SetBuilderWithMode(key, value.ToBuilder(), mode)
}

func (d *AugmentedDictionary) SetBuilder(key *Cell, value *Builder) error {
	_, err := d.SetBuilderWithMode(key, value, DictSetModeSet)
	return err
}

func (d *AugmentedDictionary) SetBuilderWithMode(key *Cell, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("dict is nil")
	}
	if key == nil || key.BitsSize() != d.keySz {
		return false, fmt.Errorf("invalid key size")
	}
	if value == nil {
		return false, fmt.Errorf("value builder is nil")
	}
	if err := d.ensureWritable(); err != nil {
		return false, err
	}

	newRoot, rootExtra, changed, err := d.set(d.root, key.BeginParse(), d.keySz, value, mode)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err = d.setRootWithExtra(newRoot, rootExtra); err != nil {
		return false, err
	}
	return true, nil
}

func (d *AugmentedDictionary) ToCell() (*Cell, error) {
	if d == nil {
		return nil, nil
	}

	if !d.wrapped {
		if d.root == nil {
			return nil, fmt.Errorf("inline augmented dict cannot be empty")
		}
		return d.root, nil
	}

	rootExtra, err := d.ensureRootExtra()
	if err != nil {
		return nil, err
	}

	b := BeginCell()
	if d.root == nil {
		if err := b.StoreUInt(0, 1); err != nil {
			return nil, err
		}
		if err := b.StoreBuilder(rootExtra.ToBuilder()); err != nil {
			return nil, err
		}
		return b.EndCell(), nil
	}

	if err := b.StoreUInt(1, 1); err != nil {
		return nil, err
	}
	if err := b.StoreRef(d.root); err != nil {
		return nil, err
	}
	if err := b.StoreBuilder(rootExtra.ToBuilder()); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (d *AugmentedDictionary) MustToCell() *Cell {
	c, err := d.ToCell()
	if err != nil {
		panic(err)
	}
	return c
}

func (d *AugmentedDictionary) AsCell() *Cell {
	return d.MustToCell()
}

func (d *AugmentedDictionary) skipExtra(loader *Slice) error {
	if d == nil || d.aug == nil {
		return fmt.Errorf("augmentation is nil")
	}
	return d.aug.SkipExtra(loader)
}

func (d *AugmentedDictionary) ensureWritable() error {
	if d == nil || d.aug == nil {
		return fmt.Errorf("augmentation is nil")
	}
	_, err := d.aug.EmptyExtra()
	if err != nil {
		return err
	}
	return nil
}

func (d *AugmentedDictionary) ensureRootExtra() (*Cell, error) {
	if d.rootExtra != nil {
		return d.rootExtra, nil
	}
	if d.aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}
	if d.root == nil {
		extra, err := d.aug.EmptyExtra()
		if err != nil {
			return nil, err
		}
		d.rootExtra = extra
		return extra, nil
	}
	extra, err := extractAugmentedNodeExtra(d.root, d.keySz, d.aug.SkipExtra)
	if err != nil {
		return nil, err
	}
	d.rootExtra = extra
	return extra, nil
}

func (d *AugmentedDictionary) decomposeValueExtra(valueExtra *Slice) (*Slice, *Slice, error) {
	if valueExtra == nil {
		return nil, nil, ErrNoSuchKeyInDict
	}

	value := valueExtra.Copy()
	extraCell, err := captureConsumedPrefix(value, d.skipExtra)
	if err != nil {
		return nil, nil, err
	}
	return value, extraCell.BeginParse(), nil
}

func (d *AugmentedDictionary) lookupDeleteWithExtra(key *Cell) (*Slice, bool, error) {
	if d == nil {
		return nil, false, fmt.Errorf("dict is nil")
	}
	if key == nil || key.BitsSize() != d.keySz {
		return nil, false, fmt.Errorf("incorrect key size")
	}
	if err := d.ensureWritable(); err != nil {
		return nil, false, err
	}

	newRoot, rootExtra, removed, changed, err := d.delete(d.root, key.BeginParse(), d.keySz)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return nil, false, nil
	}
	if err = d.setRootWithExtra(newRoot, rootExtra); err != nil {
		return nil, false, err
	}
	return removed, true, nil
}

func (d *AugmentedDictionary) setRootWithExtra(root, rootExtra *Cell) error {
	d.root = root
	if !d.wrapped {
		d.rootExtra = rootExtra
		return nil
	}

	if d.aug == nil {
		return fmt.Errorf("augmentation is nil")
	}

	if root == nil {
		if rootExtra == nil {
			extra, err := d.aug.EmptyExtra()
			if err != nil {
				return err
			}
			rootExtra = extra
		}
		d.rootExtra = rootExtra
		return nil
	}

	if rootExtra == nil {
		extra, err := extractAugmentedNodeExtra(root, d.keySz, d.aug.SkipExtra)
		if err != nil {
			return err
		}
		rootExtra = extra
	}
	d.rootExtra = rootExtra
	return nil
}

func (d *AugmentedDictionary) set(branch *Cell, pfx *Slice, keyOffset uint, value *Builder, mode DictSetMode) (*Cell, *Cell, bool, error) {
	if branch == nil {
		if mode == DictSetModeReplace {
			return nil, nil, false, nil
		}
		leaf, extra, err := d.storeLeafWithExtra(pfx, value, keyOffset)
		return leaf, extra, err == nil, err
	}

	s := branch.BeginParse()
	sz, kPart, err := loadLabel(keyOffset, s, BeginCell())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load label: %w", err)
	}

	isNewRight, matches := false, true
	kPartSlice := kPart.ToSlice()
	var bitsMatches uint
	for bitsMatches = 0; bitsMatches < sz; bitsMatches++ {
		vCurr, err := kPartSlice.LoadUInt(1)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load current key bit: %w", err)
		}

		vNew, err := pfx.LoadUInt(1)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to load new key bit: %w", err)
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
				return branch, nil, false, nil
			}
			leaf, extra, err := d.storeLeafWithExtra(kPart.ToSlice(), value, keyOffset)
			return leaf, extra, err == nil, err
		}

		refIdx := int(pfx.MustLoadUInt(1))
		ref, err := branch.PeekRef(refIdx)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
		}

		nextKeyOffset := keyOffset - (bitsMatches + 1)
		ref, refExtra, changed, err := d.set(ref, pfx, nextKeyOffset, value, mode)
		if err != nil {
			return nil, nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
		}
		if !changed {
			return branch, nil, false, nil
		}

		if ref == nil {
			return nil, nil, false, fmt.Errorf("set produced nil child")
		}

		left, err := branch.PeekRef(0)
		if err != nil {
			return nil, nil, false, err
		}
		right, err := branch.PeekRef(1)
		if err != nil {
			return nil, nil, false, err
		}
		otherExtra, err := extractAugmentedNodeExtra(branch.MustPeekRef(refIdx^1), nextKeyOffset, d.aug.SkipExtra)
		if err != nil {
			return nil, nil, false, err
		}
		leftExtra, rightExtra := otherExtra, otherExtra
		if refIdx == 0 {
			left = ref
			leftExtra = refExtra
		} else {
			right = ref
			rightExtra = refExtra
		}

		if refIdx == 0 {
			rightExtra = otherExtra
		} else {
			leftExtra = otherExtra
		}

		newBranch, extra, err := d.storeForkWithExtra(kPart.ToSlice(), left, leftExtra, right, rightExtra, keyOffset)
		return newBranch, extra, err == nil, err
	}

	if mode == DictSetModeReplace {
		return branch, nil, false, nil
	}

	prefixBits := kPart.ToSlice().MustLoadSlice(bitsMatches)
	prefixLabel := BeginCell().MustStoreSlice(prefixBits, bitsMatches).ToSlice()

	oldChild := BeginCell()
	if err = storeDictLabel(oldChild, kPartSlice, keyOffset-(bitsMatches+1)); err != nil {
		return nil, nil, false, fmt.Errorf("failed to store old child label: %w", err)
	}
	if err = oldChild.StoreBuilder(s.ToBuilder()); err != nil {
		return nil, nil, false, fmt.Errorf("failed to store old child payload: %w", err)
	}
	oldExtra, err := extractAugmentedNodeExtra(branch, keyOffset, d.aug.SkipExtra)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to extract old child extra: %w", err)
	}

	newChild, newExtra, err := d.storeLeafWithExtra(pfx, value, keyOffset-(bitsMatches+1))
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to store new child leaf: %w", err)
	}

	left, right := newChild, oldChild.EndCell()
	leftExtra, rightExtra := newExtra, oldExtra
	if isNewRight {
		left, right = right, left
		leftExtra, rightExtra = rightExtra, leftExtra
	}

	newBranch, extra, err := d.storeForkWithExtra(prefixLabel, left, leftExtra, right, rightExtra, keyOffset)
	return newBranch, extra, err == nil, err
}

func (d *AugmentedDictionary) delete(branch *Cell, pfx *Slice, keyOffset uint) (*Cell, *Cell, *Slice, bool, error) {
	if branch == nil {
		return nil, nil, nil, false, nil
	}

	s := branch.BeginParse()
	sz, kPart, err := loadLabel(keyOffset, s, BeginCell())
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("failed to load label: %w", err)
	}

	bitsMatches, err := consumeCommonPrefix(kPart.ToSlice(), pfx, sz)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}
	if bitsMatches < sz {
		return branch, nil, nil, false, nil
	}

	if pfx.BitsLeft() == 0 {
		return nil, nil, s, true, nil
	}

	refIdx := int(pfx.MustLoadUInt(1))
	ref, err := branch.PeekRef(refIdx)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
	}

	nextKeyOffset := keyOffset - (bitsMatches + 1)
	ref, refExtra, removed, changed, err := d.delete(ref, pfx, nextKeyOffset)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
	}
	if !changed {
		return branch, nil, nil, false, nil
	}

	if ref == nil {
		otherIdx := refIdx ^ 1
		otherRef, err := branch.PeekRef(otherIdx)
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to peek neighbour ref %d: %w", otherIdx, err)
		}

		slc := otherRef.BeginParse()
		_, otherLabel, err := loadLabel(nextKeyOffset, slc, BeginCell())
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to load neighbour label: %w", err)
		}
		otherExtra, err := extractAugmentedNodeExtra(otherRef, nextKeyOffset, d.aug.SkipExtra)
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to extract neighbour extra: %w", err)
		}

		if err = kPart.StoreUInt(uint64(otherIdx), 1); err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to append neighbour edge bit: %w", err)
		}
		if err = kPart.StoreBuilder(otherLabel); err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to append neighbour label: %w", err)
		}

		merged, err := d.storeNode(kPart.ToSlice(), slc.ToBuilder(), keyOffset)
		if err != nil {
			return nil, nil, nil, false, err
		}
		return merged, otherExtra, removed, true, nil
	}

	left, err := branch.PeekRef(0)
	if err != nil {
		return nil, nil, nil, false, err
	}
	right, err := branch.PeekRef(1)
	if err != nil {
		return nil, nil, nil, false, err
	}
	otherExtra, err := extractAugmentedNodeExtra(branch.MustPeekRef(refIdx^1), nextKeyOffset, d.aug.SkipExtra)
	if err != nil {
		return nil, nil, nil, false, err
	}
	leftExtra, rightExtra := otherExtra, otherExtra
	if refIdx == 0 {
		left = ref
		leftExtra = refExtra
	} else {
		right = ref
		rightExtra = refExtra
	}
	if refIdx == 0 {
		rightExtra = otherExtra
	} else {
		leftExtra = otherExtra
	}

	newBranch, extra, err := d.storeForkWithExtra(kPart.ToSlice(), left, leftExtra, right, rightExtra, keyOffset)
	if err != nil {
		return nil, nil, nil, false, err
	}
	return newBranch, extra, removed, true, nil
}

func (d *AugmentedDictionary) storeLeafWithExtra(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, *Cell, error) {
	if value == nil {
		return nil, nil, nil
	}

	extra, err := d.aug.LeafExtra(value.ToSlice())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute leaf extra: %w", err)
	}

	b := BeginCell()
	if err = storeDictLabel(b, keyPfx, keyOffset); err != nil {
		return nil, nil, fmt.Errorf("failed to store label: %w", err)
	}
	if err = b.StoreBuilder(extra.ToBuilder()); err != nil {
		return nil, nil, fmt.Errorf("failed to store leaf extra: %w", err)
	}
	if err = b.StoreBuilder(value); err != nil {
		return nil, nil, fmt.Errorf("failed to store value: %w", err)
	}
	return b.EndCell(), extra, nil
}

func (d *AugmentedDictionary) storeLeaf(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	leaf, _, err := d.storeLeafWithExtra(keyPfx, value, keyOffset)
	return leaf, err
}

func (d *AugmentedDictionary) storeForkWithExtra(label *Slice, left, leftExtra, right, rightExtra *Cell, keyOffset uint) (*Cell, *Cell, error) {
	if left == nil || right == nil {
		return nil, nil, fmt.Errorf("augmented dict fork child is nil")
	}

	labelLen := label.BitsLeft()
	if labelLen >= keyOffset {
		return nil, nil, fmt.Errorf("invalid fork label length")
	}

	extra, err := d.aug.CombineExtra(leftExtra.BeginParse(), rightExtra.BeginParse())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute fork extra: %w", err)
	}

	b := BeginCell()
	if err = storeDictLabel(b, label, keyOffset); err != nil {
		return nil, nil, fmt.Errorf("failed to store label: %w", err)
	}
	if err = b.StoreRef(left); err != nil {
		return nil, nil, err
	}
	if err = b.StoreRef(right); err != nil {
		return nil, nil, err
	}
	if err = b.StoreBuilder(extra.ToBuilder()); err != nil {
		return nil, nil, fmt.Errorf("failed to store fork extra: %w", err)
	}
	return b.EndCell(), extra, nil
}

func (d *AugmentedDictionary) storeFork(label *Slice, left, right *Cell, keyOffset uint) (*Cell, error) {
	labelLen := label.BitsLeft()
	childKeyBits := keyOffset - labelLen - 1

	leftExtra, err := extractAugmentedNodeExtra(left, childKeyBits, d.aug.SkipExtra)
	if err != nil {
		return nil, fmt.Errorf("failed to extract left child extra: %w", err)
	}
	rightExtra, err := extractAugmentedNodeExtra(right, childKeyBits, d.aug.SkipExtra)
	if err != nil {
		return nil, fmt.Errorf("failed to extract right child extra: %w", err)
	}

	fork, _, err := d.storeForkWithExtra(label, left, leftExtra, right, rightExtra, keyOffset)
	return fork, err
}

func (d *AugmentedDictionary) storeNode(label *Slice, payload *Builder, keyOffset uint) (*Cell, error) {
	return storeDictNode(label, payload, keyOffset)
}

func validateAugmentedDictRoot(root *Cell, keySz uint, aug Augmentation) error {
	if root == nil {
		return nil
	}

	if err := validateAugmentedDictNode(root, keySz, aug.SkipExtra); err != nil {
		return err
	}

	hasSemantics, err := augmentationSupportsSemantics(aug)
	if err != nil {
		return err
	}
	if hasSemantics {
		if _, err = computeAugmentedNodeExtra(root, keySz, aug); err != nil {
			return err
		}
	}
	return nil
}

func augmentationSupportsSemantics(aug Augmentation) (bool, error) {
	if aug == nil {
		return false, fmt.Errorf("augmentation is nil")
	}

	_, err := aug.EmptyExtra()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrAugmentationSemanticsUnavailable) {
		return false, nil
	}
	return false, err
}

func validateAugmentedDictNode(c *Cell, keySz uint, skipExtra AugmentedExtraSkipper) error {
	if c == nil {
		return fmt.Errorf("augmented dict branch is nil")
	}

	if c.IsSpecial() {
		if c.GetType() == PrunedCellType {
			return nil
		}
		return fmt.Errorf("augmented dict has unsupported special cell in tree structure")
	}

	loader := c.BeginParse()
	labelLen, _, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return fmt.Errorf("failed to parse augmented dict label: %w", err)
	}

	if labelLen == keySz {
		leaf := loader.Copy()
		if _, err = captureConsumedPrefix(leaf, skipExtra); err != nil {
			return fmt.Errorf("invalid augmented dict leaf extra: %w", err)
		}
		return nil
	}

	fork := loader.Copy()
	if _, err = fork.LoadRefCell(); err != nil {
		return fmt.Errorf("invalid augmented dict fork left ref: %w", err)
	}
	if _, err = fork.LoadRefCell(); err != nil {
		return fmt.Errorf("invalid augmented dict fork right ref: %w", err)
	}
	if _, err = captureConsumedPrefix(fork, skipExtra); err != nil {
		return fmt.Errorf("invalid augmented dict fork extra: %w", err)
	}
	if fork.BitsLeft() != 0 || fork.RefsNum() != 0 {
		return fmt.Errorf("invalid augmented dict fork node")
	}
	return nil
}

func computeAugmentedNodeExtra(c *Cell, keySz uint, aug Augmentation) (*Cell, error) {
	if c == nil {
		return aug.EmptyExtra()
	}

	if c.IsSpecial() {
		if c.GetType() == PrunedCellType {
			return nil, ErrAugmentationSemanticsUnavailable
		}
		return nil, fmt.Errorf("augmented dict has unsupported special cell in tree structure")
	}

	loader := c.BeginParse()
	labelLen, _, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return nil, fmt.Errorf("failed to parse augmented dict label: %w", err)
	}

	if labelLen == keySz {
		valueExtra := loader.Copy()
		storedExtra, err := captureConsumedPrefix(valueExtra, aug.SkipExtra)
		if err != nil {
			return nil, fmt.Errorf("invalid augmented dict leaf extra: %w", err)
		}

		computedExtra, err := aug.LeafExtra(valueExtra.Copy())
		if err != nil {
			return nil, err
		}
		if !equalCellContents(storedExtra, computedExtra) {
			return nil, fmt.Errorf("augmented dict leaf extra mismatch")
		}
		return computedExtra, nil
	}

	left, err := loader.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("invalid augmented dict fork left ref: %w", err)
	}
	right, err := loader.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("invalid augmented dict fork right ref: %w", err)
	}
	storedExtra, err := captureConsumedPrefix(loader, aug.SkipExtra)
	if err != nil {
		return nil, fmt.Errorf("invalid augmented dict fork extra: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return nil, fmt.Errorf("invalid augmented dict fork node")
	}

	childKeyBits := keySz - labelLen - 1
	leftExtra, err := computeAugmentedNodeExtra(left, childKeyBits, aug)
	if err != nil {
		return nil, fmt.Errorf("invalid left branch: %w", err)
	}
	rightExtra, err := computeAugmentedNodeExtra(right, childKeyBits, aug)
	if err != nil {
		return nil, fmt.Errorf("invalid right branch: %w", err)
	}

	computedExtra, err := aug.CombineExtra(leftExtra.BeginParse(), rightExtra.BeginParse())
	if err != nil {
		return nil, err
	}
	if !equalCellContents(storedExtra, computedExtra) {
		return nil, fmt.Errorf("augmented dict fork extra mismatch")
	}
	return computedExtra, nil
}

func extractAugmentedNodeExtra(c *Cell, keySz uint, skipExtra AugmentedExtraSkipper) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("augmented dict branch is nil")
	}

	loader := c.BeginParse()
	labelLen, _, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return nil, fmt.Errorf("failed to parse augmented dict label: %w", err)
	}

	if labelLen == keySz {
		return captureConsumedPrefix(loader.Copy(), skipExtra)
	}

	fork := loader.Copy()
	if _, err = fork.LoadRefCell(); err != nil {
		return nil, fmt.Errorf("failed to load augmented dict left ref: %w", err)
	}
	if _, err = fork.LoadRefCell(); err != nil {
		return nil, fmt.Errorf("failed to load augmented dict right ref: %w", err)
	}
	return captureConsumedPrefix(fork, skipExtra)
}

func captureConsumedPrefix(loader *Slice, consume func(*Slice) error) (*Cell, error) {
	beforeBits := loader.BitsLeft()
	beforeRefs := loader.RefsNum()

	tmp := loader.Copy()
	if err := consume(tmp); err != nil {
		return nil, err
	}

	consumedBits := beforeBits - tmp.BitsLeft()
	consumedRefs := beforeRefs - tmp.RefsNum()

	b := BeginCell()
	if consumedBits > 0 {
		bits, err := loader.LoadSlice(consumedBits)
		if err != nil {
			return nil, err
		}
		if err = b.StoreSlice(bits, consumedBits); err != nil {
			return nil, err
		}
	}
	for i := 0; i < consumedRefs; i++ {
		ref, err := loader.LoadRefCell()
		if err != nil {
			return nil, err
		}
		if err = b.StoreRef(ref); err != nil {
			return nil, err
		}
	}
	return b.EndCell(), nil
}

func equalCellContents(a, b *Cell) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.BitsSize() != b.BitsSize() || a.RefsNum() != b.RefsNum() {
		return false
	}
	return bytes.Equal(a.Hash(0), b.Hash(0))
}
