package tlb

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

var (
	errInvalid            = errors.New("invalid string")
	errDecimalMismatch    = errors.New("decimal mismatch")
	errTooBigForVarUint16 = errors.New("value is too big to be represented as a VarUint16")
	errDivisionByZero     = errors.New("division by zero in rational denominator")
)

type Coins struct {
	decimals int
	val      *big.Int
}

var ZeroCoins = MustFromTON("0")

// Deprecated: use String
func (g Coins) TON() string {
	return g.String()
}

func (g Coins) String() string {
	if g.val == nil {
		return "0"
	}

	// add sign if negative
	sign := ""
	val := g.val
	if val.Sign() < 0 {
		sign = "-"
		val = new(big.Int).Abs(val)
	}

	a := val.String()
	if a == "0" {
		return "0"
	}

	splitter := len(a) - g.decimals
	if splitter <= 0 {
		a = "0." + strings.Repeat("0", g.decimals-len(a)) + a
	} else {
		// set . between lo and hi
		a = a[:splitter] + "." + a[splitter:]
	}

	// cut last zeroes
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] == '.' {
			a = a[:i]
			break
		}
		if a[i] != '0' {
			a = a[:i+1]
			break
		}
	}

	return sign + a
}

// Deprecated: use Nano
func (g Coins) NanoTON() *big.Int {
	return g.Nano()
}

func (g Coins) Nano() *big.Int {
	if g.val == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(g.val)
}

func MustFromDecimal(val string, decimals int) Coins {
	v, err := FromDecimal(val, decimals)
	if err != nil {
		panic(err)
	}
	return v
}

func MustFromTON(val string) Coins {
	v, err := FromTON(val)
	if err != nil {
		panic(err)
	}
	return v
}

func MustFromNano(val *big.Int, decimals int) Coins {
	v, err := FromNano(val, decimals)
	if err != nil {
		panic(err)
	}
	return v
}

// tooBigForVarUint16 checks if the given big.Int value requires 16 or more bytes
// for its representation. This is used to ensure the value can be encoded
// as a TL-B VarUInteger16, which is the type used for Coins.
//
// Docs: https://docs.ton.org/v3/documentation/smart-contracts/func/docs/stdlib#store_coins
func tooBigForVarUint16(val *big.Int) bool {
	return (val.BitLen()+7)>>3 >= 16
}

func FromNano(val *big.Int, decimals int) (Coins, error) {
	if tooBigForVarUint16(val) {
		return Coins{}, fmt.Errorf("too big number for coins")
	}

	return Coins{
		decimals: decimals,
		val:      new(big.Int).Set(val),
	}, nil
}

func FromNanoTON(val *big.Int) Coins {
	return Coins{
		decimals: 9,
		val:      new(big.Int).Set(val),
	}
}

func FromNanoTONU(val uint64) Coins {
	return Coins{
		decimals: 9,
		val:      new(big.Int).SetUint64(val),
	}
}

func FromNanoTONStr(val string) (Coins, error) {
	v, ok := new(big.Int).SetString(val, 10)
	if !ok {
		return Coins{}, errInvalid
	}

	return Coins{
		decimals: 9,
		val:      v,
	}, nil
}

func FromTON(val string) (Coins, error) {
	return FromDecimal(val, 9)
}

func FromDecimal(val string, decimals int) (Coins, error) {
	if decimals < 0 || decimals >= 128 {
		return Coins{}, fmt.Errorf("invalid decimals")
	}

	s := strings.SplitN(val, ".", 2)

	if len(s) == 0 {
		return Coins{}, errInvalid
	}

	hi, ok := new(big.Int).SetString(s[0], 10)
	if !ok {
		return Coins{}, errInvalid
	}

	hi = hi.Mul(hi, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))

	if len(s) == 2 {
		loStr := s[1]
		// lo can have max {decimals} digits
		if len(loStr) > decimals {
			loStr = loStr[:decimals]
		}

		leadZeroes := 0
		for _, sym := range loStr {
			if sym != '0' {
				break
			}
			leadZeroes++
		}

		lo, ok := new(big.Int).SetString(loStr, 10)
		if !ok {
			return Coins{}, errInvalid
		}

		digits := len(lo.String()) // =_=
		lo = lo.Mul(lo, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64((decimals-leadZeroes)-digits)), nil))

		// Add lo for positive numbers and subtract for negative numbers.
		if val[0] == '-' {
			hi = hi.Sub(hi, lo)
		} else {
			hi = hi.Add(hi, lo)
		}
	}

	if tooBigForVarUint16(hi) {
		return Coins{}, fmt.Errorf("too big number for coins")
	}

	return Coins{
		decimals: decimals,
		val:      hi,
	}, nil
}

