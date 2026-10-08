package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExecuteClassifiesNullCodeOrStackAsFatal(t *testing.T) {
	validCode := cell.BeginCell().EndCell()

	for _, test := range []struct {
		name  string
		code  *cell.Cell
		stack *vmcore.Stack
	}{
		{name: "null_code", stack: vmcore.NewStack()},
		{name: "null_stack", code: validCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			gas := vmcore.GasWithLimit(1_000)
			res, err := NewTVM().Execute(
				test.code,
				cell.BeginCell().EndCell(),
				tuple.Tuple{},
				gas,
				test.stack,
				testExecutionConfig(t),
			)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if res == nil {
				t.Fatal("fatal classification returned nil result")
			}
			if res.ExitCode != ^int64(vmerr.CodeFatal) {
				t.Fatalf("exit code = %d, want %d", res.ExitCode, ^int64(vmerr.CodeFatal))
			}
			if res.Gas.Remaining != gas.Remaining || res.GasUsed != 0 || res.Steps != 0 {
				t.Fatalf("fatal precheck consumed execution resources: %+v", res)
			}
			if res.Stack != test.stack {
				t.Fatal("fatal precheck did not preserve the input stack")
			}
		})
	}
}
