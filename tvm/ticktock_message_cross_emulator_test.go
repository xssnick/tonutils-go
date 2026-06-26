//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTickTockTransaction(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()
	stateOnlyCode := makeTickTockStateOnlyCode(t, tickData, tockData)

	t.Run("TickSuccessMatchesReference", func(t *testing.T) {
		goRes, _, _, err := emulateTickTockForTest(t, stateOnlyCode, origData, false)
		if err != nil {
			t.Fatalf("go tick emulation failed: %v", err)
		}
		refRes, err := runReferenceTickTock(stateOnlyCode, origData, tickTockTestAddr, false, uint32(tonopsTestTime.Unix()), tickTockTestBalance, tonopsTestSeed)
		if err != nil {
			t.Fatalf("reference tick emulation failed: %v", err)
		}
		assertTickTockMatchesReference(t, goRes, refRes)
	})

	t.Run("TockSuccessMatchesReference", func(t *testing.T) {
		goRes, _, _, err := emulateTickTockForTest(t, stateOnlyCode, origData, true)
		if err != nil {
			t.Fatalf("go tock emulation failed: %v", err)
		}
		refRes, err := runReferenceTickTock(stateOnlyCode, origData, tickTockTestAddr, true, uint32(tonopsTestTime.Unix()), tickTockTestBalance, tonopsTestSeed)
		if err != nil {
			t.Fatalf("reference tock emulation failed: %v", err)
		}
		assertTickTockMatchesReference(t, goRes, refRes)
	})

	t.Run("FailureMatchesReference", func(t *testing.T) {
		failCode := makeTickTockFailureCode(t)
		goRes, _, _, err := emulateTickTockForTest(t, failCode, origData, false)
		if err != nil {
			t.Fatalf("go failing tick emulation failed: %v", err)
		}
		refRes, err := runReferenceTickTock(failCode, origData, tickTockTestAddr, false, uint32(tonopsTestTime.Unix()), tickTockTestBalance, tonopsTestSeed)
		if err != nil {
			t.Fatalf("reference failing tick emulation failed: %v", err)
		}
		assertTickTockMatchesReference(t, goRes, refRes)
	})
}

func TestTVMCrossEmulatorTickTockAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TICKTOCK_VERSION_AUDIT")

	for _, isTock := range []bool{false, true} {
		name := "tick"
		if isTock {
			name = "tock"
		}
		t.Run(name, func(t *testing.T) {
			for _, version := range versions {
				t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
					assertTickTockVersionParity(t, version, isTock, 0xAAAA, 0x1111, 0x2222)
				})
			}
		})
	}
}

func TestTVMCrossEmulatorTickTockBuildProofLibrariesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TICKTOCK_VERSION_AUDIT")

	for _, isTock := range []bool{false, true} {
		name := "tick"
		if isTock {
			name = "tock"
		}
		t.Run(name, func(t *testing.T) {
			for _, version := range versions {
				t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
					assertTickTockBuildProofLibrariesVersionParity(t, version, isTock, 0xA400, uint16(0xB400+version), uint16(0xC400+version))
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTickTockGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xAAAA), uint16(0x1111), uint16(0x2222))
		f.Add(uint8(version), true, uint16(0xAAAA), uint16(0x1111), uint16(0x2222))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, isTock bool, origTag, tickTag, tockTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertTickTockVersionParity(t, version, isTock, origTag, tickTag, tockTag)
	})
}

func FuzzTVMCrossEmulatorTickTockBuildProofGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xAAAA), uint16(0x1111), uint16(0x2222))
		f.Add(uint8(version), true, uint16(0xAAAA), uint16(0x1111), uint16(0x2222))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, isTock bool, origTag, tickTag, tockTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertTickTockBuildProofVersionParity(t, version, isTock, origTag, tickTag, tockTag)
	})
}

