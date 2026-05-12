package tlb

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

var (
	ErrBlockchainConfigRootNil     = errors.New("blockchain config root is nil")
	ErrBlockchainConfigParamAbsent = errors.New("blockchain config param is absent")
)

type BlockchainConfig struct {
	Root *cell.Cell
}

func (c BlockchainConfig) ToCell() (*cell.Cell, error) {
	if c.Root == nil {
		return nil, ErrBlockchainConfigRootNil
	}

	return c.Root, nil
}

func (c BlockchainConfig) Get(id int32) *cell.Cell {
	if c.Root == nil {
		return nil
	}

	val, err := c.Root.AsDict(32).LoadValueByIntKey(big.NewInt(int64(id)))
	if err != nil {
		return nil
	}

	ref, err := val.LoadRefCell()
	if err != nil {
		return nil
	}

	return ref
}

func (c BlockchainConfig) All() map[int32]*cell.Cell {
	if c.Root == nil {
		return nil
	}

	kvs, err := c.Root.AsDict(32).LoadAll()
	if err != nil {
		return nil
	}

	res := make(map[int32]*cell.Cell, len(kvs))

	for _, kv := range kvs {
		ref, loadErr := kv.Value.LoadRefCell()
		if loadErr != nil {
			return nil
		}

		res[int32(kv.Key.MustLoadInt(32))] = ref
	}

	return res
}

func (c BlockchainConfig) GetParam(id uint32) (*cell.Cell, error) {
	if c.Root == nil {
		return nil, ErrBlockchainConfigRootNil
	}

	val, err := c.Root.AsDict(32).LoadValueByIntKey(new(big.Int).SetUint64(uint64(id)))
	if err != nil {
		if errors.Is(err, cell.ErrNoSuchKeyInDict) {
			return nil, fmt.Errorf("%w: %d", ErrBlockchainConfigParamAbsent, id)
		}
		return nil, fmt.Errorf("failed to load config param %d: %w", id, err)
	}

	ref, err := val.LoadRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to decode config param %d: %w", id, err)
	}

	return ref, nil
}

func (c BlockchainConfig) GetStoragePrices(now uint32) (*ConfigStoragePrices, error) {
	entries, err := c.getStoragePricesEntries()
	if err != nil {
		return nil, err
	}
	var best *ConfigStoragePrices
	var bestSince uint32

	for i := range entries {
		price := entries[i]
		if now != 0 && price.ValidSince > now {
			continue
		}
		if best != nil && price.ValidSince <= bestSince {
			continue
		}

		priceCopy := price
		best = &priceCopy
		bestSince = price.ValidSince
	}

	if best == nil {
		return nil, fmt.Errorf("no active storage prices for time %d", now)
	}

	return best, nil
}

func (c BlockchainConfig) ComputeStorageFee(masterchain bool, lastPaid, now uint32, bits, cells uint64) (*big.Int, error) {
	if now <= lastPaid || lastPaid == 0 {
		return big.NewInt(0), nil
	}

	entries, err := c.getStoragePricesEntries()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 || now <= entries[0].ValidSince {
		return big.NewInt(0), nil
	}

	i := len(entries)
	for i > 0 && entries[i-1].ValidSince > lastPaid {
		i--
	}
	if i > 0 {
		i--
	}

	upto := lastPaid
	if entries[0].ValidSince > upto {
		upto = entries[0].ValidSince
	}

	total := big.NewInt(0)
	for ; i < len(entries) && upto < now; i++ {
		validUntil := now
		if i < len(entries)-1 && entries[i+1].ValidSince < validUntil {
			validUntil = entries[i+1].ValidSince
		}
		if upto >= validUntil {
			continue
		}

		total.Add(total, blockchainConfigStorageFeeRaw(entries[i], masterchain, uint64(validUntil-upto), bits, cells))
		upto = validUntil
	}

	return configPricesCeilShiftRight(total, 16), nil
}

