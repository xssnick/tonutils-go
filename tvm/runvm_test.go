package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func popInt64(t *testing.T, stack *vmcore.Stack) int64 {
	t.Helper()

	val, err := stack.PopIntFinite()
	if err != nil {
		t.Fatalf("failed to pop int: %v", err)
	}
	return val.Int64()
}

func popMaybeCell(t *testing.T, stack *vmcore.Stack) *cell.Cell {
	t.Helper()

	val, err := stack.PopMaybeCell()
	if err != nil {
		t.Fatalf("failed to pop maybe cell: %v", err)
	}
	return val
}

type runVMFailedDataActionsResult struct {
	parentExit  int64
	childExit   int64
	exception   int64
	data        *cell.Cell
	actions     *cell.Cell
	restStackSz int
}

func runRunVMFailedDataActions(t *testing.T, version int, dynamicMode bool, initialData *cell.Cell) runVMFailedDataActionsResult {
	t.Helper()

	mode := 4 | 32
	stackValues := []any{
		int64(0),
		codeFromOpcodes(t, 0xF205).MustBeginParse(),
		initialData,
	}
	parentCode := codeFromBuilders(t, execop.RUNVM(mode).Serialize())
	if dynamicMode {
		stackValues = append(stackValues, int64(mode))
		parentCode = codeFromBuilders(t, execop.RUNVMX().Serialize())
	}

	_, res, err := runRawCodeWithEnv(
		t,
		parentCode,
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		version,
		stackValues...,
	)

	out := runVMFailedDataActionsResult{parentExit: exitCodeFromResult(res, err)}
	if out.parentExit != 0 || res == nil {
		return out
	}

	stack := res.Stack
	out.actions = popMaybeCell(t, stack)
	out.data = popMaybeCell(t, stack)
	out.childExit = popInt64(t, stack)
	out.exception = popInt64(t, stack)
	out.restStackSz = stack.Len()
	return out
}

func assertRunVMFailedDataActionsGlobalVersion(t *testing.T, version int, dynamicMode bool, initialData *cell.Cell) {
	t.Helper()

	res := runRunVMFailedDataActions(t, version, dynamicMode, initialData)
	name := runVMOpcodeName(dynamicMode)
	if version < 4 {
		if res.parentExit != vmerr.CodeInvalidOpcode {
			t.Fatalf("%s v%d parent exit = %d, want invalid opcode", name, version, res.parentExit)
		}
		return
	}

	if res.parentExit != 0 {
		t.Fatalf("%s v%d parent exit = %d, want 0", name, version, res.parentExit)
	}
	if res.childExit != 5 {
		t.Fatalf("%s v%d child exit = %d, want 5", name, version, res.childExit)
	}
	if res.exception != 0 {
		t.Fatalf("%s v%d child exception arg = %d, want 0", name, version, res.exception)
	}
	if res.restStackSz != 0 {
		t.Fatalf("%s v%d left %d extra stack items", name, version, res.restStackSz)
	}

	if version < 11 {
		if res.data == nil {
			t.Fatalf("%s v%d data = nil, want current child c4", name, version)
		}
		if !bytes.Equal(res.data.Hash(), initialData.Hash()) {
			t.Fatalf("%s v%d data hash mismatch", name, version)
		}
		if res.actions == nil {
			t.Fatalf("%s v%d actions = nil, want current child c5", name, version)
		}
		return
	}

	if res.data != nil {
		t.Fatalf("%s v%d data is non-nil, want nil", name, version)
	}
	if res.actions != nil {
		t.Fatalf("%s v%d actions is non-nil, want nil", name, version)
	}
}

func makeRunvmSameC3CallDictChild(t *testing.T, id, tail int64) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DUP().Serialize(),
		execop.IFRET().Serialize(),
		stackop.DROP().Serialize(),
		execop.CALLDICT(int(id)).Serialize(),
		stackop.PUSHINT(big.NewInt(tail)).Serialize(),
	)
}

func makeRunvmSameC3JmpDictChild(t *testing.T, id int64) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DUP().Serialize(),
		execop.IFRET().Serialize(),
		stackop.DROP().Serialize(),
		execop.JMPDICT(int(id)).Serialize(),
		stackop.PUSHINT(big.NewInt(99)).Serialize(),
	)
}

func makeRunvmSameC3PrepareDictChild(t *testing.T, id, tail int64) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t,
		stackop.DUP().Serialize(),
		execop.IFRET().Serialize(),
		stackop.DROP().Serialize(),
		execop.PREPAREDICT(int(id)).Serialize(),
		execop.EXECUTE().Serialize(),
		stackop.PUSHINT(big.NewInt(tail)).Serialize(),
	)
}

func makeRunvmSignatureCheckAlwaysVariantChildCode(t *testing.T, tt executionConfigSignatureCase, signature []byte) *cell.Cell {
	t.Helper()

	builders := make([]*cell.Builder, 0, 4)
	if tt.fromSlice {
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice([]byte{0x10, 0x20, 0x30, 0x40}, 32).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(0)).Serialize())
	}

	builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()).Serialize())
	if tt.p256 {
		key := make([]byte, 33)
		key[0] = 0x05
		builders = append(builders, stackop.PUSHSLICE(cell.BeginCell().MustStoreSlice(key, 264).ToSlice()).Serialize())
	} else {
		builders = append(builders, stackop.PUSHINT(big.NewInt(2)).Serialize())
	}

	builders = append(builders, signatureCheckAlwaysVariantOpcode(t, tt))
	return codeFromBuilders(t, builders...)
}

type runvmSignatureCheckAlwaysResult struct {
	parentExit int64
	childExit  int64
	ok         bool
}

func runRunvmSignatureCheckAlwaysVariant(t *testing.T, version int, childCode *cell.Cell, always bool) runvmSignatureCheckAlwaysResult {
	t.Helper()

	machine := NewTVM()

	stack := vmcore.NewStack()
	if err := stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push child stack size: %v", err)
	}
	if err := stack.PushSlice(childCode.MustBeginParse()); err != nil {
		t.Fatalf("push child code: %v", err)
	}

	res, err := machine.Execute(
		codeFromBuilders(t, execop.RUNVM(0).Serialize()),
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		vmcore.GasWithLimit(100_000),
		stack,
		ExecutionConfig{
			Config:                      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
			SignatureCheckAlwaysSucceed: always,
		})

	parentExit := exitCodeFromResult(res, err)
	if parentExit == -1 {
		t.Fatalf("RUNVM parent always=%v failed: %v", always, err)
	}
	if !vmcore.IsSuccessExitCode(parentExit) {
		return runvmSignatureCheckAlwaysResult{parentExit: parentExit}
	}

	childExit := popInt64(t, res.Stack)
	if !vmcore.IsSuccessExitCode(childExit) {
		return runvmSignatureCheckAlwaysResult{parentExit: parentExit, childExit: childExit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop child signature check result always=%v: %v", always, err)
	}
	return runvmSignatureCheckAlwaysResult{parentExit: parentExit, childExit: childExit, ok: got}
}

