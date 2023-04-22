package cell

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
)

type Dictionary struct {
	storage map[string]*HashmapKV
	keySz   uint
}

type HashmapKV struct {
	Key   *Cell
	Value *Cell
}

func NewDict(keySz uint) *Dictionary {
	return &Dictionary{
		storage: map[string]*HashmapKV{},
		keySz:   keySz,
	}
}

func (c *Slice) ToDict(keySz uint) (*Dictionary, error) {
	d := &Dictionary{
		storage: map[string]*HashmapKV{},
		keySz:   keySz,
	}

	err := d.mapInner(keySz, keySz, c, BeginCell())
	if err != nil {
		return nil, err
	}

	return d, nil
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
			storage: map[string]*HashmapKV{},
			keySz:   keySz,
		}, nil
	}

	return cl.ToDict(keySz)
}

func (d *Dictionary) SetIntKey(key *big.Int, value *Cell) error {
	return d.Set(BeginCell().MustStoreBigInt(key, d.keySz).EndCell(), value)
}

func (d *Dictionary) Set(key, value *Cell) error {
	if key.BitsSize() != d.keySz {
		return fmt.Errorf("invalid key size")
	}

	data, err := key.BeginParse().LoadSlice(d.keySz)
	if err != nil {
		return fmt.Errorf("failed to set in dict, err: %w", err)
	}

	d.storage[hex.EncodeToString(data)] = &HashmapKV{
		Key:   key,
		Value: value,
	}
	return nil
}

func (d *Dictionary) GetByIntKey(key *big.Int) *Cell {
	return d.Get(BeginCell().MustStoreBigInt(key, d.keySz).EndCell())
}

func (d *Dictionary) Get(key *Cell) *Cell {
	data, err := key.BeginParse().LoadSlice(d.keySz)
	if err != nil {
		return nil
	}

	v := d.storage[hex.EncodeToString(data)]
	if v == nil {
		return nil
	}

	return v.Value
}

func (d *Dictionary) All() []*HashmapKV {
	all := make([]*HashmapKV, 0, len(d.storage))
	for _, v := range d.storage {
		all = append(all, v)
	}

	return all
}

