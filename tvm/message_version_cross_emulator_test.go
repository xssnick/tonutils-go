//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func messageVersionCrossEmulatorVersions(t *testing.T) []int {
	t.Helper()

	return crossEmulatorVersionAuditVersions(t, "TVM_MESSAGE_VERSION_AUDIT")
}

func TestTVMCrossEmulatorMessageVersionAuditShardSelection(t *testing.T) {
	t.Setenv("TVM_MESSAGE_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_MESSAGE_VERSION_AUDIT_SHARD", "")

	all := messageVersionCrossEmulatorVersions(t)
	wantLen := MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != MinSupportedGlobalVersion || all[len(all)-1] != MaxSupportedGlobalVersion {
		t.Fatalf("default version selection = %v, want range %d..%d", all, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
	}

	t.Setenv("TVM_MESSAGE_VERSION_AUDIT_SHARDS", "4")
	t.Setenv("TVM_MESSAGE_VERSION_AUDIT_SHARD", "1")
	got := messageVersionCrossEmulatorVersions(t)
	want := []int{1, 5, 9, 13}
	if len(got) != len(want) {
		t.Fatalf("sharded version selection = %v, want %v", got, want)
	}
	for i, version := range want {
		if got[i] != version {
			t.Fatalf("sharded version selection = %v, want %v", got, want)
		}
	}
}

func TestTVMCrossEmulatorDirectMessageAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("external_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageVersionParity(t, version, false, uint16(0xAAAA), uint16(0xB0E0+version), uint16(0xCAFE))
		})

		t.Run("internal_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageVersionParity(t, version, true, uint16(0xAAAA), uint16(0xD0E0+version), uint16(0xCAFE))
		})
	}
}

func TestTVMCrossEmulatorDirectMessageBuildProofLibrariesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("external_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageBuildProofLibrariesVersionParity(t, version, false, uint16(0xA400), uint16(0xB400+version), uint16(0xC400))
		})

		t.Run("internal_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageBuildProofLibrariesVersionParity(t, version, true, uint16(0xA500), uint16(0xB500+version), uint16(0xC500))
		})
	}
}

func FuzzTVMCrossEmulatorDirectMessageGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xAAAA), uint16(0xB0E0+version), uint16(0xCAFE))
		f.Add(uint8(version), true, uint16(0xAAAA), uint16(0xD0E0+version), uint16(0xCAFE))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, internal bool, origTag uint16, newTag uint16, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertDirectMessageVersionParity(t, version, internal, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorDirectMessageGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, false, uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version))
		f.Add(uint8(version), uint8(opposite), false, true, uint16(0xA100+version), uint16(0xB100+version), uint16(0xC100+version))
		f.Add(uint8(opposite), uint8(version), true, false, uint16(0xA200+version), uint16(0xB200+version), uint16(0xC200+version))
		f.Add(uint8(opposite), uint8(version), true, true, uint16(0xA300+version), uint16(0xB300+version), uint16(0xC300+version))
	}
	f.Add(uint8(255), uint8(0), true, true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion uint8, useConfigRoot, internal bool, origTag, newTag, bodyTag uint16) {
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		configVersion := tvmFuzzGlobalVersionByte(rawConfigVersion)
		assertDirectMessageFallbackVersionParity(t, machineVersion, configVersion, useConfigRoot, internal, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorDirectMessageGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("external_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, false, uint16(0xAC00+version), uint16(0xBC00+version), uint16(0xCC00+version))
		})
		t.Run("internal_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, true, uint16(0xAD00+version), uint16(0xBD00+version), uint16(0xCD00+version))
		})
	}
}

