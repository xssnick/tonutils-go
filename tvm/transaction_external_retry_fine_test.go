package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var transactionExternalPackingRetryCases = []struct {
	name     string
	budget   uint64
	fine     uint64
	messages uint16
}{
	{name: "original", budget: 7, fine: 2},
	{name: "state_init_ref", budget: 10, fine: 3},
	{name: "body_ref", budget: 14, fine: 4},
	{name: "success", budget: 20, messages: 1},
}

func buildTransactionExternalPackingRetryMessage(t *testing.T) *cell.Cell {
	t.Helper()

	state, err := tlb.ToCell(&tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(41, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(59, 9).EndCell(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both StateInit and body fit inline with addr_none. Replacing the source
	// with a standard address overflows even after moving StateInit to a ref.
	return cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreUInt(0b10, 2).
		MustStoreBuilder(state.ToBuilder()).
		MustStoreUInt(0, 1).
		MustStoreSlice(make([]byte, 100), 800).
		EndCell()
}

func TestTransactionExternalSendChecksFundsBeforeEachPackingRetry(t *testing.T) {
	// C++ Transaction::try_action_send_msg checks fees before packing each
	// layout. The original, init-ref and body-ref attempts charge 2, 3 and 4
	// cells respectively, with a fine of one nanoton per visited cell.
	prices, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		t.Fatal(err)
	}
	msg := buildTransactionExternalPackingRetryMessage(t)

	for version := uint32(0); version <= vm.MaxSupportedGlobalVersion; version++ {
		cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion:               transactionTestGlobalVersionCell(t, version),
			tlb.ConfigParamMsgForwardPricesBasechain:   prices,
			tlb.ConfigParamMsgForwardPricesMasterchain: prices,
		})
		for _, tc := range transactionExternalPackingRetryCases {
			for _, mode := range []uint8{0, 1, 2, 3} {
				t.Run(fmt.Sprintf("v%d/%s/mode%d", version, tc.name, mode), func(t *testing.T) {
					out := applyTransactionActionsForTestWithParams(t, []any{tlb.ActionSendMsg{Mode: mode, Msg: msg}},
						cfg, new(big.Int).SetUint64(tc.budget), nil, transactionZeroCurrencyBalance())
					fine := tc.fine
					if version < 4 {
						fine = 0
					}
					wantSuccess := mode&2 != 0 || tc.messages != 0
					if !out.phase.Valid || out.phase.Success != wantSuccess || out.phase.MessagesCreated != tc.messages || out.actionFine.Uint64() != fine {
						t.Fatalf("phase=%+v fine=%s, want success=%t messages=%d fine=%d", out.phase, out.actionFine, wantSuccess, tc.messages, fine)
					}
					if !wantSuccess && (out.phase.ResultCode != 37 || !out.phase.NoFunds) {
						t.Fatalf("expected insufficient funds: %+v", out.phase)
					}
					if wantSuccess && (out.phase.ResultCode != 0 || out.phase.NoFunds) {
						t.Fatalf("successful action phase has an error: %+v", out.phase)
					}
					fees := fine
					if tc.messages != 0 {
						fees = 16
					}
					if out.balance.Uint64() != tc.budget-fees || out.actionFees.Uint64() != fees {
						t.Fatalf("balance=%s fees=%s, want %d/%d", out.balance, out.actionFees, tc.budget-fees, fees)
					}
				})
			}
		}
	}
}
