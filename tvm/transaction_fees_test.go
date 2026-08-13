package tvm

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
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

func TestTransactionForwardFeesWrapUint64(t *testing.T) {
	prices := &tlb.ConfigMsgForwardPrices{
		LumpPrice: math.MaxUint64,
		CellPrice: 1 << 16,
		IHRFactor: math.MaxUint32,
	}

	wideForward := prices.ComputeForwardFee(1, 0)
	wantWideForward := new(big.Int).Lsh(big.NewInt(1), 64)
	if wideForward.Cmp(wantWideForward) != 0 {
		t.Fatalf("public forward fee = %s, want %s", wideForward, wantWideForward)
	}
	if got := transactionComputeForwardFeeWithPrices(prices, 1, 0); got.Sign() != 0 {
		t.Fatalf("transaction forward fee = %s, want uint64 wrap to zero", got)
	}

	pricesCell, err := tlb.ToCell(prices)
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamMsgForwardPricesBasechain: pricesCell,
	})
	fwdFee := new(big.Int).SetUint64(math.MaxUint64)
	wantIHR := new(big.Int).Mul(new(big.Int).SetUint64(math.MaxUint32), fwdFee)
	wantIHR.Rsh(wantIHR, 16)
	wantIHR.SetUint64(wantIHR.Uint64())
	if got := transactionComputeIHRFee(cfg, tonopsTestAddr, tonopsTestAddr, fwdFee, false); got.Cmp(wantIHR) != 0 {
		t.Fatalf("transaction IHR fee = %s, want wrapped %s", got, wantIHR)
	}
}

func TestTransactionSelectComputedMessageFeeSignedBoundary(t *testing.T) {
	computed := big.NewInt(100)
	if got := transactionSelectComputedMessageFee(computed, nil); got != computed {
		t.Fatalf("normal computed fee did not replace zero suggestion: %s", got)
	}
	suggested := big.NewInt(200)
	if got := transactionSelectComputedMessageFee(computed, suggested); got != suggested {
		t.Fatalf("larger suggested fee was replaced: %s", got)
	}

	highComputed := new(big.Int).Lsh(big.NewInt(1), 63)
	suggested = big.NewInt(7)
	if got := transactionSelectComputedMessageFee(highComputed, suggested); got != suggested {
		t.Fatalf("high computed fee replaced suggestion: %s", got)
	}
	if got := transactionSelectComputedMessageFee(highComputed, nil); got.Sign() != 0 {
		t.Fatalf("high computed fee replaced forced zero: %s", got)
	}
}

func TestTransactionGasBoundaryHelpers(t *testing.T) {
	if got := transactionGasInt(uint64(math.MaxInt64) + 1); got != math.MinInt64 {
		t.Fatalf("gas signed cast = %d, want %d", got, int64(math.MinInt64))
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
	if got := transactionGasBoughtForAccount(emptyPreparedTestConfig(), nil, big.NewInt(100), tonopsTestAddr, 0); got != 0 {
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
	if got := transactionMaxGasThresholdForLimit(nil, 100); got.Cmp64(0) != 0 {
		t.Fatalf("nil max gas threshold = %s, want 0", got.Big())
	}
	if got := transactionMaxGasThresholdForLimit(prices, prices.FlatGasLimit); got.Cmp64(prices.FlatGasPrice) != 0 {
		t.Fatalf("flat max gas threshold = %s, want %d", got.Big(), prices.FlatGasPrice)
	}

	fallback := transactionMessageGas(vm.Gas{}, 0, emptyPreparedTestConfig(), tonopsTestAddr, big.NewInt(0), nil, tlb.MsgTypeInternal, false)
	if fallback.Limit != 0 || fallback.Remaining != 0 {
		t.Fatalf("nil internal message balance gas = %+v, want zero limit fallback", fallback)
	}
	hugeBalance := new(big.Int).Lsh(big.NewInt(1), 70)
	fallback = transactionMessageGas(vm.Gas{}, 0, emptyPreparedTestConfig(), tonopsTestAddr, big.NewInt(0), hugeBalance, tlb.MsgTypeInternal, false)
	if fallback.Limit != 0 || fallback.Remaining != 0 {
		t.Fatalf("oversized internal message balance gas = %+v, want zero limit fallback", fallback)
	}

	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, *prices),
	})
	gas := transactionMessageGas(vm.Gas{}, 0, cfg, tonopsTestAddr, big.NewInt(1000), big.NewInt(1000), tlb.MsgTypeExternalIn, true)
	if gas.Credit != transactionGasInt(prices.SpecialGasLimit) || gas.Limit != transactionGasInt(prices.SpecialGasLimit) {
		t.Fatalf("special external gas = %+v, want credit/limit clamped to special limit", gas)
	}
}

