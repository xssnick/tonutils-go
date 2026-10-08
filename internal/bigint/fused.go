// Package bigint allocates big.Int values together with the storage their
// digits live in.
//
// A big.Int is a header plus a separately allocated []Word, so the ordinary
// new(big.Int).Set(x) costs two heap objects for a number that almost always
// fits in one or two machine words. Transaction emulation makes tens of
// thousands of such values per block — balances, fees, stack integers — and
// pays for each of them twice: once in the allocator and again in every GC
// mark that walks them.
//
// The values here carry their digits in an array inside the same object, so a
// number up to 128 bits costs one allocation instead of two and one object for
// the collector to trace instead of two. Nothing about the resulting *big.Int
// is special: it is an ordinary value that may be mutated, and a mutation that
// outgrows the inline array simply reallocates the way it always would.
package bigint

import "math/big"

// inlineWords is how many machine words the inline array holds. Two covers
// 128 bits, which spans every Grams amount (VarUInteger 16 is 120 bits) and
// the overwhelming majority of stack integers; wider values still work, they
// just allocate their digits separately on first growth.
const inlineWords = 2

// fused is a big.Int whose digits live in the same allocation. The abs slice
// of v is pointed at buf, so the two must never be separated: always hand out
// &f.v, never a copy of v.
type fused struct {
	v   big.Int
	buf [inlineWords]big.Word
}

// alloc returns a zero value backed by its own inline storage.
func alloc() *fused {
	f := &fused{}
	// An empty slice with capacity is a normalized zero, and nat.make reuses
	// the capacity for any value that fits, which is what keeps the digits in
	// this object.
	f.v.SetBits(f.buf[:0])
	return f
}

// New returns a zero big.Int backed by inline storage.
func New() *big.Int {
	return &alloc().v
}

// Set returns a private copy of src that the caller owns and may mutate.
func Set(src *big.Int) *big.Int {
	f := alloc()
	f.v.Set(src)
	return &f.v
}

// FromInt64 returns v as a big.Int backed by inline storage.
func FromInt64(v int64) *big.Int {
	f := alloc()
	f.v.SetInt64(v)
	return &f.v
}

// FromUint64 returns v as a big.Int backed by inline storage.
func FromUint64(v uint64) *big.Int {
	f := alloc()
	f.v.SetUint64(v)
	return &f.v
}
