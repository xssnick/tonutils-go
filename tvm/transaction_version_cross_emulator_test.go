//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func transactionVersionCrossEmulatorVersions(t *testing.T) []uint32 {
	t.Helper()

	versions := crossEmulatorVersionAuditVersions(t, "TVM_TRANSACTION_VERSION_AUDIT")
	out := make([]uint32, 0, len(versions))
	for _, version := range versions {
		out = append(out, uint32(version))
	}
	return out
}

func transactionV15LibraryReferenceSkip(version uint32) string {
	if version >= 15 {
		return "bundled reference emulator predates upstream transaction v15 library action restrictions"
	}
	return ""
}

func TestTVMCrossEmulatorTransactionVersionAuditShardSelection(t *testing.T) {
	t.Setenv("TVM_TRANSACTION_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_TRANSACTION_VERSION_AUDIT_SHARD", "")

	all := transactionVersionCrossEmulatorVersions(t)
	wantLen := vm.MaxSupportedGlobalVersion - 0 + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != uint32(0) || all[len(all)-1] != uint32(vm.MaxSupportedGlobalVersion) {
		t.Fatalf("default version selection = %v, want range %d..%d", all, 0, vm.MaxSupportedGlobalVersion)
	}

	t.Setenv("TVM_TRANSACTION_VERSION_AUDIT_SHARDS", "4")
	t.Setenv("TVM_TRANSACTION_VERSION_AUDIT_SHARD", "2")
	got := transactionVersionCrossEmulatorVersions(t)
	want := []uint32{2, 6, 10, 14}
	if len(got) != len(want) {
		t.Fatalf("sharded version selection = %v, want %v", got, want)
	}
	for i, version := range want {
		if got[i] != version {
			t.Fatalf("sharded version selection = %v, want %v", got, want)
		}
	}
}

func TestTVMCrossEmulatorTransactionAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionBasicSuccessVersionParity(t, version, 0xAAAA, 0xBEEF, 0xCAFE)
		})
	}
}

func TestTVMCrossEmulatorTransactionBuildProofLibrariesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionBuildProofLibrariesVersionParity(t, version, 0xA400, uint16(0xB400+version), 0xC400)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionBasicSuccessGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(0xAAAA), uint16(0xBEEF), uint16(0xCAFE))
	}
	f.Add(uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionBasicSuccessVersionParity(t, version, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorTransactionBuildProofBasicSuccessGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(0xAAAA), uint16(0xBEEF), uint16(0xCAFE))
	}
	f.Add(uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionBuildProofBasicSuccessVersionParity(t, version, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorTransactionBuildProofLibrariesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(0xA400+version), uint16(0xB400+version), uint16(0xC400+version))
	}
	f.Add(uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionBuildProofLibrariesVersionParity(t, version, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorTransactionBuildProofLibrariesGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionBuildProofLibrariesGlobalVersionOverrideParity(t, int(version), vm.MaxSupportedGlobalVersion-int(version), uint16(0xE400+version), uint16(0xF400+version), uint16(0xA800+version))
		})
	}
}

func FuzzTVMCrossEmulatorTransactionBuildProofLibrariesGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		opposite := vm.MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), uint16(0xE400+version), uint16(0xF400+version), uint16(0xA800+version))
	}
	f.Add(uint8(255), uint8(0), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertTransactionBuildProofLibrariesGlobalVersionOverrideParity(t, version, machineVersion, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorTransactionBuildProofComputeGlobalVersionBoundaries(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint16(0xD400+version), uint16(0xE400+version), uint16(0xF400+version))
		f.Add(uint8(version), uint8(1), uint16(0xD500+version), uint16(0xE500+version), uint16(0xF500+version))
	}
	f.Add(uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawProgram uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionBuildProofComputeBoundaryVersionParity(t, version, rawProgram, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorTransactionBuildProofComputeGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run(fmt.Sprintf("gasconsumed_global_v%d", version), func(t *testing.T) {
			assertTransactionBuildProofComputeGlobalVersionOverrideParity(t, int(version), vm.MaxSupportedGlobalVersion-int(version), 0, uint16(0xD800+version), uint16(0xE800+version), uint16(0xF800+version))
		})
		t.Run(fmt.Sprintf("inmsgparams_global_v%d", version), func(t *testing.T) {
			assertTransactionBuildProofComputeGlobalVersionOverrideParity(t, int(version), vm.MaxSupportedGlobalVersion-int(version), 1, uint16(0xD900+version), uint16(0xE900+version), uint16(0xF900+version))
		})
	}
}

func FuzzTVMCrossEmulatorTransactionBuildProofComputeGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		opposite := vm.MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), uint8(0), uint16(0xD800+version), uint16(0xE800+version), uint16(0xF800+version))
		f.Add(uint8(version), uint8(opposite), uint8(1), uint16(0xD900+version), uint16(0xE900+version), uint16(0xF900+version))
	}
	f.Add(uint8(255), uint8(0), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion, rawProgram uint8, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertTransactionBuildProofComputeGlobalVersionOverrideParity(t, version, machineVersion, rawProgram, origTag, newTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorTransactionComputeGlobalVersionFallbackAndConfigOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		opposite := vm.MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), false, uint8(0), uint16(0xAC00+version), uint16(0xBC00+version), uint16(0xCC00+version))
		f.Add(uint8(version), uint8(opposite), false, uint8(1), uint16(0xAD00+version), uint16(0xBD00+version), uint16(0xCD00+version))
		f.Add(uint8(opposite), uint8(version), true, uint8(0), uint16(0xAE00+version), uint16(0xBE00+version), uint16(0xCE00+version))
		f.Add(uint8(opposite), uint8(version), true, uint8(1), uint16(0xAF00+version), uint16(0xBF00+version), uint16(0xCF00+version))
	}
	f.Add(uint8(255), uint8(0), true, uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawMachineVersion, rawConfigVersion uint8, useConfigRoot bool, rawProgram uint8, origTag, newTag, bodyTag uint16) {
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		configVersion := tvmFuzzGlobalVersionByte(rawConfigVersion)
		assertTransactionComputeFallbackVersionParity(t, machineVersion, configVersion, useConfigRoot, rawProgram, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorTransactionComputeGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run(fmt.Sprintf("gasconsumed_global_v%d", version), func(t *testing.T) {
			assertTransactionComputeGlobalVersionOverrideParity(t, int(version), vm.MaxSupportedGlobalVersion-int(version), 0, uint16(0xB000+version), uint16(0xC000+version), uint16(0xD000+version))
		})
		t.Run(fmt.Sprintf("inmsgparams_global_v%d", version), func(t *testing.T) {
			assertTransactionComputeGlobalVersionOverrideParity(t, int(version), vm.MaxSupportedGlobalVersion-int(version), 1, uint16(0xB100+version), uint16(0xC100+version), uint16(0xD100+version))
		})
	}
}

func FuzzTVMCrossEmulatorTransactionComputeGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		opposite := vm.MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), uint8(0), uint16(0xB000+version), uint16(0xC000+version), uint16(0xD000+version))
		f.Add(uint8(version), uint8(opposite), uint8(1), uint16(0xB100+version), uint16(0xC100+version), uint16(0xD100+version))
	}
	f.Add(uint8(255), uint8(0), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion, rawProgram uint8, origTag, newTag, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertTransactionComputeGlobalVersionOverrideParity(t, version, machineVersion, rawProgram, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorTransactionC7OptionsGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		for _, rawCase := range []uint8{0, 1} {
			t.Run(fmt.Sprintf("global_v%d_%s", version, transactionC7OptionsCaseName(rawCase)), func(t *testing.T) {
				assertTransactionC7OptionsVersionParity(t, version, rawCase, 0xAAAA, 0xCAFE, 0xBEEF)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTransactionC7OptionsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint16(0xA000+version), uint16(0xC000+version), uint16(0xB000+version))
		f.Add(uint8(version), uint8(1), uint16(0xA100+version), uint16(0xC100+version), uint16(0xB100+version))
	}
	f.Add(uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, origTag, bodyTag, libraryTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))

		assertTransactionC7OptionsVersionParity(t, version, rawCase, origTag, bodyTag, libraryTag)
	})
}

func TestTVMCrossEmulatorTransactionMalformedActionsGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		for _, rawCase := range []uint8{0, 1, 2} {
			t.Run(fmt.Sprintf("global_v%d_%s", version, transactionMalformedActionsCaseName(rawCase)), func(t *testing.T) {
				assertTransactionMalformedActionsVersionParity(t, version, rawCase, 0xAAAA, 0xBEEF, 0xCAFE)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTransactionMalformedActionsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version))
		f.Add(uint8(version), uint8(1), uint16(0xA100+version), uint16(0xB100+version), uint16(0xC100+version))
		f.Add(uint8(version), uint8(2), uint16(0xA200+version), uint16(0xB200+version), uint16(0xC200+version))
	}
	f.Add(uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))

		assertTransactionMalformedActionsVersionParity(t, version, rawCase, origTag, newTag, bodyTag)
	})
}

func TestTVMCrossEmulatorTransactionRawReserveActionsGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		for _, rawCase := range []uint8{0, 1, 2, 3} {
			t.Run(fmt.Sprintf("global_v%d_%s", version, transactionRawReserveActionsCaseName(rawCase)), func(t *testing.T) {
				reserveAmount, outAmount := transactionRawReserveDefaultAmounts(rawCase)
				assertTransactionRawReserveActionsVersionParity(t, version, rawCase, 0xAAAA, 0xBEEF, 0xCAFE, reserveAmount, outAmount)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTransactionRawReserveActionsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version), uint64(10_800_000_000), uint64(1000))
		f.Add(uint8(version), uint8(1), uint16(0xA100+version), uint16(0xB100+version), uint16(0xC100+version), uint64(20_000_000_000), uint64(1000))
		f.Add(uint8(version), uint8(2), uint16(0xA200+version), uint16(0xB200+version), uint16(0xC200+version), uint64(5_000_000), uint64(0))
		f.Add(uint8(version), uint8(3), uint16(0xA300+version), uint16(0xB300+version), uint16(0xC300+version), uint64(20_000_000_000), uint64(0))
	}
	f.Add(uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234), uint64(0), uint64(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, origTag, newTag, bodyTag uint16, rawReserveAmount, rawOutAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		reserveAmount := rawReserveAmount % 25_000_000_001
		outAmount := rawOutAmount % 2_000_000_001

		assertTransactionRawReserveActionsVersionParity(t, version, rawCase, origTag, newTag, bodyTag, reserveAmount, outAmount)
	})
}

func FuzzTVMCrossEmulatorTransactionChangeLibraryActionsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		for rawCase := uint8(0); rawCase < 5; rawCase++ {
			f.Add(uint8(version), rawCase, uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version), uint16(0xD000+version))
		}
	}
	f.Add(uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234), uint16(0xeeee))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, origTag, newTag, bodyTag, libTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionChangeLibraryActionsVersionParity(t, version, rawCase, origTag, newTag, bodyTag, libTag)
	})
}

func assertTransactionBasicSuccessVersionParity(t *testing.T, version uint32, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, transactionActionPhaseExpectation{success: true, valid: true})
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, transactionActionPhaseExpectation{success: true, valid: true})
}

func assertTransactionC7OptionsVersionParity(t *testing.T, version uint32, rawCase uint8, origTag, bodyTag, libraryTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	now := uint32(tonopsTestTime.Unix())
	prevBlocks := tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333))
	libraryTarget := cell.BeginCell().MustStoreUInt(uint64(libraryTag), 16).EndCell()
	libraryCollection := mustCrossLibraryCollection(t, libraryTarget)
	code, opts := transactionC7OptionsVersionCode(t, rawCase, libraryTarget, libraryCollection, prevBlocks)
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	var goLibs []*cell.Cell
	if opts.libs != nil {
		goLibs = []*cell.Cell{opts.libs}
	}
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
		PrevBlocks:  opts.prevBlocks,
		Libraries:   goLibs,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot, opts)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionC7OptionsVersionCode(t *testing.T, rawCase uint8, libraryTarget, libraryCollection *cell.Cell, prevBlocks tuple.Tuple) (*cell.Cell, referenceTransactionOptions) {
	t.Helper()

	txCode := func(builders ...*cell.Builder) *cell.Cell {
		all := make([]*cell.Builder, 0, 5+len(builders))
		for range 5 {
			all = append(all, stackop.DROP().Serialize())
		}
		all = append(all, builders...)
		return codeFromBuilders(t, all...)
	}

	if rawCase%2 == 0 {
		return txCode(
			funcsop.PREVMCBLOCKS_100().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(16).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		), referenceTransactionOptions{prevBlocks: prevBlocks}
	}

	libraryRef := mustCrossLibraryCellForHash(t, libraryTarget.Hash())
	return txCode(
		stackop.PUSHREF(libraryRef).Serialize(),
		cellsliceop.XLOADQ().Serialize(),
		stackop.DROP().Serialize(),
		cellsliceop.HASHCU().Serialize(),
		cellsliceop.NEWC().Serialize(),
		cellsliceop.STU(256).Serialize(),
		cellsliceop.ENDC().Serialize(),
		execop.POPCTR(4).Serialize(),
	), referenceTransactionOptions{libs: libraryCollection}
}

func transactionC7OptionsCaseName(rawCase uint8) string {
	if rawCase%2 == 0 {
		return "prev_blocks_info"
	}
	return "library_collection"
}

func assertTransactionMalformedActionsVersionParity(t *testing.T, version uint32, rawCase uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	actions, wantAction := transactionMalformedActionsCase(t, version, rawCase, newData)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
}

func transactionMalformedActionsCase(t *testing.T, version uint32, rawCase uint8, newData *cell.Cell) (*cell.Cell, transactionActionPhaseExpectation) {
	t.Helper()

	switch rawCase % 3 {
	case 0:
		return buildTransactionRepeatedSetCodeActions(t, newData, 256), transactionActionPhaseExpectation{resultCode: 33}
	case 1:
		if version >= 8 {
			return buildTransactionMalformedSendAction(2), transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
		}
		return buildTransactionMalformedSendAction(2), transactionActionPhaseExpectation{resultCode: 34}
	default:
		return buildTransactionMalformedSendAction(16), transactionActionPhaseExpectation{resultCode: 34}
	}
}

func transactionMalformedActionsCaseName(rawCase uint8) string {
	switch rawCase % 3 {
	case 0:
		return "too_many_actions_256"
	case 1:
		return "malformed_send_mode2"
	default:
		return "malformed_send_mode16"
	}
}

func assertTransactionRawReserveActionsVersionParity(t *testing.T, version uint32, rawCase uint8, origTag, newTag, bodyTag uint16, reserveAmount, outAmount uint64) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	code, bounce := transactionRawReserveActionsCode(t, rawCase, reserveAmount, outAmount, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      bounce,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionRawReserveActionsCode(t *testing.T, rawCase uint8, reserveAmount, outAmount uint64, newData *cell.Cell) (*cell.Cell, bool) {
	t.Helper()

	switch rawCase % 4 {
	case 0:
		outMsg := buildTransactionOutboundInternalCell(t, outAmount)
		return makeTransactionInternalReserveSendCode(t, reserveAmount, 0, outMsg, newData, 1), false
	case 1:
		outMsg := buildTransactionOutboundInternalCell(t, outAmount)
		return makeTransactionInternalReserveSendCode(t, reserveAmount, 2, outMsg, newData, 1), false
	case 2:
		return makeTransactionInternalReserveOnlyCode(t, reserveAmount, 5, newData), false
	default:
		return makeTransactionInternalReserveOnlyCode(t, reserveAmount, 28, newData), true
	}
}

func transactionRawReserveDefaultAmounts(rawCase uint8) (uint64, uint64) {
	switch rawCase % 4 {
	case 0:
		return 10_800_000_000, 1000
	case 1:
		return 20_000_000_000, 1000
	case 2:
		return 5_000_000, 0
	default:
		return 20_000_000_000, 0
	}
}

func transactionRawReserveActionsCaseName(rawCase uint8) string {
	switch rawCase % 4 {
	case 0:
		return "rawreserve_limits_later_send"
	case 1:
		return "rawreserve_mode2_clamps"
	case 2:
		return "rawreserve_mode5_keep_amount"
	default:
		return "rawreserve_mode12_invalid_bounce"
	}
}

func assertTransactionChangeLibraryActionsVersionParity(t *testing.T, version uint32, rawCase uint8, origTag, newTag, bodyTag, libTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	lib := cell.BeginCell().MustStoreUInt(uint64(libTag), 16).EndCell()
	action, want := transactionChangeLibraryActionCase(t, version, rawCase, lib)
	actions := buildTransactionActionList(t, action)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	if reason := transactionV15LibraryReferenceSkip(version); reason != "" {
		t.Skip(reason)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionChangeLibraryActionCase(t *testing.T, version uint32, rawCase uint8, lib *cell.Cell) (tlb.ActionChangeLibrary, transactionActionPhaseExpectation) {
	t.Helper()

	action := tlb.ActionChangeLibrary{}
	want := transactionActionPhaseExpectation{success: true, valid: true}
	switch rawCase % 5 {
	case 0:
		action = tlb.ActionChangeLibrary{Mode: 17, LibRef: tlb.LibRefRef{Library: lib}}
	case 1:
		action = tlb.ActionChangeLibrary{Mode: 18, LibRef: tlb.LibRefRef{Library: lib}}
	case 2:
		action = tlb.ActionChangeLibrary{Mode: 19, LibRef: tlb.LibRefRef{Library: lib}}
		want = transactionActionPhaseExpectation{valid: true, resultCode: 34}
	case 3:
		action = tlb.ActionChangeLibrary{Mode: 17, LibRef: tlb.LibRefHash{LibHash: lib.Hash()}}
		want = transactionActionPhaseExpectation{valid: true, resultCode: 41}
	default:
		action = tlb.ActionChangeLibrary{Mode: 16, LibRef: tlb.LibRefHash{LibHash: lib.Hash()}}
	}
	if version < 4 {
		want = transactionActionPhaseExpectation{valid: true, resultCode: 34}
	}
	if version >= 15 && rawCase%5 != 2 {
		want = transactionActionPhaseExpectation{valid: true, resultCode: 46}
	}
	return action, want
}

func assertTransactionBuildProofLibrariesVersionParity(t *testing.T, version uint32, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	targetCode := makeTransactionInternalSuccessCode(t, newData)
	code := mustCrossLibraryCellForHash(t, targetCode.Hash())
	libs := mustCrossLibraryCollection(t, targetCode)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
		BuildProof:  true,
		Libraries:   []*cell.Cell{libs},
	})
	if err != nil {
		t.Fatalf("go transaction build-proof libraries emulation failed: %v", err)
	}
	if goRes.Proof == nil {
		t.Fatal("transaction execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("transaction execution proof is invalid: %v", err)
	}
	if goRes.ExitCode != 0 {
		t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.ExitCode)
	}
	if testResultAccountState(goRes) == nil || testResultAccountState(goRes).StateInit == nil || testResultAccountState(goRes).StateInit.Data == nil {
		t.Fatalf("go transaction account state has no data")
	}
	if !bytes.Equal(testResultAccountState(goRes).StateInit.Data.Hash(), newData.Hash()) {
		t.Fatalf("go data mismatch after library execution:\ngo=%s\nwant=%s", testResultAccountState(goRes).StateInit.Data.Dump(), newData.Dump())
	}
	if version >= 9 {
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot, referenceTransactionOptions{
		libs: libs,
	})
	if err != nil {
		t.Fatalf("reference transaction build-proof libraries emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, transactionActionPhaseExpectation{success: true, valid: true})
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, transactionActionPhaseExpectation{success: true, valid: true})
}

func assertTransactionBuildProofLibrariesGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine := NewTVM()

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	targetCode := makeTransactionInternalSuccessCode(t, newData)
	code := mustCrossLibraryCellForHash(t, targetCode.Hash())
	libs := mustCrossLibraryCollection(t, targetCode)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(machine, shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  refConfigRoot,
		BuildProof:  true,
		Libraries:   []*cell.Cell{libs},
	})
	if err != nil {
		t.Fatalf("go transaction build-proof libraries global-version override machine_v=%d effective_v=%d failed: %v", machineVersion, version, err)
	}
	if goRes.Proof == nil {
		t.Fatal("transaction execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("transaction execution proof is invalid: %v", err)
	}
	if goRes.ExitCode != 0 {
		t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.ExitCode)
	}
	if testResultAccountState(goRes) == nil || testResultAccountState(goRes).StateInit == nil || testResultAccountState(goRes).StateInit.Data == nil {
		t.Fatalf("go transaction account state has no data")
	}
	if !bytes.Equal(testResultAccountState(goRes).StateInit.Data.Hash(), newData.Hash()) {
		t.Fatalf("go data mismatch after library execution:\ngo=%s\nwant=%s", testResultAccountState(goRes).StateInit.Data.Dump(), newData.Dump())
	}
	if version >= 9 {
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, refConfigRoot, referenceTransactionOptions{
		libs: libs,
	})
	if err != nil {
		t.Fatalf("reference transaction build-proof libraries global-version override machine_v=%d effective_v=%d failed: %v", machineVersion, version, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d effective_v=%d: go=%d reference=%d", machineVersion, version, goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d effective_v=%d: go=%d reference=%d", machineVersion, version, goRes.GasUsed, refRes.gasUsed)
	}
	wantData := []byte{byte(newTag >> 8), byte(newTag)}
	assertShardAccountDataBytes(t, "go", goRes.NextAccount.ShardAccountCell(), wantData)
	assertShardAccountDataBytes(t, "reference", refRes.shardCell, wantData)

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, transactionActionPhaseExpectation{success: true, valid: true})
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, transactionActionPhaseExpectation{success: true, valid: true})
}

