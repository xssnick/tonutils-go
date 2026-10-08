package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TON ac605b3b21d010a4e97b1ff15560c5be1a0e398b:
// crypto/block/transaction.cpp validates inbound StateInitWithLibs separately;
// crypto/block/block-parse.cpp::validate_message_relaxed_libs does the same
// during send processing. crypto/tl/tlblib.hpp sets the budget to 1024 cells.
func TestPrepareMessageStateInitLibraryValidationBudget(t *testing.T) {
	for _, count := range []int{512, 513} {
		for _, external := range []bool{false, true} {
			for _, referenced := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/external_%t/ref_%t", count, external, referenced), func(t *testing.T) {
					message := transactionStateInitBudgetMessage(t, count, external, referenced)
					_, err := PrepareMessage(message)
					if (err == nil) != (count == 512) {
						t.Fatalf("%d libraries: PrepareMessage error = %v", count, err)
					}
					var parsed tlb.Message
					if err := tlb.Parse(&parsed, message); err != nil {
						t.Fatal(err)
					}
					_, err = PrepareParsedMessage(message, &parsed)
					if (err == nil) != (count == 512) {
						t.Fatalf("%d libraries: PrepareParsedMessage error = %v", count, err)
					}
				})
			}
		}
	}
}

func TestTransactionOutgoingStateInitLibraryBudgetFailureStage(t *testing.T) {
	state, err := tlb.ToCell(&tlb.StateInit{Lib: transactionStateInitBudgetLibraries(t, 513)})
	if err != nil {
		t.Fatal(err)
	}
	for _, referenced := range []bool{false, true} {
		message := transactionStateInitLibraryTestMessage(state, false, referenced)
		for _, version := range []uint32{12, 13} {
			for _, mode := range []uint8{1, 3} {
				t.Run(fmt.Sprintf("ref_%t/v%d/mode%d", referenced, version, mode), func(t *testing.T) {
					actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: message})
					loaded, err := transactionLoadActions(actions, version, false)
					if err != nil || loaded.resultCode != 0 || loaded.skippedActions != 0 {
						t.Fatalf("well-formed message failed the action prepass: result=%+v err=%v", loaded, err)
					}
					result := emulateActionRegression(t, NewTVM(), "ordinary", transactionTestConfigWithGlobalVersion(t, version),
						actions, cell.BeginCell().EndCell(), cell.BeginCell().EndCell(), false)
					var transaction tlb.Transaction
					if err := tlb.Parse(&transaction, result.TransactionCell); err != nil {
						t.Fatal(err)
					}
					phase := transaction.Description.(tlb.TransactionDescriptionOrdinary).ActionPhase
					skipped := version >= 13 && mode&2 != 0
					wantCode := int32(34)
					wantSkipped := uint16(0)
					if skipped {
						wantCode, wantSkipped = 0, 1
					}
					if !phase.Valid || phase.Success != skipped || phase.ResultCode != wantCode ||
						phase.SkippedActions != wantSkipped || phase.MessagesCreated != 0 || len(result.OutMessages) != 0 {
						t.Fatalf("unexpected library-budget failure: %+v", phase)
					}
				})
			}
		}
	}
}

func transactionStateInitBudgetLibraries(t *testing.T, count int) *cell.Dictionary {
	t.Helper()

	// Cell payloads are opaque to StateInitWithLibs validation. Including
	// descendants makes the boundary test reject counting their references.
	payload := cell.BeginCell().EndCell()
	for range 16 {
		payload = cell.BeginCell().MustStoreRef(payload).EndCell()
	}
	libraries := cell.NewDict(256)
	for i := range count {
		code := cell.BeginCell().MustStoreUInt(uint64(i), 32).MustStoreRef(payload).EndCell()
		value := cell.BeginCell().MustStoreBoolBit(false).MustStoreRef(code).EndCell()
		if err := libraries.SetIntKey(big.NewInt(int64(i)), value); err != nil {
			t.Fatal(err)
		}
	}
	return libraries
}

func transactionStateInitBudgetMessage(t *testing.T, count int, external, referenced bool) *cell.Cell {
	t.Helper()

	state := &tlb.StateInit{Lib: transactionStateInitBudgetLibraries(t, count)}
	var message tlb.Message
	if external {
		message = tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr, StateInit: state, Body: cell.BeginCell().EndCell(),
		}}
	} else {
		message = tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{
			IHRDisabled: true, SrcAddr: internalEmulationSrcAddr, DstAddr: tonopsTestAddr,
			Amount: tlb.FromNanoTONU(500_000_000), StateInit: state, Body: cell.BeginCell().EndCell(),
		}}
	}
	builder := cell.BeginCell()
	if err := tlb.StoreMessageWithLayout(builder, &message, tlb.MessageLayout{StateInitInRef: referenced}); err != nil {
		t.Fatal(err)
	}
	return builder.EndCell()
}
