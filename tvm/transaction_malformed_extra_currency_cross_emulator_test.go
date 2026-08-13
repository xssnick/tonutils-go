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

// CurrencyCollection.other is HashmapE 32 VarUInteger 32. The action-list
// prepass validates it as a strict plain dictionary before executing actions.
func TestTVMCrossEmulatorTransactionMalformedOutboundExtraCurrencyFork(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, 15)
	originalData := cell.BeginCell().MustStoreUInt(0, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
	inMsg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(500_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	for _, malformed := range []struct {
		name     string
		extraBit bool
		extraRef bool
	}{
		{name: "fork-payload-bit", extraBit: true},
		{name: "third-fork-ref", extraRef: true},
	} {
		for _, mode := range []uint8{0, 2} {
			t.Run(fmt.Sprintf("%s/mode-%d", malformed.name, mode), func(t *testing.T) {
				extra := transactionMalformedExtraCurrencyFork(t, malformed.extraBit, malformed.extraRef)
				outMsg := transactionTestRelaxedInternalMessageWithExtra(extra, false)
				actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outMsg})
				code := makeTransactionInternalActionsCode(t, actions, newData)
				shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, originalData, walletSendTestBalance, now)

				goRes, err := testEmulateTransaction(NewTVM(), shard, inMsg, testTxParams{
					Address:     tonopsTestAddr,
					Now:         now,
					BlockLT:     transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime,
					RandSeed:    append([]byte(nil), tonopsTestSeed...),
					ConfigRoot:  configRoot,
				})
				if err != nil {
					t.Fatalf("Go transaction emulation failed: %v", err)
				}
				refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inMsg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
				if err != nil {
					t.Fatalf("reference transaction emulation failed: %v", err)
				}

				if goRes.GasUsed != refRes.gasUsed {
					t.Fatalf("gas mismatch: Go %d, reference %d", goRes.GasUsed, refRes.gasUsed)
				}
				if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
					t.Fatalf("transaction hash mismatch:\nGo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				}
				if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
					t.Fatalf("shard-account hash mismatch:\nGo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
				}
			})
		}
	}
}
