package cell

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
)

var ErrAugmentationSemanticsUnavailable = errors.New("augmented dict was loaded without augmentation semantics; provide an augmentation in LoadAugDict to enable mutation and extra validation")

type AugmentedExtraSkipper func(*Slice) error

// Augmentation computes HashmapAug extras directly into an empty destination
// builder. Inputs are synchronous borrowed views that support Slice parsing
// methods, ToCell and BaseCell; both return an owned, finalized cell.
// Implementations must not use RawCell identity, hashes, levels, or metadata —
// it exposes an unfinalized shell over reused mutation scratch — and must not
// retain the Slice pointers after returning.
type Augmentation interface {
	SkipExtra(*Slice) error
	EmptyExtra(dst *Builder) error
	LeafExtra(value *Slice, dst *Builder) error
	CombineExtra(leftExtra, rightExtra *Slice, dst *Builder) error
}

type ReadOnlyAugmentation struct {
	SkipExtraFn AugmentedExtraSkipper
}

func (a ReadOnlyAugmentation) SkipExtra(loader *Slice) error {
	if a.SkipExtraFn == nil {
		return fmt.Errorf("augmented dict extra skipper is nil")
	}
	return a.SkipExtraFn(loader)
}

func (a ReadOnlyAugmentation) EmptyExtra(*Builder) error {
	return ErrAugmentationSemanticsUnavailable
}

func (a ReadOnlyAugmentation) LeafExtra(*Slice, *Builder) error {
	return ErrAugmentationSemanticsUnavailable
}

func (a ReadOnlyAugmentation) CombineExtra(*Slice, *Slice, *Builder) error {
	return ErrAugmentationSemanticsUnavailable
}

type AugmentedDictionary struct {
	keySz uint

	root      *Cell
	rootExtra *Cell
	wrapped   bool

	aug Augmentation

	trace *Trace
}

type augmentedMutationState struct {
	extra                 Builder
	node                  Builder
	valueCell             Cell
	value                 Slice
	leftExtra, rightExtra Slice
}

func NewAugDict(keySz uint, aug Augmentation) (*AugmentedDictionary, error) {
	if aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}

	var rootExtra Builder
	if err := aug.EmptyExtra(&rootExtra); err != nil {
		return nil, fmt.Errorf("failed to compute empty extra: %w", err)
	}

	return &AugmentedDictionary{
		keySz:     keySz,
		rootExtra: rootExtra.EndCell(),
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
	return c.ToAugDictWithValueAndAugmentation(keySz, ReadOnlyAugmentation{SkipExtraFn: skipExtra}, skipValue)
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

	if err = validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate augmented dict: %w", err)
	}

	return &AugmentedDictionary{
		keySz: keySz,
		root:  root,
		aug:   aug,
	}, nil
}

func (c *Slice) LoadAugDict(keySz uint, aug Augmentation, asProof bool) (*AugmentedDictionary, error) {
	if asProof {
		return c.loadAugDictAsProof(keySz, aug)
	}

	return c.loadAugDictWithAugmentation(keySz, aug)
}

// AugDictInlineIterator iterates a non-empty HashmapAug whose root node is
// serialized inline starting at the slice's current position (as in an
// AccountBlock's transactions field). The slice is advanced past the
// dictionary. skipValue must consume a leaf value that does not occupy the
// rest of the node; pass nil when it does. Unlike ToAugDictWithValue no
// synthetic root cell is built, so no hashing happens. Reset is unsupported
// on the returned iterator: the inline root has no standalone cell to rewind to.
func (c *Slice) AugDictInlineIterator(keySz uint, aug Augmentation, skipValue AugmentedExtraSkipper, rev bool, sgnd bool) (*AugDictIterator, error) {
	raw, dict, err := newAugDictIteratorInline(c, keySz, aug, skipValue, rev, sgnd)
	if err != nil {
		return nil, err
	}
	return newAugDictIterator(raw, dict), nil
}