func assertTransactionBuildProofBasicSuccessVersionParity(t *testing.T, version uint32, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
		BuildProof:  true,
	})
	if err != nil {
		t.Fatalf("go transaction build-proof emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction build-proof emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
	if goRes.Proof == nil {
		t.Fatal("transaction execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("transaction execution proof is invalid: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, transactionActionPhaseExpectation{success: true, valid: true})
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, transactionActionPhaseExpectation{success: true, valid: true})
}

func assertTransactionBuildProofComputeBoundaryVersionParity(t *testing.T, version uint32, rawProgram uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeMessageGasConsumedCode(t)
	wantExitCode := int64(0)
	if version < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if rawProgram%2 != 0 {
		code = makeDirectMessageInMsgParamsCode(t, newData, false)
		if version < 11 {
			wantExitCode = vmerr.CodeInvalidOpcode
		} else {
			wantExitCode = 0
		}
	}

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
		BuildProof:  true,
	})
	if err != nil {
		t.Fatalf("go transaction build-proof boundary v%d program=%d failed: %v", version, rawProgram%2, err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction build-proof boundary v%d program=%d failed: %v", version, rawProgram%2, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch v%d program=%d: go=%d reference=%d", version, rawProgram%2, goRes.ExitCode, refRes.exitCode)
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("v%d program=%d exit=%d, want %d", version, rawProgram%2, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch v%d program=%d: go=%d reference=%d", version, rawProgram%2, goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
	if goRes.Proof == nil {
		t.Fatal("transaction execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("transaction execution proof is invalid: %v", err)
	}
}

func assertTransactionBuildProofComputeGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, rawProgram uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine := NewTVM()

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeMessageGasConsumedCode(t)
	wantExitCode := int64(0)
	if version < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if rawProgram%2 != 0 {
		code = makeDirectMessageInMsgParamsCode(t, newData, false)
		if version < 11 {
			wantExitCode = vmerr.CodeInvalidOpcode
		} else {
			wantExitCode = 0
		}
	}

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(machine, shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  refConfigRoot,
		BuildProof:  true,
	})
	if err != nil {
		t.Fatalf("go transaction build-proof global-version override machine_v=%d effective_v=%d program=%d failed: %v", machineVersion, version, rawProgram%2, err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, refConfigRoot)
	if err != nil {
		t.Fatalf("reference transaction build-proof global-version override machine_v=%d effective_v=%d program=%d failed: %v", machineVersion, version, rawProgram%2, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d effective_v=%d program=%d: go=%d reference=%d", machineVersion, version, rawProgram%2, goRes.ExitCode, refRes.exitCode)
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("effective v%d program=%d exit=%d, want %d", version, rawProgram%2, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d effective_v=%d program=%d: go=%d reference=%d", machineVersion, version, rawProgram%2, goRes.GasUsed, refRes.gasUsed)
	}
	if goRes.Proof == nil {
		t.Fatal("transaction execution proof should be available")
	}
	if _, err = cell.UnwrapProof(goRes.Proof, shard.Account.Hash()); err != nil {
		t.Fatalf("transaction execution proof is invalid: %v", err)
	}
}

func assertTransactionComputeFallbackVersionParity(t *testing.T, machineVersion, configVersion int, useConfigRoot bool, rawProgram uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	effectiveVersion := machineVersion
	var goConfigRoot *cell.Cell
	if useConfigRoot {
		effectiveVersion = configVersion
		goConfigRoot = referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(configVersion))
	}
	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(effectiveVersion))

	machine := NewTVM()

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeMessageGasConsumedCode(t)
	wantExitCode := int64(0)
	if effectiveVersion < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if rawProgram%2 != 0 {
		code = makeDirectMessageInMsgParamsCode(t, newData, false)
		if effectiveVersion < 11 {
			wantExitCode = vmerr.CodeInvalidOpcode
		} else {
			wantExitCode = 0
		}
	}

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(machine, shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  goConfigRoot,
	})
	if !useConfigRoot {
		if !errors.Is(err, errConfigRootRequired) {
			t.Fatalf("go transaction compute fallback without config root machine_v=%d program=%d error = %v, want %v", machineVersion, rawProgram%2, err, errConfigRootRequired)
		}
		return
	}
	if err != nil {
		t.Fatalf("go transaction compute fallback machine_v=%d config_v=%d use_config=%t program=%d failed: %v", machineVersion, configVersion, useConfigRoot, rawProgram%2, err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, refConfigRoot)
	if err != nil {
		t.Fatalf("reference transaction compute fallback machine_v=%d config_v=%d effective_v=%d use_config=%t program=%d failed: %v", machineVersion, configVersion, effectiveVersion, useConfigRoot, rawProgram%2, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d config_v=%d effective_v=%d use_config=%t program=%d: go=%d reference=%d", machineVersion, configVersion, effectiveVersion, useConfigRoot, rawProgram%2, goRes.ExitCode, refRes.exitCode)
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("effective v%d program=%d exit=%d, want %d", effectiveVersion, rawProgram%2, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d config_v=%d effective_v=%d use_config=%t program=%d: go=%d reference=%d", machineVersion, configVersion, effectiveVersion, useConfigRoot, rawProgram%2, goRes.GasUsed, refRes.gasUsed)
	}

	if !useConfigRoot {
		return
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func assertTransactionComputeGlobalVersionOverrideParity(t *testing.T, version, machineVersion int, rawProgram uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	refConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	machine := NewTVM()

	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeMessageGasConsumedCode(t)
	wantExitCode := int64(0)
	if version < 4 {
		wantExitCode = vmerr.CodeInvalidOpcode
	}
	if rawProgram%2 != 0 {
		code = makeDirectMessageInMsgParamsCode(t, newData, false)
		if version < 11 {
			wantExitCode = vmerr.CodeInvalidOpcode
		} else {
			wantExitCode = 0
		}
	}

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(machine, shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  refConfigRoot,
	})
	if err != nil {
		t.Fatalf("go transaction compute global-version override machine_v=%d effective_v=%d program=%d failed: %v", machineVersion, version, rawProgram%2, err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, refConfigRoot)
	if err != nil {
		t.Fatalf("reference transaction compute global-version override machine_v=%d effective_v=%d program=%d failed: %v", machineVersion, version, rawProgram%2, err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch machine_v=%d effective_v=%d program=%d: go=%d reference=%d", machineVersion, version, rawProgram%2, goRes.ExitCode, refRes.exitCode)
	}
	if goRes.ExitCode != wantExitCode {
		t.Fatalf("effective v%d program=%d exit=%d, want %d", version, rawProgram%2, goRes.ExitCode, wantExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch machine_v=%d effective_v=%d program=%d: go=%d reference=%d", machineVersion, version, rawProgram%2, goRes.GasUsed, refRes.gasUsed)
	}
}

func TestTVMCrossEmulatorTransactionOldVersionActionModes(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionOldVersionActionModeParity(t, version)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionOldVersionActionModes(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version))
	}
	f.Add(uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionOldVersionActionModeParity(t, version)
	})
}

func assertTransactionOldVersionActionModeParity(t *testing.T, version uint32) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	actions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 16,
		Currency: tlb.CurrencyCollection{
			Coins: tlb.FromNanoTON(big.NewInt(20_000_000_000)),
		},
	})
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionInvalidSendModeGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		for _, mode := range []uint8{4, 6} {
			t.Run(fmt.Sprintf("global_v%d_mode_%d", version, mode), func(t *testing.T) {
				assertTransactionInvalidSendModeVersionParity(t, version, mode, 0xA900, 0xB900, 0xC900)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTransactionInvalidSendModeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint16(0xA900+version), uint16(0xB900+version), uint16(0xC900+version))
		f.Add(uint8(version), uint8(1), uint16(0xD900+version), uint16(0xE900+version), uint16(0xF900+version))
	}
	f.Add(uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMode uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		mode := transactionInvalidSendModeFuzzMode(rawMode)
		assertTransactionInvalidSendModeVersionParity(t, version, mode, origTag, newTag, bodyTag)
	})
}

func transactionInvalidSendModeFuzzMode(raw uint8) uint8 {
	modes := [...]uint8{4, 6, 12, 14, 68, 70, 192, 194}
	return modes[int(raw)%len(modes)]
}

func assertTransactionInvalidSendModeVersionParity(t *testing.T, version uint32, mode uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{
		Mode: mode,
		Msg:  buildTransactionOutboundInternalCell(t, 0),
	})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	want := transactionInvalidSendModeExpectation(version, mode)
	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionInvalidSendModeExpectation(version uint32, mode uint8) transactionActionPhaseExpectation {
	if version >= 13 && mode&2 != 0 {
		return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
	}
	return transactionActionPhaseExpectation{valid: true, resultCode: 34}
}

func TestTVMCrossEmulatorTransactionActionFineGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        body,
	})
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		CellPrice: 4 << 16,
	})
	if err != nil {
		t.Fatalf("failed to build message forward prices: %v", err)
	}
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasLimit:      1_000_000,
		BlockGasLimit: 1_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build gas prices: %v", err)
	}
	outMsg := buildTransactionOutboundInternalCellWithBody(t, 10, transactionTestCellChain(20))

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			want := transactionActionPhaseExpectation{valid: true, resultCode: 37}
			wantFee := uint64(0)
			if version >= 4 {
				want = transactionActionPhaseExpectation{valid: true, resultCode: 40}
				wantFee = 10
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
				int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
				int32(tlb.ConfigParamGasPricesBasechain):          gasCell,
				int32(tlb.ConfigParamGasPricesMasterchain):        gasCell,
			})
			code := makeTransactionInternalSendCode(t, outMsg, newData, 0)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 11, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}

			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
			assertOrdinaryTransactionActionFees(t, "go", goRes.TransactionCell, wantFee)
			assertOrdinaryTransactionActionFees(t, "reference", refRes.txCell, wantFee)
		})
	}
}

func TestTVMCrossEmulatorTransactionOutboundMessageResult39(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const version = uint32(7)
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		LumpPrice: 400_000,
		CellPrice: 4 << 16,
		FirstFrac: 21_845,
	})
	if err != nil {
		t.Fatalf("failed to build message forward prices: %v", err)
	}
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	balance := new(big.Int).Lsh(big.NewInt(1), 118)
	result39Msg := buildTransactionResult39LegacyOutboundMessage(t)

	for _, tc := range []struct {
		name       string
		mode       uint8
		want       transactionActionPhaseExpectation
		wantFees   uint64
		wantResult *int32
	}{
		{
			name:       "result 39",
			mode:       1,
			want:       transactionActionPhaseExpectation{valid: true, resultCode: 39, messagesCreated: 1},
			wantFees:   4,
			wantResult: transactionActionResultArg(1),
		},
		{
			name:     "mode 2 ignores",
			mode:     3,
			want:     transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1},
			wantFees: 133_335,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := buildTransactionActionList(t,
				tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCell(t, 1)},
				tlb.ActionSendMsg{Mode: tc.mode, Msg: result39Msg},
			)
			code := makeTransactionInternalActionsCode(t, actions, newData)
			shard := buildTransactionTestShardAccountWithBigBalance(t, tonopsTestAddr, code, origData, balance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}
			var debugTx tlb.Transaction
			if err := tlb.LoadFromCell(&debugTx, goRes.TransactionCell.MustBeginParse()); err == nil && debugTx.IO.Out != nil {
				if kvs, err := debugTx.IO.Out.List.LoadAll(); err == nil {
					for i, kv := range kvs {
						if sl, err := kv.Value.LoadRef(); err == nil {
							t.Logf("out %d root bits=%d refs=%d", i, sl.BaseCell().BitsSize(), sl.BaseCell().RefsNum())
						}
					}
				}
			}

			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, tc.want)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, tc.want)
			assertOrdinaryTransactionActionFees(t, "go", goRes.TransactionCell, tc.wantFees)
			assertOrdinaryTransactionActionFees(t, "reference", refRes.txCell, tc.wantFees)
			assertOrdinaryTransactionActionResultArg(t, "go", goRes.TransactionCell, tc.wantResult)
			assertOrdinaryTransactionActionResultArg(t, "reference", refRes.txCell, tc.wantResult)
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func buildTransactionResult39LegacyOutboundMessage(t *testing.T) *cell.Cell {
	t.Helper()

	largeAmount := new(big.Int).Lsh(big.NewInt(1), 108)
	largeFee := new(big.Int).Lsh(big.NewInt(1), 112)
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(27, transactionAddressPrefix(tonopsTestAddr.Data(), 27)))
	msgCell, usedLayout, err := transactionInternalMessageToCellWithLayout(&tlb.InternalMessage{
		SrcAddr: address.NewAddressNone(),
		DstAddr: dst,
		Amount:  tlb.FromNanoTON(largeAmount),
		IHRFee:  tlb.FromNanoTON(largeFee),
		FwdFee:  tlb.FromNanoTON(largeFee),
		StateInit: &tlb.StateInit{
			Code: cell.BeginCell().MustStoreUInt(0xC0, 8).EndCell(),
			Data: cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell(),
		},
		Body: cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	}, transactionOutboundLayout{})
	if err != nil {
		t.Fatalf("build legacy outbound message: %v", err)
	}
	if usedLayout != (transactionOutboundLayout{}) {
		t.Fatalf("initial message layout = %+v, want inline StateInit and body", usedLayout)
	}
	return msgCell
}

func buildTransactionTestShardAccountWithBigBalance(t *testing.T, addr *address.Address, code, data *cell.Cell, balance *big.Int, lastPaid uint32) *tlb.ShardAccount {
	t.Helper()

	storageInfoCell, err := tlb.ToCell(&tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     lastPaid,
	})
	if err != nil {
		t.Fatalf("failed to serialize storage info: %v", err)
	}
	stateInitCell, err := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}
	accountCell := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreBuilder(storageInfoCell.ToBuilder()).
		MustStoreUInt(0, 64).
		MustStoreBigCoins(balance).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInitCell.ToBuilder()).
		EndCell()

	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
	}
}

func TestTVMCrossEmulatorTransactionFailedActionTotalActionFeesV4Boundary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	priceCell := buildTransactionMsgForwardPricesCell(t, 400_000, 21_845)
	actions := buildTransactionActionList(t,
		tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCell(t, 1)},
		tlb.ActionSendMsg{Mode: 4, Msg: buildTransactionOutboundInternalCell(t, 0)},
	)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, 2_000_000_000, now)

	for _, tc := range []struct {
		version  uint32
		wantFees uint64
	}{
		{version: 3, wantFees: 133_331},
		{version: 4},
		{version: 14},
	} {
		t.Run(fmt.Sprintf("global_v%d", tc.version), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, tc.version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
				int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
			})
			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			want := transactionActionPhaseExpectation{valid: true, resultCode: 34, messagesCreated: 1}
			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
			assertOrdinaryTransactionActionFees(t, "go", goRes.TransactionCell, tc.wantFees)
			assertOrdinaryTransactionActionFees(t, "reference", refRes.txCell, tc.wantFees)
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func assertOrdinaryTransactionActionResultArg(t *testing.T, side string, txCell *cell.Cell, want *int32) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok || desc.ActionPhase == nil {
		t.Fatalf("%s transaction has no ordinary action phase", side)
	}
	got := desc.ActionPhase.ResultArg
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Fatalf("%s result arg = %v, want %v", side, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s result arg = %d, want %d", side, *got, *want)
	}
}