func runRunvmXSignatureCheckAlwaysVariant(t *testing.T, version int, childCode *cell.Cell, always bool) runvmSignatureCheckAlwaysResult {
	t.Helper()

	machine := NewTVM()

	stack := vmcore.NewStack()
	if err := stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push child stack size: %v", err)
	}
	if err := stack.PushSlice(childCode.MustBeginParse()); err != nil {
		t.Fatalf("push child code: %v", err)
	}
	if err := stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push RUNVMX mode: %v", err)
	}

	res, err := machine.Execute(
		codeFromBuilders(t, execop.RUNVMX().Serialize()),
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		vmcore.GasWithLimit(100_000),
		stack,
		ExecutionConfig{
			Config:                      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
			SignatureCheckAlwaysSucceed: always,
		})

	parentExit := exitCodeFromResult(res, err)
	if parentExit == -1 {
		t.Fatalf("RUNVMX parent always=%v failed: %v", always, err)
	}
	if !vmcore.IsSuccessExitCode(parentExit) {
		return runvmSignatureCheckAlwaysResult{parentExit: parentExit}
	}

	childExit := popInt64(t, res.Stack)
	if !vmcore.IsSuccessExitCode(childExit) {
		return runvmSignatureCheckAlwaysResult{parentExit: parentExit, childExit: childExit}
	}
	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop RUNVMX child signature check result always=%v: %v", always, err)
	}
	return runvmSignatureCheckAlwaysResult{parentExit: parentExit, childExit: childExit, ok: got}
}

func assertRunvmSignatureCheckAlwaysVariant(t *testing.T, tt executionConfigSignatureCase, version int, always bool, res runvmSignatureCheckAlwaysResult, want bool) {
	t.Helper()

	if !vmcore.IsSuccessExitCode(res.parentExit) {
		t.Fatalf("%s v%d always=%t parent exit=%d, want success", tt.name, version, always, res.parentExit)
	}
	if !vmcore.IsSuccessExitCode(res.childExit) {
		t.Fatalf("%s v%d always=%t child exit=%d, want success", tt.name, version, always, res.childExit)
	}
	if res.ok != want {
		t.Fatalf("%s v%d always=%t child result=%t, want %t", tt.name, version, always, res.ok, want)
	}
}

func TestRunVMSignatureCheckAlwaysSucceedPerRun(t *testing.T) {
	signature := make([]byte, 64)
	signature[0] = 0x7A
	signature[31] = 0xA7
	signature[63] = 0xE1

	for _, tt := range executionConfigSignatureCases {
		t.Run(tt.name, func(t *testing.T) {
			childCode := makeRunvmSignatureCheckAlwaysVariantChildCode(t, tt, signature)

			for version := 0; version <= vmcore.MaxSupportedGlobalVersion; version++ {
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					if version < 4 {
						for _, always := range []bool{false, true, false} {
							res := runRunvmSignatureCheckAlwaysVariant(t, version, childCode, always)
							if res.parentExit != vmerr.CodeInvalidOpcode {
								t.Fatalf("%s v%d always=%t parent exit=%d, want invalid opcode", tt.name, version, always, res.parentExit)
							}
						}
						return
					}

					assertRunvmSignatureCheckAlwaysVariant(t, tt, version, false, runRunvmSignatureCheckAlwaysVariant(t, version, childCode, false), false)
					assertRunvmSignatureCheckAlwaysVariant(t, tt, version, true, runRunvmSignatureCheckAlwaysVariant(t, version, childCode, true), true)
					assertRunvmSignatureCheckAlwaysVariant(t, tt, version, false, runRunvmSignatureCheckAlwaysVariant(t, version, childCode, false), false)
				})
			}
		})
	}
}

func FuzzRunVMSignatureCheckAlwaysSucceedPerRun(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, byte(version)^kind^0x5C)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind, sigTag byte) {
		version := tvmFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[31] = sigTag ^ 0x66
		signature[63] = sigTag ^ 0xff

		childCode := makeRunvmSignatureCheckAlwaysVariantChildCode(t, tt, signature)
		if version < 4 {
			for _, always := range []bool{false, true, false} {
				res := runRunvmSignatureCheckAlwaysVariant(t, version, childCode, always)
				if res.parentExit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s v%d always=%t parent exit=%d, want invalid opcode", tt.name, version, always, res.parentExit)
				}
			}
			return
		}

		assertRunvmSignatureCheckAlwaysVariant(t, tt, version, false, runRunvmSignatureCheckAlwaysVariant(t, version, childCode, false), false)
		assertRunvmSignatureCheckAlwaysVariant(t, tt, version, true, runRunvmSignatureCheckAlwaysVariant(t, version, childCode, true), true)
		assertRunvmSignatureCheckAlwaysVariant(t, tt, version, false, runRunvmSignatureCheckAlwaysVariant(t, version, childCode, false), false)
	})
}

func FuzzRunVMXSignatureCheckAlwaysSucceedPerRun(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < byte(len(executionConfigSignatureCases)); kind++ {
			f.Add(version, kind, byte(version)^kind^0xA9)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind, sigTag byte) {
		version := tvmFuzzGlobalVersion(rawVersion)
		tt := executionConfigSignatureCases[int(rawKind)%len(executionConfigSignatureCases)]

		signature := make([]byte, 64)
		signature[0] = sigTag
		signature[31] = sigTag ^ 0x66
		signature[63] = sigTag ^ 0xff

		childCode := makeRunvmSignatureCheckAlwaysVariantChildCode(t, tt, signature)
		if version < 4 {
			for _, always := range []bool{false, true, false} {
				res := runRunvmXSignatureCheckAlwaysVariant(t, version, childCode, always)
				if res.parentExit != vmerr.CodeInvalidOpcode {
					t.Fatalf("%s RUNVMX v%d always=%t parent exit=%d, want invalid opcode", tt.name, version, always, res.parentExit)
				}
			}
			return
		}

		assertRunvmSignatureCheckAlwaysVariant(t, tt, version, false, runRunvmXSignatureCheckAlwaysVariant(t, version, childCode, false), false)
		assertRunvmSignatureCheckAlwaysVariant(t, tt, version, true, runRunvmXSignatureCheckAlwaysVariant(t, version, childCode, true), true)
		assertRunvmSignatureCheckAlwaysVariant(t, tt, version, false, runRunvmXSignatureCheckAlwaysVariant(t, version, childCode, false), false)
	})
}

