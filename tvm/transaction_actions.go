package tvm

import (
	"errors"
	"fmt"
	"math/big"
	"math/bits"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type transactionActionApplyResult struct {
	outMsgs             []OutMessage
	phase               *tlb.ActionPhase
	nextCode            *cell.Cell
	nextLibraries       *cell.Dictionary
	extraCurrencies     *cell.Dictionary
	endLT               uint64
	balance             *big.Int
	actionFees          *big.Int
	actionFine          *big.Int
	msgBalanceRemaining *transactionCurrencyBalance
	bounce              bool
	deleteAccount       bool
}

type transactionSendActionResult struct {
	msgCell         *cell.Cell
	msg             *tlb.Message
	usage           transactionUsage
	debit           *transactionCurrencyBalance
	totalFwdFees    *big.Int
	totalActionFees *big.Int
	actionFine      *big.Int
	resultCode      int32
	clearMsgBalance bool
	skipped         bool
	ignored         bool
	invalid         bool
	bounceOnFail    bool
	deleteAccount   bool
}

type transactionReserveActionResult struct {
	resultCode   int32
	bounceOnFail bool
}

type transactionChangeLibraryActionResult struct {
	nextLibraries *cell.Dictionary
	resultCode    int32
	bounceOnFail  bool
}

type transactionActionEntry struct {
	action  any
	skipped bool
}

var errTransactionInvalidRelaxedActionMessage = errors.New("invalid relaxed action message")

type transactionNormalizedOutboundMessage struct {
	cell   *cell.Cell
	msg    tlb.Message
	layout transactionOutboundLayout
	stats  transactionMessageStatsResult
}

type transactionRelaxedActionMessageValidation struct {
	valueCanonical      bool
	extraFlagsCanonical bool
	fwdFeeCanonical     bool
	layout              transactionOutboundLayout
	layoutKnown         bool
}

type transactionActionLoadResult struct {
	actions        []transactionActionEntry
	totalActions   uint16
	skippedActions uint16
	resultCode     int32
	resultArg      *int32
	bounce         bool
}

func transactionApplyActions(acc *transactionRuntimeAccount, res *MessageExecutionResult, startLT uint64, now uint32, cfg *PreparedConfig, balanceAfterGas *big.Int, extraCurrencies *cell.Dictionary, msgBalance *transactionCurrencyBalance, gasFees *big.Int) (*transactionActionApplyResult, error) {
	computeSuccess := transactionComputeSucceeded(res)
	endLT := startLT + 1
	out := &transactionActionApplyResult{
		nextCode:            acc.code,
		nextLibraries:       acc.libraries,
		extraCurrencies:     extraCurrencies,
		endLT:               endLT,
		balance:             transactionBigOrZero(balanceAfterGas),
		actionFees:          big.NewInt(0),
		actionFine:          big.NewInt(0),
		msgBalanceRemaining: msgBalance.copy(),
	}
	if !computeSuccess {
		return out, nil
	}

	actionsRoot := res.Actions
	if actionsRoot == nil {
		actionsRoot = cell.BeginCell().EndCell()
	}

	globalVersion := cfg.globalVersion()
	loadedActions, err := transactionLoadActions(actionsRoot, globalVersion)
	if err != nil {
		return nil, err
	}
	actionPhase := &tlb.ActionPhase{
		Success:        false,
		Valid:          loadedActions.resultCode == 0,
		NoFunds:        false,
		StatusChange:   tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged},
		ResultCode:     -1,
		ActionListHash: actionsRoot.Hash(),
		TotalActions:   loadedActions.totalActions,
		SkippedActions: loadedActions.skippedActions,
		TotalMsgSize: tlb.StorageUsedShort{
			Cells: big.NewInt(0),
			Bits:  big.NewInt(0),
		},
	}
	out.phase = actionPhase
	if loadedActions.resultCode != 0 {
		actionPhase.ResultCode = loadedActions.resultCode
		actionPhase.ResultArg = loadedActions.resultArg
		out.bounce = loadedActions.bounce
		return out, nil
	}

	actions := loadedActions.actions
	outMsgs := make([]OutMessage, 0, len(actions))
	totalUsage := transactionUsage{}
	totalFwdFees := big.NewInt(0)
	totalActionFees := big.NewInt(0)
	actionFine := big.NewInt(0)
	specActions := uint16(0)
	nextCode := acc.code
	nextLibraries := acc.libraries
	remainingBalance, err := transactionCurrencyFromParts(balanceAfterGas, extraCurrencies)
	if err != nil {
		return nil, err
	}
	msgBalanceRemaining := msgBalance.copy()
	reservedBalance := transactionZeroCurrencyBalance()
	originalBalance := remainingBalance.copy()
	if gasFees != nil {
		originalBalance.grams.Add(originalBalance.grams, gasFees)
	}
	if !originalBalance.sub(msgBalance) {
		originalBalance = remainingBalance.copy()
	}
	lastProcessedActionIdx := -1
	msgBalanceBeforeActions := msgBalanceRemaining.copy()
	failedActionMsgBalance := func() *transactionCurrencyBalance {
		if globalVersion >= 14 {
			return msgBalanceBeforeActions.copy()
		}
		return msgBalanceRemaining.copy()
	}

	failAction := func(resultCode int32, idx int, bounceOnFail bool, noFunds bool, valid bool) {
		stateLimitExceeded, stateLimitErr := transactionAccountStateExceedsLimits(acc, acc.code, acc.data, nextLibraries, cfg)
		if stateLimitErr != nil {
			err = stateLimitErr
			return
		}
		if stateLimitExceeded {
			resultCode = 50
			bounceOnFail = true
			nextLibraries = acc.libraries
		}

		actionPhase.ResultCode = resultCode
		actionPhase.Valid = valid
		actionPhase.NoFunds = noFunds
		actionPhase.ResultArg = transactionActionResultArg(idx)
		actionPhase.SpecActions = specActions
		actionPhase.MessagesCreated = uint16(len(outMsgs))
		actionPhase.TotalFwdFees = transactionCoinsPtr(totalFwdFees)
		failureActionFees := new(big.Int).Set(totalActionFees)
		if noFunds || resultCode == 50 {
			actionPhase.TotalActionFees = transactionCoinsPtr(actionFine)
		} else {
			actionPhase.TotalActionFees = transactionCoinsPtr(failureActionFees)
		}
		actionPhase.TotalMsgSize = tlb.StorageUsedShort{
			Cells: new(big.Int).SetUint64(totalUsage.cells),
			Bits:  new(big.Int).SetUint64(totalUsage.bits),
		}
		if bounceOnFail {
			out.bounce = true
		}
		out.nextLibraries = nextLibraries
		out.msgBalanceRemaining = failedActionMsgBalance()
		out.actionFine = new(big.Int).Set(actionFine)
		out.actionFees = new(big.Int).Set(actionFine)
		out.balance = new(big.Int).Sub(transactionBigOrZero(balanceAfterGas), actionFine)
		if out.balance.Sign() < 0 {
			out.balance.SetInt64(0)
		}
	}

	for i, action := range actions {
		if action.skipped {
			continue
		}
		lastProcessedActionIdx = i
		switch act := action.action.(type) {
		case tlb.ActionSendMsg:
			sendRes, err := transactionProcessSendAction(acc, act, startLT+1+uint64(len(outMsgs)), now, cfg, globalVersion, remainingBalance, msgBalanceRemaining, gasFees, actionFine)
			if err != nil {
				return nil, err
			}
			if sendRes.actionFine.Sign() > 0 {
				fine := transactionMinBig(sendRes.actionFine, remainingBalance.grams)
				remainingBalance.grams.Sub(remainingBalance.grams, fine)
				actionFine.Add(actionFine, fine)
				totalActionFees.Add(totalActionFees, fine)
			}
			if sendRes.skipped {
				actionPhase.SkippedActions++
				continue
			}
			if sendRes.ignored {
				continue
			}
			if sendRes.resultCode != 0 {
				failAction(sendRes.resultCode, i, sendRes.bounceOnFail, sendRes.resultCode == 37 || sendRes.resultCode == 38, !sendRes.invalid)
				if err != nil {
					return nil, err
				}
				return out, nil
			}
			if !remainingBalance.sub(sendRes.debit) {
				failAction(37, i, sendRes.bounceOnFail, true, true)
				if err != nil {
					return nil, err
				}
				return out, nil
			}
			if sendRes.clearMsgBalance {
				msgBalanceRemaining.grams.SetInt64(0)
			}
			outMsgs = append(outMsgs, OutMessage{Cell: sendRes.msgCell, Msg: sendRes.msg})
			totalUsage = transactionAddUsage(totalUsage, sendRes.usage)
			totalFwdFees.Add(totalFwdFees, sendRes.totalFwdFees)
			totalActionFees.Add(totalActionFees, sendRes.totalActionFees)
			if sendRes.deleteAccount {
				out.deleteAccount = remainingBalance.grams.Sign() == 0 && reservedBalance.grams.Sign() == 0
			}
		case tlb.ActionSetCode:
			specActions++
			nextCode = act.NewCode
		case tlb.ActionReserveCurrency:
			reserveRes, err := transactionProcessReserveAction(act, originalBalance, remainingBalance, reservedBalance, globalVersion)
			if err != nil {
				return nil, err
			}
			if reserveRes.resultCode != 0 {
				failAction(reserveRes.resultCode, i, reserveRes.bounceOnFail, reserveRes.resultCode == 37 || reserveRes.resultCode == 38, true)
				if err != nil {
					return nil, err
				}
				return out, nil
			}
			specActions++
		case tlb.ActionChangeLibrary:
			libRes, err := transactionProcessChangeLibraryAction(act, nextLibraries, cfg, globalVersion)
			if err != nil {
				return nil, err
			}
			if libRes.nextLibraries != nil {
				nextLibraries = libRes.nextLibraries
			}
			if libRes.resultCode != 0 {
				failAction(libRes.resultCode, i, libRes.bounceOnFail, false, true)
				if err != nil {
					return nil, err
				}
				out.nextLibraries = nextLibraries
				return out, nil
			}
			specActions++
		default:
			failAction(34, i, false, false, false)
			if err != nil {
				return nil, err
			}
			return out, nil
		}
	}

	remainingBalance.add(reservedBalance)

	stateLimitExceeded, err := transactionAccountStateExceedsLimits(acc, nextCode, res.Data, nextLibraries, cfg)
	if err != nil {
		return nil, err
	}
	if stateLimitExceeded {
		actionPhase.Valid = true
		actionPhase.ResultCode = 50
		resultArgIdx := lastProcessedActionIdx
		if resultArgIdx < 0 && len(actions) > 0 {
			resultArgIdx = len(actions) - 1
		}
		if resultArgIdx >= 0 {
			actionPhase.ResultArg = transactionActionResultArg(resultArgIdx)
		}
		actionPhase.SpecActions = specActions
		actionPhase.MessagesCreated = uint16(len(outMsgs))
		actionPhase.TotalFwdFees = transactionCoinsPtr(totalFwdFees)
		actionPhase.TotalActionFees = transactionCoinsPtr(actionFine)
		actionPhase.TotalMsgSize = tlb.StorageUsedShort{
			Cells: new(big.Int).SetUint64(totalUsage.cells),
			Bits:  new(big.Int).SetUint64(totalUsage.bits),
		}
		out.bounce = true
		out.actionFine = actionFine
		out.actionFees = actionFine
		out.msgBalanceRemaining = failedActionMsgBalance()
		out.balance = new(big.Int).Sub(transactionBigOrZero(balanceAfterGas), actionFine)
		if out.balance.Sign() < 0 {
			out.balance.SetInt64(0)
		}
		return out, nil
	}

	endLT = startLT + 1 + uint64(len(outMsgs))
	actionPhase.Success = true
	actionPhase.Valid = true
	actionPhase.NoFunds = false
	actionPhase.ResultCode = 0
	actionPhase.ResultArg = nil
	actionPhase.TotalActions = uint16(len(actions))
	actionPhase.SpecActions = specActions
	actionPhase.MessagesCreated = uint16(len(outMsgs))
	if out.deleteAccount {
		actionPhase.StatusChange = tlb.AccStatusChange{Type: tlb.AccStatusChangeDeleted}
	}
	actionPhase.TotalFwdFees = transactionCoinsPtr(totalFwdFees)
	actionPhase.TotalActionFees = transactionCoinsPtr(totalActionFees)
	actionPhase.TotalMsgSize = tlb.StorageUsedShort{
		Cells: new(big.Int).SetUint64(totalUsage.cells),
		Bits:  new(big.Int).SetUint64(totalUsage.bits),
	}
	extraDict, err := remainingBalance.extraDict()
	if err != nil {
		return nil, err
	}
	out.outMsgs = outMsgs
	out.nextCode = nextCode
	out.nextLibraries = nextLibraries
	out.extraCurrencies = extraDict
	out.endLT = endLT
	out.balance = remainingBalance.grams
	out.actionFees = totalActionFees
	out.actionFine = actionFine
	out.msgBalanceRemaining = msgBalanceRemaining.copy()
	return out, nil
}

