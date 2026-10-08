package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type valueFlowRoundTripCase struct {
	name   string
	burned uint64
	tag    uint64
}

func TestValueFlowRoundTripAndVersionSelection(t *testing.T) {
	cases := []valueFlowRoundTripCase{
		{name: "v1", tag: valueFlowV1Tag},
		{name: "v2", burned: 7, tag: valueFlowV2Tag},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			flow := balancedValueFlow(testCase.burned)
			encoded, err := flow.ToCell()
			if err != nil {
				t.Fatal(err)
			}

			tag, err := encoded.MustBeginParse().LoadUInt(32)
			if err != nil {
				t.Fatal(err)
			}
			if tag != testCase.tag {
				t.Fatalf("tag = %08x, want %08x", tag, testCase.tag)
			}

			var parsed ValueFlow
			if err = parsed.LoadFromCell(encoded.MustBeginParse()); err != nil {
				t.Fatal(err)
			}
			if err = parsed.Validate(); err != nil {
				t.Fatal(err)
			}
			assertValueFlowEqual(t, parsed, flow)

			back, err := parsed.ToCell()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(back.Hash(), encoded.Hash()) {
				t.Fatal("value flow round-trip changed the cell hash")
			}
		})
	}
}

func TestValueFlowUsesV1ForZeroBurnedCollection(t *testing.T) {
	flow := balancedValueFlow(0)
	flow.Burned.ExtraCurrencies = cell.NewDict(32)

	encoded, err := flow.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	tag, err := encoded.MustBeginParse().LoadUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	if tag != valueFlowV1Tag {
		t.Fatalf("tag = %08x, want v1", tag)
	}
}

func TestValueFlowRejectsInvalidData(t *testing.T) {
	flow := balancedValueFlow(0)
	flow.Exported = valueFlowCoins(11)
	if err := flow.Validate(); err == nil {
		t.Fatal("unbalanced value flow was accepted")
	}
	// Serialization mirrors C++ ValueFlow::store, which packs any
	// well-formed flow; only Validate enforces the balance equation.
	unbalanced, err := flow.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var reparsed ValueFlow
	if err = reparsed.LoadFromCell(unbalanced.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	assertValueFlowEqual(t, reparsed, flow)

	unknownTag := cell.BeginCell().MustStoreUInt(0x12345678, 32).EndCell()
	var parsed ValueFlow
	if err := parsed.LoadFromCell(unknownTag.MustBeginParse()); err == nil {
		t.Fatal("unknown value flow tag was accepted")
	}

	valid, err := balancedValueFlow(0).ToCell()
	if err != nil {
		t.Fatal(err)
	}
	trailingRoot := cell.BeginCell().MustStoreBuilder(valid.ToBuilder()).MustStoreBoolBit(true).EndCell()
	if err = parsed.LoadFromCell(trailingRoot.MustBeginParse()); err == nil {
		t.Fatal("value flow with trailing root data was accepted")
	}

	validSlice := valid.MustBeginParse()
	validTag, err := validSlice.LoadUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	first, err := validSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	feesCollected, err := loadValueFlowCurrency(validSlice, "fees collected")
	if err != nil {
		t.Fatal(err)
	}
	second, err := validSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	badFirst := cell.BeginCell().MustStoreBuilder(first.ToBuilder()).MustStoreBoolBit(true).EndCell()
	badNested := cell.BeginCell().MustStoreUInt(validTag, 32).MustStoreRef(badFirst)
	if err = storeCurrencyCollection(badNested, feesCollected); err != nil {
		t.Fatal(err)
	}
	badNested.MustStoreRef(second)
	if err = parsed.LoadFromCell(badNested.EndCell().MustBeginParse()); err == nil {
		t.Fatal("value flow with trailing nested data was accepted")
	}

	zeroFlow, err := (ValueFlow{}).ToCell()
	if err != nil {
		t.Fatal(err)
	}
	zeroSlice := zeroFlow.MustBeginParse()
	zeroTag, err := zeroSlice.LoadUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = zeroSlice.LoadRefCell(); err != nil {
		t.Fatal(err)
	}
	zeroFees, err := loadValueFlowCurrency(zeroSlice, "fees collected")
	if err != nil {
		t.Fatal(err)
	}
	zeroSecond, err := zeroSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	paddedFirst := cell.BeginCell().MustStoreUInt(1, 4).MustStoreUInt(0, 8).MustStoreBoolBit(false)
	for range 3 {
		if err = storeCurrencyCollection(paddedFirst, CurrencyCollection{}); err != nil {
			t.Fatal(err)
		}
	}
	paddedCoins := cell.BeginCell().MustStoreUInt(zeroTag, 32).MustStoreRef(paddedFirst.EndCell())
	if err = storeCurrencyCollection(paddedCoins, zeroFees); err != nil {
		t.Fatal(err)
	}
	paddedCoins.MustStoreRef(zeroSecond)
	if err = parsed.LoadFromCell(paddedCoins.EndCell().MustBeginParse()); err == nil {
		t.Fatal("value flow with padded coins was accepted")
	}
}

func TestValueFlowRejectsInvalidExtraCurrency(t *testing.T) {
	extra := cell.NewDict(32)
	if err := extra.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(0, 5).EndCell()); err != nil {
		t.Fatal(err)
	}

	flow := balancedValueFlow(0)
	flow.FromPrevBlock.ExtraCurrencies = extra
	flow.ToNextBlock.ExtraCurrencies = extra
	if err := flow.Validate(); err == nil {
		t.Fatal("zero-valued extra currency was accepted")
	}
}