func TestRunVMGoSemantics(t *testing.T) {
	t.Run("RunvmReturnsChildStackAndExitCode", func(t *testing.T) {
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(0).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}

		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 7 {
			t.Fatalf("expected returned value 7, got %d", got)
		}
	})

	t.Run("RunvmxReadsModeFromStack", func(t *testing.T) {
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(9)).Serialize())
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVMX().Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
			int64(0),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}

		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 9 {
			t.Fatalf("expected returned value 9, got %d", got)
		}
	})

	t.Run("PushZeroWorksOnlyWithSameC3", func(t *testing.T) {
		dropCode := codeFromBuilders(t, stackop.DROP().Serialize())

		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(3).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			dropCode.MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0 for RUNVM 3, got %d", code)
		}
		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}

		stack, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(2).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			dropCode.MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected parent exit 0, got %d", code)
		}
		stack = res.Stack
		if got := popInt64(t, stack); got != 2 {
			t.Fatalf("expected child exit code 2, got %d", got)
		}
	})

	t.Run("SameC3StartupContinuationWorksWithDictJumps", func(t *testing.T) {
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(3).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			makeRunvmSameC3CallDictChild(t, 5, 9).MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0 for same_c3 calldict, got %d", code)
		}
		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 9 {
			t.Fatalf("expected tail value 9 after CALLDICT, got %d", got)
		}
		if got := popInt64(t, stack); got != 5 {
			t.Fatalf("expected dictionary argument 5 returned from startup continuation, got %d", got)
		}

		stack, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(3).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			makeRunvmSameC3JmpDictChild(t, 8).MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0 for same_c3 jmpdict, got %d", code)
		}
		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 8 {
			t.Fatalf("expected dictionary argument 8 returned from startup continuation, got %d", got)
		}
		if stack.Len() != 0 {
			t.Fatalf("expected no extra stack items after JMPDICT, got %d", stack.Len())
		}

		stack, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(3).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			makeRunvmSameC3PrepareDictChild(t, 9, 11).MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0 for same_c3 preparedict, got %d", code)
		}
		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 11 {
			t.Fatalf("expected tail value 11 after PREPAREDICT, got %d", got)
		}
		if got := popInt64(t, stack); got != 9 {
			t.Fatalf("expected dictionary argument 9 returned from startup continuation, got %d", got)
		}
	})

	t.Run("RunvmLoadsChildC7", func(t *testing.T) {
		childC7 := tuple.NewTupleValue(tuple.NewTupleValue(big.NewInt(11), big.NewInt(22)))
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(16).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			codeFromBuilders(t, funcsop.GETPARAM(1).Serialize()).MustBeginParse(),
			childC7,
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}

		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 22 {
			t.Fatalf("expected GETPARAM(1)=22, got %d", got)
		}
	})

	t.Run("RunvmReturnsCommittedDataAndActions", func(t *testing.T) {
		initialData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		returnedData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		returnedActions := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

		child := codeFromBuilders(t,
			stackop.PUSHREF(returnedData).Serialize(),
			execop.POPCTR(4).Serialize(),
			stackop.PUSHREF(returnedActions).Serialize(),
			execop.POPCTR(5).Serialize(),
		)

		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(4|32).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
			initialData,
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}

		stack = res.Stack
		actions := popMaybeCell(t, stack)
		data := popMaybeCell(t, stack)
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if actions == nil || !bytes.Equal(actions.Hash(), returnedActions.Hash()) {
			t.Fatalf("unexpected returned actions")
		}
		if data == nil || !bytes.Equal(data.Hash(), returnedData.Hash()) {
			t.Fatalf("unexpected returned data")
		}
	})

	t.Run("RunvmReturnValuesExact", func(t *testing.T) {
		child := codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(7)).Serialize(),
			stackop.PUSHINT(big.NewInt(8)).Serialize(),
		)
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(256).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
			int64(1),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected parent exit 0, got %d", code)
		}
		stack = res.Stack
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exit code 0, got %d", got)
		}
		if got := popInt64(t, stack); got != 8 {
			t.Fatalf("expected top return value 8, got %d", got)
		}
		if stack.Len() != 0 {
			t.Fatalf("expected exactly one returned value, stack len=%d", stack.Len())
		}
	})

	t.Run("RunvmReturnValuesUnderflowBecomesChildStackUnderflow", func(t *testing.T) {
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(256).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
			int64(2),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected parent exit 0, got %d", code)
		}

		stack = res.Stack
		if got := popInt64(t, stack); got != ^int64(2) {
			t.Fatalf("expected synthetic child exit code -3, got %d", got)
		}
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected stack-underflow sentinel 0, got %d", got)
		}
	})

	t.Run("RunvmRejectsInvalidFlags", func(t *testing.T) {
		_, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(512).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			codeFromBuilders(t).MustBeginParse(),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("expected range check for invalid flags, got %d", code)
		}
	})

	t.Run("RunvmReturnsGasAndCommittedDataOnFailure", func(t *testing.T) {
		returnedData := cell.BeginCell().MustStoreUInt(0xDD, 8).EndCell()
		child := codeFromBuilders(t,
			stackop.PUSHREF(returnedData).Serialize(),
			execop.POPCTR(4).Serialize(),
			funcsop.COMMIT().Serialize(),
			codeFromOpcodes(t, 0xF22A).ToBuilder(),
		)

		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(4|8).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
			cell.BeginCell().EndCell(),
			int64(10_000),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected parent exit 0, got %d", code)
		}

		stack = res.Stack
		if gas := popInt64(t, stack); gas <= 0 {
			t.Fatalf("expected positive child gas, got %d", gas)
		}
		data := popMaybeCell(t, stack)
		if data == nil || !bytes.Equal(data.Hash(), returnedData.Hash()) {
			t.Fatalf("unexpected returned committed data")
		}
		if got := popInt64(t, stack); got != 42 {
			t.Fatalf("expected child exit code 42, got %d", got)
		}
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exception argument 0, got %d", got)
		}
	})

	t.Run("RunvmReturnsGasOnOutOfGasWithGasMax", func(t *testing.T) {
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(8|64).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.MaxSupportedGlobalVersion,
			int64(0),
			child.MustBeginParse(),
			int64(0),
			int64(100),
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected parent exit 0, got %d", code)
		}
		stack = res.Stack
		reportedGas := popInt64(t, stack)
		if got := popInt64(t, stack); got != ^int64(vmerr.CodeOutOfGas) {
			t.Fatalf("expected child out-of-gas exit code %d, got %d", ^int64(vmerr.CodeOutOfGas), got)
		}
		innerGas := popInt64(t, stack)
		if reportedGas <= 0 || innerGas <= 0 {
			t.Fatalf("expected positive reported gas values, got reported=%d inner=%d", reportedGas, innerGas)
		}
	})

	t.Run("RunvmIsolateGasSeparatesLoadedCells", func(t *testing.T) {
		refCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		child := codeFromBuilders(t, cellsliceop.CTOS().Serialize(), stackop.DROP().Serialize())

		makeCode := func(mode int) *cell.Cell {
			return codeFromBuilders(t,
				stackop.PUSHREF(refCell).Serialize(),
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				stackop.PUSHREF(refCell).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				stackop.PUSHSLICEINLINE(child.MustBeginParse()).Serialize(),
				execop.RUNVM(mode).Serialize(),
			)
		}

		_, baseRes, err := runRawCodeWithEnv(t, makeCode(0), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.MaxSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("non-isolated unexpected error: %v", err)
		}
		_, isoRes, err := runRawCodeWithEnv(t, makeCode(128), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.MaxSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("isolated unexpected error: %v", err)
		}
		if isoRes.GasUsed <= baseRes.GasUsed {
			t.Fatalf("expected isolated run to use more gas, got base=%d isolated=%d", baseRes.GasUsed, isoRes.GasUsed)
		}
	})

	t.Run("RunvmFailedChildDataActionsFollowGlobalVersion", func(t *testing.T) {
		for _, dynamic := range []bool{false, true} {
			for _, version := range []int{4, 10, 11, 14} {
				t.Run(runVMOpcodeName(dynamic)+"_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					initialData := cell.BeginCell().MustStoreUInt(uint64(version), 8).EndCell()
					assertRunVMFailedDataActionsGlobalVersion(t, version, dynamic, initialData)
				})
			}
		}
	})
}

