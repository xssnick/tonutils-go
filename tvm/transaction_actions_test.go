package tvm

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionMessageToCellWithRetryKeepsStateInitRefWhenMovingBody(t *testing.T) {
	var attempts []transactionOutboundLayout
	root, used, err := transactionMessageToCellWithRetry(transactionOutboundLayout{}, true, func(layout transactionOutboundLayout) (*cell.Cell, error) {
		attempts = append(attempts, layout)
		if layout.stateInitInRef && layout.bodyInRef {
			return cell.BeginCell().EndCell(), nil
		}
		return nil, errors.New("does not fit")
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if root == nil {
		t.Fatal("retry returned nil cell")
	}
	if !used.stateInitInRef || !used.bodyInRef {
		t.Fatalf("used layout = %+v, want state init and body in refs", used)
	}
	if len(attempts) != 3 {
		t.Fatalf("attempts = %d, want 3", len(attempts))
	}
	if attempts[2] != (transactionOutboundLayout{stateInitInRef: true, bodyInRef: true}) {
		t.Fatalf("final attempt = %+v, want progressive state init/body refs", attempts[2])
	}
}

func TestTransactionMessageToCellWithRetryCanSkipStateInitRef(t *testing.T) {
	var attempts []transactionOutboundLayout
	root, used, err := transactionMessageToCellWithRetry(transactionOutboundLayout{}, false, func(layout transactionOutboundLayout) (*cell.Cell, error) {
		attempts = append(attempts, layout)
		if layout.bodyInRef {
			return cell.BeginCell().EndCell(), nil
		}
		return nil, errors.New("does not fit")
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if root == nil {
		t.Fatal("retry returned nil cell")
	}
	if used.stateInitInRef || !used.bodyInRef {
		t.Fatalf("used layout = %+v, want only body in ref", used)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	if attempts[1] != (transactionOutboundLayout{bodyInRef: true}) {
		t.Fatalf("final attempt = %+v, want body ref only", attempts[1])
	}
}

func TestTransactionCollectUsageLoadsLazyRefs(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	child := cell.BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(child).EndCell()

	lazyRoot := lazyTransactionTestRoot(t, root)
	lazyChild, err := lazyRoot.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !lazyChild.IsLazy() {
		t.Fatal("test root should keep child behind lazy boundary")
	}

	want, err := transactionCollectUsage(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := transactionCollectUsage(lazyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("usage from lazy tree = %+v, want %+v", got, want)
	}
}

func TestTransactionCollectUsageDeduplicatesSharedRefs(t *testing.T) {
	shared := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xA, 4).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	got, err := transactionCollectUsage(root)
	if err != nil {
		t.Fatal(err)
	}

	want := transactionUsage{cells: 2, bits: 12}
	if got != want {
		t.Fatalf("usage = %+v, want %+v", got, want)
	}
}

func TestTransactionMaxMerkleDepthNestedProofs(t *testing.T) {
	leaf := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	innerProof, err := cell.CreateMerkleProof(leaf)
	if err != nil {
		t.Fatal(err)
	}

	outerBody := cell.BeginCell().MustStoreRef(innerProof).EndCell()
	outerProof, err := cell.CreateMerkleProof(outerBody)
	if err != nil {
		t.Fatal(err)
	}

	got, err := transactionMaxMerkleDepth(outerProof)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("max merkle depth = %d, want 2", got)
	}
}

func TestTransactionValidateRelaxedActionMessageCurrencies(t *testing.T) {
	t.Run("reports non canonical value", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
			b.MustStoreUInt(4, 4).
				MustStoreSlice([]byte{0x00, 0x0a, 0xb1, 0x47}, 32)
		}, func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(0))
		})

		res, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if err != nil {
			t.Fatalf("non-canonical value should keep message structurally valid: %v", err)
		}
		if res.valueCanonical {
			t.Fatal("expected non-canonical value to be reported")
		}
	})

	t.Run("reports non canonical extra flags as skippable", func(t *testing.T) {
		msg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(700743))
		}, func(b *cell.Builder) {
			b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
		})

		res, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if err != nil {
			t.Fatalf("extra flags should not be fatal: %v", err)
		}
		if res.extraFlagsCanonical {
			t.Fatal("expected non-canonical extra flags to be reported")
		}
	})

	t.Run("reports truncated extra flags as invalid relaxed message", func(t *testing.T) {
		msg := transactionTestTruncatedRelaxedInternalMessage()

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})

	t.Run("reports truncated extra currency marker as invalid relaxed message", func(t *testing.T) {
		msg := transactionTestTruncatedRelaxedInternalMessageExtraCurrencyMarker()

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})

	t.Run("reports truncated fwd fee as invalid relaxed message", func(t *testing.T) {
		msg := transactionTestTruncatedRelaxedInternalMessageFwdFee()

		_, err := transactionValidateRelaxedActionMessageCurrencies(msg)
		if !errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			t.Fatalf("expected invalid relaxed message, got %v", err)
		}
	})
}

