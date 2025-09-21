package exec

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func setThrowImmediate(t *testing.T, op *helpers.AdvancedOP, bits uint, value uint64) {
	t.Helper()

	slice := cell.BeginCell().MustStoreUInt(value, bits).EndCell().BeginParse()
	if err := op.DeserializeSuffix(slice); err != nil {
		t.Fatalf("set immediate: %v", err)
	}
}

func mustStackInt(t *testing.T, stack *vm.Stack, index int) *big.Int {
	t.Helper()

	val, err := stack.Get(index)
	if err != nil {
		t.Fatalf("stack get: %v", err)
	}

	bi, ok := val.(*big.Int)
	if !ok {
		t.Fatalf("stack value at %d has type %T, want *big.Int", index, val)
	}

	return bi
}

func newThrowRecorderState() (*vm.State, *throwRecorder) {
	state := newTestState()
	recorder := &throwRecorder{}

	state.Reg.C[2] = &testContinuation{
		name: "throw_handler",
		onJump: func(s *vm.State) (vm.Continuation, error) {
			recorder.calls++
			recorder.stacks = append(recorder.stacks, s.Stack.Copy())
			return nil, nil
		},
	}

	return state, recorder
}

type throwRecorder struct {
	calls  int
	stacks []*vm.Stack
}

func TestThrowFixedSerialization(t *testing.T) {
	t.Run("short", func(t *testing.T) {
		op := newThrowFixed("THROW", []byte{0xF2, 0x00}, 10, 6, 0, false)
		setThrowImmediate(t, op, 6, 0x37)

		if got := op.SerializeText(); got != "THROW 55" {
			t.Fatalf("unexpected mnemonic: %s", got)
		}

		val := op.Serialize().EndCell().BeginParse().MustLoadUInt(16)
		if val != 0xF237 {
			t.Fatalf("expected machine encoding 0xF237, got %#x", val)
		}
	})

	t.Run("long", func(t *testing.T) {
		op := newThrowFixed("THROWARG", []byte{0xF2, 0xC8, 0x00}, 13, 11, 0, true)
		setThrowImmediate(t, op, 11, 0x345)

		if got := op.SerializeText(); got != "THROWARG 837" {
			t.Fatalf("unexpected mnemonic: %s", got)
		}

		val := op.Serialize().EndCell().BeginParse().MustLoadUInt(24)
		if val != 0xF2CB45 {
			t.Fatalf("expected machine encoding 0xF2CB45, got %#x", val)
		}
	})
}

func TestThrowUnconditional(t *testing.T) {
	op := newThrowFixed("THROW", []byte{0xF2, 0x00}, 10, 6, 0, false)
	setThrowImmediate(t, op, 6, 0x12)

	state, recorder := newThrowRecorderState()

	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret: %v", err)
	}

	if recorder.calls != 1 {
		t.Fatalf("expected throw handler invoked once, got %d", recorder.calls)
	}

	if len(recorder.stacks) != 1 {
		t.Fatalf("expected one captured stack, got %d", len(recorder.stacks))
	}

	thrown := recorder.stacks[0]
	if thrown.Len() != 2 {
		t.Fatalf("expected throw stack size 2, got %d", thrown.Len())
	}

	if exc := mustStackInt(t, thrown, 0); exc.Uint64() != 0x12 {
		t.Fatalf("expected exception 0x12 on top, got %s", exc.String())
	}

	if arg := mustStackInt(t, thrown, 1); arg.Sign() != 0 {
		t.Fatalf("expected default zero argument, got %s", arg.String())
	}

	if state.Stack.Len() != 2 {
		t.Fatalf("expected runtime stack size 2, got %d", state.Stack.Len())
	}

	if exc := mustStackInt(t, state.Stack, 0); exc.Uint64() != 0x12 {
		t.Fatalf("runtime stack top mismatch, got %s", exc.String())
	}

	if arg := mustStackInt(t, state.Stack, 1); arg.Sign() != 0 {
		t.Fatalf("runtime stack argument mismatch, got %s", arg.String())
	}

	if state.Gas.Consumed != vm.ExceptionGasPrice {
		t.Fatalf("expected exception gas %d, got %d", vm.ExceptionGasPrice, state.Gas.Consumed)
	}

	if bits := state.CurrentCode.BitsLeft(); bits != 0 {
		t.Fatalf("expected empty current code, have %d bits", bits)
	}
}

