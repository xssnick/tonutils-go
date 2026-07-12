package exec

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestCONDSELCHKNaNHasIntegerStackType(t *testing.T) {
	tests := []struct {
		name string
		cond int64
		x    any
		y    any
		nan  bool
		want int64
	}{
		{name: "select NaN", cond: -1, x: vm.NaN{}, y: big.NewInt(5), nan: true},
		{name: "select finite", cond: 0, x: vm.NaN{}, y: big.NewInt(5), want: 5},
		{name: "reverse types", cond: 0, x: big.NewInt(5), y: vm.NaN{}, nan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushSmallInt(tt.cond); err != nil {
				t.Fatalf("failed to push condition: %v", err)
			}
			if err := state.Stack.PushAny(tt.x); err != nil {
				t.Fatalf("failed to push x: %v", err)
			}
			if err := state.Stack.PushAny(tt.y); err != nil {
				t.Fatalf("failed to push y: %v", err)
			}

			if err := CONDSELCHK().Interpret(state); err != nil {
				t.Fatalf("CONDSELCHK failed: %v", err)
			}
			got, err := state.Stack.PopAny()
			if err != nil {
				t.Fatalf("failed to pop result: %v", err)
			}
			if tt.nan {
				if _, ok := got.(vm.NaN); !ok {
					t.Fatalf("result = %T, want NaN", got)
				}
				return
			}
			value, ok := got.(*big.Int)
			if !ok || value.Int64() != tt.want {
				t.Fatalf("result = %T %v, want %d", got, got, tt.want)
			}
		})
	}
}
