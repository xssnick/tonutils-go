package int257

import (
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
)

// Global constants for 257-bit arithmetic.
var (
	mod257    = new(big.Int).Lsh(big.NewInt(1), 257)     // 2^257
	half257   = new(big.Int).Lsh(big.NewInt(1), 256)     // 2^256
	maxInt257 = new(big.Int).Sub(half257, big.NewInt(1)) // 2^256 - 1
	minInt257 = new(big.Int).Neg(half257)                // -2^256
)

func Zero() Int257 {
	return Int257{value: new(big.Int)}
}

func False() Int257 {
	return Zero()
}

func True() Int257 {
	return Zero().Not()
}

// normalize adjusts x modulo 2^257 and then maps it into [-2^256, 2^256-1].
func normalize(x *big.Int) *big.Int {
	r := x.Mod(x, mod257)
	if r.Cmp(half257) >= 0 {
		r.Sub(r, mod257)
	}
	return r
}

// Int257 represents a signed 257-bit integer in two’s complement.
// The internal value is always normalized to the range [–2^256, 2^256–1].
type Int257 struct {
	value *big.Int
	valid bool
}

// NewInt257FromBigInt creates a new Int257 number from *big.Int.
func NewInt257FromBigInt(x *big.Int) Int257 {
	return Int257{value: normalize(new(big.Int).Set(x)), valid: true}
}

// NewInt257FromBytes creates a new Int257 number from bytes
func NewInt257FromBytes(x []byte) Int257 {
	return Int257{value: normalize(new(big.Int).SetBytes(x)), valid: true}
}

func fromBigInt(x *big.Int) Int257 {
	return Int257{value: normalize(x), valid: true}
}

// NewInt257FromInt64 creates a new Int257 number from int64.
func NewInt257FromInt64(x int64) Int257 {
	return fromBigInt(big.NewInt(x))
}

// checkOverflow verifies that x is in the representable range.
func checkOverflow(x *big.Int) bool {
	return x.Cmp(minInt257) >= 0 && x.Cmp(maxInt257) <= 0
}

// ---------------- Arithmetic Operations with Overflow Checks ----------------

func (x Int257) Sign() int {
	return x.value.Sign()
}

func (x Int257) Bool() bool {
	return x.value.Sign() != 0
}

func (x Int257) IsValid() bool {
	return x.valid
}

func (x Int257) Invalidate() Int257 {
	return Int257{value: x.value, valid: false}
}

// Add returns the sum x + y. If overflow occurs, an error is returned.
func (x Int257) Add(y Int257) (Int257, error) {
	sum := new(big.Int).Add(x.value, y.value)
	// Overflow detection for two’s complement: if operands have the same sign and result’s sign differs.
	if x.value.Sign() == y.value.Sign() && x.value.Sign() != sum.Sign() {
		return Int257{}, vmerr.ErrIntOverflow
	}
	if !checkOverflow(sum) {
		return Int257{}, vmerr.ErrIntOverflow
	}
	return fromBigInt(sum), nil
}

// Sub returns the difference x - y. If overflow occurs, an error is returned.
func (x Int257) Sub(y Int257) (Int257, error) {
	diff := new(big.Int).Sub(x.value, y.value)
	// For subtraction, overflow if x and -y have same sign but result’s sign differs.
	if x.value.Sign() == new(big.Int).Neg(y.value).Sign() && x.value.Sign() != diff.Sign() {
		return Int257{}, vmerr.ErrIntOverflow
	}
	if !checkOverflow(diff) {
		return Int257{}, vmerr.ErrIntOverflow
	}
	return fromBigInt(diff), nil
}

// Mul returns the product x * y. If overflow occurs, an error is returned.
func (x Int257) Mul(y Int257) (Int257, error) {
	prod := new(big.Int).Mul(x.value, y.value)
	if !checkOverflow(prod) {
		return Int257{}, vmerr.ErrIntOverflow
	}
	return fromBigInt(prod), nil
}

// Div performs integer division x / y.
// Returns an error if y is 0 or if the result overflows.
func (x Int257) Div(y Int257) (Int257, error) {
	if y.value.Sign() == 0 {
		return Int257{}, vmerr.ErrIntOverflow
	}
	quot := new(big.Int).Div(x.value, y.value)
	if !checkOverflow(quot) {
		return Int257{}, vmerr.ErrIntOverflow
	}
	return fromBigInt(quot), nil
}