func FuzzTVMCrossEmulatorTickTockBuildProofLibrariesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xA400+version), uint16(0xB400+version), uint16(0xC400+version))
		f.Add(uint8(version), true, uint16(0xA500+version), uint16(0xB500+version), uint16(0xC500+version))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, isTock bool, origTag, tickTag, tockTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertTickTockBuildProofLibrariesVersionParity(t, version, isTock, origTag, tickTag, tockTag)
	})
}

func TestTVMCrossEmulatorTickTockBuildProofLibrariesGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TICKTOCK_VERSION_AUDIT")
	for _, isTock := range []bool{false, true} {
		name := "tick"
		if isTock {
			name = "tock"
		}
		t.Run(name, func(t *testing.T) {
			for _, version := range versions {
				t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
					assertTickTockBuildProofLibrariesGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, isTock, uint16(0xE400+version), uint16(0xF400+version), uint16(0xA800+version))
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTickTockBuildProofLibrariesGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint16(0xE400+version), uint16(0xF400+version), uint16(0xA800+version))
		f.Add(uint8(version), uint8(opposite), true, uint16(0xE500+version), uint16(0xF500+version), uint16(0xA900+version))
	}
	f.Add(uint8(255), uint8(0), true, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, isTock bool, origTag, tickTag, tockTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertTickTockBuildProofLibrariesGlobalVersionOverrideParity(t, version, machineVersion, isTock, origTag, tickTag, tockTag)
	})
}

func FuzzTVMCrossEmulatorTickTockBuildProofGlobalVersionBoundary(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xD600+version))
		f.Add(uint8(version), true, uint16(0xD700+version))
	}
	f.Add(uint8(255), true, uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, isTock bool, dataTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertTickTockBuildProofBoundaryVersionParity(t, version, isTock, dataTag)
	})
}

func TestTVMCrossEmulatorTickTockBuildProofGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TICKTOCK_VERSION_AUDIT")
	for _, isTock := range []bool{false, true} {
		name := "tick"
		if isTock {
			name = "tock"
		}
		t.Run(name, func(t *testing.T) {
			for _, version := range versions {
				t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
					assertTickTockBuildProofGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, isTock, uint16(0xD800+version))
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTickTockBuildProofGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint16(0xD800+version))
		f.Add(uint8(version), uint8(opposite), true, uint16(0xD900+version))
	}
	f.Add(uint8(255), uint8(0), true, uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, isTock bool, dataTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertTickTockBuildProofGlobalVersionOverrideParity(t, version, machineVersion, isTock, dataTag)
	})
}

func FuzzTVMCrossEmulatorTickTockGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, false, uint16(0xD000+version))
		f.Add(uint8(version), uint8(opposite), false, true, uint16(0xD100+version))
		f.Add(uint8(opposite), uint8(version), true, false, uint16(0xD200+version))
		f.Add(uint8(opposite), uint8(version), true, true, uint16(0xD300+version))
	}
	f.Add(uint8(255), uint8(0), true, true, uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion uint8, useConfigRoot, isTock bool, dataTag uint16) {
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		configVersion := tvmFuzzGlobalVersionByte(rawConfigVersion)
		assertTickTockFallbackVersionParity(t, machineVersion, configVersion, useConfigRoot, isTock, dataTag)
	})
}

func TestTVMCrossEmulatorTickTockGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TICKTOCK_VERSION_AUDIT")
	for _, isTock := range []bool{false, true} {
		name := "tick"
		if isTock {
			name = "tock"
		}
		t.Run(name, func(t *testing.T) {
			for _, version := range versions {
				t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
					assertTickTockGlobalVersionOverrideParity(t, version, MaxSupportedGlobalVersion-version, isTock, uint16(0xD400+version))
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTickTockGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint16(0xD400+version))
		f.Add(uint8(version), uint8(opposite), true, uint16(0xD500+version))
	}
	f.Add(uint8(255), uint8(0), true, uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, isTock bool, dataTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertTickTockGlobalVersionOverrideParity(t, version, machineVersion, isTock, dataTag)
	})
}