func (c BlockchainConfig) getStoragePricesEntries() ([]ConfigStoragePrices, error) {
	cfg, err := blockchainConfigLoadParam[StoragePricesConfig](c, ConfigParamStoragePrices)
	if err != nil {
		return nil, err
	}

	entries, err := cfg.Prices.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load storage prices dict: %w", err)
	}

	prices := make([]ConfigStoragePrices, 0, len(entries))
	for _, entry := range entries {
		key, err := entry.Key.LoadUInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to decode storage prices key: %w", err)
		}

		var price ConfigStoragePrices
		if err = LoadFromCell(&price, entry.Value.Copy()); err != nil {
			return nil, fmt.Errorf("failed to decode storage prices entry %d: %w", key, err)
		}

		validSince := uint32(key)
		if price.ValidSince != validSince {
			return nil, fmt.Errorf("invalid storage prices entry %d: valid_since=%d", validSince, price.ValidSince)
		}

		prices = append(prices, price)
	}

	sort.Slice(prices, func(i, j int) bool {
		return prices[i].ValidSince < prices[j].ValidSince
	})
	return prices, nil
}

func blockchainConfigStorageFeeRaw(price ConfigStoragePrices, masterchain bool, delta, bits, cells uint64) *big.Int {
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

func (c BlockchainConfig) GetGasPrices(masterchain bool) (*ConfigGasLimitsPrices, error) {
	paramID := ConfigParamGasPricesBasechain
	if masterchain {
		paramID = ConfigParamGasPricesMasterchain
	}

	return blockchainConfigLoadParam[ConfigGasLimitsPrices](c, paramID)
}

func (c BlockchainConfig) GetMsgForwardPrices(masterchain bool) (*ConfigMsgForwardPrices, error) {
	paramID := ConfigParamMsgForwardPricesBasechain
	if masterchain {
		paramID = ConfigParamMsgForwardPricesMasterchain
	}

	return blockchainConfigLoadParam[ConfigMsgForwardPrices](c, paramID)
}

func (c BlockchainConfig) GetConfigAddress() ([]byte, error) {
	return c.getAddressParam(ConfigParamConfigAddress)
}

func (c BlockchainConfig) GetElectorAddress() ([]byte, error) {
	return c.getAddressParam(ConfigParamElectorAddress)
}

func (c BlockchainConfig) GetMinterAddress() ([]byte, error) {
	addr, err := c.getAddressParam(ConfigParamMinterAddress)
	if errors.Is(err, ErrBlockchainConfigParamAbsent) {
		return c.GetConfigAddress()
	}
	return addr, err
}

func (c BlockchainConfig) GetFeeCollectorAddress() ([]byte, error) {
	addr, err := c.getAddressParam(ConfigParamFeeCollectorAddress)
	if errors.Is(err, ErrBlockchainConfigParamAbsent) {
		return c.GetElectorAddress()
	}
	return addr, err
}

func (c BlockchainConfig) GetDNSRootAddress() ([]byte, error) {
	return c.getAddressParam(ConfigParamDNSRootAddress)
}

func (c BlockchainConfig) GetGlobalVersion() (GlobalVersion, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamGlobalVersion, GlobalVersion{})
}

func (c BlockchainConfig) GetGlobalID() (*GlobalIDConfig, error) {
	return blockchainConfigLoadParam[GlobalIDConfig](c, ConfigParamGlobalID)
}

func (c BlockchainConfig) GetBurningConfig() (BurningConfig, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamBurningConfig, BurningConfig{})
}

func (c BlockchainConfig) GetExtraCurrencyMintPrices() (*ExtraCurrencyMintPricesConfig, error) {
	return blockchainConfigLoadParam[ExtraCurrencyMintPricesConfig](c, ConfigParamExtraCurrencyMintPrices)
}

func (c BlockchainConfig) GetExtraCurrencyToMint() (*ExtraCurrencyToMintConfig, error) {
	return blockchainConfigLoadParam[ExtraCurrencyToMintConfig](c, ConfigParamExtraCurrencyToMint)
}

// Deprecated: use GetExtraCurrencyMintPrices.
func (c BlockchainConfig) GetMintPrices() (*ExtraCurrencyMintPricesConfig, error) {
	return c.GetExtraCurrencyMintPrices()
}