func TestTransactionTickTockGasHonorsConfiguredZeroLimits(t *testing.T) {
	tests := []struct {
		name      string
		prices    tlb.ConfigGasLimitsPrices
		isSpecial bool
	}{
		{
			name: "ordinary_gas_limit",
			prices: tlb.ConfigGasLimitsPrices{
				GasPrice:      1 << 16,
				GasLimit:      0,
				BlockGasLimit: 1_000_000,
			},
		},
		{
			name: "special_gas_limit",
			prices: tlb.ConfigGasLimitsPrices{
				HasSeparateSpecialLimit: true,
				GasPrice:                1 << 16,
				GasLimit:                1_000,
				SpecialGasLimit:         0,
				BlockGasLimit:           1_000_000,
			},
			isSpecial: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, tt.prices),
			})
			got := transactionTickTockGas(vm.Gas{}, 0, cfg, tonopsTestAddr, big.NewInt(1_000_000), tt.isSpecial)
			if got != (vm.Gas{}) {
				t.Fatalf("configured zero tick/tock gas = %+v, want zero limits", got)
			}
		})
	}
}

func TestTransactionGlobalVersionFallbackContracts(t *testing.T) {
	if _, err := prepareBlockchainConfigLenient(nil); !errors.Is(err, errConfigRootRequired) {
		t.Fatalf("prepare config without root error = %v, want %v", err, errConfigRootRequired)
	}

	if _, err := prepareBlockchainConfigLenient(buildTransactionConfigRoot(t, map[uint32]*cell.Cell{})); err == nil {
		t.Fatal("prepare config without global version param should fail")
	}
	if _, err := prepareBlockchainConfigLenient(buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: cell.BeginCell().MustStoreUInt(0, 1).EndCell(),
	})); err == nil {
		t.Fatal("prepare config with malformed global version should fail")
	}

	// The zero-value prepared config used by unit tests keeps the legacy v0
	// contract for helpers operating below the config layer.
	cfg := emptyPreparedTestConfig()
	if got := cfg.globalVersion(); got != 0 {
		t.Fatalf("zero config global version = %d, want legacy v0", got)
	}
	if cfg.hasCapability(1) {
		t.Fatal("zero config should not expose capabilities")
	}
	if cfg.specialGasFull() {
		t.Fatal("zero config should not enable v5 special-gas-full")
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
	if limits.maxTotalMsgBits != (1<<21)*5/2 || limits.maxTotalMsgCells != (1<<13)*5/2 {
		t.Fatalf("v1 total message defaults were not preserved: %+v", limits)
	}
}