func FuzzRunVMFailedChildDataActionsGlobalVersion(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		f.Add(version, false, byte(version)^0x34)
		f.Add(version, true, byte(version)^0xC3)
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, dynamic bool, seed byte) {
		version := tvmFuzzGlobalVersion(rawVersion)
		initialData := cell.BeginCell().MustStoreUInt(uint64(seed), 8).EndCell()
		assertRunVMFailedDataActionsGlobalVersion(t, version, dynamic, initialData)
	})
}

type runVMModeEquivalenceCase struct {
	name   string
	mode   int
	values func() []any
	check  func(t *testing.T, label string, stack *vmcore.Stack)
}

func runVMModeEquivalenceCases(t *testing.T, rawSeed int64) []runVMModeEquivalenceCase {
	t.Helper()

	seed := rawSeed % 1_000_000
	dictID := rawSeed % 16
	if dictID < 0 {
		dictID = -dictID
	}
	dictID++

	initialData := cell.BeginCell().MustStoreUInt(uint64(byte(rawSeed)), 8).EndCell()
	returnedData := cell.BeginCell().MustStoreUInt(uint64(byte(rawSeed)^0x55), 8).EndCell()
	returnedActions := cell.BeginCell().MustStoreUInt(uint64(byte(rawSeed)^0xAA), 8).EndCell()

	return []runVMModeEquivalenceCase{
		{
			name: "basic",
			mode: 0,
			values: func() []any {
				child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(seed)).Serialize())
				return []any{int64(0), child.MustBeginParse()}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s child exit = %d, want success", label, got)
				}
				if got := popInt64(t, stack); got != seed {
					t.Fatalf("%s returned value = %d, want %d", label, got, seed)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "push_zero_without_same_c3",
			mode: 2,
			values: func() []any {
				child := codeFromBuilders(t, stackop.DROP().Serialize())
				return []any{int64(0), child.MustBeginParse()}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if got := popInt64(t, stack); got != vmerr.CodeStackUnderflow {
					t.Fatalf("%s child exit = %d, want stack underflow", label, got)
				}
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s exception argument = %d, want 0", label, got)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "same_c3_jmpdict",
			mode: 3,
			values: func() []any {
				return []any{int64(0), makeRunvmSameC3JmpDictChild(t, dictID).MustBeginParse()}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s child exit = %d, want success", label, got)
				}
				if got := popInt64(t, stack); got != dictID {
					t.Fatalf("%s dictionary argument = %d, want %d", label, got, dictID)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "child_c7",
			mode: 16,
			values: func() []any {
				child := codeFromBuilders(t, funcsop.GETPARAM(1).Serialize())
				childC7 := tuple.NewTupleValue(tuple.NewTupleValue(big.NewInt(seed), big.NewInt(seed+1)))
				return []any{int64(0), child.MustBeginParse(), childC7}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s child exit = %d, want success", label, got)
				}
				if got := popInt64(t, stack); got != seed+1 {
					t.Fatalf("%s GETPARAM(1) = %d, want %d", label, got, seed+1)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "returns_data_actions",
			mode: 4 | 32,
			values: func() []any {
				child := codeFromBuilders(t,
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					stackop.PUSHREF(returnedActions).Serialize(),
					execop.POPCTR(5).Serialize(),
				)
				return []any{int64(0), child.MustBeginParse(), initialData}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				actions := popMaybeCell(t, stack)
				data := popMaybeCell(t, stack)
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s child exit = %d, want success", label, got)
				}
				if actions == nil || !bytes.Equal(actions.Hash(), returnedActions.Hash()) {
					t.Fatalf("%s returned unexpected actions", label)
				}
				if data == nil || !bytes.Equal(data.Hash(), returnedData.Hash()) {
					t.Fatalf("%s returned unexpected data", label)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "return_values_exact",
			mode: 256,
			values: func() []any {
				child := codeFromBuilders(t,
					stackop.PUSHINT(big.NewInt(seed)).Serialize(),
					stackop.PUSHINT(big.NewInt(seed+1)).Serialize(),
				)
				return []any{int64(0), child.MustBeginParse(), int64(1)}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s child exit = %d, want success", label, got)
				}
				if got := popInt64(t, stack); got != seed+1 {
					t.Fatalf("%s return value = %d, want %d", label, got, seed+1)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "return_values_underflow",
			mode: 256,
			values: func() []any {
				child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(seed)).Serialize())
				return []any{int64(0), child.MustBeginParse(), int64(2)}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if got := popInt64(t, stack); got != ^int64(vmerr.CodeStackUnderflow) {
					t.Fatalf("%s child exit = %d, want synthetic stack underflow", label, got)
				}
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s stack-underflow sentinel = %d, want 0", label, got)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "commit_then_throw",
			mode: 4 | 8,
			values: func() []any {
				child := codeFromBuilders(t,
					stackop.PUSHREF(returnedData).Serialize(),
					execop.POPCTR(4).Serialize(),
					funcsop.COMMIT().Serialize(),
					codeFromOpcodes(t, 0xF22A).ToBuilder(),
				)
				return []any{int64(0), child.MustBeginParse(), initialData, int64(10_000)}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				if gas := popInt64(t, stack); gas <= 0 {
					t.Fatalf("%s child gas = %d, want positive", label, gas)
				}
				data := popMaybeCell(t, stack)
				if data == nil || !bytes.Equal(data.Hash(), returnedData.Hash()) {
					t.Fatalf("%s returned unexpected committed data", label)
				}
				if got := popInt64(t, stack); got != 42 {
					t.Fatalf("%s child exit = %d, want 42", label, got)
				}
				if got := popInt64(t, stack); got != 0 {
					t.Fatalf("%s exception argument = %d, want 0", label, got)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
		{
			name: "oog_with_gas_max",
			mode: 8 | 64,
			values: func() []any {
				child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(seed)).Serialize())
				return []any{int64(0), child.MustBeginParse(), int64(0), int64(100)}
			},
			check: func(t *testing.T, label string, stack *vmcore.Stack) {
				t.Helper()
				reportedGas := popInt64(t, stack)
				if got := popInt64(t, stack); got != ^int64(vmerr.CodeOutOfGas) {
					t.Fatalf("%s child exit = %d, want out of gas", label, got)
				}
				innerGas := popInt64(t, stack)
				if reportedGas <= 0 || innerGas <= 0 {
					t.Fatalf("%s gas values = reported %d inner %d, want positive", label, reportedGas, innerGas)
				}
				assertRunVMModeEquivalenceStackEmpty(t, label, stack)
			},
		},
	}
}

func assertRunVMModeEquivalenceStackEmpty(t *testing.T, label string, stack *vmcore.Stack) {
	t.Helper()

	if stack.Len() != 0 {
		t.Fatalf("%s stack len = %d, want 0", label, stack.Len())
	}
}

func FuzzRunVMXDynamicModeMatchesStaticRUNVM(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < 9; kind++ {
			f.Add(version, kind, version*31+int64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind byte, rawSeed int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		cases := runVMModeEquivalenceCases(t, rawSeed)
		tc := cases[int(rawKind)%len(cases)]

		staticExit, staticRes := runRunVMModeEquivalenceVariant(t, version, tc, false)
		dynamicExit, dynamicRes := runRunVMModeEquivalenceVariant(t, version, tc, true)
		if staticExit != dynamicExit {
			t.Fatalf("%s v%d parent exit mismatch: RUNVM=%d RUNVMX=%d", tc.name, version, staticExit, dynamicExit)
		}
		if version < 4 {
			if staticExit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s v%d parent exit = %d, want invalid opcode", tc.name, version, staticExit)
			}
			return
		}
		if staticExit != 0 {
			t.Fatalf("%s v%d parent exit = %d, want success", tc.name, version, staticExit)
		}

		tc.check(t, "RUNVM "+tc.name, staticRes.Stack)
		tc.check(t, "RUNVMX "+tc.name, dynamicRes.Stack)
	})
}

func runRunVMModeEquivalenceVariant(t *testing.T, version int, tc runVMModeEquivalenceCase, dynamicMode bool) (int64, *ExecutionResult) {
	t.Helper()

	stackValues := tc.values()
	parentCode := codeFromBuilders(t, execop.RUNVM(tc.mode).Serialize())
	if dynamicMode {
		stackValues = append(stackValues, int64(tc.mode))
		parentCode = codeFromBuilders(t, execop.RUNVMX().Serialize())
	}

	_, res, err := runRawCodeWithEnv(
		t,
		parentCode,
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		version,
		stackValues...,
	)
	code := exitCodeFromResult(res, err)
	if code == -1 {
		t.Fatalf("%s %s v%d failed with non-VM error: %v", runVMOpcodeName(dynamicMode), tc.name, version, err)
	}
	return code, res
}

func runVMInvalidFlagsMode(rawMode int64) int {
	offset := rawMode % (4096 - 512)
	if offset < 0 {
		offset = -offset
	}
	return 512 + int(offset)
}

func FuzzRunVMInvalidFlagsReturnsExceptionArg(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for _, mode := range []int64{512, 513, 1023, 2048, 4095} {
			f.Add(version, mode)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawMode int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		mode := runVMInvalidFlagsMode(rawMode)
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(rawMode%1000)).Serialize())

		_, res, err := runRawCodeWithEnv(
			t,
			codeFromBuilders(t, execop.RUNVM(mode).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			version,
			int64(0),
			child.MustBeginParse(),
		)

		exit := exitCodeFromResult(res, err)
		if version < 4 {
			if exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("RUNVM %d v%d exit = %d, want invalid opcode", mode, version, exit)
			}
			return
		}
		if exit != vmerr.CodeRangeCheck {
			t.Fatalf("RUNVM %d v%d exit = %d, want range check", mode, version, exit)
		}
		assertRunVMTopLevelExceptionArg(t, fmt.Sprintf("RUNVM %d", mode), res.Stack)
	})
}

