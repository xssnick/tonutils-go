package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionSendChecksFundsBeforeEachPackingRetry(t *testing.T) {
	// Rewriting addr_none as the sender makes the inline message overflow.
	// Referencing StateInit, then the body, raises its fee-bearing cell count
	// from two to three to four. Every attempt must check funds separately.
	prices, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		t.Fatal(err)
	}
	state := &tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(41, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(59, 9).EndCell(),
	}
	body := cell.BeginCell().MustStoreSlice(make([]byte, 63), 500).EndCell()
	msg, layout, err := transactionInternalMessageToCellWithLayout(&tlb.InternalMessage{
		IHRDisabled: true, SrcAddr: address.NewAddressNone(), DstAddr: address.NewAddress(0, 0, make([]byte, 32)),
		Amount: tlb.FromNanoTONU(100), StateInit: state, Body: body,
	}, transactionOutboundLayout{})
	if err != nil || layout != (transactionOutboundLayout{}) {
		t.Fatal("initial inline layout", layout, err)
	}

	for _, version := range []uint32{4, 6, 8, 10, 15, 16} {
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamMsgForwardPricesBasechain:   prices,
			tlb.ConfigParamMsgForwardPricesMasterchain: prices,
		})
		for _, tc := range []struct {
			name      string
			feeBudget int64
			fine      int64
			messages  uint16
		}{
			{name: "original", feeBudget: 7, fine: 2},
			{name: "init-retry", feeBudget: 10, fine: 3},
			{name: "body-retry", feeBudget: 14, fine: 4},
			{name: "success", feeBudget: 20, messages: 1},
		} {
			for _, mode := range []uint8{1, 3} {
				t.Run(fmt.Sprintf("v%d/%s/mode%d", version, tc.name, mode), func(t *testing.T) {
					balance := big.NewInt(100 + tc.feeBudget)
					out := applyTransactionActionsForTestWithParams(t, []any{tlb.ActionSendMsg{Mode: mode, Msg: msg}},
						cfg, balance, nil, transactionZeroCurrencyBalance())
					wantSuccess := mode&2 != 0 || tc.messages != 0
					if !out.phase.Valid || out.phase.Success != wantSuccess || out.phase.MessagesCreated != tc.messages || out.actionFine.Int64() != tc.fine {
						t.Fatalf("phase=%+v fine=%s, want messages=%d fine=%d", out.phase, out.actionFine, tc.messages, tc.fine)
					}
					if !wantSuccess && (out.phase.ResultCode != 37 || !out.phase.NoFunds) {
						t.Fatal("expected insufficient funds", out.phase)
					}
					if wantSuccess && (out.phase.ResultCode != 0 || out.phase.NoFunds) {
						t.Fatal("successful action phase has an error", out.phase)
					}
					if tc.messages == 0 && out.balance.Int64() != balance.Int64()-tc.fine {
						t.Fatalf("balance=%s, want %d", out.balance, balance.Int64()-tc.fine)
					}
				})
			}
		}
	}
}
