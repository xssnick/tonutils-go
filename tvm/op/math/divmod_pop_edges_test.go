package math

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestCompoundDivModPopOrderEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		args []int64
	}{
		{name: "DIVMOD", op: DIVMOD(), args: []int64{9, 4}},
		{name: "DIVMODR", op: DIVMODR(), args: []int64{9, 4}},
		{name: "DIVMODC", op: DIVMODC(), args: []int64{9, 4}},
		{name: "ADDDIVMOD", op: ADDDIVMOD(), args: []int64{9, 4, 3}},
		{name: "ADDDIVMODR", op: ADDDIVMODR(), args: []int64{9, 4, 3}},
		{name: "ADDDIVMODC", op: ADDDIVMODC(), args: []int64{9, 4, 3}},
		{name: "MULDIVMOD", op: MULDIVMOD(), args: []int64{9, 4, 3}},
		{name: "MULDIVMODR", op: MULDIVMODR(), args: []int64{9, 4, 3}},
		{name: "MULDIVMODC", op: MULDIVMODC(), args: []int64{9, 4, 3}},
		{name: "MULADDDIVMOD", op: MULADDDIVMOD(), args: []int64{9, 4, 2, 3}},
		{name: "MULADDDIVMODR", op: MULADDDIVMODR(), args: []int64{9, 4, 2, 3}},
		{name: "MULADDDIVMODC", op: MULADDDIVMODC(), args: []int64{9, 4, 2, 3}},
		{name: "LSHIFTDIVMOD", op: LSHIFTDIVMOD(), args: []int64{9, 4, 3}},
		{name: "LSHIFTDIVMODR", op: LSHIFTDIVMODR(), args: []int64{9, 4, 3}},
		{name: "LSHIFTDIVMODC", op: LSHIFTDIVMODC(), args: []int64{9, 4, 3}},
		{name: "LSHIFTADDDIVMOD", op: LSHIFTADDDIVMOD(), args: []int64{9, 4, 2, 3}},
		{name: "LSHIFTADDDIVMODR", op: LSHIFTADDDIVMODR(), args: []int64{9, 4, 2, 3}},
		{name: "LSHIFTADDDIVMODC", op: LSHIFTADDDIVMODC(), args: []int64{9, 4, 2, 3}},
	} {
		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, tt.args[:len(tt.args)-1]...)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		for badSlot := range tt.args {
			t.Run(fmt.Sprintf("%s_type_slot_%d", tt.name, badSlot), func(t *testing.T) {
				st := newMathCoverageState()
				pushMathCoverageArgsWithBadSlot(t, st, tt.args, badSlot)
				assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
			})
		}
	}
}

func TestShiftModPopOrderEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		args []int64
	}{
		{name: "RSHIFTR", op: RSHIFTR(), args: []int64{9, 3}},
		{name: "RSHIFTC", op: RSHIFTC(), args: []int64{9, 3}},
		{name: "MODPOW2", op: MODPOW2(), args: []int64{9, 3}},
		{name: "MODPOW2R", op: MODPOW2R(), args: []int64{9, 3}},
		{name: "MODPOW2C", op: MODPOW2C(), args: []int64{9, 3}},
		{name: "RSHIFTMOD", op: RSHIFTMOD(), args: []int64{9, 3}},
		{name: "RSHIFTMODR", op: RSHIFTMODR(), args: []int64{9, 3}},
		{name: "RSHIFTMODC", op: RSHIFTMODC(), args: []int64{9, 3}},
		{name: "MULMODPOW2_VAR", op: MULMODPOW2_VAR(), args: []int64{9, 4, 3}},
		{name: "MULMODPOW2R_VAR", op: MULMODPOW2R_VAR(), args: []int64{9, 4, 3}},
		{name: "MULMODPOW2C_VAR", op: MULMODPOW2C_VAR(), args: []int64{9, 4, 3}},
		{name: "MULRSHIFTMOD_VAR", op: MULRSHIFTMOD_VAR(), args: []int64{9, 4, 3}},
		{name: "MULRSHIFTRMOD_VAR", op: MULRSHIFTRMOD_VAR(), args: []int64{9, 4, 3}},
		{name: "MULRSHIFTCMOD_VAR", op: MULRSHIFTCMOD_VAR(), args: []int64{9, 4, 3}},
		{name: "ADDRSHIFTMOD", op: ADDRSHIFTMOD(), args: []int64{9, 4, 3}},
		{name: "ADDRSHIFTMODR", op: ADDRSHIFTMODR(), args: []int64{9, 4, 3}},
		{name: "ADDRSHIFTMODC", op: ADDRSHIFTMODC(), args: []int64{9, 4, 3}},
		{name: "MULADDRSHIFTMOD", op: MULADDRSHIFTMOD(), args: []int64{9, 4, 2, 3}},
		{name: "MULADDRSHIFTRMOD", op: MULADDRSHIFTRMOD(), args: []int64{9, 4, 2, 3}},
		{name: "MULADDRSHIFTCMOD", op: MULADDRSHIFTCMOD(), args: []int64{9, 4, 2, 3}},
	} {
		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageInts(t, st, tt.args[:len(tt.args)-1]...)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		for badSlot := range tt.args {
			t.Run(fmt.Sprintf("%s_type_slot_%d", tt.name, badSlot), func(t *testing.T) {
				st := newMathCoverageState()
				pushMathCoverageArgsWithBadSlot(t, st, tt.args, badSlot)
				assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
			})
		}
	}
}

func pushMathCoverageArgsWithBadSlot(t *testing.T, st *vm.State, args []int64, badSlot int) {
	t.Helper()
	for i, arg := range args {
		if i == badSlot {
			pushMathCoverageNonInt(t, st)
			continue
		}
		pushMathCoverageInts(t, st, arg)
	}
}
