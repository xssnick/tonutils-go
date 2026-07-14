package tlb

import "github.com/xssnick/tonutils-go/tvm/cell"

const (
	ConfigParamConfigAddress               uint32 = 0
	ConfigParamElectorAddress              uint32 = 1
	ConfigParamMinterAddress               uint32 = 2
	ConfigParamFeeCollectorAddress         uint32 = 3
	ConfigParamDNSRootAddress              uint32 = 4
	ConfigParamBurningConfig               uint32 = 5
	ConfigParamExtraCurrencyMintPrices     uint32 = 6
	ConfigParamExtraCurrencyToMint         uint32 = 7
	ConfigParamGlobalVersion               uint32 = 8
	ConfigParamMandatoryParams             uint32 = 9
	ConfigParamCriticalParams              uint32 = 10
	ConfigParamConfigVotingSetup           uint32 = 11
	ConfigParamWorkchains                  uint32 = 12
	ConfigParamComplaintPricing            uint32 = 13
	ConfigParamBlockCreateFees             uint32 = 14
	ConfigParamValidatorElectionTimings    uint32 = 15
	ConfigParamValidatorCountLimits        uint32 = 16
	ConfigParamValidatorStakeLimits        uint32 = 17
	ConfigParamStoragePrices               uint32 = 18
	ConfigParamGlobalID                    uint32 = 19
	ConfigParamGasPricesMasterchain        uint32 = 20
	ConfigParamGasPricesBasechain          uint32 = 21
	ConfigParamBlockLimitsMasterchain      uint32 = 22
	ConfigParamBlockLimitsBasechain        uint32 = 23
	ConfigParamMsgForwardPricesMasterchain uint32 = 24
	ConfigParamMsgForwardPricesBasechain   uint32 = 25
	ConfigParamCatchainConfig              uint32 = 28
	ConfigParamConsensusConfig             uint32 = 29
	ConfigParamNewConsensusConfig          uint32 = 30
	ConfigParamFundamentalSMCAddresses     uint32 = 31
	ConfigParamPrevValidators              uint32 = 32
	ConfigParamPrevTempValidators          uint32 = 33
	ConfigParamCurrentValidators           uint32 = 34
	ConfigParamCurrentTempValidators       uint32 = 35
	ConfigParamNextValidators              uint32 = 36
	ConfigParamNextTempValidators          uint32 = 37
	ConfigParamValidatorTempKeys           uint32 = 39
	ConfigParamMisbehaviourPunishment      uint32 = 40
	ConfigParamSizeLimits                  uint32 = 43
	ConfigParamSuspendedAddressList        uint32 = 44
	ConfigParamPrecompiledContracts        uint32 = 45
)

const (
	// Deprecated: use ConfigParamExtraCurrencyMintPrices.
	ConfigParamMintPrices = ConfigParamExtraCurrencyMintPrices

	// Deprecated: use ConfigParamExtraCurrencyToMint.
	ConfigParamToMint = ConfigParamExtraCurrencyToMint
)

const (
	defaultCatchainMCLifetime                    = 200
	defaultCatchainShardLifetime                 = 200
	defaultShardValidatorsLifetime               = 3000
	defaultShardValidatorsNum                    = 7
	defaultSizeLimitsMaxMsgBits                  = 1 << 21
	defaultSizeLimitsMaxMsgCells                 = 1 << 13
	defaultSizeLimitsMaxLibraryCells             = 1000
	defaultSizeLimitsMaxVMDataDepth              = 512
	defaultSizeLimitsMaxExtMsgSize               = 65535
	defaultSizeLimitsMaxExtMsgDepth              = 512
	defaultSizeLimitsMaxAccStateCells            = 1 << 16
	defaultSizeLimitsMaxMCAccStateCells          = 1 << 11
	defaultSizeLimitsMaxAccPublicLibraries       = 256
	defaultSizeLimitsDeferOutQueueSizeLimit      = 256
	defaultSizeLimitsMaxMsgExtraCurrencies       = 2
	defaultSizeLimitsMaxAccFixedPrefixLength     = 8
	defaultSizeLimitsAccStateCellsForStorageDict = 26
)