func TestThrowArgUnconditional(t *testing.T) {
	op := newThrowFixed("THROWARG", []byte{0xF2, 0xC8, 0x00}, 13, 11, 0, true)
	setThrowImmediate(t, op, 11, 0x345)

	state, recorder := newThrowRecorderState()

	argVal := big.NewInt(789)
	if err := state.Stack.PushInt(argVal); err != nil {
		t.Fatalf("push arg: %v", err)
	}

	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret: %v", err)
	}

	if recorder.calls != 1 {
		t.Fatalf("expected throw handler invoked once, got %d", recorder.calls)
	}

	thrown := recorder.stacks[0]
	if thrown.Len() != 2 {
		t.Fatalf("expected throw stack size 2, got %d", thrown.Len())
	}

	if exc := mustStackInt(t, thrown, 0); exc.Uint64() != 0x345 {
		t.Fatalf("expected exception 0x345 on top, got %s", exc.String())
	}

	if arg := mustStackInt(t, thrown, 1); arg.Cmp(argVal) != 0 {
		t.Fatalf("expected argument %s, got %s", argVal.String(), arg.String())
	}

	if state.Stack.Len() != 2 {
		t.Fatalf("expected runtime stack size 2, got %d", state.Stack.Len())
	}

	if state.Gas.Consumed != vm.ExceptionGasPrice {
		t.Fatalf("expected exception gas %d, got %d", vm.ExceptionGasPrice, state.Gas.Consumed)
	}
}

func TestThrowIfVariants(t *testing.T) {
	cases := []struct {
		name        string
		op          *helpers.AdvancedOP
		immBits     uint
		imm         uint64
		cond        bool
		expectThrow bool
	}{
		{
			name:        "throwif-true",
			op:          newThrowFixed("THROWIF", []byte{0xF2, 0x40}, 10, 6, 3, false),
			immBits:     6,
			imm:         0x21,
			cond:        true,
			expectThrow: true,
		},
		{
			name:        "throwif-false",
			op:          newThrowFixed("THROWIF", []byte{0xF2, 0x40}, 10, 6, 3, false),
			immBits:     6,
			imm:         0x1B,
			cond:        false,
			expectThrow: false,
		},
		{
			name:        "throwifnot-false",
			op:          newThrowFixed("THROWIFNOT", []byte{0xF2, 0x80}, 10, 6, 2, false),
			immBits:     6,
			imm:         0x02,
			cond:        false,
			expectThrow: true,
		},
		{
			name:        "throwifnot-true",
			op:          newThrowFixed("THROWIFNOT", []byte{0xF2, 0x80}, 10, 6, 2, false),
			immBits:     6,
			imm:         0x03,
			cond:        true,
			expectThrow: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setThrowImmediate(t, tc.op, tc.immBits, tc.imm)

			state, recorder := newThrowRecorderState()

			sentinel := big.NewInt(777)
			if err := state.Stack.PushInt(sentinel); err != nil {
				t.Fatalf("push sentinel: %v", err)
			}

			if err := state.Stack.PushBool(tc.cond); err != nil {
				t.Fatalf("push cond: %v", err)
			}

			if err := tc.op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}

			if tc.expectThrow {
				if recorder.calls != 1 {
					t.Fatalf("expected throw handler once, got %d", recorder.calls)
				}

				if state.Stack.Len() != 2 {
					t.Fatalf("expected runtime stack size 2, got %d", state.Stack.Len())
				}

				if exc := mustStackInt(t, state.Stack, 0); exc.Uint64() != tc.imm {
					t.Fatalf("unexpected exception %s", exc.String())
				}

				if arg := mustStackInt(t, state.Stack, 1); arg.Sign() != 0 {
					t.Fatalf("expected zero argument, got %s", arg.String())
				}

				if state.Gas.Consumed != vm.ExceptionGasPrice {
					t.Fatalf("expected gas %d, got %d", vm.ExceptionGasPrice, state.Gas.Consumed)
				}
			} else {
				if recorder.calls != 0 {
					t.Fatalf("unexpected throw handler invocation: %d", recorder.calls)
				}

				if state.Stack.Len() != 1 {
					t.Fatalf("expected stack size 1, got %d", state.Stack.Len())
				}

				if top := mustStackInt(t, state.Stack, 0); top.Cmp(sentinel) != 0 {
					t.Fatalf("expected sentinel %s, got %s", sentinel.String(), top.String())
				}

				if state.Gas.Consumed != 0 {
					t.Fatalf("expected zero gas consumption, got %d", state.Gas.Consumed)
				}
			}
		})
	}
}