// Mod returns the remainder x mod y.
// Returns an error if y is 0.
func (x Int257) Mod(y Int257) (Int257, error) {
	if y.value.Sign() == 0 {
		return Int257{}, vmerr.ErrIntOverflow
	}
	rem := new(big.Int).Mod(x.value, y.value)
	// Note: The remainder in big.Int follows sign-of-dividend convention.
	if !checkOverflow(rem) {
		return Int257{}, vmerr.ErrIntOverflow
	}
	return fromBigInt(rem), nil
}

// Neg returns the negation -x. If overflow occurs (e.g. negation of -2^256), an error is returned.
func (x Int257) Neg() (Int257, error) {
	neg := new(big.Int).Neg(x.value)
	if !checkOverflow(neg) {
		return Int257{}, vmerr.ErrIntOverflow
	}
	return fromBigInt(neg), nil
}

// Abs returns the absolute value of x.
func (x Int257) Abs() Int257 {
	if x.value.Sign() >= 0 {
		return x
	}
	n, _ := x.Neg()
	return n
}

// Cmp compares x and y: returns -1 if x < y, 0 if x == y, and 1 if x > y.
func (x Int257) Cmp(y Int257) int {
	return x.value.Cmp(y.value)
}

// ---------------- Bitwise Operations ----------------
//
// For bitwise operations we first compute the "raw" representation:
// If x is negative, then raw(x) = x.value + 2^257; otherwise, raw(x) = x.value.
func (x Int257) raw() *big.Int {
	if x.value.Sign() < 0 {
		return new(big.Int).Add(x.value, mod257)
	}
	return new(big.Int).Set(x.value)
}

// fromRaw converts a raw representation (0 ≤ r < 2^257) back into an Int257 number.
func fromRaw(r *big.Int) Int257 {
	if r.Cmp(half257) >= 0 {
		v := r.Sub(r, mod257)
		return fromBigInt(v)
	}
	return fromBigInt(r)
}

var mask257 = new(big.Int).Sub(mod257, big.NewInt(1))

// And returns the bitwise AND of x and y, computed on the 257-bit raw representations.
func (x Int257) And(y Int257) Int257 {
	rx := x.raw()
	ry := y.raw()
	res := rx.And(rx, ry)
	res.And(res, mask257)
	return fromRaw(res)
}

// Or returns the bitwise OR of x and y.
func (x Int257) Or(y Int257) Int257 {
	rx := x.raw()
	ry := y.raw()
	res := rx.Or(rx, ry)
	res.And(res, mask257)
	return fromRaw(res)
}

// Xor returns the bitwise XOR of x and y.
func (x Int257) Xor(y Int257) Int257 {
	rx := x.raw()
	ry := y.raw()
	res := rx.Xor(rx, ry)
	res.And(res, mask257)
	return fromRaw(res)
}

// Not returns the bitwise NOT of x.
func (x Int257) Not() Int257 {
	rx := x.raw()
	res := rx.Xor(rx, mask257)
	return fromRaw(res)
}

// Lsh performs a logical left shift x << n modulo 2^257.
func (x Int257) Lsh(n uint) Int257 {
	// Lsh on raw representation; result modulo 2^257.
	rx := x.raw()
	res := rx.Lsh(rx, n)
	res.Mod(res, mod257)
	return fromRaw(res)
}

// Rsh performs an arithmetic right shift x >> n.
// For negative numbers, the sign is extended.
func (x Int257) Rsh(n uint) Int257 {
	// Arithmetic right shift on the signed value.
	res := new(big.Int).Rsh(x.value, n)
	return fromBigInt(res)
}

// ---------------- Other Methods ----------------

// ToBigInt returns a copy of the internal *big.Int value.
func (x Int257) ToBigInt() *big.Int {
	return new(big.Int).Set(x.value)
}

// Uint64 returns uint64
func (x Int257) Uint64() uint64 {
	return new(big.Int).Set(x.value).Uint64()
}

// String returns the decimal string representation of the number.
func (x Int257) String() string {
	return x.value.String()
}
