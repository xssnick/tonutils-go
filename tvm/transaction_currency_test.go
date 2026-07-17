package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionCurrencyBalanceAddClampAndCopyEdges(t *testing.T) {
	var nilBalance *transactionCurrencyBalance
	nilBalance.add(transactionCurrencyTestBalance(1, map[uint32]uint64{7: 1}))
	nilBalance.clamp(transactionCurrencyTestBalance(1, nil))
	if got := nilBalance.copy(); got.grams.Sign() != 0 || len(got.extra) != 0 {
		t.Fatalf("nil balance copy = %+v, want zero balance", got)
	}

	balance := &transactionCurrencyBalance{
		grams: big.NewInt(10),
		extra: map[uint32]*big.Int{
			1: big.NewInt(4),
			2: big.NewInt(0),
			5: nil,
		},
	}
	balance.add(nil)
	balance.add(&transactionCurrencyBalance{
		extra: map[uint32]*big.Int{
			1: big.NewInt(3),
			2: nil,
			3: big.NewInt(0),
			4: big.NewInt(9),
		},
	})

	if balance.grams.Int64() != 10 {
		t.Fatalf("grams after add = %s, want 10", balance.grams)
	}
	transactionCurrencyRequireExtra(t, balance, 1, 7)
	transactionCurrencyRequireExtra(t, balance, 4, 9)
	if got := transactionCurrencyExtraAmount(balance, 2); got != 0 {
		t.Fatalf("zero extra survived add: got %d", got)
	}
	if _, ok := balance.extra[5]; ok {
		t.Fatal("nil extra survived add")
	}

	max := transactionCurrencyTestBalance(6, map[uint32]uint64{
		1: 5,
		3: 0,
		4: 20,
	})
	balance.clamp(max)
	if balance.grams.Int64() != 6 {
		t.Fatalf("grams after clamp = %s, want 6", balance.grams)
	}
	transactionCurrencyRequireExtra(t, balance, 1, 5)
	transactionCurrencyRequireExtra(t, balance, 4, 9)
	if got := transactionCurrencyExtraAmount(balance, 3); got != 0 {
		t.Fatalf("non-positive max extra survived clamp: got %d", got)
	}
}

func TestTransactionCurrencyExtraDictRoundTripAndErrors(t *testing.T) {
	if dict, err := ((*transactionCurrencyBalance)(nil)).extraDict(); err != nil || dict != nil {
		t.Fatalf("nil balance extra dict = %v, %v; want nil, nil", dict, err)
	}

	extra := map[uint32]*big.Int{
		1: big.NewInt(0),
		7: big.NewInt(11),
		9: nil,
	}
	dict, err := transactionStoreExtraCurrencies(extra)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := transactionLoadExtraCurrencies(dict)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[7].Uint64() != 11 {
		t.Fatalf("loaded extra = %v, want only id 7 = 11", loaded)
	}

	cloned, err := transactionCloneExtraCurrencies(dict)
	if err != nil {
		t.Fatal(err)
	}
	clonedLoaded, err := transactionLoadExtraCurrencies(cloned)
	if err != nil {
		t.Fatal(err)
	}
	if len(clonedLoaded) != 1 || clonedLoaded[7].Uint64() != 11 {
		t.Fatalf("cloned extra = %v, want only id 7 = 11", clonedLoaded)
	}

	if _, err = transactionStoreExtraCurrencies(map[uint32]*big.Int{1: big.NewInt(-1)}); err == nil {
		t.Fatal("expected negative extra currency error")
	}
	tooLarge := new(big.Int).Lsh(big.NewInt(1), 248)
	if _, err = transactionStoreExtraCurrencies(map[uint32]*big.Int{1: tooLarge}); err == nil {
		t.Fatal("expected oversized extra currency error")
	}

	malformed := cell.NewDict(32)
	if err = malformed.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreBigVarUInt(big.NewInt(11), 32).MustStoreUInt(1, 1).EndCell()); err != nil {
		t.Fatal(err)
	}
	if _, err = transactionLoadExtraCurrencies(malformed); err == nil {
		t.Fatal("expected trailing extra currency data error")
	}
	if _, err = transactionCurrencyFromParts(big.NewInt(1), malformed); err == nil {
		t.Fatal("expected currency-from-parts malformed extra error")
	}
	if _, err = transactionCloneExtraCurrencies(malformed); err == nil {
		t.Fatal("expected clone malformed extra error")
	}
	if _, err = transactionAddExtraCurrencies(nil, malformed); err == nil {
		t.Fatal("expected add malformed extra error")
	}
}

func TestTransactionCurrencyAddExtraCurrenciesAndMinBig(t *testing.T) {
	left := transactionCurrencyTestDict(t, map[uint32]uint64{1: 5, 7: 11})
	right := transactionCurrencyTestDict(t, map[uint32]uint64{1: 6, 9: 13})

	sum, err := transactionAddExtraCurrencies(left, right)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := transactionLoadExtraCurrencies(sum)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 || loaded[1].Uint64() != 11 || loaded[7].Uint64() != 11 || loaded[9].Uint64() != 13 {
		t.Fatalf("sum extra = %v, want ids 1/7/9", loaded)
	}

	maxHalf := new(big.Int).Lsh(big.NewInt(1), 247)
	largeLeft, err := transactionStoreExtraCurrencies(map[uint32]*big.Int{1: maxHalf})
	if err != nil {
		t.Fatal(err)
	}
	largeRight, err := transactionStoreExtraCurrencies(map[uint32]*big.Int{1: maxHalf})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = transactionAddExtraCurrencies(largeLeft, largeRight); err == nil {
		t.Fatal("expected extra currency sum overflow error")
	}

	cases := []struct {
		a, b *big.Int
		want int64
	}{
		{a: nil, b: big.NewInt(9), want: 0},
		{a: big.NewInt(7), b: nil, want: 7},
		{a: big.NewInt(5), b: big.NewInt(8), want: 5},
		{a: big.NewInt(9), b: big.NewInt(3), want: 3},
	}
	for _, tc := range cases {
		got := transactionMinBig(tc.a, tc.b)
		if got.Int64() != tc.want {
			t.Fatalf("transactionMinBig(%v, %v) = %s, want %d", tc.a, tc.b, got, tc.want)
		}
		if tc.a != nil && got == tc.a {
			t.Fatal("transactionMinBig returned aliased left operand")
		}
		if tc.b != nil && got == tc.b {
			t.Fatal("transactionMinBig returned aliased right operand")
		}
	}
}

