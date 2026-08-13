package tlb

import (
	"fmt"
	"math/big"
	"math/bits"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// gramsU128 holds a Grams amount as a fixed 128-bit unsigned integer.
//
// Grams is VarUInteger 16 — fifteen bytes at most, so 120 bits — and the
// augmentation of every currency-carrying dictionary sums two of them at every
// fork it rebuilds. Routing that through big.Int costs two heap objects per
// amount for arithmetic a pair of machine words does exactly, and a block
// collation performs it tens of thousands of times. The width is chosen to
// cover the encoding rather than the domain: no valid amount comes close, and
// a sum that would overflow is reported instead of wrapping.
type gramsU128 struct {
	hi, lo uint64
}

func (g gramsU128) bitLen() int {
	if g.hi != 0 {
		return 64 + bits.Len64(g.hi)
	}
	return bits.Len64(g.lo)
}

// loadGramsU128 reads a Grams: a 4-bit byte length followed by that many bytes,
// big-endian. It reads through LoadUInt rather than LoadSlice so the value
// never touches a byte slice.
func loadGramsU128(s *cell.Slice) (gramsU128, error) {
	ln, err := s.LoadUInt(4)
	if err != nil {
		return gramsU128{}, fmt.Errorf("failed to load grams length: %w", err)
	}
	if ln == 0 {
		return gramsU128{}, nil
	}

	var v gramsU128
	if ln <= 8 {
		if v.lo, err = s.LoadUInt(uint(ln) * 8); err != nil {
			return gramsU128{}, fmt.Errorf("failed to load grams value: %w", err)
		}
	} else {
		if v.hi, err = s.LoadUInt(uint(ln-8) * 8); err != nil {
			return gramsU128{}, fmt.Errorf("failed to load grams value: %w", err)
		}
		if v.lo, err = s.LoadUInt(64); err != nil {
			return gramsU128{}, fmt.Errorf("failed to load grams value: %w", err)
		}
	}

	// Chain-validated Grams never carry a leading zero byte. A value that would
	// have fit in one byte fewer is therefore a malformed encoding, not another
	// spelling of the same number — the same rejection rawGrams.integer makes.
	if v.bitLen() <= int(ln-1)*8 {
		return gramsU128{}, fmt.Errorf("grams value has a leading zero byte")
	}
	return v, nil
}

// storeTo appends the canonical encoding: the byte length in 4 bits, then that
// many bytes with no leading zero. This is StoreBigVarUInt(v, 16) without the
// big.Int, including its refusal of anything needing sixteen bytes.
func (g gramsU128) storeTo(b *cell.Builder) error {
	ln := uint((g.bitLen() + 7) / 8)
	if ln >= 16 {
		return fmt.Errorf("grams value does not fit VarUInteger 16")
	}
	if err := b.StoreUInt(uint64(ln), 4); err != nil {
		return err
	}
	if ln == 0 {
		return nil
	}
	if ln <= 8 {
		return b.StoreUInt(g.lo, ln*8)
	}
	if err := b.StoreUInt(g.hi, (ln-8)*8); err != nil {
		return err
	}
	return b.StoreUInt(g.lo, 64)
}

// greater reports g > o.
func (g gramsU128) greater(o gramsU128) bool {
	if g.hi != o.hi {
		return g.hi > o.hi
	}
	return g.lo > o.lo
}

// String prints the value in decimal for diagnostics. It allocates, which is
// fine: every caller is an error path.
func (g gramsU128) String() string {
	return new(big.Int).Or(
		new(big.Int).Lsh(new(big.Int).SetUint64(g.hi), 64),
		new(big.Int).SetUint64(g.lo),
	).String()
}

func (g gramsU128) add(o gramsU128) (gramsU128, error) {
	lo, carry := bits.Add64(g.lo, o.lo, 0)
	hi, overflow := bits.Add64(g.hi, o.hi, carry)
	if overflow != 0 {
		return gramsU128{}, fmt.Errorf("grams sum overflows 128 bits")
	}
	return gramsU128{hi: hi, lo: lo}, nil
}

// skipVarUInt advances past a VarUInteger sz without materialising its value.
// Reading a number only to discard it costs a big.Int; skipping costs nothing.
func skipVarUInt(s *cell.Slice, sz uint64) error {
	szLen := uint(bits.Len64(sz - 1))
	ln, err := s.LoadUInt(szLen)
	if err != nil {
		return err
	}
	return s.SkipBits(uint(ln) * 8)
}

// skipGrams advances past a Grams (VarUInteger 16) field.
func skipGrams(s *cell.Slice) error {
	return skipVarUInt(s, 16)
}

// addGramsSlices reads one Grams from each slice and appends their sum.
func addGramsSlices(b *cell.Builder, left, right *cell.Slice) error {
	lg, err := loadGramsU128(left)
	if err != nil {
		return fmt.Errorf("failed to load left grams: %w", err)
	}
	rg, err := loadGramsU128(right)
	if err != nil {
		return fmt.Errorf("failed to load right grams: %w", err)
	}
	sum, err := lg.add(rg)
	if err != nil {
		return err
	}
	return sum.storeTo(b)
}

// bothExtraDictsAbsent reports whether neither slice carries an extra-currency
// dictionary, without consuming either. Slices are values, so a copy is an
// independent cursor and peeking costs nothing.
//
// This matters because the overwhelming majority of amounts carry no extra
// currencies at all, and LoadDict builds a Dictionary even for an absent one.
func bothExtraDictsAbsent(left, right *cell.Slice) (bool, error) {
	lc, rc := *left, *right
	lHas, err := lc.LoadBoolBit()
	if err != nil {
		return false, fmt.Errorf("failed to peek left extra currencies: %w", err)
	}
	rHas, err := rc.LoadBoolBit()
	if err != nil {
		return false, fmt.Errorf("failed to peek right extra currencies: %w", err)
	}
	return !lHas && !rHas, nil
}
