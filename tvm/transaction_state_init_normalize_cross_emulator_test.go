//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Witness from the 2026-08 gap sweep: the stored account state is rebuilt from
// scalar values, so an all-zero tick/tock pair or a zero prefix length in the
// deploying state init collapses to nothing$0 instead of being carried over.
// Getting this wrong changes the account hash and thus the transaction hash.
func TestTVMCrossEmulatorTransactionStateInitMaybeNormalization(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	code := makeTransactionInternalActionsCode(t, cell.BeginCell().EndCell(), origData)

	zeroDepth := uint64(0)
	oneDepth := uint64(1)

	for _, tc := range []struct {
		name     string
		tickTock *tlb.TickTock
		depth    *uint64
	}{
		{name: "ticktock_just_zero", tickTock: &tlb.TickTock{}},
		{name: "ticktock_tick", tickTock: &tlb.TickTock{Tick: true}},
		{name: "ticktock_tock", tickTock: &tlb.TickTock{Tock: true}},
		{name: "ticktock_both", tickTock: &tlb.TickTock{Tick: true, Tock: true}},
		{name: "depth_just_zero", depth: &zeroDepth},
		{name: "depth_one", depth: &oneDepth},
		{name: "depth_zero_ticktock_zero", depth: &zeroDepth, tickTock: &tlb.TickTock{}},
		{name: "plain", tickTock: nil, depth: nil},
	} {
		for _, version := range []uint32{10, 13, 15} {
			t.Run(fmt.Sprintf("%s_v%d", tc.name, version), func(t *testing.T) {
				stateInit := &tlb.StateInit{
					Depth:    tc.depth,
					TickTock: tc.tickTock,
					Code:     code,
					Data:     origData,
				}
				stateCell, err := tlb.ToCell(stateInit)
				if err != nil {
					t.Fatalf("build state init: %v", err)
				}
				// deploy target: the address is the hash of the state init as
				// it appears in the message, before any normalization
				addr := address.NewAddress(0, 0, stateCell.Hash())

				configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)
				shard := buildTransactionTestUninitShardAccount(t, addr, walletSendTestBalance, tlb.StorageInfo{
					StorageUsed: tlb.StorageUsed{
						CellsUsed: big.NewInt(1),
						BitsUsed:  big.NewInt(0),
					},
					StorageExtra: tlb.StorageExtraNone{},
					LastPaid:     now,
				})
				msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
					IHRDisabled: true,
					SrcAddr:     internalEmulationSrcAddr,
					DstAddr:     addr,
					Amount:      tlb.FromNanoTONU(500_000_000),
					StateInit:   stateInit,
					Body:        cell.BeginCell().EndCell(),
				})

				goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
					Address:     addr,
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
				if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
					t.Fatalf("v%d shard account hash mismatch:\ngo=%s\nreference=%s", version,
						transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
				}
				if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
					t.Fatalf("v%d transaction hash mismatch:\ngo=%s\nreference=%s", version,
						transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				}
			})
		}
	}
}
