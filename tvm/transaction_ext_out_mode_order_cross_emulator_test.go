//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Witness from the 2026-08 gap sweep: an outbound external message accepts
// only the two low mode bits, and an invalid mode is rejected before the
// source address is inspected. Checking the source first turns a hard action
// list failure into a skipped or differently-coded action.
func TestTVMCrossEmulatorTransactionExtOutModeCheckOrder(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(1, 8).EndCell()

	foreignSrc := address.NewAddress(0, 0, bytes.Repeat([]byte{0x77}, 32))

	extOut := func(src *address.Address) *cell.Cell {
		return mustTransactionMsgCell(t, &tlb.ExternalMessageOut{
			SrcAddr: src,
			DstAddr: nil,
			Body:    cell.BeginCell().EndCell(),
		})
	}

	for _, tc := range []struct {
		name string
		mode uint8
		src  *address.Address
	}{
		// invalid mode plus a foreign source: the mode must decide
		{name: "bad_mode_foreign_src", mode: 6, src: foreignSrc},
		{name: "bad_mode_no_ignore_foreign_src", mode: 4, src: foreignSrc},
		{name: "bad_mode_own_src", mode: 4, src: tonopsTestAddr},
		{name: "bad_mode_128_foreign_src", mode: 128, src: foreignSrc},
		// controls: a valid mode leaves the source check in charge
		{name: "ok_mode_foreign_src", mode: 2, src: foreignSrc},
		{name: "ok_mode_own_src", mode: 0, src: tonopsTestAddr},
		{name: "ok_mode_none_src", mode: 1, src: nil},
	} {
		for _, version := range []uint32{10, 13, 15} {
			t.Run(fmt.Sprintf("%s_v%d", tc.name, version), func(t *testing.T) {
				actions := buildTransactionActionList(t, tlb.ActionSendMsg{
					Mode: tc.mode,
					Msg:  extOut(tc.src),
				})
				code := makeTransactionInternalActionsCode(t, actions, newData)
				shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
				msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
					IHRDisabled: true,
					SrcAddr:     internalEmulationSrcAddr,
					DstAddr:     tonopsTestAddr,
					Amount:      tlb.FromNanoTONU(500_000_000),
					Body:        cell.BeginCell().EndCell(),
				})
				configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)

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
