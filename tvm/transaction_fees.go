package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func transactionMessageGas(gasOverride vm.Gas, now uint32, blockchainCfg *PreparedBlockchainConfig, addr *address.Address, balance, msgBalance *big.Int, msgType tlb.MsgType, isSpecial bool) vm.Gas {
	if transactionGasConfigured(gasOverride) {
		return gasOverride
	}

	// Only an absent gas prices param (possible on lenient test configs) falls
	// back to the default profiles; a present param is honored literally, so a
	// configured zero gas limit computes to zero limits and skips with NO_GAS,
	// matching the reference compute_gas_limits.
	prices := blockchainCfg.gasPricesFor(transactionIsMasterchain(addr))
	if prices != nil {
		if isSpecial {
			limit := transactionGasInt(prices.SpecialGasLimit)
			credit := int64(0)
			if msgType == tlb.MsgTypeExternalIn && prices.GasCredit > 0 {
				credit = min(transactionGasInt(prices.GasCredit), limit)
			}
			if !blockchainCfg.specialGasFull() {
				msgLimit := transactionGasInt(min(transactionGasBoughtFor(prices, msgBalance), prices.SpecialGasLimit))
				return vm.Gas{
					Max:       limit,
					Limit:     msgLimit,
					Credit:    credit,
					Base:      msgLimit + credit,
					Remaining: msgLimit + credit,
				}
			}
			return vm.Gas{
				Max:       limit,
				Limit:     limit,
				Credit:    credit,
				Base:      limit + credit,
				Remaining: limit + credit,
			}
		}

		gasMax := transactionGasBoughtForAccount(blockchainCfg, prices, balance, addr, now)
		gasLimit := uint64(0)
		gasCredit := uint64(0)
		if msgType == tlb.MsgTypeExternalIn {
			gasCredit = min(prices.GasCredit, gasMax)
		} else {
			gasLimit = min(transactionGasBoughtForAccount(blockchainCfg, prices, msgBalance, addr, now), gasMax)
		}
		return transactionGasFromLimits(gasMax, gasLimit, gasCredit)
	}

	if msgType == tlb.MsgTypeInternal {
		if msgBalance == nil || !msgBalance.IsUint64() {
			return defaultInternalMessageGas(vm.Gas{}, 0)
		}
		return defaultInternalMessageGas(vm.Gas{}, msgBalance.Uint64())
	}
	return defaultExternalMessageGas(vm.Gas{})
}

func transactionTickTockGas(gasOverride vm.Gas, now uint32, blockchainCfg *PreparedBlockchainConfig, addr *address.Address, balance *big.Int, isSpecial bool) vm.Gas {
	if transactionGasConfigured(gasOverride) {
		return gasOverride
	}

	prices := blockchainCfg.gasPricesFor(transactionIsMasterchain(addr))
	if prices != nil && prices.GasLimit > 0 {
		if isSpecial && prices.SpecialGasLimit > 0 {
			limit := prices.SpecialGasLimit
			return transactionGasFromLimits(limit, limit, 0)
		}
		limit := transactionGasBoughtForAccount(blockchainCfg, prices, balance, addr, now)
		return transactionGasFromLimits(limit, limit, 0)
	}
	return defaultTickTockTransactionGas(vm.Gas{})
}

func transactionGasFromLimits(max, limit, credit uint64) vm.Gas {
	maxGas := transactionGasInt(max)
	limitGas := transactionGasInt(limit)
	creditGas := transactionGasInt(credit)
	return vm.Gas{
		Max:       maxGas,
		Limit:     limitGas,
		Credit:    creditGas,
		Base:      limitGas + creditGas,
		Remaining: limitGas + creditGas,
	}
}

