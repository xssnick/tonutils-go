package tlb

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func ccFromNano(nano int64, extra *cell.Dictionary) CurrencyCollection {
	return CurrencyCollection{Coins: FromNanoTON(big.NewInt(nano)), ExtraCurrencies: extra}
}

func extraAmount(t *testing.T, d *cell.Dictionary, id uint32) *big.Int {
	t.Helper()
	if d == nil {
		return big.NewInt(0)
	}
	v, err := d.LoadValueByIntKey(new(big.Int).SetUint64(uint64(id)))
	if err != nil || v == nil {
		return big.NewInt(0)
	}
	amount, err := v.LoadVarUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	return amount
}

func TestCurrencyCollectionAddBasic(t *testing.T) {
	a := ccFromNano(1500, mustExtraDict(t, map[uint32]int64{1: 10, 2: 20}))
	b := ccFromNano(500, mustExtraDict(t, map[uint32]int64{2: 5, 3: 7}))

	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Coins.Nano().Int64() != 2000 {
		t.Fatalf("grams sum mismatch: %s", sum.Coins.Nano())
	}
	if got := extraAmount(t, sum.ExtraCurrencies, 1); got.Int64() != 10 {
		t.Fatalf("currency 1: %s", got)
	}
	if got := extraAmount(t, sum.ExtraCurrencies, 2); got.Int64() != 25 {
		t.Fatalf("currency 2: %s", got)
	}
	if got := extraAmount(t, sum.ExtraCurrencies, 3); got.Int64() != 7 {
		t.Fatalf("currency 3: %s", got)
	}

	// inputs must not be mutated
	if a.Coins.Nano().Int64() != 1500 || b.Coins.Nano().Int64() != 500 {
		t.Fatal("inputs mutated")
	}
	if got := extraAmount(t, a.ExtraCurrencies, 2); got.Int64() != 20 {
		t.Fatalf("input extra mutated: %s", got)
	}
}

func TestCurrencyCollectionAddNilExtraReusesOtherSide(t *testing.T) {
	extra := mustExtraDict(t, map[uint32]int64{9: 33})
	a := ccFromNano(1, nil)
	b := ccFromNano(2, extra)

	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	// C++ add_extra_currency returns the other dict as is when one side is null
	// (block.cpp:1775-1785): same root object, no re-serialization
	if sum.ExtraCurrencies != extra {
		t.Fatal("expected the non-empty side dictionary to be reused as is")
	}

	sum2, err := b.Add(a)
	if err != nil {
		t.Fatal(err)
	}
	if sum2.ExtraCurrencies != extra {
		t.Fatal("expected the non-empty side dictionary to be reused as is (reversed)")
	}

	both, err := a.Add(ccFromNano(5, nil))
	if err != nil {
		t.Fatal(err)
	}
	if both.ExtraCurrencies != nil {
		t.Fatal("nil + nil extra must stay nil")
	}
}

