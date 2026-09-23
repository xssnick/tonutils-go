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

func TestTVMCrossEmulatorTransactionStateInitLibraryValidationBudget(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().EndCell()
	code := makeTransactionInternalActionsCode(t, cell.BeginCell().EndCell(), data)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	config := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), 14)
	for _, count := range []int{512, 513} {
		for _, referenced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/ref_%t", count, referenced), func(t *testing.T) {
				message := transactionStateInitBudgetMessage(t, count, false, referenced)
				ref, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, message, now, uint64(transactionTestLogicalTime), tonopsTestSeed, config)
				goResult, goErr := testEmulateTransaction(NewTVM(), shard, message, testTxParams{
					Address: tonopsTestAddr, Now: now, BlockLT: transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: config,
				})
				if (refErr == nil) != (count == 512) {
					t.Fatalf("unexpected reference result for %d libraries: %v", count, refErr)
				}
				if (goErr == nil) != (refErr == nil) {
					t.Fatalf("%d libraries: Go error = %v, reference error = %v", count, goErr, refErr)
				}
				if refErr == nil && (!bytes.Equal(goResult.TransactionCell.Hash(), ref.txCell.Hash()) ||
					!bytes.Equal(goResult.NextAccount.ShardAccountCell().Hash(), ref.shardCell.Hash())) {
					t.Fatal("accepted transaction or account differs from reference")
				}
			})
		}
	}
}

func TestTVMCrossEmulatorTransactionOutgoingStateInitLibraryValidationBudget(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	for _, count := range []int{512, 513} {
		state, err := tlb.ToCell(&tlb.StateInit{Lib: transactionStateInitBudgetLibraries(t, count)})
		if err != nil {
			t.Fatal(err)
		}
		for _, referenced := range []bool{false, true} {
			outgoing := transactionStateInitLibraryTestMessage(state, false, referenced)
			for _, version := range []uint32{12, 13} {
				for _, mode := range []uint8{1, 3} {
					t.Run(fmt.Sprintf("%d/ref_%t/v%d/mode%d", count, referenced, version, mode), func(t *testing.T) {
						actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outgoing})
						code := makeTransactionInternalActionsCode(t, actions, cell.BeginCell().EndCell())
						shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, now)
						message := mustTransactionMsgCell(t, &tlb.InternalMessage{IHRDisabled: true, SrcAddr: internalEmulationSrcAddr,
							DstAddr: tonopsTestAddr, Amount: tlb.FromNanoTONU(500_000_000), Body: cell.BeginCell().EndCell()})
						config := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)
						ref, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, message, now, uint64(transactionTestLogicalTime), tonopsTestSeed, config)
						if err != nil {
							t.Fatal(err)
						}
						goResult, err := testEmulateTransaction(NewTVM(), shard, message, testTxParams{
							Address: tonopsTestAddr, Now: now, BlockLT: transactionTestLogicalTime,
							LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: config,
						})
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(goResult.TransactionCell.Hash(), ref.txCell.Hash()) ||
							!bytes.Equal(goResult.NextAccount.ShardAccountCell().Hash(), ref.shardCell.Hash()) {
							t.Fatal("transaction or account differs from reference")
						}
					})
				}
			}
		}
	}
}