func (g *Coins) LoadFromCell(loader *cell.Slice) error {
	coins, err := loader.LoadBigCoins()
	if err != nil {
		return err
	}
	g.decimals = 9
	g.val = coins
	return nil
}

func (g Coins) ToCell() (*cell.Cell, error) {
	return cell.BeginCell().MustStoreBigCoins(g.Nano()).EndCell(), nil
}

func (g Coins) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", g.Nano().String())), nil
}

func (g *Coins) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid coins data")
	}

	data = data[1 : len(data)-1]

	coins, err := FromNanoTONStr(string(data))
	if err != nil {
		return err
	}

	*g = coins

	return nil
}

func (g Coins) Compare(coins Coins) int {
	if g.decimals != coins.decimals {
		panic("invalid comparison")
	}

	return g.Nano().Cmp(coins.Nano())
}

// MustAdd adds the provided coins to the current coins and returns the result.
// It panics if the operation fails (e.g., due to decimal mismatch or overflow).
func (g Coins) MustAdd(coins Coins) Coins {
	result, err := g.Add(coins)
	if err != nil {
		panic(err)
	}

	return result
}

// Add adds the provided coins to the current coins and returns the result.
// Returns an error if the coins have different decimal places or if the result
// would overflow the maximum allowed value.
func (g Coins) Add(coins Coins) (Coins, error) {
	if g.decimals != coins.decimals {
		return Coins{}, errDecimalMismatch
	}

	result := Coins{
		decimals: g.decimals,
		val:      new(big.Int).Add(g.Nano(), coins.Nano()),
	}
	if tooBigForVarUint16(result.val) {
		return Coins{}, errTooBigForVarUint16
	}

	return result, nil
}

// MustSub subtracts the provided coins from the current coins and returns the result.
// It panics if the operation fails (e.g., due to decimal mismatch or overflow).
func (g Coins) MustSub(coins Coins) Coins {
	result, err := g.Sub(coins)
	if err != nil {
		panic(err)
	}

	return result
}

// Sub subtracts the provided coins from the current coins and returns the result.
// Returns an error if the coins have different decimal places or if the result
// would overflow the maximum allowed value.
func (g Coins) Sub(coins Coins) (Coins, error) {
	if g.decimals != coins.decimals {
		return Coins{}, errDecimalMismatch
	}

	result := Coins{
		decimals: g.decimals,
		val:      new(big.Int).Sub(g.Nano(), coins.Nano()),
	}
	if tooBigForVarUint16(result.val) {
		return Coins{}, errTooBigForVarUint16
	}

	return result, nil
}

// MustMul multiplies the current coins by the provided big.Int and returns the result.
// It panics if the operation fails (e.g., due to overflow).
func (g Coins) MustMul(x *big.Int) Coins {
	result, err := g.Mul(x)
	if err != nil {
		panic(err)
	}

	return result
}

// Mul multiplies the current coins by the provided big.Int and returns the result.
// Returns an error if the result would overflow the maximum allowed value.
func (g Coins) Mul(x *big.Int) (Coins, error) {
	result := Coins{
		decimals: g.decimals,
		val:      new(big.Int).Mul(g.val, x),
	}
	if tooBigForVarUint16(result.val) {
		return Coins{}, errTooBigForVarUint16
	}

	return result, nil
}

// MustMulRat multiplies the current coins by the provided big.Rat and returns the result.
// It panics if the operation fails (e.g., due to division by zero or overflow).
func (g Coins) MustMulRat(r *big.Rat) Coins {
	result, err := g.MulRat(r)
	if err != nil {
		panic(err)
	}

	return result
}

