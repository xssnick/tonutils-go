package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestStoreVarIntNaNMatchesCppRangeCheck(t *testing.T) {
	tests := []struct {
		name string
		op   vm.OP
	}{
		{name: "STVARINT16", op: STVARINT16()},
		{name: "STVARUINT32", op: STVARUINT32()},
		{name: "STVARINT32", op: STVARINT32()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
				t.Fatalf("PushBuilder failed: %v", err)
			}
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("PushAny(NaN) failed: %v", err)
			}

			err := tt.op.Interpret(st)
			if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
				t.Fatalf("exit code = (%d, %t), want range check: %v", code, ok, err)
			}
			if got := st.Stack.Len(); got != 0 {
				t.Fatalf("stack depth = %d, want both operands consumed", got)
			}
		})
	}
}

func TestStoreOptStdAddrQNonSliceVersionSemantics(t *testing.T) {
	for _, version := range []int{12, 13, 14} {
		t.Run("v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			st := newFuncTestState(t, nil)
			st.GlobalVersion = version
			if err := st.Stack.PushSmallInt(7); err != nil {
				t.Fatalf("PushSmallInt failed: %v", err)
			}
			if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
				t.Fatalf("PushBuilder failed: %v", err)
			}

			if err := STOPTSTDADDRQ().Interpret(st); err != nil {
				t.Fatalf("STOPTSTDADDRQ failed: %v", err)
			}
			failed, err := st.Stack.PopBool()
			if err != nil || !failed {
				t.Fatalf("failure flag = (%t, %v), want true", failed, err)
			}
			builder, err := st.Stack.PopBuilder()
			if err != nil {
				t.Fatalf("PopBuilder failed: %v", err)
			}
			if builder.BitsUsed() != 0 || builder.RefsUsed() != 0 {
				t.Fatalf("builder changed: bits=%d refs=%d", builder.BitsUsed(), builder.RefsUsed())
			}

			raw, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("PopAny failed: %v", err)
			}
			if version < 14 {
				legacy, ok := raw.(*cell.Slice)
				if !ok || legacy != nil {
					t.Fatalf("restored value = %T %v, want legacy null cell slice", raw, raw)
				}
				return
			}

			got, ok := raw.(*big.Int)
			if !ok || got.Int64() != 7 {
				t.Fatalf("restored value = %T %v, want integer 7", raw, raw)
			}
		})
	}
}
