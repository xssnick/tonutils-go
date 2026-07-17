package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzExecutionConfigVersion(raw byte) int {
	return tvmFuzzGlobalVersionByte(raw)
}

func executionConfigForGlobalVersion(t *testing.T, version int, always bool) ExecutionConfig {
	t.Helper()
	return ExecutionConfig{
		Config:                      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
		SignatureCheckAlwaysSucceed: always,
	}
}

func TestFuzzExecutionConfigVersionCoversSupportedRange(t *testing.T) {
	if vm.MaxSupportedGlobalVersion > 255 {
		t.Fatalf("execution config fuzz raw byte cannot cover max global version %d", vm.MaxSupportedGlobalVersion)
	}
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		if got := fuzzExecutionConfigVersion(byte(version)); got != version {
			t.Fatalf("version seed %d mapped to %d, want %d", version, got, version)
		}
	}
}

func FuzzExecutionConfigGlobalVersionPerRunEntrypoints(f *testing.F) {
	for version := byte(0); version <= byte(vm.MaxSupportedGlobalVersion); version++ {
		for entrypoint := byte(0); entrypoint < 3; entrypoint++ {
			f.Add(version, entrypoint, version^entrypoint^0x71)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawEntrypoint, sigTag byte) {
		version := fuzzExecutionConfigVersion(rawVersion)
		entrypoint := rawEntrypoint % 3
		tt := executionConfigSignatureCases[2]
		code := testOpcodeCell(t, tt.code)
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff

		machine := *NewTVM()

		baseCfg := executionConfigForGlobalVersion(t, vm.MaxSupportedGlobalVersion, true)
		defaultRes := runExecutionConfigSignatureEntrypointWithConfig(t, machine, code, tt, signature, entrypoint, baseCfg)
		assertExecutionConfigSignatureBool(t, tt, vm.MaxSupportedGlobalVersion, entrypoint, true, defaultRes, true)

		versionCfg := baseCfg
		versionCfg.Config = testPreparedBlockchainConfigWithVersion(t, uint32(version))
		configuredRes := runExecutionConfigSignatureEntrypointWithConfig(t, machine, code, tt, signature, entrypoint, versionCfg)
		if version < tt.minVersion {
			if configuredRes.exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s per-run v%d entrypoint=%d exit=%d, want invalid opcode", tt.name, version, entrypoint, configuredRes.exit)
			}
		} else {
			assertExecutionConfigSignatureBool(t, tt, version, entrypoint, true, configuredRes, true)
		}

		leakCheck := runExecutionConfigSignatureEntrypointWithConfig(t, machine, code, tt, signature, entrypoint, baseCfg)
		assertExecutionConfigSignatureBool(t, tt, vm.MaxSupportedGlobalVersion, entrypoint, true, leakCheck, true)
	})
}

func TestExecutionConfigGlobalVersionValidatesRange(t *testing.T) {
	futureVersion := uint32(vm.MaxSupportedGlobalVersion + 1)
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: futureVersion})
	if err != nil {
		t.Fatalf("build global version cell: %v", err)
	}
	dict := cell.NewDict(32)
	value := cell.BeginCell().MustStoreRef(versionCell).EndCell()
	if err = dict.SetIntKey(new(big.Int).SetUint64(uint64(tlb.ConfigParamGlobalVersion)), value); err != nil {
		t.Fatalf("store global version config param: %v", err)
	}
	root := dict.AsCell()

	cfg, err := prepareBlockchainConfigLenient(root)
	if err != nil {
		t.Fatalf("PrepareBlockchainConfig rejected future global version %d by default: %v", futureVersion, err)
	}
	if got := cfg.GlobalVersion(); got != uint32(vm.MaxSupportedGlobalVersion) {
		t.Fatalf("prepared effective global version = %d, want %d", got, vm.MaxSupportedGlobalVersion)
	}
}

