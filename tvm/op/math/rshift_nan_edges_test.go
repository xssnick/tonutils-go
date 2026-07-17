package math

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type mathInterpretOp interface {
	Interpret(*vm.State) error
}

func TestRoundedRightShiftNaNValueRules(t *testing.T) {
	t.Run("DynamicShiftZeroOverflows", func(t *testing.T) {
		for _, op := range []struct {
			name string
			op   mathInterpretOp
		}{
			{name: "RSHIFTR", op: RSHIFTR()},
			{name: "RSHIFTC", op: RSHIFTC()},
		} {
			t.Run(op.name, func(t *testing.T) {
				st := newMathCoverageState()
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
				pushMathCoverageInts(t, st, 0)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			})
		}
	})

	t.Run("DynamicPositiveShiftReturnsZero", func(t *testing.T) {
		for _, op := range []struct {
			name string
			op   mathInterpretOp
		}{
			{name: "RSHIFTR", op: RSHIFTR()},
			{name: "RSHIFTC", op: RSHIFTC()},
		} {
			for _, shift := range []int64{1, 13, 256} {
				t.Run(op.name, func(t *testing.T) {
					st := newMathCoverageState()
					if err := st.Stack.PushAny(vm.NaN{}); err != nil {
						t.Fatalf("push NaN: %v", err)
					}
					pushMathCoverageInts(t, st, shift)
					if err := op.op.Interpret(st); err != nil {
						t.Fatalf("%s shift=%d: %v", op.name, shift, err)
					}
					if got := popMathCoverageInt(t, st); got != 0 {
						t.Fatalf("%s shift=%d result = %d, want 0", op.name, shift, got)
					}
				})
			}
		}
	})

	t.Run("ImmediateReturnsZero", func(t *testing.T) {
		for _, op := range []struct {
			name string
			op   mathInterpretOp
		}{
			{name: "RSHIFTR#1", op: RSHIFTRCODE(1)},
			{name: "RSHIFTR#13", op: RSHIFTRCODE(13)},
			{name: "RSHIFTC#1", op: RSHIFTCCODE(1)},
			{name: "RSHIFTC#13", op: RSHIFTCCODE(13)},
		} {
			t.Run(op.name, func(t *testing.T) {
				st := newMathCoverageState()
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN: %v", err)
				}
				if err := op.op.Interpret(st); err != nil {
					t.Fatalf("%s: %v", op.name, err)
				}
				if got := popMathCoverageInt(t, st); got != 0 {
					t.Fatalf("%s result = %d, want 0", op.name, got)
				}
			})
		}
	})
}

func TestFiniteRightShiftModImmediateNaNOverflows(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MODPOW2#", op: MODPOW2CODE(1)},
		{name: "MODPOW2R#", op: MODPOW2RCODE(1)},
		{name: "MODPOW2C#", op: MODPOW2CCODE(1)},
		{name: "RSHIFT#MOD", op: RSHIFTCODEMOD(1)},
		{name: "RSHIFTR#MOD", op: RSHIFTRCODEMOD(1)},
		{name: "RSHIFTC#MOD", op: RSHIFTCCODEMOD(1)},
	} {
		t.Run(op.name, func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestFiniteDynamicShiftModNaNRules(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MODPOW2", op: MODPOW2()},
		{name: "MODPOW2R", op: MODPOW2R()},
		{name: "MODPOW2C", op: MODPOW2C()},
		{name: "RSHIFTMOD", op: RSHIFTMOD()},
		{name: "RSHIFTMODR", op: RSHIFTMODR()},
		{name: "RSHIFTMODC", op: RSHIFTMODC()},
	} {
		t.Run(op.name, func(t *testing.T) {
			for _, shift := range []int64{0, 1, 13, 256} {
				st := newMathCoverageState()
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN value: %v", err)
				}
				pushMathCoverageInts(t, st, shift)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			}

			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN value: %v", err)
			}
			pushMathCoverageInts(t, st, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)

			st = newMathCoverageState()
			pushMathCoverageInts(t, st, 7)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})
	}
}