func TestTransactionApplyActionsInvalidRelaxedActionMessage(t *testing.T) {
	badInternalDst := buildTransactionOutboundInternalCellWithAddresses(
		t,
		address.NewAddressNone(),
		address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32)),
		1000,
		cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	)
	extInMsg, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		DstAddr: tonopsTestAddr,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external inbound message: %v", err)
	}
	truncatedExtraCurrencyMarker := transactionTestTruncatedRelaxedInternalMessageExtraCurrencyMarker()
	truncatedExtraFlags := transactionTestTruncatedRelaxedInternalMessage()
	truncatedFwdFee := transactionTestTruncatedRelaxedInternalMessageFwdFee()

	for _, tc := range []struct {
		name        string
		mode        uint8
		msg         *cell.Cell
		wantSuccess bool
		wantValid   bool
		wantCode    int32
		wantSkipped uint16
	}{
		{
			name:        "internal with external destination aborts as invalid",
			mode:        64,
			msg:         badInternalDst,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with external destination is skipped with mode 2",
			mode:        2,
			msg:         badInternalDst,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "external inbound action aborts as invalid",
			mode:        0,
			msg:         extInMsg,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "external inbound action is skipped with mode 2",
			mode:        2,
			msg:         extInMsg,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "internal with truncated extra currency marker aborts as invalid",
			mode:        1,
			msg:         truncatedExtraCurrencyMarker,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with truncated extra currency marker is skipped with mode 2",
			mode:        3,
			msg:         truncatedExtraCurrencyMarker,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "internal with truncated extra flags aborts as invalid",
			mode:        1,
			msg:         truncatedExtraFlags,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with truncated extra flags is skipped with mode 2",
			mode:        3,
			msg:         truncatedExtraFlags,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:        "internal with truncated fwd fee aborts as invalid",
			mode:        1,
			msg:         truncatedFwdFee,
			wantSuccess: false,
			wantValid:   false,
			wantCode:    34,
		},
		{
			name:        "internal with truncated fwd fee is skipped with mode 2",
			mode:        3,
			msg:         truncatedFwdFee,
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := applyTransactionSendActionForTest(t, tc.mode, tc.msg)
			if res.phase == nil {
				t.Fatal("missing action phase")
			}
			if res.phase.Success != tc.wantSuccess || res.phase.Valid != tc.wantValid || res.phase.ResultCode != tc.wantCode || res.phase.SkippedActions != tc.wantSkipped || res.phase.MessagesCreated != 0 {
				t.Fatalf("unexpected action phase: %+v", res.phase)
			}
		})
	}
}