func FuzzExecutionConfigSignatureCheckAlwaysSucceedRawEntrypoints(f *testing.F) {
	for version := byte(0); version <= byte(vm.MaxSupportedGlobalVersion); version++ {
		for entrypoint := byte(0); entrypoint < 3; entrypoint++ {
			f.Add(version, entrypoint, version^entrypoint^0x5a)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawEntrypoint, sigTag byte) {
		version := fuzzExecutionConfigVersion(rawVersion)
		entrypoint := rawEntrypoint % 3
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff

		machine := *NewTVM()
		code := testOpcodeCell(t, "f910")

		if got := runExecutionConfigSignatureCheckEntrypoint(t, machine, version, code, signature, entrypoint, false); got {
			t.Fatalf("v%d entrypoint=%d default run accepted forged CHKSIGNU", version, entrypoint)
		}
		if got := runExecutionConfigSignatureCheckEntrypoint(t, machine, version, code, signature, entrypoint, true); !got {
			t.Fatalf("v%d entrypoint=%d configured run rejected forged CHKSIGNU", version, entrypoint)
		}
		if got := runExecutionConfigSignatureCheckEntrypoint(t, machine, version, code, signature, entrypoint, false); got {
			t.Fatalf("v%d entrypoint=%d SignatureCheckAlwaysSucceed leaked into next run", version, entrypoint)
		}
	})
}

type executionConfigSignatureCase struct {
	name       string
	code       string
	minVersion int
	fromSlice  bool
	p256       bool
}

var executionConfigSignatureCases = []executionConfigSignatureCase{
	{name: "CHKSIGNU", code: "f910"},
	{name: "CHKSIGNS", code: "f911", fromSlice: true},
	{name: "P256_CHKSIGNU", code: "f914", minVersion: 4, p256: true},
	{name: "P256_CHKSIGNS", code: "f915", minVersion: 4, fromSlice: true, p256: true},
}

func FuzzExecutionConfigSignatureCheckAlwaysSucceedSignatureVariants(f *testing.F) {
	for version := byte(0); version <= byte(vm.MaxSupportedGlobalVersion); version++ {
		for entrypoint := byte(0); entrypoint < 3; entrypoint++ {
			for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
				f.Add(version, entrypoint, kind, version^entrypoint^kind^0x33)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawEntrypoint, rawKind, sigTag byte) {
		version := fuzzExecutionConfigVersion(rawVersion)
		entrypoint := rawEntrypoint % 3
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff

		machine := *NewTVM()
		code := testOpcodeCell(t, tt.code)

		if version < tt.minVersion {
			for _, always := range []bool{false, true} {
				res := runExecutionConfigSignatureEntrypoint(t, machine, version, code, tt, signature, entrypoint, always)
				if res.exit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s v%d entrypoint=%d always=%t exit=%d, want invalid opcode", tt.name, version, entrypoint, always, res.exit)
				}
			}
			return
		}

		defaultRes := runExecutionConfigSignatureEntrypoint(t, machine, version, code, tt, signature, entrypoint, false)
		assertExecutionConfigSignatureBool(t, tt, version, entrypoint, false, defaultRes, false)

		configuredRes := runExecutionConfigSignatureEntrypoint(t, machine, version, code, tt, signature, entrypoint, true)
		assertExecutionConfigSignatureBool(t, tt, version, entrypoint, true, configuredRes, true)

		leakCheck := runExecutionConfigSignatureEntrypoint(t, machine, version, code, tt, signature, entrypoint, false)
		assertExecutionConfigSignatureBool(t, tt, version, entrypoint, false, leakCheck, false)
	})
}

func FuzzExecutionConfigSignatureCheckAlwaysSucceedMalformedOperands(f *testing.F) {
	for version := byte(0); version <= byte(vm.MaxSupportedGlobalVersion); version++ {
		for entrypoint := byte(0); entrypoint < 3; entrypoint++ {
			for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
				f.Add(version, entrypoint, kind, byte(0), uint16(version)^uint16(entrypoint)<<4)
				f.Add(version, entrypoint, kind, byte(1), uint16(kind)<<6)
				f.Add(version, entrypoint, kind, byte(2), uint16(7))
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawEntrypoint, rawKind, rawMalformed byte, rawBits uint16) {
		version := fuzzExecutionConfigVersion(rawVersion)
		entrypoint := rawEntrypoint % 3
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		machine := *NewTVM()
		code := testOpcodeCell(t, tt.code)

		for _, always := range []bool{false, true} {
			stack := executionConfigMalformedSignatureStack(t, tt, rawMalformed, rawBits)
			res := runExecutionConfigSignatureStackEntrypoint(t, machine, version, code, stack, entrypoint, always)
			wantExit := int64(vmerr.CodeCellUnderflow)
			if version < tt.minVersion {
				wantExit = vmerr.CodeInvalidOpcode
			}
			if res.exit != wantExit {
				t.Fatalf("%s malformed v%d entrypoint=%d always=%t exit=%d, want %d", tt.name, version, entrypoint, always, res.exit, wantExit)
			}
		}
	})
}