func transactionLoadActions(root *cell.Cell, globalVersion uint32) (*transactionActionLoadResult, error) {
	out := &transactionActionLoadResult{}
	if root == nil {
		return out, nil
	}
	if transactionCellIsEmpty(root) {
		return out, nil
	}

	var nodes [256]*cell.Cell
	nodesNum := 0
	for cur := root; cur != nil && !transactionCellIsEmpty(cur); {
		if cur.IsSpecial() {
			out.resultCode = 32
			out.resultArg = transactionActionResultArg(nodesNum)
			return out, nil
		}
		sl, err := cur.BeginParseWithoutTrace()
		if err != nil {
			out.resultCode = 32
			out.resultArg = transactionActionResultArg(nodesNum)
			return out, nil
		}
		if sl.RefsNum() == 0 {
			out.resultCode = 32
			out.resultArg = transactionActionResultArg(nodesNum)
			return out, nil
		}
		prev, err := sl.LoadRefCell()
		if err != nil {
			out.resultCode = 32
			out.resultArg = transactionActionResultArg(nodesNum)
			return out, nil
		}
		nodes[nodesNum] = cur
		nodesNum++
		if nodesNum > 255 {
			out.resultCode = 33
			out.resultArg = transactionActionResultArg(nodesNum)
			return out, nil
		}
		cur = prev
	}

	out.totalActions = uint16(nodesNum)
	actions := make([]transactionActionEntry, nodesNum)
	actionIdx := 0
	for i := nodesNum - 1; i >= 0; i-- {
		node := nodes[i]
		var list tlb.OutList
		if err := transactionParseCell(&list, node); err != nil {
			mode, isSend := transactionMalformedSendMode(node)
			if isSend {
				if globalVersion >= 8 && mode&2 != 0 {
					out.skippedActions++
					actions[actionIdx] = transactionActionEntry{skipped: true}
					actionIdx++
					continue
				}
				if globalVersion >= 4 && mode&16 != 0 {
					out.bounce = true
				}
			}
			out.resultCode = 34
			out.resultArg = transactionActionResultArg(actionIdx)
			return out, nil
		}
		actions[actionIdx] = transactionActionEntry{action: list.Out}
		actionIdx++
	}
	out.actions = actions
	return out, nil
}

