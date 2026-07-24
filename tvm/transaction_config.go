package tvm

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var errConfigRootRequired = errors.New("config root is required")

type preparedAddr256 [32]byte

type preparedSuspendedAddr struct {
	workchain int32
	addr      preparedAddr256
}

type preparedStoragePrice struct {
	price tlb.ConfigStoragePrices
	// slice is the raw dictionary value stored in the c7 unpacked config tuple.
	slice *cell.Slice
}

// preparedUnpackedParamIDs are the config params mirrored into the c7 unpacked
// config tuple (elements 1..6); element 0 is the storage prices entry active
// at block time and is resolved per block.
var preparedUnpackedParamIDs = [6]uint32{
	tlb.ConfigParamGlobalID,
	tlb.ConfigParamGasPricesMasterchain,
	tlb.ConfigParamGasPricesBasechain,
	tlb.ConfigParamMsgForwardPricesMasterchain,
	tlb.ConfigParamMsgForwardPricesBasechain,
	tlb.ConfigParamSizeLimits,
}

// PreparedBlockchainConfig is the per-config-epoch execution context: everything the
// transaction executor needs from the blockchain config, derived once from the
// config root. It is immutable after PrepareBlockchainConfig returns and safe to share
// between concurrently executing account lanes.
type PreparedBlockchainConfig struct {
	root         *cell.Cell
	version      uint32
	capabilities uint64

	sizeLimits transactionSizeLimits
	// gasPrices/fwdPrices/dueLimits are indexed by transactionConfigMasterchainIndex.
	gasPrices [2]*tlb.ConfigGasLimitsPrices
	fwdPrices [2]*tlb.ConfigMsgForwardPrices
	dueLimits [2]transactionStorageDueLimits
	// storagePrices are sorted by ValidSince ascending.
	storagePrices []preparedStoragePrice

	globalID    int32
	hasGlobalID bool

	specialAccounts map[preparedAddr256]struct{}
	blackholeAddr   *preparedAddr256
	suspendedUntil  uint32
	suspended       map[preparedSuspendedAddr]struct{}
	precompiled     map[preparedAddr256]uint64
	workchains      map[int32]*tlb.WorkchainDescr
	hasWorkchains   bool

	// unpackedParams are the raw param roots for c7 unpacked config elements
	// 1..6 (see preparedUnpackedParamIDs); nil when the param is absent.
	unpackedParams [6]*cell.Cell
}

// PrepareBlockchainConfig derives the immutable per-epoch execution context from a
// blockchain config root. The global version (config param 8) must be present.
// Versions newer than this implementation execute with all known version gates.
//
// Storage prices (18), gas prices (20/21), and message forward prices (24/25)
// must be present and fully consumed. Size limits (43) may be absent, in which
// case defaults apply. An absent workchain list (12) remains an empty list that
// rejects every non-masterchain destination.
func PrepareBlockchainConfig(configRoot *cell.Cell) (*PreparedBlockchainConfig, error) {
	if configRoot == nil {
		return nil, errConfigRootRequired
	}
	bc := tlb.BlockchainConfig{Root: configRoot}

	globalVersion, err := bc.GetGlobalVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to load global version config param: %w", err)
	}
	if err = validateGlobalVersion(int(globalVersion.Version)); err != nil {
		return nil, err
	}
	return prepareBlockchainConfigParams(bc, globalVersion, true)
}

// PrepareBlockchainConfigLenient accepts partial configs for tooling and
// differential harnesses. The transaction executor must use the strict
// PrepareBlockchainConfig.
func PrepareBlockchainConfigLenient(configRoot *cell.Cell) (*PreparedBlockchainConfig, error) {
	return prepareBlockchainConfigLenient(configRoot)
}

func prepareBlockchainConfigLenient(configRoot *cell.Cell) (*PreparedBlockchainConfig, error) {
	if configRoot == nil {
		return nil, errConfigRootRequired
	}
	bc := tlb.BlockchainConfig{Root: configRoot}

	globalVersion, err := bc.GetGlobalVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to load global version config param: %w", err)
	}
	if err = validateGlobalVersion(int(globalVersion.Version)); err != nil {
		return nil, err
	}
	return prepareBlockchainConfigParams(bc, globalVersion, false)
}