func TestFiniteMulDynamicShiftModNaNRules(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULMODPOW2", op: MULMODPOW2_VAR()},
		{name: "MULMODPOW2R", op: MULMODPOW2R_VAR()},
		{name: "MULMODPOW2C", op: MULMODPOW2C_VAR()},
		{name: "MULRSHIFTMOD", op: MULRSHIFTMOD_VAR()},
		{name: "MULRSHIFTRMOD", op: MULRSHIFTRMOD_VAR()},
		{name: "MULRSHIFTCMOD", op: MULRSHIFTCMOD_VAR()},
	} {
		t.Run(op.name, func(t *testing.T) {
			for _, shift := range []int64{0, 1, 13, 256} {
				st := newMathCoverageState()
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN x: %v", err)
				}
				pushMathCoverageInts(t, st, 2, shift)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)

				st = newMathCoverageState()
				pushMathCoverageInts(t, st, 2)
				if err := st.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatalf("push NaN y: %v", err)
				}
				pushMathCoverageInts(t, st, shift)
				assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
			}

			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
			pushMathCoverageInts(t, st, 2, 257)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)

			st = newMathCoverageState()
			pushMathCoverageInts(t, st, 2, 3)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN shift: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeRangeCheck)
		})
	}
}

func TestMulDynamicShiftModWideProductBoundaries(t *testing.T) {
	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULMODPOW2", op: MULMODPOW2_VAR()},
		{name: "MULMODPOW2R", op: MULMODPOW2R_VAR()},
		{name: "MULMODPOW2C", op: MULMODPOW2C_VAR()},
	} {
		t.Run(op.name, func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushInt(maxTVMInt); err != nil {
				t.Fatalf("push max int: %v", err)
			}
			pushMathCoverageInts(t, st, 2, 0)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s result = %d, want 0", op.name, got)
			}
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULRSHIFTMOD", op: MULRSHIFTMOD_VAR()},
		{name: "MULRSHIFTRMOD", op: MULRSHIFTRMOD_VAR()},
		{name: "MULRSHIFTCMOD", op: MULRSHIFTCMOD_VAR()},
	} {
		t.Run(op.name, func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushInt(maxTVMInt); err != nil {
				t.Fatalf("push max int: %v", err)
			}
			pushMathCoverageInts(t, st, 2, 0)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}

func TestFiniteMulImmediateShiftModNaNAndWideRules(t *testing.T) {
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULMODPOW2#", op: MULMODPOW2CODE(1)},
		{name: "MULMODPOW2R#", op: MULMODPOW2RCODE(1)},
		{name: "MULMODPOW2C#", op: MULMODPOW2CCODE(1)},
		{name: "MULRSHIFT#MOD", op: MULRSHIFTCODEMOD(1)},
		{name: "MULRSHIFTR#MOD", op: MULRSHIFTRCODEMOD(1)},
		{name: "MULRSHIFTC#MOD", op: MULRSHIFTCCODEMOD(1)},
	} {
		t.Run(op.name, func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN x: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)

			st = newMathCoverageState()
			pushMathCoverageInts(t, st, 2)
			if err := st.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN y: %v", err)
			}
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}

	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULMODPOW2#", op: MULMODPOW2CODE(1)},
		{name: "MULMODPOW2R#", op: MULMODPOW2RCODE(1)},
		{name: "MULMODPOW2C#", op: MULMODPOW2CCODE(1)},
	} {
		t.Run(op.name+"_wide_product", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushInt(maxTVMInt); err != nil {
				t.Fatalf("push max int: %v", err)
			}
			pushMathCoverageInts(t, st, 2)
			if err := op.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}
			if got := popMathCoverageInt(t, st); got != 0 {
				t.Fatalf("%s result = %d, want 0", op.name, got)
			}
		})
	}

	for _, op := range []struct {
		name string
		op   mathInterpretOp
	}{
		{name: "MULRSHIFT#MOD", op: MULRSHIFTCODEMOD(1)},
		{name: "MULRSHIFTR#MOD", op: MULRSHIFTRCODEMOD(1)},
		{name: "MULRSHIFTC#MOD", op: MULRSHIFTCCODEMOD(1)},
	} {
		t.Run(op.name+"_wide_product_overflow", func(t *testing.T) {
			st := newMathCoverageState()
			if err := st.Stack.PushInt(maxTVMInt); err != nil {
				t.Fatalf("push max int: %v", err)
			}
			pushMathCoverageInts(t, st, 3)
			assertMathCoverageVMError(t, op.op.Interpret(st), vmerr.CodeIntOverflow)
		})
	}
}
