//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExecutionProofCrossEmulatorGasAndStackParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code, data := executionProofFixture(t)
	stack := vm.NewStack()

	accountRoot := executionProofAccountRoot(t, code, data)
	goRes, proof, err := runGoCrossCodeWithExecutionProof(accountRoot, stack)
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}
	refRes, err := runReferenceCrossCode(code, data, tuple.Tuple{}, stack)
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

	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
}

func TestExecutionProofCrossEmulatorGlobalVersionBoundary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_EXECUTION_PROOF_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionProofGlobalVersionBoundary(t, version, 0)
		})
	}
}

func FuzzExecutionProofCrossEmulatorGlobalVersionBoundary(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(0))
	}
	f.Add(uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, dataTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertExecutionProofGlobalVersionBoundary(t, version, dataTag)
	})
}

func TestExecutionProofCrossEmulatorExecutionConfigGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_EXECUTION_PROOF_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionProofGlobalVersionOverride(t, version, MaxSupportedGlobalVersion-version, 0)
		})
	}
}

func FuzzExecutionProofCrossEmulatorExecutionConfigGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), uint16(0xA000+version))
	}
	f.Add(uint8(255), uint8(0), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion uint8, dataTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertExecutionProofGlobalVersionOverride(t, version, machineVersion, dataTag)
	})
}

func FuzzExecutionProofCrossEmulatorResultStackGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0xAB))
	}
	f.Add(uint8(255), uint8(0))
	f.Add(uint8(0), uint8(0xff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, dataTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertExecutionProofResultStackGlobalVersion(t, version, dataTag)
	})
}

func TestExecutionProofCrossEmulatorExecutionConfigLibrariesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_EXECUTION_PROOF_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionProofLibrariesGlobalVersion(t, version, 13)
		})
	}
}

func FuzzExecutionProofCrossEmulatorExecutionConfigLibrariesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(13+version))
	}
	f.Add(uint8(255), uint8(0))
	f.Add(uint8(0), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertExecutionProofLibrariesGlobalVersion(t, version, stackTag)
	})
}

func TestExecutionProofCrossEmulatorExecutionConfigLibrariesGlobalVersionOverrideAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_EXECUTION_PROOF_VERSION_AUDIT") {
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			assertExecutionProofLibrariesGlobalVersionOverride(t, version, MaxSupportedGlobalVersion-version, 23)
		})
	}
}

func FuzzExecutionProofCrossEmulatorExecutionConfigLibrariesGlobalVersionOverride(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		opposite := MaxSupportedGlobalVersion - version
		f.Add(uint8(version), uint8(opposite), uint8(23+version))
	}
	f.Add(uint8(255), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawMachineVersion, stackTag uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		machineVersion := tvmFuzzGlobalVersionByte(rawMachineVersion)
		assertExecutionProofLibrariesGlobalVersionOverride(t, version, machineVersion, stackTag)
	})
}

func assertExecutionProofLibrariesGlobalVersion(t *testing.T, version int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()
	accountRoot := executionProofAccountRoot(t, code, data)

	goRes, proof, err := runGoCrossCodeWithExecutionProofConfigVersion(accountRoot, vm.NewStack(), version, []*cell.Cell{libs})
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}
	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
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
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vm.NewStack(), *refCfg)
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

func assertExecutionProofLibrariesGlobalVersionOverride(t *testing.T, version, machineVersion int, stackTag uint8) {
	t.Helper()

	stackValue := int64(stackTag)
	target := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(stackValue)).Serialize(),
	))
	code := mustCrossLibraryCellForHash(t, target.Hash())
	libs := mustCrossLibraryCollection(t, target)
	data := cell.BeginCell().EndCell()
	accountRoot := executionProofAccountRoot(t, code, data)

	goRes, proof, err := runGoCrossCodeWithExecutionProofConfigOverrideAndLibraries(accountRoot, vm.NewStack(), machineVersion, version, []*cell.Cell{libs})
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}
	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
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
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vm.NewStack(), *refCfg)
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

