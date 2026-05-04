package tlb

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestBlockchainConfigGetParam(t *testing.T) {
	root := mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
		17: cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
	})

	cfg := BlockchainConfig{Root: root}

	param, err := cfg.GetParam(17)
	if err != nil {
		t.Fatalf("GetParam failed: %v", err)
	}
	if param == nil {
		t.Fatal("GetParam returned nil cell")
	}

	val, err := param.BeginParse().LoadUInt(8)
	if err != nil {
		t.Fatalf("LoadUInt failed: %v", err)
	}
	if val != 0xAB {
		t.Fatalf("unexpected param value: %x", val)
	}

	missing, err := cfg.GetParam(18)
	if err == nil {
		t.Fatal("GetParam(missing) should fail")
	}
	if missing != nil {
		t.Fatal("expected missing param to return nil cell")
	}
}

func TestBlockchainConfigGetParamNilRoot(t *testing.T) {
	cfg := BlockchainConfig{}

	param, err := cfg.GetParam(17)
	if err == nil {
		t.Fatal("GetParam(nil root) should fail")
	}
	if param != nil {
		t.Fatal("GetParam(nil root) should return nil cell")
	}
}

func TestBlockchainConfigGetGlobalVersion(t *testing.T) {
	t.Run("default when absent", func(t *testing.T) {
		cfg := BlockchainConfig{Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{})}

		version, err := cfg.GetGlobalVersion()
		if err != nil {
			t.Fatalf("GetGlobalVersion failed: %v", err)
		}
		if version.Version != 0 || version.Capabilities != 0 {
			t.Fatalf("unexpected default global version: %+v", version)
		}
	})

	t.Run("parsed value", func(t *testing.T) {
		paramCell, err := ToCell(&GlobalVersion{
			Version:      13,
			Capabilities: 77,
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		cfg := BlockchainConfig{
			Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
				ConfigParamGlobalVersion: paramCell,
			}),
		}

		version, err := cfg.GetGlobalVersion()
		if err != nil {
			t.Fatalf("GetGlobalVersion failed: %v", err)
		}
		if version.Version != 13 || version.Capabilities != 77 {
			t.Fatalf("unexpected global version: %+v", version)
		}
	})
}

func TestBlockchainConfigAddressFallbacks(t *testing.T) {
	configAddr := make([]byte, 32)
	electorAddr := make([]byte, 32)
	configAddr[0] = 1
	electorAddr[0] = 2

	configCell, err := ToCell(&ConfigParamAddress{Address: configAddr})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}
	electorCell, err := ToCell(&ConfigParamAddress{Address: electorAddr})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	cfg := BlockchainConfig{
		Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
			ConfigParamConfigAddress:  configCell,
			ConfigParamElectorAddress: electorCell,
		}),
	}

	minter, err := cfg.GetMinterAddress()
	if err != nil {
		t.Fatalf("GetMinterAddress failed: %v", err)
	}
	if minter[0] != 1 {
		t.Fatalf("unexpected minter fallback: %x", minter)
	}

	collector, err := cfg.GetFeeCollectorAddress()
	if err != nil {
		t.Fatalf("GetFeeCollectorAddress failed: %v", err)
	}
	if collector[0] != 2 {
		t.Fatalf("unexpected fee collector fallback: %x", collector)
	}
}

func TestBlockchainConfigGetBurningConfig(t *testing.T) {
	t.Run("default when absent", func(t *testing.T) {
		cfg := BlockchainConfig{Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{})}

		burning, err := cfg.GetBurningConfig()
		if err != nil {
			t.Fatalf("GetBurningConfig failed: %v", err)
		}
		if burning.FeeBurnNum != 0 || burning.FeeBurnDenom != 0 || burning.BlackholeAddr != nil {
			t.Fatalf("unexpected default burning config: %+v", burning)
		}
	})

	t.Run("parsed value", func(t *testing.T) {
		addr := make([]byte, 32)
		addr[31] = 0xAA

		paramCell, err := ToCell(&BurningConfig{
			BlackholeAddr: addr,
			FeeBurnNum:    1,
			FeeBurnDenom:  3,
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		cfg := BlockchainConfig{
			Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
				ConfigParamBurningConfig: paramCell,
			}),
		}

		burning, err := cfg.GetBurningConfig()
		if err != nil {
			t.Fatalf("GetBurningConfig failed: %v", err)
		}
		if burning.FeeBurnNum != 1 || burning.FeeBurnDenom != 3 || len(burning.BlackholeAddr) != 32 || burning.BlackholeAddr[31] != 0xAA {
			t.Fatalf("unexpected burning config: %+v", burning)
		}
	})
}

