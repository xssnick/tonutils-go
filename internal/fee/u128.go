// Package fee implements the fixed-width integer arithmetic behind the
// blockchain fee formulas.
//
// Every operand those formulas take is a uint64 — config prices, cell and bit
// counts, gas amounts, time deltas — so every product they form fits in 128
// bits exactly and every rounding step is a shift. Routing that through big.Int
// costs two heap objects per intermediate on a path a collation walks several
// times per transaction. The formulas themselves live in tlb and tvm and have
// to agree to the bit, so the arithmetic under them is implemented here once.
package fee

import (
	"math/big"
	"math/bits"
)

// U128 is an unsigned 128-bit integer.
type U128 struct {
	Hi, Lo uint64
}

// Mul64 returns the exact 128-bit product of two uint64 values. It cannot
// overflow: the largest such product is (2^64-1)^2 < 2^128.
func Mul64(a, b uint64) U128 {
	hi, lo := bits.Mul64(a, b)
	return U128{Hi: hi, Lo: lo}
}

// Shl64 returns v shifted left by n, for n < 64.
func Shl64(v uint64, n uint) U128 {
	return U128{Hi: v >> (64 - n), Lo: v << n}
}

// Add returns a+b and reports whether the sum fits in 128 bits. The sum of two
// full-width products does not always fit, so callers have to handle both.
func (a U128) Add(b U128) (U128, bool) {
	lo, carry := bits.Add64(a.Lo, b.Lo, 0)
	hi, carry := bits.Add64(a.Hi, b.Hi, carry)
	return U128{Hi: hi, Lo: lo}, carry == 0
}

// Mul64 returns a*b and reports whether the product fits in 128 bits.
func (a U128) Mul64(b uint64) (U128, bool) {
	hiHi, hiLo := bits.Mul64(a.Hi, b)
	loHi, lo := bits.Mul64(a.Lo, b)
	hi, carry := bits.Add64(hiLo, loHi, 0)
	return U128{Hi: hi, Lo: lo}, hiHi == 0 && carry == 0
}

// Add64 returns a+b. The fee formulas only add a uint64 to a value a CeilShr
// has already brought below 2^112, where the addition cannot leave 128 bits.
func (a U128) Add64(b uint64) U128 {
	lo, carry := bits.Add64(a.Lo, b, 0)
	return U128{Hi: a.Hi + carry, Lo: lo}
}

// CeilShr returns ceil(a / 2^n) for 0 < n < 64. Rounding up can carry out of
// 128 bits before the shift brings the value back down, so the carry is kept
// and the result is exact for every 128-bit input.
func (a U128) CeilShr(n uint) U128 {
	lo, carry := bits.Add64(a.Lo, 1<<n-1, 0)
	hi, carry := bits.Add64(a.Hi, 0, carry)
	return U128{Hi: hi>>n | carry<<(64-n), Lo: hi<<(64-n) | lo>>n}
}

// DivU64 returns a/b and reports whether the quotient fits in uint64. A zero
// divisor reports false rather than dividing.
func (a U128) DivU64(b uint64) (uint64, bool) {
	if a.Hi >= b {
		return 0, false
	}

	q, _ := bits.Div64(a.Hi, a.Lo, b)
	return q, true
}

// Cmp64 compares a with b, returning -1, 0 or 1 as in big.Int.Cmp.
func (a U128) Cmp64(b uint64) int {
	if a.Hi != 0 || a.Lo > b {
		return 1
	}
	if a.Lo < b {
		return -1
	}
	return 0
}

// Big allocates the big.Int the public fee signatures return.
func (a U128) Big() *big.Int {
	if a.Hi == 0 {
		return new(big.Int).SetUint64(a.Lo)
	}

	z := new(big.Int).SetUint64(a.Hi)
	return z.Lsh(z, 64).Or(z, new(big.Int).SetUint64(a.Lo))
}