func FuzzExecutionConfigLibrariesAndSignatureCheckAlwaysSucceed(f *testing.F) {
	for version := byte(0); version <= byte(vm.MaxSupportedGlobalVersion); version++ {
		for entrypoint := byte(0); entrypoint < 3; entrypoint++ {
			f.Add(version, entrypoint, version^entrypoint^0xa7)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawEntrypoint, sigTag byte) {
		version := fuzzExecutionConfigVersion(rawVersion)
		entrypoint := rawEntrypoint % 3
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff

		machine := *NewTVM()

		target := testOpcodeCell(t, "f910")
		code := mustLibraryCellForHash(t, target.Hash())
		libraries := []*cell.Cell{mustLibraryCollection(t, target)}
		cfg := executionConfigForGlobalVersion(t, version, false)
		cfg.Libraries = libraries

		if got := runExecutionConfigSignatureCheckEntrypointWithConfig(t, machine, code, signature, entrypoint, cfg); got {
			t.Fatalf("v%d entrypoint=%d library run accepted forged CHKSIGNU without flag", version, entrypoint)
		}
		cfg.SignatureCheckAlwaysSucceed = true
		if got := runExecutionConfigSignatureCheckEntrypointWithConfig(t, machine, code, signature, entrypoint, cfg); !got {
			t.Fatalf("v%d entrypoint=%d library run rejected forged CHKSIGNU with flag", version, entrypoint)
		}
		cfg.SignatureCheckAlwaysSucceed = false
		if got := runExecutionConfigSignatureCheckEntrypointWithConfig(t, machine, code, signature, entrypoint, cfg); got {
			t.Fatalf("v%d entrypoint=%d library run leaked SignatureCheckAlwaysSucceed into next run", version, entrypoint)
		}
	})
}