func init() {
	Register(BlockLimitsV1{})
	Register(BlockLimitsV2{})
	Register(NewConsensusConfigSimplex{})
	Register(NewConsensusConfigSimplexV2{})
	Register(SizeLimitsConfigV1{})
	Register(SizeLimitsConfigV2{})
	Register(WorkchainDescrV1{})
	Register(WorkchainDescrV2{})
	Register(WorkchainFormatBasic{})
	Register(WorkchainFormatExtended{})
}

type ConfigParamAddress struct {
	Address []byte `tlb:"bits 256"`
}

type BurningConfig struct {
	_             Magic  `tlb:"#01"`
	BlackholeAddr []byte `tlb:"maybe bits 256"`
	FeeBurnNum    uint32 `tlb:"## 32"`
	FeeBurnDenom  uint32 `tlb:"## 32"`
}

type ExtraCurrencyMintPricesConfig struct {
	MintNewPrice Coins `tlb:"."`
	MintAddPrice Coins `tlb:"."`
}

type ExtraCurrencyToMintConfig struct {
	ToMint *cell.Dictionary `tlb:"dict 32"`
}

// Deprecated: use ExtraCurrencyMintPricesConfig.
type MintPricesConfig = ExtraCurrencyMintPricesConfig

// Deprecated: use ExtraCurrencyToMintConfig.
type ToMintConfig = ExtraCurrencyToMintConfig

type MandatoryParamsConfig struct {
	Params *cell.Dictionary `tlb:"dict inline 32"`
}

type CriticalParamsConfig struct {
	Params *cell.Dictionary `tlb:"dict inline 32"`
}

type ConfigProposalSetup struct {
	_            Magic  `tlb:"#36"`
	MinTotRounds uint8  `tlb:"## 8"`
	MaxTotRounds uint8  `tlb:"## 8"`
	MinWins      uint8  `tlb:"## 8"`
	MaxLosses    uint8  `tlb:"## 8"`
	MinStoreSec  uint32 `tlb:"## 32"`
	MaxStoreSec  uint32 `tlb:"## 32"`
	BitPrice     uint32 `tlb:"## 32"`
	CellPrice    uint32 `tlb:"## 32"`
}

type ConfigVotingSetup struct {
	_              Magic                `tlb:"#91"`
	NormalParams   *ConfigProposalSetup `tlb:"^"`
	CriticalParams *ConfigProposalSetup `tlb:"^"`
}

type WorkchainsConfig struct {
	Workchains *cell.Dictionary `tlb:"dict 32"`
}

type WorkchainDescr struct {
	Descr any `tlb:"[WorkchainDescrV1,WorkchainDescrV2]"`
}

type WorkchainDescrV1 struct {
	_                    Magic `tlb:"#a6"`
	WorkchainDescrFields `tlb:"."`
}

type WorkchainDescrV2 struct {
	_                    Magic `tlb:"#a7"`
	WorkchainDescrFields `tlb:"."`
}

type WorkchainDescrFields struct {
	EnabledSince      uint32 `tlb:"## 32"`
	ActualMinSplit    uint8  `tlb:"## 8"`
	MinSplit          uint8  `tlb:"## 8"`
	MaxSplit          uint8  `tlb:"## 8"`
	Basic             bool   `tlb:"bool"`
	Active            bool   `tlb:"bool"`
	AcceptMsgs        bool   `tlb:"bool"`
	Flags             uint16 `tlb:"## 13"`
	ZeroStateRootHash []byte `tlb:"bits 256"`
	ZeroStateFileHash []byte `tlb:"bits 256"`
	Version           uint32 `tlb:"## 32"`
	Format            any    `tlb:"[WorkchainFormatBasic,WorkchainFormatExtended]"`
}