func transactionGasInt(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func transactionGasBoughtFor(prices *tlb.ConfigGasLimitsPrices, nanograms *big.Int) uint64 {
	if prices == nil {
		return 0
	}
	return transactionGasBoughtForLimit(prices, nanograms, prices.GasLimit)
}

func transactionGasBoughtForAccount(cfg *PreparedBlockchainConfig, prices *tlb.ConfigGasLimitsPrices, nanograms *big.Int, addr *address.Address, now uint32) uint64 {
	if prices == nil {
		return 0
	}
	if limit, ok := transactionGasLimitOverride(cfg, addr, now); ok {
		return transactionGasBoughtForLimit(prices, nanograms, limit)
	}
	return transactionGasBoughtForLimit(prices, nanograms, prices.GasLimit)
}

func transactionGasBoughtForLimit(prices *tlb.ConfigGasLimitsPrices, nanograms *big.Int, gasLimit uint64) uint64 {
	if prices == nil || nanograms == nil || nanograms.Sign() < 0 {
		return 0
	}

	threshold := transactionMaxGasThresholdForLimit(prices, gasLimit)
	if nanograms.Cmp(threshold) >= 0 {
		return gasLimit
	}

	flatGasPrice := new(big.Int).SetUint64(prices.FlatGasPrice)
	if nanograms.Cmp(flatGasPrice) < 0 {
		return 0
	}
	if prices.GasPrice == 0 {
		return gasLimit
	}

	remaining := new(big.Int).Sub(nanograms, flatGasPrice)
	remaining.Lsh(remaining, 16)
	remaining.Div(remaining, new(big.Int).SetUint64(prices.GasPrice))
	remaining.Add(remaining, new(big.Int).SetUint64(prices.FlatGasLimit))
	if !remaining.IsUint64() || remaining.Uint64() > gasLimit {
		return gasLimit
	}
	return remaining.Uint64()
}

func transactionMaxGasThresholdForLimit(prices *tlb.ConfigGasLimitsPrices, gasLimit uint64) *big.Int {
	if prices == nil || gasLimit <= prices.FlatGasLimit {
		return new(big.Int).SetUint64(transactionGasFlatPrice(prices))
	}

	total := new(big.Int).SetUint64(prices.GasPrice)
	total.Mul(total, new(big.Int).SetUint64(gasLimit-prices.FlatGasLimit))
	total = transactionCeilShiftRight(total, 16)
	return total.Add(total, new(big.Int).SetUint64(prices.FlatGasPrice))
}

type transactionGasLimitOverrideEntry struct {
	addr        *address.Address
	limit       uint64
	fromVersion uint32
	until       uint32
}

var transactionGasLimitOverrides = []transactionGasLimitOverrideEntry{
	{addr: address.MustParseRawAddr("0:FFBFD8F5AE5B2E1C7C3614885CB02145483DFAEE575F0DD08A72C366369211CD"), limit: 70_000_000, fromVersion: 5, until: 1_709_164_800},
	{addr: address.MustParseRawAddr("0:5E4A5F9DBA638789E6770C990D2959237ACA3BC19D15A734782C26CB19343CC6"), limit: 70_000_000, fromVersion: 9, until: 1_740_787_200},
	{addr: address.MustParseRawAddr("0:B755C43EE37925C30F547E2991E7C4C18C1CE4EC63EEA5743708DBAD868369FA"), limit: 70_000_000, fromVersion: 9, until: 1_740_787_200},
	{addr: address.MustParseRawAddr("0:61C016FC8EFA241AF7EB787451A1E571236DFB3EB389832AEC0212C0FB8AC10B"), limit: 70_000_000, fromVersion: 9, until: 1_740_787_200},
	{addr: address.MustParseRawAddr("0:A4A11A78384F92154A0C12761F2F7BC5E374F703335F5BC8F24C2E32CE4F1C26"), limit: 70_000_000, fromVersion: 9, until: 1_740_787_200},
	{addr: address.MustParseRawAddr("0:4DE480AB6ACEFD53C158126EF5C2CDF89FE64D210D0B44DA5C90E52C215DCE79"), limit: 70_000_000, fromVersion: 9, until: 1_740_787_200},
	{addr: address.MustParseRawAddr("0:436A76C2794A88E3FBFEC6B9C0374FC8DB046F10868B835420D9937973A665D4"), limit: 225_000_000, fromVersion: 9, until: 1_740_787_200},
}

func transactionGasLimitOverride(cfg *PreparedBlockchainConfig, addr *address.Address, now uint32) (uint64, bool) {
	if addr == nil || addr.Type() != address.StdAddress {
		return 0, false
	}
	addrData := addr.Data()
	if len(addrData) != 32 {
		return 0, false
	}

	version := cfg.globalVersion()
	workchain := addr.Workchain()
	for _, override := range transactionGasLimitOverrides {
		if version >= override.fromVersion && now < override.until && workchain == override.addr.Workchain() && bytes.Equal(addrData, override.addr.Data()) {
			return override.limit, true
		}
	}
	return 0, false
}

func transactionGasFlatPrice(prices *tlb.ConfigGasLimitsPrices) uint64 {
	if prices == nil {
		return 0
	}
	return prices.FlatGasPrice
}

func transactionCeilShiftRight(x *big.Int, bits uint) *big.Int {
	if x == nil || x.Sign() == 0 {
		return new(big.Int)
	}

	out := new(big.Int).Rsh(x, bits)
	if transactionLowBitsNonZero(x, bits) {
		out.Add(out, big.NewInt(1))
	}
	return out
}

func transactionLowBitsNonZero(x *big.Int, bits uint) bool {
	for i := uint(0); i < bits; i++ {
		if x.Bit(int(i)) != 0 {
			return true
		}
	}
	return false
}

func transactionGasConfigured(g vm.Gas) bool {
	return g.Max != 0 || g.Limit != 0 || g.Credit != 0
}

func transactionPrecompiledGasUsage(value *big.Int) (int64, bool, error) {
	if value == nil {
		return 0, false, nil
	}
	if value.Sign() < 0 {
		return 0, false, fmt.Errorf("precompiled gas usage is negative")
	}
	if !value.IsInt64() {
		return 0, false, fmt.Errorf("precompiled gas usage exceeds int64")
	}
	return value.Int64(), true, nil
}

// transactionApplyPrecompiledGasConfig resolves the precompiled-contract gas
// usage for code from the prepared config (param 45) and adjusts the compute
// gas accordingly. The resolved usage is stored in env for the c7 tuple and
// the final gas accounting.
func transactionApplyPrecompiledGasConfig(blockchainCfg *PreparedBlockchainConfig, code *cell.Cell, addr *address.Address, isSpecial bool, gas vm.Gas, env *transactionExecEnv) (vm.Gas, *tlb.ComputeSkipReason) {
	usage := blockchainCfg.precompiledGasUsage(code)
	if usage == nil {
		return gas, nil
	}
	env.precompiledGasUsage = usage

	precompiledGas, ok, err := transactionPrecompiledGasUsage(usage)
	if err != nil || !ok {
		return gas, nil
	}
	if precompiledGas > gas.Limit {
		return gas, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	}
	return transactionPrecompiledFallbackGas(gas, transactionPrecompiledFallbackLimit(blockchainCfg, addr, isSpecial)), nil
}

func transactionPrecompiledFallbackLimit(blockchainCfg *PreparedBlockchainConfig, addr *address.Address, isSpecial bool) uint64 {
	prices := blockchainCfg.gasPricesFor(transactionIsMasterchain(addr))
	if prices == nil {
		return 0
	}
	if isSpecial {
		return prices.SpecialGasLimit
	}
	return prices.GasLimit
}

func transactionPrecompiledFallbackGas(gas vm.Gas, rawLimit uint64) vm.Gas {
	limit := transactionGasInt(rawLimit)
	credit := int64(0)
	if gas.Credit != 0 {
		credit = limit
	}
	return vm.Gas{
		Max:       limit,
		Limit:     limit,
		Credit:    credit,
		Base:      limit + credit,
		Remaining: limit + credit,
	}
}

func transactionComputeForwardFeeForMessage(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell) (*big.Int, error) {
	usage, err := transactionMessageTailUsage(msgCell)
	if err != nil {
		return nil, err
	}
	return transactionComputeForwardFeeForUsage(cfg, srcAddr, dstAddr, usage), nil
}

func transactionComputeForwardFeeForUsage(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, usage transactionUsage) *big.Int {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil {
		return big.NewInt(0)
	}
	return prices.ComputeForwardFee(usage.cells, usage.bits)
}

func transactionComputeIHRFee(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int, ihrDisabled bool) *big.Int {
	if ihrDisabled || fwdFee == nil || fwdFee.Sign() == 0 {
		return big.NewInt(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.IHRFactor == 0 {
		return big.NewInt(0)
	}

	fee := new(big.Int).SetUint64(uint64(prices.IHRFactor))
	fee.Mul(fee, fwdFee)
	return fee.Rsh(fee, 16)
}

func transactionFirstPartForwardFee(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int) *big.Int {
	if fwdFee == nil || fwdFee.Sign() == 0 {
		return big.NewInt(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.FirstFrac == 0 {
		return big.NewInt(0)
	}

	fee := new(big.Int).SetUint64(uint64(prices.FirstFrac))
	fee.Mul(fee, fwdFee)
	return fee.Rsh(fee, 16)
}

func transactionGetMsgForwardPrices(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address) *tlb.ConfigMsgForwardPrices {
	return cfg.msgForwardPricesFor(transactionIsMasterchain(srcAddr) || transactionIsMasterchain(dstAddr))
}

func transactionOutboundInternalMessageFeeUsage(cfg *PreparedBlockchainConfig, msg *tlb.InternalMessage, layout transactionOutboundLayout) (transactionUsage, error) {
	collector := newTransactionUsageCollector()
	usage := transactionUsage{}

	if msg.StateInit != nil {
		stateCell, err := tlb.ToCell(msg.StateInit)
		if err != nil {
			return transactionUsage{}, err
		}
		stateUsage, err := collector.addCell(stateCell, !layout.stateInitInRef)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, stateUsage)
	}
	if msg.Body != nil {
		bodyUsage, err := collector.addCell(msg.Body, !layout.bodyInRef)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, bodyUsage)
	}
	if cfg.globalVersion() < 10 && msg.ExtraCurrencies != nil {
		extraUsage, err := collector.addCell(msg.ExtraCurrencies.AsCell(), false)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, extraUsage)
	}

	return usage, nil
}

func transactionOutboundExternalMessageFeeUsage(msg *tlb.ExternalMessageOut, layout transactionOutboundLayout) (transactionUsage, error) {
	collector := newTransactionUsageCollector()
	usage := transactionUsage{}

	if msg.StateInit != nil {
		stateCell, err := tlb.ToCell(msg.StateInit)
		if err != nil {
			return transactionUsage{}, err
		}
		stateUsage, err := collector.addCell(stateCell, !layout.stateInitInRef)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, stateUsage)
	}
	if msg.Body != nil {
		bodyUsage, err := collector.addCell(msg.Body, !layout.bodyInRef)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, bodyUsage)
	}

	return usage, nil
}

