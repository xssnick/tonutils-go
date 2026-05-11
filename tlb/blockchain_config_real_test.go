package tlb

import (
	_ "embed"
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

//go:embed testdata/blockchain_config_mainnet.boc
var blockchainConfigMainnetBOC []byte

func TestBlockchainConfigMainnetFixture(t *testing.T) {
	root, err := cell.FromBOC(blockchainConfigMainnetBOC)
	if err != nil {
		t.Fatalf("FromBOC failed: %v", err)
	}

	cfg := BlockchainConfig{Root: root}

	for _, id := range []uint32{
		ConfigParamConfigAddress,
		ConfigParamElectorAddress,
		ConfigParamMinterAddress,
		ConfigParamFeeCollectorAddress,
		ConfigParamDNSRootAddress,
		ConfigParamBurningConfig,
		ConfigParamExtraCurrencyMintPrices,
		ConfigParamExtraCurrencyToMint,
		ConfigParamGlobalVersion,
		ConfigParamMandatoryParams,
		ConfigParamCriticalParams,
		ConfigParamConfigVotingSetup,
		ConfigParamWorkchains,
		ConfigParamComplaintPricing,
		ConfigParamBlockCreateFees,
		ConfigParamValidatorElectionTimings,
		ConfigParamValidatorCountLimits,
		ConfigParamValidatorStakeLimits,
		ConfigParamStoragePrices,
		ConfigParamGlobalID,
		ConfigParamGasPricesMasterchain,
		ConfigParamGasPricesBasechain,
		ConfigParamBlockLimitsMasterchain,
		ConfigParamBlockLimitsBasechain,
		ConfigParamMsgForwardPricesMasterchain,
		ConfigParamMsgForwardPricesBasechain,
		ConfigParamCatchainConfig,
		ConfigParamConsensusConfig,
		ConfigParamNewConsensusConfig,
		ConfigParamFundamentalSMCAddresses,
		ConfigParamPrevValidators,
		ConfigParamPrevTempValidators,
		ConfigParamCurrentValidators,
		ConfigParamCurrentTempValidators,
		ConfigParamNextValidators,
		ConfigParamNextTempValidators,
		ConfigParamValidatorTempKeys,
		ConfigParamMisbehaviourPunishment,
		ConfigParamSizeLimits,
		ConfigParamSuspendedAddressList,
		ConfigParamPrecompiledContracts,
	} {
		param, err := cfg.GetParam(id)
		if err != nil {
			if errors.Is(err, ErrBlockchainConfigParamAbsent) && testBlockchainConfigFixtureOptionalParam(id) {
				continue
			}

			t.Fatalf("GetParam(%d) failed: %v", id, err)
		}

		if param == nil {
			t.Fatalf("GetParam(%d) returned nil cell", id)
		}
	}

	testBlockchainConfigFixtureMustGet(t, "GetConfigAddress", func() error {
		_, err := cfg.GetConfigAddress()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetElectorAddress", func() error {
		_, err := cfg.GetElectorAddress()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetMinterAddress", func() error {
		_, err := cfg.GetMinterAddress()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetFeeCollectorAddress", func() error {
		_, err := cfg.GetFeeCollectorAddress()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetDNSRootAddress", func() error {
		_, err := cfg.GetDNSRootAddress()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetBurningConfig", func() error {
		_, err := cfg.GetBurningConfig()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetExtraCurrencyMintPrices", func() error {
		_, err := cfg.GetExtraCurrencyMintPrices()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetExtraCurrencyToMint", func() error {
		_, err := cfg.GetExtraCurrencyToMint()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetGlobalVersion", func() error {
		_, err := cfg.GetGlobalVersion()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetMandatoryParams", func() error {
		_, err := cfg.GetMandatoryParams()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetCriticalParams", func() error {
		_, err := cfg.GetCriticalParams()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetConfigVotingSetup", func() error {
		_, err := cfg.GetConfigVotingSetup()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetWorkchains", func() error {
		_, err := cfg.GetWorkchains()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetComplaintPricing", func() error {
		_, err := cfg.GetComplaintPricing()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetBlockCreateFees", func() error {
		_, err := cfg.GetBlockCreateFees()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetValidatorElectionTimings", func() error {
		_, err := cfg.GetValidatorElectionTimings()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetValidatorCountLimits", func() error {
		_, err := cfg.GetValidatorCountLimits()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetValidatorStakeLimits", func() error {
		_, err := cfg.GetValidatorStakeLimits()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetStoragePrices", func() error {
		_, err := cfg.GetStoragePrices(0)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetGlobalID", func() error {
		_, err := cfg.GetGlobalID()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetGasPrices(masterchain)", func() error {
		_, err := cfg.GetGasPrices(true)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetGasPrices(basechain)", func() error {
		_, err := cfg.GetGasPrices(false)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetBlockLimits(masterchain)", func() error {
		_, err := cfg.GetBlockLimits(true)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetBlockLimits(basechain)", func() error {
		_, err := cfg.GetBlockLimits(false)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetMsgForwardPrices(masterchain)", func() error {
		_, err := cfg.GetMsgForwardPrices(true)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetMsgForwardPrices(basechain)", func() error {
		_, err := cfg.GetMsgForwardPrices(false)
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetCatchainConfig", func() error {
		_, err := cfg.GetCatchainConfig()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetConsensusConfig", func() error {
		_, err := cfg.GetConsensusConfig()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetNewConsensusConfig", func() error {
		_, err := cfg.GetNewConsensusConfig()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetFundamentalSmartContractAddresses", func() error {
		_, err := cfg.GetFundamentalSmartContractAddresses()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetPrevValidators", func() error {
		_, err := cfg.GetPrevValidators()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetPrevTempValidators", func() error {
		_, err := cfg.GetPrevTempValidators()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetCurrentValidators", func() error {
		_, err := cfg.GetCurrentValidators()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetCurrentTempValidators", func() error {
		_, err := cfg.GetCurrentTempValidators()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetNextValidators", func() error {
		_, err := cfg.GetNextValidators()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetNextTempValidators", func() error {
		_, err := cfg.GetNextTempValidators()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetValidatorTempKeys", func() error {
		_, err := cfg.GetValidatorTempKeys()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetMisbehaviourPunishmentConfig", func() error {
		_, err := cfg.GetMisbehaviourPunishmentConfig()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetSizeLimitsConfig", func() error {
		_, err := cfg.GetSizeLimitsConfig()
		return err
	})

	testBlockchainConfigFixtureMaybeAbsent(t, "GetSuspendedAddressList", func() error {
		_, err := cfg.GetSuspendedAddressList()
		return err
	})

	testBlockchainConfigFixtureMustGet(t, "GetPrecompiledContractsConfig", func() error {
		_, err := cfg.GetPrecompiledContractsConfig()
		return err
	})
}

func testBlockchainConfigFixtureOptionalParam(id uint32) bool {
	switch id {
	case ConfigParamMinterAddress,
		ConfigParamFeeCollectorAddress,
		ConfigParamExtraCurrencyMintPrices,
		ConfigParamExtraCurrencyToMint,
		ConfigParamNewConsensusConfig,
		ConfigParamFundamentalSMCAddresses,
		ConfigParamPrevValidators,
		ConfigParamPrevTempValidators,
		ConfigParamCurrentTempValidators,
		ConfigParamNextValidators,
		ConfigParamNextTempValidators,
		ConfigParamValidatorTempKeys,
		ConfigParamMisbehaviourPunishment,
		ConfigParamSizeLimits,
		ConfigParamSuspendedAddressList,
		ConfigParamPrecompiledContracts:
		return true
	}

	return false
}

func testBlockchainConfigFixtureMustGet(t *testing.T, name string, fn func() error) {
	t.Helper()

	if err := fn(); err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
}

func testBlockchainConfigFixtureMaybeAbsent(t *testing.T, name string, fn func() error) {
	t.Helper()

	if err := fn(); err != nil && !errors.Is(err, ErrBlockchainConfigParamAbsent) {
		t.Fatalf("%s failed: %v", name, err)
	}
}