func TestCurrencyCollectionAddBigValues(t *testing.T) {
	big1 := new(big.Int).Lsh(big.NewInt(1), 200) // 2^200
	big2 := new(big.Int).Lsh(big.NewInt(1), 199)

	a := CurrencyCollection{Coins: FromNanoTON(big1)}
	b := CurrencyCollection{Coins: FromNanoTON(big2)}

	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	want := new(big.Int).Add(big1, big2)
	if sum.Coins.Nano().Cmp(want) != 0 {
		t.Fatalf("big grams sum mismatch: %s", sum.Coins.Nano())
	}

	// large extra currency amounts survive the VarUIntegerPos 32 re-encode
	bigExtra := cell.NewDict(32)
	amount := new(big.Int).Lsh(big.NewInt(7), 190)
	if err = bigExtra.SetIntKey(big.NewInt(5), cell.BeginCell().MustStoreBigVarUInt(amount, 32).EndCell()); err != nil {
		t.Fatal(err)
	}
	c := CurrencyCollection{Coins: FromNanoTON(big.NewInt(0)), ExtraCurrencies: bigExtra}
	sum, err = c.Add(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := extraAmount(t, sum.ExtraCurrencies, 5); got.Cmp(new(big.Int).Add(amount, amount)) != 0 {
		t.Fatalf("big extra sum mismatch: %s", got)
	}
}

func TestCurrencyCollectionSubUnderflow(t *testing.T) {
	// grams underflow
	_, err := ccFromNano(5, nil).Sub(ccFromNano(6, nil))
	if !errors.Is(err, ErrCurrencyCollectionUnderflow) {
		t.Fatalf("expected grams underflow, got %v", err)
	}

	// extra currency amount underflow
	a := ccFromNano(100, mustExtraDict(t, map[uint32]int64{1: 10}))
	b := ccFromNano(1, mustExtraDict(t, map[uint32]int64{1: 11}))
	if _, err = a.Sub(b); !errors.Is(err, ErrCurrencyCollectionUnderflow) {
		t.Fatalf("expected extra underflow, got %v", err)
	}

	// key present in subtrahend but missing in minuend
	c := ccFromNano(100, mustExtraDict(t, map[uint32]int64{1: 10}))
	d := ccFromNano(1, mustExtraDict(t, map[uint32]int64{2: 1}))
	if _, err = c.Sub(d); !errors.Is(err, ErrCurrencyCollectionUnderflow) {
		t.Fatalf("expected missing-key underflow, got %v", err)
	}

	// nil minuend extra with non-empty subtrahend extra
	// (C++ sub_extra_currency fails, block.cpp:1790-1792)
	e := ccFromNano(100, nil)
	f := ccFromNano(1, mustExtraDict(t, map[uint32]int64{2: 1}))
	if _, err = e.Sub(f); !errors.Is(err, ErrCurrencyCollectionUnderflow) {
		t.Fatalf("expected nil-extra underflow, got %v", err)
	}
}

func TestCurrencyCollectionSubDropsZeroEntries(t *testing.T) {
	a := ccFromNano(1000, mustExtraDict(t, map[uint32]int64{1: 10, 2: 20}))
	b := ccFromNano(400, mustExtraDict(t, map[uint32]int64{1: 10, 2: 5}))

	diff, err := a.Sub(b)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Coins.Nano().Int64() != 600 {
		t.Fatalf("grams diff mismatch: %s", diff.Coins.Nano())
	}
	// currency 1 reached exactly zero and must NOT be stored
	// (HashmapE::sub_values drops zero results, block-parse.cpp:550-567)
	if v, err := diff.ExtraCurrencies.LoadValueByIntKey(big.NewInt(1)); err == nil && v != nil {
		t.Fatal("zero-valued extra currency entry must be dropped")
	}
	if got := extraAmount(t, diff.ExtraCurrencies, 2); got.Int64() != 15 {
		t.Fatalf("currency 2: %s", got)
	}

	// all entries reaching zero -> resulting dict must be nil (empty)
	c := ccFromNano(1, mustExtraDict(t, map[uint32]int64{1: 10}))
	d := ccFromNano(0, mustExtraDict(t, map[uint32]int64{1: 10}))
	diff, err = c.Sub(d)
	if err != nil {
		t.Fatal(err)
	}
	if diff.ExtraCurrencies != nil {
		t.Fatal("fully drained extra currencies must produce a nil dictionary")
	}

	// subtracting itself yields the zero collection
	self := ccFromNano(77, mustExtraDict(t, map[uint32]int64{4: 4}))
	zero, err := self.Sub(self)
	if err != nil {
		t.Fatal(err)
	}
	if zero.Coins.Nano().Sign() != 0 || zero.ExtraCurrencies != nil {
		t.Fatalf("self subtraction must be zero, got %s / %v", zero.Coins.Nano(), zero.ExtraCurrencies)
	}
}

func TestCurrencyCollectionAddSubRoundTrip(t *testing.T) {
	a := ccFromNano(123456, mustExtraDict(t, map[uint32]int64{1: 11, 7: 70}))
	b := ccFromNano(654321, mustExtraDict(t, map[uint32]int64{7: 7, 9: 90}))

	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	back, err := sum.Sub(b)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equals(a) {
		t.Fatal("(a + b) - b != a")
	}

	ge, err := sum.GreaterOrEqual(a)
	if err != nil {
		t.Fatal(err)
	}
	if !ge {
		t.Fatal("a + b must be >= a")
	}
	ge, err = a.GreaterOrEqual(sum)
	if err != nil {
		t.Fatal(err)
	}
	if ge {
		t.Fatal("a must not be >= a + b")
	}
}

func TestCurrencyCollectionEquals(t *testing.T) {
	a := ccFromNano(5, mustExtraDict(t, map[uint32]int64{1: 1}))
	b := ccFromNano(5, mustExtraDict(t, map[uint32]int64{1: 1}))
	if !a.Equals(b) {
		t.Fatal("identical collections must be equal")
	}

	// empty dict object equals nil dict (both are the C++ null root)
	c := ccFromNano(5, cell.NewDict(32))
	d := ccFromNano(5, nil)
	if !c.Equals(d) || !d.Equals(c) {
		t.Fatal("empty dict must equal nil dict")
	}

	if a.Equals(ccFromNano(6, mustExtraDict(t, map[uint32]int64{1: 1}))) {
		t.Fatal("different grams must not be equal")
	}
	if a.Equals(ccFromNano(5, mustExtraDict(t, map[uint32]int64{1: 2}))) {
		t.Fatal("different extras must not be equal")
	}
	if a.Equals(d) {
		t.Fatal("extra vs no extra must not be equal")
	}
}

// TestCurrencyCollectionMainnetRoundTrip cross-checks the arithmetic against a
// real mainnet CurrencyCollection: the total fees aggregate of the whole
// ShardAccountBlocks dictionary of block 0:8000000000000000:71398501, and the
// per-account fee extras it is composed of.
func TestCurrencyCollectionMainnetRoundTrip(t *testing.T) {
	blk := loadMainnetBlock(t)

	var accounts ShardAccountBlocksAugDict
	if err := LoadFromCell(&accounts, blk.Extra.ShardAccountBlocks.MustBeginParse()); err != nil {
		t.Fatalf("failed to load shard account blocks: %v", err)
	}

	rootExtraCell := accounts.GetRootExtra()
	var total CurrencyCollection
	if err := LoadFromCell(&total, rootExtraCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to parse root fees: %v", err)
	}

	// round-trip: parse + serialize must reproduce the exact mainnet cell
	serialized, err := total.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "mainnet CurrencyCollection round-trip", serialized, rootExtraCell)

	// summing every per-account fee extra with Add must reproduce the mainnet
	// root value exactly (same fold the C++ fork evaluation performs)
	items, err := accounts.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	sum := CurrencyCollection{Coins: FromNanoTON(big.NewInt(0))}
	for _, item := range items {
		var fees CurrencyCollection
		if err = LoadFromCell(&fees, item.Extra.Copy()); err != nil {
			t.Fatal(err)
		}
		if sum, err = sum.Add(fees); err != nil {
			t.Fatal(err)
		}
	}
	if !sum.Equals(total) {
		t.Fatalf("sum of account fees %s != mainnet total %s", sum.Coins.Nano(), total.Coins.Nano())
	}
	if sum.Coins.Nano().Sign() <= 0 {
		t.Fatal("mainnet block total fees must be positive")
	}

	// and subtracting them all back must drain to zero
	rest := total
	for _, item := range items {
		var fees CurrencyCollection
		if err = LoadFromCell(&fees, item.Extra.Copy()); err != nil {
			t.Fatal(err)
		}
		if rest, err = rest.Sub(fees); err != nil {
			t.Fatal(err)
		}
	}
	if rest.Coins.Nano().Sign() != 0 {
		t.Fatalf("draining mainnet total left %s", rest.Coins.Nano())
	}
}
