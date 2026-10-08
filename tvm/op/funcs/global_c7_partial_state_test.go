package funcs

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSetGlobOutOfGasLeavesEmptyC7(t *testing.T) {
	tests := []struct {
		name string
		op   vm.OP
		push func(*testing.T, *vm.State)
	}{
		{
			name: "fixed",
			op:   SETGLOB(1),
			push: func(t *testing.T, state *vm.State) {
				t.Helper()
				if err := state.Stack.PushSmallInt(7); err != nil {
					t.Fatalf("failed to push value: %v", err)
				}
			},
		},
		{
			name: "variable",
			op:   SETGLOBVAR(),
			push: func(t *testing.T, state *vm.State) {
				t.Helper()
				if err := state.Stack.PushSmallInt(7); err != nil {
					t.Fatalf("failed to push value: %v", err)
				}
				if err := state.Stack.PushSmallInt(1); err != nil {
					t.Fatalf("failed to push index: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c7 := tuple.NewTupleValue(tuple.NewTupleValue())
			state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(1), nil, c7, vm.NewStack())
			state.InitForExecution()
			tt.push(t, state)

			err := tt.op.Interpret(state)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeOutOfGas {
				t.Fatalf("error = %v, want out of gas", err)
			}
			if state.Reg.C7.IsNull() || state.Reg.C7.Len() != 0 {
				t.Fatalf("c7 after failed replacement = null:%v len:%d, want non-null empty tuple", state.Reg.C7.IsNull(), state.Reg.C7.Len())
			}
			if state.Stack.Len() != 0 {
				t.Fatalf("stack depth = %d, want consumed operands", state.Stack.Len())
			}
		})
	}
}

func BenchmarkSetGlobSuccess(b *testing.B) {
	tests := []struct {
		name string
		op   vm.OP
		push func(*vm.State) error
	}{
		{
			name: "fixed",
			op:   SETGLOB(1),
			push: func(state *vm.State) error {
				return state.Stack.PushSmallInt(7)
			},
		},
		{
			name: "variable",
			op:   SETGLOBVAR(),
			push: func(state *vm.State) error {
				if err := state.Stack.PushSmallInt(7); err != nil {
					return err
				}
				return state.Stack.PushSmallInt(1)
			},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(vm.GasInfinite), nil, tuple.NewTupleSized(2), vm.NewStack())
			state.InitForExecution()

			b.ReportAllocs()
			for b.Loop() {
				if err := tt.push(state); err != nil {
					b.Fatal(err)
				}
				if err := tt.op.Interpret(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