func FuzzTVMCrossEmulatorTransactionActionFineGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint64(10), uint8(6), uint16(0xA000+version), uint16(0xB000+version))
		f.Add(uint8(version), uint64(1), uint8(1), uint16(0xC000+version), uint16(0xD000+version))
		f.Add(uint8(version), uint64(32), uint8(31), uint16(0xE000+version), uint16(0xF000+version))
	}
	f.Add(uint8(255), uint64(0xffffffffffffffff), uint8(255), uint16(0), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawRemaining uint64, rawExtraCells uint8, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionActionFineVersionParity(t, version, rawRemaining, rawExtraCells, dataTag, bodyTag)
	})
}

func assertTransactionActionFineVersionParity(t *testing.T, version uint32, rawRemaining uint64, rawExtraCells uint8, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	remaining := rawRemaining%32 + 1
	bodyCells := int(remaining) + int(rawExtraCells%32) + 4
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        body,
	})
	outMsg := buildTransactionOutboundInternalCellWithBody(t, remaining, transactionTestCellChain(bodyCells))
	code := makeTransactionInternalSendCode(t, outMsg, newData, 0)

	priceCell, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{
		CellPrice: 4 << 16,
	})
	if err != nil {
		t.Fatalf("failed to build message forward prices: %v", err)
	}
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasLimit:      1_000_000,
		BlockGasLimit: 1_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build gas prices: %v", err)
	}
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
		int32(tlb.ConfigParamGasPricesBasechain):          gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain):        gasCell,
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, remaining+1, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	want := transactionActionPhaseExpectation{valid: true, resultCode: 37}
	wantFee := uint64(0)
	if version >= 4 {
		want = transactionActionPhaseExpectation{valid: true, resultCode: 40}
		wantFee = remaining
	}
	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	assertOrdinaryTransactionActionFees(t, "go", goRes.TransactionCell, wantFee)
	assertOrdinaryTransactionActionFees(t, "reference", refRes.txCell, wantFee)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func assertOrdinaryTransactionActionFees(t *testing.T, side string, txCell *cell.Cell, want uint64) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("%s transaction description = %T, want ordinary", side, tx.Description)
	}
	if desc.ActionPhase == nil {
		t.Fatalf("%s transaction has no action phase", side)
	}

	got := uint64(0)
	if desc.ActionPhase.TotalActionFees != nil {
		got = desc.ActionPhase.TotalActionFees.Nano().Uint64()
	}
	if got != want {
		t.Fatalf("%s action fees = %d, want %d", side, got, want)
	}
}

func transactionCrossShardAccountDataDump(t *testing.T, shardCell *cell.Cell) string {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		return "decode shard account failed: " + err.Error()
	}
	if shard.Account == nil {
		return "<nil account>"
	}
	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		return "decode account failed: " + err.Error()
	}
	if account.StateInit == nil || account.StateInit.Data == nil {
		return "<nil data>"
	}
	return account.StateInit.Data.Dump()
}

func transactionCrossShardAccountSummary(t *testing.T, shardCell *cell.Cell) string {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		return "decode shard account failed: " + err.Error()
	}
	if shard.Account == nil {
		return "<nil account>"
	}
	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		return "decode account failed: " + err.Error()
	}
	anycast := "<nil>"
	if account.Address != nil && account.Address.Anycast() != nil {
		anycast = fmt.Sprintf("depth=%d prefix=%x", account.Address.Anycast().Depth(), account.Address.Anycast().Prefix())
	}
	stateDepth := "<nil>"
	if account.StateInit != nil && account.StateInit.Depth != nil {
		stateDepth = big.NewInt(int64(*account.StateInit.Depth)).String()
	}
	return fmt.Sprintf("addr=%s data=%x anycast=%s state_depth=%s status=%s balance=%s storage=%s/%s last_paid=%d due=%s hash=%x",
		account.Address.StringRaw(),
		account.Address.Data(),
		anycast,
		stateDepth,
		account.Status,
		account.Balance.Nano().String(),
		account.StorageInfo.StorageUsed.CellsUsed.String(),
		account.StorageInfo.StorageUsed.BitsUsed.String(),
		account.StorageInfo.LastPaid,
		transactionCrossCoinsPtr(account.StorageInfo.DuePayment),
		shard.Account.Hash(),
	)
}

func transactionCrossShardDuePayment(t *testing.T, shardCell *cell.Cell) uint64 {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to decode shard account: %v", err)
	}
	if shard.Account == nil {
		t.Fatal("shard account is nil")
	}

	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to decode account: %v", err)
	}
	if account.StorageInfo.DuePayment == nil {
		return 0
	}
	return account.StorageInfo.DuePayment.Nano().Uint64()
}

func transactionCrossShardStatus(t *testing.T, shardCell *cell.Cell) tlb.AccountStatus {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to decode shard account: %v", err)
	}
	if shard.Account == nil {
		t.Fatal("shard account is nil")
	}

	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to decode account: %v", err)
	}
	if !account.IsValid {
		return tlb.AccountStatusNonExist
	}
	return account.Status
}

func transactionCrossOrdinaryDestroyed(t *testing.T, txCell *cell.Cell) bool {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode transaction: %v", err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("transaction description = %T, want ordinary", tx.Description)
	}
	return desc.Destroyed
}

func transactionCrossEndStatus(t *testing.T, txCell *cell.Cell) tlb.AccountStatus {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode transaction: %v", err)
	}
	return tx.EndStatus
}

func transactionCrossShardStorageExtra(t *testing.T, shardCell *cell.Cell) any {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to decode shard account: %v", err)
	}
	if shard.Account == nil {
		t.Fatal("shard account is nil")
	}

	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to decode account: %v", err)
	}
	return account.StorageInfo.StorageExtra
}

func transactionCrossShardStorageUsedCells(t *testing.T, shardCell *cell.Cell) uint64 {
	t.Helper()

	return transactionCrossShardStorageUsed(t, shardCell).cells
}

type transactionCrossStorageUsed struct {
	cells uint64
	bits  uint64
}

func transactionCrossShardStorageUsed(t *testing.T, shardCell *cell.Cell) transactionCrossStorageUsed {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to decode shard account: %v", err)
	}
	if shard.Account == nil {
		t.Fatal("shard account is nil")
	}

	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to decode account: %v", err)
	}
	return transactionCrossStorageUsed{
		cells: account.StorageInfo.StorageUsed.CellsUsed.Uint64(),
		bits:  account.StorageInfo.StorageUsed.BitsUsed.Uint64(),
	}
}

func transactionStorageExtraIsInfo(t *testing.T, extra any) bool {
	t.Helper()

	switch v := extra.(type) {
	case tlb.StorageExtraInfo:
		if len(v.DictHash) != 32 {
			t.Fatalf("storage extra dict hash len = %d, want 32", len(v.DictHash))
		}
		return true
	case tlb.StorageExtraNone:
		return false
	default:
		t.Fatalf("storage extra = %T, want none or info", extra)
		return false
	}
}

func transactionVersionStoragePricesCell(t *testing.T, feePerSecondPerCell uint64) *cell.Cell {
	t.Helper()

	priceCell, err := tlb.ToCell(&tlb.ConfigStoragePrices{
		ValidSince:  0,
		CellPrice:   feePerSecondPerCell << 16,
		MCCellPrice: feePerSecondPerCell << 16,
	})
	if err != nil {
		t.Fatalf("failed to build storage prices cell: %v", err)
	}
	dict := cell.NewDict(32)
	if err = dict.SetIntKey(big.NewInt(0), priceCell); err != nil {
		t.Fatalf("failed to store storage prices: %v", err)
	}
	configCell, err := tlb.ToCell(&tlb.StoragePricesConfig{Prices: dict})
	if err != nil {
		t.Fatalf("failed to build storage prices config: %v", err)
	}
	return configCell
}

func transactionVersionSizeLimitsCell(t *testing.T, accStateCellsForStorageDict uint32) *cell.Cell {
	t.Helper()

	limitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV2{
		MaxMsgBits:                  1 << 21,
		MaxMsgCells:                 1 << 13,
		MaxLibraryCells:             1000,
		MaxVMDataDepth:              512,
		MaxExtMsgSize:               65535,
		MaxExtMsgDepth:              512,
		MaxAccStateCells:            1 << 16,
		MaxMCAccStateCells:          1 << 11,
		MaxAccPublicLibraries:       256,
		DeferOutQueueSizeLimit:      256,
		MaxMsgExtraCurrencies:       2,
		MaxAccFixedPrefixLength:     8,
		AccStateCellsForStorageDict: accStateCellsForStorageDict,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}
	return limitsCell
}

func transactionVersionShardAccountWithExtra(t *testing.T, addr *address.Address, code, data *cell.Cell, balance uint64, extra *cell.Dictionary, lastPaid uint32) *tlb.ShardAccount {
	t.Helper()

	accountCell, err := tlb.ToCell(&tlb.AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     lastPaid,
		},
		AccountStorage: tlb.AccountStorage{
			LastTransactionLT: 0,
			Balance:           tlb.FromNanoTONU(balance),
			ExtraCurrencies:   extra,
			Status:            tlb.AccountStatusActive,
			StateInit: &tlb.StateInit{
				Code: code,
				Data: data,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to build shard account with extra currencies: %v", err)
	}

	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}
}

func TestTVMCrossEmulatorTransactionRandSeedGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.RANDSEED().Serialize(),
		cellsliceop.NEWC().Serialize(),
		cellsliceop.STU(256).Serialize(),
		cellsliceop.ENDC().Serialize(),
		execop.POPCTR(4).Serialize(),
	)

	addrData := append([]byte(nil), tonopsTestAddr.Data()...)
	addrData[0] &^= 0xE0
	anycastAddr := address.NewAddress(0, 0, addrData).WithAnycast(address.NewAnycast(3, []byte{0xA0}))
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     anycastAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	rewrittenData, err := transactionRewrittenAccountAddressData(anycastAddr)
	if err != nil {
		t.Fatalf("failed to rewrite anycast address: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			shard := buildTransactionTestShardAccount(t, anycastAddr, code, origData, walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     anycastAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			assertShardAccountAnycastAddressVersion(t, "go", goRes.NextAccount.ShardAccountCell(), version, anycastAddr.Data(), rewrittenData, 3)
			assertShardAccountAnycastAddressVersion(t, "reference", refRes.shardCell, version, anycastAddr.Data(), rewrittenData, 3)
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionRandSeedGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(3), uint8(0xA0), uint64(0x1111111111111111), uint64(0x2222222222222222), uint16(0xAAAA), uint16(0xCAFE))
		f.Add(uint8(version), uint8(1), uint8(0x80), uint64(0), uint64(0), uint16(0xBBBB), uint16(0xD00D))
		f.Add(uint8(version), uint8(8), uint8(0x5A), uint64(0x0123456789ABCDEF), uint64(0xFEDCBA9876543210), uint16(0xCCCC), uint16(0xBEEF))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0), uint16(0), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawDepth, rawPrefix uint8, rawSeedA, rawSeedB uint64, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionRandSeedVersionParity(t, version, rawDepth, rawPrefix, rawSeedA, rawSeedB, dataTag, bodyTag)
	})
}

func assertTransactionRandSeedVersionParity(t *testing.T, version uint32, rawDepth, rawPrefix uint8, rawSeedA, rawSeedB uint64, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	depth := uint(rawDepth%8) + 1
	addrData := append([]byte(nil), tonopsTestAddr.Data()...)
	addrData[1] = byte(dataTag)
	addrData[2] = byte(dataTag >> 8)
	for i := uint(0); i < depth; i++ {
		mask := byte(1 << (7 - i%8))
		if transactionBit([]byte{rawPrefix}, int(i)) == 1 {
			addrData[i/8] &^= mask
		} else {
			addrData[i/8] |= mask
		}
	}
	anycastAddr := address.NewAddress(0, 0, addrData).WithAnycast(address.NewAnycast(depth, []byte{rawPrefix}))
	rewrittenData, err := transactionRewrittenAccountAddressData(anycastAddr)
	if err != nil {
		t.Fatalf("failed to rewrite anycast address: %v", err)
	}

	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := transactionRandSeedStoreCode(t)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     anycastAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	seed := transactionCrossRandSeed(rawSeedA, rawSeedB)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, anycastAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     anycastAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), seed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), seed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	assertShardAccountAnycastAddressVersion(t, "go", goRes.NextAccount.ShardAccountCell(), version, anycastAddr.Data(), rewrittenData, depth)
	assertShardAccountAnycastAddressVersion(t, "reference", refRes.shardCell, version, anycastAddr.Data(), rewrittenData, depth)
	if version >= uint32(funcsop.RANDSEED().MinGlobalVersion()) {
		wantSeed, err := transactionEmulationSeedForTest(seed, anycastAddr, version)
		if err != nil {
			t.Fatalf("failed to build expected seed: %v", err)
		}
		wantData := transactionBits256(wantSeed.Bytes())
		assertShardAccountDataBytes(t, "go", goRes.NextAccount.ShardAccountCell(), wantData)
		assertShardAccountDataBytes(t, "reference", refRes.shardCell, wantData)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionRandSeedStoreCode(t *testing.T) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.RANDSEED().Serialize(),
		cellsliceop.NEWC().Serialize(),
		cellsliceop.STU(256).Serialize(),
		cellsliceop.ENDC().Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func transactionCrossRandSeed(rawA, rawB uint64) []byte {
	seed := make([]byte, 32)
	for i := range seed {
		shift := uint((i % 8) * 8)
		seed[i] = byte(rawA>>shift) ^ byte(rawB>>shift) ^ byte(i*29)
	}
	return seed
}

func assertShardAccountDataBytes(t *testing.T, side string, shardCell *cell.Cell, want []byte) {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to parse %s shard account: %v", side, err)
	}
	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse %s account: %v", side, err)
	}
	if account.StateInit == nil || account.StateInit.Data == nil {
		t.Fatalf("%s account has no state data", side)
	}
	data := account.StateInit.Data.MustBeginParse()
	if data.BitsLeft() != uint(len(want)*8) || data.RefsNum() != 0 {
		t.Fatalf("%s account data shape = bits:%d refs:%d, want bits:%d refs:0", side, data.BitsLeft(), data.RefsNum(), len(want)*8)
	}
	got := data.MustLoadSlice(uint(len(want) * 8))
	if !bytes.Equal(got, want) {
		t.Fatalf("%s account data = %x, want %x", side, got, want)
	}
}

func assertShardAccountAnycastAddressVersion(t *testing.T, side string, shardCell *cell.Cell, version uint32, rawData, rewrittenData []byte, depth uint) {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to parse %s shard account: %v", side, err)
	}
	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to parse %s account: %v", side, err)
	}

	wantData := rawData
	wantAnycast := true
	var wantDepth *uint64
	if version < 10 {
		v := uint64(depth)
		wantDepth = &v
	} else {
		wantData = rewrittenData
		wantAnycast = false
	}

	if !bytes.Equal(account.Address.Data(), wantData) {
		t.Fatalf("%s v%d account data = %x, want %x", side, version, account.Address.Data(), wantData)
	}
	if (account.Address.Anycast() != nil) != wantAnycast {
		t.Fatalf("%s v%d anycast present = %t, want %t", side, version, account.Address.Anycast() != nil, wantAnycast)
	}
	var gotDepth *uint64
	if account.StateInit != nil {
		gotDepth = account.StateInit.Depth
	}
	if !transactionUint64PtrEqual(gotDepth, wantDepth) {
		t.Fatalf("%s v%d state depth = %v, want %v", side, version, gotDepth, wantDepth)
	}
}

func TestTVMCrossEmulatorTransactionInboundExternalValidationGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(1, []byte{tonopsTestAddr.Data()[0] & 0x80}))
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		DstAddr: dst,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("anycast_destination_global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
			goRes, goErr := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)

			if version >= 10 {
				if goErr == nil {
					t.Fatal("go transaction accepted invalid v10 inbound external destination")
				}
				if refErr == nil {
					t.Fatal("reference transaction accepted invalid v10 inbound external destination")
				}
				return
			}

			if goErr != nil {
				t.Fatalf("go transaction emulation failed: %v", goErr)
			}
			if refErr != nil {
				t.Fatalf("reference transaction emulation failed: %v", refErr)
			}
			if !goRes.Accepted {
				t.Fatal("expected go transaction emulation to accept v9 external message")
			}
			if goRes.ExitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
			}
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}

	largeMsgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		DstAddr: tonopsTestAddr,
		Body:    buildTransactionZeroBitsCell(t, 900),
	})
	if err != nil {
		t.Fatalf("failed to build large external message: %v", err)
	}
	sizeLimits := buildTransactionSizeLimitsCell(t, 64, 1<<13, 1000, 1<<16, 1<<11)
	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("message_size_global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamSizeLimits): sizeLimits,
			})
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
			_, goErr := testEmulateTransaction(NewTVM(), shard, largeMsgCell, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, largeMsgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)

			if goErr == nil {
				t.Fatalf("go transaction accepted oversized v%d inbound external message", version)
			}
			if refErr == nil {
				t.Fatalf("reference transaction accepted oversized v%d inbound external message: %+v", version, refRes)
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionOutboundAnycastDestinationGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	wrongPrefix := []byte{tonopsTestAddr.Data()[0] & 0x80}
	wrongPrefix[0] ^= 0x80
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(1, wrongPrefix))
	outMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), dst, 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	tests := []struct {
		name string
		mode uint8
		want func(uint32) transactionActionPhaseExpectation
	}{
		{
			name: "without_ignore_errors",
			mode: 0,
			want: func(version uint32) transactionActionPhaseExpectation {
				if version < 10 {
					return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
				}
				return transactionActionPhaseExpectation{valid: true, resultCode: 36}
			},
		},
		{
			name: "with_ignore_errors",
			mode: 2,
			want: func(version uint32) transactionActionPhaseExpectation {
				if version < 10 {
					return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
				}
				return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
			},
		},
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
				int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
			})

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					code := makeTransactionInternalSendCode(t, outMsg, newData, tt.mode)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

					goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
						Address:     tonopsTestAddr,
						Now:         now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}

					refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference transaction emulation failed: %v", err)
					}

					want := tt.want(version)
					assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
					assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
					assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
					assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
					assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionOutboundAnycastDestinationGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(1), uint32(0x00000000), uint16(0xA000+version), uint64(1000))
		f.Add(uint8(version), uint8(1), uint8(3), uint32(0xE0000000), uint16(0xB000+version), uint64(777))
		f.Add(uint8(version), uint8(0), uint8(30), uint32(0xFFFFFFFC), uint16(0xC000+version), uint64(1))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint32(0x12345678), uint16(0xffff), uint64(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawMode, rawDepth uint8, rawPrefix uint32, bodyTag uint16, rawAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionOutboundAnycastDestinationVersionParity(t, version, rawMode, rawDepth, rawPrefix, bodyTag, rawAmount)
	})
}