// prepareBlockchainConfigParams derives the execution context from the validated
// global version and the remaining config params. Strict mode rejects missing
// or malformed mandatory params; lenient mode accepts partial emulation configs.
func prepareBlockchainConfigParams(bc tlb.BlockchainConfig, globalVersion tlb.GlobalVersion, strict bool) (*PreparedBlockchainConfig, error) {
	out := &PreparedBlockchainConfig{
		root:         bc.Root,
		version:      effectiveGlobalVersion(globalVersion.Version),
		capabilities: globalVersion.Capabilities,
	}

	var err error
	if strict {
		if out.sizeLimits, err = transactionLoadSizeLimitsStrict(bc); err != nil {
			return nil, err
		}
	} else {
		out.sizeLimits = transactionLoadSizeLimits(bc)
	}
	for _, masterchain := range []bool{false, true} {
		idx := transactionConfigMasterchainIndex(masterchain)
		if strict {
			if out.gasPrices[idx], err = transactionLoadGasPricesStrict(bc, masterchain); err != nil {
				return nil, err
			}
			if out.fwdPrices[idx], err = transactionLoadMsgForwardPricesStrict(bc, masterchain); err != nil {
				return nil, err
			}
		} else {
			out.gasPrices[idx] = transactionLoadGasPrices(bc, masterchain)
			out.fwdPrices[idx] = transactionLoadMsgForwardPrices(bc, masterchain)
		}
		out.dueLimits[idx] = transactionLoadStorageDueLimits(out.gasPrices[idx])
	}
	if err = out.prepareStoragePrices(bc, strict); err != nil {
		return nil, err
	}
	out.prepareGlobalID(bc)
	out.prepareSpecialAccounts(bc)
	out.prepareBlackhole(bc)
	if err = out.prepareSuspended(bc); err != nil {
		return nil, err
	}
	if err = out.preparePrecompiled(bc); err != nil {
		return nil, err
	}
	if err = out.prepareWorkchains(bc, strict); err != nil {
		return nil, err
	}
	for i, id := range preparedUnpackedParamIDs {
		if param, err := bc.GetParam(id); err == nil {
			out.unpackedParams[i] = param
		}
	}
	return out, nil
}

// MustPrepareBlockchainConfig is PrepareBlockchainConfig that panics on error.
func MustPrepareBlockchainConfig(configRoot *cell.Cell) *PreparedBlockchainConfig {
	out, err := PrepareBlockchainConfig(configRoot)
	if err != nil {
		panic(err)
	}
	return out
}

// Root returns the raw config root the context was prepared from.
func (c *PreparedBlockchainConfig) Root() *cell.Cell {
	return c.root
}

// GlobalVersion returns the effective global version used for execution.
func (c *PreparedBlockchainConfig) GlobalVersion() uint32 {
	return c.version
}

// Capabilities returns the capability bit set of config param 8.
func (c *PreparedBlockchainConfig) Capabilities() uint64 {
	return c.capabilities
}

func (c *PreparedBlockchainConfig) globalVersion() uint32 {
	return c.version
}

func (c *PreparedBlockchainConfig) hasCapability(capability uint64) bool {
	return c.capabilities&capability != 0
}

func (c *PreparedBlockchainConfig) specialGasFull() bool {
	return c.globalVersion() >= 5
}

func transactionConfigMasterchainIndex(masterchain bool) int {
	if masterchain {
		return 1
	}
	return 0
}

func (c *PreparedBlockchainConfig) gasPricesFor(masterchain bool) *tlb.ConfigGasLimitsPrices {
	return c.gasPrices[transactionConfigMasterchainIndex(masterchain)]
}

func (c *PreparedBlockchainConfig) msgForwardPricesFor(masterchain bool) *tlb.ConfigMsgForwardPrices {
	return c.fwdPrices[transactionConfigMasterchainIndex(masterchain)]
}

func (c *PreparedBlockchainConfig) storageDueLimitsFor(masterchain bool) transactionStorageDueLimits {
	return c.dueLimits[transactionConfigMasterchainIndex(masterchain)]
}