func FuzzExecutionConfigSignatureCheckAlwaysSucceedRunVMChild(f *testing.F) {
	for version := byte(0); version <= byte(vm.MaxSupportedGlobalVersion); version++ {
		for entrypoint := byte(0); entrypoint < 3; entrypoint++ {
			for modeKind := byte(0); modeKind < 4; modeKind++ {
				f.Add(version, entrypoint, modeKind, version^entrypoint^modeKind^0x4d)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawEntrypoint, rawModeKind, sigTag byte) {
		version := fuzzExecutionConfigVersion(rawVersion)
		entrypoint := rawEntrypoint % 3
		modeKind := rawModeKind % 4
		dynamicMode := modeKind&1 != 0
		mode := 0
		if modeKind&2 != 0 {
			mode = 128
		}
		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[63] = sigTag ^ 0xff

		machine := *NewTVM()

		defaultRes := runExecutionConfigRunVMChildSignatureCheck(t, machine, version, signature, entrypoint, dynamicMode, mode, false)
		configuredRes := runExecutionConfigRunVMChildSignatureCheck(t, machine, version, signature, entrypoint, dynamicMode, mode, true)
		leakCheck := runExecutionConfigRunVMChildSignatureCheck(t, machine, version, signature, entrypoint, dynamicMode, mode, false)
		if version < execop.RUNVM(0).MinGlobalVersion() {
			assertExecutionConfigRunVMChildInvalidOpcode(t, version, entrypoint, dynamicMode, mode, defaultRes)
			assertExecutionConfigRunVMChildInvalidOpcode(t, version, entrypoint, dynamicMode, mode, configuredRes)
			assertExecutionConfigRunVMChildInvalidOpcode(t, version, entrypoint, dynamicMode, mode, leakCheck)
			return
		}

		assertExecutionConfigRunVMChildBool(t, version, entrypoint, dynamicMode, mode, false, defaultRes, false)
		assertExecutionConfigRunVMChildBool(t, version, entrypoint, dynamicMode, mode, true, configuredRes, true)
		assertExecutionConfigRunVMChildBool(t, version, entrypoint, dynamicMode, mode, false, leakCheck, false)
	})
}

func runExecutionConfigSignatureCheckEntrypoint(t *testing.T, machine TVM, version int, code *cell.Cell, signature []byte, entrypoint byte, always bool) bool {
	t.Helper()

	return runExecutionConfigSignatureCheckEntrypointWithConfig(t, machine, code, signature, entrypoint, executionConfigForGlobalVersion(t, version, always))
}

func runExecutionConfigSignatureCheckEntrypointWithConfig(t *testing.T, machine TVM, code *cell.Cell, signature []byte, entrypoint byte, cfg ExecutionConfig) bool {
	t.Helper()

	data := cell.BeginCell().EndCell()
	stack := executionConfigSignatureCheckStack(t, signature)

	switch entrypoint {
	case 0:
		if _, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg); err != nil {
			t.Fatalf("ExecuteWithConfig always=%t failed: %v", cfg.SignatureCheckAlwaysSucceed, err)
		}
		return popExecutionConfigSignatureCheckResult(t, stack)
	case 1:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		if err != nil {
			t.Fatalf("ExecuteDetailedWithConfig always=%t failed: %v", cfg.SignatureCheckAlwaysSucceed, err)
		}
		if !vm.IsSuccessExitCode(res.ExitCode) {
			t.Fatalf("ExecuteDetailedWithConfig always=%t exit code = %d", cfg.SignatureCheckAlwaysSucceed, res.ExitCode)
		}
		return popExecutionConfigSignatureCheckResult(t, res.Stack)
	default:
		res, err := machine.ExecuteGetMethod(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		if err != nil {
			t.Fatalf("ExecuteGetMethodDetailedWithConfig always=%t failed: %v", cfg.SignatureCheckAlwaysSucceed, err)
		}
		if !vm.IsSuccessExitCode(res.ExitCode) {
			t.Fatalf("ExecuteGetMethodDetailedWithConfig always=%t exit code = %d", cfg.SignatureCheckAlwaysSucceed, res.ExitCode)
		}
		return popExecutionConfigSignatureCheckResult(t, res.Stack)
	}
}

type executionConfigRunVMChildRunResult struct {
	exit      int64
	childExit int64
	ok        bool
}

func runExecutionConfigRunVMChildSignatureCheck(t *testing.T, machine TVM, version int, signature []byte, entrypoint byte, dynamicMode bool, mode int, always bool) executionConfigRunVMChildRunResult {
	t.Helper()

	code := codeFromBuilders(t, execop.RUNVM(mode).Serialize())
	if dynamicMode {
		code = codeFromBuilders(t, execop.RUNVMX().Serialize())
	}
	data := cell.BeginCell().EndCell()
	stack := executionConfigRunVMChildStack(t, signature, dynamicMode, mode)
	cfg := executionConfigForGlobalVersion(t, version, always)

	switch entrypoint {
	case 0:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit != 0 {
			return executionConfigRunVMChildRunResult{exit: exit}
		}
		return popExecutionConfigRunVMChildResult(t, stack)
	case 1:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit != 0 {
			return executionConfigRunVMChildRunResult{exit: exit}
		}
		return popExecutionConfigRunVMChildResult(t, res.Stack)
	default:
		res, err := machine.ExecuteGetMethod(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit != 0 {
			return executionConfigRunVMChildRunResult{exit: exit}
		}
		return popExecutionConfigRunVMChildResult(t, res.Stack)
	}
}

func executionConfigRunVMChildStack(t *testing.T, signature []byte, dynamicMode bool, mode int) *vm.Stack {
	t.Helper()

	stack := executionConfigSignatureCheckStack(t, signature)
	if err := stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("push child stack size: %v", err)
	}
	if err := stack.PushSlice(testOpcodeCell(t, "f910").MustBeginParse()); err != nil {
		t.Fatalf("push child code: %v", err)
	}
	if dynamicMode {
		if err := stack.PushInt(big.NewInt(int64(mode))); err != nil {
			t.Fatalf("push RUNVMX mode: %v", err)
		}
	}
	return stack
}

func popExecutionConfigRunVMChildResult(t *testing.T, stack *vm.Stack) executionConfigRunVMChildRunResult {
	t.Helper()

	childExit, err := stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop RUNVM child exit code: %v", err)
	}
	return executionConfigRunVMChildRunResult{
		exit:      0,
		childExit: childExit.Int64(),
		ok:        popExecutionConfigSignatureCheckResult(t, stack),
	}
}