func assertExecutionProofGlobalVersionBoundary(t *testing.T, version int, dataTag uint16) {
	t.Helper()

	body := codeFromBuilders(t, funcsop.GASCONSUMED().Serialize())
	code := prependRawMethodDrop(body)
	data := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	accountRoot := executionProofAccountRoot(t, code, data)

	goRes, proof, err := runGoCrossCodeWithExecutionProofVersion(accountRoot, vm.NewStack(), version)
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vm.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if version < 4 && goRes.exitCode != int32(vmerr.CodeInvalidOpcode) {
		t.Fatalf("v%d exit code = %d, want invalid opcode", version, goRes.exitCode)
	}
	if version >= 4 && goRes.exitCode != 0 {
		t.Fatalf("v%d exit code = %d, want success", version, goRes.exitCode)
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

	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
}

func assertExecutionProofGlobalVersionOverride(t *testing.T, version, machineVersion int, dataTag uint16) {
	t.Helper()

	body := codeFromBuilders(t, funcsop.GASCONSUMED().Serialize())
	code := prependRawMethodDrop(body)
	data := cell.BeginCell().MustStoreUInt(uint64(dataTag), 16).EndCell()
	accountRoot := executionProofAccountRoot(t, code, data)

	goRes, proof, err := runGoCrossCodeWithExecutionProofConfigOverride(accountRoot, vm.NewStack(), machineVersion, version)
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vm.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if version < 4 && goRes.exitCode != int32(vmerr.CodeInvalidOpcode) {
		t.Fatalf("v%d machine_v%d exit code = %d, want invalid opcode", version, machineVersion, goRes.exitCode)
	}
	if version >= 4 && goRes.exitCode != 0 {
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

	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
}

func assertExecutionProofResultStackGlobalVersion(t *testing.T, version int, dataTag uint8) {
	t.Helper()

	codeTail := executionProofCodeTail(t,
		stackop.PUSHCTR(4).Serialize(),
		cellsliceop.CTOS().Serialize(),
		cellsliceop.LDREFRTOS().Serialize(),
		cellsliceop.LDU(8).Serialize(),
		execop.RET().Serialize(),
	)
	code := cell.BeginCell().MustStoreRef(codeTail).EndCell()

	usedData := cell.BeginCell().MustStoreUInt(uint64(dataTag), 8).EndCell()
	unusedData := executionProofUnusedDataBranch()
	data := cell.BeginCell().MustStoreRef(usedData).MustStoreRef(unusedData).EndCell()
	accountRoot := executionProofAccountRoot(t, code, data)

	goRes, proof, err := runGoCrossCodeWithExecutionProofVersion(accountRoot, vm.NewStack(), version)
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, vm.NewStack(), *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != 0 {
		t.Fatalf("v%d exit code = %d, want success", version, goRes.exitCode)
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

	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	accountBody, err := cell.UnwrapProof(proof, accountRoot.Hash())
	if err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
	loadedResultData, err := accountBody.PeekRef(1)
	if err != nil {
		t.Fatalf("peek proof data: %v", err)
	}
	resultReferencedData, err := loadedResultData.PeekRef(1)
	if err != nil {
		t.Fatalf("peek result stack referenced data: %v", err)
	}
	if resultReferencedData.IsSpecial() {
		t.Fatal("result stack slice should keep referenced data branch ordinary")
	}
}

func runGoCrossCodeWithExecutionProof(accountRoot *cell.Cell, stack *vm.Stack) (*crossRunResult, *cell.Cell, error) {
	return runGoCrossCodeWithExecutionProofVersion(accountRoot, stack, referenceRawRunGlobalVersion)
}

func runGoCrossCodeWithExecutionProofVersion(accountRoot *cell.Cell, stack *vm.Stack, globalVersion int) (*crossRunResult, *cell.Cell, error) {
	execStack := stack.Copy()
	if err := execStack.PushInt(big.NewInt(0)); err != nil {
		return nil, nil, err
	}

	machine, err := NewTVM().WithGlobalVersion(globalVersion)
	if err != nil {
		return nil, nil, err
	}
	res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{AccountRoot: accountRoot})
	if err != nil {
		return nil, nil, err
	}
	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}

	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, res.Proof, nil
}

func runGoCrossCodeWithExecutionProofConfigOverride(accountRoot *cell.Cell, stack *vm.Stack, machineVersion, globalVersion int) (*crossRunResult, *cell.Cell, error) {
	return runGoCrossCodeWithExecutionProofConfigOverrideAndLibraries(accountRoot, stack, machineVersion, globalVersion, nil)
}

func runGoCrossCodeWithExecutionProofConfigOverrideAndLibraries(accountRoot *cell.Cell, stack *vm.Stack, machineVersion, globalVersion int, libs []*cell.Cell) (*crossRunResult, *cell.Cell, error) {
	execStack := stack.Copy()
	if err := execStack.PushInt(big.NewInt(0)); err != nil {
		return nil, nil, err
	}

	machine, err := NewTVM().WithGlobalVersion(machineVersion)
	if err != nil {
		return nil, nil, err
	}
	res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		AccountRoot:      accountRoot,
		GlobalVersion:    globalVersion,
		GlobalVersionSet: true,
		Libraries:        libs,
	})
	if err != nil {
		return nil, nil, err
	}
	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}

	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, res.Proof, nil
}

func runGoCrossCodeWithExecutionProofConfigVersion(accountRoot *cell.Cell, stack *vm.Stack, globalVersion int, libs []*cell.Cell) (*crossRunResult, *cell.Cell, error) {
	execStack := stack.Copy()
	if err := execStack.PushInt(big.NewInt(0)); err != nil {
		return nil, nil, err
	}

	machine, err := NewTVM().WithGlobalVersion(globalVersion)
	if err != nil {
		return nil, nil, err
	}
	res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		AccountRoot: accountRoot,
		Libraries:   libs,
	})
	if err != nil {
		return nil, nil, err
	}
	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}

	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, res.Proof, nil
}