func (c *PreparedBlockchainConfig) prepareStoragePrices(bc tlb.BlockchainConfig, strict bool) error {
	param, err := bc.GetParam(tlb.ConfigParamStoragePrices)
	if err != nil {
		// Param 18 is mandatory in strict mode. Lenient tooling may use an empty
		// price list when it is absent.
		if !strict && errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
			return nil
		}
		return fmt.Errorf("failed to load storage prices config param: %w", err)
	}

	entries, err := param.AsDict(32).LoadAll()
	if err != nil {
		return fmt.Errorf("failed to load storage prices dict: %w", err)
	}

	prices := make([]preparedStoragePrice, 0, len(entries))
	for _, entry := range entries {
		key, err := entry.Key.LoadUInt(32)
		if err != nil {
			return fmt.Errorf("failed to decode storage prices key: %w", err)
		}

		var price tlb.ConfigStoragePrices
		value := entry.Value.Copy()
		if err = tlb.LoadFromCell(&price, value); err != nil {
			return fmt.Errorf("failed to decode storage prices entry %d: %w", key, err)
		}
		if value.BitsLeft() != 0 || value.RefsNum() != 0 {
			return fmt.Errorf("storage prices entry %d has trailing data", key)
		}
		if price.ValidSince != uint32(key) {
			return fmt.Errorf("invalid storage prices entry %d: valid_since=%d", uint32(key), price.ValidSince)
		}

		prices = append(prices, preparedStoragePrice{price: price, slice: entry.Value})
	}

	sort.Slice(prices, func(i, j int) bool {
		return prices[i].price.ValidSince < prices[j].price.ValidSince
	})
	c.storagePrices = prices
	return nil
}

func (c *PreparedBlockchainConfig) prepareGlobalID(bc tlb.BlockchainConfig) {
	globalID, err := bc.GetGlobalID()
	if err != nil {
		return
	}
	c.globalID = globalID.GlobalID
	c.hasGlobalID = true
}

func (c *PreparedBlockchainConfig) prepareSpecialAccounts(bc tlb.BlockchainConfig) {
	c.specialAccounts = map[preparedAddr256]struct{}{}
	if configAddr, err := bc.GetConfigAddress(); err == nil && len(configAddr) == 32 {
		c.specialAccounts[preparedAddr256(configAddr)] = struct{}{}
	}

	fundamental, err := bc.GetFundamentalSmartContractAddresses()
	if err != nil || fundamental.Addresses == nil {
		return
	}
	items, err := fundamental.Addresses.LoadAll(true)
	if err != nil {
		return
	}
	for _, item := range items {
		addr, err := item.Key.LoadSlice(256)
		if err != nil || len(addr) != 32 {
			continue
		}
		c.specialAccounts[preparedAddr256(addr)] = struct{}{}
	}
}

func (c *PreparedBlockchainConfig) prepareBlackhole(bc tlb.BlockchainConfig) {
	burning, err := bc.GetBurningConfig()
	if err != nil || len(burning.BlackholeAddr) != 32 {
		return
	}
	blackhole := preparedAddr256(burning.BlackholeAddr)
	c.blackholeAddr = &blackhole
}

func (c *PreparedBlockchainConfig) prepareSuspended(bc tlb.BlockchainConfig) error {
	list, err := bc.GetSuspendedAddressList()
	if err != nil {
		if errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
			return nil
		}
		return fmt.Errorf("failed to load suspended address list config param: %w", err)
	}
	c.suspendedUntil = list.SuspendedUntil
	c.suspended = map[preparedSuspendedAddr]struct{}{}
	if list.Addresses == nil || list.Addresses.IsEmpty() {
		return nil
	}

	items, err := list.Addresses.LoadAll(true)
	if err != nil {
		return fmt.Errorf("failed to load suspended address list: %w", err)
	}
	for _, item := range items {
		workchain, err := item.Key.LoadInt(32)
		if err != nil {
			return fmt.Errorf("failed to decode suspended address workchain: %w", err)
		}
		addr, err := item.Key.LoadSlice(256)
		if err != nil {
			return fmt.Errorf("failed to decode suspended address: %w", err)
		}
		c.suspended[preparedSuspendedAddr{
			workchain: int32(workchain),
			addr:      preparedAddr256(addr),
		}] = struct{}{}
	}
	return nil
}

func (c *PreparedBlockchainConfig) preparePrecompiled(bc tlb.BlockchainConfig) error {
	precompiled, err := bc.GetPrecompiledContractsConfig()
	if err != nil {
		if errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
			return nil
		}
		return fmt.Errorf("failed to load precompiled contracts config param: %w", err)
	}
	if precompiled.List == nil || precompiled.List.IsEmpty() {
		return nil
	}

	items, err := precompiled.List.LoadAll(true)
	if err != nil {
		return fmt.Errorf("failed to load precompiled contracts list: %w", err)
	}
	c.precompiled = make(map[preparedAddr256]uint64, len(items))
	for _, item := range items {
		codeHash, err := item.Key.LoadSlice(256)
		if err != nil {
			return fmt.Errorf("failed to decode precompiled contract code hash: %w", err)
		}
		var smc tlb.PrecompiledSmc
		if err = tlb.LoadFromCell(&smc, item.Value); err != nil {
			return fmt.Errorf("failed to decode precompiled contract entry: %w", err)
		}
		c.precompiled[preparedAddr256(codeHash)] = smc.GasUsage
	}
	return nil
}