func TestBlockchainConfigGetGasPrices(t *testing.T) {
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

	cellValue, err := ToCell(&src)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	cfg := BlockchainConfig{
		Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
			ConfigParamGasPricesBasechain: cellValue,
		}),
	}

	got, err := cfg.GetGasPrices(false)
	if err != nil {
		t.Fatalf("GetGasPrices failed: %v", err)
	}
	if got == nil || *got != src {
		t.Fatalf("unexpected gas prices: got=%+v want=%+v", got, src)
	}
}

func TestBlockchainConfigComputeStorageFeeAcrossIntervals(t *testing.T) {
	prices := cell.NewDict(32)

	entryA, err := ToCell(&ConfigStoragePrices{
		ValidSince: 100,
		CellPrice:  1 << 16,
	})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}
	if err = prices.SetIntKey(big.NewInt(100), entryA); err != nil {
		t.Fatalf("SetIntKey failed: %v", err)
	}

	entryB, err := ToCell(&ConfigStoragePrices{
		ValidSince: 150,
		CellPrice:  2 << 16,
	})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}
	if err = prices.SetIntKey(big.NewInt(150), entryB); err != nil {
		t.Fatalf("SetIntKey failed: %v", err)
	}

	paramCell, err := ToCell(&StoragePricesConfig{Prices: prices})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	cfg := BlockchainConfig{
		Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
			ConfigParamStoragePrices: paramCell,
		}),
	}

	fee, err := cfg.ComputeStorageFee(false, 120, 180, 0, 1)
	if err != nil {
		t.Fatalf("ComputeStorageFee failed: %v", err)
	}
	if got := fee.String(); got != "90" {
		t.Fatalf("unexpected storage fee: got=%s want=90", got)
	}
}

func TestBlockchainConfigGetMsgForwardPrices(t *testing.T) {
	src := ConfigMsgForwardPrices{
		LumpPrice: 1000,
		BitPrice:  200,
		CellPrice: 300,
		IHRFactor: 500,
		FirstFrac: 1000,
		NextFrac:  2000,
	}

	cellValue, err := ToCell(&src)
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	cfg := BlockchainConfig{
		Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
			ConfigParamMsgForwardPricesBasechain: cellValue,
		}),
	}

	got, err := cfg.GetMsgForwardPrices(false)
	if err != nil {
		t.Fatalf("GetMsgForwardPrices failed: %v", err)
	}
	if got == nil || *got != src {
		t.Fatalf("unexpected msg prices: got=%+v want=%+v", got, src)
	}
}

func TestBlockchainConfigGetStoragePrices(t *testing.T) {
	first := ConfigStoragePrices{
		ValidSince:  100,
		BitPrice:    3,
		CellPrice:   5,
		MCBitPrice:  7,
		MCCellPrice: 11,
	}
	second := ConfigStoragePrices{
		ValidSince:  200,
		BitPrice:    13,
		CellPrice:   17,
		MCBitPrice:  19,
		MCCellPrice: 23,
	}

	cfg := BlockchainConfig{
		Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
			ConfigParamStoragePrices: mustBlockchainConfigStoragePricesDict(t, first, second),
		}),
	}

	got, err := cfg.GetStoragePrices(150)
	if err != nil {
		t.Fatalf("GetStoragePrices failed: %v", err)
	}
	if got == nil || *got != first {
		t.Fatalf("unexpected storage prices for now=150: got=%+v want=%+v", got, first)
	}

	got, err = cfg.GetStoragePrices(250)
	if err != nil {
		t.Fatalf("GetStoragePrices failed: %v", err)
	}
	if got == nil || *got != second {
		t.Fatalf("unexpected storage prices for now=250: got=%+v want=%+v", got, second)
	}

	got, err = cfg.GetStoragePrices(50)
	if err == nil {
		t.Fatal("GetStoragePrices(now before first entry) should fail")
	}
	if got != nil {
		t.Fatalf("expected nil storage prices on error, got=%+v", got)
	}
}