func transactionCellIsEmpty(c *cell.Cell) bool {
	return c == nil || (c.BitsSize() == 0 && c.RefsNum() == 0)
}

func transactionParseCell(v any, root *cell.Cell) error {
	sl, err := root.BeginParseWithoutTrace()
	if err != nil {
		return err
	}
	return tlb.LoadFromCell(v, sl)
}

func transactionMalformedSendMode(node *cell.Cell) (uint8, bool) {
	if node == nil || node.IsSpecial() {
		return 0, false
	}
	sl, err := node.BeginParseWithoutTrace()
	if err != nil {
		return 0, false
	}
	if sl.RefsNum() == 0 {
		return 0, false
	}
	if _, err := sl.LoadRefCell(); err != nil {
		return 0, false
	}
	if sl.BitsLeft() < 40 {
		return 0, false
	}
	tag, err := sl.LoadUInt(32)
	if err != nil || tag != 0x0ec3c86d {
		return 0, false
	}
	mode, err := sl.LoadUInt(8)
	if err != nil {
		return 0, false
	}
	return uint8(mode), true
}

func transactionProcessSendAction(acc *transactionRuntimeAccount, act tlb.ActionSendMsg, createdLT uint64, now uint32, cfg *PreparedConfig, globalVersion uint32, remainingBalance, msgBalanceRemaining *transactionCurrencyBalance, gasFees, currentActionFine *big.Int) (*transactionSendActionResult, error) {
	out := &transactionSendActionResult{
		debit:           transactionZeroCurrencyBalance(),
		totalFwdFees:    big.NewInt(0),
		totalActionFees: big.NewInt(0),
		actionFine:      big.NewInt(0),
	}

	mode := act.Mode
	if globalVersion >= 4 && mode&16 != 0 {
		mode &^= 16
		out.bounceOnFail = true
	}

	if transactionSendModeInvalid(mode) {
		if globalVersion >= 13 {
			return transactionSendResultCode(out, mode, 34, globalVersion), nil
		}
		out.resultCode = 34
		return out, nil
	}

	msgValidation, err := transactionValidateRelaxedActionMessageCurrencies(act.Msg)
	if err != nil {
		if errors.Is(err, errTransactionInvalidRelaxedActionMessage) {
			return transactionSendPrepassInvalid(out, mode, globalVersion), nil
		}
		out.resultCode = 34
		return out, nil
	}
	if !msgValidation.valueCanonical || (globalVersion < 8 && (!msgValidation.extraFlagsCanonical || !msgValidation.fwdFeeCanonical)) {
		out.resultCode = 34
		return out, nil
	}

	var suggestedMsg tlb.Message
	if err := transactionParseCell(&suggestedMsg, act.Msg); err != nil {
		return transactionSendPrepassInvalid(out, mode, globalVersion), nil
	}
	if err = transactionValidateMessageStateInitLibs(&suggestedMsg); err != nil {
		return transactionSendPrepassInvalidV13(out, mode, globalVersion), nil
	}
	var normalizedInternalDst *address.Address
	switch suggestedMsg.MsgType {
	case tlb.MsgTypeInternal:
		intMsg := suggestedMsg.AsInternal()
		if !transactionOutboundSourceValid(intMsg.SrcAddr, acc.addr) {
			return transactionSendInvalidSource(out, mode, globalVersion), nil
		}
		if !transactionOutboundInternalDestTypeValid(intMsg.DstAddr) {
			return transactionSendPrepassInvalid(out, mode, globalVersion), nil
		}
		var ok bool
		normalizedInternalDst, ok = transactionValidateAndNormalizeInternalDestAddr(intMsg.DstAddr, cfg, acc.addr)
		if !ok {
			return transactionSendInvalidDestination(out, mode, globalVersion), nil
		}
	case tlb.MsgTypeExternalOut:
		if !transactionOutboundSourceValid(suggestedMsg.AsExternalOut().SrcAddr, acc.addr) {
			return transactionSendInvalidSource(out, mode, globalVersion), nil
		}
	default:
		return transactionSendPrepassInvalid(out, mode, globalVersion), nil
	}

	layout := msgValidation.layout
	if !msgValidation.layoutKnown {
		layout, err = transactionOutboundMessageLayout(act.Msg)
		if err != nil {
			return transactionSendPrepassInvalid(out, mode, globalVersion), nil
		}
	}

	normalized, err := transactionPrepareNormalizedOutboundMessage(&suggestedMsg, layout, normalizedInternalDst, acc.addr, createdLT, now, cfg)
	if err != nil {
		if errors.Is(err, errTransactionInvalidDestination) {
			return transactionSendInvalidDestination(out, mode, globalVersion), nil
		}
		out.resultCode = 34
		return out, nil
	}

	msgCell := normalized.cell
	msg := normalized.msg

	switch msg.MsgType {
	case tlb.MsgTypeExternalOut:
		extMsg := msg.AsExternalOut()
		if extMsg.DstAddr != nil && extMsg.DstAddr.Type() != address.NoneAddress && extMsg.DstAddr.Type() != address.ExtAddress {
			return transactionSendPrepassInvalid(out, mode, globalVersion), nil
		}
		if mode&^uint8(3) != 0 {
			out.resultCode = 34
			return out, nil
		}
		actionFineEnabled := globalVersion >= 4
		sizeCode, fine := transactionCheckOutboundMessageStatsSize(cfg, acc.addr, msg.Msg.DestAddr(), normalized.stats, remainingBalance.grams, acc.isSpecial, actionFineEnabled)
		if sizeCode != 0 {
			if actionFineEnabled {
				out.actionFine = fine
			}
			return transactionSendResultCode(out, mode, sizeCode, globalVersion), nil
		}
		fwdFee := transactionComputeForwardFeeForUsage(cfg, acc.addr, msg.Msg.DestAddr(), normalized.stats.usage)
		if acc.isSpecial {
			fwdFee.SetInt64(0)
		}
		if remainingBalance.grams.Cmp(fwdFee) < 0 {
			if actionFineEnabled && !acc.isSpecial {
				out.actionFine = transactionComputeActionFineForUsage(cfg, acc.addr, msg.Msg.DestAddr(), normalized.stats.usage, remainingBalance.grams)
			}
			return transactionSendResultCode(out, mode, 37, globalVersion), nil
		}
		out.msgCell = msgCell
		out.msg = &msg
		out.usage = normalized.stats.totalUsage
		out.debit.grams = fwdFee
		out.totalFwdFees = fwdFee
		out.totalActionFees = fwdFee
		return out, nil
	case tlb.MsgTypeInternal:
		intMsg := *msg.AsInternal()
		if globalVersion >= 11 {
			intMsg.IHRDisabled = true
		}
		extraFlags := transactionBigOrZero(intMsg.IHRFee.Nano())
		if globalVersion >= 12 && (!msgValidation.extraFlagsCanonical || !extraFlags.IsUint64() || extraFlags.Uint64()&^uint64(3) != 0) {
			return transactionSendResultCode(out, mode, 45, globalVersion), nil
		}

		req, err := transactionCurrencyFromParts(intMsg.Amount.Nano(), intMsg.ExtraCurrencies)
		if err != nil {
			return transactionSendResultCode(out, mode, 37, globalVersion), nil
		}
		req.removeZeroExtra()
		if transactionExtraCount(req.extra) > transactionGetSizeLimits(cfg).maxMsgExtraCurrencies {
			return transactionSendResultCode(out, mode, 44, globalVersion), nil
		}
		fineFunds, ok := transactionSendActionFineFunds(remainingBalance.grams, msgBalanceRemaining.grams, req.grams, gasFees, currentActionFine, mode)
		if !ok {
			return transactionSendResultCode(out, mode, 37, globalVersion), nil
		}
		actionFineEnabled := globalVersion >= 4
		sizeCode, fine := transactionCheckOutboundMessageStatsSize(cfg, acc.addr, intMsg.DstAddr, normalized.stats, fineFunds, acc.isSpecial, actionFineEnabled)
		if sizeCode != 0 {
			if actionFineEnabled {
				out.actionFine = fine
			}
			return transactionSendResultCode(out, mode, sizeCode, globalVersion), nil
		}

		baseReq := req.copy()
		sendMode := mode
		layoutForFees := normalized.layout
		for attempt := 0; attempt < 3; attempt++ {
			prepared, usedLayout, err := transactionPrepareInternalSendAction(
				out, acc, &intMsg, layoutForFees, sendMode, extraFlags, cfg,
				globalVersion, remainingBalance, msgBalanceRemaining, baseReq, gasFees, currentActionFine,
			)
			if err != nil {
				return nil, err
			}
			if prepared.resultCode != 0 || usedLayout == layoutForFees {
				return prepared, nil
			}
			layoutForFees = usedLayout
		}
		return nil, errors.New("failed to stabilize outbound internal message layout")
	default:
		out.resultCode = 34
		return out, nil
	}
}