// transactionOutboundMessageFailedLayout mirrors the reference redo rules for a
// message that failed to pack: StateInit moves into a ref only when it has at
// least two refs, the body only when its inline form is non-empty.
func transactionOutboundMessageFailedLayout(state *tlb.StateInit, body *cell.Cell, layout transactionOutboundLayout) (transactionOutboundLayout, error) {
	next := layout
	if !next.stateInitInRef {
		move, err := transactionStateInitRefRetryNeeded(state)
		if err != nil {
			return layout, err
		}
		if move {
			next.stateInitInRef = true
		}
	}
	if !next.bodyInRef && transactionMessageBodyRefRetryNeeded(body) {
		next.bodyInRef = true
	}
	return next, nil
}

func transactionOutboundInternalMessageActionUsage(cfg *PreparedBlockchainConfig, msg *tlb.InternalMessage, msgCell *cell.Cell, layout transactionOutboundLayout) (transactionUsage, error) {
	root, err := transactionLoadedCell(msgCell)
	if err != nil {
		return transactionUsage{}, err
	}
	if root == nil {
		return transactionUsage{}, nil
	}

	tailUsage, err := transactionOutboundInternalMessageFeeUsage(cfg, msg, layout)
	if err != nil {
		return transactionUsage{}, err
	}
	return transactionAddUsage(transactionUsage{
		cells: 1,
		bits:  uint64(root.BitsSize()),
	}, tailUsage), nil
}