func TestTransactionSizeLimitsV3Config(t *testing.T) {
	maxLibraryLoads := uint32(17)
	limitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV3{
		MaxMsgBits:                  100,
		MaxMsgCells:                 101,
		MaxLibraryCells:             102,
		MaxVMDataDepth:              103,
		MaxExtMsgSize:               104,
		MaxExtMsgDepth:              105,
		MaxAccStateCells:            106,
		MaxMCAccStateCells:          107,
		MaxAccPublicLibraries:       108,
		DeferOutQueueSizeLimit:      109,
		MaxMsgExtraCurrencies:       110,
		MaxAccFixedPrefixLength:     11,
		AccStateCellsForStorageDict: 112,
		MaxTransactionLibraryLoads:  &maxLibraryLoads,
		MaxTotalMsgBits:             113,
		MaxTotalMsgCells:            114,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamSizeLimits: limitsCell,
	})
	limits := transactionGetSizeLimits(cfg)
	if limits.maxMsgBits != 100 || limits.maxMsgCells != 101 || limits.maxLibraryCells != 102 || limits.maxExtMsgDepth != 105 {
		t.Fatalf("v3 base size limits = %+v", limits)
	}
	if limits.maxAccStateCells != 106 || limits.maxMCAccStateCells != 107 || limits.maxAccPublicLibraries != 108 || limits.maxMsgExtraCurrencies != 110 {
		t.Fatalf("v3 account size limits = %+v", limits)
	}
	if limits.maxTotalMsgBits != 113 || limits.maxTotalMsgCells != 114 {
		t.Fatalf("v3 total message limits = %d/%d, want 113/114", limits.maxTotalMsgBits, limits.maxTotalMsgCells)
	}
	if limits.maxTransactionLibraryLoads == nil || *limits.maxTransactionLibraryLoads != maxLibraryLoads {
		t.Fatalf("v3 max transaction library loads = %v, want %d", limits.maxTransactionLibraryLoads, maxLibraryLoads)
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
	usage, err = transactionOutboundInternalMessageActionUsage(emptyPreparedTestConfig(), &tlb.InternalMessage{}, nil, transactionOutboundLayout{})
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
	usage := transactionUsage{cells: 10}
	maxCells, _ := transactionActionFineCellLimitWithPrices(nil, usage.cells, big.NewInt(100))
	if got := transactionComputeActionFineForUsageWithLimit(nil, usage, maxCells); got.Sign() != 0 {
		t.Fatalf("nil prices fine = %s, want 0", got)
	}
	prices := &tlb.ConfigMsgForwardPrices{CellPrice: 8 << 16}
	maxCells, _ = transactionActionFineCellLimitWithPrices(prices, usage.cells, big.NewInt(2))
	if got := transactionComputeActionFineForUsageWithLimit(prices, usage, maxCells); got.Uint64() != 2 {
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
		emptyPreparedTestConfig(),
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
	if fine.Sign() != 0 || maxCells != transactionGetSizeLimits(emptyPreparedTestConfig()).maxMsgCells || limited {
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

	if got := transactionTestPublicLibrariesCount(t, nil); got != 0 {
		t.Fatalf("nil public libraries count = %d, want 0", got)
	}
	library := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	libs := cell.NewDict(256)
	if err := libs.Set(
		cell.BeginCell().MustStoreSlice(library.Hash(), 256).EndCell(),
		cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(library).EndCell(),
	); err != nil {
		t.Fatal(err)
	}
	wrongKey := append([]byte(nil), library.Hash()...)
	wrongKey[0] ^= 0x80
	if err := libs.Set(
		cell.BeginCell().MustStoreSlice(wrongKey, 256).EndCell(),
		cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(library).EndCell(),
	); err != nil {
		t.Fatal(err)
	}
	if got := transactionTestPublicLibrariesCount(t, libs); got != 1 {
		t.Fatalf("public libraries count = %d, want 1", got)
	}
	malformed := cell.NewDict(256)
	if err := malformed.SetIntKey(big.NewInt(1), cell.BeginCell().EndCell()); err != nil {
		t.Fatal(err)
	}
	if got := transactionTestPublicLibrariesCount(t, malformed); got != 0 {
		t.Fatalf("malformed public libraries count = %d, want 0", got)
	}
}

func transactionTestPublicLibrariesCount(t testing.TB, libs *cell.Dictionary) uint64 {
	t.Helper()

	count, err := transactionPublicLibrariesCountChecked(libs)
	if err != nil {
		t.Fatalf("count public libraries: %v", err)
	}
	return count
}

func TestTransactionActionFineWrapsUint64(t *testing.T) {
	const (
		finePerCell  = uint64(1 << 33)
		maxFineCells = uint64(1 << 31)
	)
	prices := &tlb.ConfigMsgForwardPrices{CellPrice: finePerCell << 18}
	available := new(big.Int).SetUint64(15_000_000_000)

	maxCells, limited := transactionActionFineCellLimitWithPrices(prices, maxFineCells, available)
	if maxCells != maxFineCells || limited {
		t.Fatalf("wrapped fine cell limit = %d/%t, want %d/false", maxCells, limited, maxFineCells)
	}

	pricesCell, err := tlb.ToCell(prices)
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamMsgForwardPricesBasechain: pricesCell,
		tlb.ConfigParamSizeLimits: buildTransactionSizeLimitsCell(
			t,
			1<<21,
			uint32(maxFineCells),
			1<<16,
			1<<16,
			1<<16,
		),
	})
	usage := transactionUsage{cells: 2}
	wantFine := new(big.Int).SetUint64(2 * finePerCell)
	if got := transactionComputeActionFineForUsage(cfg, tonopsTestAddr, tonopsTestAddr, usage, available); got.Cmp(wantFine) != 0 {
		t.Fatalf("action fine = %s, want wrapped %s", got, wantFine)
	}

	fine, maxCells, limited := transactionComputeSendActionFineForUsage(
		cfg,
		tonopsTestAddr,
		tonopsTestAddr,
		usage,
		available,
		nil,
		big.NewInt(1),
		big.NewInt(0),
		big.NewInt(0),
		1,
	)
	if fine.Cmp(wantFine) != 0 || maxCells != maxFineCells || limited {
		t.Fatalf("send action fine = %s/%d/%t, want raw %s/%d/false", fine, maxCells, limited, wantFine, maxFineCells)
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
