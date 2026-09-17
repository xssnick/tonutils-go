package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestHistoricalNoActionStateLimitsScope(t *testing.T) {
	data := cell.BeginCell().MustStoreUInt(1, 8).MustStoreRef(cell.BeginCell().EndCell()).EndCell()
	for _, version := range []uint32{0, 1, 2, 3, 4, 15} {
		for _, emptyActions := range []bool{false, true} {
			for _, historical := range []bool{false, true} {
				t.Run(fmt.Sprintf("v%d/empty%t/historical%t", version, emptyActions, historical), func(t *testing.T) {
					actions := cell.BeginCell().EndCell()
					if !emptyActions {
						actions = buildTransactionActionList(t, tlb.ActionSetCode{NewCode: data})
					}
					out := applyActionRegression(t, actionRegressionLimitedConfig(t, version, 1, 1000, 1<<13), data, actions, historical)
					wantSuccess := historical && version <= 3
					wantCode := int32(50)
					if wantSuccess {
						wantCode = 0
					}
					if out.phase.Success != wantSuccess || out.phase.ResultCode != wantCode || out.bounce == wantSuccess {
						t.Fatalf("phase=%+v bounce=%t, want success=%t code=%d", out.phase, out.bounce, wantSuccess, wantCode)
					}
					if !emptyActions && transactionCellEqual(out.nextCode, data) != wantSuccess {
						t.Fatal("SETCODE commit does not match action success")
					}
				})
			}
		}
	}
}

func TestHistoricalNoActionStateLimitsFailedLibraryChanges(t *testing.T) {
	lib := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	actions := buildTransactionActionList(t,
		tlb.ActionChangeLibrary{Mode: 2, LibRef: tlb.LibRefRef{Library: lib}},
		tlb.ActionSendMsg{Msg: buildTransactionOutboundInternalCell(t, 5000)},
	)
	for _, historical := range []bool{false, true} {
		out := applyActionRegression(t, actionRegressionLimitedConfig(t, 2, 1, 1000, 1<<13), cell.BeginCell().EndCell(), actions, historical)
		wantCode := int32(50)
		if historical {
			wantCode = 37
		}
		if out.phase.Success || out.phase.ResultCode != wantCode || out.deleteAccount || len(out.outMsgs) != 0 {
			t.Fatalf("historical=%t: unexpected failure result: %+v", historical, out.phase)
		}
		if historical {
			if out.nextLibraries == nil {
				t.Fatal("historical library update was discarded after failed send")
			}
			if _, err := out.nextLibraries.LoadValueByIntKey(new(big.Int).SetBytes(lib.Hash())); err != nil {
				t.Fatal(err)
			}
		} else if out.nextLibraries != nil {
			t.Fatal("modern size failure retained an oversized library dictionary")
		}
	}
}

func TestHistoricalNoActionStateLimitsKeepsActionChecks(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	large := cell.BeginCell().MustStoreUInt(1, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 8).MustStoreRef(empty).EndCell()).EndCell()
	for _, tc := range []struct {
		name    string
		actions *cell.Cell
		code    int32
	}{
		{"library size", buildTransactionActionList(t, tlb.ActionChangeLibrary{Mode: 2, LibRef: tlb.LibRefRef{Library: large}}), 43},
		{"missing library", buildTransactionActionList(t, tlb.ActionChangeLibrary{Mode: 2, LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)}}), 41},
		{"message size", buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCellWithBody(t, 1, large)}), 40},
		{"message source", buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1, empty)}), 35},
		{"invalid list", cell.BeginCell().MustStoreUInt(1, 8).EndCell(), 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, historical := range []bool{false, true} {
				out := applyActionRegression(t, actionRegressionLimitedConfig(t, 2, 1000, 1, 1), empty, tc.actions, historical)
				if out.phase.Success || out.phase.ResultCode != tc.code || len(out.outMsgs) != 0 {
					t.Fatalf("historical=%t: phase=%+v, want code=%d", historical, out.phase, tc.code)
				}
			}
		})
	}
}

func TestHistoricalNoActionStateLimitsEntrypoints(t *testing.T) {
	machine := NewTVM()
	oldData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCell(t, 1)})
	cfg := actionRegressionLimitedConfig(t, 2, 1, 1000, 1<<13)
	for _, kind := range []string{"ordinary", "tick", "tock"} {
		for _, historical := range []bool{false, true, false} {
			result := emulateActionRegression(t, machine, kind, cfg, actions, oldData, newData, historical)
			state := result.NextAccount.State()
			wantData := oldData
			wantMessages := 0
			if historical {
				wantData, wantMessages = newData, 1
			}
			if state.Status != tlb.AccountStatusActive || state.StateInit == nil || !transactionCellEqual(state.StateInit.Data, wantData) || len(result.OutMessages) != wantMessages {
				t.Fatalf("kind=%s historical=%t: wrong committed data or messages", kind, historical)
			}
		}
	}
}