func transactionMessageTailUsage(root *cell.Cell) (transactionUsage, error) {
	usage, err := transactionCollectUsage(root)
	if err != nil {
		return transactionUsage{}, err
	}
	if root == nil || usage.cells == 0 {
		return transactionUsage{}, nil
	}

	loaded, err := transactionLoadedCell(root)
	if err != nil {
		return transactionUsage{}, err
	}
	usage.cells--
	rootBits := uint64(loaded.BitsSize())
	if usage.bits >= rootBits {
		usage.bits -= rootBits
	} else {
		usage.bits = 0
	}
	return usage, nil
}

type transactionMessageStatsResult struct {
	usage       transactionUsage
	totalUsage  transactionUsage
	merkleDepth uint16
}

func transactionMessageStats(root *cell.Cell) (transactionMessageStatsResult, error) {
	var stats transactionMessageStatsResult
	if root == nil {
		return stats, nil
	}

	seenUsage := make(map[cell.Hash]struct{})
	var rootBits uint64
	rootSeen := false
	var walk func(c *cell.Cell, depth uint16, isRoot bool) error
	walk = func(c *cell.Cell, depth uint16, isRoot bool) error {
		if c == nil {
			return nil
		}

		var sl cell.Slice
		if err := c.BeginParseIntoWithoutTrace(&sl); err != nil {
			return err
		}

		loaded := sl.BaseCell()
		if isRoot {
			rootBits = uint64(loaded.BitsSize())
		}
		switch loaded.GetType() {
		case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
			depth++
			if depth > stats.merkleDepth {
				stats.merkleDepth = depth
			}
		}

		key := loaded.HashKey()
		if _, ok := seenUsage[key]; !ok {
			seenUsage[key] = struct{}{}
			stats.totalUsage.cells++
			stats.totalUsage.bits += uint64(loaded.BitsSize())
			if isRoot {
				rootSeen = true
			}
		}

		for sl.RefsNum() > 0 {
			ref, err := sl.LoadRefCell()
			if err != nil {
				return err
			}

			if err := walk(ref, depth, false); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, 0, true); err != nil {
		return transactionMessageStatsResult{}, err
	}

	stats.usage = stats.totalUsage
	if rootSeen && stats.usage.cells > 0 {
		stats.usage.cells--
		if stats.usage.bits >= rootBits {
			stats.usage.bits -= rootBits
		} else {
			stats.usage.bits = 0
		}
	}
	return stats, nil
}

func transactionComputeGasFee(cfg *PreparedBlockchainConfig, addr *address.Address, gasUsed uint64) *big.Int {
	prices := cfg.gasPricesFor(transactionIsMasterchain(addr))
	if prices == nil {
		return big.NewInt(0)
	}

	return prices.ComputeGasPrice(gasUsed)
}

func transactionComputeImportFee(cfg *PreparedBlockchainConfig, addr *address.Address, msg *tlb.Message, msgCell *cell.Cell) (*big.Int, error) {
	if msg.MsgType != tlb.MsgTypeExternalIn {
		return big.NewInt(0), nil
	}

	prices := cfg.msgForwardPricesFor(transactionIsMasterchain(addr))
	if prices == nil {
		return big.NewInt(0), nil
	}

	usage, err := transactionMessageTailUsage(msgCell)
	if err != nil {
		return nil, err
	}

	return prices.ComputeForwardFee(usage.cells, usage.bits), nil
}

func transactionComputeStorageFee(cfg *PreparedBlockchainConfig, acc *transactionRuntimeAccount, now uint32) (*big.Int, error) {
	if now < acc.storageInfo.LastPaid {
		return nil, fmt.Errorf("transaction unix time %d is before account last_paid %d", now, acc.storageInfo.LastPaid)
	}

	total := big.NewInt(0)
	if acc.storageInfo.DuePayment != nil && acc.storageInfo.DuePayment.Nano().Sign() > 0 {
		total.Add(total, acc.storageInfo.DuePayment.Nano())
	}

	usage := acc.storageInfo.StorageUsed
	if usage.CellsUsed == nil || usage.BitsUsed == nil || now <= acc.storageInfo.LastPaid || acc.storageInfo.LastPaid == 0 {
		return total, nil
	}

	fee := cfg.computeStorageFee(transactionIsMasterchain(acc.addr), acc.storageInfo.LastPaid, now, usage.BitsUsed.Uint64(), usage.CellsUsed.Uint64())
	total.Add(total, fee)
	return total, nil
}

func transactionGetSizeLimits(cfg *PreparedBlockchainConfig) transactionSizeLimits {
	return cfg.sizeLimits
}

func transactionCheckOutboundMessageSize(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell, available *big.Int, isSpecial, actionFineEnabled bool) (int32, *big.Int, error) {
	stats, err := transactionMessageStats(msgCell)
	if err != nil {
		return 0, nil, err
	}
	code, fine := transactionCheckOutboundMessageStatsSize(cfg, srcAddr, dstAddr, stats, available, isSpecial, actionFineEnabled)
	return code, fine, nil
}

func transactionCheckOutboundMessageStatsSize(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, stats transactionMessageStatsResult, available *big.Int, isSpecial, actionFineEnabled bool) (int32, *big.Int) {
	limits := transactionGetSizeLimits(cfg)
	if isSpecial || !actionFineEnabled {
		if stats.usage.bits <= limits.maxMsgBits && stats.usage.cells <= limits.maxMsgCells && stats.merkleDepth <= 2 {
			return 0, big.NewInt(0)
		}
		return 40, big.NewInt(0)
	}

	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	fine := transactionComputeActionFineForUsageWithPrices(prices, stats.usage, available)
	maxCells, limitedByFunds := transactionActionFineCellLimitWithPrices(prices, limits.maxMsgCells, available)
	if stats.usage.bits <= limits.maxMsgBits && stats.usage.cells <= maxCells && stats.merkleDepth <= 2 {
		return 0, big.NewInt(0)
	}
	if limitedByFunds && stats.usage.cells > maxCells {
		return 40, fine
	}
	return 40, fine
}

func transactionValidateInboundExternalMessage(msgCell *cell.Cell, msg *tlb.Message, cfg *PreparedBlockchainConfig) error {
	if msg == nil || msg.MsgType != tlb.MsgTypeExternalIn {
		return nil
	}

	dst := msg.AsExternalIn().DstAddr
	if cfg.globalVersion() >= 10 && (dst == nil || dst.Type() != address.StdAddress || dst.Anycast() != nil) {
		return errors.New("invalid inbound external message destination")
	}

	limits := transactionGetSizeLimits(cfg)
	if msgCell != nil && msgCell.Depth() > limits.maxExtMsgDepth {
		return errors.New("inbound external message depth exceeds limit")
	}

	stats, err := transactionMessageStats(msgCell)
	if err != nil {
		return err
	}
	if stats.usage.bits > limits.maxMsgBits || stats.usage.cells > limits.maxMsgCells {
		return errors.New("inbound external message size exceeds limit")
	}

	if stats.merkleDepth > 2 {
		return errors.New("inbound external message merkle depth exceeds limit")
	}
	return nil
}

func transactionComputeActionFine(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell, available *big.Int) (*big.Int, error) {
	usage, err := transactionMessageTailUsage(msgCell)
	if err != nil {
		return nil, err
	}
	return transactionComputeActionFineForUsage(cfg, srcAddr, dstAddr, usage, available), nil
}

func transactionComputeActionFineForUsage(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, usage transactionUsage, available *big.Int) *big.Int {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	return transactionComputeActionFineForUsageWithPrices(prices, usage, available)
}

func transactionComputeActionFineForUsageWithPrices(prices *tlb.ConfigMsgForwardPrices, usage transactionUsage, available *big.Int) *big.Int {
	if prices == nil {
		return big.NewInt(0)
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return big.NewInt(0)
	}

	fineCells := usage.cells
	maxCells, _ := transactionActionFineCellLimitWithPrices(prices, usage.cells, available)
	if fineCells > maxCells {
		fineCells = maxCells
	}

	fine := new(big.Int).SetUint64(finePerCell)
	return fine.Mul(fine, new(big.Int).SetUint64(fineCells))
}

func transactionActionFineCellLimitWithPrices(prices *tlb.ConfigMsgForwardPrices, maxCells uint64, available *big.Int) (uint64, bool) {
	if prices == nil || available == nil {
		return maxCells, false
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return maxCells, false
	}

	finePerCellInt := new(big.Int).SetUint64(finePerCell)
	maxFine := new(big.Int).SetUint64(maxCells)
	maxFine.Mul(maxFine, finePerCellInt)
	if available.Cmp(maxFine) >= 0 {
		return maxCells, false
	}
	cells := new(big.Int).Div(available, finePerCellInt)
	if !cells.IsUint64() {
		return 0, true
	}
	return cells.Uint64(), true
}

func transactionSendActionFineFunds(remaining, msgBalance, messageValue, gasFees, currentActionFine *big.Int, mode uint8) (*big.Int, bool) {
	funds := transactionBigOrZero(remaining)
	if mode&0x80 != 0 || mode&1 != 0 {
		return funds, true
	}

	valueFunds := transactionBigOrZero(messageValue)
	if mode&0x40 != 0 {
		if msgBalance != nil {
			valueFunds.Add(valueFunds, msgBalance)
		}
		if gasFees != nil {
			valueFunds.Sub(valueFunds, gasFees)
		}
		if currentActionFine != nil {
			valueFunds.Sub(valueFunds, currentActionFine)
		}
		if valueFunds.Sign() < 0 {
			return nil, false
		}
	}
	if valueFunds.Cmp(funds) < 0 {
		funds = valueFunds
	}
	return funds, true
}

func transactionComputeSendActionFineForUsage(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, usage transactionUsage, remaining, msgBalance, messageValue, gasFees, currentActionFine *big.Int, mode uint8) (*big.Int, uint64, bool) {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil {
		return big.NewInt(0), transactionGetSizeLimits(cfg).maxMsgCells, false
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return big.NewInt(0), transactionGetSizeLimits(cfg).maxMsgCells, false
	}

	limits := transactionGetSizeLimits(cfg)
	maxCells := limits.maxMsgCells
	limitedByFunds := false
	funds, ok := transactionSendActionFineFunds(remaining, msgBalance, messageValue, gasFees, currentActionFine, mode)
	if !ok {
		funds = big.NewInt(0)
	}

	finePerCellInt := new(big.Int).SetUint64(finePerCell)
	maxFine := new(big.Int).SetUint64(maxCells)
	maxFine.Mul(maxFine, finePerCellInt)
	if funds.Cmp(maxFine) < 0 {
		cells := new(big.Int).Div(funds, finePerCellInt)
		if cells.IsUint64() {
			maxCells = cells.Uint64()
		} else {
			maxCells = 0
		}
		limitedByFunds = true
	}

	fineCells := usage.cells
	if fineCells > maxCells {
		fineCells = maxCells
	}
	fine := new(big.Int).Set(finePerCellInt)
	fine.Mul(fine, new(big.Int).SetUint64(fineCells))
	if remaining != nil && fine.Cmp(remaining) > 0 {
		fine.Set(remaining)
	}
	return fine, maxCells, limitedByFunds
}

func transactionAccountStateExceedsLimits(acc *transactionRuntimeAccount, code, data *cell.Cell, libs *cell.Dictionary, cfg *PreparedBlockchainConfig, exemptSpecial bool) (bool, error) {
	if exemptSpecial && acc.isSpecial {
		return false, nil
	}
	if transactionCellEqual(acc.code, code) && transactionCellEqual(acc.data, data) && transactionDictEqual(acc.libraries, libs) {
		return false, nil
	}

	limits := transactionGetSizeLimits(cfg)
	maxCells := limits.maxAccStateCells
	if transactionIsMasterchain(acc.addr) && cfg.globalVersion() >= 12 {
		maxCells = limits.maxMCAccStateCells
	}

	var libCell *cell.Cell
	if libs != nil && !libs.IsEmpty() {
		libCell = libs.AsCell()
	}

	stats, err := transactionCellStatsForRoots(code, data, libCell)
	if err != nil {
		return false, err
	}
	if stats.usage.cells > maxCells {
		return true, nil
	}
	if stats.merkleDepth > 2 {
		return true, nil
	}

	if transactionIsMasterchain(acc.addr) && !transactionDictEqual(acc.libraries, libs) && transactionPublicLibrariesCount(libs) > limits.maxAccPublicLibraries {
		return true, nil
	}

	return false, nil
}

func transactionCellEqual(a, b *cell.Cell) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.HashKey() == b.HashKey()
}

