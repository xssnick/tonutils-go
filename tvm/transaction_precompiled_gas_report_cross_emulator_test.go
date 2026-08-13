//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Witness from the 2026-08 gap sweep: when a contract is listed as precompiled
// but no built-in implementation exists, the fallback replaces only the limits
// the VM runs under. The serialized compute phase keeps reporting the limits
// derived from the account balance, so a poor account whose balance buys less
// gas than the configured limit must still report the smaller number.
func TestTVMCrossEmulatorTransactionPrecompiledFallbackGasReport(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	code := makeTransactionInternalActionsCode(t, cell.BeginCell().EndCell(), newData)

	for _, tc := range []struct {
		name     string
		balance  uint64
		value    uint64
		gasUsage uint64
	}{
		// balance buys far less gas than the configured limit
		{name: "poor_account", balance: 20_000_000, value: 5_000_000, gasUsage: 7},
		{name: "poor_account_mid_usage", balance: 50_000_000, value: 10_000_000, gasUsage: 1_000},
		// rich account: both limits coincide
		{name: "rich_account", balance: walletSendTestBalance, value: 1_000_000_000, gasUsage: 7},
		// usage above what the balance can buy skips the compute phase
		{name: "usage_over_limit", balance: 10_000_000, value: 1_000_000, gasUsage: 10_000_000},
	} {
		for _, version := range []uint32{13, 15} {
			t.Run(fmt.Sprintf("%s_v%d", tc.name, version), func(t *testing.T) {
				configRoot := referenceTransactionConfigRootWithOverrides(t,
					referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version),
					map[int32]*cell.Cell{
						int32(tlb.ConfigParamPrecompiledContracts): buildTransactionV13PrecompiledConfig(t, code, tc.gasUsage),
					})

				shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, tc.balance, now)
				msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
					IHRDisabled: true,
					SrcAddr:     internalEmulationSrcAddr,
					DstAddr:     tonopsTestAddr,
					Amount:      tlb.FromNanoTONU(tc.value),
					Body:        cell.BeginCell().EndCell(),
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
