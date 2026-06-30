package tvm

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionCeilShiftRight(t *testing.T) {
	tests := []struct {
		name  string
		value int64
		bits  uint
		want  int64
	}{
		{name: "zero", value: 0, bits: 8, want: 0},
		{name: "no_shift", value: 7, bits: 0, want: 7},
		{name: "positive_exact", value: 256, bits: 8, want: 1},
		{name: "positive_rounded", value: 257, bits: 8, want: 2},
		{name: "negative_exact", value: -512, bits: 8, want: -2},
		{name: "negative_rounded", value: -513, bits: 8, want: -2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transactionCeilShiftRight(big.NewInt(tt.value), tt.bits)
			if got.Cmp(big.NewInt(tt.want)) != 0 {
				t.Fatalf("ceil shift = %s, want %d", got.String(), tt.want)
			}
		})
	}

	if got := transactionCeilShiftRight(nil, 8); got.Sign() != 0 {
		t.Fatalf("nil ceil shift = %s, want 0", got.String())
	}

	in := big.NewInt(257)
	got := transactionCeilShiftRight(in, 8)
	if got.Int64() != 2 {
		t.Fatalf("ceil shift = %s, want 2", got.String())
	}
	if in.Int64() != 257 {
		t.Fatalf("ceil shift mutated input to %s", in.String())
	}
}

func TestTransactionGasBoundaryHelpers(t *testing.T) {
	if got := transactionGasInt(uint64(math.MaxInt64) + 1); got != math.MaxInt64 {
		t.Fatalf("gas int overflow = %d, want %d", got, int64(math.MaxInt64))
	}

	prices := &tlb.ConfigGasLimitsPrices{
		FlatGasLimit:            10,
		FlatGasPrice:            100,
		HasSeparateSpecialLimit: true,
		GasPrice:                0,
		GasLimit:                1000,
		SpecialGasLimit:         500,
		GasCredit:               2000,
	}
	if got := transactionGasBoughtFor(nil, big.NewInt(100)); got != 0 {
		t.Fatalf("nil gas prices bought = %d, want 0", got)
	}
	if got := transactionGasBoughtForAccount(transactionConfig{}, nil, big.NewInt(100), tonopsTestAddr, 0); got != 0 {
		t.Fatalf("nil account gas prices bought = %d, want 0", got)
	}
	if got := transactionGasBoughtForLimit(prices, nil, prices.GasLimit); got != 0 {
		t.Fatalf("nil nanograms gas bought = %d, want 0", got)
	}
	if got := transactionGasBoughtForLimit(prices, big.NewInt(-1), prices.GasLimit); got != 0 {
		t.Fatalf("negative nanograms gas bought = %d, want 0", got)
	}
	if got := transactionGasBoughtForLimit(prices, big.NewInt(99), prices.GasLimit); got != 0 {
		t.Fatalf("below flat price gas bought = %d, want 0", got)
	}
	if got := transactionGasBoughtForLimit(prices, big.NewInt(100), 777); got != 777 {
		t.Fatalf("zero gas price gas bought = %d, want 777", got)
	}
	if got := transactionGasFlatPrice(prices); got != 100 {
		t.Fatalf("flat gas price = %d, want 100", got)
	}
	if got := transactionMaxGasThresholdForLimit(nil, 100); got.Sign() != 0 {
		t.Fatalf("nil max gas threshold = %s, want 0", got)
	}
	if got := transactionMaxGasThresholdForLimit(prices, prices.FlatGasLimit); got.Uint64() != prices.FlatGasPrice {
		t.Fatalf("flat max gas threshold = %s, want %d", got, prices.FlatGasPrice)
	}

	fallback := transactionMessageGas(TransactionEmulationConfig{}, transactionConfig{}, tonopsTestAddr, big.NewInt(0), nil, tlb.MsgTypeInternal, false)
	if fallback.Limit != 0 || fallback.Remaining != 0 {
		t.Fatalf("nil internal message balance gas = %+v, want zero limit fallback", fallback)
	}
	hugeBalance := new(big.Int).Lsh(big.NewInt(1), 70)
	fallback = transactionMessageGas(TransactionEmulationConfig{}, transactionConfig{}, tonopsTestAddr, big.NewInt(0), hugeBalance, tlb.MsgTypeInternal, false)
	if fallback.Limit != 0 || fallback.Remaining != 0 {
		t.Fatalf("oversized internal message balance gas = %+v, want zero limit fallback", fallback)
	}

	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, *prices),
	})
	gas := transactionMessageGas(TransactionEmulationConfig{}, cfg, tonopsTestAddr, big.NewInt(1000), big.NewInt(1000), tlb.MsgTypeExternalIn, true)
	if gas.Credit != transactionGasInt(prices.SpecialGasLimit) || gas.Limit != transactionGasInt(prices.SpecialGasLimit) {
		t.Fatalf("special external gas = %+v, want credit/limit clamped to special limit", gas)
	}
}