func TestThrowArgIfVariants(t *testing.T) {
	cases := []struct {
		name        string
		op          *helpers.AdvancedOP
		immBits     uint
		imm         uint64
		cond        bool
		expectThrow bool
	}{
		{
			name:        "throwargif-true",
			op:          newThrowFixed("THROWARGIF", []byte{0xF2, 0xD8, 0x00}, 13, 11, 3, true),
			immBits:     11,
			imm:         0x155,
			cond:        true,
			expectThrow: true,
		},
		{
			name:        "throwargif-false",
			op:          newThrowFixed("THROWARGIF", []byte{0xF2, 0xD8, 0x00}, 13, 11, 3, true),
			immBits:     11,
			imm:         0x066,
			cond:        false,
			expectThrow: false,
		},
		{
			name:        "throwargifnot-false",
			op:          newThrowFixed("THROWARGIFNOT", []byte{0xF2, 0xE8, 0x00}, 13, 11, 2, true),
			immBits:     11,
			imm:         0x077,
			cond:        false,
			expectThrow: true,
		},
		{
			name:        "throwargifnot-true",
			op:          newThrowFixed("THROWARGIFNOT", []byte{0xF2, 0xE8, 0x00}, 13, 11, 2, true),
			immBits:     11,
			imm:         0x088,
			cond:        true,
			expectThrow: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setThrowImmediate(t, tc.op, tc.immBits, tc.imm)

			state, recorder := newThrowRecorderState()

			sentinel := big.NewInt(555)
			if err := state.Stack.PushInt(sentinel); err != nil {
				t.Fatalf("push sentinel: %v", err)
			}

			argVal := big.NewInt(321)
			if err := state.Stack.PushInt(argVal); err != nil {
				t.Fatalf("push arg: %v", err)
			}

			if err := state.Stack.PushBool(tc.cond); err != nil {
				t.Fatalf("push cond: %v", err)
			}

			if err := tc.op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}

			if tc.expectThrow {
				if recorder.calls != 1 {
					t.Fatalf("expected throw handler once, got %d", recorder.calls)
				}

				if state.Stack.Len() != 2 {
					t.Fatalf("expected runtime stack size 2, got %d", state.Stack.Len())
				}

				if exc := mustStackInt(t, state.Stack, 0); exc.Uint64() != tc.imm {
					t.Fatalf("unexpected exception %s", exc.String())
				}

				if arg := mustStackInt(t, state.Stack, 1); arg.Cmp(argVal) != 0 {
					t.Fatalf("expected argument %s, got %s", argVal.String(), arg.String())
				}

				if state.Gas.Consumed != vm.ExceptionGasPrice {
					t.Fatalf("expected gas %d, got %d", vm.ExceptionGasPrice, state.Gas.Consumed)
				}
			} else {
				if recorder.calls != 0 {
					t.Fatalf("unexpected throw handler invocation: %d", recorder.calls)
				}

				if state.Stack.Len() != 1 {
					t.Fatalf("expected stack size 1, got %d", state.Stack.Len())
				}

				if top := mustStackInt(t, state.Stack, 0); top.Cmp(sentinel) != 0 {
					t.Fatalf("expected sentinel %s, got %s", sentinel.String(), top.String())
				}

				if state.Gas.Consumed != 0 {
					t.Fatalf("expected zero gas consumption, got %d", state.Gas.Consumed)
				}
			}
		})
	}
}

func TestThrowAnyUnconditional(t *testing.T) {
	cases := []struct {
		name       string
		args       uint64
		expectName string
		hasParam   bool
		exc        uint64
		argValue   int64
	}{
		{
			name:       "without-arg",
			args:       0,
			expectName: "THROWANY",
			hasParam:   false,
			exc:        0x1234,
		},
		{
			name:       "with-arg",
			args:       1,
			expectName: "THROWARGANY",
			hasParam:   true,
			exc:        0x245,
			argValue:   456,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := newThrowAny()
			setThrowImmediate(t, op, 3, tc.args)

			if got := op.SerializeText(); got != tc.expectName {
				t.Fatalf("unexpected mnemonic %s", got)
			}

			state, recorder := newThrowRecorderState()

			if tc.hasParam {
				if err := state.Stack.PushInt(big.NewInt(tc.argValue)); err != nil {
					t.Fatalf("push arg: %v", err)
				}
			}

			if err := state.Stack.PushInt(big.NewInt(int64(tc.exc))); err != nil {
				t.Fatalf("push exception: %v", err)
			}

			if err := op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}

			if recorder.calls != 1 {
				t.Fatalf("expected throw handler once, got %d", recorder.calls)
			}

			thrown := recorder.stacks[0]
			if thrown.Len() != 2 {
				t.Fatalf("expected throw stack size 2, got %d", thrown.Len())
			}

			if exc := mustStackInt(t, thrown, 0); exc.Uint64() != tc.exc {
				t.Fatalf("unexpected exception %s", exc.String())
			}

			if tc.hasParam {
				if arg := mustStackInt(t, thrown, 1); arg.Int64() != tc.argValue {
					t.Fatalf("unexpected argument %s", arg.String())
				}
			} else {
				if arg := mustStackInt(t, thrown, 1); arg.Sign() != 0 {
					t.Fatalf("expected zero argument, got %s", arg.String())
				}
			}

			if state.Gas.Consumed != vm.ExceptionGasPrice {
				t.Fatalf("expected gas %d, got %d", vm.ExceptionGasPrice, state.Gas.Consumed)
			}
		})
	}
}

