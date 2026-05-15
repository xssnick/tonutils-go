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

func transactionMessageGas(cfg TransactionEmulationConfig, blockchainCfg tlb.BlockchainConfig, addr *address.Address, balance, msgBalance *big.Int, msgType tlb.MsgType, isSpecial bool) vm.Gas {
	if transactionGasConfigured(cfg.Gas) {
		return cfg.Gas
	}

	prices, err := blockchainCfg.GetGasPrices(transactionIsMasterchain(addr))
	if err == nil && prices != nil && prices.GasLimit > 0 {
		if isSpecial && prices.SpecialGasLimit > 0 {
			limit := transactionGasInt(prices.SpecialGasLimit)
			credit := int64(0)
			if msgType == tlb.MsgTypeExternalIn && prices.GasCredit > 0 {
				credit = min(transactionGasInt(prices.GasCredit), limit)
			}
			if !transactionSpecialGasFull(blockchainCfg) {
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

		gasMax := transactionGasBoughtFor(prices, balance)
		gasLimit := uint64(0)
		gasCredit := uint64(0)
		if msgType == tlb.MsgTypeExternalIn {
			gasCredit = min(prices.GasCredit, gasMax)
		} else {
			gasLimit = min(transactionGasBoughtFor(prices, msgBalance), gasMax)
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

func transactionTickTockGas(cfg TransactionEmulationConfig, blockchainCfg tlb.BlockchainConfig, addr *address.Address) vm.Gas {
	if transactionGasConfigured(cfg.Gas) {
		return cfg.Gas
	}

	prices, err := blockchainCfg.GetGasPrices(transactionIsMasterchain(addr))
	if err == nil && prices != nil && prices.SpecialGasLimit > 0 {
		limit := transactionGasInt(prices.SpecialGasLimit)
		return vm.Gas{
			Max:       limit,
			Limit:     limit,
			Base:      limit,
			Remaining: limit,
		}
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
	if prices == nil || nanograms == nil || nanograms.Sign() < 0 {
		return 0
	}

	threshold := transactionMaxGasThreshold(prices)
	if nanograms.Cmp(threshold) >= 0 {
		return prices.GasLimit
	}
	if nanograms.Cmp(new(big.Int).SetUint64(prices.FlatGasPrice)) < 0 {
		return 0
	}
	if prices.GasPrice == 0 {
		return prices.GasLimit
	}

	remaining := new(big.Int).Sub(new(big.Int).Set(nanograms), new(big.Int).SetUint64(prices.FlatGasPrice))
	remaining.Lsh(remaining, 16)
	remaining.Div(remaining, new(big.Int).SetUint64(prices.GasPrice))
	remaining.Add(remaining, new(big.Int).SetUint64(prices.FlatGasLimit))
	if !remaining.IsUint64() || remaining.Uint64() > prices.GasLimit {
		return prices.GasLimit
	}
	return remaining.Uint64()
}

func transactionMaxGasThreshold(prices *tlb.ConfigGasLimitsPrices) *big.Int {
	if prices == nil || prices.GasLimit <= prices.FlatGasLimit {
		return new(big.Int).SetUint64(transactionGasFlatPrice(prices))
	}

	units := new(big.Int).SetUint64(prices.GasLimit - prices.FlatGasLimit)
	total := new(big.Int).Mul(new(big.Int).SetUint64(prices.GasPrice), units)
	total = transactionCeilShiftRight(total, 16)
	return total.Add(total, new(big.Int).SetUint64(prices.FlatGasPrice))
}

func transactionGasFlatPrice(prices *tlb.ConfigGasLimitsPrices) uint64 {
	if prices == nil {
		return 0
	}
	return prices.FlatGasPrice
}

func transactionCeilShiftRight(x *big.Int, bits uint) *big.Int {
	if x == nil || x.Sign() == 0 {
		return big.NewInt(0)
	}

	add := new(big.Int).Lsh(big.NewInt(1), bits)
	add.Sub(add, big.NewInt(1))
	return new(big.Int).Rsh(new(big.Int).Add(x, add), bits)
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
func transactionHasCapability(cfg tlb.BlockchainConfig, capability uint64) bool {
	version, err := cfg.GetGlobalVersion()
	return err == nil && version.Capabilities&capability != 0
}

func transactionGlobalVersion(cfg tlb.BlockchainConfig) uint32 {
	version, err := cfg.GetGlobalVersion()
	if err != nil {
		return 0
	}
	return version.Version
}

func transactionComputeForwardFeeForMessage(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell) *big.Int {
	return transactionComputeForwardFeeForUsage(cfg, srcAddr, dstAddr, transactionMessageTailUsage(msgCell))
}

func transactionComputeForwardFeeForUsage(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, usage transactionUsage) *big.Int {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil {
		return big.NewInt(0)
	}
	return prices.ComputeForwardFee(usage.cells, usage.bits)
}

func transactionComputeIHRFee(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int, ihrDisabled bool) *big.Int {
	if ihrDisabled || fwdFee == nil || fwdFee.Sign() == 0 {
		return big.NewInt(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.IHRFactor == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Rsh(new(big.Int).Mul(fwdFee, new(big.Int).SetUint64(uint64(prices.IHRFactor))), 16)
}

func transactionFirstPartForwardFee(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, fwdFee *big.Int) *big.Int {
	if fwdFee == nil || fwdFee.Sign() == 0 {
		return big.NewInt(0)
	}
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil || prices.FirstFrac == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Rsh(new(big.Int).Mul(fwdFee, new(big.Int).SetUint64(uint64(prices.FirstFrac))), 16)
}

func transactionGetMsgForwardPrices(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address) *tlb.ConfigMsgForwardPrices {
	prices, err := cfg.GetMsgForwardPrices(transactionIsMasterchain(srcAddr) || transactionIsMasterchain(dstAddr))
	if err != nil {
		return nil
	}
	return prices
}

func transactionOutboundInternalMessageFeeUsage(cfg tlb.BlockchainConfig, msg *tlb.InternalMessage, layout transactionOutboundLayout) (transactionUsage, error) {
	collector := newTransactionUsageCollector()
	usage := transactionUsage{}

	if msg.StateInit != nil {
		stateCell, err := tlb.ToCell(msg.StateInit)
		if err != nil {
			return transactionUsage{}, err
		}
		usage = transactionAddUsage(usage, collector.addCell(stateCell, !layout.stateInitInRef))
	}
	if msg.Body != nil {
		usage = transactionAddUsage(usage, collector.addCell(msg.Body, !layout.bodyInRef))
	}
	if transactionGlobalVersion(cfg) < 10 && msg.ExtraCurrencies != nil {
		usage = transactionAddUsage(usage, collector.addCell(msg.ExtraCurrencies.AsCell(), false))
	}

	return usage, nil
}

func transactionMessageTailUsage(root *cell.Cell) transactionUsage {
	usage := transactionCollectUsage(root)
	if root == nil || usage.cells == 0 {
		return transactionUsage{}
	}
	usage.cells--
	rootBits := uint64(root.BitsSize())
	if usage.bits >= rootBits {
		usage.bits -= rootBits
	} else {
		usage.bits = 0
	}
	return usage
}

func transactionComputeGasFee(cfg tlb.BlockchainConfig, addr *address.Address, gasUsed uint64) *big.Int {
	prices, err := cfg.GetGasPrices(transactionIsMasterchain(addr))
	if err != nil || prices == nil {
		return big.NewInt(0)
	}

	return prices.ComputeGasPrice(gasUsed)
}

func transactionComputeImportFee(cfg tlb.BlockchainConfig, addr *address.Address, msg *tlb.Message, msgCell *cell.Cell) *big.Int {
	if msg.MsgType != tlb.MsgTypeExternalIn {
		return big.NewInt(0)
	}

	prices, err := cfg.GetMsgForwardPrices(transactionIsMasterchain(addr))
	if err != nil || prices == nil {
		return big.NewInt(0)
	}

	usage := transactionCollectUsage(msgCell)
	if usage.cells == 0 {
		return big.NewInt(0)
	}
	if usage.cells > 0 {
		usage.cells--
	}

	rootBits := uint64(msgCell.BitsSize())
	if usage.bits >= rootBits {
		usage.bits -= rootBits
	} else {
		usage.bits = 0
	}

	return prices.ComputeForwardFee(usage.cells, usage.bits)
}

func transactionComputeStorageFee(cfg tlb.BlockchainConfig, acc *transactionRuntimeAccount, now uint32) (*big.Int, error) {
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

	fee, err := cfg.ComputeStorageFee(transactionIsMasterchain(acc.addr), acc.storageInfo.LastPaid, now, usage.BitsUsed.Uint64(), usage.CellsUsed.Uint64())
	if err != nil {
		if errors.Is(err, tlb.ErrBlockchainConfigRootNil) || errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
			return total, nil
		}
		return nil, err
	}
	total.Add(total, fee)
	return total, nil
}

func transactionGetStorageDueLimits(cfg tlb.BlockchainConfig, addr *address.Address) transactionStorageDueLimits {
	limits := transactionStorageDueLimits{
		freezeDue: big.NewInt(0),
		deleteDue: big.NewInt(0),
	}
	prices, err := cfg.GetGasPrices(transactionIsMasterchain(addr))
	if err != nil || prices == nil {
		return limits
	}
	limits.freezeDue = new(big.Int).SetUint64(prices.FreezeDueLimit)
	limits.deleteDue = new(big.Int).SetUint64(prices.DeleteDueLimit)
	return limits
}
func transactionGetSizeLimits(cfg tlb.BlockchainConfig) transactionSizeLimits {
	out := transactionSizeLimits{
		maxMsgBits:                  1 << 21,
		maxMsgCells:                 1 << 13,
		maxLibraryCells:             1000,
		maxAccStateCells:            1 << 16,
		maxMCAccStateCells:          1 << 11,
		maxAccPublicLibraries:       256,
		maxMsgExtraCurrencies:       2,
		accStateCellsForStorageDict: 26,
	}
	limits, err := cfg.GetSizeLimitsConfig()
	if err != nil {
		return out
	}
	switch v := limits.Config.(type) {
	case tlb.SizeLimitsConfigV1:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
	case tlb.SizeLimitsConfigV2:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
		out.maxAccStateCells = uint64(v.MaxAccStateCells)
		out.maxMCAccStateCells = uint64(v.MaxMCAccStateCells)
		out.maxAccPublicLibraries = uint64(v.MaxAccPublicLibraries)
		out.maxMsgExtraCurrencies = uint64(v.MaxMsgExtraCurrencies)
		out.accStateCellsForStorageDict = uint64(v.AccStateCellsForStorageDict)
	}
	return out
}

func transactionCheckOutboundMessageSize(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell, available *big.Int) (int32, *big.Int) {
	limits := transactionGetSizeLimits(cfg)
	usage := transactionMessageTailUsage(msgCell)
	if usage.bits <= limits.maxMsgBits && usage.cells <= limits.maxMsgCells && transactionMaxMerkleDepth(msgCell) <= 2 {
		return 0, big.NewInt(0)
	}
	return 40, transactionComputeActionFine(cfg, srcAddr, dstAddr, msgCell, available)
}

func transactionComputeActionFine(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, msgCell *cell.Cell, available *big.Int) *big.Int {
	return transactionComputeActionFineForUsage(cfg, srcAddr, dstAddr, transactionMessageTailUsage(msgCell), available)
}

func transactionComputeActionFineForUsage(cfg tlb.BlockchainConfig, srcAddr, dstAddr *address.Address, usage transactionUsage, available *big.Int) *big.Int {
	prices := transactionGetMsgForwardPrices(cfg, srcAddr, dstAddr)
	if prices == nil {
		return big.NewInt(0)
	}
	finePerCell := (prices.CellPrice >> 16) / 4
	if finePerCell == 0 {
		return big.NewInt(0)
	}
	fine := new(big.Int).Mul(new(big.Int).SetUint64(finePerCell), new(big.Int).SetUint64(usage.cells))
	if available != nil && fine.Cmp(available) > 0 {
		fine.Set(available)
	}
	return fine
}

func transactionAccountStateExceedsLimits(acc *transactionRuntimeAccount, code, data *cell.Cell, libs *cell.Dictionary, cfg tlb.BlockchainConfig) (bool, error) {
	if transactionCellEqual(acc.code, code) && transactionCellEqual(acc.data, data) && transactionDictEqual(acc.libraries, libs) {
		return false, nil
	}
	limits := transactionGetSizeLimits(cfg)
	maxCells := limits.maxAccStateCells
	if transactionIsMasterchain(acc.addr) {
		maxCells = limits.maxMCAccStateCells
	}
	usage := transactionCollectUniqueUsage(code, data)
	if libs != nil && !libs.IsEmpty() {
		usage = transactionAddUsage(usage, transactionCollectUsage(libs.AsCell()))
	}
	if usage.cells > maxCells {
		return true, nil
	}
	if transactionIsMasterchain(acc.addr) && transactionPublicLibrariesCount(libs) > limits.maxAccPublicLibraries {
		return true, nil
	}
	return false, nil
}

func transactionCellEqual(a, b *cell.Cell) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return bytes.Equal(a.Hash(), b.Hash())
}

func transactionDictEqual(a, b *cell.Dictionary) bool {
	if a == nil || a.IsEmpty() {
		return b == nil || b.IsEmpty()
	}
	if b == nil || b.IsEmpty() {
		return false
	}
	return bytes.Equal(a.AsCell().Hash(), b.AsCell().Hash())
}

func transactionCollectUniqueUsage(roots ...*cell.Cell) transactionUsage {
	seen := map[cell.Hash]struct{}{}
	var walk func(c *cell.Cell) transactionUsage
	walk = func(c *cell.Cell) transactionUsage {
		if c == nil {
			return transactionUsage{}
		}
		key := c.HashKey()
		if _, ok := seen[key]; ok {
			return transactionUsage{}
		}
		seen[key] = struct{}{}
		res := transactionUsage{
			cells: 1,
			bits:  uint64(c.BitsSize()),
		}
		for i := 0; i < int(c.RefsNum()); i++ {
			res = transactionAddUsage(res, walk(c.MustPeekRef(i)))
		}
		return res
	}
	out := transactionUsage{}
	for _, root := range roots {
		out = transactionAddUsage(out, walk(root))
	}
	return out
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

func transactionMaxMerkleDepth(root *cell.Cell) uint16 {
	var maxDepth uint16
	var walk func(c *cell.Cell, depth uint16)
	walk = func(c *cell.Cell, depth uint16) {
		if c == nil {
			return
		}
		switch c.GetType() {
		case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		for i := 0; i < int(c.RefsNum()); i++ {
			walk(c.MustPeekRef(i), depth)
		}
	}
	walk(root, 0)
	return maxDepth
}

func transactionIsMasterchain(addr *address.Address) bool {
	return addr != nil && addr.Type() == address.StdAddress && addr.Workchain() == -1
}

func transactionIsSpecialAccount(cfg tlb.BlockchainConfig, addr *address.Address) bool {
	if !transactionIsMasterchain(addr) || len(addr.Data()) != 32 {
		return false
	}

	configAddr, err := cfg.GetConfigAddress()
	if err == nil && bytes.Equal(configAddr, addr.Data()) {
		return true
	}

	fundamental, err := cfg.GetFundamentalSmartContractAddresses()
	if err != nil || fundamental.Addresses == nil {
		return false
	}

	key := cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
	_, err = fundamental.Addresses.LoadValue(key)
	return err == nil
}

func transactionSpecialGasFull(cfg tlb.BlockchainConfig) bool {
	version, err := cfg.GetGlobalVersion()
	return err == nil && version.Version >= 5
}
