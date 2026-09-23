//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionMalformedStateInitLibraryStage(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}
	libs := cell.NewDict(256)
	if err := libs.SetIntKey(big.NewInt(0), cell.BeginCell().MustStoreBoolBit(false).EndCell()); err != nil {
		t.Fatal(err)
	}
	state, err := tlb.ToCell(&tlb.StateInit{Lib: libs})
	if err != nil {
		t.Fatal(err)
	}
	now := uint32(tonopsTestTime.Unix())
	base := mustReferenceTransactionConfigRoot(t)
	for _, layout := range []struct {
		external, referenced bool
	}{{false, false}, {false, true}, {true, false}, {true, true}} {
		for _, version := range []uint32{0, 3, 4, 7, 8, 12, 13, 16} {
			for _, mode := range []uint8{1, 3} {
				t.Run(fmt.Sprintf("external_%t/ref_%t/v%d/mode%d", layout.external, layout.referenced, version, mode), func(t *testing.T) {
					outgoing := transactionStateInitLibraryTestMessage(state, layout.external, layout.referenced)
					actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outgoing})
					data := cell.BeginCell().EndCell()
					code := makeTransactionInternalActionsCode(t, actions, data)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
					msg := buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1_000_000_000, data)
					cfg := referenceTransactionConfigRootWithGlobalVersion(t, base, version)
					ref, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, cfg)
					if err != nil {
						t.Fatal(err)
					}
					got, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{Address: tonopsTestAddr, Now: now,
						BlockLT: transactionTestLogicalTime, LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: cfg})
					if err != nil {
						t.Fatal(err)
					}
					var reference, actual tlb.Transaction
					if err := tlb.Parse(&reference, ref.txCell); err != nil {
						t.Fatal(err)
					}
					if err := tlb.Parse(&actual, got.TransactionCell); err != nil {
						t.Fatal(err)
					}
					r := reference.Description.(tlb.TransactionDescriptionOrdinary).ActionPhase
					g := actual.Description.(tlb.TransactionDescriptionOrdinary).ActionPhase
					t.Logf("reference valid=%t success=%t code=%d skipped=%d; Go valid=%t success=%t code=%d skipped=%d", r.Valid, r.Success, r.ResultCode, r.SkippedActions, g.Valid, g.Success, g.ResultCode, g.SkippedActions)
					if got.TransactionCell.HashKey() != ref.txCell.HashKey() || got.NextAccount.ShardAccountCell().HashKey() != ref.shardCell.HashKey() {
						t.Error("malformed StateInit library stage differs from native reference")
					}
				})
			}
		}
	}
}