func assertTickTockVersionParity(t *testing.T, version int, isTock bool, origTag, tickTag, tockTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(uint64(tickTag), 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(uint64(tockTag), 16).EndCell()
	code := makeTickTockStateOnlyCode(t, tickData, tockData)
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}

	goRes, err := NewTVM().EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:        now,
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("go tick/tock emulation failed: %v", err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference tick/tock emulation failed: %v", err)
	}

	assertTickTockMatchesReference(t, goRes, refRes)
}

func assertTickTockBuildProofLibrariesVersionParity(t *testing.T, version int, isTock bool, origTag, tickTag, tockTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(uint64(tickTag), 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(uint64(tockTag), 16).EndCell()
	targetCode := makeTickTockStateOnlyCode(t, tickData, tockData)
	code := mustCrossLibraryCellForHash(t, targetCode.Hash())
	libs := mustCrossLibraryCollection(t, targetCode)
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}
	accountHash := shard.Account.Hash()

	goRes, err := NewTVM().EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:        now,
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
		BuildProof: true,
		Libraries:  []*cell.Cell{libs},
	})
	if err != nil {
		t.Fatalf("go tick/tock build-proof libraries emulation failed: %v", err)
	}
	if goRes.Proof == nil {
		t.Fatal("tick/tock execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountHash); err != nil {
		t.Fatalf("tick/tock execution proof is invalid: %v", err)
	}
	if goRes.ExitCode != 0 {
		t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.ExitCode)
	}
	wantData := tickData
	if isTock {
		wantData = tockData
	}
	if goRes.Data == nil || !bytes.Equal(goRes.Data.Hash(), wantData.Hash()) {
		t.Fatalf("go data mismatch after library execution:\ngo=%v\nwant=%s", goRes.Data, wantData.Dump())
	}
	if version >= 9 {
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refRes, err := runReferenceTickTockWithConfigRootAndLibraries(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, configRoot, libs)
	if err != nil {
		t.Fatalf("reference tick/tock build-proof libraries emulation failed: %v", err)
	}

	assertTickTockMatchesReference(t, goRes, refRes)
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
	}
}

func assertTickTockBuildProofLibrariesGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, isTock bool, origTag, tickTag, tockTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(uint64(tickTag), 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(uint64(tockTag), 16).EndCell()
	targetCode := makeTickTockStateOnlyCode(t, tickData, tockData)
	code := mustCrossLibraryCellForHash(t, targetCode.Hash())
	libs := mustCrossLibraryCollection(t, targetCode)
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}
	accountHash := shard.Account.Hash()

	goRes, err := machine.EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:              now,
		Balance:          new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:         append([]byte(nil), tonopsTestSeed...),
		GlobalVersion:    version,
		GlobalVersionSet: true,
		BuildProof:       true,
		Libraries:        []*cell.Cell{libs},
	})
	if err != nil {
		t.Fatalf("go tick/tock build-proof libraries global-version override machine_v=%d effective_v=%d is_tock=%t failed: %v", machineVersion, version, isTock, err)
	}
	if goRes.Proof == nil {
		t.Fatal("tick/tock execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountHash); err != nil {
		t.Fatalf("tick/tock execution proof is invalid: %v", err)
	}
	if goRes.ExitCode != 0 {
		t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.ExitCode)
	}
	wantData := tickData
	if isTock {
		wantData = tockData
	}
	if goRes.Data == nil || !bytes.Equal(goRes.Data.Hash(), wantData.Hash()) {
		t.Fatalf("go data mismatch after library execution:\ngo=%v\nwant=%s", goRes.Data, wantData.Dump())
	}
	if version >= 9 {
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refRes, err := runReferenceTickTockWithConfigRootAndLibraries(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, refConfigRoot, libs)
	if err != nil {
		t.Fatalf("reference tick/tock build-proof libraries global-version override machine_v=%d effective_v=%d is_tock=%t failed: %v", machineVersion, version, isTock, err)
	}

	assertTickTockMatchesReference(t, goRes, refRes)
}