func TestTransactionApplyActionsV13InvalidActionFormsAffectValidityLikeCpp(t *testing.T) {
	truncatedNonCanonicalValue := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(tonopsTestAddr).
		MustStoreUInt(1, 4).
		MustStoreUInt(0, 8).
		EndCell()
	extOutMsg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x11}, 32)),
		Body:    cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external outbound message: %v", err)
	}

	for _, tc := range []struct {
		name        string
		actions     []any
		wantSuccess bool
		wantValid   bool
		wantCode    int32
		wantSkipped uint16
	}{
		{
			name:      "send invalid mode aborts as action error",
			actions:   []any{tlb.ActionSendMsg{Mode: 4, Msg: buildTransactionOutboundInternalCell(t, 0)}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name:        "send invalid mode is skipped with ignore errors",
			actions:     []any{tlb.ActionSendMsg{Mode: 6, Msg: buildTransactionOutboundInternalCell(t, 0)}},
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:      "truncated non canonical value aborts as prepass invalid without ignore errors",
			actions:   []any{tlb.ActionSendMsg{Mode: 0, Msg: truncatedNonCanonicalValue}},
			wantCode:  34,
			wantValid: false,
		},
		{
			name:        "truncated non canonical value is skipped with ignore errors",
			actions:     []any{tlb.ActionSendMsg{Mode: 2, Msg: truncatedNonCanonicalValue}},
			wantSuccess: true,
			wantValid:   true,
			wantCode:    0,
			wantSkipped: 1,
		},
		{
			name:      "external outbound invalid mode is not skipped",
			actions:   []any{tlb.ActionSendMsg{Mode: 66, Msg: extOutMsg}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name: "reserve extra currencies aborts as action error",
			actions: []any{tlb.ActionReserveCurrency{
				Mode: 0,
				Currency: tlb.CurrencyCollection{
					Coins:           tlb.FromNanoTONU(0),
					ExtraCurrencies: makeTransactionExtraCurrencies(t, 7, 11),
				},
			}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name: "reserve original balance minus too large amount is regular failure",
			actions: []any{tlb.ActionReserveCurrency{
				Mode: 12,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(2_000_000_000),
				},
			}},
			wantCode:  34,
			wantValid: true,
		},
		{
			name: "change library invalid mode aborts as action error",
			actions: []any{tlb.ActionChangeLibrary{
				Mode:   3,
				LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)},
			}},
			wantCode:  34,
			wantValid: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := applyTransactionActionsForTestWithParams(t, tc.actions, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000_000_000), nil, transactionZeroCurrencyBalance())
			if out.phase == nil {
				t.Fatal("missing action phase")
			}
			if out.phase.Success != tc.wantSuccess || out.phase.Valid != tc.wantValid || out.phase.ResultCode != tc.wantCode || out.phase.SkippedActions != tc.wantSkipped {
				t.Fatalf("unexpected action phase: %+v", out.phase)
			}
		})
	}
}

func TestTransactionApplyActionsV13MessageBalanceExtraSurvivesMode64(t *testing.T) {
	msgBalance := &transactionCurrencyBalance{
		grams: big.NewInt(500),
		extra: map[uint32]*big.Int{
			7: big.NewInt(11),
		},
	}
	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 64,
		Msg:  buildTransactionOutboundInternalCell(t, 0),
	}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000), nil, msgBalance)

	if out.phase == nil || !out.phase.Success || out.phase.MessagesCreated != 1 {
		t.Fatalf("unexpected action phase: %+v", out.phase)
	}
	if out.msgBalanceRemaining.grams.Sign() != 0 {
		t.Fatalf("message balance grams = %s, want 0", out.msgBalanceRemaining.grams)
	}
	if got := out.msgBalanceRemaining.extra[7]; got == nil || got.Int64() != 11 {
		t.Fatalf("message balance extra = %v, want 11", out.msgBalanceRemaining.extra)
	}
}