// AugDictInlineIteratorAt is AugDictInlineIterator positioned at the nearest
// key to `key` in iteration order; see (*AugmentedDictionary).IteratorExtraAt
// for the positioning semantics.
func (c *Slice) AugDictInlineIteratorAt(keySz uint, aug Augmentation, skipValue AugmentedExtraSkipper, key *Cell, rev bool, sgnd bool, allowEq bool) (*AugDictIterator, error) {
	if key == nil || key.BitsSize() != keySz {
		return nil, fmt.Errorf("incorrect key size")
	}

	raw, dict, err := newAugDictIteratorInline(c, keySz, aug, skipValue, rev, sgnd)
	if err != nil {
		return nil, err
	}

	var target Slice
	if err = key.BeginParseInto(&target); err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}
	if err = raw.seekFromTop(&target, allowEq); err != nil {
		return nil, err
	}
	return newAugDictIterator(raw, dict), nil
}

// newAugDictIteratorInline parses the inline root node at the slice position,
// bounds its view to exactly the dictionary content (so a leaf value excludes
// unrelated payload following the dict in the same cell) and pushes it as the
// root frame of a fresh iterator. The caller's slice is advanced past the dict.
func newAugDictIteratorInline(loader *Slice, keySz uint, aug Augmentation, skipValue AugmentedExtraSkipper, rev, invertFirst bool) (*DictIterator, *AugmentedDictionary, error) {
	if aug == nil {
		return nil, nil, fmt.Errorf("augmentation is nil")
	}
	if err := validateDictKeySize(keySz); err != nil {
		return nil, nil, fmt.Errorf("failed to validate augmented dict: %w", err)
	}

	labelLen, label, err := readLabelView(keySz, loader)
	if err != nil {
		return nil, nil, err
	}

	body := *loader // positioned right after the label, like a cell-rooted node
	if labelLen == keySz {
		if err = aug.SkipExtra(loader); err != nil {
			return nil, nil, err
		}
		if skipValue != nil {
			if err = skipValue(loader); err != nil {
				return nil, nil, err
			}
		} else {
			loader.bitStart = loader.bitEnd
			loader.refStart = loader.refEnd
		}
	} else {
		if err = loader.SkipBitsAndRefs(0, 2); err != nil {
			return nil, nil, err
		}
		if err = aug.SkipExtra(loader); err != nil {
			return nil, nil, err
		}
	}
	body.bitEnd = loader.bitStart
	body.refEnd = loader.refStart

	node := fixedDictNode{
		cell:     body.cell,
		refView:  newCellRefView(body.cell),
		loader:   body,
		label:    label,
		labelLen: labelLen,
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return nil, nil, err
	}

	it := &DictIterator{keySz: keySz, rev: rev, invertFirst: invertFirst}
	it.stack = make([]dictIteratorFrame, 0, min(int(keySz)+1, dictIteratorInitialStackDepth))
	if err = it.pushNode(node, keySz, true); err != nil {
		return nil, nil, err
	}
	return it, &AugmentedDictionary{keySz: keySz, aug: aug}, nil
}

func (c *Slice) loadAugDictWithAugmentation(keySz uint, aug Augmentation) (*AugmentedDictionary, error) {
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
			var expected Builder
			if err = aug.EmptyExtra(&expected); err != nil {
				return nil, fmt.Errorf("failed to compute augmented dict empty extra: %w", err)
			}
			if !expected.EqualsCell(rootExtra) {
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

	if err = validateDictKeySize(keySz); err != nil {
		return nil, fmt.Errorf("failed to validate augmented dict root: %w", err)
	}

	nodeExtra, err := extractAugmentedNodeExtra(root.WithTrace(nil), keySz, aug.SkipExtra)
	if err != nil {
		return nil, fmt.Errorf("failed to extract augmented dict node extra: %w", err)
	}
	if !equalCellContents(rootExtra, nodeExtra) {
		return nil, fmt.Errorf("augmented dict root extra mismatch")
	}

	return (&AugmentedDictionary{
		keySz:     keySz,
		root:      root,
		rootExtra: rootExtra,
		wrapped:   true,
		aug:       aug,
	}).SetTrace(root.Trace()), nil
}

func (c *Slice) loadAugDictAsProof(keySz uint, aug Augmentation) (*AugmentedDictionary, error) {
	hasRoot, err := c.LoadBoolBit()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dict root flag: %w", err)
	}

	if !hasRoot {
		rootExtra, err := captureConsumedPrefix(c, aug.SkipExtra)
		if err != nil {
			return nil, fmt.Errorf("failed to load augmented dict empty extra: %w", err)
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

	if root.GetType() != PrunedCellType {
		if err = validateDictKeySize(keySz); err != nil {
			return nil, fmt.Errorf("failed to validate augmented dict root: %w", err)
		}
	}

	return (&AugmentedDictionary{
		keySz:     keySz,
		root:      root,
		rootExtra: rootExtra,
		wrapped:   true,
		aug:       aug,
	}).SetTrace(root.Trace()), nil
}

func (d *AugmentedDictionary) GetKeySize() uint {
	if d == nil {
		return 0
	}
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
		trace:     d.trace,
	}
}

func (d *AugmentedDictionary) SetTrace(trace *Trace) *AugmentedDictionary {
	if d == nil {
		return nil
	}
	d.trace = trace
	d.root = d.root.withTraceCombined(trace)
	return d
}

func (d *AugmentedDictionary) IsEmpty() bool {
	return d == nil || d.root == nil
}

// RootCell returns the trie root without the HashmapAugE wrapper.
func (d *AugmentedDictionary) RootCell() *Cell {
	if d == nil {
		return nil
	}
	return d.root
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
		return d.rootExtra.BeginParse()
	}

	if d.root == nil {
		if d.aug == nil {
			return nil, fmt.Errorf("augmentation is nil")
		}
		var extra Builder
		if err := d.aug.EmptyExtra(&extra); err != nil {
			return nil, err
		}
		return extra.EndCell().BeginParse()
	}

	if d.aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}

	extra, err := extractAugmentedNodeExtra(d.root, d.keySz, d.aug.SkipExtra)
	if err != nil {
		return nil, err
	}
	return extra.BeginParse()
}

