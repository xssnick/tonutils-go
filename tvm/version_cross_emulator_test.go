//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := []struct {
		name string
		code *cell.Cell
	}{
		{
			name: "pushint_add",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				stackop.PUSHINT(big.NewInt(35)).Serialize(),
				mathop.SUM().Serialize(),
			),
		},
		{
			name: "pushint_mul_neg",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(-6)).Serialize(),
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				mathop.MUL().Serialize(),
			),
		},
		{
			name: "initial_stack_sub",
			code: codeFromBuilders(t,
				mathop.SUB().Serialize(),
			),
		},
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT")
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				t.Run("v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					stackValues := []any(nil)
					if tt.name == "initial_stack_sub" {
						stackValues = []any{int64(50), int64(8)}
					}
					runCrossAllVersionSmokeCase(t, tt.code, stackValues, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorAllGlobalVersionsSmoke(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%crossAllGlobalVersionSmokeCaseCount), int16(7), int16(35))
		f.Add(uint8(version), uint8(1), int16(-6), int16(7))
		f.Add(uint8(version), uint8(2), int16(50), int16(8))
	}
	f.Add(uint8(255), uint8(255), int16(-32768), int16(32767))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, rawA, rawB int16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		runCrossAllVersionSmokeFuzzCase(t, version, rawCase, rawA, rawB)
	})
}

func TestTVMCrossEmulatorExecutionConfigGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionConfigGlobalVersionOverride(t, version)
		})
	}
}

func FuzzTVMCrossEmulatorExecutionConfigGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(vmcore.MaxSupportedGlobalVersion-version))
	}
	f.Add(uint8(255), uint8(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertExecutionConfigGlobalVersionOverrideWithMachine(t, version, machineVersion)
	})
}

func assertExecutionConfigGlobalVersionOverride(t *testing.T, version int) {
	t.Helper()

	machineVersion := vmcore.MaxSupportedGlobalVersion - version
	assertExecutionConfigGlobalVersionOverrideWithMachine(t, version, machineVersion)
}

func assertExecutionConfigGlobalVersionOverrideWithMachine(t *testing.T, version, machineVersion int) {
	t.Helper()

	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.GASCONSUMED().Serialize()))
	data := cell.BeginCell().EndCell()
	goRes, err := runGoCrossCodeWithExecutionConfigGlobalVersion(code, data, tuple.Tuple{}, vmcore.NewStack(), machineVersion, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if version < funcsop.GASCONSUMED().MinGlobalVersion() && goRes.exitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("v%d machine_v%d exit code = %d, want invalid opcode", version, machineVersion, goRes.exitCode)
	}
	if version >= funcsop.GASCONSUMED().MinGlobalVersion() && goRes.exitCode != 0 {
		t.Fatalf("v%d machine_v%d exit code = %d, want success", version, machineVersion, goRes.exitCode)
	}
	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

const crossAllGlobalVersionSmokeCaseCount = 3

func runCrossAllVersionSmokeFuzzCase(t *testing.T, version int, rawCase uint8, rawA, rawB int16) {
	t.Helper()

	a := big.NewInt(int64(rawA))
	b := big.NewInt(int64(rawB))
	switch rawCase % crossAllGlobalVersionSmokeCaseCount {
	case 0:
		runCrossAllVersionSmokeCase(t, codeFromBuilders(t,
			stackop.PUSHINT(a).Serialize(),
			stackop.PUSHINT(b).Serialize(),
			mathop.SUM().Serialize(),
		), nil, version)
	case 1:
		runCrossAllVersionSmokeCase(t, codeFromBuilders(t,
			stackop.PUSHINT(a).Serialize(),
			stackop.PUSHINT(b).Serialize(),
			mathop.MUL().Serialize(),
		), nil, version)
	default:
		runCrossAllVersionSmokeCase(t, codeFromBuilders(t,
			mathop.SUB().Serialize(),
		), []any{int64(rawA), int64(rawB)}, version)
	}
}

func TestTVMCrossEmulatorLibraryCodeCellStartupAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertLibraryCodeCellStartupGlobalVersion(t, version, 7)
		})
	}
}