func transactionDictEqual(a, b *cell.Dictionary) bool {
	if a == nil || a.IsEmpty() {
		return b == nil || b.IsEmpty()
	}
	if b == nil || b.IsEmpty() {
		return false
	}
	return a.AsCell().HashKey() == b.AsCell().HashKey()
}

func transactionCollectUniqueUsage(roots ...*cell.Cell) (transactionUsage, error) {
	stats, err := transactionCellStatsForRoots(roots...)
	if err != nil {
		return transactionUsage{}, err
	}
	return stats.usage, nil
}

func transactionPublicLibrariesCount(libs *cell.Dictionary) uint64 {
	if libs == nil || libs.IsEmpty() {
		return 0
	}
	items, err := libs.LoadAll(true)
	if err != nil {
		return 0
	}
	var count uint64
	for _, item := range items {
		isPublic, err := item.Value.LoadBoolBit()
		if err == nil && isPublic {
			count++
		}
	}
	return count
}

func transactionMaxMerkleDepth(root *cell.Cell) (uint16, error) {
	return transactionMaxMerkleDepthForRoots(root)
}

func transactionMaxMerkleDepthForRoots(roots ...*cell.Cell) (uint16, error) {
	stats, err := transactionCellStatsForRoots(roots...)
	if err != nil {
		return 0, err
	}
	return stats.merkleDepth, nil
}