func TestThrowAnyConditional(t *testing.T) {
	cases := []struct {
		name        string
		args        uint64
		expectName  string
		cond        bool
		hasParam    bool
		expectThrow bool
	}{
		{"if-true-throws", 2, "THROWANYIF", true, false, true},
		{"if-true-skips", 2, "THROWANYIF", false, false, false},
		{"ifnot-false-throws", 4, "THROWANYIFNOT", false, false, true},
		{"ifnot-true-skips", 4, "THROWANYIFNOT", true, false, false},
		{"arg-if-true-throws", 3, "THROWARGANYIF", true, true, true},
		{"arg-if-true-skips", 3, "THROWARGANYIF", false, true, false},
		{"arg-ifnot-false-throws", 5, "THROWARGANYIFNOT", false, true, true},
		{"arg-ifnot-true-skips", 5, "THROWARGANYIFNOT", true, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := newThrowAny()
			setThrowImmediate(t, op, 3, tc.args)

			if got := op.SerializeText(); got != tc.expectName {
				t.Fatalf("unexpected mnemonic %s", got)
			}

			state, recorder := newThrowRecorderState()

			sentinel := big.NewInt(999)
			if err := state.Stack.PushInt(sentinel); err != nil {
				t.Fatalf("push sentinel: %v", err)
			}

			var argVal *big.Int
			if tc.hasParam {
				argVal = big.NewInt(0x77)
				if err := state.Stack.PushInt(argVal); err != nil {
					t.Fatalf("push arg: %v", err)
				}
			}

			excVal := big.NewInt(0x222)
			if err := state.Stack.PushInt(excVal); err != nil {
				t.Fatalf("push exception: %v", err)
			}

			if err := state.Stack.PushBool(tc.cond); err != nil {
				t.Fatalf("push cond: %v", err)
			}

			if err := op.Interpret(state); err != nil {
				t.Fatalf("interpret: %v", err)
			}

			if tc.expectThrow {
				if recorder.calls != 1 {
					t.Fatalf("expected throw handler once, got %d", recorder.calls)
				}

				if state.Stack.Len() != 2 {
					t.Fatalf("expected runtime stack size 2, got %d", state.Stack.Len())
				}

				if exc := mustStackInt(t, state.Stack, 0); exc.Cmp(excVal) != 0 {
					t.Fatalf("unexpected exception %s", exc.String())
				}

				if tc.hasParam {
					if arg := mustStackInt(t, state.Stack, 1); arg.Cmp(argVal) != 0 {
						t.Fatalf("unexpected argument %s", arg.String())
					}
				} else {
					if arg := mustStackInt(t, state.Stack, 1); arg.Sign() != 0 {
						t.Fatalf("expected zero argument, got %s", arg.String())
					}
				}

				if state.Gas.Consumed != vm.ExceptionGasPrice {
					t.Fatalf("expected gas %d, got %d", vm.ExceptionGasPrice, state.Gas.Consumed)
				}
			} else {
				if recorder.calls != 0 {
					t.Fatalf("unexpected throw handler invocation: %d", recorder.calls)
				}

				expectedLen := 1
				if state.Stack.Len() != expectedLen {
					t.Fatalf("expected stack size %d, got %d", expectedLen, state.Stack.Len())
				}

				if top := mustStackInt(t, state.Stack, 0); top.Cmp(sentinel) != 0 {
					t.Fatalf("expected sentinel %s, got %s", sentinel.String(), top.String())
				}

				if state.Gas.Consumed != 0 {
					t.Fatalf("expected zero gas consumption, got %d", state.Gas.Consumed)
				}
			}
		})
	}
}
