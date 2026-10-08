package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestPrepareBlockchainConfigSizeLimitsV3QueueTail(t *testing.T) {
	maxLibraryLoads := uint32(17)
	current, err := tlb.ToCell(&tlb.SizeLimitsConfigV3{
		MaxMsgBits:                 1000,
		MaxMsgCells:                101,
		MaxTransactionLibraryLoads: &maxLibraryLoads,
		MaxTotalMsgBits:            2000,
		MaxTotalMsgCells:           202,
		OutMsgQueueSizeHardLimit:   18001,
		OutMsgQueueSizeSoftLimit:   12001,
	})
	if err != nil {
		t.Fatal(err)
	}
	prefixBits := current.BitsSize() - 64
	historical := cell.BeginCell().MustStoreSlice(current.MustBeginParse().MustLoadSlice(prefixBits), prefixBits).EndCell()
	for _, tt := range []struct {
		name   string
		record *cell.Cell
	}{
		{name: "historical", record: historical},
		{name: "current", record: current},
	} {
		t.Run(tt.name, func(t *testing.T) {
			params := transactionReportStrictConfigParams(t)
			params[tlb.ConfigParamSizeLimits] = tt.record
			root := buildTransactionConfigRoot(t, params)
			cfg, err := PrepareBlockchainConfig(root)
			if err != nil {
				t.Fatal(err)
			}
			limits := transactionGetSizeLimits(cfg)
			if limits.maxMsgBits != 1000 || limits.maxMsgCells != 101 || limits.maxTotalMsgBits != 2000 || limits.maxTotalMsgCells != 202 || limits.maxTransactionLibraryLoads == nil || *limits.maxTransactionLibraryLoads != 17 {
				t.Fatalf("execution size limits changed while parsing queue limits: %+v", limits)
			}
			if cfg.Root().HashKey() != root.HashKey() {
				t.Fatal("config preparation replaced the supplied config root")
			}
		})
	}

	params := transactionReportStrictConfigParams(t)
	params[tlb.ConfigParamSizeLimits] = historical.ToBuilder().MustStoreUInt(18001, 32).EndCell()
	if _, err = PrepareBlockchainConfig(buildTransactionConfigRoot(t, params)); err == nil {
		t.Fatal("strict config accepted a partial queue limits tail")
	}
}
