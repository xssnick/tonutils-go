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
//
// It mirrors C++ block::CurrencyCollection::sub returning false when
// td::sgn(c.grams) < 0 or sub_extra_currency fails
// (ton/crypto/block/block.cpp:1209-1219, :1787-1797).
var ErrCurrencyCollectionUnderflow = errors.New("currency collection underflow")

// Add returns c + other following C++ block::CurrencyCollection::add
// (ton/crypto/block/block.cpp:1141-1145):
//   - grams are added as arbitrary precision integers,
//   - extra currency dictionaries are merged with add_extra_currency
//     (ton/crypto/block/block.cpp:1775-1785).
//
// Neither receiver nor argument is mutated.
func (c CurrencyCollection) Add(other CurrencyCollection) (CurrencyCollection, error) {
	grams := new(big.Int).Add(c.Coins.Nano(), other.Coins.Nano())

	extra, err := addExtraCurrencyDicts(c.ExtraCurrencies, other.ExtraCurrencies)
	if err != nil {
		return CurrencyCollection{}, fmt.Errorf("failed to add extra currencies: %w", err)
	}

	return CurrencyCollection{
		Coins:           FromNanoTON(grams),
		ExtraCurrencies: extra,
	}, nil
}

// Sub returns c - other following C++ block::CurrencyCollection::sub
// (ton/crypto/block/block.cpp:1209-1213):
//   - a negative grams result is an underflow error (td::sgn(c.grams) >= 0 check),
//   - extra currency dictionaries are subtracted with sub_extra_currency
//     (ton/crypto/block/block.cpp:1787-1797): every extra currency entry of other
//     must be present in c with a value >= the subtracted one; entries that reach
//     exactly zero are removed from the result dictionary
//     (HashmapE::sub_values drops entries whose difference is zero,
//     ton/crypto/block/block-parse.cpp:550-567).
//
// Neither receiver nor argument is mutated. On underflow the returned error wraps
// ErrCurrencyCollectionUnderflow.
func (c CurrencyCollection) Sub(other CurrencyCollection) (CurrencyCollection, error) {
	grams := new(big.Int).Sub(c.Coins.Nano(), other.Coins.Nano())
	if grams.Sign() < 0 {
		return CurrencyCollection{}, fmt.Errorf("%w: grams %s < %s",
			ErrCurrencyCollectionUnderflow, c.Coins.Nano().String(), other.Coins.Nano().String())
	}

	extra, ok, err := subExtraCurrencyDicts(c.ExtraCurrencies, other.ExtraCurrencies)
	if err != nil {
		return CurrencyCollection{}, fmt.Errorf("failed to subtract extra currencies: %w", err)
	}
	if !ok {
		return CurrencyCollection{}, fmt.Errorf("%w: extra currencies", ErrCurrencyCollectionUnderflow)
	}

	return CurrencyCollection{
		Coins:           FromNanoTON(grams),
		ExtraCurrencies: extra,
	}, nil
}

// Equals follows C++ block::CurrencyCollection::operator==
// (ton/crypto/block/block.cpp:1277-1281): grams are compared numerically and
// extra currency dictionaries are compared by root cell hash (an empty dictionary
// equals a nil one, both correspond to the C++ null root reference).
func (c CurrencyCollection) Equals(other CurrencyCollection) bool {
	if c.Coins.Nano().Cmp(other.Coins.Nano()) != 0 {
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

// GreaterOrEqual follows C++ block::CurrencyCollection::operator>=
// (ton/crypto/block/block.cpp:1283-1287): true when grams of c are >= grams of
// other and subtracting other's extra currencies from c's would not underflow.
func (c CurrencyCollection) GreaterOrEqual(other CurrencyCollection) (bool, error) {
	if c.Coins.Nano().Cmp(other.Coins.Nano()) < 0 {
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
// bit-exactly, the same way C++ combine_with copies unmatched leaves without
// re-serializing them (ton/crypto/vm/dict.cpp dict_combine_with).
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

// loadExtraCurrencyAmount parses a single extra currency leaf payload as
// VarUIntegerPos 32: the value type of ExtraCurrencyCollection is strictly
// positive (ton/crypto/block/block-parse.h:332-334 uses t_VarUIntegerPos_32,
// parsing per ton/crypto/block/block-parse.cpp:349-352 requires a non-zero
// length with a non-zero leading byte). Zero or padded encodings are rejected,
// exactly like C++ as_integer_skip.
func loadExtraCurrencyAmount(payload *cell.Cell) (*big.Int, error) {
	s, err := payload.BeginParse()
	if err != nil {
		return nil, err
	}

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
		// VarUIntegerPos 32 cannot represent zero
		// (ton/crypto/block/block-parse.cpp:361-365 requires a positive value),
		// callers must drop zero entries instead of storing them.
		return nil, fmt.Errorf("extra currency value must be strictly positive")
	}
	b := cell.BeginCell()
	if err := b.StoreBigVarUInt(amount, 32); err != nil {
		return nil, fmt.Errorf("failed to store extra currency value: %w", err)
	}
	return b.EndCell(), nil
}

// addExtraCurrencyDicts mirrors C++ add_extra_currency
// (ton/crypto/block/block.cpp:1775-1785):
//   - if either side is empty, the other side's dictionary is reused as is
//     (no re-serialization),
//   - otherwise dictionaries are merged like
//     ExtraCurrencyCollection add_values / vm::Dictionary::combine_with
//     (ton/crypto/block/block-parse.cpp:514-527): entries unique to one side are
//     copied bit-exactly, colliding entries are re-encoded as the canonical
//     VarUIntegerPos 32 sum of both values.
//
// The sum of two strictly positive VarUIntegerPos values cannot be zero, so
// addition never has to drop entries; zero-valued entries cannot legally exist
// in the inputs in the first place (and are rejected when they collide, same as
// C++ as_integer_skip failing on them).
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

// subExtraCurrencyDicts mirrors C++ sub_extra_currency
// (ton/crypto/block/block.cpp:1787-1797) combined with
// ExtraCurrencyCollection sub_values (ton/crypto/block/block-parse.cpp:550-567):
//   - subtracting an empty dictionary reuses the minuend as is,
//   - every entry of b must exist in a: a missing key is an underflow
//     (combine_with in subset mode fails when a subtree of b has no counterpart
//     in a; the pre-fix mode check in the local ton checkout,
//     ton/crypto/vm/dict.cpp:1781, is ineffective, current upstream throws
//     CombineError when dict1 is null and dict2 is not),
//   - a negative difference is an underflow,
//   - entries whose difference is exactly zero are dropped from the result
//     (TLB sub_values returns 0 for them and HashmapE::sub_values removes such
//     leaves, ton/crypto/tl/tlblib.hpp:222-226).
//
// Returns ok=false on underflow.
func subExtraCurrencyDicts(a, b *cell.Dictionary) (*cell.Dictionary, bool, error) {
	if b == nil || b.IsEmpty() {
		return a, true, nil
	}
	if a == nil || a.IsEmpty() {
		// C++ sub_extra_currency: extra1 null and extra2 not null -> failure
		// (ton/crypto/block/block.cpp:1790-1792).
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
