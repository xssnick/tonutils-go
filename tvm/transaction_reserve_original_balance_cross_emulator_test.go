//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Witness from the 2026-08 gap sweep: the RAWRESERVE mode&4 base differs by
// global version. Since v9 it is the balance at compute-phase start minus the
// remaining message value; below v9 it is the pre-transaction balance minus
// the fees collected so far, and going negative there fails the action.
func TestTVMCrossEmulatorTransactionReserveOriginalBalanceVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	body := cell.BeginCell().EndCell()

	for _, version := range []uint32{4, 8, 9, 10, 13} {
		configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)

		for _, tc := range []struct {
			name    string
			mode    uint8
			reserve uint64
			balance uint64
			value   uint64
		}{
			// mode 4: reserve = original balance + amount
			{name: "mode4_small", mode: 4, reserve: 1_000_000, balance: 2_000_000_000, value: 1_000_000_000},
			// mode 4 with a reserve that only fits under one of the two bases
			{name: "mode4_tight", mode: 4, reserve: 1_900_000_000, balance: 2_000_000_000, value: 1_000_000_000},
			// mode 12: reserve = original balance - amount
			{name: "mode12_partial", mode: 12, reserve: 500_000_000, balance: 2_000_000_000, value: 1_000_000_000},
			// mode 12 asking for more than the base holds
			{name: "mode12_over", mode: 12, reserve: 5_000_000_000, balance: 2_000_000_000, value: 1_000_000_000},
			// a nearly empty account: below v9 the fees can exceed the base
			{name: "mode4_poor", mode: 4, reserve: 1, balance: 20_000_000, value: 1_000_000_000},
		} {
			t.Run(fmt.Sprintf("%s_v%d", tc.name, version), func(t *testing.T) {
				code := makeTransactionInternalReserveOnlyCode(t, tc.reserve, tc.mode, newData)
				shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, tc.balance, now)
				msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
					IHRDisabled: true,
					SrcAddr:     internalEmulationSrcAddr,
					DstAddr:     tonopsTestAddr,
					Amount:      tlb.FromNanoTONU(tc.value),
					Body:        body,
				})

				goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
					Address:     tonopsTestAddr,
					Now:         now,
					BlockLT:     transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime,
					RandSeed:    append([]byte(nil), tonopsTestSeed...),
					ConfigRoot:  configRoot,
				})
				if err != nil {
					t.Fatalf("go transaction emulation failed: %v", err)
				}
				refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
				if err != nil {
					t.Fatalf("reference transaction emulation failed: %v", err)
				}

				if goRes.GasUsed != refRes.gasUsed {
					t.Fatalf("v%d gas mismatch: go=%d reference=%d", version, goRes.GasUsed, refRes.gasUsed)
				}
				if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
					t.Fatalf("v%d transaction hash mismatch:\ngo=%s\nreference=%s", version,
						transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				}
				if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
					t.Fatalf("v%d shard account hash mismatch:\ngo=%s\nreference=%s", version,
						transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
				}
			})
		}
	}
}

// The two bases only drift apart once the storage phase clamps the credited
// message value: an account with a long-unpaid storage debt reserves against
// the pre-transaction balance below v9 and against the clamped compute-phase
// balance since v9.
func TestTVMCrossEmulatorTransactionReserveOriginalBalanceStorageDebt(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	body := cell.BeginCell().EndCell()

	// a storage debt accrued over a long unpaid period on a sizeable state
	storageInfo := func(lastPaid uint32) tlb.StorageInfo {
		return tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(500),
				BitsUsed:  big.NewInt(400_000),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     lastPaid,
		}
	}

	for _, version := range []uint32{4, 8, 9, 13} {
		configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)
		for _, tc := range []struct {
			name     string
			mode     uint8
			reserve  uint64
			balance  uint64
			value    uint64
			lastPaid uint32
		}{
			{name: "debt_mode4", mode: 4, reserve: 1_000_000, balance: 300_000_000, value: 200_000_000, lastPaid: now - 60*60*24*365},
			{name: "debt_mode12", mode: 12, reserve: 100_000_000, balance: 300_000_000, value: 200_000_000, lastPaid: now - 60*60*24*365},
			{name: "debt_heavy_mode4", mode: 4, reserve: 1, balance: 50_000_000, value: 40_000_000, lastPaid: now - 60*60*24*365*3},
			{name: "debt_heavy_mode12", mode: 12, reserve: 10_000_000, balance: 50_000_000, value: 40_000_000, lastPaid: now - 60*60*24*365*3},
		} {
			t.Run(fmt.Sprintf("%s_v%d", tc.name, version), func(t *testing.T) {
				code := makeTransactionInternalReserveOnlyCode(t, tc.reserve, tc.mode, newData)
				shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, tc.balance, storageInfo(tc.lastPaid))
				msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
					IHRDisabled: true,
					SrcAddr:     internalEmulationSrcAddr,
					DstAddr:     tonopsTestAddr,
					Amount:      tlb.FromNanoTONU(tc.value),
					Body:        body,
				})

				goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
					Address:     tonopsTestAddr,
					Now:         now,
					BlockLT:     transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime,
					RandSeed:    append([]byte(nil), tonopsTestSeed...),
					ConfigRoot:  configRoot,
				})
				if err != nil {
					t.Fatalf("go transaction emulation failed: %v", err)
				}
				refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
				if err != nil {
					t.Fatalf("reference transaction emulation failed: %v", err)
				}

				if goRes.GasUsed != refRes.gasUsed {
					t.Fatalf("v%d gas mismatch: go=%d reference=%d", version, goRes.GasUsed, refRes.gasUsed)
				}
				if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
					t.Fatalf("v%d transaction hash mismatch:\ngo=%s\nreference=%s", version,
						transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				}
				if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
					t.Fatalf("v%d shard account hash mismatch:\ngo=%s\nreference=%s", version,
						transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
				}
			})
		}
	}
}