// Deprecated: use GetExtraCurrencyToMint.
func (c BlockchainConfig) GetToMint() (*ExtraCurrencyToMintConfig, error) {
	return c.GetExtraCurrencyToMint()
}

func (c BlockchainConfig) GetMandatoryParams() (*MandatoryParamsConfig, error) {
	return blockchainConfigLoadParam[MandatoryParamsConfig](c, ConfigParamMandatoryParams)
}

func (c BlockchainConfig) GetCriticalParams() (*CriticalParamsConfig, error) {
	return blockchainConfigLoadParam[CriticalParamsConfig](c, ConfigParamCriticalParams)
}

func (c BlockchainConfig) GetConfigVotingSetup() (*ConfigVotingSetup, error) {
	return blockchainConfigLoadParam[ConfigVotingSetup](c, ConfigParamConfigVotingSetup)
}

func (c BlockchainConfig) GetWorkchains() (*WorkchainsConfig, error) {
	return blockchainConfigLoadParam[WorkchainsConfig](c, ConfigParamWorkchains)
}

func (c BlockchainConfig) GetComplaintPricing() (*ComplaintPricing, error) {
	return blockchainConfigLoadParam[ComplaintPricing](c, ConfigParamComplaintPricing)
}

func (c BlockchainConfig) GetBlockCreateFees() (*BlockCreateFees, error) {
	return blockchainConfigLoadParam[BlockCreateFees](c, ConfigParamBlockCreateFees)
}

func (c BlockchainConfig) GetValidatorElectionTimings() (*ValidatorElectionTimings, error) {
	return blockchainConfigLoadParam[ValidatorElectionTimings](c, ConfigParamValidatorElectionTimings)
}

func (c BlockchainConfig) GetValidatorCountLimits() (*ValidatorCountLimits, error) {
	return blockchainConfigLoadParam[ValidatorCountLimits](c, ConfigParamValidatorCountLimits)
}

func (c BlockchainConfig) GetValidatorStakeLimits() (*ValidatorStakeLimits, error) {
	return blockchainConfigLoadParam[ValidatorStakeLimits](c, ConfigParamValidatorStakeLimits)
}

func (c BlockchainConfig) GetBlockLimits(masterchain bool) (*BlockLimits, error) {
	paramID := ConfigParamBlockLimitsBasechain
	if masterchain {
		paramID = ConfigParamBlockLimitsMasterchain
	}

	return blockchainConfigLoadParam[BlockLimits](c, paramID)
}

func (c BlockchainConfig) GetCatchainConfig() (CatchainConfig, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamCatchainConfig, CatchainConfig{
		Config: CatchainConfigV2{
			McCatchainLifetime:      defaultCatchainMCLifetime,
			ShardCatchainLifetime:   defaultCatchainShardLifetime,
			ShardValidatorsLifetime: defaultShardValidatorsLifetime,
			ShardValidatorsNum:      defaultShardValidatorsNum,
		},
	})
}

func (c BlockchainConfig) GetConsensusConfig() (ConsensusConfig, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamConsensusConfig, ConsensusConfig{})
}

func (c BlockchainConfig) GetNewConsensusConfig() (NewConsensusConfigAll, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamNewConsensusConfig, NewConsensusConfigAll{})
}

func (c BlockchainConfig) GetFundamentalSmartContractAddresses() (FundamentalSmartContractAddresses, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamFundamentalSMCAddresses, FundamentalSmartContractAddresses{
		Addresses: cell.NewDict(256),
	})
}

func (c BlockchainConfig) GetPrevValidators() (*ValidatorSetAny, error) {
	return blockchainConfigLoadParam[ValidatorSetAny](c, ConfigParamPrevValidators)
}

func (c BlockchainConfig) GetPrevTempValidators() (*ValidatorSetAny, error) {
	return blockchainConfigLoadParam[ValidatorSetAny](c, ConfigParamPrevTempValidators)
}

func (c BlockchainConfig) GetCurrentValidators() (*ValidatorSetAny, error) {
	return blockchainConfigLoadParam[ValidatorSetAny](c, ConfigParamCurrentValidators)
}

