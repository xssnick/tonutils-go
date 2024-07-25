package cell

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
)

type Dictionary struct {
	keySz uint

	root *Cell
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

	return &Dictionary{
		keySz: keySz,
		root:  root,
	}, nil
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
		return &Dictionary{
			keySz: keySz,
		}, nil
	}

	return cl.ToDict(keySz)
}

func (d *Dictionary) GetKeySize() uint {
	return d.keySz
}

func (d *Dictionary) Copy() *Dictionary {
	return &Dictionary{
		keySz: d.keySz,
		root:  d.root,
	}
}

func (d *Dictionary) SetIntKey(key *big.Int, value *Cell) error {
	return d.Set(BeginCell().MustStoreBigInt(key, d.keySz).EndCell(), value)
}

func (d *Dictionary) storeLeaf(keyPfx *Slice, value *Builder, keyOffset uint) (*Cell, error) {
	// process last branch
	if value == nil {
		return nil, nil
	}

	b := BeginCell()
	if err := d.storeLabel(b, keyPfx, keyOffset); err != nil {
		return nil, fmt.Errorf("failed to store label: %w", err)
	}

	if err := b.StoreBuilder(value); err != nil {
		return nil, fmt.Errorf("failed to store value: %w", err)
	}

	return b.EndCell(), nil
}

func (d *Dictionary) Set(key, value *Cell) error {
	if key.BitsSize() != d.keySz {
		return fmt.Errorf("invalid key size")
	}

	var val *Builder
	if value != nil {
		val = value.ToBuilder()
	}

	var dive func(branch *Cell, pfx *Slice, keyOffset uint) (*Cell, error)
	dive = func(branch *Cell, pfx *Slice, keyOffset uint) (*Cell, error) {
		// branch is exist, we need to check it
		s := branch.BeginParse()
		sz, kPart, err := loadLabel(keyOffset, s, BeginCell())
		if err != nil {
			return nil, fmt.Errorf("failed to load label: %w", err)
		}

		isNewRight, matches := false, true
		kPartSlice := kPart.ToSlice()
		var bitsMatches uint
		for bitsMatches = 0; bitsMatches < sz; bitsMatches++ {
			vCurr, err := kPartSlice.LoadUInt(1)
			if err != nil {
				return nil, fmt.Errorf("failed to load current key bit: %w", err)
			}

			vNew, err := pfx.LoadUInt(1)
			if err != nil {
				return nil, fmt.Errorf("failed to load new key bit: %w", err)
			}

			if vCurr != vNew {
				isNewRight = vNew != 0
				matches = false
				break
			}
		}

		if matches {
			if pfx.BitsLeft() == 0 {
				// label is same with our new key, we just need to change value
				return d.storeLeaf(kPart.ToSlice(), val, keyOffset)
			}

			// full label is matches part of our key, we need to go deeper
			refIdx := int(pfx.MustLoadUInt(1))
			ref, err := branch.PeekRef(refIdx)
			if err != nil {
				return nil, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
			}

			ref, err = dive(ref, pfx, keyOffset-(bitsMatches+1))
			if err != nil {
				return nil, fmt.Errorf("failed to dive into %d ref of branch: %w", refIdx, err)
			}

			if ref == nil {
				refIdx = refIdx ^ 1
				nRef, err := branch.PeekRef(refIdx)
				if err != nil {
					return nil, fmt.Errorf("failed to peek neighbour ref %d: %w", refIdx, err)
				}

				slc := nRef.BeginParse()
				_, k2Part, err := loadLabel(keyOffset-(bitsMatches+1), slc, BeginCell())
				if err != nil {
					return nil, fmt.Errorf("failed to load neighbour label: %w", err)
				}

				if err = kPart.StoreUInt(uint64(refIdx), 1); err != nil {
					return nil, fmt.Errorf("failed to store neighbour label part bit: %w", err)
				}

				if err = kPart.StoreBuilder(k2Part); err != nil {
					return nil, fmt.Errorf("failed to store neighbour label part: %w", err)
				}

				// deleted, store neighbour leaf instead of branch
				return d.storeLeaf(kPart.ToSlice(), slc.ToBuilder(), keyOffset)
			}

			b := branch.copy()
			b.refs[refIdx] = ref

			// recalculate hashes after direct modification
			b.calculateHashes()
			return b, nil
		}

		if value == nil {
			// key is not exists, and want to delete, do nothing
			return branch, nil
		}

		b := BeginCell()
		// label is not matches our key, we need to split it
		nkPart := kPart.ToSlice().MustLoadSlice(bitsMatches)
		if err = d.storeLabel(b, BeginCell().MustStoreSlice(nkPart, bitsMatches).ToSlice(), keyOffset); err != nil {
			return nil, fmt.Errorf("failed to store middle label: %w", err)
		}

		b1 := BeginCell()
		if err = d.storeLabel(b1, kPartSlice, keyOffset-(bitsMatches+1)); err != nil {
			return nil, fmt.Errorf("failed to store middle left label: %w", err)
		}
		b1.MustStoreBuilder(s.ToBuilder())

		dRef, err := d.storeLeaf(pfx, val, keyOffset-(bitsMatches+1))
		if err != nil {
			return nil, fmt.Errorf("failed to dive into right ref of new branch: %w", err)
		}

		// place refs according to last not matched bit, it is part of the key
		if isNewRight {
			b.refs = append(b.refs, b1.EndCell(), dRef)
		} else {
			b.refs = append(b.refs, dRef, b1.EndCell())
		}

		return b.EndCell(), nil
	}

	var err error
	var newRoot *Cell
	if d.root == nil {
		newRoot, err = d.storeLeaf(key.BeginParse(), val, d.keySz)
	} else {
		newRoot, err = dive(d.root, key.BeginParse(), d.keySz)
	}

	if err != nil {
		return fmt.Errorf("failed to set value in dict, err: %w", err)
	}

	d.root = newRoot
	return nil
}