func FuzzTVMCrossEmulatorLibraryCodeCellStartupGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(7+version))
	}
	f.Add(uint8(255), uint8(0))
	f.Add(uint8(0), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertLibraryCodeCellStartupGlobalVersion(t, version, stackTag)
	})
}

func assertLibraryCodeCellStartupGlobalVersion(t *testing.T, version int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossCodeWithVersionAndLibs(code, data, tuple.Tuple{}, []*cell.Cell{libs}, vmcore.NewStack(), version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	if version >= 9 {
		if goRes.exitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.exitCode)
		}
		assertCrossSkippedGoStack(t, goRes.stack, []any{stackValue})
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func TestTVMCrossEmulatorLibraryLookupGasAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		for lookups := 1; lookups <= 2; lookups++ {
			t.Run("global_v"+big.NewInt(int64(version)).String()+"_lookups_"+big.NewInt(int64(lookups)).String(), func(t *testing.T) {
				assertLibraryLookupGasGlobalVersion(t, version, lookups, 0xBEEF)
			})
		}
	}
}

func FuzzTVMCrossEmulatorLibraryLookupGasGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(1), uint16(0xA000+version))
		f.Add(uint8(version), uint8(2), uint16(0xB000+version))
		f.Add(uint8(version), uint8(3), uint16(0xC000+version))
	}
	f.Add(uint8(255), uint8(255), uint16(0xFFFF))

	f.Fuzz(func(t *testing.T, rawVersion, rawLookups uint8, rawTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		lookups := int(rawLookups%3) + 1
		assertLibraryLookupGasGlobalVersion(t, version, lookups, rawTag)
	})
}

func assertLibraryLookupGasGlobalVersion(t *testing.T, version, lookups int, tag uint16) {
	t.Helper()

	target := cell.BeginCell().MustStoreUInt(uint64(tag), 16).EndCell()
	libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	code := prependRawMethodDrop(libraryLookupGasCode(t, lookups))

	goLibraryRes, refLibraryRes := runLibraryLookupGasCrossCase(t, version, code, repeatedCrossStack(libraryCell, lookups), libs)
	if goLibraryRes.exitCode != 0 || refLibraryRes.exitCode != 0 {
		t.Fatalf("v%d lookups=%d library exit code: go=%d reference=%d", version, lookups, goLibraryRes.exitCode, refLibraryRes.exitCode)
	}
	if goLibraryRes.gasUsed != refLibraryRes.gasUsed {
		t.Fatalf("v%d lookups=%d library gas mismatch: go=%d reference=%d", version, lookups, goLibraryRes.gasUsed, refLibraryRes.gasUsed)
	}

	goOrdinaryRes, refOrdinaryRes := runLibraryLookupGasCrossCase(t, version, code, repeatedCrossStack(target, lookups), nil)
	if goOrdinaryRes.exitCode != 0 || refOrdinaryRes.exitCode != 0 {
		t.Fatalf("v%d lookups=%d ordinary exit code: go=%d reference=%d", version, lookups, goOrdinaryRes.exitCode, refOrdinaryRes.exitCode)
	}
	if goOrdinaryRes.gasUsed != refOrdinaryRes.gasUsed {
		t.Fatalf("v%d lookups=%d ordinary gas mismatch: go=%d reference=%d", version, lookups, goOrdinaryRes.gasUsed, refOrdinaryRes.gasUsed)
	}

	wantDelta := expectedCrossLibraryLookupGasDelta(version, lookups)
	goDelta := goLibraryRes.gasUsed - goOrdinaryRes.gasUsed
	if goDelta != wantDelta {
		t.Fatalf("v%d lookups=%d go library gas delta = %d, want %d", version, lookups, goDelta, wantDelta)
	}
	refDelta := refLibraryRes.gasUsed - refOrdinaryRes.gasUsed
	if refDelta != wantDelta {
		t.Fatalf("v%d lookups=%d reference library gas delta = %d, want %d", version, lookups, refDelta, wantDelta)
	}
}

func runLibraryLookupGasCrossCase(t *testing.T, version int, code *cell.Cell, stackValues []any, libs *cell.Cell) (*crossRunResult, *crossRunResult) {
	t.Helper()

	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	var goLibs []*cell.Cell
	if libs != nil {
		goLibs = []*cell.Cell{libs}
	}
	goRes, err := runGoCrossCodeWithVersionAndLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goLibs, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("v%d stack mismatch:\ngo=%s\nreference=%s", version, goStackCell.Dump(), refStackCell.Dump())
	}

	return goRes, refRes
}