func (c BlockchainConfig) GetCurrentTempValidators() (*ValidatorSetAny, error) {
	return blockchainConfigLoadParam[ValidatorSetAny](c, ConfigParamCurrentTempValidators)
}

func (c BlockchainConfig) GetNextValidators() (*ValidatorSetAny, error) {
	return blockchainConfigLoadParam[ValidatorSetAny](c, ConfigParamNextValidators)
}

func (c BlockchainConfig) GetNextTempValidators() (*ValidatorSetAny, error) {
	return blockchainConfigLoadParam[ValidatorSetAny](c, ConfigParamNextTempValidators)
}

func (c BlockchainConfig) GetValidatorTempKeys() (*ValidatorTempKeysConfig, error) {
	return blockchainConfigLoadParam[ValidatorTempKeysConfig](c, ConfigParamValidatorTempKeys)
}

func (c BlockchainConfig) GetMisbehaviourPunishmentConfig() (*MisbehaviourPunishmentConfig, error) {
	return blockchainConfigLoadParam[MisbehaviourPunishmentConfig](c, ConfigParamMisbehaviourPunishment)
}

func (c BlockchainConfig) GetSizeLimitsConfig() (SizeLimitsConfig, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamSizeLimits, SizeLimitsConfig{
		Config: SizeLimitsConfigV2{
			MaxMsgBits:                  defaultSizeLimitsMaxMsgBits,
			MaxMsgCells:                 defaultSizeLimitsMaxMsgCells,
			MaxLibraryCells:             defaultSizeLimitsMaxLibraryCells,
			MaxVMDataDepth:              defaultSizeLimitsMaxVMDataDepth,
			MaxExtMsgSize:               defaultSizeLimitsMaxExtMsgSize,
			MaxExtMsgDepth:              defaultSizeLimitsMaxExtMsgDepth,
			MaxAccStateCells:            defaultSizeLimitsMaxAccStateCells,
			MaxMCAccStateCells:          defaultSizeLimitsMaxMCAccStateCells,
			MaxAccPublicLibraries:       defaultSizeLimitsMaxAccPublicLibraries,
			DeferOutQueueSizeLimit:      defaultSizeLimitsDeferOutQueueSizeLimit,
			MaxMsgExtraCurrencies:       defaultSizeLimitsMaxMsgExtraCurrencies,
			MaxAccFixedPrefixLength:     defaultSizeLimitsMaxAccFixedPrefixLength,
			AccStateCellsForStorageDict: defaultSizeLimitsAccStateCellsForStorageDict,
		},
	})
}

func (c BlockchainConfig) GetSuspendedAddressList() (*SuspendedAddressList, error) {
	return blockchainConfigLoadParam[SuspendedAddressList](c, ConfigParamSuspendedAddressList)
}

func (c BlockchainConfig) GetPrecompiledContractsConfig() (PrecompiledContractsConfig, error) {
	return blockchainConfigLoadParamOrDefault(c, ConfigParamPrecompiledContracts, PrecompiledContractsConfig{})
}

func (c BlockchainConfig) getAddressParam(id uint32) ([]byte, error) {
	addr, err := blockchainConfigLoadParam[ConfigParamAddress](c, id)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), addr.Address...), nil
}

func blockchainConfigLoadParam[T any](cfg BlockchainConfig, id uint32) (*T, error) {
	root, err := cfg.GetParam(id)
	return blockchainConfigLoad[T](root, err)
}

func blockchainConfigLoadParamOrDefault[T any](cfg BlockchainConfig, id uint32, def T) (T, error) {
	out, err := blockchainConfigLoadParam[T](cfg, id)
	if err != nil {
		if errors.Is(err, ErrBlockchainConfigParamAbsent) {
			return def, nil
		}
		var zero T
		return zero, err
	}
	return *out, nil
}

func blockchainConfigLoad[T any](root *cell.Cell, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("config param root is nil")
	}

	var out T
	if err = LoadFromCell(&out, root.MustBeginParse()); err != nil {
		return nil, err
	}

	return &out, nil
}
