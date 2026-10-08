package stack

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func newStack(values ...int64) *vm.Stack {
	st := vm.NewStack()
	for _, v := range values {
		st.PushAny(big.NewInt(v))
	}
	return st
}

func instructionBits(t *testing.T, op vm.OP) int64 {
	t.Helper()
	priced, ok := op.(vm.GasPricedOp)
	if !ok {
		t.Fatalf("opcode %T does not report an instruction length", op)
	}
	return priced.InstructionBits()
}

func popInts(t *testing.T, st *vm.Stack, count int) []int64 {
	t.Helper()
	res := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		v, err := st.PopInt()
		if err != nil {
			t.Fatalf("failed to pop int: %v", err)
		}
		res = append(res, v.Int64())
	}
	return res
}