type runVMReturnCountValidationCase struct {
	name  string
	count any
	want  int64
}

func runVMReturnCountValidationCases(rawSeed int64) []runVMReturnCountValidationCase {
	offset := rawSeed % 1024
	if offset < 0 {
		offset = -offset
	}

	return []runVMReturnCountValidationCase{
		{
			name:  "negative",
			count: int64(-1 - offset),
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "too_large",
			count: int64(1<<30 + 1 + offset),
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "huge_int",
			count: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(offset)),
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "nan",
			count: vmcore.NaN{},
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "cell",
			count: cell.BeginCell().EndCell(),
			want:  vmerr.CodeTypeCheck,
		},
	}
}

func FuzzRunVMReturnCountValidationReturnsExceptionArg(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < 5; kind++ {
			f.Add(version, kind, false, version*59+int64(kind))
			f.Add(version, kind, true, version*67+int64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind byte, dynamicMode bool, rawSeed int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		tc := runVMReturnCountValidationCases(rawSeed)[int(rawKind)%5]
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(rawSeed%1000)).Serialize())

		parentCode := codeFromBuilders(t, execop.RUNVM(256).Serialize())
		stackValues := []any{int64(0), child.MustBeginParse(), tc.count}
		if dynamicMode {
			parentCode = codeFromBuilders(t, execop.RUNVMX().Serialize())
			stackValues = append(stackValues, int64(256))
		}

		_, res, err := runRawCodeWithEnv(
			t,
			parentCode,
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			version,
			stackValues...,
		)

		opName := runVMOpcodeName(dynamicMode)
		exit := exitCodeFromResult(res, err)
		if version < 4 {
			if exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s %s v%d exit = %d, want invalid opcode", opName, tc.name, version, exit)
			}
			return
		}
		if exit != tc.want {
			t.Fatalf("%s %s v%d exit = %d, want %d", opName, tc.name, version, exit, tc.want)
		}
		assertRunVMTopLevelExceptionArg(t, opName+" "+tc.name, res.Stack)
	})
}

