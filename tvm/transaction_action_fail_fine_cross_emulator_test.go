//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Since v15 a successful send contributes its full uint64 per-cell fine when
// a later action fails. The fine is capped against the transaction balance
// only after all actions are rolled back, not against the temporary balance
// left after RAWRESERVE.
func TestTVMCrossEmulatorTransactionV15FailedActionFineAfterReserve(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tc := range []struct {
		name          string
		stateLimit    bool
		laterFailure  bool
		wantFine      uint64
		wantResult    int32
		wantSucceeded bool
	}{
		{
			name:          "successful_action_phase_does_not_collect_pending_fine",
			wantSucceeded: true,
		},
		{
			name:         "later_action_failure_collects_pending_fine",
			laterFailure: true,
			wantFine:     2 * (1 << 33),
			wantResult:   34,
		},
		{
			name:         "final_state_limit_failure_collects_pending_fine",
			stateLimit:   true,
			laterFailure: true,
			wantFine:     2 * (1 << 33),
			wantResult:   50,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertTransactionV15ActionFineAfterReserve(t, tc.stateLimit, tc.laterFailure, tc.wantFine, tc.wantResult, tc.wantSucceeded)
		})
	}
}

func assertTransactionV15ActionFineAfterReserve(t *testing.T, stateLimit, laterFailure bool, wantFine uint64, wantResult int32, wantSucceeded bool) {
	t.Helper()

	const finePerCell = uint64(1 << 33)
	const paidCells = uint64(2)
	if laterFailure && wantFine != finePerCell*paidCells {
		t.Fatalf("bad test fine = %d, want %d", wantFine, finePerCell*paidCells)
	}
	// The max-cell budget multiplication wraps to zero. The message's forward
	// fee is independently wrapped to one nanogram by the lump price.
	pricesCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		LumpPrice: math.MaxUint64 - (uint64(1) << 36) + 2,
		CellPrice: finePerCell << 18,
	})
	if err != nil {
		t.Fatalf("failed to build forward prices: %v", err)
	}
	maxAccountCells := uint32(1 << 16)
	maxAccountBits := uint32(1 << 16)
	lastAction := any(tlb.ActionChangeLibrary{Mode: 3, LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)}})
	if stateLimit {
		maxAccountCells = 1
		maxAccountBits = 1
		lastAction = tlb.ActionSetCode{NewCode: cell.BeginCell().
			MustStoreUInt(0xCC, 8).
			MustStoreRef(cell.BeginCell().EndCell()).
			EndCell()}
	}
	configRoot := referenceTransactionConfigRootWithOverrides(t, mustReferenceTransactionConfigRoot(t), map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   pricesCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): pricesCell,
		int32(tlb.ConfigParamSizeLimits): buildTransactionSizeLimitsCell(
			t,
			1<<21,
			1<<31,
			maxAccountCells,
			maxAccountBits,
			1<<16,
		),
	})

	body := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell()
	outMsg := &tlb.Message{
		MsgType: tlb.MsgTypeInternal,
		Msg: &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     tonopsTestAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(1),
			Body:        body,
		},
	}
	outBuilder := cell.BeginCell()
	if err = tlb.StoreMessageWithLayout(outBuilder, outMsg, tlb.MessageLayout{BodyInRef: true}); err != nil {
		t.Fatalf("failed to build outbound message: %v", err)
	}
	outMsgCell := outBuilder.EndCell()
	usage, err := transactionMessageTailUsage(outMsgCell)
	if err != nil {
		t.Fatalf("failed to measure outbound message: %v", err)
	}
	if usage != (transactionUsage{cells: paidCells}) {
		t.Fatalf("outbound message tail usage = %+v, want %d paid cells", usage, paidCells)
	}

	actionList := []any{
		tlb.ActionReserveCurrency{
			Mode: 0,
			Currency: tlb.CurrencyCollection{
				Coins: tlb.FromNanoTONU(20_000_000_000),
			},
		},
		tlb.ActionSendMsg{Mode: 1, Msg: outMsgCell},
	}
	if laterFailure {
		actionList = append(actionList, lastAction)
	}
	actions := buildTransactionActionList(t, actionList...)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	code := makeTransactionInternalActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 30_000_000_000, now)
	inMsg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, inMsg, testTxParams{
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
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inMsg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	wantAction := transactionActionPhaseExpectation{success: wantSucceeded, valid: true, resultCode: wantResult, messagesCreated: 1}
	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
	assertOrdinaryTransactionActionFees(t, "go", goRes.TransactionCell, wantFine)
	assertOrdinaryTransactionActionFees(t, "reference", refRes.txCell, wantFine)
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}