func assertTransactionOutboundAnycastDestinationVersionParity(t *testing.T, version uint32, rawMode, rawDepth uint8, rawPrefix uint32, bodyTag uint16, rawAmount uint64) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	depth := uint(rawDepth%30) + 1
	prefix := transactionCrossAnycastPrefix(rawPrefix, depth)
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(depth, prefix))
	outMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), dst, rawAmount%1_000_001, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	mode := uint8(0)
	if rawMode%2 != 0 {
		mode = 2
	}
	want := transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	if version >= 10 {
		if mode&2 != 0 {
			want = transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
		} else {
			want = transactionActionPhaseExpectation{valid: true, resultCode: 36}
		}
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})
	code := makeTransactionInternalSendCode(t, outMsg, newData, mode)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
}

func transactionCrossAnycastPrefix(raw uint32, depth uint) []byte {
	prefix := make([]byte, (depth+7)/8)
	for i := range prefix {
		shift := uint((len(prefix) - 1 - i) * 8)
		prefix[i] = byte(raw >> shift)
	}
	if rem := depth % 8; rem != 0 {
		prefix[len(prefix)-1] &= 0xff << (8 - rem)
	}
	return prefix
}

func TestTVMCrossEmulatorTransactionOutboundVarDestinationGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	tests := []struct {
		name    string
		dst     *address.Address
		want    transactionActionPhaseExpectation
		wantOut *address.Address
	}{
		{
			name:    "normalized_256_bit_basechain",
			dst:     address.NewAddressVar(0, tonopsTestAddr.Workchain(), 256, tonopsTestAddr.Data()),
			want:    transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1},
			wantOut: tonopsTestAddr,
		},
		{
			name: "rejects_short_masterchain",
			dst:  address.NewAddressVar(0, address.MasterchainID, 255, tonopsTestAddr.Data()),
			want: transactionActionPhaseExpectation{valid: true, resultCode: 36},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), tt.dst, 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
			code := makeTransactionInternalSendCode(t, outMsg, newData, 1)

			for _, version := range transactionVersionCrossEmulatorVersions(t) {
				t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
					configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
						int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
						int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
					})
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

					goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
						Address:     tonopsTestAddr,
						Now:         now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}

					refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference transaction emulation failed: %v", err)
					}

					assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, tt.want)
					assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, tt.want)
					if tt.wantOut != nil {
						assertOrdinaryTransactionSingleInternalOutDest(t, "go", goRes.TransactionCell, tt.wantOut)
						assertOrdinaryTransactionSingleInternalOutDest(t, "reference", refRes.txCell, tt.wantOut)
					}
					if goRes.GasUsed != refRes.gasUsed {
						t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
					}
					if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
						t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
						t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
						t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
					}
					if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
						t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
						t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
					}
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionOutboundVarDestinationGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0), uint16(256), uint8(0x11), uint16(0xA000+version), uint64(1000))
		f.Add(uint8(version), uint8(1), uint8(0), uint16(256), uint8(0x22), uint16(0xB000+version), uint64(777))
		f.Add(uint8(version), uint8(2), uint8(0), uint16(255), uint8(0x33), uint16(0xC000+version), uint64(1))
		f.Add(uint8(version), uint8(2), uint8(1), uint16(0), uint8(0x44), uint16(0xD000+version), uint64(1_000_000))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint16(0xffff), uint8(0xff), uint16(0xffff), uint64(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase, rawMode uint8, rawBits uint16, fill uint8, bodyTag uint16, rawAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionOutboundVarDestinationVersionParity(t, version, rawCase, rawMode, rawBits, fill, bodyTag, rawAmount)
	})
}

func assertTransactionOutboundVarDestinationVersionParity(t *testing.T, version uint32, rawCase, rawMode uint8, rawBits uint16, fill uint8, bodyTag uint16, rawAmount uint64) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)

	mode := uint8(0)
	if rawMode%2 != 0 {
		mode = 2
	}

	workchain := int32(tonopsTestAddr.Workchain())
	bits := uint(256)
	invalidDst := false
	switch rawCase % 3 {
	case 1:
		workchain = address.MasterchainID
	case 2:
		workchain = address.MasterchainID
		bits = uint(rawBits % 256)
		invalidDst = true
	}
	data := bytes.Repeat([]byte{fill}, 32)
	dst := address.NewAddressVar(0, workchain, bits, data)
	outAmount := uint64(1 + rawAmount%1_000_000)
	outMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), dst, outAmount, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())

	want := transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	if invalidDst {
		if mode&2 != 0 {
			want = transactionActionPhaseExpectation{success: true, valid: true}
			if version >= 8 {
				want.skippedActions = 1
			}
		} else {
			want = transactionActionPhaseExpectation{valid: true, resultCode: 36}
		}
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})
	code := makeTransactionInternalSendCode(t, outMsg, newData, mode)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func FuzzTVMCrossEmulatorTransactionInboundExternalValidationGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(16), uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version))
		f.Add(uint8(version), uint8(3), uint8(24), uint16(0xD000+version), uint16(0xE000+version), uint16(0xF000+version))
		f.Add(uint8(version), uint8(29), uint8(64), uint16(0xA100+version), uint16(0xB100+version), uint16(0xC100+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawDepth, rawSrcBits uint8, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionInboundExternalValidationVersionParity(t, version, rawDepth, rawSrcBits, origTag, newTag, bodyTag)
	})
}

func assertTransactionInboundExternalValidationVersionParity(t *testing.T, version uint32, rawDepth, rawSrcBits uint8, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	depth := uint(rawDepth%30) + 1
	dst := tonopsTestAddr.WithAnycast(address.NewAnycast(depth, transactionAddressPrefix(tonopsTestAddr.Data(), depth)))
	srcBits := uint(rawSrcBits%64) + 1
	srcData := bytes.Repeat([]byte{byte(bodyTag)}, int((srcBits+7)/8))
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		SrcAddr: address.NewAddressExt(0, srcBits, srcData),
		DstAddr: dst,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	goRes, goErr := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msgCell, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)

	if version >= 10 {
		if goErr == nil {
			t.Fatal("go transaction accepted invalid v10 inbound external destination")
		}
		if refErr == nil {
			t.Fatal("reference transaction accepted invalid v10 inbound external destination")
		}
		return
	}

	if goErr != nil {
		t.Fatalf("go transaction emulation failed: %v", goErr)
	}
	if refErr != nil {
		t.Fatalf("reference transaction emulation failed: %v", refErr)
	}
	if !goRes.Accepted {
		t.Fatal("expected go transaction emulation to accept legacy external message")
	}
	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionSendMsgCustomFwdFeeGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	outMsg := transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(1))
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	}, func(b *cell.Builder) {
		b.MustStoreUInt(1, 4).
			MustStoreUInt(0, 8)
	})
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 0, Msg: outMsg})

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			code := makeTransactionInternalActionsCode(t, actions, newData)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionSendMsgCustomFwdFeeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0), uint16(0xA000+version), uint16(0xB000+version))
		f.Add(uint8(version), uint8(1), uint8(1), uint16(0xC000+version), uint16(0xD000+version))
		f.Add(uint8(version), uint8(0), uint8(3), uint16(0xE000+version), uint16(0xF000+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint16(0xffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawMode, rawLen uint8, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionSendMsgCustomFwdFeeVersionParity(t, version, rawMode, rawLen, dataTag, bodyTag)
	})
}

func assertTransactionSendMsgCustomFwdFeeVersionParity(t *testing.T, version uint32, rawMode, rawLen uint8, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).MustStoreUInt(uint64(dataTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).MustStoreUInt(uint64(dataTag)^0xffff, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	mode := uint8(0)
	if rawMode&1 != 0 {
		mode = 2
	}

	fwdFeeBytes := uint(rawLen%4) + 1
	outMsg := transactionSendMsgCustomFwdFeeOutMsg(t, fwdFeeBytes)
	validation, err := transactionValidateRelaxedActionMessageCurrencies(outMsg)
	if err != nil {
		t.Fatalf("failed to validate relaxed action message: %v", err)
	}
	if validation.fwdFeeCanonical {
		t.Fatalf("generated fwd_fee is canonical, len=%d", fwdFeeBytes)
	}

	priceCell := buildTransactionMsgForwardPricesCell(t, 100, 1<<15)
	configRoot := referenceTransactionConfigRootWithOverrides(t,
		referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version),
		map[int32]*cell.Cell{
			int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
			int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
		},
	)
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outMsg})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	want := transactionSendMsgCustomFwdFeeExpectation(version, mode)
	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionSendMsgCustomFwdFeeOutMsg(t *testing.T, fwdFeeBytes uint) *cell.Cell {
	t.Helper()

	payload := make([]byte, fwdFeeBytes)
	return transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(1))
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	}, func(b *cell.Builder) {
		b.MustStoreUInt(uint64(fwdFeeBytes), 4)
		b.MustStoreSlice(payload, fwdFeeBytes*8)
	})
}

func transactionSendMsgCustomFwdFeeExpectation(version uint32, mode uint8) transactionActionPhaseExpectation {
	if version < 8 {
		return transactionActionPhaseExpectation{valid: true, resultCode: 34}
	}
	if mode&2 != 0 {
		return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
	}
	return transactionActionPhaseExpectation{valid: true, resultCode: 37}
}

func TestTVMCrossEmulatorTransactionSendMsgExtraFlagsGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	buildOutMsg := func(extraFlags func(*cell.Builder)) *cell.Cell {
		return transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(1_000_000_000))
		}, extraFlags, func(b *cell.Builder) {
			b.MustStoreBigCoins(big.NewInt(0))
		})
	}

	for _, tc := range []struct {
		name   string
		mode   uint8
		outMsg *cell.Cell
		want   func(uint32) transactionActionPhaseExpectation
	}{
		{
			name: "valid_flags",
			outMsg: buildOutMsg(func(b *cell.Builder) {
				b.MustStoreBigCoins(big.NewInt(3))
			}),
			want: func(uint32) transactionActionPhaseExpectation {
				return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
			},
		},
		{
			name: "forbidden_flags",
			outMsg: buildOutMsg(func(b *cell.Builder) {
				b.MustStoreBigCoins(big.NewInt(4))
			}),
			want: func(version uint32) transactionActionPhaseExpectation {
				if version >= 12 {
					return transactionActionPhaseExpectation{valid: true, resultCode: 45}
				}
				return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
			},
		},
		{
			name: "forbidden_flags_skipped",
			mode: 2,
			outMsg: buildOutMsg(func(b *cell.Builder) {
				b.MustStoreBigCoins(big.NewInt(4))
			}),
			want: func(version uint32) transactionActionPhaseExpectation {
				if version >= 12 {
					return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
				}
				return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
			},
		},
		{
			name: "non_canonical_zero_flags",
			outMsg: buildOutMsg(func(b *cell.Builder) {
				b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
			}),
			want: func(version uint32) transactionActionPhaseExpectation {
				if version < 8 {
					return transactionActionPhaseExpectation{valid: true, resultCode: 34}
				}
				if version >= 12 {
					return transactionActionPhaseExpectation{valid: true, resultCode: 45}
				}
				return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: tc.mode, Msg: tc.outMsg})
			code := makeTransactionInternalActionsCode(t, actions, newData)

			for _, version := range transactionVersionCrossEmulatorVersions(t) {
				t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

					goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
						Address:     tonopsTestAddr,
						Now:         now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}

					refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference transaction emulation failed: %v", err)
					}

					want := tc.want(version)
					assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
					assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
					if goRes.GasUsed != refRes.gasUsed {
						t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
					}
					if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
						t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
						t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
						t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
						t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
					}
					if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
						t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
						t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
						t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
					}
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionSendMsgExtraFlagsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0), uint8(3), uint16(0xA000+version), uint64(1_000_000_000))
		f.Add(uint8(version), uint8(1), uint8(0), uint8(4), uint16(0xB000+version), uint64(777))
		f.Add(uint8(version), uint8(2), uint8(1), uint8(0), uint16(0xC000+version), uint64(1))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint8(255), uint16(0xffff), uint64(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase, rawMode, rawFlag uint8, bodyTag uint16, rawAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionSendMsgExtraFlagsVersionParity(t, version, rawCase, rawMode, rawFlag, bodyTag, rawAmount)
	})
}

func assertTransactionSendMsgExtraFlagsVersionParity(t *testing.T, version uint32, rawCase, rawMode, rawFlag uint8, bodyTag uint16, rawAmount uint64) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	mode := uint8(0)
	if rawMode%2 != 0 {
		mode = 2
	}
	flag := uint64(rawFlag)
	outAmount := uint64(1_000_000_000 + rawAmount%1_000_000)
	outMsg := transactionSendMsgExtraFlagsOutMsg(t, rawCase, flag, outAmount)
	want := transactionSendMsgExtraFlagsExpectation(version, rawCase, mode)

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outMsg})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionSendMsgExtraFlagsOutMsg(t *testing.T, rawCase uint8, flag uint64, amount uint64) *cell.Cell {
	t.Helper()

	return transactionTestRelaxedInternalMessageWithFwdFee(func(b *cell.Builder) {
		b.MustStoreBigCoins(new(big.Int).SetUint64(amount))
	}, func(b *cell.Builder) {
		switch rawCase % 3 {
		case 0:
			b.MustStoreBigCoins(new(big.Int).SetUint64(flag % 4))
		case 1:
			b.MustStoreBigCoins(new(big.Int).SetUint64(flag%252 + 4))
		default:
			b.MustStoreUInt(1, 4).MustStoreUInt(0, 8)
		}
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	})
}

func transactionSendMsgExtraFlagsExpectation(version uint32, rawCase, mode uint8) transactionActionPhaseExpectation {
	switch rawCase % 3 {
	case 0:
		return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	case 1:
		if version >= 12 {
			if mode&2 != 0 {
				return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
			}
			return transactionActionPhaseExpectation{valid: true, resultCode: 45}
		}
		return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	default:
		if version < 8 {
			return transactionActionPhaseExpectation{valid: true, resultCode: 34}
		}
		if version >= 12 {
			if mode&2 != 0 {
				return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
			}
			return transactionActionPhaseExpectation{valid: true, resultCode: 45}
		}
		return transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	}
}

func TestTVMCrossEmulatorTransactionInvalidSourceMode2GlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	outMsg := buildTransactionOutboundInternalCellWithAddresses(
		t,
		address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x22}, 32)),
		tonopsTestAddr,
		1000,
		cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	)
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 2, Msg: outMsg})
	code := makeTransactionInternalActionsCode(t, actions, newData)

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			want := transactionActionPhaseExpectation{valid: true, resultCode: 35}
			if version >= 13 {
				want = transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionInvalidSourceDestinationMode2GlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0), uint8(0), uint16(0xA000+version))
		f.Add(uint8(version), uint8(0), uint8(1), uint8(1), uint16(0xB000+version))
		f.Add(uint8(version), uint8(1), uint8(0), uint8(2), uint16(0xC000+version))
		f.Add(uint8(version), uint8(1), uint8(1), uint8(3), uint16(0xD000+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, rawMode, rawWorkchain uint8, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionInvalidSourceDestinationMode2VersionParity(t, version, rawKind, rawMode, rawWorkchain, bodyTag)
	})
}

func assertTransactionInvalidSourceDestinationMode2VersionParity(t *testing.T, version uint32, rawKind, rawMode, rawWorkchain uint8, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).MustStoreUInt(uint64(bodyTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).MustStoreUInt(uint64(bodyTag)^0xffff, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	mode := uint8(0)
	if rawMode&1 != 0 {
		mode = 2
	}
	kind := rawKind % 2
	outMsg := transactionInvalidSourceDestinationOutMsg(t, kind, rawWorkchain, byte(bodyTag))
	want := transactionInvalidSourceDestinationMode2Expectation(version, kind, mode)

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outMsg})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionInvalidSourceDestinationOutMsg(t *testing.T, kind, rawBits, tag byte) *cell.Cell {
	t.Helper()

	src := address.NewAddressNone()
	dst := tonopsTestAddr
	if kind == 0 {
		src = address.NewAddressExt(0, 256, bytes.Repeat([]byte{tag ^ 0x22}, 32))
	} else {
		bits := uint(rawBits%255) + 1
		data := bytes.Repeat([]byte{tag ^ 0x36}, int((bits+7)/8))
		dst = address.NewAddressVar(0, address.MasterchainID, bits, data)
	}
	return buildTransactionOutboundInternalCellWithAddresses(
		t,
		src,
		dst,
		1000,
		cell.BeginCell().MustStoreUInt(uint64(tag), 8).EndCell(),
	)
}

func transactionInvalidSourceDestinationMode2Expectation(version uint32, kind, mode uint8) transactionActionPhaseExpectation {
	if kind == 0 {
		if mode&2 != 0 && version >= 13 {
			return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
		}
		return transactionActionPhaseExpectation{valid: true, resultCode: 35}
	}
	if mode&2 != 0 {
		if version >= 8 {
			return transactionActionPhaseExpectation{success: true, valid: true, skippedActions: 1}
		}
		return transactionActionPhaseExpectation{success: true, valid: true}
	}
	return transactionActionPhaseExpectation{valid: true, resultCode: 36}
}

func TestTVMCrossEmulatorTransactionExternalOutActionsGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name       string
		rawKind    uint8
		rawMode    uint8
		rawDstBits uint8
		bodyTag    uint16
	}{
		{
			name:       "valid_mode0",
			rawKind:    0,
			rawMode:    0,
			rawDstBits: 15,
			bodyTag:    0xE001,
		},
		{
			name:       "valid_mode1",
			rawKind:    0,
			rawMode:    1,
			rawDstBits: 255,
			bodyTag:    0xE002,
		},
		{
			name:       "invalid_std_destination_mode0",
			rawKind:    1,
			rawMode:    0,
			rawDstBits: 31,
			bodyTag:    0xE003,
		},
		{
			name:       "invalid_std_destination_mode2",
			rawKind:    1,
			rawMode:    1,
			rawDstBits: 63,
			bodyTag:    0xE004,
		},
		{
			name:       "invalid_mode32",
			rawKind:    2,
			rawMode:    0,
			rawDstBits: 7,
			bodyTag:    0xE005,
		},
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assertTransactionExternalOutActionVersionParity(t, version, tt.rawKind, tt.rawMode, tt.rawDstBits, tt.bodyTag)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionExternalOutActionsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0), uint8(15), uint16(0xE100+version))
		f.Add(uint8(version), uint8(0), uint8(1), uint8(255), uint16(0xE200+version))
		f.Add(uint8(version), uint8(1), uint8(0), uint8(31), uint16(0xE300+version))
		f.Add(uint8(version), uint8(1), uint8(1), uint8(63), uint16(0xE400+version))
		f.Add(uint8(version), uint8(2), uint8(0), uint8(7), uint16(0xE500+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawKind, rawMode, rawDstBits uint8, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionExternalOutActionVersionParity(t, version, rawKind, rawMode, rawDstBits, bodyTag)
	})
}