// MulRat multiplies the current coins by the provided big.Rat and returns the result.
// Returns an error if the denominator is zero or if the result would overflow the maximum allowed value.
func (g Coins) MulRat(r *big.Rat) (Coins, error) {
	// Get numerator and denominator
	num := r.Num()
	den := r.Denom()

	if den.Sign() == 0 {
		return Coins{}, errDivisionByZero
	}

	// Calculate new nano value: (g.val * num) / den
	newVal := new(big.Int).Div(
		new(big.Int).Mul(g.val, num),
		den,
	)
	if tooBigForVarUint16(newVal) {
		return Coins{}, errTooBigForVarUint16
	}

	return Coins{
		decimals: g.decimals,
		val:      newVal,
	}, nil
}

// MustDiv divides the current coins by the provided big.Int and returns the result.
// It panics if the operation fails (e.g., due to division by zero or overflow).
func (g Coins) MustDiv(x *big.Int) Coins {
	result, err := g.Div(x)
	if err != nil {
		panic(err)
	}

	return result
}

// Div divides the current coins by the provided big.Int and returns the result.
// Returns an error if the divisor is zero or if the result would overflow the maximum allowed value.
func (g Coins) Div(x *big.Int) (Coins, error) {
	if x.Sign() == 0 {
		return Coins{}, errDivisionByZero
	}

	result := Coins{
		decimals: g.decimals,
		val:      new(big.Int).Div(g.Nano(), x),
	}
	if tooBigForVarUint16(result.val) {
		return Coins{}, errTooBigForVarUint16
	}

	return result, nil
}

// MustDivRat divides the current coins by the provided big.Rat and returns the result.
// It panics if the operation fails (e.g., due to division by zero or overflow).
func (g Coins) MustDivRat(r *big.Rat) Coins {
	result, err := g.DivRat(r)
	if err != nil {
		panic(err)
	}

	return result
}

// DivRat divides the current coins by the provided big.Rat and returns the result.
// This is equivalent to multiplying by the reciprocal of the rational number.
// Returns an error if the rational has zero numerator or denominator, or if the result would overflow.
func (g Coins) DivRat(r *big.Rat) (Coins, error) {
	// Get numerator and denominator
	num := r.Num()
	den := r.Denom()

	if num.Sign() == 0 || den.Sign() == 0 {
		return Coins{}, errDivisionByZero
	}

	// Calculate new nano value: (g.val * den) / num
	newVal := new(big.Int).Div(
		new(big.Int).Mul(g.val, den),
		num,
	)
	if tooBigForVarUint16(newVal) {
		return Coins{}, errTooBigForVarUint16
	}

	return Coins{
		decimals: g.decimals,
		val:      newVal,
	}, nil
}

// Neg returns a new Coins value representing the negation of the original value.
// The number of decimals remains the same.
func (g Coins) Neg() Coins {
	result := Coins{
		decimals: g.decimals,
		val:      new(big.Int).Neg(g.Nano()),
	}
	return result
}

// Abs returns a new Coins value representing the absolute value of the original value.
// The number of decimals remains the same.
func (g Coins) Abs() Coins {
	return Coins{
		decimals: g.decimals,
		val:      new(big.Int).Abs(g.Nano()),
	}
}

// GreaterThan returns true if the current coins amount is greater than the
// given coins amount
func (g Coins) GreaterThan(coins Coins) bool {
	return g.Compare(coins) > 0
}

// GreaterOrEqual returns true if the current coins amount is greater than or
// equal to the given coins amount
func (g Coins) GreaterOrEqual(coins Coins) bool {
	return g.Compare(coins) >= 0
}

// LessThan returns true if the current coins amount is less than the given coins
// amount
func (g Coins) LessThan(coins Coins) bool {
	return g.Compare(coins) < 0
}

// LessOrEqual returns true if the current coins amount is less than or equal to
// the given coins amount
func (g Coins) LessOrEqual(coins Coins) bool {
	return g.Compare(coins) <= 0
}

// Equals returns true if the current coins amount is equal to the given coins
// amount
func (g Coins) Equals(coins Coins) bool {
	return g.Compare(coins) == 0
}

// IsZero returns true if the coins amount is zero
func (g Coins) IsZero() bool {
	return g.Nano().Sign() == 0
}

// IsPositive returns true if the coins amount is greater than zero
func (g Coins) IsPositive() bool {
	return g.Nano().Sign() > 0
}

// IsNegative returns true if the coins amount is less than zero
func (g Coins) IsNegative() bool {
	return g.Nano().Sign() < 0
}

func (g Coins) Decimals() int {
	return g.decimals
}