func (d *AugmentedDictionary) SetIntKey(key *big.Int, value *Cell) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	if value == nil {
		return fmt.Errorf("value is nil")
	}
	if err := d.ensureWritable(); err != nil {
		return err
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	_, err := d.setBuilderWithModeSlice(&keySlice, value.ToBuilder(), DictSetModeSet)
	return err
}

func (d *AugmentedDictionary) DeleteIntKey(key *big.Int) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	if err := d.ensureWritable(); err != nil {
		return err
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	_, _, err := d.lookupDeleteWithExtraSlice(&keySlice)
	return err
}

func (d *AugmentedDictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueByIntKeyInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (d *AugmentedDictionary) LoadValueWithExtraByIntKey(key *big.Int) (*Slice, error) {
	valueExtra := new(Slice)
	if err := d.LoadValueWithExtraByIntKeyInto(key, valueExtra); err != nil {
		return nil, err
	}
	return valueExtra, nil
}

// LoadValueWithExtraByIntKeyInto is LoadValueWithExtraByIntKey with
// caller-owned result storage.
func (d *AugmentedDictionary) LoadValueWithExtraByIntKeyInto(key *big.Int, valueExtra *Slice) error {
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	plain := Dictionary{keySz: d.keySz, root: d.root, trace: d.trace}
	return plain.findKeySliceInto(&keySlice, valueExtra)
}

// LoadValueByIntKeyInto is LoadValueByIntKey with caller-owned result storage.
func (d *AugmentedDictionary) LoadValueByIntKeyInto(key *big.Int, value *Slice) error {
	var valueExtra Slice
	if err := d.LoadValueWithExtraByIntKeyInto(key, &valueExtra); err != nil {
		return err
	}
	return d.decomposeValueExtraInto(&valueExtra, value, nil)
}

func (d *AugmentedDictionary) LoadValueWithExtra(key *Cell) (*Slice, error) {
	valueExtra := new(Slice)
	if err := d.LoadValueWithExtraInto(key, valueExtra); err != nil {
		return nil, err
	}
	return valueExtra, nil
}

// LoadValueWithExtraInto is LoadValueWithExtra with caller-owned result storage.
func (d *AugmentedDictionary) LoadValueWithExtraInto(key *Cell, valueExtra *Slice) error {
	if key == nil || key.BitsSize() != d.keySz {
		return fmt.Errorf("incorrect key size")
	}
	plain := Dictionary{
		keySz: d.keySz,
		root:  d.root,
		trace: d.trace,
	}
	return plain.LoadValueInto(key, valueExtra)
}

func (d *AugmentedDictionary) LoadValue(key *Cell) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueInto is LoadValue with caller-owned result storage.
func (d *AugmentedDictionary) LoadValueInto(key *Cell, value *Slice) error {
	var valueExtra Slice
	if err := d.LoadValueWithExtraInto(key, &valueExtra); err != nil {
		return err
	}
	return d.decomposeValueExtraInto(&valueExtra, value, nil)
}

func (d *AugmentedDictionary) LoadValueExtra(key *Cell) (*Slice, *Slice, error) {
	value := new(Slice)
	extra := new(Slice)
	if err := d.LoadValueExtraInto(key, value, extra); err != nil {
		return nil, nil, err
	}
	return value, extra, nil
}

// LoadValueExtraInto is LoadValueExtra with caller-owned result storage. The
// value and extra destinations must be distinct.
func (d *AugmentedDictionary) LoadValueExtraInto(key *Cell, value, extra *Slice) error {
	var valueExtra Slice
	if err := d.LoadValueWithExtraInto(key, &valueExtra); err != nil {
		return err
	}
	return d.decomposeValueExtraInto(&valueExtra, value, extra)
}

func (d *AugmentedDictionary) LoadValueExtraByIntKey(key *big.Int) (*Slice, *Slice, error) {
	value := new(Slice)
	extra := new(Slice)
	if err := d.LoadValueExtraByIntKeyInto(key, value, extra); err != nil {
		return nil, nil, err
	}
	return value, extra, nil
}

// LoadValueExtraByIntKeyInto is LoadValueExtraByIntKey with caller-owned
// result storage. The value and extra destinations must be distinct.
func (d *AugmentedDictionary) LoadValueExtraByIntKeyInto(key *big.Int, value, extra *Slice) error {
	var valueExtra Slice
	if err := d.LoadValueWithExtraByIntKeyInto(key, &valueExtra); err != nil {
		return err
	}
	return d.decomposeValueExtraInto(&valueExtra, value, extra)
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

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

func (d *AugmentedDictionary) setBuilderWithModeSlice(keySlice *Slice, value *Builder, mode DictSetMode) (bool, error) {
	var state augmentedMutationState
	newRoot, rootExtra, changed, err := d.set(d.root, keySlice, d.keySz, value, mode, &state)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	rootExtraCell, err := rootExtra.ToCell()
	if err != nil {
		return false, err
	}
	if err = d.setRootWithExtra(newRoot, rootExtraCell); err != nil {
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
	var extra Builder
	return d.aug.EmptyExtra(&extra)
}

func (d *AugmentedDictionary) ensureRootExtra() (*Cell, error) {
	if d.rootExtra != nil {
		return d.rootExtra, nil
	}
	if d.aug == nil {
		return nil, fmt.Errorf("augmentation is nil")
	}
	if d.root == nil {
		var extra Builder
		if err := d.aug.EmptyExtra(&extra); err != nil {
			return nil, err
		}
		d.rootExtra = extra.EndCell()
		return d.rootExtra, nil
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
	value := new(Slice)
	extra := new(Slice)
	if err := d.decomposeValueExtraInto(valueExtra, value, extra); err != nil {
		return nil, nil, err
	}
	return value, extra, nil
}

func (d *AugmentedDictionary) decomposeValueExtraInto(valueExtra, value, extra *Slice) error {
	if value == extra && extra != nil {
		return fmt.Errorf("value and extra destinations must be distinct")
	}

	original := *valueExtra
	valueBefore := *value
	*value = original
	if err := d.skipExtra(value); err != nil {
		// Use the caller-owned destination as parsing scratch so the
		// interface call does not force a temporary Slice onto the heap.
		// Roll it back before returning to keep Into error paths atomic.
		*value = valueBefore
		return err
	}

	if extra != nil {
		*extra = original
		extra.bitEnd = value.bitStart
		extra.refEnd = value.refStart
	}
	return nil
}

func augmentedNodeExtraView(node fixedDictNode, remaining uint, skipExtra AugmentedExtraSkipper) (Slice, error) {
	extra := node.loader
	if !node.isLeaf(remaining) {
		if err := extra.SkipBitsAndRefs(0, 2); err != nil {
			return Slice{}, err
		}
	}

	after := extra
	if err := skipExtra(&after); err != nil {
		return Slice{}, err
	}
	extra.bitEnd = after.bitStart
	extra.refEnd = after.refStart
	return extra, nil
}

func extractAugmentedNodeExtraView(c *Cell, keySz uint, skipExtra AugmentedExtraSkipper) (Slice, error) {
	node, err := parseFixedDictNode(c, keySz)
	if err != nil {
		return Slice{}, fmt.Errorf("failed to load augmented dict node: %w", err)
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return Slice{}, err
	}
	return augmentedNodeExtraView(node, keySz, skipExtra)
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

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.lookupDeleteWithExtraSlice(&keySlice)
}

func (d *AugmentedDictionary) lookupDeleteWithExtraSlice(keySlice *Slice) (*Slice, bool, error) {
	var state augmentedMutationState
	newRoot, rootExtra, removed, changed, err := d.delete(d.root, keySlice, d.keySz, &state)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return nil, false, nil
	}
	var rootExtraCell *Cell
	if newRoot != nil {
		rootExtraCell, err = rootExtra.ToCell()
		if err != nil {
			return nil, false, err
		}
	}
	if err = d.setRootWithExtra(newRoot, rootExtraCell); err != nil {
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
			var extra Builder
			if err := d.aug.EmptyExtra(&extra); err != nil {
				return err
			}
			rootExtra = extra.EndCell()
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

func (d *AugmentedDictionary) set(branch *Cell, pfx *Slice, keyOffset uint, value *Builder, mode DictSetMode, state *augmentedMutationState) (*Cell, Slice, bool, error) {
	if branch == nil {
		if mode == DictSetModeReplace {
			return nil, Slice{}, false, nil
		}
		leaf, leafExtra, err := d.storeLeafWithExtra(pfx, value, keyOffset, state)
		return leaf, leafExtra, err == nil, err
	}

	node, err := parseFixedDictNode(branch, keyOffset)
	if err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to load branch: %w", err)
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return nil, Slice{}, false, err
	}
	sz, kPart := node.labelLen, node.label

	bitsMatches, isNewRight, diverged, err := matchLabelView(kPart, sz, pfx)
	if err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to match key prefix: %w", err)
	}

	if !diverged {
		if pfx.BitsLeft() == 0 {
			if mode == DictSetModeAdd {
				return branch, Slice{}, false, nil
			}
			kPartView := kPart
			leaf, leafExtra, err := d.storeLeafWithExtra(&kPartView, value, keyOffset, state)
			return leaf, leafExtra, err == nil, err
		}

		refIdx := int(pfx.MustLoadUInt(1))
		ref, err := node.ref(refIdx)
		if err != nil {
			return nil, Slice{}, false, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
		}

		nextKeyOffset := keyOffset - (bitsMatches + 1)
		ref, refExtra, changed, err := d.set(ref, pfx, nextKeyOffset, value, mode, state)
		if err != nil {
			return nil, Slice{}, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
		}
		if !changed {
			return branch, Slice{}, false, nil
		}

		if ref == nil {
			return nil, Slice{}, false, fmt.Errorf("set produced nil child")
		}

		left, err := node.ref(0)
		if err != nil {
			return nil, Slice{}, false, err
		}
		right, err := node.ref(1)
		if err != nil {
			return nil, Slice{}, false, err
		}
		other, err := node.ref(refIdx ^ 1)
		if err != nil {
			return nil, Slice{}, false, err
		}
		otherExtra, err := extractAugmentedNodeExtraView(other, nextKeyOffset, d.aug.SkipExtra)
		if err != nil {
			return nil, Slice{}, false, err
		}
		leftExtra, rightExtra := otherExtra, otherExtra
		if refIdx == 0 {
			left = ref
			leftExtra = refExtra
		} else {
			right = ref
			rightExtra = refExtra
		}

		kPartView := kPart
		newBranch, branchExtra, err := d.storeForkWithExtraSlices(&kPartView, left, &leftExtra, right, &rightExtra, keyOffset, state)
		return newBranch, branchExtra, err == nil, err
	}

	if mode == DictSetModeReplace {
		return branch, Slice{}, false, nil
	}

	prefixLabel, labelRemainder, err := fixedDictNode{label: kPart, labelLen: sz}.splitLabel(bitsMatches)
	if err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to split old child label: %w", err)
	}

	oldChild := BeginCell()
	if err = storeDictLabel(oldChild, labelRemainder, keyOffset-(bitsMatches+1)); err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to store old child label: %w", err)
	}
	node.loader.ToBuilderInto(&state.extra)
	if err = oldChild.StoreBuilderUncheckedDepth(&state.extra); err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to store old child payload: %w", err)
	}
	oldExtra, err := augmentedNodeExtraView(node, keyOffset, d.aug.SkipExtra)
	if err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to extract old child extra: %w", err)
	}
	oldChildCell := oldChild.EndCell()

	newChild, newExtra, err := d.storeLeafWithExtra(pfx, value, keyOffset-(bitsMatches+1), state)
	if err != nil {
		return nil, Slice{}, false, fmt.Errorf("failed to store new child leaf: %w", err)
	}

	left, right := newChild, oldChildCell
	leftExtra, rightExtra := newExtra, oldExtra
	if isNewRight {
		left, right = right, left
		leftExtra, rightExtra = rightExtra, leftExtra
	}

	newBranch, branchExtra, err := d.storeForkWithExtraSlices(prefixLabel, left, &leftExtra, right, &rightExtra, keyOffset, state)
	return newBranch, branchExtra, err == nil, err
}