func assertTransactionExternalOutActionVersionParity(t *testing.T, version uint32, rawKind, rawMode, rawDstBits uint8, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).MustStoreUInt(uint64(bodyTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).MustStoreUInt(uint64(bodyTag)^0xffff, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	mode := transactionExternalOutActionMode(rawKind, rawMode)
	outMsg := transactionExternalOutActionMsg(t, rawKind, rawDstBits, bodyTag)
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})
	code := makeTransactionInternalSendCode(t, outMsg, newData, mode)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionExternalOutActionMode(rawKind, rawMode uint8) uint8 {
	switch rawKind % 3 {
	case 1:
		if rawMode&1 != 0 {
			return 2
		}
		return 0
	case 2:
		return 32
	default:
		if rawMode&1 != 0 {
			return 1
		}
		return 0
	}
}

func transactionExternalOutActionMsg(t *testing.T, rawKind, rawDstBits uint8, bodyTag uint16) *cell.Cell {
	t.Helper()

	bits := uint(rawDstBits) + 1
	dstData := bytes.Repeat([]byte{byte(bodyTag)}, int((bits+7)/8))
	dst := address.NewAddressExt(0, bits, dstData)
	if rawKind%3 == 1 {
		dst = tonopsTestAddr
	}

	msg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: dst,
		Body:    cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external outbound message: %v", err)
	}
	return msg
}

func TestTVMCrossEmulatorTransactionInboundIHRFeeGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		IHRFee:      tlb.FromNanoTONU(123),
		Body:        body,
	})

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			code := makeTransactionInternalSuccessCode(t, newData)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionInboundIHRFeeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint64(0), uint64(0), uint16(0xA000+version))
		f.Add(uint8(version), uint64(100), uint64(23), uint16(0xB000+version))
		f.Add(uint8(version), uint64(999_999), uint64(999_999), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawAmount, rawIHRFee uint64, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionInboundIHRFeeVersionParity(t, version, rawAmount, rawIHRFee, bodyTag)
	})
}

func assertTransactionInboundIHRFeeVersionParity(t *testing.T, version uint32, rawAmount, rawIHRFee uint64, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	amount := uint64(1_000_000_000 + rawAmount%1_000_000)
	ihrFee := rawIHRFee % 1_000_000
	wantCredit := amount
	if version < 12 {
		wantCredit += ihrFee
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(amount),
		IHRFee:      tlb.FromNanoTONU(ihrFee),
		Body:        body,
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionCredit(t, "go", goRes.TransactionCell, wantCredit)
	assertOrdinaryTransactionCredit(t, "reference", refRes.txCell, wantCredit)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func assertOrdinaryTransactionCredit(t *testing.T, side string, txCell *cell.Cell, want uint64) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("%s transaction description = %T, want ordinary", side, tx.Description)
	}
	if desc.CreditPhase == nil {
		t.Fatalf("%s transaction has no credit phase", side)
	}
	if got := desc.CreditPhase.Credit.Coins.Nano().Uint64(); got != want {
		t.Fatalf("%s credit = %d, want %d", side, got, want)
	}
}

func TestTVMCrossEmulatorTransactionReserveOriginalBalanceGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionReserveOriginalBalanceVersionParity(t, version, 500, 1_000, 0xCAFE)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionReserveOriginalBalanceGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint64(0), uint64(1), uint16(0xA000+version))
		f.Add(uint8(version), uint64(500), uint64(1_000), uint16(0xB000+version))
		f.Add(uint8(version), uint64(9_999), uint64(99), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawDuePayment, rawAmount uint64, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionReserveOriginalBalanceVersionParity(t, version, rawDuePayment, rawAmount, bodyTag)
	})
}

func assertTransactionReserveOriginalBalanceVersionParity(t *testing.T, version uint32, rawDuePayment, rawAmount uint64, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	duePayment := tlb.FromNanoTONU(rawDuePayment % 10_000)
	amount := rawAmount%100_000 + 1
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(amount),
		Body:        body,
	})
	actions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 4,
		Currency: tlb.CurrencyCollection{
			Coins: tlb.FromNanoTONU(0),
		},
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, 0, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   &duePayment,
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go data=%s\nreference data=%s", transactionCrossShardAccountDataDump(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountDataDump(t, refRes.shardCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionReserveExtraCurrencyGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	extra := makeTransactionExtraCurrencies(t, 7, 10)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	actions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 2,
		Currency: tlb.CurrencyCollection{
			Coins:           tlb.FromNanoTONU(0),
			ExtraCurrencies: makeTransactionExtraCurrencies(t, 7, 11),
		},
	})
	code := makeTransactionInternalActionsCode(t, actions, newData)

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			want := transactionActionPhaseExpectation{valid: true, resultCode: 38}
			if version == 9 {
				want = transactionActionPhaseExpectation{success: true, valid: true}
			} else if version >= 10 {
				want = transactionActionPhaseExpectation{valid: true, resultCode: 34}
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionReserveExtraCurrencyGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(7), uint64(10), uint64(1), uint16(0xA000+version))
		f.Add(uint8(version), uint8(1), uint64(1), uint64(1), uint16(0xB000+version))
		f.Add(uint8(version), uint8(31), uint64(999), uint64(100), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawExtraID uint8, rawExtraHave, rawExtraDelta uint64, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionReserveExtraCurrencyVersionParity(t, version, rawExtraID, rawExtraHave, rawExtraDelta, bodyTag)
	})
}

func assertTransactionReserveExtraCurrencyVersionParity(t *testing.T, version uint32, rawExtraID uint8, rawExtraHave, rawExtraDelta uint64, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	extraID := uint32(rawExtraID%31) + 1
	extraHave := rawExtraHave%1_000 + 1
	extraNeed := extraHave + rawExtraDelta%1_000 + 1
	extra := makeTransactionExtraCurrencies(t, extraID, extraHave)
	reserveExtra := makeTransactionExtraCurrencies(t, extraID, extraNeed)

	want := transactionActionPhaseExpectation{valid: true, resultCode: 38}
	if version == 9 {
		want = transactionActionPhaseExpectation{success: true, valid: true}
	} else if version >= 10 {
		want = transactionActionPhaseExpectation{valid: true, resultCode: 34}
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	actions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 2,
		Currency: tlb.CurrencyCollection{
			Coins:           tlb.FromNanoTONU(0),
			ExtraCurrencies: reserveExtra,
		},
	})
	code := makeTransactionInternalActionsCode(t, actions, newData)

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionReserveOriginalExtraCurrencyGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	extra := makeTransactionExtraCurrencies(t, 7, 3)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	outMsg := buildTransactionOutboundInternalCellWithExtra(t, 0, makeTransactionExtraCurrencies(t, 7, 1))
	actions := buildTransactionActionList(t,
		tlb.ActionReserveCurrency{
			Mode: 4,
			Currency: tlb.CurrencyCollection{
				Coins: tlb.FromNanoTONU(0),
			},
		},
		tlb.ActionSendMsg{
			Mode: 0,
			Msg:  outMsg,
		},
	)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			want := transactionActionPhaseExpectation{valid: true, resultCode: 38}
			if version >= 10 {
				want = transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
			}

			versionedConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot := referenceTransactionConfigRootWithOverrides(t, versionedConfigRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
				int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
			})
			shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionReserveOriginalExtraCurrencyGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(7), uint64(3), uint64(1), uint16(0xA000+version))
		f.Add(uint8(version), uint8(1), uint64(1), uint64(1), uint16(0xB000+version))
		f.Add(uint8(version), uint8(31), uint64(999), uint64(500), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawExtraID uint8, rawExtraHave, rawExtraSend uint64, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionReserveOriginalExtraCurrencyVersionParity(t, version, rawExtraID, rawExtraHave, rawExtraSend, bodyTag)
	})
}

func assertTransactionReserveOriginalExtraCurrencyVersionParity(t *testing.T, version uint32, rawExtraID uint8, rawExtraHave, rawExtraSend uint64, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	extraID := uint32(rawExtraID%31) + 1
	extraHave := rawExtraHave%1_000 + 1
	extraSend := rawExtraSend%extraHave + 1
	extra := makeTransactionExtraCurrencies(t, extraID, extraHave)
	outExtra := makeTransactionExtraCurrencies(t, extraID, extraSend)

	want := transactionActionPhaseExpectation{valid: true, resultCode: 38}
	if version >= 10 {
		want = transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	outMsg := buildTransactionOutboundInternalCellWithExtra(t, 0, outExtra)
	actions := buildTransactionActionList(t,
		tlb.ActionReserveCurrency{
			Mode: 4,
			Currency: tlb.CurrencyCollection{
				Coins: tlb.FromNanoTONU(0),
			},
		},
		tlb.ActionSendMsg{
			Mode: 0,
			Msg:  outMsg,
		},
	)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)

	versionedConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot := referenceTransactionConfigRootWithOverrides(t, versionedConfigRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})
	shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionSendExtraCurrencySizeGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	extra := makeTransactionExtraCurrencies(t, 7, 3)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	outMsg := buildTransactionOutboundInternalCellWithExtra(t, 0, makeTransactionExtraCurrencies(t, 7, 1))
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{
		Mode: 0,
		Msg:  outMsg,
	})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantCells := uint64(2)
			if version >= 10 {
				wantCells = 1
			}

			versionedConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot := referenceTransactionConfigRootWithOverrides(t, versionedConfigRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
				int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
			})
			shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			wantAction := transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
			assertOrdinaryTransactionActionMsgCells(t, "go", goRes.TransactionCell, wantCells)
			assertOrdinaryTransactionActionMsgCells(t, "reference", refRes.txCell, wantCells)
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionSendExtraCurrencySizeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(7), uint64(3), uint64(1), uint16(0xA000+version))
		f.Add(uint8(version), uint8(1), uint64(1), uint64(1), uint16(0xB000+version))
		f.Add(uint8(version), uint8(31), uint64(999), uint64(500), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawExtraID uint8, rawExtraHave, rawExtraSend uint64, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionSendExtraCurrencySizeVersionParity(t, version, rawExtraID, rawExtraHave, rawExtraSend, bodyTag)
	})
}

func assertTransactionSendExtraCurrencySizeVersionParity(t *testing.T, version uint32, rawExtraID uint8, rawExtraHave, rawExtraSend uint64, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	extraID := uint32(rawExtraID%31) + 1
	extraHave := rawExtraHave%1_000 + 1
	extraSend := rawExtraSend%extraHave + 1
	extra := makeTransactionExtraCurrencies(t, extraID, extraHave)
	outExtra := makeTransactionExtraCurrencies(t, extraID, extraSend)
	wantCells := uint64(2)
	if version >= 10 {
		wantCells = 1
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	outMsg := buildTransactionOutboundInternalCellWithExtra(t, 0, outExtra)
	actions := buildTransactionActionList(t, tlb.ActionSendMsg{
		Mode: 0,
		Msg:  outMsg,
	})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	priceCell := buildTransactionMsgForwardPricesCell(t, 0, 0)

	versionedConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot := referenceTransactionConfigRootWithOverrides(t, versionedConfigRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamMsgForwardPricesBasechain):   priceCell,
		int32(tlb.ConfigParamMsgForwardPricesMasterchain): priceCell,
	})
	shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	wantAction := transactionActionPhaseExpectation{success: true, valid: true, messagesCreated: 1}
	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
	assertOrdinaryTransactionActionMsgCells(t, "go", goRes.TransactionCell, wantCells)
	assertOrdinaryTransactionActionMsgCells(t, "reference", refRes.txCell, wantCells)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionStorageDuePaymentGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	})
	largeDueLimit := buildTransactionGasLimitsCell(t, 1_000_000, 1_000_000)

	for _, tc := range []struct {
		name          string
		balance       uint64
		storageFee    uint64
		oldDue        uint64
		threshold     uint32
		wantBeforeDue uint64
		wantFromDue   uint64
	}{
		{
			name:        "unpaid_due",
			balance:     50,
			storageFee:  100,
			threshold:   4,
			wantFromDue: 50,
		},
		{
			name:          "collected_old_due",
			balance:       1000,
			storageFee:    77,
			oldDue:        77,
			threshold:     7,
			wantBeforeDue: 77,
		},
	} {
		tc := tc
		for _, version := range transactionVersionCrossEmulatorVersions(t) {
			version := version
			t.Run(fmt.Sprintf("%s/global_v%d", tc.name, version), func(t *testing.T) {
				wantDue := tc.wantBeforeDue
				if version >= tc.threshold {
					wantDue = tc.wantFromDue
				}

				storageInfo := tlb.StorageInfo{
					StorageUsed: tlb.StorageUsed{
						CellsUsed: big.NewInt(1),
						BitsUsed:  big.NewInt(0),
					},
					StorageExtra: tlb.StorageExtraNone{},
					LastPaid:     now - 1,
					DuePayment:   transactionCoinsPtr(new(big.Int).SetUint64(tc.oldDue)),
				}
				configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
				configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
					int32(tlb.ConfigParamStoragePrices):        transactionVersionStoragePricesCell(t, tc.storageFee),
					int32(tlb.ConfigParamGasPricesBasechain):   largeDueLimit,
					int32(tlb.ConfigParamGasPricesMasterchain): largeDueLimit,
				})
				shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, tc.balance, storageInfo)

				goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
					Address:     tonopsTestAddr,
					Now:         now,
					BlockLT:     transactionTestLogicalTime,
					LogicalTime: transactionTestLogicalTime,
					RandSeed:    append([]byte(nil), tonopsTestSeed...),
					ConfigRoot:  configRoot,
				})
				if err != nil {
					t.Fatalf("go transaction emulation failed: %v", err)
				}

				refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
				if err != nil {
					t.Fatalf("reference transaction emulation failed: %v", err)
				}

				goDue := transactionCrossShardDuePayment(t, goRes.NextAccount.ShardAccountCell())
				refDue := transactionCrossShardDuePayment(t, refRes.shardCell)
				if goDue != wantDue || refDue != wantDue {
					t.Fatalf("due payment mismatch: go=%d reference=%d want=%d", goDue, refDue, wantDue)
				}
				if goRes.GasUsed != refRes.gasUsed {
					t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
				}
				if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
					t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
					t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
					t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
				}
				if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
					t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
					t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
				}
			})
		}
	}
}

func FuzzTVMCrossEmulatorTransactionStorageDuePaymentGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint64(50), uint64(100), uint64(0), uint16(0xA000+version))
		f.Add(uint8(version), uint8(0), uint64(1), uint64(2), uint64(17), uint16(0xB000+version))
		f.Add(uint8(version), uint8(1), uint64(1_000), uint64(77), uint64(77), uint16(0xC000+version))
		f.Add(uint8(version), uint8(1), uint64(500), uint64(1), uint64(333), uint16(0xD000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, rawBalance, rawStorageFee, rawOldDue uint64, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionStorageDuePaymentVersionParity(t, version, rawCase, rawBalance, rawStorageFee, rawOldDue, bodyTag)
	})
}

func assertTransactionStorageDuePaymentVersionParity(t *testing.T, version uint32, rawCase uint8, rawBalance, rawStorageFee, rawOldDue uint64, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	balance := rawBalance % 2_000
	storageFee := rawStorageFee % 1_000
	if storageFee == 0 {
		storageFee = 1
	}
	oldDue := rawOldDue % 1_000
	wantDue := oldDue

	switch rawCase % 2 {
	case 0:
		if balance >= storageFee {
			balance = storageFee - 1
		}
		if storageFee == 1 {
			balance = 0
		}
		oldDue = 0
		if version >= 4 {
			wantDue = storageFee - balance
		} else {
			wantDue = 0
		}
	case 1:
		if balance < storageFee {
			balance = storageFee
		}
		remaining := balance - storageFee
		if version >= 4 && remaining < oldDue {
			wantDue = oldDue - remaining
		} else if version >= 7 {
			wantDue = 0
		}
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})
	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 1,
		DuePayment:   transactionCoinsPtr(new(big.Int).SetUint64(oldDue)),
	}
	largeDueLimit := buildTransactionGasLimitsCell(t, 1_000_000, 1_000_000)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamStoragePrices):        transactionVersionStoragePricesCell(t, storageFee),
		int32(tlb.ConfigParamGasPricesBasechain):   largeDueLimit,
		int32(tlb.ConfigParamGasPricesMasterchain): largeDueLimit,
	})
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, balance, storageInfo)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goDue := transactionCrossShardDuePayment(t, goRes.NextAccount.ShardAccountCell())
	refDue := transactionCrossShardDuePayment(t, refRes.shardCell)
	if goDue != wantDue || refDue != wantDue {
		t.Fatalf("due payment mismatch: go=%d reference=%d want=%d", goDue, refDue, wantDue)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionFrozenHashEqualsAddressGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	stateInit := &tlb.StateInit{
		Code: code,
		Data: origData,
	}
	addr := stateInit.CalcAddress(0)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	})
	storagePrices := transactionVersionStoragePricesCell(t, 100)
	gasLimits := buildTransactionGasLimitsCell(t, 10, 1_000_000)
	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 1,
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantTxStatus := tlb.AccountStatus(tlb.AccountStatusFrozen)
			if version >= 13 {
				wantTxStatus = tlb.AccountStatusUninit
			}
			wantAccountStatus := tlb.AccountStatus(tlb.AccountStatusUninit)

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamStoragePrices):        storagePrices,
				int32(tlb.ConfigParamGasPricesBasechain):   gasLimits,
				int32(tlb.ConfigParamGasPricesMasterchain): gasLimits,
			})
			shard := buildTransactionTestShardAccountWithStorageInfo(t, addr, code, origData, 50, storageInfo)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     addr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			goTxStatus := transactionCrossEndStatus(t, goRes.TransactionCell)
			refTxStatus := transactionCrossEndStatus(t, refRes.txCell)
			goAccountStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
			refAccountStatus := transactionCrossShardStatus(t, refRes.shardCell)
			if goTxStatus != wantTxStatus || refTxStatus != wantTxStatus {
				t.Fatalf("tx status mismatch: go=%s reference=%s want=%s", goTxStatus, refTxStatus, wantTxStatus)
			}
			if goAccountStatus != wantAccountStatus || refAccountStatus != wantAccountStatus {
				t.Fatalf("account status mismatch: go=%s reference=%s want=%s", goAccountStatus, refAccountStatus, wantAccountStatus)
			}
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionFrozenHashEqualsAddressGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version), uint8(0), uint8(0))
		f.Add(uint8(version), true, uint16(0xD000+version), uint16(0xE000+version), uint16(0xF000+version), uint8(17), uint8(23))
	}
	f.Add(uint8(255), true, uint16(0), uint16(0xffff), uint16(0x1234), uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, mismatch bool, codeTag, dataTag, bodyTag uint16, rawBalance, rawStoragePrice uint8) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionFrozenHashEqualsAddressVersionParity(t, version, mismatch, codeTag, dataTag, bodyTag, rawBalance, rawStoragePrice)
	})
}

