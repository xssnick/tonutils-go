//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionInlineStateUsage(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	base := mustReferenceTransactionConfigRoot(t)
	data := cell.BeginCell().EndCell()
	state := &tlb.StateInit{}
	stateCell, err := tlb.ToCell(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, bodyCase := range []struct {
		name string
		body *cell.Cell
	}{
		{"identical_roots", stateCell},
		{"physical_descendant", cell.BeginCell().MustStoreUInt(7, 3).MustStoreRef(stateCell).EndCell()},
		{"shared_descendant", cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(stateCell).MustStoreRef(stateCell).EndCell()},
	} {
		for _, internal := range []bool{false, true} {
			for _, initRef := range []bool{false, true} {
				for _, bodyRef := range []bool{false, true} {
					message := &tlb.Message{MsgType: tlb.MsgTypeExternalOut, Msg: &tlb.ExternalMessageOut{StateInit: state, Body: bodyCase.body}}
					if internal {
						message = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
							IHRDisabled: true, DstAddr: tonopsTestAddr, StateInit: state, Body: bodyCase.body,
						}}
					}
					builder := cell.BeginCell()
					if err := tlb.StoreMessageWithLayout(builder, message, tlb.MessageLayout{StateInitInRef: initRef, BodyInRef: bodyRef}); err != nil {
						t.Fatal(err)
					}
					actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 1, Msg: builder.EndCell()})
					code := makeTransactionInternalActionsCode(t, actions, data)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
					inbound := buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1_000_000_000, data)
					for _, version := range []uint32{4, 9, 13} {
						t.Run(fmt.Sprintf("%s/internal%t/initref%t/bodyref%t/v%d", bodyCase.name, internal, initRef, bodyRef, version), func(t *testing.T) {
							cfg := referenceTransactionConfigRootWithGlobalVersion(t, base, version)
							ref, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inbound, now, uint64(transactionTestLogicalTime), tonopsTestSeed, cfg)
							if err != nil {
								t.Fatal(err)
							}
							got, err := testEmulateTransaction(NewTVM(), shard, inbound, testTxParams{Address: tonopsTestAddr, Now: now,
								BlockLT: transactionTestLogicalTime, LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: cfg})
							if err != nil {
								t.Fatal(err)
							}
							assertOrdinaryTransactionActionPhase(t, "reference", ref.txCell,
								transactionActionPhaseExpectation{valid: true, success: true, messagesCreated: 1})
							if got.TransactionCell.HashKey() != ref.txCell.HashKey() || got.NextAccount.ShardAccountCell().HashKey() != ref.shardCell.HashKey() {
								t.Fatalf("inline root accounting differs: transaction Go=%x reference=%x", got.TransactionCell.HashKey(), ref.txCell.HashKey())
							}
						})
					}
				}
			}
		}
	}
}