func assertTickTockBuildProofVersionParity(t *testing.T, version int, isTock bool, origTag, tickTag, tockTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	tickData := cell.BeginCell().MustStoreUInt(uint64(tickTag), 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(uint64(tockTag), 16).EndCell()
	code := makeTickTockStateOnlyCode(t, tickData, tockData)
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}
	accountHash := shard.Account.Hash()

	goRes, err := NewTVM().EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:        now,
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
		BuildProof: true,
	})
	if err != nil {
		t.Fatalf("go tick/tock build-proof emulation failed: %v", err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference tick/tock build-proof emulation failed: %v", err)
	}

	assertTickTockMatchesReference(t, goRes, refRes)
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
	}
	if goRes.Proof == nil {
		t.Fatal("tick/tock execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountHash); err != nil {
		t.Fatalf("tick/tock execution proof is invalid: %v", err)
	}
}

func assertTickTockBuildProofBoundaryVersionParity(t *testing.T, version int, isTock bool, dataTag uint16) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	now := uint32(tonopsTestTime.Unix())
	code := makeTickTockGasConsumedCode(t)
	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}
	accountHash := shard.Account.Hash()

	goRes, err := NewTVM().EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:        now,
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
		BuildProof: true,
	})
	if err != nil {
		t.Fatalf("go tick/tock build-proof boundary emulation failed: %v", err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference tick/tock build-proof boundary emulation failed: %v", err)
	}

	assertTickTockMatchesReference(t, goRes, refRes)
	wantExitCode := int64(0)
	if version < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("v%d is_tock=%t exit=%d, want %d", version, isTock, goRes.ExitCode, wantExitCode)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
	}
	if goRes.Proof == nil {
		t.Fatal("tick/tock execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountHash); err != nil {
		t.Fatalf("tick/tock execution proof is invalid: %v", err)
	}
}

func assertTickTockBuildProofGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, isTock bool, dataTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	code := makeTickTockGasConsumedCode(t)
	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}
	accountHash := shard.Account.Hash()

	goRes, err := machine.EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:              now,
		Balance:          new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:         append([]byte(nil), tonopsTestSeed...),
		GlobalVersion:    version,
		GlobalVersionSet: true,
		BuildProof:       true,
	})
	if err != nil {
		t.Fatalf("go tick/tock build-proof global-version override machine_v=%d effective_v=%d is_tock=%t failed: %v", machineVersion, version, isTock, err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, refConfigRoot)
	if err != nil {
		t.Fatalf("reference tick/tock build-proof global-version override machine_v=%d effective_v=%d is_tock=%t failed: %v", machineVersion, version, isTock, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d effective_v=%d is_tock=%t: go=%d reference=%d", machineVersion, version, isTock, goRes.ExitCode, refRes.exitCode)
	}
	wantExitCode := int64(0)
	if version < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("effective v%d is_tock=%t exit=%d, want %d", version, isTock, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d effective_v=%d is_tock=%t: go=%d reference=%d", machineVersion, version, isTock, goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Accepted != refRes.accepted {
		t.Fatalf("accepted mismatch machine_v=%d effective_v=%d is_tock=%t: go=%t reference=%t", machineVersion, version, isTock, goRes.Accepted, refRes.accepted)
	}
	if goRes.Proof == nil {
		t.Fatal("tick/tock execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, accountHash); err != nil {
		t.Fatalf("tick/tock execution proof is invalid: %v", err)
	}
}

func assertTickTockFallbackVersionParity(t *testing.T, machineVersion, configVersion int, useConfigRoot, isTock bool, dataTag uint16) {
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
	code := makeTickTockGasConsumedCode(t)
	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}

	goRes, err := machine.EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:        now,
		Balance:    new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: goConfigRoot,
	})
	if err != nil {
		t.Fatalf("go tick/tock fallback machine_v=%d config_v=%d use_config=%t is_tock=%t failed: %v", machineVersion, configVersion, useConfigRoot, isTock, err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, refConfigRoot)
	if err != nil {
		t.Fatalf("reference tick/tock fallback machine_v=%d config_v=%d effective_v=%d use_config=%t is_tock=%t failed: %v", machineVersion, configVersion, effectiveVersion, useConfigRoot, isTock, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d config_v=%d effective_v=%d use_config=%t is_tock=%t: go=%d reference=%d", machineVersion, configVersion, effectiveVersion, useConfigRoot, isTock, goRes.ExitCode, refRes.exitCode)
	}
	wantExitCode := int64(0)
	if effectiveVersion < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("effective v%d is_tock=%t exit=%d, want %d", effectiveVersion, isTock, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d config_v=%d effective_v=%d use_config=%t is_tock=%t: go=%d reference=%d", machineVersion, configVersion, effectiveVersion, useConfigRoot, isTock, goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Accepted != refRes.accepted {
		t.Fatalf("accepted mismatch machine_v=%d config_v=%d effective_v=%d use_config=%t is_tock=%t: go=%t reference=%t", machineVersion, configVersion, effectiveVersion, useConfigRoot, isTock, goRes.Accepted, refRes.accepted)
	}

	if !useConfigRoot {
		return
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
	}
}

func assertTickTockGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, isTock bool, dataTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		t.Fatalf("with global version %d: %v", machineVersion, err)
	}

	now := uint32(tonopsTestTime.Unix())
	code := makeTickTockGasConsumedCode(t)
	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}

	goRes, err := machine.EmulateTickTockTransaction(shard, isTock, TransactionEmulationConfig{
		Now:              now,
		Balance:          new(big.Int).SetUint64(tickTockTestBalance),
		RandSeed:         append([]byte(nil), tonopsTestSeed...),
		GlobalVersion:    version,
		GlobalVersionSet: true,
	})
	if err != nil {
		t.Fatalf("go tick/tock global-version override machine_v=%d effective_v=%d is_tock=%t failed: %v", machineVersion, version, isTock, err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, isTock, now, tickTockTestBalance, tonopsTestSeed, refConfigRoot)
	if err != nil {
		t.Fatalf("reference tick/tock global-version override machine_v=%d effective_v=%d is_tock=%t failed: %v", machineVersion, version, isTock, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d effective_v=%d is_tock=%t: go=%d reference=%d", machineVersion, version, isTock, goRes.ExitCode, refRes.exitCode)
	}
	wantExitCode := int64(0)
	if version < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("effective v%d is_tock=%t exit=%d, want %d", version, isTock, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d effective_v=%d is_tock=%t: go=%d reference=%d", machineVersion, version, isTock, goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Accepted != refRes.accepted {
		t.Fatalf("accepted mismatch machine_v=%d effective_v=%d is_tock=%t: go=%t reference=%t", machineVersion, version, isTock, goRes.Accepted, refRes.accepted)
	}
}

func assertTickTockMatchesReference(t *testing.T, goRes *TransactionExecutionResult, refRes *referenceTickTockResult) {
	t.Helper()

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Accepted != refRes.accepted {
		t.Fatalf("accepted mismatch: go=%t reference=%t", goRes.Accepted, refRes.accepted)
	}
	if goRes.Code == nil || refRes.code == nil || !bytes.Equal(goRes.Code.Hash(), refRes.code.Hash()) {
		t.Fatalf("code mismatch:\ngo=%v\nreference=%v", goRes.Code, refRes.code)
	}
	if !bytes.Equal(goRes.Data.Hash(), refRes.data.Hash()) {
		t.Fatalf("data mismatch:\ngo=%s\nreference=%s", goRes.Data.Dump(), refRes.data.Dump())
	}

	switch {
	case goRes.Actions == nil && refRes.actions == nil:
	case goRes.Actions == nil || refRes.actions == nil:
		t.Fatalf("actions presence mismatch: go=%v reference=%v", goRes.Actions != nil, refRes.actions != nil)
	default:
		if !bytes.Equal(goRes.Actions.Hash(), refRes.actions.Hash()) {
			t.Fatalf("actions mismatch:\ngo=%s\nreference=%s", goRes.Actions.Dump(), refRes.actions.Dump())
		}
	}
}
