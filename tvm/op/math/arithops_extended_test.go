package math

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestQuietArithmeticOverflowAndNaN(t *testing.T) {
	t.Run("QADDOverflowBecomesNaN", func(t *testing.T) {
		st := vm.NewStack()
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		if err := st.PushInt(max); err != nil {
			t.Fatalf("failed to push max int: %v", err)
		}
		if err := st.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push increment: %v", err)
		}

		if err := QADD().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("QADD failed: %v", err)
		}

		got, err := st.PopInt()
		if err != nil {
			t.Fatalf("failed to pop QADD result: %v", err)
		}
		if got != nil {
			t.Fatalf("expected NaN after quiet overflow, got %s", got.String())
		}
	})

	t.Run("QANDPropagatesNaN", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("failed to push NaN: %v", err)
		}
		if err := st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push int: %v", err)
		}

		if err := QAND().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("QAND failed: %v", err)
		}

		got, err := st.PopInt()
		if err != nil {
			t.Fatalf("failed to pop QAND result: %v", err)
		}
		if got != nil {
			t.Fatalf("expected NaN propagation, got %s", got.String())
		}
	})

	t.Run("QPOW2OverflowBecomesNaN", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(300)); err != nil {
			t.Fatalf("failed to push exponent: %v", err)
		}

		if err := QPOW2().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("QPOW2 failed: %v", err)
		}

		got, err := st.PopInt()
		if err != nil {
			t.Fatalf("failed to pop QPOW2 result: %v", err)
		}
		if got != nil {
			t.Fatalf("expected NaN after quiet pow2 overflow, got %s", got.String())
		}
	})
}

func TestFitsAndBitSizeSemantics(t *testing.T) {
	t.Run("FITSXRejectsOutOfRange", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(64)); err != nil {
			t.Fatalf("failed to push x: %v", err)
		}
		if err := st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push bits: %v", err)
		}

		err := FITSX().Interpret(&vm.State{Stack: st})
		if err == nil {
			t.Fatalf("expected FITSX to fail for 64 in 7 signed bits")
		}
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeIntOverflow {
			t.Fatalf("expected int overflow, got %v", err)
		}
	})

	t.Run("QFITSXReturnsNaNOnOutOfRange", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(64)); err != nil {
			t.Fatalf("failed to push x: %v", err)
		}
		if err := st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push bits: %v", err)
		}

		if err := QFITSX().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("QFITSX failed: %v", err)
		}
		got, err := st.PopInt()
		if err != nil {
			t.Fatalf("failed to pop QFITSX result: %v", err)
		}
		if got != nil {
			t.Fatalf("expected NaN, got %s", got.String())
		}
	})

	t.Run("BITSIZESignedAndUnsigned", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push value: %v", err)
		}
		if err := BITSIZE().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("BITSIZE failed: %v", err)
		}
		gotSigned, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop signed bitsize: %v", err)
		}
		if gotSigned.Int64() != 4 {
			t.Fatalf("unexpected signed bitsize: %s", gotSigned.String())
		}

		if err = st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push value again: %v", err)
		}
		if err = UBITSIZE().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("UBITSIZE failed: %v", err)
		}
		gotUnsigned, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop unsigned bitsize: %v", err)
		}
		if gotUnsigned.Int64() != 3 {
			t.Fatalf("unexpected unsigned bitsize: %s", gotUnsigned.String())
		}
	})
}

func TestCompareAndNaNHelpers(t *testing.T) {
	t.Run("MINMAXKeepsTVMOrder", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(5)); err != nil {
			t.Fatalf("failed to push x: %v", err)
		}
		if err := st.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push y: %v", err)
		}

		if err := MINMAX().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("MINMAX failed: %v", err)
		}

		gotMax, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop max: %v", err)
		}
		gotMin, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop min: %v", err)
		}
		if gotMax.Int64() != 5 || gotMin.Int64() != 2 {
			t.Fatalf("unexpected MINMAX order: max=%s min=%s", gotMax.String(), gotMin.String())
		}
	})

	t.Run("CMPReturnsSign", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("failed to push x: %v", err)
		}
		if err := st.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("failed to push y: %v", err)
		}

		if err := CMP().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("CMP failed: %v", err)
		}
		got, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop cmp result: %v", err)
		}
		if got.Int64() != -1 {
			t.Fatalf("unexpected CMP result: %s", got.String())
		}
	})

	t.Run("ISNANAndCHKNAN", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("failed to push NaN: %v", err)
		}
		if err := ISNAN().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("ISNAN failed: %v", err)
		}
		isNaN, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop ISNAN result: %v", err)
		}
		if !isNaN {
			t.Fatalf("expected ISNAN to return true")
		}

		st = vm.NewStack()
		if err = st.PushAny(vm.NaN{}); err != nil {
			t.Fatalf("failed to push NaN again: %v", err)
		}
		err = CHKNAN().Interpret(&vm.State{Stack: st})
		if err == nil {
			t.Fatalf("expected CHKNAN to fail on NaN")
		}
	})
}

