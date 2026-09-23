package tvm

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestFinalAccountExtraCurrencyBudget(t *testing.T) {
	// Account validation spends one of its 1024 cells on the account root.
	// A valid dictionary with n currencies then costs 2*n-1 node visits.
	for _, count := range []int{511, 512, 513} {
		for _, lazy := range []bool{false, true} {
			t.Run(fmt.Sprintf("currencies=%d/lazy=%t", count, lazy), func(t *testing.T) {
				extra := transactionHistoricalBudgetCurrencies(t, count)
				if lazy {
					extra = lazyParityReload(t, extra.AsCell(), true).AsDict(32)
				}
				acc := &transactionRuntimeAccount{addr: tonopsTestAddr}
				_, err := buildTransactionAccountCell(acc, tlb.AccountStatusUninit, big.NewInt(1000), extra,
					1, 1, nil, nil, nil, nil, nil, false, transactionTestConfigWithGlobalVersion(t, 13), nil, nil)
				if count <= 512 {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "1023 cell validation budget") {
					t.Fatalf("513 currencies must fail final Account validation, got %v", err)
				}
			})
		}
	}
}

func TestFinalAccountBudgetKeepsStateInitOpaque(t *testing.T) {
	payload := cell.BeginCell().MustStoreUInt(42, 8).EndCell()
	for range 20 {
		payload = cell.BeginCell().MustStoreRef(payload).MustStoreRef(payload).EndCell()
	}
	// All three StateInit refs are opaque ^Cell to the Account schema.
	libs := cell.NewDict(256)
	if err := libs.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(payload).EndCell()); err != nil {
		t.Fatal(err)
	}
	acc := &transactionRuntimeAccount{addr: tonopsTestAddr}
	_, err := buildTransactionAccountCell(acc, tlb.AccountStatusActive, big.NewInt(1000), transactionHistoricalBudgetCurrencies(t, 512),
		1, 1, nil, payload, payload, libs, nil, false, transactionTestConfigWithGlobalVersion(t, 13), nil, nil)
	if err != nil {
		t.Fatalf("opaque code/data/library payload consumed the Account validation budget: %v", err)
	}
}

func TestFinalAccountExtraCurrencyBudgetEntrypoints(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	code := empty
	now := uint32(tonopsTestTime.Unix())
	block, err := transactionTestConfigWithGlobalVersion(t, 13).NewBlockContext(BlockOptions{
		Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{512, 513} {
		for _, kind := range []string{"ordinary", "tick", "tock"} {
			t.Run(fmt.Sprintf("currencies=%d/%s", count, kind), func(t *testing.T) {
				extra := transactionHistoricalBudgetCurrencies(t, count)
				var accountExtra, messageExtra *cell.Dictionary
				if kind == "ordinary" {
					messageExtra = extra
				} else {
					accountExtra = extra
				}
				shard := transactionAccountStructureShard(t, tonopsTestAddr, tlb.StorageUsed{
					CellsUsed: big.NewInt(0), BitsUsed: big.NewInt(0),
				}, accountExtra, tlb.AccountStatusActive, &tlb.StateInit{Code: code, Data: empty})
				account, err := PrepareAccount(shard, tonopsTestAddr)
				if err != nil {
					t.Fatalf("input account was rejected before final serialization: %v", err)
				}
				messageCell, err := tlb.ToCell(&tlb.InternalMessage{
					IHRDisabled: true, SrcAddr: internalEmulationSrcAddr, DstAddr: tonopsTestAddr,
					Amount: tlb.FromNanoTONU(1_000_000_000), ExtraCurrencies: messageExtra, Body: empty,
				})
				if err != nil {
					t.Fatal(err)
				}
				message, err := PrepareMessage(messageCell)
				if err != nil {
					t.Fatalf("input message was rejected before final serialization: %v", err)
				}
				machine := NewTVM()
				opts := TransactionOptions{LogicalTime: transactionTestLogicalTime}
				if kind == "ordinary" {
					_, err = machine.EmulateTransaction(block, account, message, opts)
				} else {
					_, err = machine.EmulateTickTockTransaction(block, account, kind == "tock", opts)
				}
				if count <= 512 {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "1023 cell validation budget") {
					t.Fatalf("513 currencies must fail final Account validation, got %v", err)
				}
			})
		}
	}
}

func TestTransactionCellBudgetMaterializesLazyNodes(t *testing.T) {
	root := transactionHistoricalBudgetCurrencies(t, 513).AsCell()
	for _, lazy := range []bool{false, true} {
		t.Run(fmt.Sprintf("lazy=%t", lazy), func(t *testing.T) {
			candidate := root
			if lazy {
				candidate = lazyParityReload(t, root, true)
			}
			err := transactionSpendCellBudget(candidate, 1023)
			if err == nil || !strings.Contains(err.Error(), "1023 cell validation budget") {
				t.Fatalf("oversized dictionary passed its actual cell budget: %v", err)
			}
		})
	}
}