func assertTransactionFrozenHashEqualsAddressVersionParity(t *testing.T, version uint32, mismatch bool, codeTag, dataTag, bodyTag uint16, rawBalance, rawStoragePrice uint8) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(dataTag^0xffff), 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	if codeTag != 0 {
		code = makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(uint64(codeTag), 16).EndCell())
	}
	stateInit := &tlb.StateInit{
		Code: code,
		Data: origData,
	}
	addr := stateInit.CalcAddress(0)
	if mismatch {
		addrData := append([]byte(nil), addr.Data()...)
		addrData[31] ^= 1
		addr = address.NewAddress(0, 0, addrData)
	}

	wantTxStatus := tlb.AccountStatus(tlb.AccountStatusFrozen)
	wantAccountStatus := tlb.AccountStatus(tlb.AccountStatusFrozen)
	if !mismatch {
		wantAccountStatus = tlb.AccountStatusUninit
		if version >= 13 {
			wantTxStatus = tlb.AccountStatusUninit
		}
	}

	balance := uint64(rawBalance % 50)
	storagePrice := uint64(rawStoragePrice%200) + 100
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamStoragePrices):        transactionVersionStoragePricesCell(t, storagePrice),
		int32(tlb.ConfigParamGasPricesBasechain):   buildTransactionGasLimitsCell(t, 10, 1_000_000),
		int32(tlb.ConfigParamGasPricesMasterchain): buildTransactionGasLimitsCell(t, 10, 1_000_000),
	})
	shard := buildTransactionTestShardAccountWithStorageInfo(t, addr, code, origData, balance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 1,
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goTxStatus := transactionCrossEndStatus(t, goRes.TransactionCell)
	refTxStatus := transactionCrossEndStatus(t, refRes.txCell)
	goAccountStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
	refAccountStatus := transactionCrossShardStatus(t, refRes.shardCell)
	if goTxStatus != wantTxStatus || refTxStatus != wantTxStatus {
		t.Fatalf("tx status mismatch: go=%s reference=%s want=%s mismatch=%t", goTxStatus, refTxStatus, wantTxStatus, mismatch)
	}
	if goAccountStatus != wantAccountStatus || refAccountStatus != wantAccountStatus {
		t.Fatalf("account status mismatch: go=%s reference=%s want=%s mismatch=%t", goAccountStatus, refAccountStatus, wantAccountStatus, mismatch)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionFrozenExternalStateHashGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionFrozenExternalStateHashVersionParity(t, version, false, 0xA800, 0xB800, 0xC800)
			assertTransactionFrozenExternalStateHashVersionParity(t, version, true, 0xD800, 0xE800, 0xF800)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionFrozenExternalStateHashGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), false, uint16(0xA800+version), uint16(0xB800+version), uint16(0xC800+version))
		f.Add(uint8(version), true, uint16(0xD800+version), uint16(0xE800+version), uint16(0xF800+version))
	}
	f.Add(uint8(255), false, uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion uint8, hashMismatch bool, codeTag, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionFrozenExternalStateHashVersionParity(t, version, hashMismatch, codeTag, dataTag, bodyTag)
	})
}

func assertTransactionFrozenExternalStateHashVersionParity(t *testing.T, version uint32, hashMismatch bool, codeTag, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	addr := tonopsTestAddr
	origData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(dataTag^0xffff), 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	if codeTag != 0 {
		code = makeTransactionExternalSuccessCode(t, cell.BeginCell().MustStoreUInt(uint64(codeTag), 16).EndCell())
	}
	stateInit := &tlb.StateInit{
		Code: code,
		Data: origData,
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init: %v", err)
	}
	stateHash := stateCell.Hash()
	if bytes.Equal(stateHash, addr.Data()) {
		t.Fatalf("test fixture state hash unexpectedly matches address: %x", stateHash)
	}
	if hashMismatch {
		stateHash = append([]byte(nil), stateHash...)
		stateHash[0] ^= 1
	}

	msg := mustTransactionMsgCell(t, &tlb.ExternalMessage{
		DstAddr:   addr,
		StateInit: stateInit,
		Body:      cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestFrozenShardAccount(t, addr, stateHash, walletSendTestBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})

	goRes, goErr := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)

	wantRejected := hashMismatch || version < 8
	if wantRejected {
		if goErr != nil {
			t.Fatalf("go rejected external state init with error: %v", goErr)
		}
		if goRes == nil || goRes.Accepted || goRes.TransactionCell != nil || goRes.NextAccount.ShardAccountCell() != nil {
			t.Fatalf("go external state init rejection result = %+v, want no accepted transaction", goRes)
		}
		if refErr == nil {
			t.Fatalf("reference accepted rejected external state init: tx=%s", refRes.txCell.Dump())
		}
		return
	}

	if goErr != nil {
		t.Fatalf("go transaction emulation failed: %v", goErr)
	}
	if refErr != nil {
		t.Fatalf("reference transaction emulation failed: %v", refErr)
	}
	if !goRes.Accepted {
		t.Fatal("go external state init transaction was not accepted")
	}
	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	assertTransactionFrozenExternalStateHashAccepted(t, "go", goRes.TransactionCell, goRes.NextAccount.ShardAccountCell())
	assertTransactionFrozenExternalStateHashAccepted(t, "reference", refRes.txCell, refRes.shardCell)
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func assertTransactionFrozenExternalStateHashAccepted(t *testing.T, side string, txCell, shardCell *cell.Cell) {
	t.Helper()

	if got := transactionCrossShardStatus(t, shardCell); got != tlb.AccountStatusActive {
		t.Fatalf("%s shard status = %s, want active", side, got)
	}
	phase := mustTransactionComputePhase(t, txCell)
	vmPhase, ok := phase.Phase.(tlb.ComputePhaseVM)
	if !ok {
		t.Fatalf("%s compute phase = %T, want vm", side, phase.Phase)
	}
	if !vmPhase.Success {
		t.Fatalf("%s compute phase success=false", side)
	}
}

func TestTVMCrossEmulatorTransactionStateInitFixedPrefixGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	depth := uint64(9)
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  makeTransactionInternalSuccessCode(t, newData),
		Data:  cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init cell: %v", err)
	}
	addrData := append([]byte(nil), stateCell.Hash()...)
	addrData[0] ^= 0x80
	addr := address.NewAddress(0, 0, addrData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		StateInit:   stateInit,
		Body:        cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell(),
	})
	shard := buildTransactionTestUninitShardAccount(t, addr, 0, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantAccount := tlb.AccountStatus(tlb.AccountStatusActive)
			if version >= 10 {
				wantAccount = tlb.AccountStatusUninit
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamSizeLimits): transactionVersionSizeLimitsCell(t, 2),
			})

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     addr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			goAccountStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
			refAccountStatus := transactionCrossShardStatus(t, refRes.shardCell)
			if goAccountStatus != wantAccount || refAccountStatus != wantAccount {
				t.Fatalf("account status mismatch: go=%s reference=%s want=%s", goAccountStatus, refAccountStatus, wantAccount)
			}
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionStateInitFixedPrefixGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(7), uint8(0), uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version))
		f.Add(uint8(version), uint8(9), uint8(1), uint16(0xA100+version), uint16(0xB100+version), uint16(0xC100+version))
		f.Add(uint8(version), uint8(9), uint8(2), uint16(0xA200+version), uint16(0xB200+version), uint16(0xC200+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawDepth, rawMismatch uint8, codeTag, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		depth := uint64(rawDepth % 13)
		assertTransactionStateInitFixedPrefixVersionParity(t, version, depth, rawMismatch, codeTag, dataTag, bodyTag)
	})
}

func FuzzTVMCrossEmulatorTransactionNoStateSkipReasonGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint64(1), uint64(0), uint16(0xA000+version), uint16(0xB000+version))
		f.Add(uint8(version), uint8(1), uint64(7), uint64(11), uint16(0xC000+version), uint16(0xD000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, rawAmount, rawBalance uint64, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionNoStateSkipReasonVersionParity(t, version, rawCase, rawAmount, rawBalance, dataTag, bodyTag)
	})
}

func assertTransactionNoStateSkipReasonVersionParity(t *testing.T, version uint32, rawCase uint8, rawAmount, rawBalance uint64, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	amount := 1_000_000_000 + rawAmount%1_000_000
	balance := rawBalance % 1_000_000
	addr := tonopsTestAddr
	wantSkip := tlb.ComputeSkipReasonNoState

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      rawCase&0x80 != 0,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(amount),
		Body: cell.BeginCell().
			MustStoreUInt(uint64(dataTag), 16).
			MustStoreUInt(uint64(bodyTag), 16).
			EndCell(),
	})

	var shard *tlb.ShardAccount
	if rawCase&1 == 0 {
		shard = buildTransactionTestNoneShardAccount(t)
	} else {
		shard = buildTransactionTestUninitShardAccount(t, addr, balance, tlb.StorageInfo{
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now,
		})
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionComputeSkipped(t, "go", goRes.TransactionCell, wantSkip)
	assertOrdinaryTransactionComputeSkipped(t, "reference", refRes.txCell, wantSkip)
	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
}

func FuzzTVMCrossEmulatorTransactionStateInitNoCodeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint64(1), uint64(0), uint16(0xA000+version), uint16(0xB000+version))
		f.Add(uint8(version), uint8(3), uint64(7), uint64(11), uint16(0xC000+version), uint16(0xD000+version))
		f.Add(uint8(version), uint8(6), uint64(13), uint64(17), uint16(0xE000+version), uint16(0xF000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, rawAmount, rawBalance uint64, dataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionStateInitNoCodeVersionParity(t, version, rawCase, rawAmount, rawBalance, dataTag, bodyTag)
	})
}

func assertTransactionStateInitNoCodeVersionParity(t *testing.T, version uint32, rawCase uint8, rawAmount, rawBalance uint64, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	stateInit := &tlb.StateInit{}
	if rawCase&4 != 0 {
		stateInit.Data = cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}
	addr := address.NewAddress(0, 0, stateCell.Hash())
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      rawCase&2 != 0,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000 + rawAmount%1_000_000),
		StateInit:   stateInit,
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	var shard *tlb.ShardAccount
	if rawCase&1 == 0 {
		shard = buildTransactionTestNoneShardAccount(t)
	} else {
		shard = buildTransactionTestUninitShardAccount(t, addr, rawBalance%1_000_000, tlb.StorageInfo{
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now,
		})
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.ExitCode != -vmerr.CodeOutOfGas {
		t.Fatalf("exit code=%d, want missing-code exit", goRes.ExitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func assertTransactionStateInitFixedPrefixVersionParity(t *testing.T, version uint32, depth uint64, rawMismatch uint8, codeTag, dataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	newData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	stateInit := &tlb.StateInit{
		Depth: &depth,
		Code:  makeTransactionInternalSuccessCode(t, newData),
		Data:  cell.BeginCell().MustStoreUInt(uint64(codeTag), 16).EndCell(),
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init cell: %v", err)
	}
	addrData := append([]byte(nil), stateCell.Hash()...)
	switch rawMismatch % 3 {
	case 1:
		if depth > 0 {
			flipTransactionFuzzBit(addrData, 0)
		}
	case 2:
		flipTransactionFuzzBit(addrData, depth)
	}
	addr := address.NewAddress(0, 0, addrData)
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		StateInit:   stateInit,
		Body:        body,
	})
	shard := buildTransactionTestUninitShardAccount(t, addr, 0, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})

	wantAccount := tlb.AccountStatus(tlb.AccountStatusActive)
	if rawMismatch%3 == 2 || (version >= 10 && depth > transactionDefaultSizeLimits().maxAccFixedPrefixLength) {
		wantAccount = tlb.AccountStatusUninit
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSizeLimits): transactionVersionSizeLimitsCell(t, 2),
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goAccountStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
	refAccountStatus := transactionCrossShardStatus(t, refRes.shardCell)
	if goAccountStatus != wantAccount || refAccountStatus != wantAccount {
		t.Fatalf("account status mismatch: go=%s reference=%s want=%s", goAccountStatus, refAccountStatus, wantAccount)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionMasterchainPublicLibrariesDeployGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionMasterchainPublicLibrariesDeployVersionParity(t, version, 0xA2, 0xAAAA, 0xBEEF, 0xCAFE)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionMasterchainPublicLibrariesDeployGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0xA2), uint16(0xAAAA), uint16(0xBEEF), uint16(0xA000+version))
		f.Add(uint8(version), uint8(0xE5), uint16(0x1000+version), uint16(0x2000+version), uint16(0xB000+version))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff), uint16(0xffff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, libTag uint8, stateDataTag, codeDataTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionMasterchainPublicLibrariesDeployVersionParity(t, version, libTag, stateDataTag, codeDataTag, bodyTag)
	})
}

func assertTransactionMasterchainPublicLibrariesDeployVersionParity(t *testing.T, version uint32, libTag uint8, stateDataTag, codeDataTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	lib := cell.BeginCell().MustStoreUInt(uint64(libTag), 8).EndCell()
	stateInit := &tlb.StateInit{
		Code: makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(uint64(codeDataTag), 16).EndCell()),
		Data: cell.BeginCell().MustStoreUInt(uint64(stateDataTag), 16).EndCell(),
		Lib:  buildTransactionV13LibraryDict(t, lib, true),
	}
	addr := stateInit.CalcAddress(0xFF)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		StateInit:   stateInit,
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})
	shard := buildTransactionTestUninitShardAccount(t, addr, 0, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionComputeSkipped(t, "go", goRes.TransactionCell, tlb.ComputeSkipReasonBadState)
	assertOrdinaryTransactionComputeSkipped(t, "reference", refRes.txCell, tlb.ComputeSkipReasonBadState)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionPrecompiledNoGasGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionPrecompiledNoGasVersionParity(t, version, 0xCAFE)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionPrecompiledNoGasGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(0xCAFE+version))
	}
	f.Add(uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionPrecompiledNoGasVersionParity(t, version, bodyTag)
	})
}

func assertTransactionPrecompiledNoGasVersionParity(t *testing.T, version uint32, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasPrice:      1,
		GasLimit:      10,
		BlockGasLimit: 10,
	})
	if err != nil {
		t.Fatalf("failed to build gas limits: %v", err)
	}
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamGasPricesBasechain):        gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain):      gasCell,
		int32(tlb.ConfigParamPrecompiledContracts):      buildTransactionV13PrecompiledConfig(t, code, 11),
		int32(tlb.ConfigParamMsgForwardPricesBasechain): buildTransactionMsgForwardPricesCell(t, 0, 0),
	})
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionComputeSkipped(t, "go", goRes.TransactionCell, tlb.ComputeSkipReasonNoGas)
	assertOrdinaryTransactionComputeSkipped(t, "reference", refRes.txCell, tlb.ComputeSkipReasonNoGas)
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func assertOrdinaryTransactionActionMsgCells(t *testing.T, side string, txCell *cell.Cell, want uint64) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("%s transaction description = %T, want ordinary", side, tx.Description)
	}
	if desc.ActionPhase == nil {
		t.Fatalf("%s transaction has no action phase", side)
	}
	if got := desc.ActionPhase.TotalMsgSize.Cells.Uint64(); got != want {
		t.Fatalf("%s action total message cells = %d, want %d", side, got, want)
	}
}

func assertOrdinaryTransactionComputeSkipped(t *testing.T, side string, txCell *cell.Cell, want tlb.ComputeSkipReasonType) {
	t.Helper()

	phase := mustTransactionComputePhase(t, txCell)
	skipped, ok := phase.Phase.(tlb.ComputePhaseSkipped)
	if !ok {
		t.Fatalf("%s compute phase = %T, want skipped %s", side, phase.Phase, want)
	}
	if skipped.Reason.Type != want {
		t.Fatalf("%s compute skip = %s, want %s", side, skipped.Reason.Type, want)
	}
}

func TestTVMCrossEmulatorTransactionStorageDeletionDestroyedGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := transactionPhaseInternalMessage(t, body, 0, false, 0)

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantDestroyed := version >= 13

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamGasPricesBasechain):   buildTransactionGasLimitsCell(t, 100, 500),
				int32(tlb.ConfigParamGasPricesMasterchain): buildTransactionGasLimitsCell(t, 100, 500),
			})
			shard := buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 0, tlb.StorageInfo{
				StorageUsed: tlb.StorageUsed{
					CellsUsed: big.NewInt(0),
					BitsUsed:  big.NewInt(0),
				},
				StorageExtra: tlb.StorageExtraNone{},
				LastPaid:     now - 60,
				DuePayment:   transactionCoinsPtr(big.NewInt(501)),
			})

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			goDestroyed := transactionCrossOrdinaryDestroyed(t, goRes.TransactionCell)
			refDestroyed := transactionCrossOrdinaryDestroyed(t, refRes.txCell)
			if goDestroyed != wantDestroyed || refDestroyed != wantDestroyed {
				t.Fatalf("destroyed mismatch: go=%t reference=%t want=%t", goDestroyed, refDestroyed, wantDestroyed)
			}
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionStorageDeletionDestroyedGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint64(500), false, uint16(0xA000+version))
		f.Add(uint8(version), uint8(1), uint64(500), false, uint16(0xB000+version))
		f.Add(uint8(version), uint8(2), uint64(500), false, uint16(0xC000+version))
		f.Add(uint8(version), uint8(2), uint64(500), true, uint16(0xD000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0), true, uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawDueCase uint8, rawDeleteDue uint64, hasExtra bool, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionStorageDeletionDestroyedVersionParity(t, version, rawDueCase, rawDeleteDue, hasExtra, bodyTag)
	})
}