type runVMStackSizeValidationCase struct {
	name string
	size any
	want int64
}

func runVMStackSizeValidationCases(rawSeed int64) []runVMStackSizeValidationCase {
	offset := rawSeed % 1024
	if offset < 0 {
		offset = -offset
	}

	return []runVMStackSizeValidationCase{
		{
			name: "negative",
			size: int64(-1 - offset),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "too_large",
			size: int64(1 + offset),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "huge_int",
			size: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(offset)),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "nan",
			size: vmcore.NaN{},
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "cell",
			size: cell.BeginCell().EndCell(),
			want: vmerr.CodeTypeCheck,
		},
	}
}

func FuzzRunVMStackSizeValidationReturnsExceptionArg(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < 5; kind++ {
			f.Add(version, kind, false, version*71+int64(kind))
			f.Add(version, kind, true, version*73+int64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind byte, dynamicMode bool, rawSeed int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		tc := runVMStackSizeValidationCases(rawSeed)[int(rawKind)%5]
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(rawSeed%1000)).Serialize())

		parentCode := codeFromBuilders(t, execop.RUNVM(0).Serialize())
		stackValues := []any{tc.size, child.MustBeginParse()}
		if dynamicMode {
			parentCode = codeFromBuilders(t, execop.RUNVMX().Serialize())
			stackValues = append(stackValues, int64(0))
		}

		_, res, err := runRawCodeWithEnv(
			t,
			parentCode,
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			version,
			stackValues...,
		)

		opName := runVMOpcodeName(dynamicMode)
		exit := exitCodeFromResult(res, err)
		if version < 4 {
			if exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s %s v%d exit = %d, want invalid opcode", opName, tc.name, version, exit)
			}
			return
		}
		if exit != tc.want {
			t.Fatalf("%s %s v%d exit = %d, want %d", opName, tc.name, version, exit, tc.want)
		}
		assertRunVMTopLevelExceptionArg(t, opName+" "+tc.name, res.Stack)
	})
}

type runVMGasOperandValidationCase struct {
	name  string
	value any
	want  int64
}

func runVMGasOperandValidationCases(rawSeed int64) []runVMGasOperandValidationCase {
	offset := rawSeed % 1024
	if offset < 0 {
		offset = -offset
	}

	return []runVMGasOperandValidationCase{
		{
			name:  "negative",
			value: int64(-1 - offset),
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "too_large",
			value: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(offset)),
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "huge_int",
			value: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(offset)),
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "nan",
			value: vmcore.NaN{},
			want:  vmerr.CodeRangeCheck,
		},
		{
			name:  "cell",
			value: cell.BeginCell().EndCell(),
			want:  vmerr.CodeTypeCheck,
		},
	}
}

func runVMGasValidationStackValues(t *testing.T, target byte, invalid any) (int, []any, string) {
	t.Helper()

	child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
	switch target % 3 {
	case 0:
		return 8, []any{int64(0), child.MustBeginParse(), invalid}, "gas_limit"
	case 1:
		return 64, []any{int64(0), child.MustBeginParse(), invalid}, "gas_max"
	default:
		return 8 | 64, []any{int64(0), child.MustBeginParse(), invalid, int64(10_000)}, "gas_limit_with_max"
	}
}

func FuzzRunVMGasOperandValidationReturnsExceptionArg(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < 5; kind++ {
			for target := byte(0); target < 3; target++ {
				f.Add(version, kind, target, false, version*79+int64(kind)*3+int64(target))
				f.Add(version, kind, target, true, version*83+int64(kind)*3+int64(target))
			}
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind, rawTarget byte, dynamicMode bool, rawSeed int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		tc := runVMGasOperandValidationCases(rawSeed)[int(rawKind)%5]

		mode, stackValues, target := runVMGasValidationStackValues(t, rawTarget, tc.value)
		parentCode := codeFromBuilders(t, execop.RUNVM(mode).Serialize())
		if dynamicMode {
			parentCode = codeFromBuilders(t, execop.RUNVMX().Serialize())
			stackValues = append(stackValues, int64(mode))
		}

		_, res, err := runRawCodeWithEnv(
			t,
			parentCode,
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			version,
			stackValues...,
		)

		opName := runVMOpcodeName(dynamicMode)
		exit := exitCodeFromResult(res, err)
		if version < 4 {
			if exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s %s %s v%d exit = %d, want invalid opcode", opName, target, tc.name, version, exit)
			}
			return
		}
		if exit != tc.want {
			t.Fatalf("%s %s %s v%d exit = %d, want %d", opName, target, tc.name, version, exit, tc.want)
		}
		assertRunVMTopLevelExceptionArg(t, opName+" "+target+" "+tc.name, res.Stack)
	})
}

