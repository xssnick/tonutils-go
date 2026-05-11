package cell

import (
	"bytes"
	"fmt"
)

func dictLabelSizeBits(maxLen uint) uint {
	var bits uint
	for maxLen > 0 {
		bits++
		maxLen >>= 1
	}
	return bits
}

func loadLabel(sz uint, loader *Slice, key *Builder) (uint, *Builder, error) {
	first, err := loader.LoadUInt(1)
	if err != nil {
		return 0, nil, err
	}

	// hml_short$0
	if first == 0 {
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
		if ln > sz {
			return 0, nil, fmt.Errorf("label exceeds remaining key bits")
		}

		keyBits, err := loader.LoadSlice(ln)
		if err != nil {
			return 0, nil, err
		}
		if err = key.StoreSlice(keyBits, ln); err != nil {
			return 0, nil, err
		}
		return ln, key, nil
	}

	second, err := loader.LoadUInt(1)
	if err != nil {
		return 0, nil, err
	}

	bitsLen := dictLabelSizeBits(sz)

	// hml_long$10
	if second == 0 {
		ln, err := loader.LoadUInt(bitsLen)
		if err != nil {
			return 0, nil, err
		}
		if ln > uint64(sz) {
			return 0, nil, fmt.Errorf("label exceeds remaining key bits")
		}

		keyBits, err := loader.LoadSlice(uint(ln))
		if err != nil {
			return 0, nil, err
		}
		if err = key.StoreSlice(keyBits, uint(ln)); err != nil {
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
	if ln > uint64(sz) {
		return 0, nil, fmt.Errorf("label exceeds remaining key bits")
	}

	var toStore []byte
	if bitType == 1 {
		toStore = bytes.Repeat([]byte{0xFF}, 1+(int(ln)/8))
	} else {
		toStore = bytes.Repeat([]byte{0x00}, 1+(int(ln)/8))
	}

	if err = key.StoreSlice(toStore, uint(ln)); err != nil {
		return 0, nil, err
	}
	return uint(ln), key, nil
}

func consumeCommonPrefix(label, key *Slice, limit uint) (uint, error) {
	matched := uint(0)
	for matched < limit && key.BitsLeft() > 0 {
		labelBit, err := label.LoadUInt(1)
		if err != nil {
			return 0, err
		}

		keyBit, err := key.LoadUInt(1)
		if err != nil {
			return 0, err
		}

		if labelBit != keyBit {
			return matched, nil
		}
		matched++
	}
	return matched, nil
}

func matchLabelPrefix(sz uint, loader, key *Slice) (uint, uint, error) {
	first, err := loader.LoadUInt(1)
	if err != nil {
		return 0, 0, err
	}

	if first == 0 {
		ln := uint(0)
		for {
			bit, err := loader.LoadUInt(1)
			if err != nil {
				return 0, 0, err
			}
			if bit == 0 {
				break
			}
			ln++
		}
		if ln > sz {
			return 0, 0, fmt.Errorf("label exceeds remaining key bits")
		}
		matched, err := consumeCommonPrefix(loader, key, ln)
		return ln, matched, err
	}

	second, err := loader.LoadUInt(1)
	if err != nil {
		return 0, 0, err
	}

	bitsLen := dictLabelSizeBits(sz)
	if second == 0 {
		ln, err := loader.LoadUInt(bitsLen)
		if err != nil {
			return 0, 0, err
		}
		if ln > uint64(sz) {
			return 0, 0, fmt.Errorf("label exceeds remaining key bits")
		}
		matched, err := consumeCommonPrefix(loader, key, uint(ln))
		return uint(ln), matched, err
	}

	bitType, err := loader.LoadUInt(1)
	if err != nil {
		return 0, 0, err
	}

	ln, err := loader.LoadUInt(bitsLen)
	if err != nil {
		return 0, 0, err
	}
	if ln > uint64(sz) {
		return 0, 0, fmt.Errorf("label exceeds remaining key bits")
	}

	matched := uint(0)
	for matched < uint(ln) && key.BitsLeft() > 0 {
		keyBit, err := key.LoadUInt(1)
		if err != nil {
			return 0, 0, err
		}
		if keyBit != bitType {
			return uint(ln), matched, nil
		}
		matched++
	}
	return uint(ln), matched, nil
}

func dictLabelSameBit(bits []byte, bitLen uint64) (uint64, bool) {
	if bitLen == 0 {
		return 0, true
	}

	bit := uint64((bits[0] >> 7) & 1)
	for i := uint64(0); i < bitLen; i++ {
		curr := uint64((bits[i/8] >> (7 - (i % 8))) & 1)
		if curr != bit {
			return 0, false
		}
	}
	return bit, true
}

func storeDictLabel(b *Builder, data *Slice, keyLen uint) error {
	ln := uint64(data.BitsLeft())
	if ln == 0 {
		return b.StoreUInt(0, 2)
	}

	bitsLen := uint64(dictLabelSizeBits(keyLen))
	_, dataBits, err := data.RestBits()
	if err != nil {
		return err
	}

	longLen := 2 + bitsLen + ln
	shortLength := 2 + 2*ln
	sameLength := 3 + bitsLen

	if sameLength < longLen && sameLength < shortLength {
		if bit, same := dictLabelSameBit(dataBits, ln); same {
			return storeDictSame(b, ln, bitsLen, bit)
		}
	}

	if shortLength <= longLen {
		return storeDictShort(b, ln, dataBits)
	}
	return storeDictLong(b, ln, bitsLen, dataBits)
}

func storeDictNode(label *Slice, payload *Builder, keyLen uint) (*Cell, error) {
	b := BeginCell()
	if err := storeDictLabel(b, label, keyLen); err != nil {
		return nil, err
	}
	if err := b.StoreBuilder(payload); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func storeDictNodeObserved(label *Slice, payload *Builder, keyLen uint, observer Observer) (*Cell, error) {
	b := BeginCell().SetObserver(observer)
	if err := storeDictLabel(b, label, keyLen); err != nil {
		return nil, err
	}
	if err := b.StoreBuilder(payload); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func storeDictShort(b *Builder, partSz uint64, bits []byte) error {
	if err := b.StoreUInt(0b0, 1); err != nil {
		return err
	}
	if err := b.StoreSameBit(true, uint(partSz)); err != nil {
		return err
	}
	if err := b.StoreUInt(0, 1); err != nil {
		return err
	}
	return b.StoreSlice(bits, uint(partSz))
}

func storeDictSame(b *Builder, partSz, bitsLen uint64, bit uint64) error {
	if err := b.StoreUInt(0b11, 2); err != nil {
		return err
	}
	if err := b.StoreUInt(bit, 1); err != nil {
		return err
	}
	return b.StoreUInt(partSz, uint(bitsLen))
}

func storeDictLong(b *Builder, partSz, bitsLen uint64, bits []byte) error {
	if err := b.StoreUInt(0b10, 2); err != nil {
		return err
	}
	if err := b.StoreUInt(partSz, uint(bitsLen)); err != nil {
		return err
	}
	return b.StoreSlice(bits, uint(partSz))
}
