package cell

import (
	"fmt"
	"math/bits"
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

		if err = key.storeSliceFromSlice(loader, ln); err != nil {
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

		if err = key.storeSliceFromSlice(loader, uint(ln)); err != nil {
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

	if err = key.StoreSameBit(bitType == 1, uint(ln)); err != nil {
		return 0, nil, err
	}
	return uint(ln), key, nil
}

func consumeCommonPrefix(label, key *Slice, limit uint) (uint, error) {
	limit = min(limit, key.BitsLeft())
	if limit == 0 {
		return 0, nil
	}

	matched, err := commonSlicePrefix(label, key, limit)
	if err != nil {
		return 0, err
	}

	consumed := matched
	if matched < limit {
		consumed++
	}
	if err = label.SkipBits(consumed); err != nil {
		return 0, err
	}
	if err = key.SkipBits(consumed); err != nil {
		return 0, err
	}

	return matched, nil
}

func consumeSameBitPrefix(key *Slice, bit uint64, limit uint) (uint, error) {
	limit = min(limit, key.BitsLeft())
	if limit == 0 {
		return 0, nil
	}

	matched := uint(0)
	for matched < limit {
		chunk := min(uint(64), limit-matched)
		value, err := preloadSliceUIntAt(key, matched, chunk)
		if err != nil {
			return 0, err
		}

		want := sameBitsValue(bit != 0, chunk)
		if value == want {
			matched += chunk
			continue
		}

		matched += chunk - uint(bits.Len64(value^want))
		if err = key.SkipBits(matched + 1); err != nil {
			return 0, err
		}
		return matched, nil
	}

	if err := key.SkipBits(matched); err != nil {
		return 0, err
	}
	return matched, nil
}

func commonSlicePrefix(a, b *Slice, limit uint) (uint, error) {
	if a.BitsLeft() < limit {
		return 0, ErrNotEnoughData(int(a.BitsLeft()), int(limit))
	}

	matched := uint(0)
	for matched < limit {
		chunk := min(uint(64), limit-matched)
		aVal, err := preloadSliceUIntAt(a, matched, chunk)
		if err != nil {
			return 0, err
		}
		bVal, err := preloadSliceUIntAt(b, matched, chunk)
		if err != nil {
			return 0, err
		}

		if aVal == bVal {
			matched += chunk
			continue
		}

		matched += chunk - uint(bits.Len64(aVal^bVal))
		return matched, nil
	}
	return matched, nil
}

func preloadSliceUIntAt(src *Slice, offset, sz uint) (uint64, error) {
	if sz == 0 {
		return 0, nil
	}
	if offset > src.BitsLeft() || src.BitsLeft()-offset < sz {
		return 0, ErrNotEnoughData(int(src.BitsLeft()-min(offset, src.BitsLeft())), int(sz))
	}

	cp := *src
	cp.bitStart += uint16(offset)
	return cp.loadUintFast(sz, true)
}

func sameBitsValue(bit bool, sz uint) uint64 {
	if !bit {
		return 0
	}
	if sz == 64 {
		return ^uint64(0)
	}
	return (uint64(1) << sz) - 1
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

	matched, err := consumeSameBitPrefix(key, bitType, uint(ln))
	if err != nil {
		return 0, 0, err
	}
	return uint(ln), matched, nil
}

func dictLabelSameBit(bits []byte, bitLen uint64) (uint64, bool) {
	if bitLen == 0 {
		return 0, true
	}

	bit := uint64((bits[0] >> 7) & 1)
	fullBytes := int(bitLen / 8)
	if bit == 0 {
		for _, b := range bits[:fullBytes] {
			if b != 0 {
				return 0, false
			}
		}
	} else {
		for _, b := range bits[:fullBytes] {
			if b != 0xFF {
				return 0, false
			}
		}
	}

	if rem := bitLen % 8; rem != 0 {
		mask := byte(0xFF << (8 - rem))
		want := byte(0)
		if bit == 1 {
			want = mask
		}
		if bits[fullBytes]&mask != want {
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

func storeDictNodeTraced(label *Slice, payload *Builder, keyLen uint, trace *Trace) (*Cell, error) {
	b := BeginCell().SetTrace(trace)
	if err := storeDictLabel(b, label, keyLen); err != nil {
		return nil, err
	}
	if err := b.StoreBuilderUncheckedDepth(payload); err != nil {
		return nil, err
	}
	return b.EndCellSpecial(false)
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
