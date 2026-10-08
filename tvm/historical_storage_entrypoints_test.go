package tvm

import (
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
)

func TestHistoricalStorageFeeEntrypoints(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	code := codeFromBuilders(t, funcsop.ACCEPT().Serialize())
	const now = 1627999052
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, empty, 500_000_000, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{CellsUsed: big.NewInt(218), BitsUsed: big.NewInt(6250)},
		LastPaid:    1588340301,
	})
	account, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty})
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatal(err)
	}
	cfg := transactionTestConfigWithGlobalVersion(t, 2)
	cfg.storagePrices = []preparedStoragePrice{{price: tlb.ConfigStoragePrices{BitPrice: 1000, CellPrice: 500000}}}
	block, err := cfg.NewBlockContext(BlockOptions{Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed})
	if err != nil {
		t.Fatal(err)
	}

	// Modern fees exhaust the balance before compute. The old sign check
	// collects zero, leaving enough gas to execute ACCEPT in every entrypoint.
	machine := NewTVM()
	for _, historical := range []bool{false, true, false} {
		opts := TransactionOptions{LogicalTime: transactionTestLogicalTime, HistoricalStorageFee: historical}
		accepted, err := machine.CheckExternalMessageAccepted(block, account, message, opts)
		if err != nil || accepted != historical {
			t.Fatalf("historical=%t: accept check = %t, %v", historical, accepted, err)
		}
		for _, ticktock := range []bool{false, true} {
			var result *TransactionExecutionResult
			if ticktock {
				result, err = machine.EmulateTickTockTransaction(block, account, false, opts)
			} else {
				result, err = machine.EmulateTransaction(block, account, message, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Accepted != historical || (result.Steps > 0) != historical {
				t.Fatalf("historical=%t ticktock=%t: accepted=%t steps=%d", historical, ticktock, result.Accepted, result.Steps)
			}
			if historical && (result.ExitCode != 0 || result.TransactionCell == nil || result.NextAccount == nil) {
				t.Fatalf("ticktock=%t: historical execution failed", ticktock)
			}
		}
	}

	// Values outside the signed historical arithmetic must surface as errors
	// before VM execution; no entrypoint may silently use modern arithmetic.
	cfg.storagePrices[0].price.CellPrice = math.MaxUint64
	opts := TransactionOptions{HistoricalStorageFee: true}
	checks := map[string]func() error{
		"ordinary": func() error {
			_, err := machine.EmulateTransaction(block, account, message, opts)
			return err
		},
		"ticktock": func() error {
			_, err := machine.EmulateTickTockTransaction(block, account, false, opts)
			return err
		},
		"accept": func() error {
			_, err := machine.CheckExternalMessageAccepted(block, account, message, opts)
			return err
		},
	}
	for name, check := range checks {
		t.Run(name+"_overflow", func(t *testing.T) {
			if err := check(); !errors.Is(err, errHistoricalStorageOverflow) {
				t.Fatalf("expected historical arithmetic error, got %v", err)
			}
		})
	}
}