func FuzzTVMCrossEmulatorDirectMessageGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint16(0xAC00+version), uint16(0xBC00+version), uint16(0xCC00+version))
		f.Add(uint8(version), uint8(opposite), true, uint16(0xAD00+version), uint16(0xBD00+version), uint16(0xCD00+version))
	}
	f.Add(uint8(255), uint8(0), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, internal bool, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertDirectMessageGlobalVersionOverrideParity(t, version, machineVersion, internal, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorDirectMessageBuildProofGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xAAAA), uint16(0xB0E0+version), uint16(0xCAFE))
		f.Add(uint8(version), true, uint16(0xAAAA), uint16(0xD0E0+version), uint16(0xCAFE))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, internal bool, origTag uint16, newTag uint16, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertDirectMessageBuildProofVersionParity(t, version, internal, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorDirectMessageBuildProofGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("external_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageBuildProofGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, false, uint16(0xB800+version), uint16(0xC800+version), uint16(0xD800+version))
		})
		t.Run("internal_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageBuildProofGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, true, uint16(0xB900+version), uint16(0xC900+version), uint16(0xD900+version))
		})
	}
}

func FuzzTVMCrossEmulatorDirectMessageBuildProofGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint16(0xB800+version), uint16(0xC800+version), uint16(0xD800+version))
		f.Add(uint8(version), uint8(opposite), true, uint16(0xB900+version), uint16(0xC900+version), uint16(0xD900+version))
	}
	f.Add(uint8(255), uint8(0), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, internal bool, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertDirectMessageBuildProofGlobalVersionOverrideParity(t, version, machineVersion, internal, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorDirectMessageBuildProofLibrariesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xA400+version), uint16(0xB400+version), uint16(0xC400+version))
		f.Add(uint8(version), true, uint16(0xA500+version), uint16(0xB500+version), uint16(0xC500+version))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, internal bool, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertDirectMessageBuildProofLibrariesVersionParity(t, version, internal, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorDirectMessageBuildProofLibrariesGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("external_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageBuildProofLibrariesGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, false, uint16(0xE400+version), uint16(0xF400+version), uint16(0xA800+version))
		})
		t.Run("internal_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertDirectMessageBuildProofLibrariesGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, true, uint16(0xE500+version), uint16(0xF500+version), uint16(0xA900+version))
		})
	}
}

func FuzzTVMCrossEmulatorDirectMessageBuildProofLibrariesGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint16(0xE400+version), uint16(0xF400+version), uint16(0xA800+version))
		f.Add(uint8(version), uint8(opposite), true, uint16(0xE500+version), uint16(0xF500+version), uint16(0xA900+version))
	}
	f.Add(uint8(255), uint8(0), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, internal bool, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertDirectMessageBuildProofLibrariesGlobalVersionOverrideParity(t, version, machineVersion, internal, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorDirectMessageBuildProofGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, false, false, uint16(0xA400+version), uint16(0xB400+version), uint16(0xC400+version))
		f.Add(uint8(version), uint8(opposite), false, true, true, uint16(0xA500+version), uint16(0xB500+version), uint16(0xC500+version))
		f.Add(uint8(opposite), uint8(version), true, false, true, uint16(0xA600+version), uint16(0xB600+version), uint16(0xC600+version))
		f.Add(uint8(opposite), uint8(version), true, true, false, uint16(0xA700+version), uint16(0xB700+version), uint16(0xC700+version))
	}
	f.Add(uint8(255), uint8(0), true, true, false, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion uint8, useConfigRoot, internal, explicitAddress bool, origTag, newTag, bodyTag uint16) {
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		configVersion := tvmFuzzGlobalVersionByte(rawConfigVersion)
		assertDirectMessageBuildProofFallbackVersionParity(t, machineVersion, configVersion, useConfigRoot, internal, explicitAddress, origTag, newTag, bodyTag)
	})
}

