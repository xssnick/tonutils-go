package main

import (
	"math/big"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func mustValidRunMethodParams(t *testing.T) RunMethodParams {
	t.Helper()

	stack := vm.NewStack()
	if err := stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("seed vm stack: %v", err)
	}
	tlbStack, err := tlb.NewStackFromVM(stack)
	if err != nil {
		t.Fatalf("NewStackFromVM: %v", err)
	}
	stackCell, err := tlbStack.ToCell()
	if err != nil {
		t.Fatalf("stack ToCell: %v", err)
	}

	c7tuple, err := PrepareC7(
		address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO"),
		time.Unix(123, 0),
		make([]byte, 32),
		big.NewInt(10000000),
		nil,
		MainContractCode,
	)
	if err != nil {
		t.Fatalf("PrepareC7: %v", err)
	}

	c7stack := tlb.NewStack()
	c7stack.Push(c7tuple)
	c7cell, err := c7stack.ToCell()
	if err != nil {
		t.Fatalf("c7 stack ToCell: %v", err)
	}

	return RunMethodParams{
		Code:  MainContractCode,
		Data:  cell.BeginCell().EndCell(),
		Stack: stackCell,
		Params: MethodConfig{
			C7:   c7cell,
			Libs: cell.BeginCell().EndCell(),
		},
		MethodID: int32(tlb.MethodNameHash("simpleRepeatWhile")),
	}
}

func TestRunGetMethodRejectsMalformedButSerializableInputs(t *testing.T) {
	tests := []struct {
		name      string
		maxGas    int64
		mutate    func(*RunMethodParams)
		wantErr   bool
		wantExit  *int32
	}{
		{
			name:   "EmptyCodeCell",
			maxGas: 1000000000,
			mutate: func(params *RunMethodParams) {
				params.Code = cell.BeginCell().EndCell()
			},
			wantExit: func() *int32 { v := int32(0); return &v }(),
		},
		{
			name:   "MalformedStackCell",
			maxGas: 1000000000,
			mutate: func(params *RunMethodParams) {
				params.Stack = cell.BeginCell().EndCell()
			},
			wantErr: true,
		},
		{
			name:   "MalformedC7Cell",
			maxGas: 1000000000,
			mutate: func(params *RunMethodParams) {
				params.Params.C7 = cell.BeginCell().EndCell()
			},
			wantErr: true,
		},
		{
			name:     "NegativeGas",
			maxGas:   -1,
			mutate:   func(params *RunMethodParams) {},
			wantExit: func() *int32 { v := int32(-14); return &v }(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := mustValidRunMethodParams(t)
			tc.mutate(&params)

			res, err := RunGetMethod(params, tc.maxGas)
			if tc.wantErr {
				if err == nil {
					t.Fatal("RunGetMethod should reject malformed but serializable inputs")
				}
				return
			}
			if err != nil {
				t.Fatalf("RunGetMethod returned unexpected error: %v", err)
			}
			if res == nil {
				t.Fatal("RunGetMethod returned nil result")
			}
			if tc.wantExit != nil && res.ExitCode != *tc.wantExit {
				t.Fatalf("unexpected exit code: got %d want %d", res.ExitCode, *tc.wantExit)
			}
		})
	}
}