func TestBlockchainConfigGetSizeLimitsConfig(t *testing.T) {
	t.Run("default when absent", func(t *testing.T) {
		cfg := BlockchainConfig{Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{})}

		limits, err := cfg.GetSizeLimitsConfig()
		if err != nil {
			t.Fatalf("GetSizeLimitsConfig failed: %v", err)
		}

		v2, ok := limits.Config.(SizeLimitsConfigV2)
		if !ok {
			t.Fatalf("unexpected default size limits type: %T", limits.Config)
		}
		if v2.MaxMsgBits != defaultSizeLimitsMaxMsgBits || v2.MaxMsgCells != defaultSizeLimitsMaxMsgCells || v2.MaxAccFixedPrefixLength != defaultSizeLimitsMaxAccFixedPrefixLength {
			t.Fatalf("unexpected default size limits: %+v", v2)
		}
	})

	t.Run("parsed v1", func(t *testing.T) {
		paramCell, err := ToCell(&SizeLimitsConfigV1{
			MaxMsgBits:      100,
			MaxMsgCells:     101,
			MaxLibraryCells: 102,
			MaxVMDataDepth:  103,
			MaxExtMsgSize:   104,
			MaxExtMsgDepth:  105,
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		cfg := BlockchainConfig{
			Root: mustBlockchainConfigRoot(t, map[uint32]*cell.Cell{
				ConfigParamSizeLimits: paramCell,
			}),
		}

		limits, err := cfg.GetSizeLimitsConfig()
		if err != nil {
			t.Fatalf("GetSizeLimitsConfig failed: %v", err)
		}

		v1, ok := limits.Config.(SizeLimitsConfigV1)
		if !ok {
			t.Fatalf("unexpected parsed size limits type: %T", limits.Config)
		}
		if v1.MaxMsgBits != 100 || v1.MaxExtMsgDepth != 105 {
			t.Fatalf("unexpected parsed size limits: %+v", v1)
		}
	})
}

func mustBlockchainConfigRoot(t *testing.T, entries map[uint32]*cell.Cell) *cell.Cell {
	t.Helper()

	if len(entries) == 0 {
		dummy, err := ToCell(&ConfigParamAddress{Address: make([]byte, 32)})
		if err != nil {
			t.Fatalf("failed to build dummy config param: %v", err)
		}
		entries = map[uint32]*cell.Cell{
			ConfigParamConfigAddress: dummy,
		}
	}

	dict := cell.NewDict(32)
	for id, value := range entries {
		key := cell.BeginCell().MustStoreUInt(uint64(id), 32).EndCell()
		if err := dict.SetBuilder(key, cell.BeginCell().MustStoreRef(value)); err != nil {
			t.Fatalf("failed to build config root: %v", err)
		}
	}

	return dict.AsCell()
}

func mustBlockchainConfigStoragePricesDict(t *testing.T, values ...ConfigStoragePrices) *cell.Cell {
	t.Helper()

	dict := cell.NewDict(32)
	for _, value := range values {
		valueCell, err := ToCell(&value)
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		if err = dict.SetIntKey(new(big.Int).SetUint64(uint64(value.ValidSince)), valueCell); err != nil {
			t.Fatalf("failed to add storage price %d: %v", value.ValidSince, err)
		}
	}

	return dict.AsCell()
}