func assertDirectMessageVersionParity(t *testing.T, version int, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	code := makeDirectMessageInMsgParamsCode(t, newData, !internal)

	var goRes *MessageExecutionResult
	var err error
	if internal {
		goRes, err = NewTVM().EmulateInternalMessage(code, origData, body, internalMessageTestAmount, EmulateInternalMessageConfig{
			Address:    tonopsTestAddr,
			Now:        now,
			Balance:    balance,
			RandSeed:   append([]byte(nil), referenceDefaultWalletSendSeed...),
			ConfigRoot: configRoot,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:   DefaultInternalMessageGasMax,
				Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
			}),
		})
	} else {
		goRes, err = NewTVM().EmulateExternalMessage(code, origData, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, EmulateExternalMessageConfig{
			Address:    tonopsTestAddr,
			Now:        now,
			Balance:    balance,
			RandSeed:   append([]byte(nil), referenceDefaultWalletSendSeed...),
			ConfigRoot: configRoot,
			Gas: vmcore.NewGas(vmcore.GasConfig{
				Max:    DefaultExternalMessageGasMax,
				Credit: DefaultExternalMessageGasCredit,
			}),
		})
	}
	if err != nil {
		t.Fatalf("go direct message emulation failed: %v", err)
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("reference direct message emulation failed: %v", err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func assertDirectMessageFallbackVersionParity(t *testing.T, machineVersion, configVersion int, useConfigRoot, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	effectiveVersion := machineVersion
	var goConfigRoot *cell.Cell
	if useConfigRoot {
		effectiveVersion = configVersion
		goConfigRoot = referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(configVersion))
	}
	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(effectiveVersion))

	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	code := makeDirectMessageInMsgParamsCode(t, newData, !internal)

	cfg := MessageEmulationConfig{
		Address:    tonopsTestAddr,
		Now:        now,
		Balance:    balance,
		RandSeed:   append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot: goConfigRoot,
	}

	var goRes *MessageExecutionResult
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = machine.EmulateInternalMessage(code, origData, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = machine.EmulateExternalMessage(code, origData, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message fallback emulation machine_v=%d config_v=%d use_config=%t internal=%t failed: %v", machineVersion, configVersion, useConfigRoot, internal, err)
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: refConfigRoot,
	})
	if err != nil {
		t.Fatalf("reference direct message fallback emulation machine_v=%d config_v=%d use_config=%t internal=%t failed: %v", machineVersion, configVersion, useConfigRoot, internal, err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func assertDirectMessageGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	code := makeDirectMessageInMsgParamsCode(t, newData, !internal)

	cfg := MessageEmulationConfig{
		Address:    tonopsTestAddr,
		Now:        now,
		Balance:    balance,
		RandSeed:   append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot: refConfigRoot,
	}

	var goRes *MessageExecutionResult
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = machine.EmulateInternalMessage(code, origData, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = machine.EmulateExternalMessage(code, origData, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message global-version override machine_v=%d effective_v=%d internal=%t failed: %v", machineVersion, version, internal, err)
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: refConfigRoot,
	})
	if err != nil {
		t.Fatalf("reference direct message global-version override machine_v=%d effective_v=%d internal=%t failed: %v", machineVersion, version, internal, err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func assertDirectMessageBuildProofLibrariesVersionParity(t *testing.T, version int, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	targetCode := makeDirectMessageSetDataCode(t, newData, !internal)
	code := mustCrossLibraryCellForHash(t, targetCode.Hash())
	libs := mustCrossLibraryCollection(t, targetCode)
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     tonopsTestAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(referenceDefaultWalletSendBalance),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: origData,
			},
		},
	})

	cfg := MessageEmulationConfig{
		Now:         now,
		Balance:     balance,
		RandSeed:    append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot:  configRoot,
		BuildProof:  true,
		AccountRoot: accountRoot,
		Libraries:   []*cell.Cell{libs},
	}

	var goRes *MessageExecutionResult
	var err error
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = NewTVM().EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = NewTVM().EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message build-proof libraries emulation failed: %v", err)
	}
	if goRes.Proof == nil {
		t.Fatal("message execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message execution proof is invalid: %v", err)
	}
	if version >= 9 {
		if goRes.ExitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.ExitCode)
		}
		if goRes.Data == nil || !bytes.Equal(goRes.Data.Hash(), newData.Hash()) {
			t.Fatalf("go data mismatch after library execution:\ngo=%v\nwant=%s", goRes.Data, newData.Dump())
		}
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
		libs:       libs,
	})
	if err != nil {
		t.Fatalf("reference direct message build-proof libraries emulation failed: %v", err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func assertDirectMessageBuildProofLibrariesGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	targetCode := makeDirectMessageSetDataCode(t, newData, !internal)
	code := mustCrossLibraryCellForHash(t, targetCode.Hash())
	libs := mustCrossLibraryCollection(t, targetCode)
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     tonopsTestAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(referenceDefaultWalletSendBalance),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: origData,
			},
		},
	})

	cfg := MessageEmulationConfig{
		Now:         now,
		Balance:     balance,
		RandSeed:    append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot:  refConfigRoot,
		BuildProof:  true,
		AccountRoot: accountRoot,
		Libraries:   []*cell.Cell{libs},
	}

	var goRes *MessageExecutionResult
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = machine.EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = machine.EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message build-proof libraries global-version override machine_v=%d effective_v=%d internal=%t failed: %v", machineVersion, version, internal, err)
	}
	if goRes.Proof == nil {
		t.Fatal("message execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message execution proof is invalid: %v", err)
	}
	if version >= 9 {
		if goRes.ExitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.ExitCode)
		}
		if goRes.Data == nil || !bytes.Equal(goRes.Data.Hash(), newData.Hash()) {
			t.Fatalf("go data mismatch after library execution:\ngo=%v\nwant=%s", goRes.Data, newData.Dump())
		}
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: refConfigRoot,
		libs:       libs,
	})
	if err != nil {
		t.Fatalf("reference direct message build-proof libraries global-version override machine_v=%d effective_v=%d internal=%t failed: %v", machineVersion, version, internal, err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func makeDirectMessageSetDataCode(t *testing.T, newData *cell.Cell, external bool) *cell.Cell {
	t.Helper()

	var builders []*cell.Builder
	if external {
		builders = append(builders, funcsop.ACCEPT().Serialize())
	}
	builders = append(builders,
		stackop.PUSHREF(newData).Serialize(),
		execop.POPCTR(4).Serialize(),
	)
	return codeFromBuilders(t, builders...)
}

func assertDirectMessageBuildProofFallbackVersionParity(t *testing.T, machineVersion, configVersion int, useConfigRoot, internal, explicitAddress bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	effectiveVersion := machineVersion
	var goConfigRoot *cell.Cell
	if useConfigRoot {
		effectiveVersion = configVersion
		goConfigRoot = referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(configVersion))
	}
	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(effectiveVersion))

	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeDirectMessageInMsgParamsCode(t, newData, !internal)
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     tonopsTestAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(referenceDefaultWalletSendBalance),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: origData,
			},
		},
	})

	cfg := MessageEmulationConfig{
		Now:         now,
		Balance:     new(big.Int).SetUint64(referenceDefaultWalletSendBalance),
		RandSeed:    append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot:  goConfigRoot,
		BuildProof:  true,
		AccountRoot: accountRoot,
	}
	if explicitAddress {
		cfg.Address = tonopsTestAddr
	}

	var goRes *MessageExecutionResult
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = machine.EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = machine.EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message build-proof fallback emulation machine_v=%d config_v=%d use_config=%t internal=%t explicit_addr=%t failed: %v", machineVersion, configVersion, useConfigRoot, internal, explicitAddress, err)
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: refConfigRoot,
	})
	if err != nil {
		t.Fatalf("reference direct message build-proof fallback emulation machine_v=%d config_v=%d use_config=%t internal=%t explicit_addr=%t failed: %v", machineVersion, configVersion, useConfigRoot, internal, explicitAddress, err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
	if goRes.Proof == nil {
		t.Fatal("message execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message execution proof is invalid: %v", err)
	}
}

func assertDirectMessageBuildProofVersionParity(t *testing.T, version int, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	code := makeDirectMessageInMsgParamsCode(t, newData, !internal)
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     tonopsTestAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(referenceDefaultWalletSendBalance),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: origData,
			},
		},
	})

	cfg := MessageEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         now,
		Balance:     balance,
		RandSeed:    append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot:  configRoot,
		BuildProof:  true,
		AccountRoot: accountRoot,
	}

	var goRes *MessageExecutionResult
	var err error
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = NewTVM().EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = NewTVM().EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message build-proof emulation failed: %v", err)
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("reference direct message build-proof emulation failed: %v", err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
	if goRes.Proof == nil {
		t.Fatal("message execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message execution proof is invalid: %v", err)
	}
}

func assertDirectMessageBuildProofGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, internal bool, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	code := makeDirectMessageInMsgParamsCode(t, newData, !internal)
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     tonopsTestAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(referenceDefaultWalletSendBalance),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: origData,
			},
		},
	})

	cfg := MessageEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         now,
		Balance:     balance,
		RandSeed:    append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot:  refConfigRoot,
		BuildProof:  true,
		AccountRoot: accountRoot,
	}

	var goRes *MessageExecutionResult
	if internal {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		})
		goRes, err = machine.EmulateInternalMessage(nil, nil, body, internalMessageTestAmount, cfg)
	} else {
		cfg.Gas = vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultExternalMessageGasMax,
			Credit: DefaultExternalMessageGasCredit,
		})
		goRes, err = machine.EmulateExternalMessage(nil, nil, &tlb.ExternalMessage{
			DstAddr: tonopsTestAddr,
			Body:    body,
		}, cfg)
	}
	if err != nil {
		t.Fatalf("go direct message build-proof global-version override machine_v=%d effective_v=%d internal=%t failed: %v", machineVersion, version, internal, err)
	}

	amount := uint64(0)
	if internal {
		amount = internalMessageTestAmount
	}
	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, amount, internal, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: refConfigRoot,
	})
	if err != nil {
		t.Fatalf("reference direct message build-proof global-version override machine_v=%d effective_v=%d internal=%t failed: %v", machineVersion, version, internal, err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
	if goRes.Proof == nil {
		t.Fatal("message execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("message execution proof is invalid: %v", err)
	}
}

func TestTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertCheckExternalMessageAcceptedVersionParity(t, version, uint16(0xAAAA), uint16(0xC0DE+version), uint16(0xCAFE))
		})
	}
}

func FuzzTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(0xAAAA), uint16(0xC0DE+version), uint16(0xCAFE))
	}
	f.Add(uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertCheckExternalMessageAcceptedVersionParity(t, version, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint8(0), uint16(0xA800+version), uint16(0xB800+version), uint16(0xC800+version))
		f.Add(uint8(version), uint8(opposite), false, uint8(1), uint16(0xA900+version), uint16(0xB900+version), uint16(0xC900+version))
		f.Add(uint8(opposite), uint8(version), true, uint8(0), uint16(0xAA00+version), uint16(0xBA00+version), uint16(0xCA00+version))
		f.Add(uint8(opposite), uint8(version), true, uint8(1), uint16(0xAB00+version), uint16(0xBB00+version), uint16(0xCB00+version))
	}
	f.Add(uint8(255), uint8(0), true, uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion uint8, useConfigRoot bool, rawProgram uint8, origTag, newTag, bodyTag uint16) {
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		configVersion := tvmFuzzGlobalVersionByte(rawConfigVersion)
		assertCheckExternalMessageAcceptedFallbackVersionParity(t, machineVersion, configVersion, useConfigRoot, rawProgram, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range messageVersionCrossEmulatorVersions(t) {
		t.Run("gasconsumed_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertCheckExternalMessageAcceptedGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, 0, uint16(0xAE00+version), uint16(0xBE00+version), uint16(0xCE00+version))
		})
		t.Run("inmsgparams_global_v"+strconv.Itoa(version), func(t *testing.T) {
			assertCheckExternalMessageAcceptedGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, 1, uint16(0xAF00+version), uint16(0xBF00+version), uint16(0xCF00+version))
		})
	}
}

func FuzzTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), uint8(0), uint16(0xAE00+version), uint16(0xBE00+version), uint16(0xCE00+version))
		f.Add(uint8(version), uint8(opposite), uint8(1), uint16(0xAF00+version), uint16(0xBF00+version), uint16(0xCF00+version))
	}
	f.Add(uint8(255), uint8(0), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion, rawProgram uint8, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertCheckExternalMessageAcceptedGlobalVersionOverrideParity(t, version, machineVersion, rawProgram, origTag, newTag, bodyTag)
	})
}