func FuzzTransactionCurrencyBalanceRoundTripAndClamp(f *testing.F) {
	f.Add(uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0))
	f.Add(uint64(10), uint64(3), uint64(4), uint64(20), uint64(2), uint64(9))
	f.Add(uint64(100), uint64(30), uint64(0), uint64(25), uint64(40), uint64(1))

	f.Fuzz(func(t *testing.T, grams, extra1, extra2, addGrams, addExtra1, addExtra3 uint64) {
		grams %= 1_000
		extra1 %= 100
		extra2 %= 100
		addGrams %= 1_000
		addExtra1 %= 100
		addExtra3 %= 100

		start := transactionCurrencyTestBalance(grams, map[uint32]uint64{
			1: extra1,
			2: extra2,
		})
		add := transactionCurrencyTestBalance(addGrams, map[uint32]uint64{
			1: addExtra1,
			3: addExtra3,
		})

		start.add(add)
		dict, err := start.extraDict()
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := transactionCurrencyFromParts(start.grams, dict)
		if err != nil {
			t.Fatal(err)
		}
		if roundTrip.grams.Cmp(start.grams) != 0 {
			t.Fatalf("round-trip grams = %s, want %s", roundTrip.grams, start.grams)
		}
		for _, id := range []uint32{1, 2, 3} {
			if transactionCurrencyExtraAmount(roundTrip, id) != transactionCurrencyExtraAmount(start, id) {
				t.Fatalf("round-trip extra id %d = %d, want %d", id, transactionCurrencyExtraAmount(roundTrip, id), transactionCurrencyExtraAmount(start, id))
			}
		}

		max := transactionCurrencyTestBalance(grams/2, map[uint32]uint64{
			1: extra1 / 2,
			3: addExtra3 / 2,
		})
		start.clamp(max)
		if start.grams.Cmp(max.grams) > 0 {
			t.Fatalf("clamped grams = %s, max %s", start.grams, max.grams)
		}
		if transactionCurrencyExtraAmount(start, 1) > transactionCurrencyExtraAmount(max, 1) {
			t.Fatalf("clamped extra 1 = %d, max %d", transactionCurrencyExtraAmount(start, 1), transactionCurrencyExtraAmount(max, 1))
		}
		if transactionCurrencyExtraAmount(start, 2) != 0 {
			t.Fatalf("extra 2 survived clamp without max entry: %d", transactionCurrencyExtraAmount(start, 2))
		}
		if transactionCurrencyExtraAmount(start, 3) > transactionCurrencyExtraAmount(max, 3) {
			t.Fatalf("clamped extra 3 = %d, max %d", transactionCurrencyExtraAmount(start, 3), transactionCurrencyExtraAmount(max, 3))
		}

		beforeSub := start.copy()
		if !start.sub(beforeSub) {
			t.Fatal("subtracting copied balance should succeed")
		}
		if start.grams.Sign() != 0 || !start.extraEmpty() {
			t.Fatalf("subtracted balance = grams %s extra %v, want zero", start.grams, start.extra)
		}
	})
}

func transactionCurrencyTestBalance(grams uint64, extra map[uint32]uint64) *transactionCurrencyBalance {
	out := &transactionCurrencyBalance{
		grams: new(big.Int).SetUint64(grams),
		extra: map[uint32]*big.Int{},
	}
	for id, amount := range extra {
		if amount > 0 {
			out.extra[id] = new(big.Int).SetUint64(amount)
		}
	}
	return out
}

func transactionCurrencyTestDict(t *testing.T, extra map[uint32]uint64) *cell.Dictionary {
	t.Helper()

	var dict *cell.Dictionary
	for id, amount := range extra {
		if amount == 0 {
			continue
		}
		if dict == nil {
			dict = cell.NewDict(32)
		}
		if err := dict.SetIntKey(new(big.Int).SetUint64(uint64(id)), cell.BeginCell().MustStoreBigVarUInt(new(big.Int).SetUint64(amount), 32).EndCell()); err != nil {
			t.Fatalf("failed to store extra currency %d: %v", id, err)
		}
	}
	return dict
}

func transactionCurrencyRequireExtra(t *testing.T, balance *transactionCurrencyBalance, id uint32, want uint64) {
	t.Helper()

	if got := transactionCurrencyExtraAmount(balance, id); got != want {
		t.Fatalf("extra id %d = %d, want %d", id, got, want)
	}
}

func transactionCurrencyExtraAmount(balance *transactionCurrencyBalance, id uint32) uint64 {
	amount := balance.extra[id]
	if amount == nil {
		return 0
	}
	return amount.Uint64()
}