type runVMXInvalidModeCase struct {
	name string
	mode any
	want int64
}

func runVMXInvalidModeCases(rawSeed int64) []runVMXInvalidModeCase {
	offset := rawSeed % 4096
	if offset < 0 {
		offset = -offset
	}

	return []runVMXInvalidModeCase{
		{
			name: "negative",
			mode: int64(-1 - offset),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "invalid_flags",
			mode: int64(512 + offset%3584),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "too_large",
			mode: int64(4096 + offset),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "huge_int",
			mode: new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(offset)),
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "nan",
			mode: vmcore.NaN{},
			want: vmerr.CodeRangeCheck,
		},
		{
			name: "cell",
			mode: cell.BeginCell().EndCell(),
			want: vmerr.CodeTypeCheck,
		},
	}
}

func FuzzRunVMXInvalidModeReturnsExceptionArg(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := byte(0); kind < 6; kind++ {
			f.Add(version, kind, version*43+int64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind byte, rawSeed int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		tc := runVMXInvalidModeCases(rawSeed)[int(rawKind)%6]

		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(rawSeed%1000)).Serialize())
		_, res, err := runRawCodeWithEnv(
			t,
			codeFromBuilders(t, execop.RUNVMX().Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			version,
			int64(0),
			child.MustBeginParse(),
			tc.mode,
		)

		exit := exitCodeFromResult(res, err)
		if version < 4 {
			if exit != vmerr.CodeInvalidOpcode {
				t.Fatalf("%s v%d exit = %d, want invalid opcode", tc.name, version, exit)
			}
			return
		}
		if exit != tc.want {
			t.Fatalf("%s v%d exit = %d, want %d", tc.name, version, exit, tc.want)
		}
		assertRunVMTopLevelExceptionArg(t, tc.name, res.Stack)
	})
}

func assertRunVMTopLevelExceptionArg(t *testing.T, label string, stack *vmcore.Stack) {
	t.Helper()

	if stack.Len() != 1 {
		t.Fatalf("%s stack len = %d, want only exception argument", label, stack.Len())
	}
	if got := popInt64(t, stack); got != 0 {
		t.Fatalf("%s exception argument = %d, want 0", label, got)
	}
}

func FuzzRunVMChildGlobalVersionInheritance(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		f.Add(version)
	}

	f.Fuzz(func(t *testing.T, rawVersion int64) {
		version := tvmFuzzGlobalVersion(rawVersion)

		child := codeFromBuilders(t, funcsop.INMSGPARAMS().Serialize())
		childC7 := makeRunvmInMsgParamsChildC7(t)
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(16).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			version,
			int64(0),
			child.MustBeginParse(),
			childC7,
		)
		if version < 4 {
			if code := exitCodeFromResult(res, err); code != vmerr.CodeInvalidOpcode {
				t.Fatalf("v%d parent exit = %d, want invalid opcode", version, code)
			}
			return
		}
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("v%d parent exit = %d, want success", version, code)
		}

		stack = res.Stack
		childExit := popInt64(t, stack)
		if version < 11 {
			if childExit != vmerr.CodeInvalidOpcode {
				t.Fatalf("v%d child exit = %d, want invalid opcode", version, childExit)
			}
			return
		}
		if childExit != 0 {
			t.Fatalf("v%d child exit = %d, want success", version, childExit)
		}
		params, err := stack.PopTuple()
		if err != nil {
			t.Fatalf("v%d pop in_msg_params tuple: %v", version, err)
		}
		if params.Len() != 2 {
			t.Fatalf("v%d in_msg_params tuple len = %d, want 2", version, params.Len())
		}
	})
}

type runvmVersionedChildCase struct {
	name       string
	mode       int
	child      *cell.Cell
	c7         tuple.Tuple
	minVersion int
}

const runvmVersionedChildCaseCount = 10

func runvmVersionedChildCases(t *testing.T) []runvmVersionedChildCase {
	t.Helper()

	emptyBody := codeFromBuilders(t)
	cellValue := cell.BeginCell().MustStoreUInt(0xCA, 8).EndCell()
	depthCell := cell.BeginCell().MustStoreRef(cellValue).EndCell()
	feeC7 := feeTestC7(t)
	extraBalanceC7 := makeRunvmExtraBalanceChildC7(t)
	inMsgParamsC7 := makeRunvmInMsgParamsChildC7(t)

	return []runvmVersionedChildCase{
		{
			name: "gasconsumed",
			mode: 0,
			child: codeFromBuilders(t,
				funcsop.GASCONSUMED().Serialize(),
			),
			minVersion: 4,
		},
		{
			name: "getprecompiledgas",
			mode: 16,
			child: codeFromBuilders(t,
				funcsop.GETPRECOMPILEDGAS().Serialize(),
			),
			c7:         feeC7,
			minVersion: 6,
		},
		{
			name: "cdepthi",
			mode: 0,
			child: codeFromBuilders(t,
				stackop.PUSHREF(depthCell).Serialize(),
				cellsliceop.CDEPTHI(0).Serialize(),
			),
			minVersion: 6,
		},
		{
			name: "setcontctrmany",
			mode: 0,
			child: codeFromBuilders(t,
				stackop.PUSHREF(cellValue).Serialize(),
				execop.POPCTR(4).Serialize(),
				stackop.PUSHCONT(emptyBody).Serialize(),
				execop.SETCONTCTRMANY(1<<4).Serialize(),
				stackop.DROP().Serialize(),
			),
			minVersion: 9,
		},
		{
			name: "prevmcblocks_100",
			mode: 16,
			child: codeFromBuilders(t,
				funcsop.PREVMCBLOCKS_100().Serialize(),
			),
			c7:         feeC7,
			minVersion: 9,
		},
		{
			name: "getextrabalance",
			mode: 16,
			child: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(7)).Serialize(),
				funcsop.GETEXTRABALANCE().Serialize(),
			),
			c7:         extraBalanceC7,
			minVersion: 10,
		},
		{
			name: "getparamlong",
			mode: 16,
			child: codeFromBuilders(t,
				funcsop.GETPARAMLONG(14).Serialize(),
			),
			c7:         feeC7,
			minVersion: 11,
		},
		{
			name: "btos",
			mode: 0,
			child: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(0xAB)).Serialize(),
				cellsliceop.NEWC().Serialize(),
				cellsliceop.STU(8).Serialize(),
				cellsliceop.BTOS().Serialize(),
			),
			minVersion: 12,
		},
		{
			name: "lshiftadddivmod",
			mode: 0,
			child: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(2)).Serialize(),
				stackop.PUSHINT(big.NewInt(3)).Serialize(),
				stackop.PUSHINT(big.NewInt(4)).Serialize(),
				stackop.PUSHINT(big.NewInt(1)).Serialize(),
				mathop.LSHIFTADDDIVMOD().Serialize(),
			),
			minVersion: 4,
		},
		{
			name: "inmsgparams",
			mode: 16,
			child: codeFromBuilders(t,
				funcsop.INMSGPARAMS().Serialize(),
			),
			c7:         inMsgParamsC7,
			minVersion: 11,
		},
	}
}