func transactionValidateRelaxedActionMessageCurrencies(root *cell.Cell) (transactionRelaxedActionMessageValidation, error) {
	out := transactionRelaxedActionMessageValidation{
		valueCanonical:      true,
		extraFlagsCanonical: true,
		fwdFeeCanonical:     true,
	}
	if root == nil {
		return out, fmt.Errorf("%w: outbound message cell is nil", errTransactionInvalidRelaxedActionMessage)
	}

	sl, err := root.BeginParseWithoutTrace()
	if err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}

	isExternal, err := sl.LoadBoolBit()
	if err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	if isExternal {
		isOut, err := sl.LoadBoolBit()
		if err != nil {
			return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
		}
		if !isOut {
			return out, fmt.Errorf("%w: external inbound message is not relaxed", errTransactionInvalidRelaxedActionMessage)
		}
		return out, nil
	}

	if err = sl.SkipBitsAndRefs(3, 0); err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	if _, err = sl.LoadAddr(); err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	dst, err := sl.LoadAddr()
	if err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	if !transactionOutboundInternalDestTypeValid(dst) {
		return out, fmt.Errorf("%w: relaxed internal message destination is not MsgAddressInt", errTransactionInvalidRelaxedActionMessage)
	}

	out.valueCanonical, err = transactionLoadRelaxedVarUIntCanonical(sl, 16, false)
	if err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	extraCanonical, err := transactionValidateCanonicalExtraCurrencyCollection(sl)
	if err != nil {
		return out, err
	}
	out.valueCanonical = out.valueCanonical && extraCanonical
	out.extraFlagsCanonical, err = transactionLoadRelaxedVarUIntCanonical(sl, 16, false)
	if err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	out.fwdFeeCanonical, err = transactionLoadRelaxedVarUIntCanonical(sl, 16, false)
	if err != nil {
		return out, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	layout, err := transactionValidateRelaxedActionMessageTail(sl)
	if err != nil {
		return out, err
	}
	out.layout = layout
	out.layoutKnown = true
	return out, nil
}

