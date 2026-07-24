package tlb

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// ErrCurrencyCollectionUnderflow is returned by CurrencyCollection.Sub when the
// subtrahend is greater than the minuend, either in grams or in any extra currency
// (including extra currency ids that are missing on the minuend side).
var ErrCurrencyCollectionUnderflow = errors.New("currency collection underflow")

// Add returns c + other without mutating either operand.
func (c CurrencyCollection) Add(other CurrencyCollection) (CurrencyCollection, error) {
	grams := new(big.Int).Add(c.Coins.nanoValue(), other.Coins.nanoValue())

	extra, err := addExtraCurrencyDicts(c.ExtraCurrencies, other.ExtraCurrencies)
	if err != nil {
		return CurrencyCollection{}, fmt.Errorf("failed to add extra currencies: %w", err)
	}

	return CurrencyCollection{
		Coins:           Coins{decimals: 9, val: grams},
		ExtraCurrencies: extra,
	}, nil
}

// Sub returns c - other without mutating either operand. Missing currencies,
// negative results and insufficient balances wrap ErrCurrencyCollectionUnderflow.
func (c CurrencyCollection) Sub(other CurrencyCollection) (CurrencyCollection, error) {
	grams := new(big.Int).Sub(c.Coins.nanoValue(), other.Coins.nanoValue())
	if grams.Sign() < 0 {
		return CurrencyCollection{}, fmt.Errorf("%w: grams %s < %s",
			ErrCurrencyCollectionUnderflow, c.Coins.nanoValue().String(), other.Coins.nanoValue().String())
	}

	extra, ok, err := subExtraCurrencyDicts(c.ExtraCurrencies, other.ExtraCurrencies)
	if err != nil {
		return CurrencyCollection{}, fmt.Errorf("failed to subtract extra currencies: %w", err)
	}
	if !ok {
		return CurrencyCollection{}, fmt.Errorf("%w: extra currencies", ErrCurrencyCollectionUnderflow)
	}

	return CurrencyCollection{
		Coins:           Coins{decimals: 9, val: grams},
		ExtraCurrencies: extra,
	}, nil
}

// Equals compares grams numerically and extra currencies by dictionary root.
// Nil and empty extra-currency dictionaries are equivalent.
func (c CurrencyCollection) Equals(other CurrencyCollection) bool {
	if c.Coins.nanoValue().Cmp(other.Coins.nanoValue()) != 0 {
		return false
	}

	left, right := extraDictRoot(c.ExtraCurrencies), extraDictRoot(other.ExtraCurrencies)
	if (left == nil) != (right == nil) {
		return false
	}
	if left == nil {
		return true
	}
	return left.HashKey(0) == right.HashKey(0)
}

// GreaterOrEqual reports whether every currency in c covers other.
func (c CurrencyCollection) GreaterOrEqual(other CurrencyCollection) (bool, error) {
	if c.Coins.nanoValue().Cmp(other.Coins.nanoValue()) < 0 {
		return false, nil
	}
	_, ok, err := subExtraCurrencyDicts(c.ExtraCurrencies, other.ExtraCurrencies)
	if err != nil {
		return false, fmt.Errorf("failed to compare extra currencies: %w", err)
	}
	return ok, nil
}

func extraDictRoot(d *cell.Dictionary) *cell.Cell {
	if d == nil || d.IsEmpty() {
		return nil
	}
	return d.AsCell()
}

type extraCurrencyEntry struct {
	key   uint32
	value *cell.Cell // raw leaf payload, VarUIntegerPos 32
}

// loadExtraCurrencyEntries returns raw dict entries ordered by dictionary walk.
// Values are kept as raw payload cells so untouched entries can be re-stored
// without changing their encoding.
func loadExtraCurrencyEntries(d *cell.Dictionary) ([]extraCurrencyEntry, error) {
	if d == nil || d.IsEmpty() {
		return nil, nil
	}

	items, err := d.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currency dict: %w", err)
	}

	entries := make([]extraCurrencyEntry, 0, len(items))
	for _, item := range items {
		key, err := item.Key.LoadUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load extra currency id: %w", err)
		}
		value, err := item.Value.ToCell()
		if err != nil {
			return nil, fmt.Errorf("failed to capture extra currency value: %w", err)
		}
		entries = append(entries, extraCurrencyEntry{key: uint32(key), value: value})
	}
	return entries, nil
}

// loadExtraCurrencyAmount parses a canonical positive VarUInteger 32 payload.
func loadExtraCurrencyAmount(payload *cell.Cell) (*big.Int, error) {
	s, err := payload.BeginParse()
	if err != nil {
		return nil, err
	}
	return loadExtraCurrencyAmountSlice(s)
}