func (d *AugmentedDictionary) delete(branch *Cell, pfx *Slice, keyOffset uint, state *augmentedMutationState) (*Cell, Slice, *Slice, bool, error) {
	if branch == nil {
		return nil, Slice{}, nil, false, nil
	}

	node, err := parseFixedDictNode(branch, keyOffset)
	if err != nil {
		return nil, Slice{}, nil, false, fmt.Errorf("failed to load branch: %w", err)
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return nil, Slice{}, nil, false, err
	}
	sz, kPart := node.labelLen, node.label

	label := kPart
	bitsMatches, err := commonSlicePrefix(&label, pfx, sz)
	if err != nil {
		return nil, Slice{}, nil, false, fmt.Errorf("failed to match key prefix: %w", err)
	}
	if bitsMatches < sz {
		return branch, Slice{}, nil, false, nil
	}
	if err = pfx.SkipBits(sz); err != nil {
		return nil, Slice{}, nil, false, fmt.Errorf("failed to consume key prefix: %w", err)
	}

	if pfx.BitsLeft() == 0 {
		removed := node.loader
		return nil, Slice{}, &removed, true, nil
	}

	refIdx := int(pfx.MustLoadUInt(1))
	ref, err := node.ref(refIdx)
	if err != nil {
		return nil, Slice{}, nil, false, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
	}

	nextKeyOffset := keyOffset - (bitsMatches + 1)
	ref, refExtra, removed, changed, err := d.delete(ref, pfx, nextKeyOffset, state)
	if err != nil {
		return nil, Slice{}, nil, false, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
	}
	if !changed {
		return branch, Slice{}, nil, false, nil
	}

	if ref == nil {
		otherIdx := refIdx ^ 1
		otherRef, err := node.ref(otherIdx)
		if err != nil {
			return nil, Slice{}, nil, false, fmt.Errorf("failed to peek neighbour ref %d: %w", otherIdx, err)
		}

		otherNode, err := parseFixedDictNode(otherRef, nextKeyOffset)
		if err != nil {
			return nil, Slice{}, nil, false, fmt.Errorf("failed to load neighbour ref %d: %w", otherIdx, err)
		}
		if err = otherNode.rejectSpecial("augmented dict"); err != nil {
			return nil, Slice{}, nil, false, err
		}
		otherExtra, err := augmentedNodeExtraView(otherNode, nextKeyOffset, d.aug.SkipExtra)
		if err != nil {
			return nil, Slice{}, nil, false, fmt.Errorf("failed to extract neighbour extra: %w", err)
		}

		var mergedLabel Builder
		if err = mergedLabel.storeSliceFromSlice(&kPart, sz); err != nil {
			return nil, Slice{}, nil, false, fmt.Errorf("failed to append base label: %w", err)
		}
		if err = mergedLabel.StoreUInt(uint64(otherIdx), 1); err != nil {
			return nil, Slice{}, nil, false, fmt.Errorf("failed to append neighbour edge bit: %w", err)
		}
		otherLabel := otherNode.labelSlice()
		if err = mergedLabel.storeSliceFromSlice(&otherLabel, otherNode.labelLen); err != nil {
			return nil, Slice{}, nil, false, fmt.Errorf("failed to append neighbour label: %w", err)
		}
		otherNode.loader.ToBuilderInto(&state.extra)
		merged, err := d.storeNode(builderSliceView(&mergedLabel), &state.extra, keyOffset)
		if err != nil {
			return nil, Slice{}, nil, false, err
		}
		return merged, otherExtra, removed, true, nil
	}

	left, err := node.ref(0)
	if err != nil {
		return nil, Slice{}, nil, false, err
	}
	right, err := node.ref(1)
	if err != nil {
		return nil, Slice{}, nil, false, err
	}
	otherRef, err := node.ref(refIdx ^ 1)
	if err != nil {
		return nil, Slice{}, nil, false, err
	}
	otherExtra, err := extractAugmentedNodeExtraView(otherRef, nextKeyOffset, d.aug.SkipExtra)
	if err != nil {
		return nil, Slice{}, nil, false, err
	}
	leftExtra, rightExtra := otherExtra, otherExtra
	if refIdx == 0 {
		left = ref
		leftExtra = refExtra
	} else {
		right = ref
		rightExtra = refExtra
	}

	newBranch, branchExtra, err := d.storeForkWithExtraSlices(&kPart, left, &leftExtra, right, &rightExtra, keyOffset, state)
	if err != nil {
		return nil, Slice{}, nil, false, err
	}
	return newBranch, branchExtra, removed, true, nil
}

