package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestFeeOpcodesPreserveShortConfigRangeCheck(t *testing.T) {
	tests := []struct {
		name  string
		op    vm.OP
		stack []*big.Int
	}{
		{name: "GETGASFEE", op: GETGASFEE(), stack: []*big.Int{big.NewInt(1), big.NewInt(0)}},
		{name: "GETSTORAGEFEE", op: GETSTORAGEFEE(), stack: []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(0)}},
		{name: "GETFORWARDFEE", op: GETFORWARDFEE(), stack: []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(0)}},
		{name: "GETORIGINALFWDFEE", op: GETORIGINALFWDFEE(), stack: []*big.Int{big.NewInt(1), big.NewInt(0)}},
		{name: "GETGASFEESIMPLE", op: GETGASFEESIMPLE(), stack: []*big.Int{big.NewInt(1), big.NewInt(0)}},
		{name: "GETFORWARDFEESIMPLE", op: GETFORWARDFEESIMPLE(), stack: []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(0)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newFuncTestState(t, map[int]any{
				paramIdxUnpackedConfig: tuple.NewTupleValue(),
			})
			for _, value := range tt.stack {
				if err := state.Stack.PushInt(value); err != nil {
					t.Fatalf("failed to push operand: %v", err)
				}
			}

			assertFuncParityErrorCode(t, tt.op.Interpret(state), vmerr.CodeRangeCheck)
		})
	}
}

func TestGETEXTRABALANCEShortBalanceIsRangeCheck(t *testing.T) {
	state := newFuncTestState(t, map[int]any{
		7: tuple.NewTupleValue(big.NewInt(1000)),
	})
	if err := state.Stack.PushSmallInt(1); err != nil {
		t.Fatalf("failed to push currency id: %v", err)
	}

	assertFuncParityErrorCode(t, GETEXTRABALANCE().Interpret(state), vmerr.CodeRangeCheck)
}

func TestPRNGNaNSeedIsRangeCheck(t *testing.T) {
	tests := []struct {
		name    string
		op      vm.OP
		operand bool
	}{
		{name: "RANDU256", op: RANDU256()},
		{name: "RAND", op: RAND(), operand: true},
		{name: "ADDRAND", op: ADDRAND(), operand: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newFuncTestState(t, map[int]any{paramIdxRandomSeed: vm.NaN{}})
			if tt.operand {
				if err := state.Stack.PushSmallInt(1); err != nil {
					t.Fatalf("failed to push operand: %v", err)
				}
			}

			assertFuncParityErrorCode(t, tt.op.Interpret(state), vmerr.CodeRangeCheck)
		})
	}
}

func TestPrevBlockAliasesRejectOversizedTuple(t *testing.T) {
	prev := tuple.NewTupleSized(256)
	tests := []struct {
		name string
		op   vm.OP
	}{
		{name: "PREVMCBLOCKS", op: PREVMCBLOCKS()},
		{name: "PREVKEYBLOCK", op: PREVKEYBLOCK()},
		{name: "PREVMCBLOCKS_100", op: PREVMCBLOCKS_100()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newFuncTestState(t, map[int]any{paramIdxPrevBlocksInfo: prev})
			assertFuncParityErrorCode(t, tt.op.Interpret(state), vmerr.CodeTypeCheck)
		})
	}
}

func assertFuncParityErrorCode(t *testing.T, err error, want int64) {
	t.Helper()

	code, ok := vmerr.ErrorCode(err)
	if !ok || code != want {
		t.Fatalf("error = %v, want code %d", err, want)
	}
}