type WorkchainFormatBasic struct {
	_         Magic  `tlb:"#1"`
	VMVersion int32  `tlb:"## 32"`
	VMMode    uint64 `tlb:"## 64"`
}

type WorkchainFormatExtended struct {
	_               Magic  `tlb:"#0"`
	MinAddrLen      uint16 `tlb:"## 12"`
	MaxAddrLen      uint16 `tlb:"## 12"`
	AddrLenStep     uint16 `tlb:"## 12"`
	WorkchainTypeID uint32 `tlb:"## 32"`
}

type ComplaintPricing struct {
	_         Magic `tlb:"#1a"`
	Deposit   Coins `tlb:"."`
	BitPrice  Coins `tlb:"."`
	CellPrice Coins `tlb:"."`
}

type BlockCreateFees struct {
	_                   Magic `tlb:"#6b"`
	MasterchainBlockFee Coins `tlb:"."`
	BasechainBlockFee   Coins `tlb:"."`
}

type ValidatorElectionTimings struct {
	ValidatorsElectedFor uint32 `tlb:"## 32"`
	ElectionsStartBefore uint32 `tlb:"## 32"`
	ElectionsEndBefore   uint32 `tlb:"## 32"`
	StakeHeldFor         uint32 `tlb:"## 32"`
}

type ValidatorCountLimits struct {
	MaxValidators     uint16 `tlb:"## 16"`
	MaxMainValidators uint16 `tlb:"## 16"`
	MinValidators     uint16 `tlb:"## 16"`
}

type ValidatorStakeLimits struct {
	MinStake       Coins  `tlb:"."`
	MaxStake       Coins  `tlb:"."`
	MinTotalStake  Coins  `tlb:"."`
	MaxStakeFactor uint32 `tlb:"## 32"`
}

type StoragePricesConfig struct {
	Prices *cell.Dictionary `tlb:"dict inline 32"`
}

type GlobalIDConfig struct {
	GlobalID int32 `tlb:"## 32"`
}

type ParamLimits struct {
	_         Magic  `tlb:"#c3"`
	Underload uint32 `tlb:"## 32"`
	SoftLimit uint32 `tlb:"## 32"`
	HardLimit uint32 `tlb:"## 32"`
}

type ImportedMsgQueueLimits struct {
	_        Magic  `tlb:"#d3"`
	MaxBytes uint32 `tlb:"## 32"`
	MaxMsgs  uint32 `tlb:"## 32"`
}

type BlockLimits struct {
	Limits any `tlb:"[BlockLimitsV1,BlockLimitsV2]"`
}

type BlockLimitsV1 struct {
	_       Magic       `tlb:"#5d"`
	Bytes   ParamLimits `tlb:"."`
	Gas     ParamLimits `tlb:"."`
	LTDelta ParamLimits `tlb:"."`
}

type BlockLimitsV2 struct {
	_                Magic                  `tlb:"#5e"`
	Bytes            ParamLimits            `tlb:"."`
	Gas              ParamLimits            `tlb:"."`
	LTDelta          ParamLimits            `tlb:"."`
	CollatedData     ParamLimits            `tlb:"."`
	ImportedMsgQueue ImportedMsgQueueLimits `tlb:"."`
}

type NewConsensusConfigAll struct {
	_           Magic `tlb:"#10"`
	Masterchain any   `tlb:"maybe ^ [NewConsensusConfigSimplex,NewConsensusConfigSimplexV2]"`
	Shard       any   `tlb:"maybe ^ [NewConsensusConfigSimplex,NewConsensusConfigSimplexV2]"`
}

type NewConsensusConfigSimplex struct {
	_                     Magic  `tlb:"#21"`
	Flags                 uint8  `tlb:"## 7"`
	UseQUIC               bool   `tlb:"bool"`
	TargetRateMS          uint32 `tlb:"## 32"`
	SlotsPerLeaderWindow  uint32 `tlb:"## 32"`
	FirstBlockTimeoutMS   uint32 `tlb:"## 32"`
	MaxLeaderWindowDesync uint32 `tlb:"## 32"`
}