func libraryLookupGasCode(t *testing.T, lookups int) *cell.Cell {
	t.Helper()

	ops := make([]*cell.Builder, 0, lookups*2-1)
	for i := 0; i < lookups; i++ {
		if i > 0 {
			ops = append(ops, stackop.DROP().Serialize())
		}
		ops = append(ops, cellsliceop.CTOS().Serialize())
	}
	return codeFromBuilders(t, ops...)
}

func repeatedCrossStack(value any, count int) []any {
	stack := make([]any, count)
	for i := range stack {
		stack[i] = value
	}
	return stack
}

func expectedCrossLibraryLookupGasDelta(version, lookups int) int64 {
	if version >= 5 {
		return 0
	}
	if version >= 4 {
		return vmcore.CellLoadGasPrice + int64(lookups-1)*vmcore.CellReloadGasPrice
	}
	return 2*vmcore.CellLoadGasPrice + int64(lookups-1)*2*vmcore.CellReloadGasPrice
}

func TestTVMCrossEmulatorExecutionConfigLibrariesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionConfigLibrariesGlobalVersion(t, version, 9)
		})
	}
}

func FuzzTVMCrossEmulatorExecutionConfigLibrariesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(9+version))
	}
	f.Add(uint8(255), uint8(0))
	f.Add(uint8(0), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertExecutionConfigLibrariesGlobalVersion(t, version, stackTag)
	})
}

func TestTVMCrossEmulatorExecutionConfigLibrariesGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionConfigLibrariesGlobalVersionOverride(t, version, vmcore.MaxSupportedGlobalVersion-version, 17)
		})
	}
}

func FuzzTVMCrossEmulatorExecutionConfigLibrariesGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(vmcore.MaxSupportedGlobalVersion-version), uint8(17+version))
	}
	f.Add(uint8(255), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertExecutionConfigLibrariesGlobalVersionOverride(t, version, machineVersion, stackTag)
	})
}

func assertExecutionConfigLibrariesGlobalVersion(t *testing.T, version int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossCodeWithVersionExecutionConfig(code, data, tuple.Tuple{}, []*cell.Cell{libs}, vmcore.NewStack(), version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	if version >= 9 {
		if goRes.exitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.exitCode)
		}
		assertCrossSkippedGoStack(t, goRes.stack, []any{stackValue})
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func assertExecutionConfigLibrariesGlobalVersionOverride(t *testing.T, version, machineVersion int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossCodeWithExecutionConfigGlobalVersionAndLibraries(code, data, tuple.Tuple{}, []*cell.Cell{libs}, vmcore.NewStack(), machineVersion, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	if version >= 9 {
		if goRes.exitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.exitCode)
		}
		assertCrossSkippedGoStack(t, goRes.stack, []any{stackValue})
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("v%d machine_v%d exit code mismatch: go=%d reference=%d", version, machineVersion, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("v%d machine_v%d gas mismatch: go=%d reference=%d", version, machineVersion, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("v%d machine_v%d stack mismatch:\ngo=%s\nreference=%s", version, machineVersion, goStackCell.Dump(), refStackCell.Dump())
	}
}

func TestTVMCrossEmulatorGetMethodGlobalVersionBoundary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertGetMethodGlobalVersionBoundary(t, version)
		})
	}
}

func FuzzTVMCrossEmulatorGetMethodGlobalVersionBoundary(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version))
	}
	f.Add(uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertGetMethodGlobalVersionBoundary(t, version)
	})
}

