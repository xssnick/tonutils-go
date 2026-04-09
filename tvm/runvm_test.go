package tvm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
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

func TestRunVMGoSemantics(t *testing.T) {
	t.Run("RunvmReturnsChildStackAndExitCode", func(t *testing.T) {
		child := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(0).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			dropCode.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			dropCode.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			makeRunvmSameC3CallDictChild(t, 5, 9).BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			makeRunvmSameC3JmpDictChild(t, 8).BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			makeRunvmSameC3PrepareDictChild(t, 9, 11).BeginParse(),
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
		childC7 := *tuple.NewTuple(*tuple.NewTuple(int64(11), int64(22)))
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(16).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.DefaultGlobalVersion,
			int64(0),
			codeFromBuilders(t, funcsop.GETPARAM(1).Serialize()).BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			codeFromBuilders(t).BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
			vmcore.DefaultGlobalVersion,
			int64(0),
			child.BeginParse(),
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
				stackop.PUSHSLICEINLINE(child.BeginParse()).Serialize(),
				execop.RUNVM(mode).Serialize(),
			)
		}

		_, baseRes, err := runRawCodeWithEnv(t, makeCode(0), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("non-isolated unexpected error: %v", err)
		}
		_, isoRes, err := runRawCodeWithEnv(t, makeCode(128), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("isolated unexpected error: %v", err)
		}
		if isoRes.GasUsed <= baseRes.GasUsed {
			t.Fatalf("expected isolated run to use more gas, got base=%d isolated=%d", baseRes.GasUsed, isoRes.GasUsed)
		}
	})

	t.Run("RunvmVersion11ReturnsNullDataAndActionsOnFailure", func(t *testing.T) {
		initialData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, execop.RUNVM(4|32).Serialize()),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			11,
			int64(0),
			codeFromOpcodes(t, 0xF205).BeginParse(),
			initialData,
		)
		if code := exitCodeFromResult(res, err); code != 0 {
			t.Fatalf("expected parent exit 0, got %d", code)
		}

		stack = res.Stack
		actions := popMaybeCell(t, stack)
		data := popMaybeCell(t, stack)
		if got := popInt64(t, stack); got != 5 {
			t.Fatalf("expected child exit code 5, got %d", got)
		}
		if got := popInt64(t, stack); got != 0 {
			t.Fatalf("expected child exception argument 0, got %d", got)
		}
		if data != nil {
			t.Fatalf("expected nil data on failed child VM for v11+")
		}
		if actions != nil {
			t.Fatalf("expected nil actions on failed child VM for v11+")
		}
	})
}