func assertExecutionConfigRunVMChildInvalidOpcode(t *testing.T, version int, entrypoint byte, dynamicMode bool, mode int, res executionConfigRunVMChildRunResult) {
	t.Helper()

	if res.exit != vmerr.CodeInvalidOpcode {
		t.Fatalf("%s mode=%d v%d entrypoint=%d exit=%d, want invalid opcode", executionConfigRunVMName(dynamicMode), mode, version, entrypoint, res.exit)
	}
}

func assertExecutionConfigRunVMChildBool(t *testing.T, version int, entrypoint byte, dynamicMode bool, mode int, always bool, res executionConfigRunVMChildRunResult, want bool) {
	t.Helper()

	if res.exit != 0 || res.childExit != 0 {
		t.Fatalf("%s mode=%d v%d entrypoint=%d always=%t exits=%d/%d, want success", executionConfigRunVMName(dynamicMode), mode, version, entrypoint, always, res.exit, res.childExit)
	}
	if res.ok != want {
		t.Fatalf("%s mode=%d v%d entrypoint=%d always=%t child CHKSIGNU=%t, want %t", executionConfigRunVMName(dynamicMode), mode, version, entrypoint, always, res.ok, want)
	}
}

func executionConfigRunVMName(dynamicMode bool) string {
	if dynamicMode {
		return "RUNVMX"
	}
	return "RUNVM"
}

type executionConfigSignatureRunResult struct {
	exit int64
	ok   bool
}

func runExecutionConfigSignatureStackEntrypoint(t *testing.T, machine TVM, version int, code *cell.Cell, stack *vm.Stack, entrypoint byte, always bool) executionConfigSignatureRunResult {
	t.Helper()

	data := cell.BeginCell().EndCell()
	cfg := executionConfigForGlobalVersion(t, version, always)
	switch entrypoint {
	case 0:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit == -1 {
			t.Fatalf("ExecuteWithConfig malformed always=%t failed: %v", always, err)
		}
		return executionConfigSignatureRunResult{exit: exit}
	case 1:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit == -1 {
			t.Fatalf("ExecuteDetailedWithConfig malformed always=%t failed: %v", always, err)
		}
		return executionConfigSignatureRunResult{exit: exit}
	default:
		res, err := machine.ExecuteGetMethod(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit == -1 {
			t.Fatalf("ExecuteGetMethodDetailedWithConfig malformed always=%t failed: %v", always, err)
		}
		return executionConfigSignatureRunResult{exit: exit}
	}
}

func runExecutionConfigSignatureEntrypoint(t *testing.T, machine TVM, version int, code *cell.Cell, tt executionConfigSignatureCase, signature []byte, entrypoint byte, always bool) executionConfigSignatureRunResult {
	t.Helper()

	return runExecutionConfigSignatureEntrypointWithConfig(t, machine, code, tt, signature, entrypoint, executionConfigForGlobalVersion(t, version, always))
}

func runExecutionConfigSignatureEntrypointWithConfig(t *testing.T, machine TVM, code *cell.Cell, tt executionConfigSignatureCase, signature []byte, entrypoint byte, cfg ExecutionConfig) executionConfigSignatureRunResult {
	t.Helper()

	data := cell.BeginCell().EndCell()
	stack := executionConfigSignatureStack(t, tt, signature)

	switch entrypoint {
	case 0:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit == -1 {
			t.Fatalf("ExecuteWithConfig %s always=%t failed: %v", tt.name, cfg.SignatureCheckAlwaysSucceed, err)
		}
		if !vm.IsSuccessExitCode(exit) {
			return executionConfigSignatureRunResult{exit: exit}
		}
		return executionConfigSignatureRunResult{exit: exit, ok: popExecutionConfigSignatureCheckResult(t, stack)}
	case 1:
		res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit == -1 {
			t.Fatalf("ExecuteDetailedWithConfig %s always=%t failed: %v", tt.name, cfg.SignatureCheckAlwaysSucceed, err)
		}
		if !vm.IsSuccessExitCode(exit) {
			return executionConfigSignatureRunResult{exit: exit}
		}
		return executionConfigSignatureRunResult{exit: exit, ok: popExecutionConfigSignatureCheckResult(t, res.Stack)}
	default:
		res, err := machine.ExecuteGetMethod(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, cfg)
		exit := exitCodeFromResult(res, err)
		if exit == -1 {
			t.Fatalf("ExecuteGetMethodDetailedWithConfig %s always=%t failed: %v", tt.name, cfg.SignatureCheckAlwaysSucceed, err)
		}
		if !vm.IsSuccessExitCode(exit) {
			return executionConfigSignatureRunResult{exit: exit}
		}
		return executionConfigSignatureRunResult{exit: exit, ok: popExecutionConfigSignatureCheckResult(t, res.Stack)}
	}
}

func assertExecutionConfigSignatureBool(t *testing.T, tt executionConfigSignatureCase, version int, entrypoint byte, always bool, res executionConfigSignatureRunResult, want bool) {
	t.Helper()

	if !vm.IsSuccessExitCode(res.exit) {
		t.Fatalf("%s v%d entrypoint=%d always=%t exit=%d, want success", tt.name, version, entrypoint, always, res.exit)
	}
	if res.ok != want {
		t.Fatalf("%s v%d entrypoint=%d always=%t result=%t, want %t", tt.name, version, entrypoint, always, res.ok, want)
	}
}

func executionConfigSignatureStack(t *testing.T, tt executionConfigSignatureCase, signature []byte) *vm.Stack {
	t.Helper()

	stack := vm.NewStack()
	if tt.fromSlice {
		if err := stack.PushSlice(cell.BeginCell().MustStoreSlice([]byte{0x10, 0x20, 0x30, 0x40}, 32).ToSlice()); err != nil {
			t.Fatalf("push message: %v", err)
		}
	} else {
		if err := stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push hash: %v", err)
		}
	}
	if err := stack.PushSlice(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()); err != nil {
		t.Fatalf("push signature: %v", err)
	}
	if tt.p256 {
		key := make([]byte, 33)
		key[0] = 0x05
		if err := stack.PushSlice(cell.BeginCell().MustStoreSlice(key, 264).ToSlice()); err != nil {
			t.Fatalf("push p256 key: %v", err)
		}
		return stack
	}
	if err := stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	return stack
}