func assertGetMethodGlobalVersionBoundary(t *testing.T, version int) {
	t.Helper()

	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.GASCONSUMED().Serialize()))
	data := cell.BeginCell().EndCell()
	goStack := vmcore.NewStack()
	if err := goStack.PushSmallInt(0); err != nil {
		t.Fatalf("push method id: %v", err)
	}

	machine := NewTVM()
	goRes, err := machine.ExecuteGetMethod(code, data, tuple.Tuple{}, vmcore.GasWithLimit(referenceDefaultMaxGas), goStack, testExecutionConfigWithVersion(t, uint32(version)))
	if err != nil {
		t.Fatalf("go get method failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference get method failed: %v", err)
	}

	if version < 4 && goRes.ExitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("v%d exit code = %d, want invalid opcode", version, goRes.ExitCode)
	}
	if version >= 4 && goRes.ExitCode != 0 {
		t.Fatalf("v%d exit code = %d, want success", version, goRes.ExitCode)
	}
	if int32(goRes.ExitCode) != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(mustStackCell(t, goRes.Stack))
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func TestTVMCrossEmulatorGetMethodExecutionConfigGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertGetMethodExecutionConfigGlobalVersionOverride(t, version)
		})
	}
}

func FuzzTVMCrossEmulatorGetMethodExecutionConfigGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(vmcore.MaxSupportedGlobalVersion-version))
	}
	f.Add(uint8(255), uint8(0))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertGetMethodExecutionConfigGlobalVersionOverrideWithMachine(t, version, machineVersion)
	})
}

func assertGetMethodExecutionConfigGlobalVersionOverride(t *testing.T, version int) {
	t.Helper()

	machineVersion := vmcore.MaxSupportedGlobalVersion - version
	assertGetMethodExecutionConfigGlobalVersionOverrideWithMachine(t, version, machineVersion)
}

func assertGetMethodExecutionConfigGlobalVersionOverrideWithMachine(t *testing.T, version, machineVersion int) {
	t.Helper()

	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.GASCONSUMED().Serialize()))
	data := cell.BeginCell().EndCell()
	goRes, err := runGoCrossGetMethodWithExecutionConfigGlobalVersion(code, data, tuple.Tuple{}, vmcore.NewStack(), machineVersion, version)
	if err != nil {
		t.Fatalf("go get method failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference get method failed: %v", err)
	}

	if version < funcsop.GASCONSUMED().MinGlobalVersion() && goRes.exitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("v%d machine_v%d exit code = %d, want invalid opcode", version, machineVersion, goRes.exitCode)
	}
	if version >= funcsop.GASCONSUMED().MinGlobalVersion() && goRes.exitCode != 0 {
		t.Fatalf("v%d machine_v%d exit code = %d, want success", version, machineVersion, goRes.exitCode)
	}
	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func TestTVMCrossEmulatorGetMethodExecutionConfigLibrariesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertGetMethodExecutionConfigLibrariesGlobalVersion(t, version, 11)
		})
	}
}

func FuzzTVMCrossEmulatorGetMethodExecutionConfigLibrariesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(11+version))
	}
	f.Add(uint8(255), uint8(0))
	f.Add(uint8(0), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertGetMethodExecutionConfigLibrariesGlobalVersion(t, version, stackTag)
	})
}

func TestTVMCrossEmulatorGetMethodExecutionConfigLibrariesGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_GLOBAL_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertGetMethodExecutionConfigLibrariesGlobalVersionOverride(t, version, vmcore.MaxSupportedGlobalVersion-version, 19)
		})
	}
}

func FuzzTVMCrossEmulatorGetMethodExecutionConfigLibrariesGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(vmcore.MaxSupportedGlobalVersion-version), uint8(19+version))
	}
	f.Add(uint8(255), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertGetMethodExecutionConfigLibrariesGlobalVersionOverride(t, version, machineVersion, stackTag)
	})
}

