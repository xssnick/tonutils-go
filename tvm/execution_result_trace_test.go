package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func executeExactImplicitRetGas(t *testing.T, value any) *ExecutionResult {
	t.Helper()

	stack := vm.NewStack()
	if err := stack.PushHostValue(value); err != nil {
		t.Fatalf("push input value: %v", err)
	}

	res, err := NewTVM().Execute(
		cell.BeginCell().EndCell(),
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		vm.GasWithLimit(vm.ImplicitRetGasPrice),
		stack,
		testExecutionConfigWithVersion(t, uint32(vm.MaxSupportedGlobalVersion)),
	)
	if err != nil {
		t.Fatalf("execute empty code: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if res.GasUsed != vm.ImplicitRetGasPrice || res.Gas.Remaining != 0 {
		t.Fatalf("gas used/remaining = %d/%d, want %d/0", res.GasUsed, res.Gas.Remaining, vm.ImplicitRetGasPrice)
	}
	return res
}

func TestTVMExecutionResultValuesAreGasFree(t *testing.T) {
	t.Run("slice serialization", func(t *testing.T) {
		value := cell.BeginCell().
			MustStoreUInt(0xAB, 8).
			MustStoreRef(cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()).
			EndCell().
			MustBeginParse()
		res := executeExactImplicitRetGas(t, value)

		got, err := res.Stack.PopSlice()
		if err != nil {
			t.Fatalf("pop result slice: %v", err)
		}
		if _, err = got.ToCell(); err != nil {
			t.Fatalf("serialize result slice after execution: %v", err)
		}
	})

	t.Run("builder serialization", func(t *testing.T) {
		res := executeExactImplicitRetGas(t, cell.BeginCell().MustStoreUInt(0xAB, 8))

		got, err := res.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("pop result builder: %v", err)
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("serialize result builder after execution: %v", recovered)
				}
			}()
			got.EndCell()
		}()
	})
}
