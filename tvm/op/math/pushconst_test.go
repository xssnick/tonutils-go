package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPushConstOpcodes(t *testing.T) {
	tests := []struct {
		name string
		op   vm.OP
		want *big.Int
		nan  bool
	}{
		{
			name: "PUSHPOW2",
			op:   PUSHPOW2(4),
			want: new(big.Int).Lsh(big.NewInt(1), 5),
		},
		{
			name: "PUSHPOW2DEC",
			op:   PUSHPOW2DEC(4),
			want: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 5), big.NewInt(1)),
		},
		{
			name: "PUSHNEGPOW2",
			op:   PUSHNEGPOW2(4),
			want: new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 5)),
		},
		{
			name: "PUSHNAN",
			op:   PUSHNAN(),
			nan:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := vm.NewStack()
			if err := tt.op.Interpret(&vm.State{Stack: stack}); err != nil {
				t.Fatalf("interpret failed: %v", err)
			}

			if tt.nan {
				val, popErr := stack.PopInt()
				if popErr != nil {
					t.Fatalf("failed to pop NaN: %v", popErr)
				}
				if val != nil {
					t.Fatalf("expected NaN stack value, got %s", val.String())
				}
				return
			}

			got, popErr := stack.PopIntFinite()
			if popErr != nil {
				t.Fatalf("failed to pop result: %v", popErr)
			}
			if got.Cmp(tt.want) != 0 {
				t.Fatalf("unexpected result: want=%s got=%s", tt.want.String(), got.String())
			}
		})
	}
}