func transactionValidateCanonicalExtraCurrencyCollection(sl *cell.Slice) (bool, error) {
	has, err := sl.LoadBoolBit()
	if err != nil {
		return false, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	if !has {
		return true, nil
	}

	root, err := sl.LoadRefCell()
	if err != nil {
		return false, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	items, err := root.AsDict(32).LoadAll()
	if err != nil {
		return false, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}

	canonical := true
	for _, item := range items {
		itemCanonical, err := transactionLoadRelaxedVarUIntCanonical(item.Value, 32, true)
		if err != nil {
			return false, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
		}
		canonical = canonical && itemCanonical
		if item.Value.BitsLeft() != 0 || item.Value.RefsNum() != 0 {
			return false, errors.New("extra currency value has trailing data")
		}
	}
	return canonical, nil
}

func transactionLoadRelaxedVarUIntCanonical(sl *cell.Slice, size uint, positive bool) (bool, error) {
	if size == 0 {
		return false, cell.ErrInvalidSize
	}

	lenBits := uint(bits.Len64(uint64(size - 1)))
	ln, err := sl.LoadUInt(lenBits)
	if err != nil {
		return false, err
	}
	if ln >= uint64(size) {
		return false, cell.ErrTooBigValue
	}
	if ln == 0 {
		return !positive, nil
	}

	val, err := sl.LoadBigUInt(uint(ln * 8))
	if err != nil {
		return false, err
	}
	if val.Sign() == 0 {
		return false, nil
	}
	if val.BitLen() <= int((ln-1)*8) {
		return false, nil
	}
	return true, nil
}

func transactionValidateRelaxedActionMessageTail(sl *cell.Slice) (transactionOutboundLayout, error) {
	var layout transactionOutboundLayout
	if err := sl.SkipBitsAndRefs(96, 0); err != nil {
		return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}

	hasInit, err := sl.LoadBoolBit()
	if err != nil {
		return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	if hasInit {
		initInRef, err := sl.LoadBoolBit()
		if err != nil {
			return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
		}
		layout.stateInitInRef = initInRef
		if initInRef {
			if _, err = sl.LoadRefCell(); err != nil {
				return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
			}
		} else {
			var init tlb.StateInit
			if err = tlb.LoadFromCell(&init, sl); err != nil {
				return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
			}
		}
	}

	bodyInRef, err := sl.LoadBoolBit()
	if err != nil {
		return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
	}
	layout.bodyInRef = bodyInRef
	if bodyInRef {
		if _, err = sl.LoadRefCell(); err != nil {
			return layout, fmt.Errorf("%w: %v", errTransactionInvalidRelaxedActionMessage, err)
		}
		if sl.BitsLeft() != 0 || sl.RefsNum() != 0 {
			return layout, fmt.Errorf("%w: trailing data after body reference", errTransactionInvalidRelaxedActionMessage)
		}
	}
	return layout, nil
}

func transactionPrepareInternalSendAction(out *transactionSendActionResult, acc *transactionRuntimeAccount, intMsg *tlb.InternalMessage, layout transactionOutboundLayout, sendMode uint8, extraFlags *big.Int, cfg *PreparedConfig, globalVersion uint32, remainingBalance, msgBalanceRemaining, baseReq *transactionCurrencyBalance, gasFees, currentActionFine *big.Int) (*transactionSendActionResult, transactionOutboundLayout, error) {
	res := &transactionSendActionResult{
		debit:           transactionZeroCurrencyBalance(),
		totalFwdFees:    big.NewInt(0),
		totalActionFees: big.NewInt(0),
		actionFine:      big.NewInt(0),
		bounceOnFail:    out.bounceOnFail,
	}

	feeUsage, err := transactionOutboundInternalMessageFeeUsage(cfg, intMsg, layout)
	if err != nil {
		return nil, layout, err
	}
	actionFine := big.NewInt(0)
	maxFineCells := transactionGetSizeLimits(cfg).maxMsgCells
	fineLimitedByFunds := false
	if globalVersion >= 4 && !acc.isSpecial {
		actionFine, maxFineCells, fineLimitedByFunds = transactionComputeSendActionFineForUsage(cfg, acc.addr, intMsg.DstAddr, feeUsage, remainingBalance.grams, msgBalanceRemaining.grams, baseReq.grams, gasFees, currentActionFine, sendMode)
	}
	computedFwdFee := transactionComputeForwardFeeForUsage(cfg, acc.addr, intMsg.DstAddr, feeUsage)
	computedIHRFee := transactionComputeIHRFee(cfg, acc.addr, intMsg.DstAddr, computedFwdFee, intMsg.IHRDisabled)
	if acc.isSpecial {
		computedFwdFee.SetInt64(0)
		computedIHRFee.SetInt64(0)
	}
	fwdFee := computedFwdFee
	ihrFee := computedIHRFee
	if globalVersion < 8 {
		if suggestedFwdFee := transactionBigOrZero(intMsg.FwdFee.Nano()); suggestedFwdFee.Cmp(fwdFee) > 0 {
			fwdFee = suggestedFwdFee
		}
		if !intMsg.IHRDisabled && extraFlags.Cmp(ihrFee) > 0 {
			ihrFee = new(big.Int).Set(extraFlags)
		}
	}
	totalFees := new(big.Int).Add(fwdFee, ihrFee)
	collectedFwdFee := transactionFirstPartForwardFee(cfg, acc.addr, intMsg.DstAddr, fwdFee)
	remainingFwdFee := new(big.Int).Sub(fwdFee, collectedFwdFee)

	req := baseReq.copy()
	mode := sendMode
	clearMsgBalance := false
	if mode&128 != 0 {
		req.grams = new(big.Int).Set(remainingBalance.grams)
		mode &^= 1
		clearMsgBalance = true
	} else if mode&64 != 0 {
		req.grams.Add(req.grams, msgBalanceRemaining.grams)
		clearMsgBalance = true
		if mode&1 == 0 {
			req.grams.Sub(req.grams, gasFees)
			if currentActionFine != nil {
				req.grams.Sub(req.grams, currentActionFine)
			}
			if req.grams.Sign() < 0 {
				return transactionSendResultCode(res, mode, 37, globalVersion), layout, nil
			}
		}
	}
	if fineLimitedByFunds && feeUsage.cells > maxFineCells {
		res.actionFine = actionFine
		return transactionSendResultCode(res, mode, 40, globalVersion), layout, nil
	}

	debit := req.copy()
	msgAmount := new(big.Int).Set(req.grams)
	if mode&1 != 0 {
		debit.grams.Add(debit.grams, totalFees)
	} else {
		if msgAmount.Cmp(totalFees) < 0 {
			res.actionFine = actionFine
			return transactionSendResultCode(res, mode, 37, globalVersion), layout, nil
		}
		msgAmount.Sub(msgAmount, totalFees)
	}
	if remainingBalance.grams.Cmp(debit.grams) < 0 {
		res.actionFine = actionFine
		return transactionSendResultCode(res, mode, 37, globalVersion), layout, nil
	}
	if !remainingBalance.hasExtra(debit.extra) {
		res.actionFine = actionFine
		return transactionSendResultCode(res, mode, 38, globalVersion), layout, nil
	}
	if transactionExtraCount(req.extra) > transactionGetSizeLimits(cfg).maxMsgExtraCurrencies {
		return transactionSendResultCode(res, mode, 44, globalVersion), layout, nil
	}

	outMsg := *intMsg
	outMsg.Amount = tlb.FromNanoTON(msgAmount)
	outMsg.ExtraCurrencies, err = req.extraDict()
	if err != nil {
		return nil, layout, err
	}
	outMsg.FwdFee = tlb.FromNanoTON(remainingFwdFee)
	if globalVersion < 12 {
		outMsg.IHRFee = tlb.FromNanoTON(ihrFee)
	} else {
		outMsg.IHRFee = tlb.FromNanoTON(extraFlags)
	}
	msgCell, usedLayout, err := transactionInternalMessageToCellWithLayout(&outMsg, layout)
	if err != nil {
		return nil, layout, fmt.Errorf("failed to serialize outbound internal message: %w", err)
	}

	res.msgCell = msgCell
	res.msg = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &outMsg}
	res.usage, err = transactionOutboundInternalMessageActionUsage(cfg, &outMsg, msgCell, usedLayout)
	if err != nil {
		return nil, layout, err
	}
	res.debit = debit
	res.totalFwdFees = totalFees
	res.totalActionFees = collectedFwdFee
	res.clearMsgBalance = clearMsgBalance
	if sendMode&0xA0 == 0xA0 {
		res.deleteAccount = true
	}
	return res, usedLayout, nil
}

func transactionSendModeInvalid(mode uint8) bool {
	return mode&^uint8(0xE3) != 0 || mode&0xC0 == 0xC0
}

func transactionOutboundSourceValid(src, account *address.Address) bool {
	if src == nil || src.Type() == address.NoneAddress {
		return true
	}
	if src.Type() != address.StdAddress || account == nil || account.Type() != address.StdAddress {
		return false
	}
	return src.Equals(account)
}

func transactionSendInvalidSource(out *transactionSendActionResult, mode uint8, globalVersion uint32) *transactionSendActionResult {
	if globalVersion >= 13 && mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = 35
	return out
}

func transactionSendInvalidDestination(out *transactionSendActionResult, mode uint8, globalVersion uint32) *transactionSendActionResult {
	if mode&2 != 0 {
		if globalVersion < 8 {
			out.ignored = true
			return out
		}
		out.skipped = true
		return out
	}
	out.resultCode = 36
	return out
}

func transactionSendResultCode(out *transactionSendActionResult, mode uint8, code int32, globalVersion uint32) *transactionSendActionResult {
	if globalVersion >= 8 && mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = code
	return out
}

func transactionSendPrepassInvalid(out *transactionSendActionResult, mode uint8, globalVersion uint32) *transactionSendActionResult {
	if globalVersion >= 8 && mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = 34
	out.invalid = true
	return out
}

func transactionSendPrepassInvalidV13(out *transactionSendActionResult, mode uint8, globalVersion uint32) *transactionSendActionResult {
	if globalVersion >= 13 && mode&2 != 0 {
		out.skipped = true
		return out
	}
	out.resultCode = 34
	out.invalid = true
	return out
}

func transactionProcessReserveAction(act tlb.ActionReserveCurrency, originalBalance, remainingBalance, reservedBalance *transactionCurrencyBalance, globalVersion uint32) (*transactionReserveActionResult, error) {
	out := &transactionReserveActionResult{}
	mode := act.Mode
	if globalVersion >= 4 && mode&16 != 0 {
		mode &^= 16
		out.bounceOnFail = true
	}
	if mode&^uint8(15) != 0 {
		out.resultCode = 34
		return out, nil
	}

	reserve, err := transactionCurrencyFromCollection(act.Currency)
	if err != nil {
		out.resultCode = 34
		return out, nil
	}
	if globalVersion >= 10 && !reserve.extraEmpty() {
		out.resultCode = 34
		return out, nil
	}

	if mode&4 != 0 {
		if mode&8 != 0 {
			if globalVersion < 10 {
				nextReserve := originalBalance.copy()
				if !nextReserve.sub(reserve) {
					out.resultCode = 34
					return out, nil
				}
				reserve = nextReserve
			} else if originalBalance.grams == nil {
				reserve.grams.Neg(reserve.grams)
			} else {
				reserve.grams.Sub(originalBalance.grams, reserve.grams)
			}
			if reserve.grams.Sign() < 0 {
				out.resultCode = 34
				return out, nil
			}
		} else {
			if globalVersion < 10 {
				reserve.add(originalBalance)
			} else if originalBalance.grams != nil {
				reserve.grams.Add(reserve.grams, originalBalance.grams)
			}
		}
	} else if mode&8 != 0 {
		out.resultCode = 34
		return out, nil
	}

	if reserve.grams.Sign() < 0 {
		out.resultCode = 34
		return out, nil
	}
	if mode&2 != 0 {
		if globalVersion >= 9 {
			reserve.clamp(remainingBalance)
		} else if reserve.grams.Cmp(remainingBalance.grams) > 0 {
			reserve.grams.Set(remainingBalance.grams)
		}
	}
	if reserve.grams.Cmp(remainingBalance.grams) > 0 {
		out.resultCode = 37
		return out, nil
	}
	if !remainingBalance.hasExtra(reserve.extra) {
		out.resultCode = 38
		return out, nil
	}

	newBalance := remainingBalance.copy()
	if !newBalance.sub(reserve) {
		out.resultCode = 38
		return out, nil
	}
	if mode&1 != 0 {
		if globalVersion >= 10 {
			newBalance.grams, reserve.grams = reserve.grams, newBalance.grams
		} else {
			newBalance, reserve = reserve, newBalance
		}
	}

	*remainingBalance = *newBalance
	reservedBalance.add(reserve)
	return out, nil
}

func transactionProcessChangeLibraryAction(act tlb.ActionChangeLibrary, current *cell.Dictionary, cfg *PreparedConfig, globalVersion uint32) (*transactionChangeLibraryActionResult, error) {
	out := &transactionChangeLibraryActionResult{}
	mode := act.Mode
	if globalVersion >= 4 && mode&16 != 0 {
		mode &^= 16
		out.bounceOnFail = true
	}
	if mode > 2 {
		out.resultCode = 34
		return out, nil
	}

	libs := cell.NewDict(256)
	if current != nil && !current.IsEmpty() {
		libs = current.Copy()
	}

	var libHash cell.Hash
	var hasLibHash bool
	var libRef *cell.Cell
	switch ref := act.LibRef.(type) {
	case tlb.LibRefHash:
		rawHash := transactionNormalizeBits256(ref.LibHash)
		if len(rawHash) == 32 {
			copy(libHash[:], rawHash)
			hasLibHash = true
		}
	case tlb.LibRefRef:
		libRef = ref.Library
		if libRef != nil {
			libHash = libRef.HashKey()
			hasLibHash = true
		}
	default:
		out.resultCode = 34
		return out, nil
	}
	if !hasLibHash {
		out.resultCode = 34
		return out, nil
	}

	key := cell.BeginCell().MustStoreSlice(libHash[:], 256).EndCell()
	if mode == 0 {
		if err := libs.Delete(key); err != nil {
			out.resultCode = 42
			return out, nil
		}
		out.nextLibraries = libs
		return out, nil
	}

	if existing, err := libs.LoadValue(key); err == nil && existing != nil {
		isPublic, loadErr := existing.LoadBoolBit()
		existingRef, refErr := existing.LoadRefCell()
		if loadErr == nil && refErr == nil && existingRef != nil && existingRef.HashKey() == libHash {
			libRef = existingRef
			if isPublic == (mode == 2) {
				out.nextLibraries = libs
				return out, nil
			}
		}
	}
	if libRef == nil {
		out.resultCode = 41
		return out, nil
	}

	stats, err := transactionCellStatsForRoots(libRef)
	if err != nil {
		return nil, err
	}
	if stats.usage.cells > transactionGetSizeLimits(cfg).maxLibraryCells || stats.merkleDepth > 2 {
		out.resultCode = 43
		return out, nil
	}
	value := cell.BeginCell().MustStoreBoolBit(mode == 2).MustStoreRef(libRef).EndCell()
	if err := libs.Set(key, value); err != nil {
		out.resultCode = 42
		return out, nil
	}
	out.nextLibraries = libs
	return out, nil
}

func transactionActionResultArg(i int) *int32 {
	if i == 0 {
		return nil
	}
	arg := int32(i)
	return &arg
}

var errTransactionInvalidDestination = errors.New("invalid outbound destination address")

func transactionPrepareNormalizedOutboundMessage(msg *tlb.Message, layout transactionOutboundLayout, normalizedInternalDst, srcAddr *address.Address, createdLT uint64, now uint32, cfg *PreparedConfig) (transactionNormalizedOutboundMessage, error) {
	msgCell, normalizedMsg, err := transactionNormalizeParsedOutboundMessage(msg, layout, normalizedInternalDst, srcAddr, createdLT, now, cfg)
	if err != nil {
		return transactionNormalizedOutboundMessage{}, err
	}

	stats, err := transactionMessageStats(msgCell)
	if err != nil {
		return transactionNormalizedOutboundMessage{}, err
	}

	return transactionNormalizedOutboundMessage{
		cell:   msgCell,
		msg:    normalizedMsg,
		layout: layout,
		stats:  stats,
	}, nil
}

func transactionNormalizeOutboundMessage(msgCell *cell.Cell, srcAddr *address.Address, createdLT uint64, now uint32, cfg *PreparedConfig) (*cell.Cell, error) {
	if msgCell == nil {
		return nil, errors.New("outbound message cell is nil")
	}

	layout, err := transactionOutboundMessageLayout(msgCell)
	if err != nil {
		return nil, fmt.Errorf("failed to decode outbound message layout: %w", err)
	}

	var msg tlb.Message
	if err = transactionParseCell(&msg, msgCell); err != nil {
		return nil, fmt.Errorf("failed to decode outbound message: %w", err)
	}

	normalized, _, err := transactionNormalizeParsedOutboundMessage(&msg, layout, nil, srcAddr, createdLT, now, cfg)
	return normalized, err
}

func transactionNormalizeParsedOutboundMessage(msg *tlb.Message, layout transactionOutboundLayout, normalizedInternalDst, srcAddr *address.Address, createdLT uint64, now uint32, cfg *PreparedConfig) (*cell.Cell, tlb.Message, error) {
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		out := *msg.AsInternal()
		out.SrcAddr = srcAddr
		out.Bounced = false
		if normalizedInternalDst != nil {
			out.DstAddr = normalizedInternalDst
		} else {
			dst, ok := transactionValidateAndNormalizeInternalDestAddr(out.DstAddr, cfg, srcAddr)
			if !ok {
				return nil, tlb.Message{}, errTransactionInvalidDestination
			}
			out.DstAddr = dst
		}
		out.CreatedLT = createdLT
		out.CreatedAt = now
		msgCell, err := transactionInternalMessageToCell(&out, layout)
		if err != nil {
			return nil, tlb.Message{}, err
		}
		return msgCell, tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &out}, nil
	case tlb.MsgTypeExternalOut:
		out := *msg.AsExternalOut()
		out.SrcAddr = srcAddr
		out.CreatedLT = createdLT
		out.CreatedAt = now
		msgCell, err := transactionExternalOutMessageToCell(&out, layout)
		if err != nil {
			return nil, tlb.Message{}, err
		}
		return msgCell, tlb.Message{MsgType: tlb.MsgTypeExternalOut, Msg: &out}, nil
	default:
		return nil, tlb.Message{}, fmt.Errorf("unsupported outbound message type %s", msg.MsgType)
	}
}