func (d *Dictionary) mapInner(keySz, leftKeySz uint, loader *Slice, keyPrefix *Builder) error {
	var err error
	var sz uint

	sz, keyPrefix, err = loadLabel(leftKeySz, loader, keyPrefix)
	if err != nil {
		return err
	}

	key := keyPrefix.EndCell().BeginParse()

	// until key size is not equals we go deeper
	if key.BitsLeft() < keySz {
		// 0 bit branch
		left, err := loader.LoadRef()
		if err != nil {
			return nil
		}
		err = d.mapInner(keySz, leftKeySz-(1+sz), left, keyPrefix.Copy().MustStoreUInt(0, 1))
		if err != nil {
			return err
		}

		// 1 bit branch
		right, err := loader.LoadRef()
		if err != nil {
			return err
		}
		err = d.mapInner(keySz, leftKeySz-(1+sz), right, keyPrefix.Copy().MustStoreUInt(1, 1))
		if err != nil {
			return err
		}

		return nil
	}

	keyCell := keyPrefix.EndCell()
	// add node to map
	d.storage[hex.EncodeToString(keyCell.BeginParse().MustLoadSlice(keySz))] = &HashmapKV{
		Key:   keyCell,
		Value: loader.MustToCell(),
	}

	return nil
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

	// hml_long$10
	if second == 0 {
		bitsLen := uint(math.Ceil(math.Log2(float64(sz + 1))))

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

	bitsLen := uint(math.Ceil(math.Log2(float64(sz + 1))))

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

func (d *Dictionary) storeLabel(b *Builder, data []byte, committedOffset, bitOffset uint) error {
	partSz := uint64(bitOffset - committedOffset)

	// short unary 0
	if partSz == 0 {
		err := b.StoreUInt(0, 2)
		if err != nil {
			return err
		}
		return nil
	}

	bitsLen := uint64(math.Ceil(math.Log2(float64((d.keySz - committedOffset) + 1))))
	dataBits := getBits(data, committedOffset, bitOffset)

	longLen := 2 + bitsLen + partSz
	shortLength := 1 + 1 + 2*partSz
	sameLength := 2 + 1 + bitsLen

	var err error
	if sameLength < longLen && sameLength < shortLength {
		cmpInt := new(big.Int).SetBytes(dataBits)
		if cmpInt.Cmp(big.NewInt(0)) == 0 { // compare with all zeroes
			err = d.storeSame(b, partSz, bitsLen, 0)
		} else if cmpInt.Cmp(new(big.Int).Sub(new(big.Int).
			Lsh(big.NewInt(1), uint(bitsLen)+1),
			big.NewInt(1))) == 0 { // compare with all ones
			err = d.storeSame(b, partSz, bitsLen, 1)
		}

		if err != nil {
			return err
		}
	}

	if shortLength <= longLen {
		err = d.storeShort(b, partSz, dataBits)
	} else {
		err = d.storeLong(b, partSz, bitsLen, dataBits)
	}

	if err != nil {
		return err
	}

	return nil
}

func (d *Dictionary) storeShort(b *Builder, partSz uint64, bits []byte) error {
	// magic
	err := b.StoreUInt(0b0, 1)
	if err != nil {
		return err
	}

	all1s := uint64(1<<(partSz+1) - 1)
	err = b.StoreUInt(all1s<<1, uint(partSz+1)) // all 1s and last 0
	if err != nil {
		return err
	}

	err = b.StoreSlice(bits, uint(partSz))
	if err != nil {
		return err
	}
	return nil
}

func (d *Dictionary) storeSame(b *Builder, partSz, bitsLen uint64, bit uint64) error {
	// magic
	err := b.StoreUInt(0b11, 2)
	if err != nil {
		return err
	}

	// bit type
	err = b.StoreUInt(bit, 1)
	if err != nil {
		return err
	}

	err = b.StoreUInt(partSz, uint(bitsLen))
	if err != nil {
		return err
	}

	return nil
}

func (d *Dictionary) storeLong(b *Builder, partSz, bitsLen uint64, bits []byte) error {
	// magic
	err := b.StoreUInt(0b10, 2)
	if err != nil {
		return err
	}

	err = b.StoreUInt(partSz, uint(bitsLen))
	if err != nil {
		return err
	}

	err = b.StoreSlice(bits, uint(partSz))
	if err != nil {
		return err
	}
	return nil
}

func (d *Dictionary) MustToCell() *Cell {
	c, err := d.ToCell()
	if err != nil {
		panic(err)
	}
	return c
}

func (d *Dictionary) ToCell() (*Cell, error) {
	if len(d.storage) == 0 {
		return nil, nil
	}

	// TODO: add augmentation aggregation reading

	type kvData struct {
		data  []byte
		value *Cell
	}

	var root []*kvData
	for _, kv := range d.storage {
		root = append(root, &kvData{
			data:  kv.Key.BeginParse().MustLoadSlice(d.keySz),
			value: kv.Value,
		})
	}

	var dive func(kvs []*kvData, committedOffset, bitOffset, streakSame, streakPrefix, previous uint) (*Cell, error)
	dive = func(kvs []*kvData, committedOffset, bitOffset, streakSame, streakPrefix, previous uint) (*Cell, error) {
		if bitOffset == d.keySz {
			if len(kvs) > 1 {
				return nil, errors.New("not single key in a leaf")
			}

			b := BeginCell()

			err := d.storeLabel(b, kvs[0].data, committedOffset, bitOffset)
			if err != nil {
				return nil, fmt.Errorf("failed to store label, err: %w", err)
			}

			err = b.StoreBuilder(kvs[0].value.ToBuilder())
			if err != nil {
				return nil, fmt.Errorf("failed to store value, err: %w", err)
			}

			return b.EndCell(), nil
		}

		var zeroes, ones []*kvData
		for _, k := range kvs {
			checkBit := byte(1 << (7 - bitOffset%8))
			isOne := k.data[bitOffset/8]&checkBit > 0
			if isOne {
				ones = append(ones, k)
			} else {
				zeroes = append(zeroes, k)
			}
		}

		if len(zeroes) > 0 && len(ones) > 0 {
			b := BeginCell()

			// we took data from any key cause previous part is same
			// since we have 2 diff next values, we save same prefix here
			err := d.storeLabel(b, zeroes[0].data, committedOffset, bitOffset)
			if err != nil {
				return nil, fmt.Errorf("failed to store label, err: %w", err)
			}

			// we consider here also bit which branch indicates
			committedOffset = bitOffset + 1

			streakSame = 0
			streakPrefix = 0

			branch0, err := dive(zeroes, committedOffset, bitOffset+1, streakSame, streakPrefix, 0)
			if err != nil {
				return nil, fmt.Errorf("failed to build branch 0, err: %w", err)
			}

			branch1, err := dive(ones, committedOffset, bitOffset+1, streakSame, streakPrefix, 1)
			if err != nil {
				return nil, fmt.Errorf("failed to build branch 1, err: %w", err)
			}

			return b.MustStoreRef(branch0).MustStoreRef(branch1).EndCell(), nil
		} else if len(zeroes) > 0 {
			streakPrefix++
			if previous == 0 {
				streakSame++
			} else {
				streakSame = 1
			}

			return dive(zeroes, committedOffset, bitOffset+1, streakSame, streakPrefix, 0)
		} else if len(ones) > 0 {
			streakPrefix++
			if previous == 1 {
				streakSame++
			} else {
				streakSame = 1
			}

			return dive(ones, committedOffset, bitOffset+1, streakSame, streakPrefix, 1)
		}

		return nil, errors.New("empty branch")
	}

	dict, err := dive(root, 0, 0, 0, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create dict cell, err: %w", err)
	}

	return dict, nil
}

func getBits(data []byte, from, to uint) []byte {
	var res []byte
	var offset int

	for i := from; i < to; i++ {
		isOne := data[i/8]&(1<<(7-i%8)) > 0

		if offset%8 == 0 {
			res = append(res, 0)
		}

		if isOne {
			res[offset/8] |= 1 << (7 - offset%8)
		}
		offset++
	}
	return res
}
