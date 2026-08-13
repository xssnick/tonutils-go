package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// A node serving a config out of its storage hands over a lazily parsed root, so a
// config param sitting below the materialized part is still a pruned placeholder when
// the strict loaders inspect it. Everything that reads the declared size of such a cell
// instead of parsing it sees the placeholder, not the record.
func lazyReparse(t *testing.T, c *cell.Cell) *cell.Cell {
	t.Helper()

	roots, _, err := cell.FromBOCMultiRootReader(cell.NewBOCNoCopyReader(c.ToBOC()), cell.BOCParseOptions{Lazy: true})
	if err != nil {
		t.Fatalf("failed to reparse config root lazily: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("lazy reparse produced %d roots, want 1", len(roots))
	}
	return roots[0]
}

func TestTransactionLoadGasPricesStrictAcceptsLazyConfigRoot(t *testing.T) {
	prices := tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            1000,
		FlatGasPrice:            1000000,
		HasSeparateSpecialLimit: true,
		GasPrice:                65536000,
		GasLimit:                1000000,
		SpecialGasLimit:         1000000,
		GasCredit:               10000,
		BlockGasLimit:           10000000,
		FreezeDueLimit:          100000000,
		DeleteDueLimit:          1000000000,
	}

	root := buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGasPricesBasechain: transactionFeesGasPricesCell(t, prices),
	})

	for _, tc := range []struct {
		name string
		root *cell.Cell
	}{
		{name: "materialized", root: root},
		{name: "lazy", root: lazyReparse(t, root)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := transactionLoadGasPricesStrict(tlb.BlockchainConfig{Root: tc.root}, false)
			if err != nil {
				t.Fatalf("strict gas prices load failed: %v", err)
			}
			if *got != prices {
				t.Fatalf("gas prices = %+v, want %+v", *got, prices)
			}
		})
	}
}

func TestTransactionLoadGasPricesStrictRejectsTrailingData(t *testing.T) {
	prices := tlb.ConfigGasLimitsPrices{
		GasPrice:       65536000,
		GasLimit:       1000000,
		GasCredit:      10000,
		BlockGasLimit:  10000000,
		FreezeDueLimit: 100000000,
		DeleteDueLimit: 1000000000,
	}
	priceCell := transactionFeesGasPricesCell(t, prices)

	padded := cell.BeginCell().MustStoreBuilder(priceCell.ToBuilder()).MustStoreUInt(0, 1).EndCell()

	root := buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGasPricesBasechain: padded,
	})

	for _, tc := range []struct {
		name string
		root *cell.Cell
	}{
		{name: "materialized", root: root},
		{name: "lazy", root: lazyReparse(t, root)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := transactionLoadGasPricesStrict(tlb.BlockchainConfig{Root: tc.root}, false); err == nil {
				t.Fatal("strict gas prices load accepted a param with trailing data")
			}
		})
	}
}

func TestPrepareBlockchainConfigLoadsLazyFundamentalAccounts(t *testing.T) {
	params := transactionReportStrictConfigParams(t)
	fundamental := cell.NewDict(256)
	if err := fundamental.SetIntKey(big.NewInt(0), cell.BeginCell().EndCell()); err != nil {
		t.Fatal(err)
	}
	param, err := tlb.ToCell(&tlb.FundamentalSmartContractAddresses{Addresses: fundamental})
	if err != nil {
		t.Fatal(err)
	}
	params[tlb.ConfigParamFundamentalSMCAddresses] = param

	config, err := PrepareBlockchainConfig(lazyReparse(t, buildTransactionConfigRoot(t, params)))
	if err != nil {
		t.Fatal(err)
	}
	if !config.isSpecialAccount(address.NewAddress(0, 0xff, make([]byte, 32))) {
		t.Fatal("fundamental account behind a lazy config branch was not loaded")
	}
}

func TestPrepareBlockchainConfigLoadsLazyWorkchains(t *testing.T) {
	params := transactionReportStrictConfigParams(t)
	params[tlb.ConfigParamWorkchains] = buildTransactionResult39WorkchainsConfig(t)

	config, err := PrepareBlockchainConfig(lazyReparse(t, buildTransactionConfigRoot(t, params)))
	if err != nil {
		t.Fatal(err)
	}
	descr, found, checksEnabled := config.workchainDescr(0)
	if !checksEnabled || !found {
		t.Fatal("basechain behind a lazy config branch was not loaded")
	}
	if !descr.AcceptMessages() || !descr.ValidAddressLength(256) {
		t.Fatalf("unexpected basechain descriptor: %+v", descr)
	}
}
