package cell

import (
	"errors"
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

// AsPrefixDictWithTrace creates a traced prefix-dictionary view without
// copying the root cell.
func (c *Cell) AsPrefixDictWithTrace(keySz uint, trace *Trace) *PrefixDictionary {
	return &PrefixDictionary{
		keySz: keySz,
		root:  c,
		trace: trace,
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

func (d *PrefixDictionary) tracedRoot() *Cell {
	return d.root.withTraceCombined(d.trace)
}

func (d *PrefixDictionary) setRoot(root *Cell) {
	d.root = root
}

func (d *PrefixDictionary) IsEmpty() bool {
	return d == nil || d.root == nil
}

func (d *PrefixDictionary) LookupPrefix(key *Cell) (*Slice, uint, error) {
	value := new(Slice)
	matched, err := d.lookupPrefixInto(key, value)
	if errors.Is(err, ErrNoSuchKeyInDict) {
		return nil, matched, nil
	}
	if err != nil {
		return nil, matched, err
	}
	return value, matched, nil
}

// LookupPrefixInto finds the longest stored prefix of key and writes its value
// into caller-owned storage. It returns ErrNoSuchKeyInDict when no stored
// prefix matches. matched reports how many key bits matched even on that miss.
func (d *PrefixDictionary) LookupPrefixInto(key *Cell, value *Slice) (matched uint, err error) {
	return d.lookupPrefixInto(key, value)
}

func (d *PrefixDictionary) lookupPrefixInto(key *Cell, value *Slice) (matched uint, err error) {
	if d == nil {
		return 0, ErrNoSuchKeyInDict
	}
	if key == nil {
		return 0, fmt.Errorf("key is nil")
	}
	if d.root == nil {
		return 0, ErrNoSuchKeyInDict
	}

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return 0, fmt.Errorf("failed to load key: %w", err)
	}
	return d.lookupPrefixSliceInto(&keySlice, value)
}

// LookupPrefixBySliceInto finds the longest stored prefix using at most the
// first key-size remaining bits of key without materializing a key cell.
func (d *PrefixDictionary) LookupPrefixBySliceInto(key *Slice, value *Slice) (matched uint, err error) {
	if d == nil {
		return 0, ErrNoSuchKeyInDict
	}
	if key == nil {
		return 0, fmt.Errorf("key is nil")
	}
	keySlice := prefixDictKeySlice(key, d.keySz)
	return d.lookupPrefixSliceInto(&keySlice, value)
}

// prefixDictKeySlice views at most the first bits remaining bits of key as a
// prefix-dict key: references and the trace are dropped so the descent cannot
// consume them, and anything past bits is clamped away. Prefix keys may be
// shorter than the key size; callers that must reject a longer one check
// BitsLeft themselves before calling, so the clamp never fires for them.
func prefixDictKeySlice(key *Slice, bits uint) Slice {
	keySlice := *key
	if keySlice.BitsLeft() > bits {
		keySlice.bitEnd = keySlice.bitStart + uint16(bits)
	}
	keySlice.refEnd = keySlice.refStart
	keySlice.trace = nil
	return keySlice
}

func (d *PrefixDictionary) lookupPrefixSliceInto(keySlice *Slice, value *Slice) (matched uint, err error) {
	if d.root == nil {
		return 0, ErrNoSuchKeyInDict
	}

	branch := d.root
	branchTrace := CombineTraces(branch.Trace(), d.trace)
	remaining := d.keySz

	// The descent and result both use caller-owned Slice values.
	var branchSlice Slice

	for {
		if err := branch.BeginParseIntoWithTrace(&branchSlice, branchTrace); err != nil {
			return matched, fmt.Errorf("failed to load prefix dict branch: %w", err)
		}
		if branchSlice.cell.IsSpecial() {
			resolved, err := resolveDictNodeCell(branchSlice.cell, branchTrace, nil, "prefix dict")
			if err != nil {
				return matched, err
			}
			branchTrace = CombineTraces(resolved.Trace(), branchTrace)
			if err = resolved.BeginParseIntoWithTrace(&branchSlice, nil); err != nil {
				return matched, fmt.Errorf("failed to load prefix dict branch: %w", err)
			}
			branchSlice.SetTrace(branchTrace)
		}

		labelLen, commonPrefix, err := matchLabelPrefix(remaining, &branchSlice, keySlice)
		if err != nil {
			return matched, err
		}

		if commonPrefix < labelLen {
			return matched + commonPrefix, ErrNoSuchKeyInDict
		}

		matched += labelLen
		remaining -= labelLen

		isFork, err := branchSlice.LoadBoolBit()
		if err != nil {
			return matched, fmt.Errorf("no node constructor in a prefix code dictionary")
		}

		if !isFork {
			*value = branchSlice
			return matched, nil
		}

		if remaining == 0 {
			return matched, fmt.Errorf("a fork node in a prefix code dictionary with zero remaining key length")
		}
		if branchSlice.BitsLeft() != 0 || branchSlice.RefsNum() != 2 {
			return matched, fmt.Errorf("invalid fork node in a prefix code dictionary")
		}
		if keySlice.BitsLeft() == 0 {
			return matched, ErrNoSuchKeyInDict
		}

		idx, err := keySlice.LoadUInt(1)
		if err != nil {
			return matched, err
		}

		matched++
		remaining--

		next, nextTrace, err := branchSlice.refAndTraceAt(int(idx))
		if err != nil {
			return matched, err
		}
		branch = next
		branchTrace = nextTrace
	}
}

func (d *PrefixDictionary) LoadValue(key *Cell) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueInto is LoadValue with caller-owned result storage.
func (d *PrefixDictionary) LoadValueInto(key *Cell, value *Slice) error {
	if key == nil {
		return fmt.Errorf("key is nil")
	}

	var found Slice
	matched, err := d.lookupPrefixInto(key, &found)
	if err != nil {
		return err
	}
	if matched != key.BitsSize() {
		return ErrNoSuchKeyInDict
	}
	*value = found
	return nil
}

// LoadValueByIntKey loads a full-width prefix-dictionary key without
// finalizing and hashing an intermediate key cell.
func (d *PrefixDictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	value := new(Slice)
	if err := d.LoadValueByIntKeyInto(key, value); err != nil {
		return nil, err
	}
	return value, nil
}

// LoadValueByIntKeyInto is LoadValueByIntKey with caller-owned result storage.
func (d *PrefixDictionary) LoadValueByIntKeyInto(key *big.Int, value *Slice) error {
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	var foundValue Slice
	found, err := d.lookupPrefixSliceInto(&keySlice, &foundValue)
	if err != nil {
		return err
	}
	if found != d.keySz {
		return ErrNoSuchKeyInDict
	}
	*value = foundValue
	return nil
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
	if value == nil {
		return d.DeleteIntKey(key)
	}
	var builder Builder
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	_, err := d.setBuilderWithModeSlice(&keySlice, value.ToBuilder(), DictSetModeSet)
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
	if key == nil || key.BitsSize() > d.keySz {
		return false, fmt.Errorf("invalid key size")
	}
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return false, fmt.Errorf("failed to load key: %w", err)
	}
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

// SetBuilderBySliceKeyWithMode stores a prefix key directly from the remaining
// bits of key without materializing a key cell.
func (d *PrefixDictionary) SetBuilderBySliceKeyWithMode(key *Slice, value *Builder, mode DictSetMode) (bool, error) {
	if d == nil {
		return false, fmt.Errorf("prefix dict is nil")
	}
	if key == nil || key.BitsLeft() > d.keySz {
		return false, fmt.Errorf("invalid key size")
	}
	if value == nil {
		return false, fmt.Errorf("value is nil")
	}

	keySlice := prefixDictKeySlice(key, d.keySz)
	return d.setBuilderWithModeSlice(&keySlice, value, mode)
}

func (d *PrefixDictionary) setBuilderWithModeSlice(keySlice *Slice, value *Builder, mode DictSetMode) (bool, error) {

	newRoot, changed, err := d.set(d.tracedRoot(), keySlice, d.keySz, value, mode)
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

	var keySlice Slice
	if err := key.BeginParseInto(&keySlice); err != nil {
		return nil, fmt.Errorf("failed to load key: %w", err)
	}
	return d.loadValueAndDeleteBySliceKey(&keySlice)
}

// LoadValueAndDeleteBySliceKey removes a prefix key directly from the
// remaining key bits without materializing a key cell.
func (d *PrefixDictionary) LoadValueAndDeleteBySliceKey(key *Slice) (*Slice, error) {
	if d == nil {
		return nil, ErrNoSuchKeyInDict
	}
	if key == nil || key.BitsLeft() > d.keySz {
		return nil, fmt.Errorf("incorrect key size")
	}
	keySlice := prefixDictKeySlice(key, d.keySz)
	return d.loadValueAndDeleteBySliceKey(&keySlice)
}

func (d *PrefixDictionary) loadValueAndDeleteBySliceKey(keySlice *Slice) (*Slice, error) {

	value, newRoot, changed, err := d.lookupDelete(d.tracedRoot(), keySlice, d.keySz)
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
	initIntKeyBuilder(key, d.keySz, &builder)
	cell := Cell{data: builder.data[:builder.usedBytes()], bitsSz: uint16(builder.bitsSz)}
	keySlice := Slice{cell: &cell, bitEnd: cell.bitsSz}
	_, err := d.loadValueAndDeleteBySliceKey(&keySlice)
	return err
}

func (d *PrefixDictionary) MustToCell() *Cell {
	return d.AsCell()
}

func (d *PrefixDictionary) AsCell() *Cell {
	if d == nil {
		return nil
	}
	return d.tracedRoot()
}

func (d *PrefixDictionary) ToCell() (*Cell, error) {
	if d == nil {
		return nil, nil
	}
	return d.tracedRoot(), nil
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

	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return nil, false, fmt.Errorf("failed to load label: %w", err)
	}
	if err = node.resolveIfSpecial(remaining, branch.Trace(), nil); err != nil {
		return nil, false, err
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

		var oldPayload Builder
		node.loader.ToBuilderInto(&oldPayload)
		oldChild, err := d.storePrefixNode(labelRemainder, &oldPayload, remaining-(bitsMatches+1))
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

	if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
		return nil, false, fmt.Errorf("invalid fork node in a prefix code dictionary")
	}
	// A fork whose label consumes the whole key (or a key that ran out) means
	// the key is a proper prefix of longer entries: the set returns the tree
	// unchanged instead of failing.
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

	canonical, decided := node.canonicalLabelFast(remaining)
	if !decided {
		canonical, err = node.hasCanonicalLabel(remaining)
		if err != nil {
			return nil, false, err
		}
	}
	if canonical {
		return node.cloneWithRef(int(idx), child, d.trace)
	}
	return d.rebuildNonCanonicalPrefixForkWithRef(&node, int(idx), child, remaining)
}

func (d *PrefixDictionary) lookupDelete(branch *Cell, key *Slice, remaining uint) (*Slice, *Cell, bool, error) {
	if key.BitsLeft() > remaining {
		return nil, nil, false, fmt.Errorf("incorrect key size")
	}
	if branch == nil {
		return nil, nil, false, nil
	}

	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load label: %w", err)
	}
	if err = node.resolveIfSpecial(remaining, branch.Trace(), nil); err != nil {
		return nil, nil, false, err
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

	if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
		return nil, nil, false, fmt.Errorf("invalid fork node in a prefix code dictionary")
	}
	// a fork consuming the whole key is "not found", not an error
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
		canonical, decided := node.canonicalLabelFast(remaining)
		if !decided {
			canonical, err = node.hasCanonicalLabel(remaining)
			if err != nil {
				return nil, nil, false, err
			}
		}
		if canonical {
			cloned, changed, err := node.cloneWithRef(int(idx), newChild, d.trace)
			return oldValue, cloned, changed, err
		}
		cloned, changed, err := d.rebuildNonCanonicalPrefixForkWithRef(&node, int(idx), newChild, remaining)
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

	childRemaining := remaining - (node.labelLen + 1)
	survivorNode, err := parseFixedDictNodeWithTrace(survivor, childRemaining, survivor.Trace())
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to load survivor branch: %w", err)
	}
	if err = survivorNode.resolveIfSpecial(childRemaining, survivor.Trace(), nil); err != nil {
		return nil, nil, false, err
	}
	if err = survivorNode.rejectSpecial("prefix dict"); err != nil {
		return nil, nil, false, err
	}

	var mergedLabel Builder
	baseLabel := node.labelSlice()
	if err = mergedLabel.storeSliceFromSlice(&baseLabel, node.labelLen); err != nil {
		return nil, nil, false, fmt.Errorf("failed to append survivor base label: %w", err)
	}
	if err = mergedLabel.StoreUInt(uint64(survivorBit), 1); err != nil {
		return nil, nil, false, fmt.Errorf("failed to append survivor edge bit: %w", err)
	}
	survivorLabel := survivorNode.labelSlice()
	if err = mergedLabel.storeSliceFromSlice(&survivorLabel, survivorNode.labelLen); err != nil {
		return nil, nil, false, fmt.Errorf("failed to append survivor label: %w", err)
	}

	var survivorPayload Builder
	survivorNode.loader.ToBuilderInto(&survivorPayload)
	merged, err := d.storePrefixNode(builderSliceView(&mergedLabel), &survivorPayload, remaining)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to merge prefix edges: %w", err)
	}
	return oldValue, merged, true, nil
}

func (d *PrefixDictionary) rebuildNonCanonicalPrefixForkWithRef(node *fixedDictNode, i int, ref *Cell, remaining uint) (*Cell, bool, error) {
	left, err := node.ref(0)
	if err != nil {
		return nil, false, err
	}
	right, err := node.ref(1)
	if err != nil {
		return nil, false, err
	}
	if i == 0 {
		left = ref
	} else {
		right = ref
	}

	label := node.labelSlice()
	rebuilt, err := d.storePrefixFork(&label, left, right, remaining)
	return rebuilt, err == nil, err
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
