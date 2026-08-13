package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func executionResultThrowCode(code uint64) *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0xF200|code, 16)
}

func TestExecutionResultUsesOnlyCommittedRegisterChanges(t *testing.T) {
	originalData := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	firstData := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()
	secondData := cell.BeginCell().MustStoreUInt(0xC3, 8).EndCell()
	firstActions := cell.BeginCell().MustStoreUInt(0xD4, 8).EndCell()
	secondActions := cell.BeginCell().MustStoreUInt(0xE5, 8).EndCell()
	emptyActions := cell.BeginCell().EndCell()

	run := func(t *testing.T, code *cell.Cell) *ExecutionResult {
		t.Helper()

		res, err := NewTVM().Execute(
			code,
			originalData,
			tuple.Tuple{},
			vm.GasWithLimit(1_000_000),
			vm.NewStack(),
			testExecutionConfigWithVersion(t, vm.MaxSupportedGlobalVersion),
		)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if res.ExitCode != 5 {
			t.Fatalf("exit code = %d, want 5", res.ExitCode)
		}
		return res
	}

	t.Run("uncommitted mutations keep inputs", func(t *testing.T) {
		res := run(t, codeFromBuilders(t,
			stackop.PUSHREF(firstData).Serialize(),
			execop.POPCTR(4).Serialize(),
			stackop.PUSHREF(firstActions).Serialize(),
			execop.POPCTR(5).Serialize(),
			executionResultThrowCode(5),
		))

		if res.Committed {
			t.Fatal("unhandled throw without COMMIT reported committed state")
		}
		if res.Data == nil || res.Data.HashKey() != originalData.HashKey() {
			t.Fatalf("uncommitted data = %v, want original %x", res.Data, originalData.Hash())
		}
		if res.Actions == nil || res.Actions.HashKey() != emptyActions.HashKey() {
			t.Fatalf("uncommitted actions = %v, want initial empty cell", res.Actions)
		}
	})

	t.Run("later mutations keep committed snapshot", func(t *testing.T) {
		res := run(t, codeFromBuilders(t,
			stackop.PUSHREF(firstData).Serialize(),
			execop.POPCTR(4).Serialize(),
			stackop.PUSHREF(firstActions).Serialize(),
			execop.POPCTR(5).Serialize(),
			funcsop.COMMIT().Serialize(),
			stackop.PUSHREF(secondData).Serialize(),
			execop.POPCTR(4).Serialize(),
			stackop.PUSHREF(secondActions).Serialize(),
			execop.POPCTR(5).Serialize(),
			executionResultThrowCode(5),
		))

		if !res.Committed {
			t.Fatal("explicit COMMIT snapshot was lost after throw")
		}
		if res.Data == nil || res.Data.HashKey() != firstData.HashKey() {
			t.Fatalf("committed data = %v, want first snapshot %x", res.Data, firstData.Hash())
		}
		if res.Actions == nil || res.Actions.HashKey() != firstActions.HashKey() {
			t.Fatalf("committed actions = %v, want first snapshot %x", res.Actions, firstActions.Hash())
		}
	})
}

func TestMessageFailureStillMasksUncommittedActions(t *testing.T) {
	originalData := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	mutatedData := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()
	mutatedActions := cell.BeginCell().MustStoreUInt(0xC3, 8).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xD4, 8).EndCell()
	code := codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(mutatedData).Serialize(),
		execop.POPCTR(4).Serialize(),
		stackop.PUSHREF(mutatedActions).Serialize(),
		execop.POPCTR(5).Serialize(),
		executionResultThrowCode(5),
	)

	res, err := emulateInternalForTest(t, code, originalData, body)
	if err != nil {
		t.Fatalf("emulate internal message: %v", err)
	}
	if res.ExitCode != 5 || res.Committed {
		t.Fatalf("message exit/committed = %d/%t, want 5/false", res.ExitCode, res.Committed)
	}
	if res.Data == nil || res.Data.HashKey() != originalData.HashKey() {
		t.Fatalf("message data = %v, want original %x", res.Data, originalData.Hash())
	}
	if res.Actions != nil {
		t.Fatalf("message actions = %s, want nil", res.Actions.Dump())
	}
}