func TestTransactionApplyActionsV13DeleteRequestIgnoresRemainingExtraCurrencies(t *testing.T) {
	extra := makeTransactionExtraCurrencies(t, 7, 11)
	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 0xA0,
		Msg:  buildTransactionOutboundInternalCell(t, 0),
	}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000), extra, transactionZeroCurrencyBalance())

	if out.phase == nil || !out.phase.Success || out.phase.StatusChange.Type != tlb.AccStatusChangeDeleted {
		t.Fatalf("unexpected action phase: %+v", out.phase)
	}
	if !out.deleteAccount {
		t.Fatal("delete account should be requested when grams are exhausted even if extra currencies remain")
	}
	if out.balance.Sign() != 0 {
		t.Fatalf("remaining grams = %s, want 0", out.balance)
	}
	if transactionExtraDictIsEmpty(out.extraCurrencies) {
		t.Fatal("remaining extra currencies should stay on the account")
	}
}

func TestTransactionProcessReserveCurrencyV13DoesNotReserveOriginalExtraCurrencies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        uint8
		amount      uint64
		wantRemain  uint64
		wantReserve uint64
	}{
		{name: "plus original balance", mode: 4, amount: 0, wantRemain: 0, wantReserve: 100},
		{name: "original balance minus amount", mode: 12, amount: 40, wantRemain: 40, wantReserve: 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := &transactionCurrencyBalance{
				grams: big.NewInt(100),
				extra: map[uint32]*big.Int{
					7: big.NewInt(11),
				},
			}
			remaining := original.copy()
			reserved := transactionZeroCurrencyBalance()

			res, err := transactionProcessReserveAction(tlb.ActionReserveCurrency{
				Mode: tc.mode,
				Currency: tlb.CurrencyCollection{
					Coins: tlb.FromNanoTONU(tc.amount),
				},
			}, original, remaining, reserved)
			if err != nil {
				t.Fatal(err)
			}
			if res.resultCode != 0 {
				t.Fatalf("reserve failed with code %d", res.resultCode)
			}
			if remaining.grams.Uint64() != tc.wantRemain || reserved.grams.Uint64() != tc.wantReserve {
				t.Fatalf("grams remaining/reserved = %s/%s, want %d/%d", remaining.grams, reserved.grams, tc.wantRemain, tc.wantReserve)
			}
			if got := remaining.extra[7]; got == nil || got.Int64() != 11 {
				t.Fatalf("remaining extra = %v, want currency 7 preserved", remaining.extra)
			}
			if !reserved.extraEmpty() {
				t.Fatalf("reserved extra = %v, want empty", reserved.extra)
			}
		})
	}
}

func TestTransactionApplyActionsV13InsufficientFundsForMessageCellsReturnsCode40(t *testing.T) {
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		t.Fatalf("failed to build msg prices: %v", err)
	}
	cfg := transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamMsgForwardPricesBasechain:   priceCell,
		tlb.ConfigParamMsgForwardPricesMasterchain: priceCell,
	})
	body := transactionTestCellChain(20)
	out := applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{
		Mode: 0,
		Msg:  buildTransactionOutboundInternalCellWithBody(t, 10, body),
	}, cfg, big.NewInt(10), nil, transactionZeroCurrencyBalance())

	if out.phase == nil || out.phase.Success || out.phase.ResultCode != 40 || out.phase.NoFunds {
		t.Fatalf("unexpected action phase: %+v", out.phase)
	}
	if out.phase.TotalActionFees == nil || out.phase.TotalActionFees.Nano().Int64() != 10 {
		t.Fatalf("action fine = %v, want 10", out.phase.TotalActionFees)
	}
	if out.balance.Sign() != 0 {
		t.Fatalf("remaining balance = %s, want 0", out.balance)
	}
}

func applyTransactionSendActionForTest(t *testing.T, mode uint8, msg *cell.Cell) *transactionActionApplyResult {
	t.Helper()

	return applyTransactionSendActionForTestWithParams(t, tlb.ActionSendMsg{Mode: mode, Msg: msg}, transactionTestConfigWithGlobalVersion(t, 13), big.NewInt(1_000_000_000), nil, transactionZeroCurrencyBalance())
}

