package tvm

import (
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

type transactionConfig struct {
	tlb.BlockchainConfig

	version          uint32
	capabilities     uint64
	hasGlobalVersion bool
	globalVersionErr error
	sizeLimits       transactionSizeLimits
	prices           *transactionConfigPrices
}

type transactionConfigPrices struct {
	gas          [2]*tlb.ConfigGasLimitsPrices
	gasLoaded    [2]bool
	msgForward   [2]*tlb.ConfigMsgForwardPrices
	msgFwdLoaded [2]bool
}

func newTransactionConfig(root *cell.Cell) transactionConfig {
	return transactionConfigFromBlockchainConfig(tlb.BlockchainConfig{Root: root})
}

func transactionConfigFromBlockchainConfig(blockchainCfg tlb.BlockchainConfig) transactionConfig {
	cfg := transactionConfig{
		BlockchainConfig: blockchainCfg,
		sizeLimits:       transactionLoadSizeLimits(blockchainCfg),
		prices:           &transactionConfigPrices{},
	}

	version, err := blockchainCfg.GetGlobalVersion()
	if err != nil {
		cfg.globalVersionErr = err
		return cfg
	}

	cfg.version = version.Version
	cfg.capabilities = version.Capabilities
	cfg.hasGlobalVersion = true
	return cfg
}

func (cfg transactionConfig) globalVersion() uint32 {
	return cfg.version
}

func (cfg transactionConfig) actionGlobalVersion() uint32 {
	if !cfg.hasGlobalVersion {
		return uint32(vmcore.DefaultGlobalVersion)
	}
	return cfg.version
}

func (cfg transactionConfig) hasCapability(capability uint64) bool {
	return cfg.hasGlobalVersion && cfg.capabilities&capability != 0
}

func (cfg transactionConfig) specialGasFull() bool {
	return cfg.hasGlobalVersion && cfg.globalVersion() >= 5
}

func transactionExecutionGlobalVersion(cfg MessageEmulationConfig, blockchainCfg transactionConfig, fallback int) (int, error) {
	version := fallback
	if cfg.GlobalVersionSet {
		version = cfg.GlobalVersion
	}
	if cfg.ConfigRoot != nil {
		if !blockchainCfg.hasGlobalVersion {
			return 0, blockchainCfg.globalVersionErr
		}
		version = int(blockchainCfg.version)
	}
	if err := validateGlobalVersion(version); err != nil {
		return 0, err
	}
	return version, nil
}

func transactionConfigMasterchainIndex(masterchain bool) int {
	if masterchain {
		return 1
	}
	return 0
}

func (cfg transactionConfig) gasPricesFor(masterchain bool) *tlb.ConfigGasLimitsPrices {
	idx := transactionConfigMasterchainIndex(masterchain)
	if cfg.prices == nil {
		return transactionLoadGasPrices(cfg.BlockchainConfig, masterchain)
	}
	if !cfg.prices.gasLoaded[idx] {
		cfg.prices.gas[idx] = transactionLoadGasPrices(cfg.BlockchainConfig, masterchain)
		cfg.prices.gasLoaded[idx] = true
	}
	return cfg.prices.gas[idx]
}

func (cfg transactionConfig) msgForwardPricesFor(masterchain bool) *tlb.ConfigMsgForwardPrices {
	idx := transactionConfigMasterchainIndex(masterchain)
	if cfg.prices == nil {
		return transactionLoadMsgForwardPrices(cfg.BlockchainConfig, masterchain)
	}
	if !cfg.prices.msgFwdLoaded[idx] {
		cfg.prices.msgForward[idx] = transactionLoadMsgForwardPrices(cfg.BlockchainConfig, masterchain)
		cfg.prices.msgFwdLoaded[idx] = true
	}
	return cfg.prices.msgForward[idx]
}

func transactionLoadGasPrices(blockchainCfg tlb.BlockchainConfig, masterchain bool) *tlb.ConfigGasLimitsPrices {
	prices, err := blockchainCfg.GetGasPrices(masterchain)
	if err != nil {
		return nil
	}
	return prices
}

func transactionLoadMsgForwardPrices(blockchainCfg tlb.BlockchainConfig, masterchain bool) *tlb.ConfigMsgForwardPrices {
	prices, err := blockchainCfg.GetMsgForwardPrices(masterchain)
	if err != nil {
		return nil
	}
	return prices
}

func transactionDefaultSizeLimits() transactionSizeLimits {
	return transactionSizeLimits{
		maxMsgBits:                  1 << 21,
		maxMsgCells:                 1 << 13,
		maxLibraryCells:             1000,
		maxExtMsgDepth:              512,
		maxAccStateCells:            1 << 16,
		maxMCAccStateCells:          1 << 11,
		maxAccPublicLibraries:       256,
		maxMsgExtraCurrencies:       2,
		maxAccFixedPrefixLength:     8,
		accStateCellsForStorageDict: 26,
	}
}

func transactionLoadSizeLimits(blockchainCfg tlb.BlockchainConfig) transactionSizeLimits {
	out := transactionDefaultSizeLimits()
	config, err := blockchainCfg.GetSizeLimitsConfig()
	if err != nil {
		return out
	}

	switch v := config.Config.(type) {
	case tlb.SizeLimitsConfigV1:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
		out.maxExtMsgDepth = v.MaxExtMsgDepth
	case tlb.SizeLimitsConfigV2:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
		out.maxExtMsgDepth = v.MaxExtMsgDepth
		out.maxAccStateCells = uint64(v.MaxAccStateCells)
		out.maxMCAccStateCells = uint64(v.MaxMCAccStateCells)
		out.maxAccPublicLibraries = uint64(v.MaxAccPublicLibraries)
		out.maxMsgExtraCurrencies = uint64(v.MaxMsgExtraCurrencies)
		out.maxAccFixedPrefixLength = uint64(v.MaxAccFixedPrefixLength)
		out.accStateCellsForStorageDict = uint64(v.AccStateCellsForStorageDict)
	}
	return out
}