// loadExtraCurrencyAmountSlice parses a canonical positive VarUInteger 32
// prefix of the slice. Trailing data is left in the slice for the caller to
// judge: arithmetic accepts it like C++ as_integer_skip, while dictionary
// validation must reject it like C++ validate_skip + empty_ext.
func loadExtraCurrencyAmountSlice(s *cell.Slice) (*big.Int, error) {
	ln, err := s.LoadUInt(5) // len:(#< 32)
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currency value length: %w", err)
	}
	if ln == 0 || ln >= 32 {
		return nil, fmt.Errorf("invalid extra currency value length %d, must be in [1..31]", ln)
	}

	data, err := s.LoadSlice(uint(ln) * 8)
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currency value: %w", err)
	}
	if data[0] == 0 {
		return nil, fmt.Errorf("non-canonical extra currency value with leading zero byte")
	}
	return new(big.Int).SetBytes(data), nil
}

func storeExtraCurrencyAmount(amount *big.Int) (*cell.Cell, error) {
	if amount == nil || amount.Sign() <= 0 {
		// Zero entries must be removed instead of stored.
		return nil, fmt.Errorf("extra currency value must be strictly positive")
	}
	b := cell.BeginCell()
	if err := b.StoreBigVarUInt(amount, 32); err != nil {
		return nil, fmt.Errorf("failed to store extra currency value: %w", err)
	}
	return b.EndCell(), nil
}

// addExtraCurrencyDicts reuses an unchanged dictionary when either side is
// empty. Unique leaves retain their encoding; colliding values are summed and
// encoded canonically.
func addExtraCurrencyDicts(a, b *cell.Dictionary) (*cell.Dictionary, error) {
	if b == nil || b.IsEmpty() {
		return a, nil
	}
	if a == nil || a.IsEmpty() {
		return b, nil
	}

	left, err := loadExtraCurrencyEntries(a)
	if err != nil {
		return nil, err
	}
	right, err := loadExtraCurrencyEntries(b)
	if err != nil {
		return nil, err
	}

	rightByKey := make(map[uint32]*cell.Cell, len(right))
	for _, e := range right {
		rightByKey[e.key] = e.value
	}

	result := cell.NewDict(32)
	set := func(key uint32, payload *cell.Cell) error {
		return result.SetIntKey(new(big.Int).SetUint64(uint64(key)), payload)
	}

	for _, e := range left {
		otherValue, collides := rightByKey[e.key]
		if !collides {
			if err = set(e.key, e.value); err != nil {
				return nil, err
			}
			continue
		}
		delete(rightByKey, e.key)

		x, err := loadExtraCurrencyAmount(e.value)
		if err != nil {
			return nil, fmt.Errorf("extra currency %d: %w", e.key, err)
		}
		y, err := loadExtraCurrencyAmount(otherValue)
		if err != nil {
			return nil, fmt.Errorf("extra currency %d: %w", e.key, err)
		}

		payload, err := storeExtraCurrencyAmount(new(big.Int).Add(x, y))
		if err != nil {
			return nil, fmt.Errorf("extra currency %d: %w", e.key, err)
		}
		if err = set(e.key, payload); err != nil {
			return nil, err
		}
	}

	for _, e := range right {
		if _, unmerged := rightByKey[e.key]; !unmerged {
			continue // already merged above
		}
		if err = set(e.key, e.value); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// subExtraCurrencyDicts reuses the minuend when b is empty and drops exact-zero
// results. It returns ok=false when b contains a missing or insufficient value.
func subExtraCurrencyDicts(a, b *cell.Dictionary) (*cell.Dictionary, bool, error) {
	if b == nil || b.IsEmpty() {
		return a, true, nil
	}
	if a == nil || a.IsEmpty() {
		return nil, false, nil
	}

	left, err := loadExtraCurrencyEntries(a)
	if err != nil {
		return nil, false, err
	}
	right, err := loadExtraCurrencyEntries(b)
	if err != nil {
		return nil, false, err
	}

	rightByKey := make(map[uint32]*cell.Cell, len(right))
	for _, e := range right {
		rightByKey[e.key] = e.value
	}

	result := cell.NewDict(32)
	empty := true

	for _, e := range left {
		otherValue, collides := rightByKey[e.key]
		if !collides {
			if err = result.SetIntKey(new(big.Int).SetUint64(uint64(e.key)), e.value); err != nil {
				return nil, false, err
			}
			empty = false
			continue
		}
		delete(rightByKey, e.key)

		x, err := loadExtraCurrencyAmount(e.value)
		if err != nil {
			return nil, false, fmt.Errorf("extra currency %d: %w", e.key, err)
		}
		y, err := loadExtraCurrencyAmount(otherValue)
		if err != nil {
			return nil, false, fmt.Errorf("extra currency %d: %w", e.key, err)
		}

		diff := new(big.Int).Sub(x, y)
		switch diff.Sign() {
		case -1:
			return nil, false, nil // underflow
		case 0:
			continue // exact zero results are dropped, never stored
		}

		payload, err := storeExtraCurrencyAmount(diff)
		if err != nil {
			return nil, false, fmt.Errorf("extra currency %d: %w", e.key, err)
		}
		if err = result.SetIntKey(new(big.Int).SetUint64(uint64(e.key)), payload); err != nil {
			return nil, false, err
		}
		empty = false
	}

	if len(rightByKey) != 0 {
		// keys present in b but missing in a
		return nil, false, nil
	}

	if empty {
		return nil, true, nil
	}
	return result, true, nil
}