type transactionOutboundLayout struct {
	stateInitInRef bool
	bodyInRef      bool
}

func transactionOutboundMessageLayout(msgCell *cell.Cell) (transactionOutboundLayout, error) {
	var msg tlb.MessageRelaxed
	if err := transactionParseCell(&msg, msgCell); err != nil {
		return transactionOutboundLayout{}, err
	}

	if msg.MsgType != tlb.MsgTypeInternal && msg.MsgType != tlb.MsgTypeExternalOut {
		return transactionOutboundLayout{}, errors.New("message is not an outbound action")
	}

	return transactionOutboundLayout{
		stateInitInRef: msg.Init.Exists && msg.Init.InRef,
		bodyInRef:      msg.Body.InRef,
	}, nil
}

func transactionInternalMessageToCell(msg *tlb.InternalMessage, layout transactionOutboundLayout) (*cell.Cell, error) {
	out, _, err := transactionInternalMessageToCellWithLayout(msg, layout)
	return out, err
}

func transactionInternalMessageToCellWithLayout(msg *tlb.InternalMessage, layout transactionOutboundLayout) (*cell.Cell, transactionOutboundLayout, error) {
	moveStateInitOnRetry, err := transactionStateInitRefRetryNeeded(msg.StateInit)
	if err != nil {
		return nil, layout, err
	}

	return transactionMessageToCellWithRetry(layout, moveStateInitOnRetry, func(next transactionOutboundLayout) (*cell.Cell, error) {
		builder := cell.BeginCell().
			MustStoreBoolBit(false).
			MustStoreBoolBit(msg.IHRDisabled).
			MustStoreBoolBit(msg.Bounce).
			MustStoreBoolBit(msg.Bounced).
			MustStoreAddr(msg.SrcAddr).
			MustStoreAddr(msg.DstAddr).
			MustStoreBigCoins(transactionBigOrZero(msg.Amount.Nano())).
			MustStoreDict(msg.ExtraCurrencies).
			MustStoreBigCoins(transactionBigOrZero(msg.IHRFee.Nano())).
			MustStoreBigCoins(transactionBigOrZero(msg.FwdFee.Nano())).
			MustStoreUInt(msg.CreatedLT, 64).
			MustStoreUInt(uint64(msg.CreatedAt), 32)
		if err := transactionStoreStateInit(builder, msg.StateInit, next.stateInitInRef); err != nil {
			return nil, err
		}
		if err := transactionStoreMessageBody(builder, msg.Body, next.bodyInRef); err != nil {
			return nil, err
		}
		return builder.EndCell(), nil
	})
}