func TestTransactionGlobalVersionFallbackContracts(t *testing.T) {
	if _, err := newTransactionConfig(nil); !errors.Is(err, errConfigRootRequired) {
		t.Fatalf("new transaction config without root error = %v, want %v", err, errConfigRootRequired)
	}

	noRoot := transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{})
	if noRoot.hasGlobalVersion {
		t.Fatal("no-root transaction config should not expose global version")
	}
	if !errors.Is(noRoot.globalVersionErr, errConfigRootRequired) {
		t.Fatalf("no-root transaction config global version error = %v, want %v", noRoot.globalVersionErr, errConfigRootRequired)
	}

	for _, cfg := range []transactionConfig{
		{},
		transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{Root: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: cell.BeginCell().MustStoreUInt(0, 1).EndCell(),
		})}),
	} {
		if got := cfg.globalVersion(); got != 0 {
			t.Fatalf("transaction global version fallback = %d, want legacy v0", got)
		}
		if cfg.hasCapability(1) {
			t.Fatal("missing global version should not expose capabilities")
		}
		if cfg.specialGasFull() {
			t.Fatal("missing global version should not enable v5 special-gas-full")
		}
	}
}

func TestTransactionSizeLimitsV1Config(t *testing.T) {
	limitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV1{
		MaxMsgBits:      123,
		MaxMsgCells:     45,
		MaxLibraryCells: 67,
		MaxVMDataDepth:  89,
		MaxExtMsgSize:   321,
		MaxExtMsgDepth:  11,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamSizeLimits: limitsCell,
	})
	limits := transactionGetSizeLimits(cfg)
	if limits.maxMsgBits != 123 || limits.maxMsgCells != 45 || limits.maxLibraryCells != 67 || limits.maxExtMsgDepth != 11 {
		t.Fatalf("v1 size limits = %+v", limits)
	}
	if limits.maxAccStateCells != 1<<16 || limits.maxMCAccStateCells != 1<<11 || limits.maxMsgExtraCurrencies != 2 {
		t.Fatalf("v1 defaults were not preserved: %+v", limits)
	}
}