func TestValueFlowMainnetGoldenRoundTrip(t *testing.T) {
	block := loadMainnetBlock(t)
	var flow ValueFlow
	if err := flow.LoadFromCell(block.ValueFlow.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if err := flow.Validate(); err != nil {
		t.Fatal(err)
	}

	encoded, err := flow.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "mainnet value flow", encoded, block.ValueFlow)
}

func balancedValueFlow(burned uint64) ValueFlow {
	return ValueFlow{
		FromPrevBlock: valueFlowCoins(100),
		ToNextBlock:   valueFlowCoins(110),
		Imported:      valueFlowCoins(20),
		Exported:      valueFlowCoins(10),
		FeesCollected: valueFlowCoins(18 - burned),
		FeesImported:  valueFlowCoins(3),
		Recovered:     valueFlowCoins(6),
		Created:       valueFlowCoins(4),
		Minted:        valueFlowCoins(5),
		Burned:        valueFlowCoins(burned),
	}
}

func valueFlowCoins(value uint64) CurrencyCollection {
	return CurrencyCollection{Coins: FromNanoTONU(value)}
}

func assertValueFlowEqual(t *testing.T, got, expected ValueFlow) {
	t.Helper()
	names := [10]string{
		"from previous block",
		"to next block",
		"imported",
		"exported",
		"fees collected",
		"fees imported",
		"recovered",
		"created",
		"minted",
		"burned",
	}
	gotFields := [10]CurrencyCollection{
		got.FromPrevBlock,
		got.ToNextBlock,
		got.Imported,
		got.Exported,
		got.FeesCollected,
		got.FeesImported,
		got.Recovered,
		got.Created,
		got.Minted,
		got.Burned,
	}
	expectedFields := [10]CurrencyCollection{
		expected.FromPrevBlock,
		expected.ToNextBlock,
		expected.Imported,
		expected.Exported,
		expected.FeesCollected,
		expected.FeesImported,
		expected.Recovered,
		expected.Created,
		expected.Minted,
		expected.Burned,
	}
	for i := range names {
		if !gotFields[i].Equals(expectedFields[i]) {
			t.Fatalf("%s differs after round-trip", names[i])
		}
	}
}