func transactionExternalOutMessageToCell(msg *tlb.ExternalMessageOut, layout transactionOutboundLayout) (*cell.Cell, error) {
	moveStateInitOnRetry, err := transactionStateInitRefRetryNeeded(msg.StateInit)
	if err != nil {
		return nil, err
	}

	out, _, err := transactionMessageToCellWithRetry(layout, moveStateInitOnRetry, func(next transactionOutboundLayout) (*cell.Cell, error) {
		builder := cell.BeginCell().
			MustStoreUInt(0b11, 2).
			MustStoreAddr(msg.SrcAddr).
			MustStoreAddr(msg.DstAddr).
			MustStoreUInt(msg.CreatedLT, 64).
			MustStoreUInt(uint64(msg.CreatedAt), 32)
		if err := transactionStoreStateInit(builder, msg.StateInit, next.stateInitInRef); err != nil {
			return nil, err
		}
		if err := transactionStoreMessageBody(builder, msg.Body, next.bodyInRef); err != nil {
			return nil, err
		}
		return builder.EndCell(), nil
	})
	return out, err
}

func transactionMessageToCellWithRetry(layout transactionOutboundLayout, moveStateInitOnRetry bool, build func(transactionOutboundLayout) (*cell.Cell, error)) (*cell.Cell, transactionOutboundLayout, error) {
	out, err := build(layout)
	if err == nil {
		return out, layout, nil
	}
	if !layout.stateInitInRef && moveStateInitOnRetry {
		next := layout
		next.stateInitInRef = true
		if out, retryErr := build(next); retryErr == nil {
			return out, next, nil
		}
		layout = next
	}
	if !layout.bodyInRef {
		next := layout
		next.bodyInRef = true
		out, err = build(next)
		return out, next, err
	}
	return nil, layout, err
}