func (d *AugmentedDictionary) storeLeafWithExtra(keyPfx *Slice, value *Builder, keyOffset uint, state *augmentedMutationState) (*Cell, Slice, error) {
	if value == nil {
		return nil, Slice{}, nil
	}

	refs := value.rawRefs()
	state.valueCell = Cell{data: value.data[:value.usedBytes()], bitsSz: uint16(value.bitsSz)}
	state.valueCell.setRefs(refs)
	// The shell stands in for the cell EndCell would have produced, so it needs
	// that cell's level mask: without it a pruned-branch ref makes ToCell/BaseCell
	// on the borrowed value fail boundary validation. Refs at level 0 give 0.
	state.valueCell.setLevelMask(ordinaryLevelMask(refs))
	state.value = Slice{
		cell:              &state.valueCell,
		bitEnd:            state.valueCell.bitsSz,
		refEnd:            value.refsNum,
		forceCopyOnToCell: true,
	}
	state.extra = Builder{}
	if err := d.aug.LeafExtra(&state.value, &state.extra); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to compute leaf extra: %w", err)
	}

	state.node = Builder{trace: d.trace}
	b := &state.node
	if err := storeDictLabel(b, keyPfx, keyOffset); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store label: %w", err)
	}
	extraBitStart, extraRefStart := b.bitsSz, b.refsNum
	if err := b.StoreBuilder(&state.extra); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store leaf extra: %w", err)
	}
	extraBitEnd, extraRefEnd := b.bitsSz, b.refsNum
	if err := b.StoreBuilder(value); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store value: %w", err)
	}
	leaf := b.EndCell()
	return leaf, Slice{
		cell:     leaf,
		bitStart: uint16(extraBitStart),
		bitEnd:   uint16(extraBitEnd),
		refStart: extraRefStart,
		refEnd:   extraRefEnd,
	}, nil
}