func assertCheckExternalMessageAcceptedVersionParity(t *testing.T, version int, origTag, newTag, bodyTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	code := makeDirectMessageInMsgParamsCode(t, newData, true)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	var account tlb.AccountState
	if err = tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse account: %v", err)
	}

	accepted, err := NewTVM().CheckExternalMessageAccepted(shard, &account, msgCell, msg, CheckExternalMessageAcceptedConfig{
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("check external message accepted failed: %v", err)
	}

	refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	refAccepted := refErr == nil
	if accepted != refAccepted {
		t.Fatalf("accepted mismatch: go=%t reference=%t reference_err=%v", accepted, refAccepted, refErr)
	}
	if accepted != (version >= 11) {
		t.Fatalf("v%d accepted = %t, want %t", version, accepted, version >= 11)
	}
	if refAccepted && refRes.exitCode != 0 {
		t.Fatalf("reference exit code = %d, want 0", refRes.exitCode)
	}
}

func assertCheckExternalMessageAcceptedFallbackVersionParity(t *testing.T, machineVersion, configVersion int, useConfigRoot bool, rawProgram uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	effectiveVersion := machineVersion
	var goConfigRoot *cell.Cell
	if useConfigRoot {
		effectiveVersion = configVersion
		goConfigRoot = referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(configVersion))
	}
	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(effectiveVersion))

	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeCheckExternalAcceptedGasConsumedCode(t)
	wantAccepted := effectiveVersion >= 4
	if rawProgram%2 != 0 {
		code = makeDirectMessageInMsgParamsCode(t, newData, true)
		wantAccepted = effectiveVersion >= 11
	}

	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	account := mustParseTransactionTestAccount(t, shard)

	accepted, err := machine.CheckExternalMessageAccepted(shard, account, msgCell, msg, CheckExternalMessageAcceptedConfig{
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  goConfigRoot,
	})
	if err != nil {
		t.Fatalf("check external message accepted machine_v=%d config_v=%d use_config=%t program=%d failed: %v", machineVersion, configVersion, useConfigRoot, rawProgram%2, err)
	}

	refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, refConfigRoot)
	refAccepted := refErr == nil
	if accepted != refAccepted {
		t.Fatalf("accepted mismatch machine_v=%d config_v=%d effective_v=%d use_config=%t program=%d: go=%t reference=%t reference_err=%v", machineVersion, configVersion, effectiveVersion, useConfigRoot, rawProgram%2, accepted, refAccepted, refErr)
	}
	if accepted != wantAccepted {
		t.Fatalf("effective v%d program=%d accepted=%t, want %t", effectiveVersion, rawProgram%2, accepted, wantAccepted)
	}
	if refAccepted && refRes.exitCode != 0 {
		t.Fatalf("reference exit code machine_v=%d config_v=%d effective_v=%d program=%d = %d, want 0", machineVersion, configVersion, effectiveVersion, rawProgram%2, refRes.exitCode)
	}
}

func assertCheckExternalMessageAcceptedGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, rawProgram uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeCheckExternalAcceptedGasConsumedCode(t)
	wantAccepted := version >= 4
	if rawProgram%2 != 0 {
		code = makeDirectMessageInMsgParamsCode(t, newData, true)
		wantAccepted = version >= 11
	}

	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	account := mustParseTransactionTestAccount(t, shard)

	accepted, err := machine.CheckExternalMessageAccepted(shard, account, msgCell, msg, CheckExternalMessageAcceptedConfig{
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  refConfigRoot,
	})
	if err != nil {
		t.Fatalf("check external message accepted machine_v=%d effective_v=%d program=%d failed: %v", machineVersion, version, rawProgram%2, err)
	}

	refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, refConfigRoot)
	refAccepted := refErr == nil
	if accepted != refAccepted {
		t.Fatalf("accepted mismatch machine_v=%d effective_v=%d program=%d: go=%t reference=%t reference_err=%v", machineVersion, version, rawProgram%2, accepted, refAccepted, refErr)
	}
	if accepted != wantAccepted {
		t.Fatalf("effective v%d program=%d accepted=%t, want %t", version, rawProgram%2, accepted, wantAccepted)
	}
	if refAccepted && refRes.exitCode != 0 {
		t.Fatalf("reference exit code machine_v=%d effective_v=%d program=%d = %d, want 0", machineVersion, version, rawProgram%2, refRes.exitCode)
	}
}