func TestQuietUnaryMinMaxAndCompareVariants(t *testing.T) {
	t.Run("QuietUnary", func(t *testing.T) {
		tests := []struct {
			name string
			op   vm.OP
			in   int64
			want int64
		}{
			{name: "QNEGATE", op: QNEGATE(), in: 7, want: -7},
			{name: "QINC", op: QINC(), in: 7, want: 8},
			{name: "QDEC", op: QDEC(), in: 7, want: 6},
			{name: "QABS", op: QABS(), in: -7, want: 7},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := vm.NewStack()
				if err := st.PushInt(big.NewInt(tt.in)); err != nil {
					t.Fatalf("failed to push input: %v", err)
				}

				if err := tt.op.Interpret(&vm.State{Stack: st}); err != nil {
					t.Fatalf("%s failed: %v", tt.name, err)
				}

				got, err := st.PopIntFinite()
				if err != nil {
					t.Fatalf("failed to pop result: %v", err)
				}
				if got.Int64() != tt.want {
					t.Fatalf("unexpected result for %s: got=%s want=%d", tt.name, got.String(), tt.want)
				}
			})
		}
	})

	t.Run("QuietMinMax", func(t *testing.T) {
		tests := []struct {
			name string
			op   vm.OP
			x    int64
			y    int64
			want int64
		}{
			{name: "QMIN", op: QMIN(), x: 5, y: 2, want: 2},
			{name: "QMAX", op: QMAX(), x: 5, y: 2, want: 5},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := vm.NewStack()
				if err := st.PushInt(big.NewInt(tt.x)); err != nil {
					t.Fatalf("failed to push x: %v", err)
				}
				if err := st.PushInt(big.NewInt(tt.y)); err != nil {
					t.Fatalf("failed to push y: %v", err)
				}

				if err := tt.op.Interpret(&vm.State{Stack: st}); err != nil {
					t.Fatalf("%s failed: %v", tt.name, err)
				}

				got, err := st.PopIntFinite()
				if err != nil {
					t.Fatalf("failed to pop result: %v", err)
				}
				if got.Int64() != tt.want {
					t.Fatalf("unexpected result for %s: got=%s want=%d", tt.name, got.String(), tt.want)
				}
			})
		}
	})

	t.Run("QuietCompareVariants", func(t *testing.T) {
		tests := []struct {
			name string
			op   vm.OP
			push func(*vm.Stack) error
			want int64
		}{
			{
				name: "QLESS",
				op:   QLESS(),
				push: func(st *vm.Stack) error {
					if err := st.PushInt(big.NewInt(1)); err != nil {
						return err
					}
					return st.PushInt(big.NewInt(2))
				},
				want: -1,
			},
			{
				name: "QEQUAL",
				op:   QEQUAL(),
				push: func(st *vm.Stack) error {
					if err := st.PushInt(big.NewInt(2)); err != nil {
						return err
					}
					return st.PushInt(big.NewInt(2))
				},
				want: -1,
			},
			{
				name: "QLEQ",
				op:   QLEQ(),
				push: func(st *vm.Stack) error {
					if err := st.PushInt(big.NewInt(1)); err != nil {
						return err
					}
					return st.PushInt(big.NewInt(2))
				},
				want: -1,
			},
			{
				name: "QGREATER",
				op:   QGREATER(),
				push: func(st *vm.Stack) error {
					if err := st.PushInt(big.NewInt(3)); err != nil {
						return err
					}
					return st.PushInt(big.NewInt(2))
				},
				want: -1,
			},
			{
				name: "QNEQ",
				op:   QNEQ(),
				push: func(st *vm.Stack) error {
					if err := st.PushInt(big.NewInt(3)); err != nil {
						return err
					}
					return st.PushInt(big.NewInt(2))
				},
				want: -1,
			},
			{
				name: "QGEQ",
				op:   QGEQ(),
				push: func(st *vm.Stack) error {
					if err := st.PushInt(big.NewInt(3)); err != nil {
						return err
					}
					return st.PushInt(big.NewInt(2))
				},
				want: -1,
			},
			{
				name: "QEQINT",
				op:   QEQINT(-5),
				push: func(st *vm.Stack) error {
					return st.PushInt(big.NewInt(-5))
				},
				want: -1,
			},
			{
				name: "QLESSINT",
				op:   QLESSINT(-5),
				push: func(st *vm.Stack) error {
					return st.PushInt(big.NewInt(-6))
				},
				want: -1,
			},
			{
				name: "QGTINT",
				op:   QGTINT(-5),
				push: func(st *vm.Stack) error {
					return st.PushInt(big.NewInt(-4))
				},
				want: -1,
			},
			{
				name: "QNEQINT",
				op:   QNEQINT(-5),
				push: func(st *vm.Stack) error {
					return st.PushInt(big.NewInt(-4))
				},
				want: -1,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := vm.NewStack()
				if err := tt.push(st); err != nil {
					t.Fatalf("failed to prepare stack: %v", err)
				}

				if err := tt.op.Interpret(&vm.State{Stack: st}); err != nil {
					t.Fatalf("%s failed: %v", tt.name, err)
				}

				got, err := st.PopIntFinite()
				if err != nil {
					t.Fatalf("failed to pop result: %v", err)
				}
				if got.Int64() != tt.want {
					t.Fatalf("unexpected result for %s: got=%s want=%d", tt.name, got.String(), tt.want)
				}
			})
		}
	})
}