type NewConsensusConfigSimplexV2 struct {
	_                    Magic            `tlb:"#22"`
	Flags                uint8            `tlb:"## 7"`
	UseQUIC              bool             `tlb:"bool"`
	SlotsPerLeaderWindow uint32           `tlb:"## 32"`
	NoncriticalParams    *cell.Dictionary `tlb:"dict 8"`
}

type FundamentalSmartContractAddresses struct {
	Addresses *cell.Dictionary `tlb:"dict 256"`
}

type ValidatorTempKeysConfig struct {
	Keys *cell.Dictionary `tlb:"dict 256"`
}

type MisbehaviourPunishmentConfig struct {
	_                        Magic  `tlb:"#01"`
	DefaultFlatFine          Coins  `tlb:"."`
	DefaultProportionalFine  uint32 `tlb:"## 32"`
	SeverityFlatMult         uint16 `tlb:"## 16"`
	SeverityProportionalMult uint16 `tlb:"## 16"`
	UnpunishableInterval     uint16 `tlb:"## 16"`
	LongInterval             uint16 `tlb:"## 16"`
	LongFlatMult             uint16 `tlb:"## 16"`
	LongProportionalMult     uint16 `tlb:"## 16"`
	MediumInterval           uint16 `tlb:"## 16"`
	MediumFlatMult           uint16 `tlb:"## 16"`
	MediumProportionalMult   uint16 `tlb:"## 16"`
}

type SizeLimitsConfig struct {
	Config any `tlb:"[SizeLimitsConfigV1,SizeLimitsConfigV2]"`
}

type SizeLimitsConfigV1 struct {
	_               Magic  `tlb:"#01"`
	MaxMsgBits      uint32 `tlb:"## 32"`
	MaxMsgCells     uint32 `tlb:"## 32"`
	MaxLibraryCells uint32 `tlb:"## 32"`
	MaxVMDataDepth  uint16 `tlb:"## 16"`
	MaxExtMsgSize   uint32 `tlb:"## 32"`
	MaxExtMsgDepth  uint16 `tlb:"## 16"`
}

type SizeLimitsConfigV2 struct {
	_                           Magic   `tlb:"#02"`
	MaxMsgBits                  uint32  `tlb:"## 32"`
	MaxMsgCells                 uint32  `tlb:"## 32"`
	MaxLibraryCells             uint32  `tlb:"## 32"`
	MaxVMDataDepth              uint16  `tlb:"## 16"`
	MaxExtMsgSize               uint32  `tlb:"## 32"`
	MaxExtMsgDepth              uint16  `tlb:"## 16"`
	MaxAccStateCells            uint32  `tlb:"## 32"`
	MaxMCAccStateCells          uint32  `tlb:"## 32"`
	MaxAccPublicLibraries       uint32  `tlb:"## 32"`
	DeferOutQueueSizeLimit      uint32  `tlb:"## 32"`
	MaxMsgExtraCurrencies       uint32  `tlb:"## 32"`
	MaxAccFixedPrefixLength     uint8   `tlb:"## 8"`
	AccStateCellsForStorageDict uint32  `tlb:"## 32"`
	MaxTransactionLibraryLoads  *uint32 `tlb:"maybe ## 32"`
}

type SuspendedAddressList struct {
	_              Magic            `tlb:"#00"`
	Addresses      *cell.Dictionary `tlb:"dict 288"`
	SuspendedUntil uint32           `tlb:"## 32"`
}

type PrecompiledSmc struct {
	_        Magic  `tlb:"#b0"`
	GasUsage uint64 `tlb:"## 64"`
}

type PrecompiledContractsConfig struct {
	_    Magic            `tlb:"#c0"`
	List *cell.Dictionary `tlb:"dict 256"`
}