func transactionStateInitRefRetryNeeded(state *tlb.StateInit) (bool, error) {
	if state == nil {
		return false, nil
	}
	stateCell, err := tlb.ToCell(state)
	if err != nil {
		return false, err
	}
	return stateCell.RefsNum() >= 2, nil
}

func transactionStoreStateInit(builder *cell.Builder, state *tlb.StateInit, inRef bool) error {
	if state == nil {
		builder.MustStoreBoolBit(false)
		return nil
	}

	stateCell, err := tlb.ToCell(state)
	if err != nil {
		return err
	}
	builder.MustStoreBoolBit(true).MustStoreBoolBit(inRef)
	if inRef {
		builder.MustStoreRef(stateCell)
		return nil
	}
	return builder.StoreBuilder(stateCell.ToBuilder())
}

func transactionStoreMessageBody(builder *cell.Builder, body *cell.Cell, inRef bool) error {
	if body == nil {
		body = cell.BeginCell().EndCell()
	}
	builder.MustStoreBoolBit(inRef)
	if inRef {
		builder.MustStoreRef(body)
		return nil
	}
	return builder.StoreBuilder(body.ToBuilder())
}

func transactionValidateAndNormalizeInternalDestAddr(addr *address.Address, cfg *PreparedConfig, srcAddr *address.Address) (*address.Address, bool) {
	return transactionValidateAndNormalizeInternalAddr(addr, cfg, cfg.globalVersion() < 10, srcAddr)
}

func transactionValidateAndNormalizeBounceDestAddr(addr *address.Address, cfg *PreparedConfig, accountAddr *address.Address) (*address.Address, bool) {
	return transactionValidateAndNormalizeInternalAddr(addr, cfg, true, accountAddr)
}

func transactionValidateAndNormalizeInternalAddr(addr *address.Address, cfg *PreparedConfig, allowAnycast bool, rewriteBase *address.Address) (*address.Address, bool) {
	if addr == nil {
		return nil, false
	}
	if addr.Type() != address.StdAddress && addr.Type() != address.VarAddress {
		return nil, false
	}

	addrLen := addr.BitsLen()
	if addr.Workchain() == address.MasterchainID {
		if addrLen != 256 {
			return nil, false
		}
	} else {
		// When config param 12 is absent (partial emulation configs), keep the
		// legacy behavior and skip the workchain-specific checks.
		descr, found, checksEnabled := cfg.workchainDescr(addr.Workchain())
		if checksEnabled && (!found || !descr.AcceptMessages() || !descr.ValidAddressLength(addrLen)) {
			return nil, false
		}
	}

	if anycast := addr.Anycast(); anycast != nil {
		if !allowAnycast {
			return nil, false
		}
		var ok bool
		addr, ok = transactionRewriteAnycastPrefix(addr, anycast, rewriteBase)
		if !ok {
			return nil, false
		}
	}

	return transactionNormalizeInternalDestAddr(addr), true
}

func transactionRewriteAnycastPrefix(addr *address.Address, anycast *address.Anycast, base *address.Address) (*address.Address, bool) {
	depth := anycast.Depth()
	if depth == 0 || depth > 30 || base == nil {
		return nil, false
	}
	baseData := base.Data()
	prefix := anycast.Prefix()
	if len(baseData) != 32 || uint(len(prefix)*8) < depth {
		return nil, false
	}
	for i := uint(0); i < depth; i++ {
		if transactionBit(prefix, int(i)) != transactionBit(baseData, int(i)) {
			next := addr.Copy()
			next.SetAnycast(address.NewAnycast(depth, transactionAddressPrefix(baseData, depth)))
			return next, true
		}
	}
	return addr, true
}

func transactionAddressPrefix(data []byte, depth uint) []byte {
	out := append([]byte(nil), data[:(depth+7)/8]...)
	if rem := depth % 8; rem != 0 {
		out[len(out)-1] &= 0xFF << (8 - rem)
	}
	return out
}

func transactionOutboundInternalDestTypeValid(addr *address.Address) bool {
	return addr != nil && (addr.Type() == address.StdAddress || addr.Type() == address.VarAddress)
}

func transactionNormalizeInternalDestAddr(addr *address.Address) *address.Address {
	if addr == nil || addr.Type() != address.VarAddress || addr.BitsLen() != 256 {
		return addr
	}
	if addr.Workchain() < -128 || addr.Workchain() >= 128 {
		return addr
	}
	return address.NewAddress(0, byte(int8(addr.Workchain())), append([]byte(nil), addr.Data()...)).WithAnycast(addr.Anycast())
}