func (c *PreparedBlockchainConfig) prepareWorkchains(bc tlb.BlockchainConfig, strict bool) error {
	workchains, err := bc.GetWorkchains()
	if err != nil || workchains == nil || workchains.Workchains == nil {
		absent := err == nil || errors.Is(err, tlb.ErrBlockchainConfigParamAbsent)
		if strict {
			if !absent {
				return fmt.Errorf("failed to load workchains config param: %w", err)
			}
			// Keep destination checks enabled with an empty list when param 12 is
			// absent, rejecting every non-masterchain workchain.
			c.hasWorkchains = true
			c.workchains = map[int32]*tlb.WorkchainDescr{}
		}
		return nil
	}
	c.hasWorkchains = true
	c.workchains = map[int32]*tlb.WorkchainDescr{}

	items, err := workchains.Workchains.LoadAll(true)
	if err != nil {
		if strict {
			return fmt.Errorf("failed to load workchains config dict: %w", err)
		}
		return nil
	}
	for _, item := range items {
		workchain, err := item.Key.LoadInt(32)
		if err != nil {
			if strict {
				return fmt.Errorf("failed to decode workchain id: %w", err)
			}
			continue
		}
		var descr tlb.WorkchainDescr
		if err = tlb.LoadFromCell(&descr, item.Value); err != nil {
			if strict {
				return fmt.Errorf("failed to decode workchain %d descriptor: %w", workchain, err)
			}
			continue
		}
		c.workchains[int32(workchain)] = &descr
	}
	return nil
}

// workchainDescr resolves a workchain descriptor from config param 12.
// checksEnabled is false only for lenient (test-helper) configs prepared
// without the param; strict configs keep checks enabled with an empty list.
func (c *PreparedBlockchainConfig) workchainDescr(workchain int32) (descr *tlb.WorkchainDescr, found, checksEnabled bool) {
	if !c.hasWorkchains {
		return nil, false, false
	}
	descr, found = c.workchains[workchain]
	return descr, found, true
}

func (c *PreparedBlockchainConfig) isSpecialAccount(addr *address.Address) bool {
	if len(c.specialAccounts) == 0 || !transactionIsMasterchain(addr) {
		return false
	}
	addrData := addr.Data()
	if len(addrData) != 32 {
		return false
	}
	_, ok := c.specialAccounts[preparedAddr256(addrData)]
	return ok
}

func (c *PreparedBlockchainConfig) isBlackHoleAccount(addr *address.Address) bool {
	if c.blackholeAddr == nil || !transactionIsMasterchain(addr) {
		return false
	}
	addrData := addr.Data()
	return len(addrData) == 32 && preparedAddr256(addrData) == *c.blackholeAddr
}

func (c *PreparedBlockchainConfig) isAddressSuspended(now uint32, addr *address.Address) bool {
	if len(c.suspended) == 0 || c.suspendedUntil <= now || addr.Type() != address.StdAddress {
		return false
	}
	addrData := addr.Data()
	if len(addrData) != 32 {
		return false
	}
	_, ok := c.suspended[preparedSuspendedAddr{
		workchain: addr.Workchain(),
		addr:      preparedAddr256(addrData),
	}]
	return ok
}

// precompiledGasUsage returns the configured gas usage for a precompiled
// contract code hash (config param 45), or nil when not precompiled.
func (c *PreparedBlockchainConfig) precompiledGasUsage(code *cell.Cell) *big.Int {
	if code == nil || len(c.precompiled) == 0 {
		return nil
	}
	usage, ok := c.precompiled[preparedAddr256(code.HashKey())]
	if !ok {
		return nil
	}
	return new(big.Int).SetUint64(usage)
}

// currentStoragePricesSlice returns the raw dict value slice of the
// storage-prices entry active at now (the latest entry with ValidSince <= now),
// or nil when none applies.
func (c *PreparedBlockchainConfig) currentStoragePricesSlice(now uint32) *cell.Slice {
	var best *cell.Slice
	for i := range c.storagePrices {
		if now != 0 && c.storagePrices[i].price.ValidSince > now {
			continue
		}
		best = c.storagePrices[i].slice
	}
	if best == nil {
		return nil
	}
	return best.Copy()
}