func assertGetMethodExecutionConfigLibrariesGlobalVersion(t *testing.T, version int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossGetMethodWithVersionExecutionConfig(code, data, tuple.Tuple{}, []*cell.Cell{libs}, vmcore.NewStack(), version)
	if err != nil {
		t.Fatalf("go get method failed: %v", err)
	}
	if version >= 9 {
		if goRes.exitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.exitCode)
		}
		assertCrossSkippedGoStack(t, goRes.stack, []any{stackValue})
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference get method failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func assertGetMethodExecutionConfigLibrariesGlobalVersionOverride(t *testing.T, version, machineVersion int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossGetMethodWithExecutionConfigGlobalVersionAndLibraries(code, data, tuple.Tuple{}, []*cell.Cell{libs}, vmcore.NewStack(), machineVersion, version)
	if err != nil {
		t.Fatalf("go get method failed: %v", err)
	}
	if version >= 9 {
		if goRes.exitCode != 0 {
			t.Fatalf("unexpected go exit code: got=%d expected=0", goRes.exitCode)
		}
		assertCrossSkippedGoStack(t, goRes.stack, []any{stackValue})
		t.Skip("bundled reference emulator predates upstream v9 direct startup library code loading")
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vmcore.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference get method failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("v%d machine_v%d exit code mismatch: go=%d reference=%d", version, machineVersion, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("v%d machine_v%d gas mismatch: go=%d reference=%d", version, machineVersion, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("v%d machine_v%d stack mismatch:\ngo=%s\nreference=%s", version, machineVersion, goStackCell.Dump(), refStackCell.Dump())
	}
}

func runCrossAllVersionSmokeCase(t *testing.T, body *cell.Cell, stackValues []any, globalVersion int) {
	t.Helper()

	code := prependRawMethodDrop(body)
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}

func runGoCrossCodeWithVersionExecutionConfig(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vmcore.Stack, globalVersion int) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushSmallInt(0); err != nil {
		return nil, err
	}

	machine := NewTVM()
	cfg, err := crossRunPreparedBlockchainConfig(globalVersion)
	if err != nil {
		return nil, err
	}
	res, err := machine.Execute(code, data, c7, vmcore.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		Libraries: libs,
		Config:    cfg,
	})

	if err != nil {
		return nil, err
	}

	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}
	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func runGoCrossCodeWithExecutionConfigGlobalVersion(code, data *cell.Cell, c7 tuple.Tuple, stack *vmcore.Stack, _, globalVersion int) (*crossRunResult, error) {
	return runGoCrossCodeWithExecutionConfigGlobalVersionAndLibraries(code, data, c7, nil, stack, 0, globalVersion)
}

func runGoCrossCodeWithExecutionConfigGlobalVersionAndLibraries(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vmcore.Stack, _ int, globalVersion int) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushSmallInt(0); err != nil {
		return nil, err
	}

	machine := NewTVM()
	cfg, err := crossRunPreparedBlockchainConfig(globalVersion)
	if err != nil {
		return nil, err
	}
	res, err := machine.Execute(code, data, c7, vmcore.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		Libraries: libs,
		Config:    cfg,
	})

	if err != nil {
		return nil, err
	}

	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}
	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func runGoCrossGetMethodWithVersionExecutionConfig(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vmcore.Stack, globalVersion int) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushSmallInt(0); err != nil {
		return nil, err
	}

	machine := NewTVM()
	cfg, err := crossRunPreparedBlockchainConfig(globalVersion)
	if err != nil {
		return nil, err
	}
	res, err := machine.ExecuteGetMethod(code, data, c7, vmcore.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		Libraries: libs,
		Config:    cfg,
	})

	if err != nil {
		return nil, err
	}

	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}
	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func runGoCrossGetMethodWithExecutionConfigGlobalVersion(code, data *cell.Cell, c7 tuple.Tuple, stack *vmcore.Stack, _, globalVersion int) (*crossRunResult, error) {
	return runGoCrossGetMethodWithExecutionConfigGlobalVersionAndLibraries(code, data, c7, nil, stack, 0, globalVersion)
}

func runGoCrossGetMethodWithExecutionConfigGlobalVersionAndLibraries(code, data *cell.Cell, c7 tuple.Tuple, libs []*cell.Cell, stack *vmcore.Stack, _ int, globalVersion int) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushSmallInt(0); err != nil {
		return nil, err
	}

	machine := NewTVM()
	cfg, err := crossRunPreparedBlockchainConfig(globalVersion)
	if err != nil {
		return nil, err
	}
	res, err := machine.ExecuteGetMethod(code, data, c7, vmcore.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		Libraries: libs,
		Config:    cfg,
	})

	if err != nil {
		return nil, err
	}

	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}
	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func mustStackCell(t *testing.T, stack *vmcore.Stack) *cell.Cell {
	t.Helper()

	stackCell, err := stackToCell(stack)
	if err != nil {
		t.Fatalf("failed to serialize stack: %v", err)
	}
	return stackCell
}