func executionConfigMalformedSignatureStack(t *testing.T, tt executionConfigSignatureCase, rawMalformed byte, rawBits uint16) *vm.Stack {
	t.Helper()

	stack := vm.NewStack()
	malformed := rawMalformed % 3
	if tt.fromSlice {
		msgBits := uint(32)
		if malformed == 2 {
			msgBits = uint(rawBits%63) + 1
			if msgBits%8 == 0 {
				msgBits++
			}
		}
		if err := stack.PushSlice(executionConfigZeroSlice(msgBits)); err != nil {
			t.Fatalf("push message: %v", err)
		}
	} else if err := stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push hash: %v", err)
	}

	signatureBits := uint(512)
	if malformed == 0 || (malformed == 1 && !tt.p256) || (malformed == 2 && !tt.fromSlice) {
		signatureBits = uint(rawBits % 512)
	}
	if err := stack.PushSlice(executionConfigZeroSlice(signatureBits)); err != nil {
		t.Fatalf("push signature: %v", err)
	}

	if tt.p256 {
		keyBits := uint(264)
		if malformed == 1 {
			keyBits = uint(rawBits % 264)
		}
		if err := stack.PushSlice(executionConfigZeroSlice(keyBits)); err != nil {
			t.Fatalf("push p256 key: %v", err)
		}
		return stack
	}
	if err := stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	return stack
}

func executionConfigZeroSlice(bits uint) *cell.Slice {
	raw := make([]byte, (bits+7)/8)
	return cell.BeginCell().MustStoreSlice(raw, bits).ToSlice()
}

func executionConfigSignatureCheckStack(t *testing.T, signature []byte) *vm.Stack {
	t.Helper()

	stack := vm.NewStack()
	if err := stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push hash: %v", err)
	}
	if err := stack.PushSlice(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()); err != nil {
		t.Fatalf("push signature: %v", err)
	}
	if err := stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("push key: %v", err)
	}
	return stack
}

func popExecutionConfigSignatureCheckResult(t *testing.T, stack *vm.Stack) bool {
	t.Helper()

	got, err := stack.PopBool()
	if err != nil {
		t.Fatalf("pop CHKSIGNU result: %v", err)
	}
	return got
}