func FuzzRunVMVersionedChildOpcodeMatrix(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < runvmVersionedChildCaseCount; kind++ {
			f.Add(version, kind)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8) {
		runRunVMVersionedChildOpcodeMatrixCase(t, rawVersion, rawKind, false)
	})
}

func FuzzRunVMXVersionedChildOpcodeMatrix(f *testing.F) {
	for version := int64(0); version <= int64(vmcore.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < runvmVersionedChildCaseCount; kind++ {
			f.Add(version, kind)
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8) {
		runRunVMVersionedChildOpcodeMatrixCase(t, rawVersion, rawKind, true)
	})
}

func runRunVMVersionedChildOpcodeMatrixCase(t *testing.T, rawVersion int64, rawKind uint8, dynamicMode bool) {
	t.Helper()

	version := tvmFuzzGlobalVersion(rawVersion)
	tests := runvmVersionedChildCases(t)
	if len(tests) != runvmVersionedChildCaseCount {
		t.Fatalf("runvm versioned child case count = %d, want %d", len(tests), runvmVersionedChildCaseCount)
	}
	tc := tests[int(rawKind)%len(tests)]

	stackValues := []any{int64(0), tc.child.MustBeginParse()}
	if tc.mode&16 != 0 {
		stackValues = append(stackValues, tc.c7)
	}
	parentCode := codeFromBuilders(t, execop.RUNVM(tc.mode).Serialize())
	if dynamicMode {
		stackValues = append(stackValues, int64(tc.mode))
		parentCode = codeFromBuilders(t, execop.RUNVMX().Serialize())
	}

	_, res, err := runRawCodeWithEnv(
		t,
		parentCode,
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		version,
		stackValues...,
	)
	if version < 4 {
		if code := exitCodeFromResult(res, err); code != vmerr.CodeInvalidOpcode {
			t.Fatalf("%s %s v%d parent exit = %d, want invalid opcode", runVMOpcodeName(dynamicMode), tc.name, version, code)
		}
		return
	}
	if code := exitCodeFromResult(res, err); code != 0 {
		t.Fatalf("%s %s v%d parent exit = %d, want success", runVMOpcodeName(dynamicMode), tc.name, version, code)
	}

	childExit := popInt64(t, res.Stack)
	if version < tc.minVersion {
		if childExit != vmerr.CodeInvalidOpcode {
			t.Fatalf("%s %s v%d child exit = %d, want invalid opcode", runVMOpcodeName(dynamicMode), tc.name, version, childExit)
		}
		return
	}
	if childExit != 0 {
		t.Fatalf("%s %s v%d child exit = %d, want success", runVMOpcodeName(dynamicMode), tc.name, version, childExit)
	}

	switch tc.name {
	case "gasconsumed":
		if res.Stack.Len() != 1 {
			t.Fatalf("%s %s v%d stack len = %d, want 1", runVMOpcodeName(dynamicMode), tc.name, version, res.Stack.Len())
		}
	case "getprecompiledgas":
		if got := popInt64(t, res.Stack); got != 555 {
			t.Fatalf("%s %s v%d result = %d, want 555", runVMOpcodeName(dynamicMode), tc.name, version, got)
		}
	case "setcontctrmany":
		if res.Stack.Len() != 0 {
			t.Fatalf("%s %s v%d stack len = %d, want 0", runVMOpcodeName(dynamicMode), tc.name, version, res.Stack.Len())
		}
	case "prevmcblocks_100":
		if got := popInt64(t, res.Stack); got != 333 {
			t.Fatalf("%s %s v%d result = %d, want 333", runVMOpcodeName(dynamicMode), tc.name, version, got)
		}
	case "getextrabalance":
		if got := popInt64(t, res.Stack); got != 12345 {
			t.Fatalf("%s %s v%d result = %d, want 12345", runVMOpcodeName(dynamicMode), tc.name, version, got)
		}
	case "inmsgparams":
		params, err := res.Stack.PopTuple()
		if err != nil {
			t.Fatalf("%s %s v%d pop in_msg_params tuple: %v", runVMOpcodeName(dynamicMode), tc.name, version, err)
		}
		if params.Len() != 2 {
			t.Fatalf("%s %s v%d in_msg_params tuple len = %d, want 2", runVMOpcodeName(dynamicMode), tc.name, version, params.Len())
		}
	case "cdepthi":
		if got := popInt64(t, res.Stack); got != 1 {
			t.Fatalf("%s %s v%d result = %d, want 1", runVMOpcodeName(dynamicMode), tc.name, version, got)
		}
	case "getparamlong", "btos", "lshiftadddivmod":
		if res.Stack.Len() == 0 {
			t.Fatalf("%s %s v%d stack is empty after child success", runVMOpcodeName(dynamicMode), tc.name, version)
		}
	}
}

func runVMOpcodeName(dynamicMode bool) string {
	if dynamicMode {
		return "RUNVMX"
	}
	return "RUNVM"
}

func makeRunvmInMsgParamsChildC7(t *testing.T) tuple.Tuple {
	t.Helper()

	params := tuple.NewTupleSized(18)
	if err := params.Set(17, tuple.NewTupleValue(big.NewInt(1), big.NewInt(2))); err != nil {
		t.Fatalf("set in_msg_params: %v", err)
	}
	return tuple.NewTupleValue(params)
}

func makeRunvmExtraBalanceChildC7(t *testing.T) tuple.Tuple {
	t.Helper()

	extra := cell.NewDict(32)
	if _, err := extra.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreVarUInt(12345, 32),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("seed extra balance dict: %v", err)
	}

	return makeTonopsTestC7(t, tonopsTestC7Config{
		Balance: tuple.NewTupleValue(big.NewInt(10_000_000), extra.AsCell()),
	})
}