func assertTransactionStorageDeletionDestroyedVersionParity(t *testing.T, version uint32, rawDueCase uint8, rawDeleteDue uint64, hasExtra bool, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	deleteDue := rawDeleteDue%1_000 + 1
	duePayment := transactionCrossStorageDeletionDue(rawDueCase, deleteDue)
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := transactionPhaseInternalMessage(t, body, 0, false, 0)

	var extra *cell.Dictionary
	if hasExtra {
		extra = makeTransactionExtraCurrencies(t, 7, 11)
	}
	wantDeleted := duePayment > deleteDue && !hasExtra
	wantDestroyed := wantDeleted && version >= 13
	wantAccount := tlb.AccountStatus(tlb.AccountStatusNonExist)
	if hasExtra {
		wantAccount = tlb.AccountStatusUninit
	}
	if wantDeleted {
		wantAccount = tlb.AccountStatusNonExist
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamGasPricesBasechain):   buildTransactionGasLimitsCell(t, 100, deleteDue),
		int32(tlb.ConfigParamGasPricesMasterchain): buildTransactionGasLimitsCell(t, 100, deleteDue),
	})
	shard := buildTransactionCrossUninitShardAccountWithExtraCurrencies(t, tonopsTestAddr, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   transactionCoinsPtr(new(big.Int).SetUint64(duePayment)),
	}, extra)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goDestroyed := transactionCrossOrdinaryDestroyed(t, goRes.TransactionCell)
	refDestroyed := transactionCrossOrdinaryDestroyed(t, refRes.txCell)
	if goDestroyed != wantDestroyed || refDestroyed != wantDestroyed {
		t.Fatalf("destroyed mismatch: go=%t reference=%t want=%t", goDestroyed, refDestroyed, wantDestroyed)
	}
	goAccountStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
	refAccountStatus := transactionCrossShardStatus(t, refRes.shardCell)
	if goAccountStatus != wantAccount || refAccountStatus != wantAccount {
		t.Fatalf("account status mismatch: go=%s reference=%s want=%s", goAccountStatus, refAccountStatus, wantAccount)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func transactionCrossStorageDeletionDue(rawCase uint8, deleteDue uint64) uint64 {
	switch rawCase % 4 {
	case 0:
		if deleteDue == 0 {
			return 0
		}
		return deleteDue - 1
	case 1:
		return deleteDue
	case 2:
		return deleteDue + 1
	default:
		return deleteDue + 50
	}
}

func buildTransactionCrossUninitShardAccountWithExtraCurrencies(t *testing.T, addr *address.Address, storageInfo tlb.StorageInfo, extra *cell.Dictionary) *tlb.ShardAccount {
	t.Helper()

	accountCell, err := tlb.ToCell(&tlb.AccountState{
		IsValid:     true,
		Address:     addr,
		StorageInfo: storageInfo,
		AccountStorage: tlb.AccountStorage{
			Status:          tlb.AccountStatusUninit,
			Balance:         tlb.FromNanoTONU(0),
			ExtraCurrencies: extra,
		},
	})
	if err != nil {
		t.Fatalf("failed to build uninit account state: %v", err)
	}

	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	}
}

func TestTVMCrossEmulatorTransactionStorageExtraDictHashGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).
		EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantStorage := version >= 11

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamSizeLimits): transactionVersionSizeLimitsCell(t, 2),
			})
			code := makeTransactionInternalSuccessCode(t, newData)
			shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, origData, walletSendTestBalance, tlb.StorageInfo{
				StorageUsed: tlb.StorageUsed{
					CellsUsed: big.NewInt(0),
					BitsUsed:  big.NewInt(0),
				},
				StorageExtra: tlb.StorageExtraNone{},
				LastPaid:     now,
			})

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			goHasStorageExtra := transactionStorageExtraIsInfo(t, transactionCrossShardStorageExtra(t, goRes.NextAccount.ShardAccountCell()))
			refHasStorageExtra := transactionStorageExtraIsInfo(t, transactionCrossShardStorageExtra(t, refRes.shardCell))
			if goHasStorageExtra != wantStorage || refHasStorageExtra != wantStorage {
				t.Fatalf("storage extra info mismatch: go=%t reference=%t want=%t", goHasStorageExtra, refHasStorageExtra, wantStorage)
			}
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionStorageExtraDictHashGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(1), uint8(0), false, uint16(0xA000+version))
		f.Add(uint8(version), uint8(2), uint8(2), false, uint16(0xB000+version))
		f.Add(uint8(version), uint8(2), uint8(4), true, uint16(0xC000+version))
		f.Add(uint8(version), uint8(7), uint8(0), false, uint16(0xD000+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), true, uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawThreshold, rawRefs uint8, masterchain bool, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionStorageExtraDictHashVersionParity(t, version, rawThreshold, rawRefs, masterchain, bodyTag)
	})
}

func assertTransactionStorageExtraDictHashVersionParity(t *testing.T, version uint32, rawThreshold, rawRefs uint8, masterchain bool, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	threshold := uint32(rawThreshold%8) + 1
	refs := int(rawRefs % 5)
	addr := tonopsTestAddr
	if masterchain {
		addr = address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x5A}, 32))
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	data := cell.BeginCell().
		MustStoreUInt(uint64(rawThreshold), 8).
		MustStoreUInt(uint64(rawRefs), 8)
	for i := 0; i < refs; i++ {
		data.MustStoreRef(cell.BeginCell().
			MustStoreUInt(uint64(bodyTag), 16).
			MustStoreUInt(uint64(i+1), uint(1+i%7)).
			EndCell())
	}
	newData := data.EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSizeLimits): transactionVersionSizeLimitsCell(t, threshold),
	})
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, addr, code, origData, walletSendTestBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goCells := transactionCrossShardStorageUsedCells(t, goRes.NextAccount.ShardAccountCell())
	refCells := transactionCrossShardStorageUsedCells(t, refRes.shardCell)
	if goCells != refCells {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("storage cells mismatch: go=%d reference=%d", goCells, refCells)
	}

	wantStorage := version >= 11 && !masterchain && goCells >= uint64(threshold)
	goHasStorageExtra := transactionStorageExtraIsInfo(t, transactionCrossShardStorageExtra(t, goRes.NextAccount.ShardAccountCell()))
	refHasStorageExtra := transactionStorageExtraIsInfo(t, transactionCrossShardStorageExtra(t, refRes.shardCell))
	if goHasStorageExtra != wantStorage || refHasStorageExtra != wantStorage {
		t.Fatalf("storage extra info mismatch: go=%t reference=%t want=%t cells=%d threshold=%d masterchain=%t", goHasStorageExtra, refHasStorageExtra, wantStorage, goCells, threshold, masterchain)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
}

func TestTVMCrossEmulatorTransactionStorageExtraCurrencyUsageGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionStorageExtraCurrencyUsageVersionParity(t, version, 7, 11, 2, 0xCAFE)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionStorageExtraCurrencyUsageGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(1), uint64(1), uint8(0), uint16(0xA000+version))
		f.Add(uint8(version), uint8(7), uint64(11), uint8(2), uint16(0xB000+version))
		f.Add(uint8(version), uint8(31), uint64(999), uint8(4), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawExtraID uint8, rawExtraAmount uint64, rawRefs uint8, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionStorageExtraCurrencyUsageVersionParity(t, version, rawExtraID, rawExtraAmount, rawRefs, bodyTag)
	})
}

func assertTransactionStorageExtraCurrencyUsageVersionParity(t *testing.T, version uint32, rawExtraID uint8, rawExtraAmount uint64, rawRefs uint8, bodyTag uint16) {
	t.Helper()

	extraID := uint32(rawExtraID%31) + 1
	extraAmount := rawExtraAmount%1_000 + 1
	withExtra := transactionStorageExtraCurrencyUsageRun(t, version, makeTransactionExtraCurrencies(t, extraID, extraAmount), rawRefs, bodyTag)
	withoutExtra := transactionStorageExtraCurrencyUsageRun(t, version, nil, rawRefs, bodyTag)

	if version >= 10 {
		if withExtra != withoutExtra {
			t.Fatalf("v%d storage usage with extra = %+v, without extra = %+v, want equal from v10", version, withExtra, withoutExtra)
		}
		return
	}
	if withExtra == withoutExtra {
		t.Fatalf("v%d storage usage with extra = %+v, without extra = %+v, want different before v10", version, withExtra, withoutExtra)
	}
}

func transactionStorageExtraCurrencyUsageRun(t *testing.T, version uint32, extra *cell.Dictionary, rawRefs uint8, bodyTag uint16) transactionCrossStorageUsed {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	refs := int(rawRefs % 5)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	data := cell.BeginCell().
		MustStoreUInt(uint64(bodyTag), 16).
		MustStoreUInt(uint64(rawRefs), 8)
	for i := 0; i < refs; i++ {
		data.MustStoreRef(cell.BeginCell().
			MustStoreUInt(uint64(i+1), uint(1+i%7)).
			MustStoreUInt(uint64(bodyTag), 16).
			EndCell())
	}
	newData := data.EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := transactionVersionShardAccountWithExtra(t, tonopsTestAddr, code, origData, walletSendTestBalance, extra, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	goUsage := transactionCrossShardStorageUsed(t, goRes.NextAccount.ShardAccountCell())
	refUsage := transactionCrossShardStorageUsed(t, refRes.shardCell)
	if goUsage != refUsage {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("storage usage mismatch: go=%+v reference=%+v", goUsage, refUsage)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}
	return goUsage
}

func TestTVMCrossEmulatorTransactionSpecialGasFullGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x42}, 32))
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build config address cell: %v", err)
	}
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            10,
		FlatGasPrice:            100,
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000_000,
		SpecialGasLimit:         1_000,
		BlockGasLimit:           1_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build gas prices cell: %v", err)
	}
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     specialAddr,
		Amount:      tlb.FromNanoTONU(200),
		Body:        body,
	})

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantGasLimit := int64(110)
			if version >= 5 {
				wantGasLimit = 1_000
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
				int32(tlb.ConfigParamConfigAddress):        configAddrCell,
				int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
				int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
			})
			code := makeTransactionInternalSuccessCode(t, newData)
			shard := buildTransactionTestShardAccount(t, specialAddr, code, origData, walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     specialAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
			assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
			assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
			assertOrdinaryTransactionGasLimit(t, "go", goRes.TransactionCell, wantGasLimit)
			assertOrdinaryTransactionGasLimit(t, "reference", refRes.txCell, wantGasLimit)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionSpecialGasFullGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(100), uint8(10), uint16(100), uint16(1_000), uint8(0x42), uint16(0xA000+version))
		f.Add(uint8(version), uint16(0), uint8(1), uint16(1), uint16(64), uint8(0x43), uint16(0xB000+version))
		f.Add(uint8(version), uint16(4_000), uint8(20), uint16(250), uint16(500), uint8(0x44), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint16(0xffff), uint8(255), uint16(0xffff), uint16(0xffff), uint8(0xff), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawMsgAmount uint16, rawFlatLimit uint8, rawFlatPrice, rawSpecialLimit uint16, rawAddrTag uint8, dataTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionSpecialGasFullVersionParity(t, version, rawMsgAmount, rawFlatLimit, rawFlatPrice, rawSpecialLimit, rawAddrTag, dataTag)
	})
}

func assertTransactionSpecialGasFullVersionParity(t *testing.T, version uint32, rawMsgAmount uint16, rawFlatLimit uint8, rawFlatPrice, rawSpecialLimit uint16, rawAddrTag uint8, dataTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	flatLimit := uint64(rawFlatLimit%50) + 1
	flatPrice := uint64(rawFlatPrice%500) + 1
	specialLimit := uint64(rawSpecialLimit%5_000) + 1
	msgAmount := uint64(rawMsgAmount%5_000) + flatPrice
	prices := &tlb.ConfigGasLimitsPrices{
		HasFlatPricing:          true,
		FlatGasLimit:            flatLimit,
		FlatGasPrice:            flatPrice,
		HasSeparateSpecialLimit: true,
		GasPrice:                1 << 16,
		GasLimit:                1_000_000,
		SpecialGasLimit:         specialLimit,
		BlockGasLimit:           1_000_000,
	}
	gasCell, err := tlb.ToCell(prices)
	if err != nil {
		t.Fatalf("failed to build gas prices cell: %v", err)
	}

	addrTag := rawAddrTag
	if addrTag == 0 {
		addrTag = 0x42
	}
	specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{addrTag}, 32))
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build config address cell: %v", err)
	}

	wantGasLimit := transactionGasInt(min(transactionGasBoughtFor(prices, new(big.Int).SetUint64(msgAmount)), specialLimit))
	if version >= 5 {
		wantGasLimit = transactionGasInt(specialLimit)
	}

	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(dataTag^0xffff), 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     specialAddr,
		Amount:      tlb.FromNanoTONU(msgAmount),
		Body:        body,
	})
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamConfigAddress):        configAddrCell,
		int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
	})
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, specialAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     specialAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
	if _, ok := mustTransactionComputePhase(t, goRes.TransactionCell).Phase.(tlb.ComputePhaseVM); ok {
		assertOrdinaryTransactionGasLimit(t, "go", goRes.TransactionCell, wantGasLimit)
		assertOrdinaryTransactionGasLimit(t, "reference", refRes.txCell, wantGasLimit)
	}
}

func TestTVMCrossEmulatorTransactionHistoricalGasLimitOverrideGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	overrideAddr := address.MustParseRawAddr("0:5E4A5F9DBA638789E6770C990D2959237ACA3BC19D15A734782C26CB19343CC6")
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasPrice:      1,
		GasLimit:      1_000_000,
		BlockGasLimit: 100_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build gas prices cell: %v", err)
	}
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     overrideAddr,
		Amount:      tlb.FromNanoTONU(10_000_000_000_000),
		Body:        body,
	})

	for _, tc := range []struct {
		name string
		now  uint32
		want func(uint32) int64
	}{
		{
			name: "active_window",
			now:  1_710_000_000,
			want: func(version uint32) int64 {
				if version >= 9 {
					return 70_000_000
				}
				return 1_000_000
			},
		},
		{
			name: "last_active_second",
			now:  1_740_787_199,
			want: func(version uint32) int64 {
				if version >= 9 {
					return 70_000_000
				}
				return 1_000_000
			},
		},
		{
			name: "expired_at_cutoff",
			now:  1_740_787_200,
			want: func(uint32) int64 {
				return 1_000_000
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, version := range transactionVersionCrossEmulatorVersions(t) {
				version := version
				t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
					configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
						int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
						int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
					})
					code := makeTransactionInternalSuccessCode(t, newData)
					shard := buildTransactionTestShardAccount(t, overrideAddr, code, origData, 10_000_000_000_000, tc.now)

					goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
						Address:     overrideAddr,
						Now:         tc.now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}

					refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, tc.now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference transaction emulation failed: %v", err)
					}

					wantGasLimit := tc.want(version)
					assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
					assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
					assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
					assertOrdinaryTransactionGasLimit(t, "go", goRes.TransactionCell, wantGasLimit)
					assertOrdinaryTransactionGasLimit(t, "reference", refRes.txCell, wantGasLimit)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionHistoricalGasLimitOverrideGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), false, uint8(0), uint32(0), uint16(0xA000+version))
		f.Add(uint8(version), uint8(0), true, uint8(0), uint32(1), uint16(0xB000+version))
		f.Add(uint8(version), uint8(1), false, uint8(1), uint32(0), uint16(0xC000+version))
		f.Add(uint8(version), uint8(1), false, uint8(2), uint32(31), uint16(0xD000+version))
	}
	f.Add(uint8(255), uint8(255), true, uint8(255), uint32(1024), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawOverride uint8, mutateAddr bool, rawNowCase uint8, rawNowDelta uint32, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionHistoricalGasLimitOverrideVersionParity(t, version, rawOverride, mutateAddr, rawNowCase, rawNowDelta, bodyTag)
	})
}

func assertTransactionHistoricalGasLimitOverrideVersionParity(t *testing.T, version uint32, rawOverride uint8, mutateAddr bool, rawNowCase uint8, rawNowDelta uint32, bodyTag uint16) {
	t.Helper()

	override := transactionCrossHistoricalGasOverride(rawOverride)
	addr := override.addr
	if mutateAddr {
		data := append([]byte(nil), addr.Data()...)
		data[len(data)-1] ^= 1
		addr = address.NewAddress(0, byte(addr.Workchain()), data)
	}
	now := transactionCrossHistoricalGasOverrideNow(override, rawNowCase, rawNowDelta)
	wantGasLimit := int64(1_000_000)
	if !mutateAddr && version >= override.fromVersion && now < override.until {
		wantGasLimit = int64(override.limit)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	gasCell, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		GasPrice:      1,
		GasLimit:      1_000_000,
		BlockGasLimit: 100_000_000,
	})
	if err != nil {
		t.Fatalf("failed to build gas prices cell: %v", err)
	}
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(10_000_000_000_000),
		Body:        body,
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamGasPricesBasechain):   gasCell,
		int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
	})
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, addr, code, origData, 10_000_000_000_000, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
	assertOrdinaryTransactionGasLimit(t, "go", goRes.TransactionCell, wantGasLimit)
	assertOrdinaryTransactionGasLimit(t, "reference", refRes.txCell, wantGasLimit)
}

func transactionCrossHistoricalGasOverride(raw uint8) transactionGasLimitOverrideEntry {
	if raw%2 == 0 {
		return transactionGasLimitOverrides[0]
	}
	return transactionGasLimitOverrides[1]
}

func transactionCrossHistoricalGasOverrideNow(override transactionGasLimitOverrideEntry, rawCase uint8, delta uint32) uint32 {
	switch rawCase % 3 {
	case 0:
		return override.until - 1 - delta%64
	case 1:
		return override.until
	default:
		return override.until + delta%64
	}
}

func TestTVMCrossEmulatorTransactionMasterchainStateLimitGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertTransactionMasterchainStateLimitVersionParity(t, version, 0x45, 0xDD, 0xCAFE)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionMasterchainStateLimitGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0x45), uint8(0xDD), uint16(0xA000+version))
		f.Add(uint8(version), uint8(0x7F), uint8(0xA1), uint16(0xB000+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, addrTag, codeTag uint8, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionMasterchainStateLimitVersionParity(t, version, addrTag, codeTag, bodyTag)
	})
}

