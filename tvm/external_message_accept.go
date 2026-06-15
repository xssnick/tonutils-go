package tvm

import (
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type CheckExternalMessageAcceptedConfig struct {
	Now                 uint32
	BlockLT             int64
	LogicalTime         int64
	RandSeed            []byte
	ConfigRoot          *cell.Cell
	PrevBlocks          any
	UnpackedConfig      tuple.Tuple
	DuePayment          any
	PrecompiledGasUsage *big.Int
	Libraries           []*cell.Cell
	TraceHook           vm.TraceHook
}

// CheckExternalMessageAccepted runs only the transaction compute path needed to
// decide whether an inbound external message reaches accept.
func (tvm *TVM) CheckExternalMessageAccepted(shard *tlb.ShardAccount, account *tlb.AccountState, msgCell *cell.Cell, msg *tlb.ExternalMessage, cfg CheckExternalMessageAcceptedConfig) (bool, error) {
	wrapped := tlb.Message{
		MsgType: tlb.MsgTypeExternalIn,
		Msg:     msg,
	}
	if err := transactionValidateMessageStateInitLibs(&wrapped); err != nil {
		return false, err
	}

	runtimeAcc, err := loadTransactionRuntimeAccountState(shard, account, msg.DstAddr, false)
	if err != nil {
		return false, err
	}

	result, err := tvm.checkExternalMessageAccepted(runtimeAcc, msgCell, &wrapped, transactionConfigForExternalMessageAccepted(cfg))
	if err != nil {
		return false, err
	}
	return result != nil && result.Accepted, nil
}

func transactionConfigForExternalMessageAccepted(cfg CheckExternalMessageAcceptedConfig) TransactionEmulationConfig {
	return TransactionEmulationConfig{
		Now:                 cfg.Now,
		BlockLT:             cfg.BlockLT,
		LogicalTime:         cfg.LogicalTime,
		RandSeed:            cfg.RandSeed,
		ConfigRoot:          cfg.ConfigRoot,
		PrevBlocks:          cfg.PrevBlocks,
		UnpackedConfig:      cfg.UnpackedConfig,
		DuePayment:          cfg.DuePayment,
		PrecompiledGasUsage: cfg.PrecompiledGasUsage,
		Libraries:           cfg.Libraries,
		StopOnAccept:        true,
		TraceHook:           cfg.TraceHook,
	}
}

func (tvm *TVM) checkExternalMessageAccepted(runtimeAcc *transactionRuntimeAccount, msgCell *cell.Cell, msg *tlb.Message, cfg TransactionEmulationConfig) (*MessageExecutionResult, error) {
	now := cfg.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}

	blockchainCfg := tlb.BlockchainConfig{
		Root: cfg.ConfigRoot,
	}
	if err := transactionValidateInboundExternalMessage(msgCell, msg, blockchainCfg); err != nil {
		return nil, err
	}

	isSpecial := transactionIsSpecialAccount(blockchainCfg, runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial

	storageDueLimits := transactionGetStorageDueLimits(blockchainCfg, runtimeAcc.addr)
	storageFee := big.NewInt(0)
	var err error
	storageFee, err = transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		storageFee = transactionCoinsNano(runtimeAcc.storageInfo.DuePayment)
	}

	importFee := big.NewInt(0)
	if !isSpecial {
		importFee, err = transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, msg, msgCell)
		if err != nil {
			return nil, err
		}
	}

	prepared, err := transactionPrepareInitialPhases(runtimeAcc, msg, storageFee, importFee, now, blockchainCfg, storageDueLimits)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		prepared.lastPaid = 0
	}

	execCfg := cfg
	execCfg.Address = runtimeAcc.addr
	execCfg.Now = now
	startLT := transactionStartLT(runtimeAcc.storageLT, transactionExecutionLogicalTime(runtimeAcc.prevTxLT, execCfg.LogicalTime), msg)
	transactionPrepareExecutionConfig(&execCfg, runtimeAcc, msg, prepared, startLT)
	execCfg.Balance = new(big.Int).Set(prepared.balance)

	computeAcc := runtimeAcc
	var skipReason *tlb.ComputeSkipReason
	var gas vm.Gas
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else {
		gas = transactionMessageGas(execCfg, blockchainCfg, runtimeAcc.addr, prepared.balance, prepared.msgBalance.grams, msg.MsgType, isSpecial)
		if gas.Limit == 0 && gas.Credit == 0 {
			skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
		} else {
			addressSuspended := false
			if !prepared.deleted && (prepared.status == tlb.AccountStatusUninit || prepared.status == tlb.AccountStatusNonExist) && transactionMessageStateInit(msg) != nil {
				addressSuspended, err = transactionIsAddressSuspended(blockchainCfg, now, runtimeAcc.addr)
				if err != nil {
					return nil, fmt.Errorf("check suspended address: %w", err)
				}
			}

			computeAcc, _, skipReason, err = transactionPrepareComputeAccount(runtimeAcc, prepared.status, prepared.deleted, msg, addressSuspended, blockchainCfg)
			if err != nil {
				return nil, err
			}
			if skipReason == nil {
				var precompiledUsage *big.Int
				gas, precompiledUsage, skipReason, err = transactionApplyPrecompiledGasConfig(cfg, blockchainCfg, computeAcc.code, gas)
				if err != nil {
					return nil, err
				}
				if precompiledUsage != nil {
					execCfg.PrecompiledGasUsage = precompiledUsage
				}
			}
		}
	}
	if skipReason != nil {
		return nil, nil
	}

	msgRes, err := tvm.executeTransactionMessage(computeAcc, msgCell, msg, execCfg, gas, prepared.msgBalance.grams, nil)
	if err != nil {
		return nil, err
	}
	transactionNormalizeGasUsage(msgRes)
	if err = transactionApplyPrecompiledGasUsage(msgRes, execCfg.PrecompiledGasUsage); err != nil {
		return nil, err
	}
	return msgRes, nil
}