type transactionCellStatsResult struct {
	usage       transactionUsage
	merkleDepth uint16
}

func transactionCellStatsForRoots(roots ...*cell.Cell) (transactionCellStatsResult, error) {
	var stats transactionCellStatsResult
	// seen memoizes each cell's subtree max merkle depth (merkle cells on the
	// deepest path within the subtree, including the cell itself). The value
	// is path-independent, so a cell reached again via another path just
	// contributes its memoized depth without re-walking the shared subtree —
	// the same technique transactionAccountStorageStat.addCell uses. Usage is
	// counted on first visit only, exactly as before.
	seen := make(map[cell.Hash]uint16, 64)

	var walk func(c *cell.Cell) (uint16, error)
	walk = func(c *cell.Cell) (uint16, error) {
		if c == nil {
			return 0, nil
		}

		var sl cell.Slice
		if err := c.BeginParseIntoWithoutTrace(&sl); err != nil {
			return 0, err
		}

		loaded := sl.BaseCell()
		key := loaded.HashKey()
		if depth, ok := seen[key]; ok {
			return depth, nil
		}

		stats.usage.cells++
		stats.usage.bits += uint64(loaded.BitsSize())

		var depth uint16
		for sl.RefsNum() > 0 {
			ref, err := sl.LoadRefCell()
			if err != nil {
				return 0, err
			}

			refDepth, err := walk(ref)
			if err != nil {
				return 0, err
			}
			if refDepth > depth {
				depth = refDepth
			}
		}

		switch loaded.GetType() {
		case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
			depth++
		}

		seen[key] = depth
		return depth, nil
	}
	for _, root := range roots {
		depth, err := walk(root)
		if err != nil {
			return transactionCellStatsResult{}, err
		}
		if depth > stats.merkleDepth {
			stats.merkleDepth = depth
		}
	}
	return stats, nil
}

func transactionIsMasterchain(addr *address.Address) bool {
	return addr != nil && addr.Type() == address.StdAddress && addr.Workchain() == -1
}
