package tvm

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// CheckExternalMessageAccepted runs only the transaction compute path needed to
// decide whether an inbound external message reaches accept.
func (tvm *TVM) CheckExternalMessageAccepted(block *BlockContext, acc *PreparedAccount, msg *PreparedMessage, opts TransactionOptions) (bool, error) {
	if block == nil {
		return false, errors.New("block context is required")
	}
	if acc == nil {
		return false, errors.New("prepared account is required")
	}
	if msg == nil {
		return false, errors.New("prepared message is required")
	}
	if msg.msg.MsgType != tlb.MsgTypeExternalIn {
		return false, errors.New("accept check requires an inbound external message")
	}

	result, err := tvm.checkExternalMessageAccepted(block, acc, msg, &opts)
	if err != nil {
		return false, err
	}
	return result != nil && result.Accepted, nil
}

func (tvm *TVM) checkExternalMessageAccepted(block *BlockContext, acc *PreparedAccount, msg *PreparedMessage, opts *TransactionOptions) (*MessageExecutionResult, error) {
	blockchainCfg := block.cfg
	now := block.now

	runtimeAcc, _, err := acc.runtimeForExecution(false)
	if err != nil {
		return nil, err
	}

	if err := transactionValidateInboundExternalMessage(msg.cell, &msg.msg, blockchainCfg); err != nil {
		return nil, err
	}

	isSpecial := blockchainCfg.isSpecialAccount(runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial

	storageDueLimits := blockchainCfg.storageDueLimitsFor(transactionIsMasterchain(runtimeAcc.addr))
	storageFee, err := transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		storageFee = transactionCoinsNano(runtimeAcc.storageInfo.DuePayment)
	}

	importFee := big.NewInt(0)
	if !isSpecial {
		importFee, err = transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, &msg.msg, msg.cell)
		if err != nil {
			return nil, err
		}
	}

	prepared, err := transactionPrepareInitialPhases(runtimeAcc, &msg.msg, storageFee, importFee, now, blockchainCfg, storageDueLimits)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		prepared.lastPaid = 0
	}

	startLT := transactionStartLT(runtimeAcc.storageLT, transactionExecutionLogicalTime(runtimeAcc.prevTxLT, opts.LogicalTime), &msg.msg)
	env := newTransactionExecEnv(block, opts, runtimeAcc, &msg.msg, msg.cell, prepared, startLT)
	env.stopOnAccept = true

	computeAcc := runtimeAcc
	var skipReason *tlb.ComputeSkipReason
	var gas vm.Gas
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else {
		gas = transactionMessageGas(opts.Gas, now, blockchainCfg, runtimeAcc.addr, prepared.balance, prepared.msgBalance.grams, msg.msg.MsgType, isSpecial)
		if gas.Limit == 0 && gas.Credit == 0 {
			skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
		} else {
			addressSuspended := false
			if !prepared.deleted && (prepared.status == tlb.AccountStatusUninit || prepared.status == tlb.AccountStatusNonExist) && transactionMessageStateInit(&msg.msg) != nil {
				addressSuspended = blockchainCfg.isAddressSuspended(now, runtimeAcc.addr)
			}

			computeAcc, _, skipReason, err = transactionPrepareComputeAccount(runtimeAcc, prepared.status, prepared.deleted, &msg.msg, addressSuspended, blockchainCfg)
			if err != nil {
				return nil, err
			}
			if skipReason == nil {
				gas, skipReason = transactionApplyPrecompiledGasConfig(blockchainCfg, computeAcc.code, gas, env)
			}
		}
	}
	if skipReason != nil {
		return nil, nil
	}

	msgRes, err := tvm.executeTransactionMessage(computeAcc, env, gas, prepared.msgBalance.grams)
	if err != nil {
		return nil, err
	}
	transactionNormalizeGasUsage(msgRes)
	if err = transactionApplyPrecompiledGasUsage(msgRes, env.precompiledGasUsage); err != nil {
		return nil, err
	}
	return msgRes, nil
}