// computeStorageFee accrues the storage fee over [lastPaid, now) across all
// active storage-price windows, mirroring tlb.BlockchainConfig.ComputeStorageFee.
func (c *PreparedBlockchainConfig) computeStorageFee(masterchain bool, lastPaid, now uint32, bits, cells uint64) *big.Int {
	if now <= lastPaid || lastPaid == 0 {
		return big.NewInt(0)
	}
	entries := c.storagePrices
	if len(entries) == 0 || now <= entries[0].price.ValidSince {
		return big.NewInt(0)
	}

	i := len(entries)
	for i > 0 && entries[i-1].price.ValidSince > lastPaid {
		i--
	}
	if i > 0 {
		i--
	}

	upto := lastPaid
	if entries[0].price.ValidSince > upto {
		upto = entries[0].price.ValidSince
	}

	total := big.NewInt(0)
	for ; i < len(entries) && upto < now; i++ {
		validUntil := now
		if i < len(entries)-1 && entries[i+1].price.ValidSince < validUntil {
			validUntil = entries[i+1].price.ValidSince
		}
		if upto >= validUntil {
			continue
		}

		total.Add(total, transactionStorageFeeRaw(entries[i].price, masterchain, uint64(validUntil-upto), bits, cells))
		upto = validUntil
	}

	return transactionCeilShiftRight(total, 16)
}

func transactionStorageFeeRaw(price tlb.ConfigStoragePrices, masterchain bool, delta, bits, cells uint64) *big.Int {
	bitPrice := price.BitPrice
	cellPrice := price.CellPrice
	if masterchain {
		bitPrice = price.MCBitPrice
		cellPrice = price.MCCellPrice
	}

	total := new(big.Int).Mul(new(big.Int).SetUint64(cells), new(big.Int).SetUint64(cellPrice))
	total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(bits), new(big.Int).SetUint64(bitPrice)))
	total.Mul(total, new(big.Int).SetUint64(delta))
	return total
}

func transactionLoadGasPrices(blockchainCfg tlb.BlockchainConfig, masterchain bool) *tlb.ConfigGasLimitsPrices {
	prices, err := blockchainCfg.GetGasPrices(masterchain)
	if err != nil {
		return nil
	}
	return prices
}

// transactionLoadGasPricesStrict requires config param 20/21 to be present,
// well-formed, and fully consumed.
func transactionLoadGasPricesStrict(blockchainCfg tlb.BlockchainConfig, masterchain bool) (*tlb.ConfigGasLimitsPrices, error) {
	paramID := tlb.ConfigParamGasPricesBasechain
	if masterchain {
		paramID = tlb.ConfigParamGasPricesMasterchain
	}
	root, err := blockchainCfg.GetParam(paramID)
	if err != nil {
		return nil, fmt.Errorf("failed to load gas prices config param %d: %w", paramID, err)
	}

	var prices tlb.ConfigGasLimitsPrices
	if err = tlb.Parse(&prices, root); err != nil {
		return nil, fmt.Errorf("failed to parse gas prices config param %d: %w", paramID, err)
	}
	// ConfigGasLimitsPrices parses a copy of the slice, so exactness is checked
	// against the serialized size of the parsed record.
	expectedBits := uint(392) // 8-bit tag + 3x64 gas fields + 3x64 block/due limits
	if prices.HasSeparateSpecialLimit {
		expectedBits += 64
	}
	if prices.HasFlatPricing {
		expectedBits += 136 // 8-bit tag + 2x64 flat fields
	}
	if root.BitsSize() != expectedBits || root.RefsNum() != 0 {
		return nil, fmt.Errorf("gas prices config param %d has unparsed data", paramID)
	}
	return &prices, nil
}

func transactionLoadMsgForwardPrices(blockchainCfg tlb.BlockchainConfig, masterchain bool) *tlb.ConfigMsgForwardPrices {
	prices, err := blockchainCfg.GetMsgForwardPrices(masterchain)
	if err != nil {
		return nil
	}
	return prices
}