func applyTransactionSendActionForTestWithParams(t *testing.T, act tlb.ActionSendMsg, cfg tlb.BlockchainConfig, balance *big.Int, extra *cell.Dictionary, msgBalance *transactionCurrencyBalance) *transactionActionApplyResult {
	t.Helper()

	return applyTransactionActionsForTestWithParams(t, []any{act}, cfg, balance, extra, msgBalance)
}

func applyTransactionActionsForTestWithParams(t *testing.T, actions []any, cfg tlb.BlockchainConfig, balance *big.Int, extra *cell.Dictionary, msgBalance *transactionCurrencyBalance) *transactionActionApplyResult {
	t.Helper()

	data := cell.BeginCell().EndCell()
	res := &MessageExecutionResult{
		Accepted: true,
		ExecutionResult: ExecutionResult{
			ExitCode:  0,
			Data:      data,
			Actions:   buildTransactionActionList(t, actions...),
			Committed: true,
		},
	}
	acc := &transactionRuntimeAccount{
		addr:    tonopsTestAddr,
		status:  tlb.AccountStatusActive,
		code:    cell.BeginCell().EndCell(),
		data:    data,
		balance: new(big.Int).Set(balance),
	}
	out, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), cfg, new(big.Int).Set(balance), extra, msgBalance, big.NewInt(0))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func transactionTestConfigWithGlobalVersion(t *testing.T, version uint32) tlb.BlockchainConfig {
	t.Helper()

	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
	})
}

func transactionTestConfigWithParams(t *testing.T, params map[uint32]*cell.Cell) tlb.BlockchainConfig {
	t.Helper()

	if _, ok := params[tlb.ConfigParamGlobalVersion]; !ok {
		params[tlb.ConfigParamGlobalVersion] = transactionTestGlobalVersionCell(t, 13)
	}
	return tlb.BlockchainConfig{
		Root: buildTransactionConfigRoot(t, params),
	}
}

func transactionTestGlobalVersionCell(t *testing.T, version uint32) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version cell: %v", err)
	}
	return versionCell
}

func transactionTestCellChain(cells int) *cell.Cell {
	var next *cell.Cell
	for i := 0; i < cells; i++ {
		builder := cell.BeginCell().MustStoreUInt(uint64(i&0xff), 8)
		if next != nil {
			builder.MustStoreRef(next)
		}
		next = builder.EndCell()
	}
	return next
}

func transactionTestRelaxedInternalMessage(storeValue, storeExtraFlags func(*cell.Builder)) *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBuilder(transactionTestBuilder(storeValue)).
		MustStoreBoolBit(false).
		MustStoreBuilder(transactionTestBuilder(storeExtraFlags)).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreUInt(0, 64).
		MustStoreUInt(0, 32).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		EndCell()
}

func transactionTestTruncatedRelaxedInternalMessage() *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		MustStoreBoolBit(false).
		MustStoreUInt(1, 2).
		EndCell()
}

func transactionTestTruncatedRelaxedInternalMessageExtraCurrencyMarker() *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		EndCell()
}

func transactionTestTruncatedRelaxedInternalMessageFwdFee() *cell.Cell {
	return cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(address.NewAddress(0, 0, make([]byte, 32))).
		MustStoreBigCoins(big.NewInt(1)).
		MustStoreBoolBit(false).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreUInt(1, 2).
		EndCell()
}

func transactionTestBuilder(fn func(*cell.Builder)) *cell.Builder {
	b := cell.BeginCell()
	fn(b)
	return b
}

func lazyTransactionTestRoot(t *testing.T, root *cell.Cell) *cell.Cell {
	t.Helper()

	roots, _, err := cell.FromBOCMultiRootReader(
		cell.NewBOCNoCopyReader(root.ToBOCWithOptions(cell.BOCSerializeOptions{WithIndex: true})),
		cell.BOCParseOptions{Lazy: true, TrustedHashes: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("lazy parse returned %d roots, want 1", len(roots))
	}
	return roots[0]
}
