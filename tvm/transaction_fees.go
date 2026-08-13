package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	mathbits "math/bits"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/internal/bigint"
	"github.com/xssnick/tonutils-go/internal/fee"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func transactionMessageGas(gasOverride vm.Gas, now uint32, blockchainCfg *PreparedBlockchainConfig, addr *address.Address, balance, msgBalance *big.Int, msgType tlb.MsgType, isSpecial bool) vm.Gas {
	if transactionGasConfigured(gasOverride) {
		return gasOverride
	}

	// Only an absent gas prices param (possible on lenient test configs) falls
	// back to the default profiles; a present param is honored literally, so a
	// configured zero gas limit computes to zero limits and skips with NO_GAS
	// instead of using a default profile.
	prices := blockchainCfg.gasPricesFor(transactionIsMasterchain(addr))
	if prices != nil {
		if isSpecial {
			gasLimit := prices.SpecialGasLimit
			gasCredit := uint64(0)
			if msgType == tlb.MsgTypeExternalIn && prices.GasCredit > 0 {
				gasCredit = min(prices.GasCredit, gasLimit)
			}
			if !blockchainCfg.specialGasFull() {
				gasLimit = min(transactionGasBoughtFor(prices, msgBalance), gasLimit)
			}
			return transactionGasFromLimits(prices.SpecialGasLimit, gasLimit, gasCredit)
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
	if prices != nil {
		if isSpecial {
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

	// Every balance the chain can hold is a fraction of the total supply and
	// stays far below 2^64. A wider one can only be handed to the emulator
	// directly, and it is compared against a 128-bit threshold, so it keeps the
	// arbitrary-precision path.
	if !nanograms.IsUint64() {
		return transactionGasBoughtForLimitBig(prices, nanograms, gasLimit)
	}

	balance := nanograms.Uint64()
	if transactionMaxGasThresholdForLimit(prices, gasLimit).Cmp64(balance) <= 0 {
		return gasLimit
	}

	if balance < prices.FlatGasPrice {
		return 0
	}
	if prices.GasPrice == 0 {
		return gasLimit
	}

	// A quotient wider than uint64, or a sum that wraps, is what the big.Int
	// path rejects with !IsUint64 — both cap at the gas limit.
	bought, fits := fee.Shl64(balance-prices.FlatGasPrice, 16).DivU64(prices.GasPrice)
	if !fits {
		return gasLimit
	}

	bought, carry := mathbits.Add64(bought, prices.FlatGasLimit, 0)
	if carry != 0 || bought > gasLimit {
		return gasLimit
	}
	return bought
}

func transactionGasBoughtForLimitBig(prices *tlb.ConfigGasLimitsPrices, nanograms *big.Int, gasLimit uint64) uint64 {
	if nanograms.Cmp(transactionMaxGasThresholdForLimit(prices, gasLimit).Big()) >= 0 {
		return gasLimit
	}

	flatGasPrice := bigint.FromUint64(prices.FlatGasPrice)
	if nanograms.Cmp(flatGasPrice) < 0 {
		return 0
	}
	if prices.GasPrice == 0 {
		return gasLimit
	}

	remaining := new(big.Int).Sub(nanograms, flatGasPrice)
	remaining.Lsh(remaining, 16)
	remaining.Div(remaining, bigint.FromUint64(prices.GasPrice))
	remaining.Add(remaining, bigint.FromUint64(prices.FlatGasLimit))
	if !remaining.IsUint64() || remaining.Uint64() > gasLimit {
		return gasLimit
	}
	return remaining.Uint64()
}

func transactionMaxGasThresholdForLimit(prices *tlb.ConfigGasLimitsPrices, gasLimit uint64) fee.U128 {
	if prices == nil || gasLimit <= prices.FlatGasLimit {
		return fee.U128{Lo: transactionGasFlatPrice(prices)}
	}

	return fee.Mul64(prices.GasPrice, gasLimit-prices.FlatGasLimit).CeilShr(16).Add64(prices.FlatGasPrice)
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
		out.Add(out, bigint.FromInt64(1))
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
	return transactionComputeForwardFeeWithPrices(prices, usage.cells, usage.bits)
}

// The transaction engine stores forward fees in a uint64. Keep the public
// TLB price calculation arbitrary-precision for TVM opcodes, and truncate only
// at the transaction boundary to match the reference implementation.
func transactionComputeForwardFeeWithPrices(prices *tlb.ConfigMsgForwardPrices, cells, bits uint64) *big.Int {
	if prices == nil {
		return bigint.FromInt64(0)
	}

	fee := prices.ComputeForwardFee(cells, bits)
	return fee.SetUint64(fee.Uint64())
}

// The reference compares a suggested big-int fee with the uint64 computed fee
// after casting the latter to signed long long. Consequently, a computed value
// above MaxInt64 never replaces the suggestion. A nil suggestion represents
// the forced zero used for external messages and since global version 8.
func transactionSelectComputedMessageFee(computed, suggested *big.Int) *big.Int {
	if computed.Uint64() <= math.MaxInt64 {
		if suggested == nil || (suggested.Sign() >= 0 && suggested.BitLen() <= 63 && suggested.Cmp(computed) < 0) {
			return computed
		}
	}
	if suggested != nil {
		return suggested
	}
	return bigint.FromInt64(0)
}

func transactionComputeIHRFee(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int, ihrDisabled bool) *big.Int {
	if ihrDisabled || fwdFee == nil || fwdFee.Sign() == 0 {
		return bigint.FromInt64(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.IHRFactor == 0 {
		return bigint.FromInt64(0)
	}

	fee := bigint.FromUint64(uint64(prices.IHRFactor))
	fee.Mul(fee, fwdFee)
	fee.Rsh(fee, 16)
	return fee.SetUint64(fee.Uint64())
}

func transactionFirstPartForwardFee(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int) *big.Int {
	if fwdFee == nil || fwdFee.Sign() == 0 {
		return bigint.FromInt64(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.FirstFrac == 0 {
		return bigint.FromInt64(0)
	}

	fee := bigint.FromUint64(uint64(prices.FirstFrac))
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

// transactionOutboundMessageFailedLayout selects the second packing layout
// after the inline form fails: StateInit moves into a ref only when it has at
// least two refs, and the body only when its inline form is non-empty.
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

		// CellStorageStat in the reference uses NoVm here, which avoids gas but
		// still notifies UsageCell while traversing reused message subtrees.
		var sl cell.Slice
		if err := c.BeginParseInto(&sl); err != nil {
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
		return bigint.FromInt64(0)
	}

	return prices.ComputeGasPrice(gasUsed)
}

func transactionComputeImportFee(cfg *PreparedBlockchainConfig, addr *address.Address, msg *tlb.Message, msgCell *cell.Cell) (*big.Int, error) {
	if msg.MsgType != tlb.MsgTypeExternalIn {
		return bigint.FromInt64(0), nil
	}

	prices := cfg.msgForwardPricesFor(transactionIsMasterchain(addr))
	if prices == nil {
		return bigint.FromInt64(0), nil
	}

	usage, err := transactionMessageTailUsage(msgCell)
	if err != nil {
		return nil, err
	}

	return transactionComputeForwardFeeWithPrices(prices, usage.cells, usage.bits), nil
}

func transactionComputeStorageFee(cfg *PreparedBlockchainConfig, acc *transactionRuntimeAccount, now uint32) (*big.Int, error) {
	if now < acc.storageInfo.LastPaid {
		return nil, fmt.Errorf("transaction unix time %d is before account last_paid %d", now, acc.storageInfo.LastPaid)
	}

	total := bigint.FromInt64(0)
	if acc.storageInfo.DuePayment != nil && acc.storageInfo.DuePayment.NanoRef().Sign() > 0 {
		total.Add(total, acc.storageInfo.DuePayment.NanoRef())
	}
	if acc.isSpecial {
		return total, nil
	}

	usage := acc.storageInfo.StorageUsed
	if usage.CellsUsed == nil || usage.BitsUsed == nil || now <= acc.storageInfo.LastPaid || acc.storageInfo.LastPaid == 0 {
		return total, nil
	}

	fee, err := cfg.computeStorageFee(transactionIsMasterchain(acc.addr), acc.storageInfo.LastPaid, now, usage.BitsUsed.Uint64(), usage.CellsUsed.Uint64())
	if err != nil {
		return nil, err
	}
	total.Add(total, fee)
	return total, nil
}

func transactionGetSizeLimits(cfg *PreparedBlockchainConfig) transactionSizeLimits {
	return cfg.sizeLimits
}

func transactionCheckOutboundMessageStatsSize(cfg *PreparedBlockchainConfig, srcAddr, dstAddr *address.Address, stats transactionMessageStatsResult, available *big.Int, isSpecial, actionFineEnabled bool) (int32, *big.Int) {
	limits := transactionGetSizeLimits(cfg)
	if isSpecial || !actionFineEnabled {
		if stats.usage.bits <= limits.maxMsgBits && stats.usage.cells <= limits.maxMsgCells && stats.merkleDepth <= 2 {
			return 0, bigint.FromInt64(0)
		}
		return 40, bigint.FromInt64(0)
	}

	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	maxCells, limitedByFunds := transactionActionFineCellLimitWithPrices(prices, limits.maxMsgCells, available)
	fine := transactionComputeActionFineForUsageWithLimit(prices, stats.usage, maxCells)
	if stats.usage.bits <= limits.maxMsgBits && stats.usage.cells <= maxCells && stats.merkleDepth <= 2 {
		return 0, bigint.FromInt64(0)
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
	maxCells, _ := transactionActionFineCellLimitWithPrices(prices, transactionGetSizeLimits(cfg).maxMsgCells, available)
	return transactionComputeActionFineForUsageWithLimit(prices, usage, maxCells)
}

func transactionComputeActionFineForUsageWithLimit(prices *tlb.ConfigMsgForwardPrices, usage transactionUsage, maxCells uint64) *big.Int {
	if prices == nil {
		return bigint.FromInt64(0)
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return bigint.FromInt64(0)
	}

	fineCells := usage.cells
	if fineCells > maxCells {
		fineCells = maxCells
	}

	// Both operands are uint64 in the reference action engine.
	return bigint.FromUint64(finePerCell * fineCells)
}

func transactionActionFineCellLimitWithPrices(prices *tlb.ConfigMsgForwardPrices, maxCells uint64, available *big.Int) (uint64, bool) {
	if prices == nil || available == nil {
		return maxCells, false
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return maxCells, false
	}

	// Preserve the reference uint64 multiplication before comparing with the
	// arbitrary-precision account balance.
	maxFine := bigint.FromUint64(maxCells * finePerCell)
	if available.Cmp(maxFine) >= 0 {
		return maxCells, false
	}
	finePerCellInt := bigint.FromUint64(finePerCell)
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
		return bigint.FromInt64(0), transactionGetSizeLimits(cfg).maxMsgCells, false
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return bigint.FromInt64(0), transactionGetSizeLimits(cfg).maxMsgCells, false
	}

	limits := transactionGetSizeLimits(cfg)
	maxCells := limits.maxMsgCells
	limitedByFunds := false
	funds, ok := transactionSendActionFineFunds(remaining, msgBalance, messageValue, gasFees, currentActionFine, mode)
	if !ok {
		funds = bigint.FromInt64(0)
	}

	// Preserve the reference uint64 multiplication before comparing with the
	// arbitrary-precision account balance.
	maxFine := bigint.FromUint64(maxCells * finePerCell)
	if funds.Cmp(maxFine) < 0 {
		finePerCellInt := bigint.FromUint64(finePerCell)
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
	fine := bigint.FromUint64(finePerCell * fineCells)
	return fine, maxCells, limitedByFunds
}

func transactionAccountStateExceedsLimits(acc *transactionRuntimeAccount, code, data *cell.Cell, libs *cell.Dictionary, cfg *PreparedBlockchainConfig, exemptSpecial bool) (bool, error) {
	return transactionAccountStateExceedsLimitsWithHint(acc, code, data, libs, cfg, exemptSpecial, nil)
}

func transactionAccountStateExceedsLimitsWithHint(acc *transactionRuntimeAccount, code, data *cell.Cell, libs *cell.Dictionary, cfg *PreparedBlockchainConfig, exemptSpecial bool, loaded *vm.LoadedCells) (bool, error) {
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

	checker, err := newTransactionAccountStateLimitChecker(acc, cfg)
	if err != nil {
		return false, err
	}
	if loaded != nil && checker.stat != nil {
		if err = checker.stat.addHint(*loaded); err != nil {
			return false, err
		}
	}
	stats, err := checker.measure(code, data, libCell)
	if err != nil {
		return false, err
	}
	if stats.usage.cells > maxCells {
		return true, nil
	}
	if stats.merkleDepth > 2 {
		return true, nil
	}

	if transactionIsMasterchain(acc.addr) && !transactionDictEqual(acc.libraries, libs) {
		publicLibraries, err := transactionPublicLibrariesCountChecked(libs)
		if err != nil {
			return false, fmt.Errorf("failed to validate account libraries: %w", err)
		}
		if publicLibraries > limits.maxAccPublicLibraries {
			return true, nil
		}
	}

	return false, nil
}

type transactionAccountStateLimitChecker struct {
	stat *transactionAccountStorageStat
}

func newTransactionAccountStateLimitChecker(acc *transactionRuntimeAccount, cfg *PreparedBlockchainConfig) (*transactionAccountStateLimitChecker, error) {
	checker := &transactionAccountStateLimitChecker{}
	if acc.accountStorageStat == nil {
		return checker, nil
	}

	storage, err := transactionOldAccountStorageForConfig(acc, cfg)
	if err != nil {
		return nil, err
	}
	bound := storage != nil && acc.statBoundTo == storage.HashKey()
	stat, err := transactionInitAccountStorageStat(
		acc.accountStorageStat,
		storage,
		acc.storageInfo.StorageUsed,
		transactionStorageExtraDictHash(acc.storageInfo.StorageExtra),
		bound,
	)
	if err != nil {
		return nil, err
	}
	if stat == nil {
		return nil, errors.New("bound account storage stat has no valid provenance")
	}
	checker.stat = stat
	return checker, nil
}

func (c *transactionAccountStateLimitChecker) measure(roots ...*cell.Cell) (transactionCellStatsResult, error) {
	if c.stat == nil {
		return transactionCellStatsForRoots(true, roots...)
	}

	var compact [4]*cell.Cell
	compactNum := 0
	for _, root := range roots {
		if root == nil {
			continue
		}
		compact[compactNum] = root
		compactNum++
	}
	maxMerkleDepth, err := c.stat.replaceRoots(compact, compactNum)
	if err != nil {
		return transactionCellStatsResult{}, err
	}
	return transactionCellStatsResult{
		usage: transactionUsage{
			cells: c.stat.totalCells,
			bits:  c.stat.totalBits,
		},
		merkleDepth: uint16(maxMerkleDepth),
	}, nil
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
	stats, err := transactionCellStatsForRoots(false, roots...)
	if err != nil {
		return transactionUsage{}, err
	}
	return stats.usage, nil
}

func transactionPublicLibrariesCountChecked(libs *cell.Dictionary) (uint64, error) {
	if libs == nil || libs.IsEmpty() {
		return 0, nil
	}
	var count uint64
	err := libs.ForEachBorrowed(false, false, func(item cell.DictItemView) error {
		value := item.Value
		isPublic, err := value.LoadBoolBit()
		if err != nil || !isPublic {
			return nil
		}
		library, err := value.LoadRefCell()
		if err != nil || library == nil {
			return nil
		}
		var key cell.Hash
		keySlice := item.Key
		if err = keySlice.LoadSliceInto(key[:], 256); err == nil && library.HashKey() == key {
			count++
		}
		return nil
	})
	return count, err
}

func transactionMaxMerkleDepth(root *cell.Cell) (uint16, error) {
	return transactionMaxMerkleDepthForRoots(root)
}

func transactionMaxMerkleDepthForRoots(roots ...*cell.Cell) (uint16, error) {
	stats, err := transactionCellStatsForRoots(false, roots...)
	if err != nil {
		return 0, err
	}
	return stats.merkleDepth, nil
}

type transactionCellStatsResult struct {
	usage       transactionUsage
	merkleDepth uint16
}

func transactionCellStatsForRoots(traceLoads bool, roots ...*cell.Cell) (transactionCellStatsResult, error) {
	var stats transactionCellStatsResult
	// seen memoizes each cell's subtree max merkle depth (merkle cells on the
	// deepest path within the subtree, including the cell itself). The value
	// is path-independent, so a cell reached again via another path just
	// contributes its memoized depth without re-walking the shared subtree —
	// the same technique transactionAccountStorageStat.addCell uses. Usage is
	// counted on first visit only, exactly as before.
	seen := make(map[cell.Hash]uint16, 64)

	var walk func(c *cell.Cell, trace *cell.Trace) (uint16, error)
	walk = func(c *cell.Cell, trace *cell.Trace) (uint16, error) {
		if c == nil {
			return 0, nil
		}

		var sl cell.Slice
		if err := c.BeginParseIntoWithoutTrace(&sl); err != nil {
			return 0, err
		}

		loaded := sl.BaseCell()
		if loaded.GetType() == cell.PrunedCellType {
			return 0, vmerr.Virtualization(1)
		}
		// State-limit checks run after the VM gas listener is detached. They
		// must not charge gas, but cells reused from the predecessor state still
		// have to reach the collator usage trace so its proof is sufficient for
		// another validator to repeat this same traversal. A newly built parent
		// may carry a detached VM trace while a raw child retains its predecessor
		// trace, hence child descent combines both explicitly.
		if traceLoads {
			if err := trace.NotifyLoadError(loaded); err != nil {
				return 0, err
			}
		}

		key := loaded.HashKey()
		if depth, ok := seen[key]; ok {
			return depth, nil
		}

		stats.usage.cells++
		stats.usage.bits += uint64(loaded.BitsSize())

		var depth uint16
		for refIndex := 0; sl.RefsNum() > 0; refIndex++ {
			ref, err := sl.LoadRefCell()
			if err != nil {
				return 0, err
			}

			var childTrace *cell.Trace
			if traceLoads {
				childTrace = ref.Trace()
				if trace != nil {
					childTrace = cell.CombineTraces(childTrace, trace.Child(refIndex))
				}
			}
			refDepth, err := walk(ref, childTrace)
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
		var trace *cell.Trace
		if traceLoads {
			trace = root.Trace()
		}
		depth, err := walk(root, trace)
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
