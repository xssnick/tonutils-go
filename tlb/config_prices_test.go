package tlb

import "testing"

func TestConfigStoragePricesRoundTripAndCompute(t *testing.T) {
	src := ConfigStoragePrices{
		ValidSince:  100,
		BitPrice:    3,
		CellPrice:   5,
		MCBitPrice:  7,
		MCCellPrice: 11,
	}

	cell, err := ToCell(&src)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	var parsed ConfigStoragePrices
	if err = LoadFromCell(&parsed, cell.MustBeginParse()); err != nil {
		t.Fatalf("LoadFromCell failed: %v", err)
	}

	if parsed != src {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", parsed, src)
	}

	if got := parsed.ComputeStorageFee(false, 10, 20, 30).String(); got != "1" {
		t.Fatalf("unexpected storage fee: %s", got)
	}
}

func TestConfigMsgForwardPricesRoundTripAndCompute(t *testing.T) {
	src := ConfigMsgForwardPrices{
		LumpPrice: 1000,
		BitPrice:  200,
		CellPrice: 300,
		IHRFactor: 500,
		FirstFrac: 1000,
		NextFrac:  2000,
	}

	cell, err := ToCell(&src)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	var parsed ConfigMsgForwardPrices
	if err = LoadFromCell(&parsed, cell.MustBeginParse()); err != nil {
		t.Fatalf("LoadFromCell failed: %v", err)
	}

	if parsed != src {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", parsed, src)
	}

	if got := parsed.ComputeForwardFee(30, 20).String(); got != "1001" {
		t.Fatalf("unexpected forward fee: %s", got)
	}
}

func TestConfigGasLimitsPricesRoundTripAndCompute(t *testing.T) {
	src := ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            100,
		FlatGasPrice:            77,
		HasSeparateSpecialLimit: true,
		GasPrice:                200,
		GasLimit:                1000,
		SpecialGasLimit:         1200,
		GasCredit:               50,
		BlockGasLimit:           2000,
		FreezeDueLimit:          3000,
		DeleteDueLimit:          4000,
	}

	cell, err := ToCell(&src)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	var parsed ConfigGasLimitsPrices
	if err = LoadFromCell(&parsed, cell.MustBeginParse()); err != nil {
		t.Fatalf("LoadFromCell failed: %v", err)
	}

	if parsed != src {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", parsed, src)
	}

	if got := parsed.ComputeGasPrice(150).String(); got != "78" {
		t.Fatalf("unexpected gas price: %s", got)
	}
}