// transactionLoadMsgForwardPricesStrict requires config param 24/25 to be
// present, well-formed, and fully consumed.
func transactionLoadMsgForwardPricesStrict(blockchainCfg tlb.BlockchainConfig, masterchain bool) (*tlb.ConfigMsgForwardPrices, error) {
	paramID := tlb.ConfigParamMsgForwardPricesBasechain
	if masterchain {
		paramID = tlb.ConfigParamMsgForwardPricesMasterchain
	}
	root, err := blockchainCfg.GetParam(paramID)
	if err != nil {
		return nil, fmt.Errorf("failed to load msg forward prices config param %d: %w", paramID, err)
	}

	loader, err := root.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse msg forward prices config param %d: %w", paramID, err)
	}
	var prices tlb.ConfigMsgForwardPrices
	if err = tlb.LoadFromCell(&prices, loader); err != nil {
		return nil, fmt.Errorf("failed to parse msg forward prices config param %d: %w", paramID, err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return nil, fmt.Errorf("msg forward prices config param %d has unparsed data", paramID)
	}
	return &prices, nil
}

func transactionLoadStorageDueLimits(prices *tlb.ConfigGasLimitsPrices) transactionStorageDueLimits {
	limits := transactionStorageDueLimits{
		freezeDue: big.NewInt(0),
		deleteDue: big.NewInt(0),
	}
	if prices == nil {
		return limits
	}
	limits.freezeDue = new(big.Int).SetUint64(prices.FreezeDueLimit)
	limits.deleteDue = new(big.Int).SetUint64(prices.DeleteDueLimit)
	return limits
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
		maxVMDataDepth:              vm.MaxDataDepth,
	}
}

func transactionLoadSizeLimits(blockchainCfg tlb.BlockchainConfig) transactionSizeLimits {
	config, err := blockchainCfg.GetSizeLimitsConfig()
	if err != nil {
		return transactionDefaultSizeLimits()
	}
	return transactionApplySizeLimits(config)
}

// transactionLoadSizeLimitsStrict loads config param 43. An absent param uses
// defaults; a present param must be well-formed and fully consumed.
func transactionLoadSizeLimitsStrict(blockchainCfg tlb.BlockchainConfig) (transactionSizeLimits, error) {
	root, err := blockchainCfg.GetParam(tlb.ConfigParamSizeLimits)
	if err != nil {
		if errors.Is(err, tlb.ErrBlockchainConfigParamAbsent) {
			return transactionDefaultSizeLimits(), nil
		}
		return transactionSizeLimits{}, fmt.Errorf("failed to load size limits config param: %w", err)
	}

	loader, err := root.BeginParse()
	if err != nil {
		return transactionSizeLimits{}, fmt.Errorf("failed to parse size limits config param: %w", err)
	}
	var config tlb.SizeLimitsConfig
	if err = tlb.LoadFromCell(&config, loader); err != nil {
		return transactionSizeLimits{}, fmt.Errorf("failed to parse size limits config param: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return transactionSizeLimits{}, errors.New("size limits config param has unparsed data")
	}
	return transactionApplySizeLimits(config), nil
}

func transactionApplySizeLimits(config tlb.SizeLimitsConfig) transactionSizeLimits {
	out := transactionDefaultSizeLimits()
	switch v := config.Config.(type) {
	case tlb.SizeLimitsConfigV1:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
		out.maxExtMsgDepth = v.MaxExtMsgDepth
		out.maxVMDataDepth = v.MaxVMDataDepth
	case tlb.SizeLimitsConfigV2:
		out.maxMsgBits = uint64(v.MaxMsgBits)
		out.maxMsgCells = uint64(v.MaxMsgCells)
		out.maxLibraryCells = uint64(v.MaxLibraryCells)
		out.maxExtMsgDepth = v.MaxExtMsgDepth
		out.maxVMDataDepth = v.MaxVMDataDepth
		out.maxAccStateCells = uint64(v.MaxAccStateCells)
		out.maxMCAccStateCells = uint64(v.MaxMCAccStateCells)
		out.maxAccPublicLibraries = uint64(v.MaxAccPublicLibraries)
		out.maxMsgExtraCurrencies = uint64(v.MaxMsgExtraCurrencies)
		out.maxAccFixedPrefixLength = uint64(v.MaxAccFixedPrefixLength)
		out.accStateCellsForStorageDict = uint64(v.AccStateCellsForStorageDict)
		if v.MaxTransactionLibraryLoads != nil {
			limit := *v.MaxTransactionLibraryLoads
			out.maxTransactionLibraryLoads = &limit
		}
	}
	return out
}