func (d *Dictionary) Delete(key *Cell) error {
	return d.Set(key, nil)
}

func (d *Dictionary) DeleteIntKey(key *big.Int) error {
	return d.SetIntKey(key, nil)
}

// Deprecated: use LoadValueByIntKey
func (d *Dictionary) GetByIntKey(key *big.Int) *Cell {
	return d.Get(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

// LoadValueByIntKey - same as LoadValue, but constructs cell key from int
func (d *Dictionary) LoadValueByIntKey(key *big.Int) (*Slice, error) {
	return d.LoadValue(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

// LoadValue - searches key in the underline dict cell and returns its value
//
//	If key is not found ErrNoSuchKeyInDict will be returned
func (d *Dictionary) LoadValue(key *Cell) (*Slice, error) {
	res, _, err := d.LoadValueWithProof(key, nil)
	return res, err
}

// LoadValueWithProof - searches key in the underline dict cell, constructs proof path and returns leaf
//
//	If key is not found ErrNoSuchKeyInDict will be returned,
//	and path with proof of non-existing key will be attached to skeleton (if passed)
func (d *Dictionary) LoadValueWithProof(key *Cell, skeleton *ProofSkeleton) (*Slice, *ProofSkeleton, error) {
	if key.BitsSize() != d.keySz {
		return nil, nil, fmt.Errorf("incorrect key size")
	}
	return d.findKey(d.root, key, skeleton)
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

// Deprecated: use IsEmpty or LoadAll and then len
func (d *Dictionary) Size() int {
	if d == nil {
		return 0
	}
	return len(d.All())
}

func (d *Dictionary) IsEmpty() bool {
	return d == nil || d.root == nil || (d.root.BitsSize() == 0 && d.root.RefsNum() == 0)
}

// Deprecated: use LoadAll, dict was reimplemented, so it will be parsed during this call, and it can return error now.
func (d *Dictionary) All() []*HashmapKV {
	list, _ := d.LoadAll()

	old := make([]*HashmapKV, len(list))
	for i := 0; i < len(list); i++ {
		old[i] = &HashmapKV{
			Key:   list[i].Key.MustToCell(),
			Value: list[i].Value.MustToCell(),
		}
	}

	return old
}

func (d *Dictionary) LoadAll() ([]DictKV, error) {
	if d.root == nil {
		return []DictKV{}, nil
	}
	return d.mapInner(d.keySz, d.keySz, d.root.BeginParse(), BeginCell())
}

func (d *Dictionary) mapInner(keySz, leftKeySz uint, loader *Slice, keyPrefix *Builder) ([]DictKV, error) {
	var err error
	var sz uint

	sz, keyPrefix, err = loadLabel(leftKeySz, loader, keyPrefix)
	if err != nil {
		return nil, err
	}

	// until key size is not equals we go deeper
	if keyPrefix.BitsUsed() < keySz {
		// 0 bit branch
		left, err := loader.LoadRef()
		if err != nil {
			return nil, err
		}

		keysL, err := d.mapInner(keySz, leftKeySz-(1+sz), left, keyPrefix.Copy().MustStoreUInt(0, 1))
		if err != nil {
			return nil, err
		}

		// 1 bit branch
		right, err := loader.LoadRef()
		if err != nil {
			return nil, err
		}
		keysR, err := d.mapInner(keySz, leftKeySz-(1+sz), right, keyPrefix.Copy().MustStoreUInt(1, 1))
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

func (d *Dictionary) findKey(branch *Cell, lookupKey *Cell, at *ProofSkeleton) (*Slice, *ProofSkeleton, error) {
	if branch == nil {
		// empty dict
		return nil, nil, ErrNoSuchKeyInDict
	}

	var sk, root *ProofSkeleton
	if at != nil {
		root = CreateProofSkeleton()
		sk = root
	}
	lKey := lookupKey.BeginParse()

	// until key size is not equals we go deeper
	for {
		branchSlice := branch.BeginParse()
		sz, keyPrefix, err := loadLabel(lKey.BitsLeft(), branchSlice, BeginCell())
		if err != nil {
			return nil, nil, err
		}

		loadedPfx, err := keyPrefix.ToSlice().LoadSlice(sz)
		if err != nil {
			return nil, nil, err
		}

		pfx, err := lKey.LoadSlice(sz)
		if err != nil {
			return nil, nil, err
		}

		if !bytes.Equal(loadedPfx, pfx) {
			if sk != nil {
				at.Merge(root)
			}
			return nil, nil, ErrNoSuchKeyInDict
		}

		if lKey.BitsLeft() == 0 {
			if sk != nil {
				at.Merge(root)
			}
			return branchSlice, sk, nil
		}

		idx, err := lKey.LoadUInt(1)
		if err != nil {
			return nil, nil, err
		}

		branch, err = branch.PeekRef(int(idx))
		if err != nil {
			return nil, nil, err
		}

		if sk != nil {
			sk = sk.ProofRef(int(idx))
		}
	}
}

func loadLabel(sz uint, loader *Slice, key *Builder) (uint, *Builder, error) {
	first, err := loader.LoadUInt(1)
	if err != nil {
		return 0, nil, err
	}

	// hml_short$0
	if first == 0 {
		// Unary, while 1, add to ln
		ln := uint(0)
		for {
			bit, err := loader.LoadUInt(1)
			if err != nil {
				return 0, nil, err
			}

			if bit == 0 {
				break
			}
			ln++
		}

		keyBits, err := loader.LoadSlice(ln)
		if err != nil {
			return 0, nil, err
		}

		// add bits to key
		err = key.StoreSlice(keyBits, ln)
		if err != nil {
			return 0, nil, err
		}

		return ln, key, nil
	}

	second, err := loader.LoadUInt(1)
	if err != nil {
		return 0, nil, err
	}

	bitsLen := uint(math.Ceil(math.Log2(float64(sz + 1))))

	// hml_long$10
	if second == 0 {
		ln, err := loader.LoadUInt(bitsLen)
		if err != nil {
			return 0, nil, err
		}

		keyBits, err := loader.LoadSlice(uint(ln))
		if err != nil {
			return 0, nil, err
		}

		// add bits to key
		err = key.StoreSlice(keyBits, uint(ln))
		if err != nil {
			return 0, nil, err
		}

		return uint(ln), key, nil
	}

	// hml_same$11
	bitType, err := loader.LoadUInt(1)
	if err != nil {
		return 0, nil, err
	}

	ln, err := loader.LoadUInt(bitsLen)
	if err != nil {
		return 0, nil, err
	}

	var toStore []byte
	if bitType == 1 {
		// N of ones
		toStore = bytes.Repeat([]byte{0xFF}, 1+(int(ln)/8))
	} else {
		// N of zeroes
		toStore = bytes.Repeat([]byte{0x00}, 1+(int(ln)/8))
	}

	err = key.StoreSlice(toStore, uint(ln))
	if err != nil {
		return 0, nil, err
	}

	return uint(ln), key, nil
}

func (d *Dictionary) storeLabel(b *Builder, data *Slice, keyLen uint) error {
	ln := uint64(data.BitsLeft())
	// short unary 0
	if ln == 0 {
		if err := b.StoreUInt(0, 2); err != nil {
			return err
		}
		return nil
	}

	bitsLen := uint64(math.Ceil(math.Log2(float64(keyLen + 1))))
	_, dataBits, err := data.RestBits()
	if err != nil {
		return err
	}

	longLen := 2 + bitsLen + ln
	shortLength := 1 + 1 + 2*ln
	sameLength := 2 + 1 + bitsLen

	if sameLength < longLen && sameLength < shortLength {
		cmpInt := new(big.Int).SetBytes(dataBits)
		offset := ln % 8
		if offset != 0 {
			cmpInt = cmpInt.Rsh(cmpInt, uint(8-offset))
		}

		if cmpInt.Cmp(big.NewInt(0)) == 0 { // compare with all zeroes
			return d.storeSame(b, ln, bitsLen, 0)
		} else if cmpInt.BitLen() == int(ln) && cmpInt.Cmp(new(big.Int).Sub(new(big.Int).
			Lsh(big.NewInt(1), uint(ln)),
			big.NewInt(1))) == 0 { // compare with all ones
			return d.storeSame(b, ln, bitsLen, 1)
		}
	}

	if shortLength <= longLen {
		return d.storeShort(b, ln, dataBits)
	}
	return d.storeLong(b, ln, bitsLen, dataBits)
}

func (d *Dictionary) storeShort(b *Builder, partSz uint64, bits []byte) error {
	// magic
	if err := b.StoreUInt(0b0, 1); err != nil {
		return err
	}

	all1s := uint64(1<<(partSz+1) - 1)
	// all 1s and last 0
	if err := b.StoreUInt(all1s<<1, uint(partSz+1)); err != nil {
		return err
	}
	return b.StoreSlice(bits, uint(partSz))
}

func (d *Dictionary) storeSame(b *Builder, partSz, bitsLen uint64, bit uint64) error {
	// magic
	if err := b.StoreUInt(0b11, 2); err != nil {
		return err
	}

	// bit type
	if err := b.StoreUInt(bit, 1); err != nil {
		return err
	}
	return b.StoreUInt(partSz, uint(bitsLen))
}

func (d *Dictionary) storeLong(b *Builder, partSz, bitsLen uint64, bits []byte) error {
	// magic
	if err := b.StoreUInt(0b10, 2); err != nil {
		return err
	}

	if err := b.StoreUInt(partSz, uint(bitsLen)); err != nil {
		return err
	}
	return b.StoreSlice(bits, uint(partSz))
}

// Deprecated: use AsCell
func (d *Dictionary) MustToCell() *Cell {
	return d.AsCell()
}

func (d *Dictionary) AsCell() *Cell {
	return d.root
}

// Deprecated: use AsCell
func (d *Dictionary) ToCell() (*Cell, error) {
	return d.root, nil
}