func assertTransactionMasterchainStateLimitVersionParity(t *testing.T, version uint32, addrTag, codeTag uint8, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	addrTag = transactionMasterchainLimitSafeAddrTag(addrTag)
	masterAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{addrTag}, 32))
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	oversizedCode := cell.BeginCell().
		MustStoreUInt(uint64(codeTag), 8).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
	code := makeTransactionInternalSetCodeCode(t, oversizedCode, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     masterAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	want := transactionActionPhaseExpectation{success: true, valid: true}
	if version >= 12 {
		want = transactionActionPhaseExpectation{valid: true, resultCode: 50}
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSizeLimits): buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 10, 1),
	})
	shard := buildTransactionTestShardAccount(t, masterAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     masterAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	if reason := transactionV15LibraryReferenceSkip(version); reason != "" {
		t.Skip(reason)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
}

func transactionMasterchainLimitSafeAddrTag(raw uint8) byte {
	tags := [...]byte{0x31, 0x45, 0x5A, 0x66, 0x68, 0x7F}
	return tags[int(raw)%len(tags)]
}

func TestTVMCrossEmulatorTransactionMasterchainPublicLibraryLimitGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tc := range []struct {
		name    string
		rawCase uint8
		addrTag byte
	}{
		{
			name:    "masterchain_public_library_limit",
			rawCase: 0,
			addrTag: 0x66,
		},
		{
			name:    "basechain_allows_public_library",
			rawCase: 1,
			addrTag: 0x68,
		},
	} {
		tc := tc
		for _, version := range transactionVersionCrossEmulatorVersions(t) {
			version := version
			t.Run(fmt.Sprintf("%s/global_v%d", tc.name, version), func(t *testing.T) {
				assertTransactionMasterchainPublicLibraryLimitVersionParity(t, version, tc.rawCase, tc.addrTag, 0xCC, 0xCAFE)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTransactionMasterchainPublicLibraryLimitGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0x66), uint8(0xCC), uint16(0xA000+version))
		f.Add(uint8(version), uint8(1), uint8(0x68), uint8(0xE1), uint16(0xB000+version))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase, addrTag, libTag uint8, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionMasterchainPublicLibraryLimitVersionParity(t, version, rawCase, addrTag, libTag, bodyTag)
	})
}

func assertTransactionMasterchainPublicLibraryLimitVersionParity(t *testing.T, version uint32, rawCase, addrTag, libTag uint8, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	workchain := byte(0)
	want := transactionActionPhaseExpectation{success: true, valid: true}
	if rawCase%2 == 0 {
		workchain = 0xFF
		want = transactionActionPhaseExpectation{valid: true, resultCode: 50}
	}
	if version >= 15 {
		want = transactionActionPhaseExpectation{valid: true, resultCode: 46}
	}
	addrTag = transactionMasterchainLimitSafeAddrTag(addrTag)
	addr := address.NewAddress(0, workchain, bytes.Repeat([]byte{addrTag}, 32))
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	lib := cell.BeginCell().MustStoreUInt(uint64(libTag), 8).EndCell()
	code := makeTransactionInternalSetLibCode(t, lib, newData, 2)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSizeLimits): buildTransactionSizeLimitsCellWithPublicLibraries(t, 1<<21, 1<<13, 1000, 1000, 1000, 0),
	})
	shard := buildTransactionTestShardAccount(t, addr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
	if reason := transactionV15LibraryReferenceSkip(version); reason != "" {
		t.Skip(reason)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
}

func TestTVMCrossEmulatorTransactionFailedActionMessageBalanceGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	actions := buildTransactionActionList(t,
		tlb.ActionSendMsg{Mode: 64, Msg: buildTransactionOutboundInternalCell(t, 1)},
		tlb.ActionSendMsg{Mode: 16, Msg: buildTransactionOutboundInternalCell(t, 20_000_000_000)},
	)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			wantAction := transactionActionPhaseExpectation{
				valid:           true,
				resultCode:      34,
				messagesCreated: 1,
			}
			wantBounceKind := "none"
			wantOutCount := uint16(0)
			if version >= 4 {
				wantAction.resultCode = 37
				wantBounceKind = "nofunds"
			}
			if version >= 14 {
				wantBounceKind = "ok"
				wantOutCount = 1
			}

			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
			assertOrdinaryTransactionBouncePhase(t, "go", goRes.TransactionCell, wantBounceKind, wantOutCount)
			if version >= 14 {
				assertTransactionFailedActionMessageBalanceSkippedGoResult(t, goRes.TransactionCell)
				t.Skip("bundled reference emulator predates upstream transaction v14 failed-action message-balance restore")
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
			assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
			assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
			assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
			assertOrdinaryTransactionBouncePhase(t, "reference", refRes.txCell, wantBounceKind, wantOutCount)
		})
	}
}

func FuzzTVMCrossEmulatorTransactionFailedActionMessageBalanceGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint64(0), uint64(0), uint16(0xA000+version), uint16(0xB000+version), uint16(0xC000+version))
		f.Add(uint8(version), uint8(1), uint64(500), uint64(1), uint16(0xD000+version), uint16(0xE000+version), uint16(0xF000+version))
		f.Add(uint8(version), uint8(2), uint64(777), uint64(9), uint16(0xA100+version), uint16(0xB100+version), uint16(0xC100+version))
		f.Add(uint8(version), uint8(3), uint64(999), uint64(5), uint16(0xD100+version), uint16(0xE100+version), uint16(0xF100+version))
	}
	f.Add(uint8(255), uint8(255), uint64(0xffffffffffffffff), uint64(0xffffffffffffffff), uint16(0), uint16(0xffff), uint16(0x1234))

	f.Fuzz(func(t *testing.T, rawVersion, rawMode uint8, rawIncomingAmount, rawFirstAmount uint64, origTag, newTag, bodyTag uint16) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionFailedActionMessageBalanceVersionParity(t, version, rawMode, rawIncomingAmount, rawFirstAmount, origTag, newTag, bodyTag)
	})
}

func assertTransactionFailedActionMessageBalanceVersionParity(t *testing.T, version uint32, rawMode uint8, rawIncomingAmount, rawFirstAmount uint64, origTag, newTag, bodyTag uint16) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	mode := []uint8{0, 64, 65, 128}[int(rawMode)%4]
	incomingAmount := uint64(1_000_000_000 + rawIncomingAmount%1_000_000)
	firstAmount := rawFirstAmount%9 + 1
	origData := cell.BeginCell().MustStoreUInt(uint64(origTag), 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(uint64(newTag), 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	actions := buildTransactionActionList(t,
		tlb.ActionSendMsg{Mode: mode, Msg: buildTransactionOutboundInternalCell(t, firstAmount)},
		tlb.ActionSendMsg{Mode: 16, Msg: buildTransactionOutboundInternalCell(t, 20_000_000_000)},
	)
	code := makeTransactionInternalActionsCode(t, actions, newData)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(incomingAmount),
		Body:        body,
	})

	wantAction := transactionActionPhaseExpectation{
		valid:           true,
		resultCode:      34,
		messagesCreated: 1,
	}
	wantBounceKind := "none"
	wantOutCount := uint16(0)
	if mode == 0 {
		wantAction.resultCode = 37
		wantAction.messagesCreated = 0
	} else if version >= 4 {
		wantAction.resultCode = 37
		wantBounceKind = "nofunds"
	}
	if mode != 0 && version >= 14 {
		wantBounceKind = "ok"
		wantOutCount = 1
	}

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, wantAction)
	assertOrdinaryTransactionBouncePhase(t, "go", goRes.TransactionCell, wantBounceKind, wantOutCount)
	if mode != 0 && version >= 14 {
		assertTransactionFailedActionMessageBalanceSkippedGoResult(t, goRes.TransactionCell)
		t.Skip("bundled reference emulator predates upstream transaction v14 failed-action message-balance restore")
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, wantAction)
	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
	assertOrdinaryTransactionBouncePhase(t, "reference", refRes.txCell, wantBounceKind, wantOutCount)
}

func assertTransactionFailedActionMessageBalanceSkippedGoResult(t *testing.T, txCell *cell.Cell) {
	t.Helper()

	assertOrdinaryTransactionSingleInternalOutDest(t, "go", txCell, internalEmulationSrcAddr)

	details := transactionCrossBounceDetailsForTx(t, "go", txCell)
	if details.bodyTag != 0xFFFFFFFF || details.bodyRefs != 0 {
		t.Fatalf("go bounce body = tag %#x refs %d, want tag %#x refs 0", details.bodyTag, details.bodyRefs, uint64(0xFFFFFFFF))
	}
}

func TestTVMCrossEmulatorTransactionBounceFormatGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := buildTransactionZeroBitsCell(t, 520)
	code := makeTransactionStackUnderflowCode(t)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	tests := []struct {
		name       string
		extraFlags uint64
		extra      *cell.Dictionary
	}{
		{
			name:       "extra_flags_body",
			extraFlags: 1,
			extra:      makeTransactionExtraCurrencies(t, 7, 11),
		},
		{
			name:       "legacy_extra_currency_usage",
			extraFlags: 0,
			extra:      makeTransactionExtraCurrencies(t, 7, 11),
		},
	}

	legacyDetails := map[uint32]transactionCrossBounceDetails{}
	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					wantTag := uint64(0xFFFFFFFF)
					wantRefs := 0
					if tt.extraFlags != 0 && version >= 12 {
						wantTag = 0xFFFFFFFE
						wantRefs = 2
					}

					configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
					msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
						IHRDisabled:     true,
						Bounce:          true,
						SrcAddr:         internalEmulationSrcAddr,
						DstAddr:         tonopsTestAddr,
						Amount:          tlb.FromNanoTONU(1_000_000_000),
						ExtraCurrencies: tt.extra,
						IHRFee:          tlb.FromNanoTONU(tt.extraFlags),
						CreatedLT:       777,
						CreatedAt:       now - 10,
						Body:            body,
					})

					goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
						Address:     tonopsTestAddr,
						Now:         now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}
					refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
					if err != nil {
						t.Fatalf("reference transaction emulation failed: %v", err)
					}

					if goRes.GasUsed != refRes.gasUsed {
						t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
					}
					if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
						t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
						t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
					}
					if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
						t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
						t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
					}

					goDetails := transactionCrossBounceDetailsForTx(t, "go", goRes.TransactionCell)
					refDetails := transactionCrossBounceDetailsForTx(t, "reference", refRes.txCell)
					if goDetails != refDetails {
						t.Fatalf("bounce details mismatch: go=%+v reference=%+v", goDetails, refDetails)
					}
					if goDetails.bodyTag != wantTag || goDetails.bodyRefs != wantRefs {
						t.Fatalf("bounce body = tag %#x refs %d, want tag %#x refs %d", goDetails.bodyTag, goDetails.bodyRefs, wantTag, wantRefs)
					}

					if tt.extraFlags == 0 {
						legacyDetails[version] = goDetails
					}
				})
			}
		})
	}

	v12Legacy, haveV12 := legacyDetails[12]
	v13Legacy, haveV13 := legacyDetails[13]
	if haveV12 && haveV13 && v12Legacy.msgSizeCells <= v13Legacy.msgSizeCells && v12Legacy.msgSizeBits <= v13Legacy.msgSizeBits {
		t.Fatalf("legacy bounce extra usage did not shrink at v13: v12=%+v v13=%+v", v12Legacy, v13Legacy)
	}
}

func FuzzTVMCrossEmulatorTransactionBounceFormatGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0), uint8(0), uint8(0), uint16(32), uint64(0))
		f.Add(uint8(version), uint8(0), uint8(0), uint8(1), uint16(32), uint64(0))
		f.Add(uint8(version), uint8(1), uint8(1), uint8(0), uint16(520), uint64(11))
		f.Add(uint8(version), uint8(1), uint8(1), uint8(1), uint16(520), uint64(11))
		f.Add(uint8(version), uint8(1), uint8(3), uint8(1), uint16(700), uint64(777))
	}
	f.Add(uint8(255), uint8(255), uint8(255), uint8(255), uint16(1023), uint64(1_000_000))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase, rawFlags, rawCapabilities uint8, rawBodyBits uint16, rawExtraAmount uint64) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		assertTransactionBounceFormatVersionParity(t, version, rawCase, rawFlags&3, rawCapabilities&1 != 0, rawBodyBits%768, rawExtraAmount%1_000_000)
	})
}

func assertTransactionBounceFormatVersionParity(t *testing.T, version uint32, rawCase, extraFlags uint8, legacyCapability bool, bodyBits uint16, extraAmount uint64) {
	t.Helper()

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := buildTransactionZeroBitsCell(t, uint(bodyBits))
	code := makeTransactionStackUnderflowCode(t)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)

	var extra *cell.Dictionary
	if rawCase%2 != 0 {
		extra = makeTransactionExtraCurrencies(t, uint32(rawCase%31)+1, extraAmount+1)
	}
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled:     true,
		Bounce:          true,
		SrcAddr:         internalEmulationSrcAddr,
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.FromNanoTONU(1_000_000_000),
		ExtraCurrencies: extra,
		IHRFee:          tlb.FromNanoTONU(uint64(extraFlags)),
		CreatedLT:       777 + uint64(rawCase),
		CreatedAt:       now - 10,
		Body:            body,
	})

	capabilities := uint64(0)
	if legacyCapability {
		capabilities = 4
	}
	configRoot := referenceTransactionConfigRootWithGlobalVersionAndCapabilities(t, baseConfigRoot, version, capabilities)
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Logf("go summary=%s\nreference summary=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Logf("go account=%s\nreference account=%s", transactionCrossShardAccountSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardAccountSummary(t, refRes.shardCell))
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}

	wantRefs := 0
	wantTag := uint64(0)
	wantEmpty := true
	if extraFlags&1 != 0 && version >= 12 {
		wantEmpty = false
		wantTag = 0xFFFFFFFE
		wantRefs = 2
	} else if legacyCapability {
		wantEmpty = false
		wantTag = 0xFFFFFFFF
	}
	goDetails := transactionCrossBounceDetailsForTx(t, "go", goRes.TransactionCell)
	refDetails := transactionCrossBounceDetailsForTx(t, "reference", refRes.txCell)
	if goDetails != refDetails {
		t.Fatalf("bounce details mismatch: go=%+v reference=%+v", goDetails, refDetails)
	}
	if wantEmpty {
		if goDetails.bodyBits != 0 || goDetails.bodyRefs != 0 {
			t.Fatalf("bounce body = bits %d refs %d, want empty", goDetails.bodyBits, goDetails.bodyRefs)
		}
		return
	}
	if goDetails.bodyTag != wantTag || goDetails.bodyRefs != wantRefs {
		t.Fatalf("bounce body = tag %#x refs %d, want tag %#x refs %d", goDetails.bodyTag, goDetails.bodyRefs, wantTag, wantRefs)
	}
}

type transactionCrossBounceDetails struct {
	bodyTag      uint64
	bodyBits     uint
	bodyRefs     int
	msgSizeCells uint64
	msgSizeBits  uint64
}

func transactionCrossBounceDetailsForTx(t *testing.T, side string, txCell *cell.Cell) transactionCrossBounceDetails {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("%s transaction parse failed: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("%s transaction description type %T, want ordinary", side, tx.Description)
	}
	if desc.BouncePhase == nil {
		t.Fatalf("%s transaction has no bounce phase", side)
	}
	bounceOK, ok := desc.BouncePhase.Phase.(tlb.BouncePhaseOk)
	if !ok {
		t.Fatalf("%s bounce phase type %T, want ok", side, desc.BouncePhase.Phase)
	}

	out, err := tx.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("%s outbound messages parse failed: %v", side, err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("%s outbound messages = %+v, want one internal bounce", side, out)
	}
	bounced := out[0].AsInternal()
	if !bounced.Bounced || bounced.Bounce {
		t.Fatalf("%s outbound bounce flags = bounce:%t bounced:%t", side, bounced.Bounce, bounced.Bounced)
	}
	body, err := bounced.Body.BeginParse()
	if err != nil {
		t.Fatalf("%s bounce body parse failed: %v", side, err)
	}
	bodyBits := body.BitsLeft()
	if bodyBits < 32 {
		return transactionCrossBounceDetails{
			bodyBits:     bodyBits,
			bodyRefs:     body.RefsNum(),
			msgSizeCells: bounceOK.MsgSize.Cells.Uint64(),
			msgSizeBits:  bounceOK.MsgSize.Bits.Uint64(),
		}
	}
	tag, err := body.LoadUInt(32)
	if err != nil {
		t.Fatalf("%s bounce body tag read failed: %v", side, err)
	}

	return transactionCrossBounceDetails{
		bodyTag:      tag,
		bodyBits:     bodyBits,
		bodyRefs:     body.RefsNum(),
		msgSizeCells: bounceOK.MsgSize.Cells.Uint64(),
		msgSizeBits:  bounceOK.MsgSize.Bits.Uint64(),
	}
}

func assertOrdinaryTransactionGasLimit(t *testing.T, side string, txCell *cell.Cell, want int64) {
	t.Helper()

	phase := mustTransactionComputePhase(t, txCell)
	vmPhase, ok := phase.Phase.(tlb.ComputePhaseVM)
	if !ok {
		t.Fatalf("%s compute phase = %T, want VM phase", side, phase.Phase)
	}
	if got := vmPhase.Details.GasLimit.Int64(); got != want {
		t.Fatalf("%s gas limit = %d, want %d", side, got, want)
	}
}

func assertOrdinaryTransactionSingleInternalOutDest(t *testing.T, side string, txCell *cell.Cell, want *address.Address) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	if tx.OutMsgCount != 1 || tx.IO.Out == nil {
		t.Fatalf("%s out message count = %d, want one", side, tx.OutMsgCount)
	}

	out, err := tx.IO.Out.ToSlice()
	if err != nil {
		t.Fatalf("%s output messages parse failed: %v", side, err)
	}
	if len(out) != 1 || out[0].MsgType != tlb.MsgTypeInternal {
		t.Fatalf("%s output messages = %+v, want one internal message", side, out)
	}

	got := out[0].AsInternal().DstAddr
	if got == nil || got.Type() != address.StdAddress || got.Anycast() != nil || got.Workchain() != want.Workchain() || !bytes.Equal(got.Data(), want.Data()) {
		t.Fatalf("%s output destination = %v, want std %v", side, got, want)
	}
}

func assertOrdinaryTransactionBouncePhase(t *testing.T, side string, txCell *cell.Cell, wantKind string, wantOutCount uint16) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	if tx.OutMsgCount != wantOutCount {
		t.Fatalf("%s out message count = %d, want %d", side, tx.OutMsgCount, wantOutCount)
	}

	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("%s transaction description = %T, want ordinary", side, tx.Description)
	}
	if wantKind == "none" {
		if desc.BouncePhase != nil {
			t.Fatalf("%s transaction has bounce phase %T, want none", side, desc.BouncePhase.Phase)
		}
		return
	}
	if desc.BouncePhase == nil {
		t.Fatalf("%s transaction has no bounce phase", side)
	}
	switch desc.BouncePhase.Phase.(type) {
	case tlb.BouncePhaseNoFunds:
		if wantKind != "nofunds" {
			t.Fatalf("%s bounce phase = nofunds, want %s", side, wantKind)
		}
	case tlb.BouncePhaseOk:
		if wantKind != "ok" {
			t.Fatalf("%s bounce phase = ok, want %s", side, wantKind)
		}
	default:
		t.Fatalf("%s bounce phase = %T, want %s", side, desc.BouncePhase.Phase, wantKind)
	}
}
