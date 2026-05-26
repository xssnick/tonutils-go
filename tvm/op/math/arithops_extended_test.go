package math

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	"github.com/xssnick/tonutils-go/tvm/tuple"
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

func TestNonQuietNaNOperandsUseCppPopTimingUnderTry(t *testing.T) {
	tests := []struct {
		name      string
		op        vm.OP
		args      []any
		wantStack []any
		wantCode  int
	}{
		{name: "unary", op: NEGATE(), args: []any{vm.NaN{}}},
		{name: "binary", op: SUM(), args: []any{int64(7), vm.NaN{}}},
		{name: "division", op: DIV(), args: []any{int64(7), vm.NaN{}}},
		{name: "shift count", op: LSHIFT(), args: []any{int64(7), vm.NaN{}}, wantStack: []any{int64(7)}, wantCode: vmerr.CodeRangeCheck},
		{name: "shift value", op: LSHIFT(), args: []any{vm.NaN{}, int64(7)}},
		{name: "mod", op: MOD(), args: []any{int64(7), vm.NaN{}}},
		{name: "fits", op: FITS(7), args: []any{vm.NaN{}}},
		{name: "compare", op: CMP(), args: []any{int64(7), vm.NaN{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := vm.NewStack()
			pushMathArgs(t, st, tt.args...)
			var opErr error
			var stackAtError *vm.Stack
			var handlerCalls int

			body := &mathTryContinuation{
				jump: func(s *vm.State) (vm.Continuation, error) {
					opErr = tt.op.Interpret(s)
					if opErr == nil {
						return nil, nil
					}
					stackAtError = s.Stack.Copy()

					code, ok := vmerr.ErrorCode(opErr)
					if !ok {
						return nil, opErr
					}
					return nil, s.ThrowException(big.NewInt(code))
				},
			}
			handler := &mathTryContinuation{
				jump: func(*vm.State) (vm.Continuation, error) {
					handlerCalls++
					return nil, nil
				},
			}
			if err := st.PushContinuation(body); err != nil {
				t.Fatalf("failed to push TRY body: %v", err)
			}
			if err := st.PushContinuation(handler); err != nil {
				t.Fatalf("failed to push TRY handler: %v", err)
			}

			state := vm.NewExecutionState(
				vm.DefaultGlobalVersion,
				vm.GasWithLimit(1_000_000),
				cell.BeginCell().EndCell(),
				tuple.Tuple{},
				st,
			)

			if err := execop.TRY().Interpret(state); err != nil {
				t.Fatalf("TRY failed: %v", err)
			}
			if opErr == nil {
				t.Fatalf("expected %s to fail on NaN", tt.op.SerializeText())
			}
			wantCode := tt.wantCode
			if wantCode == 0 {
				wantCode = vmerr.CodeIntOverflow
			}
			if code, ok := vmerr.ErrorCode(opErr); !ok || code != int64(wantCode) {
				t.Fatalf("expected code %d, got %v", wantCode, opErr)
			}
			if handlerCalls != 1 {
				t.Fatalf("expected TRY handler to run once, got %d", handlerCalls)
			}
			if stackAtError == nil {
				t.Fatal("missing stack snapshot at arithmetic error")
			}

			assertMathStackValues(t, stackAtError, tt.wantStack...)
		})
	}
}

type mathTryContinuation struct {
	jump func(*vm.State) (vm.Continuation, error)
}

func (c *mathTryContinuation) GetControlData() *vm.ControlData {
	return nil
}

func (c *mathTryContinuation) Jump(state *vm.State) (vm.Continuation, error) {
	if c.jump == nil {
		return nil, nil
	}
	return c.jump(state)
}

func (c *mathTryContinuation) Copy() vm.Continuation {
	return &mathTryContinuation{jump: c.jump}
}

func pushMathArgs(t *testing.T, st *vm.Stack, vals ...any) {
	t.Helper()

	for _, val := range vals {
		var err error
		switch v := val.(type) {
		case int64:
			err = st.PushInt(big.NewInt(v))
		default:
			err = st.PushAny(v)
		}
		if err != nil {
			t.Fatalf("failed to push test value: %v", err)
		}
	}
}

func assertMathStackValues(t *testing.T, st *vm.Stack, vals ...any) {
	t.Helper()

	if st.Len() != len(vals) {
		t.Fatalf("stack length changed: got=%d want=%d stack=%s", st.Len(), len(vals), st.String())
	}
	for i := range vals {
		want := vals[len(vals)-1-i]
		got, err := st.Get(i)
		if err != nil {
			t.Fatalf("failed to read stack value %d: %v", i, err)
		}

		switch w := want.(type) {
		case vm.NaN:
			switch got.(type) {
			case vm.NaN:
			default:
				t.Fatalf("stack value %d changed: got=%T want NaN", i, got)
			}
		case int64:
			bi, ok := got.(*big.Int)
			if !ok || bi.Int64() != w {
				t.Fatalf("stack value %d changed: got=%v want=%d", i, got, w)
			}
		default:
			t.Fatalf("unsupported expected value type %T", want)
		}
	}
}