func TestTransactionMessageTailAndActionUsageEdges(t *testing.T) {
	usage, err := transactionMessageTailUsage(nil)
	if err != nil {
		t.Fatal(err)
	}
	if usage != (transactionUsage{}) {
		t.Fatalf("nil tail usage = %+v, want zero", usage)
	}
	usage, err = transactionOutboundInternalMessageActionUsage(transactionConfig{}, &tlb.InternalMessage{}, nil, transactionOutboundLayout{})
	if err != nil {
		t.Fatal(err)
	}
	if usage != (transactionUsage{}) {
		t.Fatalf("nil action usage = %+v, want zero", usage)
	}

	body := cell.BeginCell().MustStoreUInt(0xCA, 8).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(body).EndCell()
	tail, err := transactionMessageTailUsage(root)
	if err != nil {
		t.Fatal(err)
	}
	if tail.cells != 1 || tail.bits != 8 {
		t.Fatalf("tail usage = %+v, want one 8-bit ref", tail)
	}

	extra := makeTransactionExtraCurrencies(t, 7, 11)
	msg := &tlb.InternalMessage{
		IHRDisabled:     true,
		SrcAddr:         address.NewAddressNone(),
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.FromNanoTONU(1),
		ExtraCurrencies: extra,
		Body:            body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := transactionOutboundMessageLayout(msgCell)
	if err != nil {
		t.Fatal(err)
	}
	v9Usage, err := transactionOutboundInternalMessageActionUsage(transactionTestConfigWithGlobalVersion(t, 9), msg, msgCell, layout)
	if err != nil {
		t.Fatal(err)
	}
	v10Usage, err := transactionOutboundInternalMessageActionUsage(transactionTestConfigWithGlobalVersion(t, 10), msg, msgCell, layout)
	if err != nil {
		t.Fatal(err)
	}
	if v9Usage.cells <= v10Usage.cells {
		t.Fatalf("v9 usage %+v should include legacy extra dict cells above v10 %+v", v9Usage, v10Usage)
	}
}

func TestTransactionActionFineAndPublicLibrariesEdges(t *testing.T) {
	if got := transactionComputeActionFineForUsageWithPrices(nil, transactionUsage{cells: 10}, big.NewInt(100)); got.Sign() != 0 {
		t.Fatalf("nil prices fine = %s, want 0", got)
	}
	prices := &tlb.ConfigMsgForwardPrices{CellPrice: 8 << 16}
	if got := transactionComputeActionFineForUsageWithPrices(prices, transactionUsage{cells: 10}, big.NewInt(2)); got.Uint64() != 2 {
		t.Fatalf("limited fine = %s, want 2", got)
	}
	maxCells, limited := transactionActionFineCellLimitWithPrices(prices, 10, new(big.Int).Lsh(big.NewInt(1), 70))
	if maxCells != 10 || limited {
		t.Fatalf("huge available cell limit = %d/%t, want 10/false", maxCells, limited)
	}
	maxCells, limited = transactionActionFineCellLimitWithPrices(prices, 10, big.NewInt(2))
	if maxCells != 1 || !limited {
		t.Fatalf("small available cell limit = %d/%t, want 1/true", maxCells, limited)
	}

	fine, maxCells, limited := transactionComputeSendActionFineForUsage(
		transactionConfig{},
		address.NewAddressNone(),
		tonopsTestAddr,
		transactionUsage{cells: 5},
		big.NewInt(10),
		nil,
		big.NewInt(1),
		big.NewInt(2),
		nil,
		0x40,
	)
	if fine.Sign() != 0 || maxCells != transactionGetSizeLimits(transactionConfig{}).maxMsgCells || limited {
		t.Fatalf("send fine without prices = %s/%d/%t, want zero/default/false", fine, maxCells, limited)
	}

	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamMsgForwardPricesBasechain: transactionFeesForwardPricesCell(t, *prices),
	})
	msgCell := cell.BeginCell().
		MustStoreUInt(0xAA, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()).
		EndCell()
	forward, err := transactionComputeForwardFeeForMessage(cfg, address.NewAddressNone(), tonopsTestAddr, msgCell)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Sign() == 0 {
		t.Fatal("forward fee should be positive with configured cell price")
	}
	actionFine, err := transactionComputeActionFine(cfg, address.NewAddressNone(), tonopsTestAddr, msgCell, big.NewInt(100))
	if err != nil {
		t.Fatal(err)
	}
	if actionFine.Sign() == 0 {
		t.Fatal("action fine should be positive with configured cell price")
	}
	fine, maxCells, limited = transactionComputeSendActionFineForUsage(
		cfg,
		address.NewAddressNone(),
		tonopsTestAddr,
		transactionUsage{cells: 5},
		big.NewInt(10),
		nil,
		big.NewInt(1),
		big.NewInt(2),
		nil,
		0x40,
	)
	if fine.Sign() != 0 || maxCells != 0 || !limited {
		t.Fatalf("mode64 insufficient funds fine = %s/%d/%t, want zero/0/true", fine, maxCells, limited)
	}

	if got := transactionPublicLibrariesCount(nil); got != 0 {
		t.Fatalf("nil public libraries count = %d, want 0", got)
	}
	libs := cell.NewDict(256)
	if err := libs.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreBoolBit(true).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := libs.SetIntKey(big.NewInt(2), cell.BeginCell().MustStoreBoolBit(false).EndCell()); err != nil {
		t.Fatal(err)
	}
	if got := transactionPublicLibrariesCount(libs); got != 1 {
		t.Fatalf("public libraries count = %d, want 1", got)
	}
	malformed := cell.NewDict(256)
	if err := malformed.SetIntKey(big.NewInt(1), cell.BeginCell().EndCell()); err != nil {
		t.Fatal(err)
	}
	if got := transactionPublicLibrariesCount(malformed); got != 0 {
		t.Fatalf("malformed public libraries count = %d, want 0", got)
	}
}

func transactionFeesGasPricesCell(t *testing.T, prices tlb.ConfigGasLimitsPrices) *cell.Cell {
	t.Helper()

	gasCell, err := tlb.ToCell(&prices)
	if err != nil {
		t.Fatalf("failed to build gas prices config: %v", err)
	}
	return gasCell
}

func transactionFeesForwardPricesCell(t *testing.T, prices tlb.ConfigMsgForwardPrices) *cell.Cell {
	t.Helper()

	priceCell, err := tlb.ToCell(&prices)
	if err != nil {
		t.Fatalf("failed to build forward prices config: %v", err)
	}
	return priceCell
}