func (d *AugmentedDictionary) storeLeaf(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	var state augmentedMutationState
	leaf, _, err := d.storeLeafWithExtra(keyPfx, value, keyOffset, &state)
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

	var leftExtraSlice, rightExtraSlice Slice
	if err := leftExtra.BeginParseInto(&leftExtraSlice); err != nil {
		return nil, nil, fmt.Errorf("failed to load left extra: %w", err)
	}
	if err := rightExtra.BeginParseInto(&rightExtraSlice); err != nil {
		return nil, nil, fmt.Errorf("failed to load right extra: %w", err)
	}
	var state augmentedMutationState
	fork, extraView, err := d.storeForkWithExtraSlices(label, left, &leftExtraSlice, right, &rightExtraSlice, keyOffset, &state)
	if err != nil {
		return nil, nil, err
	}
	extraCell, err := extraView.ToCell()
	if err != nil {
		return nil, nil, err
	}
	return fork, extraCell, nil
}

func (d *AugmentedDictionary) storeForkWithExtraSlices(label *Slice, left *Cell, leftExtra *Slice, right *Cell, rightExtra *Slice, keyOffset uint, state *augmentedMutationState) (*Cell, Slice, error) {
	state.leftExtra = *leftExtra
	state.rightExtra = *rightExtra
	state.extra = Builder{}
	if err := d.aug.CombineExtra(&state.leftExtra, &state.rightExtra, &state.extra); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to compute fork extra: %w", err)
	}

	state.node = Builder{trace: d.trace}
	b := &state.node
	if err := storeDictLabel(b, label, keyOffset); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store label: %w", err)
	}
	if err := b.StoreRef(left); err != nil {
		return nil, Slice{}, err
	}
	if err := b.StoreRef(right); err != nil {
		return nil, Slice{}, err
	}
	extraBitStart, extraRefStart := b.bitsSz, b.refsNum
	if err := b.StoreBuilder(&state.extra); err != nil {
		return nil, Slice{}, fmt.Errorf("failed to store fork extra: %w", err)
	}
	fork := b.EndCell()
	return fork, Slice{
		cell:     fork,
		bitStart: uint16(extraBitStart),
		bitEnd:   uint16(b.bitsSz),
		refStart: extraRefStart,
		refEnd:   b.refsNum,
	}, nil
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
	return storeDictNodeTraced(label, payload, keyOffset, d.trace)
}

