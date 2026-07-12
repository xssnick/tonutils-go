package cell

import (
	"math/bits"
)

func dictLabelSizeBits(maxLen uint) uint {
	return uint(bits.Len(maxLen))
}

// loadUnaryLength consumes the hml_short unary length (n ones then a zero)
// in chunks instead of bit-by-bit reads. Real labels are almost always short,
// so the first probe reads just one byte before falling back to wide chunks.
func loadUnaryLength(loader *Slice) (uint, error) {
	// dense trees mostly have zero-length labels: a single zero bit
	first, err := loader.LoadUInt(1)
	if err != nil {
		return 0, err
	}
	if first == 0 {
		return 0, nil
	}

	ln := uint(1)
	chunkSize := uint(8)
	for {
		chunk := min(chunkSize, loader.BitsLeft())
		if chunk == 0 {
			return 0, ErrNotEnoughData(0, 1)
		}
		v, err := loader.loadUintFast(chunk, true)
		if err != nil {
			return 0, err
		}

		ones := uint(bits.LeadingZeros64(^(v << (64 - chunk))))
		if ones < chunk {
			// terminating zero found within this chunk
			if err = loader.SkipBits(ones + 1); err != nil {
				return 0, err
			}
			return ln + ones, nil
		}
		if err = loader.SkipBits(chunk); err != nil {
			return 0, err
		}
		ln += chunk
		chunkSize = 64
	}
}

func loadLabel(sz uint, loader *Slice, key *Builder) (uint, *Builder, error) {
	first, err := loader.LoadUInt(1)
	if err != nil {
		return 0, nil, err
	}

	// hml_short$0
	if first == 0 {
		ln, err := loadUnaryLength(loader)
		if err != nil {
			return 0, nil, err
		}
		if ln > sz {
			return 0, nil, ErrLabelExceedsKeyBits
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
			return 0, nil, ErrLabelExceedsKeyBits
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
		return 0, nil, ErrLabelExceedsKeyBits
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
		ln, err := loadUnaryLength(loader)
		if err != nil {
			return 0, 0, err
		}
		if ln > sz {
			return 0, 0, ErrLabelExceedsKeyBits
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
			return 0, 0, ErrLabelExceedsKeyBits
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
		return 0, 0, ErrLabelExceedsKeyBits
	}

	matched, err := consumeSameBitPrefix(key, bitType, uint(ln))
	if err != nil {
		return 0, 0, err
	}
	return uint(ln), matched, nil
}

func dictLabelSliceSameBit(data *Slice, bitLen uint) (uint64, bool, error) {
	if bitLen == 0 {
		return 0, true, nil
	}

	first, err := data.BitAt(0)
	if err != nil {
		return 0, false, err
	}
	wantBit := first != 0
	for offset := uint(0); offset < bitLen; {
		chunk := min(uint(64), bitLen-offset)
		value, err := preloadSliceUIntAt(data, offset, chunk)
		if err != nil {
			return 0, false, err
		}
		if value != sameBitsValue(wantBit, chunk) {
			return 0, false, nil
		}
		offset += chunk
	}
	return uint64(first), true, nil
}

func storeDictLabel(b *Builder, data *Slice, keyLen uint) error {
	ln := data.BitsLeft()
	if ln == 0 {
		return b.StoreUInt(0, 2)
	}

	bitsLen := dictLabelSizeBits(keyLen)

	longLen := uint(2) + bitsLen + ln
	shortLength := uint(2) + 2*ln
	sameLength := uint(3) + bitsLen

	if sameLength < longLen && sameLength < shortLength {
		bit, same, err := dictLabelSliceSameBit(data, ln)
		if err != nil {
			return err
		}
		if same {
			if err = storeDictSame(b, uint64(ln), uint64(bitsLen), bit); err != nil {
				return err
			}
			return data.SkipBits(ln)
		}
	}

	if shortLength <= longLen {
		if err := b.StoreUInt(0, 1); err != nil {
			return err
		}
		if err := b.StoreSameBit(true, ln); err != nil {
			return err
		}
		if err := b.StoreUInt(0, 1); err != nil {
			return err
		}
		return b.storeSliceFromSlice(data, ln)
	}

	if err := b.StoreUInt(0b10, 2); err != nil {
		return err
	}
	if err := b.StoreUInt(uint64(ln), bitsLen); err != nil {
		return err
	}
	return b.storeSliceFromSlice(data, ln)
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

func storeDictSame(b *Builder, partSz, bitsLen uint64, bit uint64) error {
	if err := b.StoreUInt(0b11, 2); err != nil {
		return err
	}
	if err := b.StoreUInt(bit, 1); err != nil {
		return err
	}
	return b.StoreUInt(partSz, uint(bitsLen))
}
