//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionOutgoingExtraCurrencyBudget(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	base := mustReferenceTransactionConfigRoot(t)
	data := cell.BeginCell().EndCell()
	for _, count := range []int{511, 512, 513} {
		extra := transactionHistoricalBudgetCurrencies(t, count)
		for _, bodyRef := range []bool{false, true} {
			for _, layout := range []string{"none", "inline", "reference"} {
				message := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
					IHRDisabled: true, DstAddr: tonopsTestAddr, ExtraCurrencies: extra, Body: data,
				}}
				if layout != "none" {
					message.AsInternal().StateInit = &tlb.StateInit{Lib: transactionStateInitBudgetLibraries(t, 1)}
				}
				builder := cell.BeginCell()
				if err := tlb.StoreMessageWithLayout(builder, message, tlb.MessageLayout{StateInitInRef: layout == "reference", BodyInRef: bodyRef}); err != nil {
					t.Fatal(err)
				}
				outgoing := builder.EndCell()
				for _, version := range []uint32{7, 8, 11, 12, 13} {
					for _, mode := range []uint8{1, 3} {
						t.Run(fmt.Sprintf("extra%d/%s/bodyref%t/v%d/mode%d", count, layout, bodyRef, version, mode), func(t *testing.T) {
							actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outgoing})
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
								t.Error("extra currency budget differs from native reference")
							}
						})
					}
				}
			}
		}
	}
}

func TestTVMCrossEmulatorTransactionInboundExtraCurrencyBudget(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	base := mustReferenceTransactionConfigRoot(t)
	data := cell.BeginCell().EndCell()
	code := makeTransactionInternalActionsCode(t, cell.BeginCell().EndCell(), data)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	for _, count := range []int{511, 512, 513} {
		extra := transactionHistoricalBudgetCurrencies(t, count)
		msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
			IHRDisabled: true, SrcAddr: internalEmulationSrcAddr, DstAddr: tonopsTestAddr,
			Amount: tlb.FromNanoTONU(1_000_000_000), ExtraCurrencies: extra, Body: data,
		})
		for _, version := range []uint32{11, 12, 13} {
			t.Run(fmt.Sprintf("extra%d/v%d", count, version), func(t *testing.T) {
				cfg := referenceTransactionConfigRootWithGlobalVersion(t, base, version)
				ref, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, cfg)
				got, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{Address: tonopsTestAddr, Now: now,
					BlockLT: transactionTestLogicalTime, LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: cfg})
				_, prepareErr := PrepareMessage(msg)
				t.Logf("reference error=%v; Go error=%v; PrepareMessage error=%v", refErr, err, prepareErr)
				if (err == nil) != (refErr == nil) {
					t.Fatal("inbound extra currency acceptance differs from native reference")
				}
				if err == nil && (got.TransactionCell.HashKey() != ref.txCell.HashKey() || got.NextAccount.ShardAccountCell().HashKey() != ref.shardCell.HashKey()) {
					t.Error("inbound extra currency transaction differs from native reference")
				}
			})
		}
	}
}

func TestTVMCrossEmulatorTransactionOutgoingReferencedBodyValidation(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}
	now := uint32(tonopsTestTime.Unix())
	base := mustReferenceTransactionConfigRoot(t)
	data := cell.BeginCell().EndCell()
	proof, err := cell.CreateMerkleProof(cell.BeginCell().MustStoreUInt(1, 8).EndCell())
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string]*cell.Cell{
		"ordinary": cell.BeginCell().MustStoreRef(proof).EndCell(),
		"proof":    proof,
		"library":  transactionSpecialMessageBodies(t)["library"],
	}
	for name, body := range bodies {
		message := &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
			IHRDisabled: true, DstAddr: tonopsTestAddr, Body: body,
		}}
		builder := cell.BeginCell()
		if err := tlb.StoreMessageWithLayout(builder, message, tlb.MessageLayout{BodyInRef: true}); err != nil {
			t.Fatal(err)
		}
		outgoing := builder.EndCell()
		for _, version := range []uint32{4, 7, 8, 13} {
			for _, mode := range []uint8{1, 3, 64, 66} {
				t.Run(fmt.Sprintf("%s/v%d/mode%d", name, version, mode), func(t *testing.T) {
					actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outgoing})
					code := makeTransactionInternalActionsCode(t, actions, data)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
					msg := buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1_000_000_000, data)
					cfg := referenceTransactionConfigRootWithGlobalVersion(t, base, version)
					ref, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, cfg)
					if err != nil {
						t.Fatal(err)
					}
					want := transactionActionPhaseExpectation{valid: true, success: true, messagesCreated: 1}
					if name != "ordinary" {
						want = transactionActionPhaseExpectation{resultCode: 34}
						if version >= 8 && mode&2 != 0 {
							want = transactionActionPhaseExpectation{valid: true, success: true, skippedActions: 1}
						}
					}
					assertOrdinaryTransactionActionPhase(t, "reference", ref.txCell, want)
					got, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{Address: tonopsTestAddr, Now: now,
						BlockLT: transactionTestLogicalTime, LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: cfg})
					if err != nil {
						t.Fatal(err)
					}
					if got.TransactionCell.HashKey() != ref.txCell.HashKey() || got.NextAccount.ShardAccountCell().HashKey() != ref.shardCell.HashKey() {
						t.Error("referenced outgoing body validation differs from native reference")
					}
				})
			}
		}
	}
}