func validateAugmentedDictRoot(root *Cell, keySz uint, aug Augmentation) error {
	if root == nil {
		return validateDictKeySize(keySz)
	}
	if err := validateDictKeySize(keySz); err != nil {
		return err
	}
	root = root.WithTrace(nil)

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

	var extra Builder
	err := aug.EmptyExtra(&extra)
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

	loader, err := c.BeginParse()
	if err != nil {
		return fmt.Errorf("failed to load augmented dict node: %w", err)
	}

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
		var extra Builder
		if err := aug.EmptyExtra(&extra); err != nil {
			return nil, err
		}
		return extra.EndCell(), nil
	}

	if c.IsSpecial() {
		if c.GetType() == PrunedCellType {
			return nil, ErrAugmentationSemanticsUnavailable
		}
		return nil, fmt.Errorf("augmented dict has unsupported special cell in tree structure")
	}

	loader, err := c.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dict node: %w", err)
	}

	labelLen, _, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return nil, fmt.Errorf("failed to parse augmented dict label: %w", err)
	}

	if labelLen == keySz {
		valueExtra := *loader
		storedExtra, err := captureConsumedPrefix(&valueExtra, aug.SkipExtra)
		if err != nil {
			return nil, fmt.Errorf("invalid augmented dict leaf extra: %w", err)
		}

		var computedExtra Builder
		if err := aug.LeafExtra(&valueExtra, &computedExtra); err != nil {
			return nil, err
		}
		if !computedExtra.EqualsCell(storedExtra) {
			return nil, fmt.Errorf("augmented dict leaf extra mismatch")
		}
		return computedExtra.EndCell(), nil
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

	var leftExtraSlice, rightExtraSlice Slice
	if err := leftExtra.BeginParseInto(&leftExtraSlice); err != nil {
		return nil, fmt.Errorf("failed to load left extra: %w", err)
	}
	if err := rightExtra.BeginParseInto(&rightExtraSlice); err != nil {
		return nil, fmt.Errorf("failed to load right extra: %w", err)
	}

	var computedExtra Builder
	if err := aug.CombineExtra(&leftExtraSlice, &rightExtraSlice, &computedExtra); err != nil {
		return nil, err
	}
	if !computedExtra.EqualsCell(storedExtra) {
		return nil, fmt.Errorf("augmented dict fork extra mismatch")
	}
	return computedExtra.EndCell(), nil
}

func extractAugmentedNodeExtra(c *Cell, keySz uint, skipExtra AugmentedExtraSkipper) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("augmented dict branch is nil")
	}

	loader, err := c.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to load augmented dict node: %w", err)
	}

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
		if err := loader.loadSliceInto(b.data[:], consumedBits, false); err != nil {
			return nil, err
		}
		b.bitsSz = consumedBits
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
	if a == b {
		return true
	}
	if a.bitsSz != b.bitsSz || a.refsCount() != b.refsCount() {
		return false
	}

	fullBytes := int(a.bitsSz / 8)
	if !bytes.Equal(a.data[:fullBytes], b.data[:fullBytes]) {
		return false
	}
	if rem := a.bitsSz % 8; rem != 0 {
		mask := byte(0xFF << (8 - rem))
		if a.data[fullBytes]&mask != b.data[fullBytes]&mask {
			return false
		}
	}

	for i := 0; i < a.refsCount(); i++ {
		if a.ref(i).HashKey() != b.ref(i).HashKey() {
			return false
		}
	}
	return true
}
