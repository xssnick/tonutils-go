package exec

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func assertVMErrCode(t *testing.T, err error, code int64) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected VM error code %d", code)
	}
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != code {
		t.Fatalf("expected VM error code %d, got %v", code, err)
	}
}

func pushTestInts(t *testing.T, stack *vm.Stack, first, count int64) {
	t.Helper()
	for i := int64(0); i < count; i++ {
		if err := stack.PushInt(big.NewInt(first + i)); err != nil {
			t.Fatalf("push int: %v", err)
		}
	}
}

func TestRegisteredExecOpsInstantiate(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("expected exec package to register opcode getters")
	}

	for i, getter := range vm.List {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("registered getter %d panicked: %v", i, r)
				}
			}()
			if op := getter(); op == nil {
				t.Fatalf("registered getter %d returned nil", i)
			}
		}()
	}
}

func TestCallCCAdditionalCoverage(t *testing.T) {
	t.Run("GuardPaths", func(t *testing.T) {
		assertVMErrCode(t, CALLCC().Interpret(newTestState()), vmerr.CodeStackUnderflow)
		assertVMErrCode(t, CALLCCARGS(1, 0).Interpret(newTestState()), vmerr.CodeStackUnderflow)

		state := newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}
		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeRangeCheck)

		state = newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}
		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeStackUnderflow)

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}
		assertVMErrCode(t, RETVARARGS().Interpret(state), vmerr.CodeRangeCheck)
	})

	t.Run("RoundTrip", func(t *testing.T) {
		src := CALLCCARGS(2, -1)
		dst := CALLCCARGS(0, 0)
		if err := dst.Deserialize(src.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "CALLCCARGS 2,-1" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 24 {
			t.Fatalf("unexpected bits: %d", got)
		}
	})
}

func TestTryArgsAdditionalCoverage(t *testing.T) {
	t.Run("RoundTripNibblePackingAndMalformedSuffix", func(t *testing.T) {
		src := TRYARGS(18, 31)
		encoded := src.Serialize().EndCell()
		raw, err := encoded.MustBeginParse().LoadUInt(16)
		if err != nil {
			t.Fatalf("load encoded TRYARGS: %v", err)
		}
		if raw != 0xF32F {
			t.Fatalf("unexpected TRYARGS encoding: %#x", raw)
		}

		dst := TRYARGS(0, 0)
		if err = dst.Deserialize(encoded.MustBeginParse()); err != nil {
			t.Fatalf("deserialize TRYARGS failed: %v", err)
		}
		if got := dst.SerializeText(); got != "TRYARGS 2,15" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 16 {
			t.Fatalf("unexpected instruction bits: %d", got)
		}

		truncated := cell.BeginCell().MustStoreUInt(0xF3, 8).EndCell()
		if err = TRYARGS(0, 0).Deserialize(truncated.MustBeginParse()); err == nil {
			t.Fatal("expected TRYARGS without suffix to fail")
		}
	})

	t.Run("InterpretCopiesArgsAndReturnArity", func(t *testing.T) {
		state := newTestState()
		oldC2 := &testContinuation{name: "old_c2"}
		state.Reg.C[2] = oldC2

		seenArg := int64(0)
		seenRetvals := 0
		body := &testContinuation{
			name: "tryargs_body",
			onJump: func(s *vm.State) (vm.Continuation, error) {
				arg, err := s.Stack.PopIntFinite()
				if err != nil {
					return nil, err
				}
				seenArg = arg.Int64()
				seenRetvals = s.Reg.C[0].GetControlData().NumArgs
				return nil, nil
			},
		}
		handler := &testContinuation{name: "tryargs_handler"}

		if err := state.Stack.PushInt(big.NewInt(42)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(body); err != nil {
			t.Fatalf("push body: %v", err)
		}
		if err := state.Stack.PushContinuation(handler); err != nil {
			t.Fatalf("push handler: %v", err)
		}
		if err := TRYARGS(1, 2).Interpret(state); err != nil {
			t.Fatalf("tryargs failed: %v", err)
		}
		if seenArg != 42 || seenRetvals != 2 {
			t.Fatalf("unexpected TRYARGS body state: arg=%d retvals=%d", seenArg, seenRetvals)
		}

		data := state.Reg.C[2].GetControlData()
		if data == nil || continuationName(t, data.Save.C[2]) != "old_c2" {
			t.Fatalf("expected handler to save old c2")
		}
		cc, ok := data.Save.C[0].(*vm.OrdinaryContinuation)
		if !ok || cc.Data.NumArgs != 2 {
			t.Fatalf("expected handler to save current continuation with 2 retvals, got %#v", data.Save.C[0])
		}
	})

	t.Run("UnderflowDoesNotPopContinuations", func(t *testing.T) {
		state := newTestState()
		oldC2 := &testContinuation{name: "old_c2"}
		state.Reg.C[2] = oldC2

		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push body: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "handler"}); err != nil {
			t.Fatalf("push handler: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, TRYARGS(2, 1).Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("TRYARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
		if state.Reg.C[2] != oldC2 {
			t.Fatalf("TRYARGS underflow mutated c2")
		}
	})
}

func TestTryAdditionalErrorStackEffects(t *testing.T) {
	t.Run("BadHandlerConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push body: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad handler: %v", err)
		}

		assertVMErrCode(t, TRY().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected only bad handler to be consumed, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop body: %v", err)
		}
		if continuationName(t, cont) != "body" {
			t.Fatalf("unexpected body continuation")
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("BadBodyConsumesHandlerAndBadBody", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad body: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "handler"}); err != nil {
			t.Fatalf("push handler: %v", err)
		}

		assertVMErrCode(t, TRY().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected handler and bad body to be consumed, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("TryArgsExtractCurrentContinuationOutOfGas", func(t *testing.T) {
		state := newTestState()
		state.Gas = vm.Gas{}
		oldC0 := state.Reg.C[0]
		oldC2 := &testContinuation{name: "old_c2"}
		state.Reg.C[2] = oldC2

		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		pushTestInts(t, state.Stack, 100, 33)
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push body: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "handler"}); err != nil {
			t.Fatalf("push handler: %v", err)
		}

		assertVMErrCode(t, TRYARGS(33, 0).Interpret(state), vmerr.CodeOutOfGas)
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("TRYARGS out-of-gas mutated c0")
		}
		if state.Reg.C[2] != oldC2 {
			t.Fatalf("TRYARGS out-of-gas mutated c2")
		}
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only residual stack after out-of-gas, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 99 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})
}

func TestJumpXVarArgsErrorStackEffects(t *testing.T) {
	t.Run("BadParamsRangeConsumesParamOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push bad params: %v", err)
		}

		assertVMErrCode(t, JMPXVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected params to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("ShortDynamicArgsConsumesParamOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push params: %v", err)
		}

		assertVMErrCode(t, JMPXVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected params to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})
}

func TestCallXVarArgsErrorStackEffects(t *testing.T) {
	t.Run("BadRetvalsRangeConsumesRetvalsOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push bad retvals: %v", err)
		}

		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 3 {
			t.Fatalf("expected retvals to be consumed only, stack len=%d", state.Stack.Len())
		}
		params, err := state.Stack.PopIntFinite()
		if err != nil || params.Int64() != 0 {
			t.Fatalf("unexpected remaining params: %v err=%v", params, err)
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("BadParamsRangeConsumesRetvalsAndParams", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push bad params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected retvals and params to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("ShortDynamicArgsConsumesCountsAndContinuation", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only original arg to remain, stack len=%d", state.Stack.Len())
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})
}

func TestCallCCVarArgsErrorStackEffects(t *testing.T) {
	t.Run("BadRetvalsRangeConsumesRetvalsOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push bad retvals: %v", err)
		}

		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 3 {
			t.Fatalf("expected retvals to be consumed only, stack len=%d", state.Stack.Len())
		}
		params, err := state.Stack.PopIntFinite()
		if err != nil || params.Int64() != 0 {
			t.Fatalf("unexpected remaining params: %v err=%v", params, err)
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("BadParamsRangeConsumesRetvalsAndParams", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(255)); err != nil {
			t.Fatalf("push bad params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected retvals and params to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("ShortDynamicArgsKeepsContinuation", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected continuation and arg to remain, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})
}

func TestCallContinuationAdditionalErrorStackEffects(t *testing.T) {
	t.Run("CallCCBadContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}

		assertVMErrCode(t, CALLCC().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad continuation to be consumed only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("CallCCArgsBadContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}

		assertVMErrCode(t, CALLCCARGS(1, 0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad continuation to be consumed only, stack len=%d", state.Stack.Len())
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("CallCCArgsExtractCurrentContinuationOutOfGas", func(t *testing.T) {
		state := newTestState()
		state.Gas = vm.Gas{}
		oldC0 := state.Reg.C[0]

		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		pushTestInts(t, state.Stack, 100, 33)
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target: %v", err)
		}

		assertVMErrCode(t, CALLCCARGS(33, 0).Interpret(state), vmerr.CodeOutOfGas)
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("CALLCCARGS out-of-gas mutated c0")
		}
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only residual stack after out-of-gas, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 99 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("CallXVarArgsShortStackPreservesValues", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push first: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push second: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("CALLXVARARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("CallXVarArgsBadContinuationConsumesCountsAndBadContinuation", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLXVARARGS().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected counts and bad continuation to be consumed, stack len=%d", state.Stack.Len())
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("CallCCVarArgsShortStackPreservesValues", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push first: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push second: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("CALLCCVARARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("CallCCVarArgsBadContinuationConsumesCountsAndBadContinuation", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected counts and bad continuation to be consumed, stack len=%d", state.Stack.Len())
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("CallCCVarArgsExtractCurrentContinuationOutOfGas", func(t *testing.T) {
		state := newTestState()
		state.Gas = vm.Gas{}
		oldC0 := state.Reg.C[0]

		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		pushTestInts(t, state.Stack, 100, 33)
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(33)); err != nil {
			t.Fatalf("push params: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push retvals: %v", err)
		}

		assertVMErrCode(t, CALLCCVARARGS().Interpret(state), vmerr.CodeOutOfGas)
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("CALLCCVARARGS out-of-gas mutated c0")
		}
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only residual stack after out-of-gas, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 99 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("CallCCArgsRejectsTruncatedSuffix", func(t *testing.T) {
		truncated := CALLCCARGS(0, 0)
		code := cell.BeginCell().MustStoreSlice(truncated.BitPrefix.Data, truncated.BitPrefix.Bits).EndCell()
		if err := truncated.DeserializeMatched(code.MustBeginParse()); err == nil {
			t.Fatal("expected CALLCCARGS truncated suffix to fail")
		}
	})
}

func TestSetContVarArgsErrorStackEffects(t *testing.T) {
	t.Run("ShortStackPreservesValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(300)); err != nil {
			t.Fatalf("push lone value: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, SETCONTVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("SETCONTVARARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("BadMoreRangeConsumesMoreOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad more: %v", err)
		}

		assertVMErrCode(t, SETCONTVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected only more to be consumed, stack len=%d", state.Stack.Len())
		}
		copyCount, err := state.Stack.PopIntFinite()
		if err != nil || copyCount.Sign() != 0 {
			t.Fatalf("unexpected remaining copy count: %v err=%v", copyCount, err)
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
	})

	t.Run("BadCopyCountRangeConsumesCounts", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, SETCONTVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected counts to be consumed, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
	})

	t.Run("ShortDynamicArgsConsumesCountsOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, SETCONTVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected counts to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
	})
}

func TestArgsBlessAdditionalStackAndDecodeEdges(t *testing.T) {
	t.Run("SetContArgsMovesIntoExistingClosureStack", func(t *testing.T) {
		state := newTestState()
		closureStack := vm.NewStack()
		if err := closureStack.PushInt(big.NewInt(100)); err != nil {
			t.Fatalf("push closure value: %v", err)
		}
		target := &testContinuation{
			name: "target",
			data: &vm.ControlData{
				Stack:   closureStack,
				NumArgs: 3,
				CP:      vm.CP,
			},
		}

		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push copied arg: %v", err)
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := SETCONTARGS(1, -1).Interpret(state); err != nil {
			t.Fatalf("setcontargs failed: %v", err)
		}

		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		data := cont.GetControlData()
		if data == nil || data.Stack == nil || data.NumArgs != 2 {
			t.Fatalf("unexpected closure data: %#v", data)
		}
		arg, err := data.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected moved arg: %v err=%v", arg, err)
		}
		arg, err = data.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 100 {
			t.Fatalf("unexpected original closure arg: %v err=%v", arg, err)
		}
	})

	t.Run("SetContArgsMoreBelowExistingNumArgsInvalidatesArity", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{
			name: "target",
			data: &vm.ControlData{
				NumArgs: 2,
				CP:      vm.CP,
			},
		}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := SETCONTARGS(0, 1).Interpret(state); err != nil {
			t.Fatalf("setcontargs failed: %v", err)
		}

		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if got := cont.GetControlData().NumArgs; got != invalidContinuationArgs {
			t.Fatalf("expected invalid arity marker, got %d", got)
		}
	})

	t.Run("ReturnArgsMovesResidualIntoExistingClosureStack", func(t *testing.T) {
		state := newTestState()
		closureStack := vm.NewStack()
		if err := closureStack.PushInt(big.NewInt(100)); err != nil {
			t.Fatalf("push closure value: %v", err)
		}
		state.Reg.C[0] = &testContinuation{
			name: "ret",
			data: &vm.ControlData{
				Stack:   closureStack,
				NumArgs: 3,
				CP:      vm.CP,
			},
		}

		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push return value: %v", err)
		}
		if err := RETURNARGS(1).Interpret(state); err != nil {
			t.Fatalf("returnargs failed: %v", err)
		}

		if state.Stack.Len() != 1 {
			t.Fatalf("expected one return value, stack len=%d", state.Stack.Len())
		}
		ret, err := state.Stack.PopIntFinite()
		if err != nil || ret.Int64() != 22 {
			t.Fatalf("unexpected return value: %v err=%v", ret, err)
		}

		data := state.Reg.C[0].GetControlData()
		if data == nil || data.Stack == nil || data.NumArgs != 2 {
			t.Fatalf("unexpected c0 closure data: %#v", data)
		}
		arg, err := data.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected moved residual: %v err=%v", arg, err)
		}
		arg, err = data.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 100 {
			t.Fatalf("unexpected original closure arg: %v err=%v", arg, err)
		}
	})

	t.Run("BlessAndBlessArgsBadCodeConsumeCodeOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push bad code: %v", err)
		}

		assertVMErrCode(t, BLESS().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected BLESS bad code to consume code only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual after BLESS: %v err=%v", residual, err)
		}

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push bad code: %v", err)
		}

		assertVMErrCode(t, BLESSARGS(0, 0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected BLESSARGS bad code to consume code only, stack len=%d", state.Stack.Len())
		}
		residual, err = state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual after BLESSARGS: %v err=%v", residual, err)
		}
	})

	t.Run("BlessVarArgsShortStackPreservesValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(300)); err != nil {
			t.Fatalf("push lone value: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, BLESSVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("BLESSVARARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("FixedArgFormsRejectTruncatedSuffix", func(t *testing.T) {
		for _, op := range []*helpers.AdvancedOP{
			SETCONTARGS(0, 0),
			RETURNARGS(0),
			BLESSARGS(0, 0),
		} {
			code := cell.BeginCell().MustStoreSlice(op.BitPrefix.Data, op.BitPrefix.Bits).EndCell()
			if err := op.DeserializeMatched(code.MustBeginParse()); err == nil {
				t.Fatalf("expected %s truncated suffix to fail", op.SerializeText())
			}
		}
	})
}

func TestArgsBlessStackGasOutOfGas(t *testing.T) {
	t.Run("SetContVarArgsCopyStackOutOfGas", func(t *testing.T) {
		state := newTestState()
		state.Gas = vm.Gas{}

		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		pushTestInts(t, state.Stack, 100, 33)
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(33)); err != nil {
			t.Fatalf("push copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, SETCONTVARARGS().Interpret(state), vmerr.CodeOutOfGas)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only residual stack after out-of-gas, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 99 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("ReturnArgsCopyStackOutOfGas", func(t *testing.T) {
		state := newTestState()
		state.Gas = vm.Gas{}
		oldC0 := &testContinuation{name: "ret"}
		state.Reg.C[0] = oldC0

		pushTestInts(t, state.Stack, 100, 33)
		if err := state.Stack.PushInt(big.NewInt(777)); err != nil {
			t.Fatalf("push return value: %v", err)
		}

		assertVMErrCode(t, RETURNARGS(1).Interpret(state), vmerr.CodeOutOfGas)
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("RETURNARGS out-of-gas mutated c0")
		}
		if state.Stack.Len() != 1 {
			t.Fatalf("expected return values stack after out-of-gas, stack len=%d", state.Stack.Len())
		}
		ret, err := state.Stack.PopIntFinite()
		if err != nil || ret.Int64() != 777 {
			t.Fatalf("unexpected return value: %v err=%v", ret, err)
		}
	})

	t.Run("BlessVarArgsCopyStackOutOfGas", func(t *testing.T) {
		state := newTestState()
		state.Gas = vm.Gas{}
		code := cell.BeginCell().EndCell().MustBeginParse()

		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		pushTestInts(t, state.Stack, 100, 33)
		if err := state.Stack.PushSlice(code); err != nil {
			t.Fatalf("push code: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(33)); err != nil {
			t.Fatalf("push copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, BLESSVARARGS().Interpret(state), vmerr.CodeOutOfGas)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only residual stack after out-of-gas, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 99 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})
}

func TestSetNumVarArgsStackEffects(t *testing.T) {
	t.Run("ShortStackPreservesValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(300)); err != nil {
			t.Fatalf("push lone value: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, SETNUMVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("SETNUMVARARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("BadMoreRangeConsumesMoreOnly", func(t *testing.T) {
		state := newTestState()
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad more: %v", err)
		}

		assertVMErrCode(t, SETNUMVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only more to be consumed, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
	})

	t.Run("BadContinuationConsumesMoreAndValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, SETNUMVARARGS().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected more and bad continuation to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("MinusOneDoesNotForceControlData", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		if err := SETNUMVARARGS().Interpret(state); err != nil {
			t.Fatalf("setnumvarargs failed: %v", err)
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
		if cont.GetControlData() != nil {
			t.Fatal("expected more=-1 to leave continuation control data unchanged")
		}
	})
}

func TestBlessVarArgsErrorStackEffects(t *testing.T) {
	t.Run("BadMoreRangeConsumesMoreOnly", func(t *testing.T) {
		state := newTestState()
		code := cell.BeginCell().EndCell().MustBeginParse()
		if err := state.Stack.PushSlice(code); err != nil {
			t.Fatalf("push code: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad more: %v", err)
		}

		assertVMErrCode(t, BLESSVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected only more to be consumed, stack len=%d", state.Stack.Len())
		}
		copyCount, err := state.Stack.PopIntFinite()
		if err != nil || copyCount.Sign() != 0 {
			t.Fatalf("unexpected remaining copy count: %v err=%v", copyCount, err)
		}
		if _, err = state.Stack.PopSlice(); err != nil {
			t.Fatalf("pop code: %v", err)
		}
	})

	t.Run("BadCopyCountRangeConsumesCounts", func(t *testing.T) {
		state := newTestState()
		code := cell.BeginCell().EndCell().MustBeginParse()
		if err := state.Stack.PushSlice(code); err != nil {
			t.Fatalf("push code: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, BLESSVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected counts to be consumed, stack len=%d", state.Stack.Len())
		}
		if _, err := state.Stack.PopSlice(); err != nil {
			t.Fatalf("pop code: %v", err)
		}
	})

	t.Run("ShortDynamicArgsConsumesCountsOnly", func(t *testing.T) {
		state := newTestState()
		code := cell.BeginCell().EndCell().MustBeginParse()
		if err := state.Stack.PushSlice(code); err != nil {
			t.Fatalf("push code: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push copy count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push more: %v", err)
		}

		assertVMErrCode(t, BLESSVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected counts to be consumed only, stack len=%d", state.Stack.Len())
		}
		if _, err := state.Stack.PopSlice(); err != nil {
			t.Fatalf("pop code: %v", err)
		}
	})
}

func TestReturnArgsErrorStackEffects(t *testing.T) {
	t.Run("ReturnVarArgsBadCountConsumesCountOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad count: %v", err)
		}

		assertVMErrCode(t, RETURNVARARGS().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected count to be consumed only, stack len=%d", state.Stack.Len())
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("ReturnVarArgsShortCountConsumesCountOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push count: %v", err)
		}

		assertVMErrCode(t, RETURNVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected count to be consumed only, stack len=%d", state.Stack.Len())
		}
		arg, err := state.Stack.PopIntFinite()
		if err != nil || arg.Int64() != 11 {
			t.Fatalf("unexpected remaining arg: %v err=%v", arg, err)
		}
	})

	t.Run("ReturnArgsShortFixedCountPreservesStack", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push arg: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, RETURNARGS(2).Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("RETURNARGS underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("ReturnArgsExactCountNoOp", func(t *testing.T) {
		state := newTestState()
		oldC0 := &testContinuation{name: "ret"}
		state.Reg.C[0] = oldC0
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push first: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push second: %v", err)
		}

		before := state.Stack.String()
		if err := RETURNARGS(2).Interpret(state); err != nil {
			t.Fatalf("returnargs exact count failed: %v", err)
		}
		if after := state.Stack.String(); after != before {
			t.Fatalf("RETURNARGS exact count mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("RETURNARGS exact count mutated c0")
		}
	})

	t.Run("ReturnArgsClosureOverflowKeepsReturnValuesStack", func(t *testing.T) {
		state := newTestState()
		oldC0 := &testContinuation{
			name: "ret",
			data: &vm.ControlData{NumArgs: 0, CP: vm.CP},
		}
		state.Reg.C[0] = oldC0
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push return value: %v", err)
		}

		assertVMErrCode(t, RETURNARGS(1).Interpret(state), vmerr.CodeStackOverflow)
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("RETURNARGS overflow mutated c0")
		}
		if state.Stack.Len() != 1 {
			t.Fatalf("expected return values stack after overflow, stack len=%d", state.Stack.Len())
		}
		ret, err := state.Stack.PopIntFinite()
		if err != nil || ret.Int64() != 22 {
			t.Fatalf("unexpected remaining return value: %v err=%v", ret, err)
		}
	})
}

func TestRefCodeMissingReferenceStackEffects(t *testing.T) {
	t.Run("JmpRefDataPreservesStack", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push value: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, JMPREFDATA(nil).Interpret(state), vmerr.CodeInvalidOpcode)
		if after := state.Stack.String(); after != before {
			t.Fatalf("missing JMPREFDATA ref mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("ConditionalRefPreservesCondition", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push condition: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, IFREF(nil).Interpret(state), vmerr.CodeInvalidOpcode)
		if after := state.Stack.String(); after != before {
			t.Fatalf("missing IFREF ref mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("IfRefElsePreservesStack", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push condition: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "alt"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, IFREFELSE(nil).Interpret(state), vmerr.CodeInvalidOpcode)
		if after := state.Stack.String(); after != before {
			t.Fatalf("missing IFREFELSE ref mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("IfRefElseRefRequiresBothRefsBeforeCondition", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushBool(true); err != nil {
			t.Fatalf("push condition: %v", err)
		}

		before := state.Stack.String()
		ref := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		assertVMErrCode(t, IFREFELSEREF(ref, nil).Interpret(state), vmerr.CodeInvalidOpcode)
		if after := state.Stack.String(); after != before {
			t.Fatalf("missing IFREFELSEREF ref mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})
}

func TestJmpRefDataCurrentCodeNilPreservesStack(t *testing.T) {
	state := newTestState()
	state.CurrentCode = nil
	if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
		t.Fatalf("push value: %v", err)
	}

	before := state.Stack.String()
	ref := cell.BeginCell().EndCell()
	assertVMErrCode(t, JMPREFDATA(ref).Interpret(state), vmerr.CodeTypeCheck)
	if after := state.Stack.String(); after != before {
		t.Fatalf("JMPREFDATA nil current code mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestJmpRefDataBadRefDoesNotPushCurrentCode(t *testing.T) {
	state := newTestState()
	state.InitForExecution()
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
		t.Fatalf("push residual: %v", err)
	}

	loads := 0
	badRef := unresolvedLibraryCell(t).WithTrace(cell.NewTrace(cell.TraceHooks{
		OnLoad: func(*cell.Cell) { loads++ },
	}))
	gasBefore := state.Gas.Used()
	assertVMErrCode(t, JMPREFDATA(badRef).Interpret(state), vmerr.CodeCellUnderflow)
	if state.Stack.Len() != 1 {
		t.Fatalf("JMPREFDATA pushed current code before loading bad ref, stack len=%d", state.Stack.Len())
	}
	residual, err := state.Stack.PopIntFinite()
	if err != nil || residual.Int64() != 11 {
		t.Fatalf("unexpected residual: %v err=%v", residual, err)
	}
	if loads != 1 {
		t.Fatalf("bad continuation load trace count = %d, want 1", loads)
	}
	if gas := state.Gas.Used() - gasBefore; gas != vm.CellLoadGasPrice {
		t.Fatalf("bad continuation load gas = %d, want %d", gas, vm.CellLoadGasPrice)
	}
}

func unresolvedLibraryCell(t *testing.T) *cell.Cell {
	t.Helper()

	lib, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("build unresolved library cell: %v", err)
	}
	return lib
}

func TestCallRefAndJmpRefEdgeStackEffects(t *testing.T) {
	cases := []struct {
		name string
		op   vm.OP
		code int64
	}{
		{name: "CALLREF", op: CALLREF(nil), code: vmerr.CodeInvalidOpcode},
		{name: "JMPREF", op: JMPREF(nil), code: vmerr.CodeInvalidOpcode},
		{name: "CALLREFUnresolvedLibrary", op: CALLREF(unresolvedLibraryCell(t)), code: vmerr.CodeCellUnderflow},
		{name: "JMPREFUnresolvedLibrary", op: JMPREF(unresolvedLibraryCell(t)), code: vmerr.CodeCellUnderflow},
	}

	for _, tc := range cases {
		t.Run(tc.name+"PreservesStack", func(t *testing.T) {
			state := newTestState()
			state.InitForExecution()
			if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
				t.Fatalf("push residual: %v", err)
			}

			before := state.Stack.String()
			assertVMErrCode(t, tc.op.Interpret(state), tc.code)
			if after := state.Stack.String(); after != before {
				t.Fatalf("%s error mutated stack:\nbefore:\n%s\nafter:\n%s", tc.name, before, after)
			}
		})
	}
}

func TestConditionalRefStackEffects(t *testing.T) {
	ref := cell.BeginCell().EndCell()
	cases := []struct {
		name     string
		op       vm.OP
		skipCond bool
	}{
		{name: "IFREF", op: IFREF(ref), skipCond: false},
		{name: "IFNOTREF", op: IFNOTREF(ref), skipCond: true},
		{name: "IFJMPREF", op: IFJMPREF(ref), skipCond: false},
		{name: "IFNOTJMPREF", op: IFNOTJMPREF(ref), skipCond: true},
	}

	for _, tc := range cases {
		t.Run(tc.name+"BadConditionConsumesTop", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
				t.Fatalf("push bad condition: %v", err)
			}

			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 0 {
				t.Fatalf("expected bad condition to be consumed, stack len=%d", state.Stack.Len())
			}
		})

		t.Run(tc.name+"SkipBranchConsumesConditionOnly", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
				t.Fatalf("push residual: %v", err)
			}
			if err := state.Stack.PushBool(tc.skipCond); err != nil {
				t.Fatalf("push condition: %v", err)
			}

			if err := tc.op.Interpret(state); err != nil {
				t.Fatalf("%s skip branch failed: %v", tc.name, err)
			}
			if state.Stack.Len() != 1 {
				t.Fatalf("expected only residual value to remain, stack len=%d", state.Stack.Len())
			}
			residual, err := state.Stack.PopIntFinite()
			if err != nil || residual.Int64() != 11 {
				t.Fatalf("unexpected residual: %v err=%v", residual, err)
			}
		})
	}

	t.Run("TakenBranchBadRefConsumesConditionOnly", func(t *testing.T) {
		badRef := unresolvedLibraryCell(t)
		for _, tc := range []struct {
			name string
			op   vm.OP
			cond bool
		}{
			{name: "IFREF", op: IFREF(badRef), cond: true},
			{name: "IFNOTREF", op: IFNOTREF(badRef), cond: false},
			{name: "IFJMPREF", op: IFJMPREF(badRef), cond: true},
			{name: "IFNOTJMPREF", op: IFNOTJMPREF(badRef), cond: false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				state := newTestState()
				state.InitForExecution()
				if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
					t.Fatalf("push residual: %v", err)
				}
				if err := state.Stack.PushBool(tc.cond); err != nil {
					t.Fatalf("push condition: %v", err)
				}

				assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeCellUnderflow)
				if state.Stack.Len() != 1 {
					t.Fatalf("expected only residual after %s bad ref, stack len=%d", tc.name, state.Stack.Len())
				}
				residual, err := state.Stack.PopIntFinite()
				if err != nil || residual.Int64() != 11 {
					t.Fatalf("unexpected residual: %v err=%v", residual, err)
				}
			})
		}
	})

	t.Run("ElseStyleTakenBranchBadRefConsumesOperands", func(t *testing.T) {
		badRef := unresolvedLibraryCell(t)
		for _, tc := range []struct {
			name string
			op   vm.OP
			cond bool
		}{
			{name: "IFREFELSE", op: IFREFELSE(badRef), cond: true},
			{name: "IFELSEREF", op: IFELSEREF(badRef), cond: false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				state := newTestState()
				state.InitForExecution()
				if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
					t.Fatalf("push residual: %v", err)
				}
				if err := state.Stack.PushBool(tc.cond); err != nil {
					t.Fatalf("push condition: %v", err)
				}
				if err := state.Stack.PushContinuation(&testContinuation{name: "alt"}); err != nil {
					t.Fatalf("push alt: %v", err)
				}

				assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeCellUnderflow)
				if state.Stack.Len() != 1 {
					t.Fatalf("expected only residual after %s bad ref, stack len=%d", tc.name, state.Stack.Len())
				}
				residual, err := state.Stack.PopIntFinite()
				if err != nil || residual.Int64() != 11 {
					t.Fatalf("unexpected residual: %v err=%v", residual, err)
				}
			})
		}
	})

	t.Run("IfRefElseRefBadConditionConsumesCondition", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad condition: %v", err)
		}

		assertVMErrCode(t, IFREFELSEREF(ref, ref).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad condition to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("IfRefElseRefBadFalseRefConsumesConditionOnly", func(t *testing.T) {
		state := newTestState()
		state.InitForExecution()
		goodRef := cell.BeginCell().EndCell()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push condition: %v", err)
		}

		assertVMErrCode(t, IFREFELSEREF(goodRef, unresolvedLibraryCell(t)).Interpret(state), vmerr.CodeCellUnderflow)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only residual after IFREFELSEREF bad false ref, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})
}

func TestDictIGetJmpZErrorStackEffects(t *testing.T) {
	t.Run("BadBitsConsumesBitsOnly", func(t *testing.T) {
		state := newTestState()
		root := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushCell(root); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1024)); err != nil {
			t.Fatalf("push bits: %v", err)
		}

		assertVMErrCode(t, DICTIGETJMPZ().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected only bits to be consumed, stack len=%d", state.Stack.Len())
		}
		gotRoot, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop root: %v", err)
		}
		if string(gotRoot.Hash()) != string(root.Hash()) {
			t.Fatal("unexpected remaining root cell")
		}
		key, err := state.Stack.PopIntFinite()
		if err != nil || key.Int64() != 11 {
			t.Fatalf("unexpected remaining key: %v err=%v", key, err)
		}
	})

	t.Run("BadRootConsumesBitsAndRoot", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatalf("push bad root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push bits: %v", err)
		}

		assertVMErrCode(t, DICTIGETJMPZ().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bits and root to be consumed, stack len=%d", state.Stack.Len())
		}
		key, err := state.Stack.PopIntFinite()
		if err != nil || key.Int64() != 11 {
			t.Fatalf("unexpected remaining key: %v err=%v", key, err)
		}
	})

	t.Run("BadKeyConsumesAllInputs", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().MustStoreUInt(0x11, 8).EndCell()); err != nil {
			t.Fatalf("push bad key: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push bits: %v", err)
		}

		assertVMErrCode(t, DICTIGETJMPZ().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected all inputs to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("MalformedDictConsumesAllInputs", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push key: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push malformed root: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push bits: %v", err)
		}

		if err := DICTIGETJMPZ().Interpret(state); err == nil {
			t.Fatal("expected malformed dictionary to fail")
		}
		if state.Stack.Len() != 0 {
			t.Fatalf("expected malformed dictionary error to consume all inputs, stack len=%d", state.Stack.Len())
		}
	})
}

func TestConditionalContinuationStackEffects(t *testing.T) {
	cases := []struct {
		name string
		op   vm.OP
	}{
		{name: "IF", op: IF()},
		{name: "IFNOT", op: IFNOT()},
		{name: "IFJMP", op: IFJMP()},
		{name: "IFNOTJMP", op: IFNOTJMP()},
	}

	for _, tc := range cases {
		t.Run(tc.name+"ShortStackPreservesValue", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushBool(true); err != nil {
				t.Fatalf("push condition: %v", err)
			}

			before := state.Stack.String()
			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeStackUnderflow)
			if after := state.Stack.String(); after != before {
				t.Fatalf("%s underflow mutated stack:\nbefore:\n%s\nafter:\n%s", tc.name, before, after)
			}
		})

		t.Run(tc.name+"BadContinuationConsumesTopOnly", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushBool(false); err != nil {
				t.Fatalf("push condition: %v", err)
			}
			if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
				t.Fatalf("push bad continuation: %v", err)
			}

			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 1 {
				t.Fatalf("expected bad continuation only to be consumed, stack len=%d", state.Stack.Len())
			}
			cond, err := state.Stack.PopBool()
			if err != nil || cond {
				t.Fatalf("unexpected remaining condition: %v err=%v", cond, err)
			}
		})

		t.Run(tc.name+"BadConditionConsumesBothInputs", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
				t.Fatalf("push bad condition: %v", err)
			}
			if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
				t.Fatalf("push continuation: %v", err)
			}

			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 0 {
				t.Fatalf("expected condition and continuation to be consumed, stack len=%d", state.Stack.Len())
			}
		})
	}
}

func TestIfElseStackEffects(t *testing.T) {
	t.Run("ShortStackPreservesValues", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushBool(true); err != nil {
			t.Fatalf("push condition: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "true"}); err != nil {
			t.Fatalf("push true continuation: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, IFELSE().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("IFELSE underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("BadTopContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushBool(true); err != nil {
			t.Fatalf("push condition: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "true"}); err != nil {
			t.Fatalf("push true continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("push bad false continuation: %v", err)
		}

		assertVMErrCode(t, IFELSE().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected only top continuation to be consumed, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop true continuation: %v", err)
		}
		if continuationName(t, cont) != "true" {
			t.Fatalf("unexpected remaining continuation")
		}
		cond, err := state.Stack.PopBool()
		if err != nil || !cond {
			t.Fatalf("unexpected remaining condition: %v err=%v", cond, err)
		}
	})

	t.Run("BadSecondContinuationConsumesTopTwo", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push condition: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
			t.Fatalf("push bad true continuation: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "false"}); err != nil {
			t.Fatalf("push false continuation: %v", err)
		}

		assertVMErrCode(t, IFELSE().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected continuations to be consumed, stack len=%d", state.Stack.Len())
		}
		cond, err := state.Stack.PopBool()
		if err != nil || cond {
			t.Fatalf("unexpected remaining condition: %v err=%v", cond, err)
		}
	})

	t.Run("BadConditionConsumesAllInputs", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad condition: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "true"}); err != nil {
			t.Fatalf("push true continuation: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "false"}); err != nil {
			t.Fatalf("push false continuation: %v", err)
		}

		assertVMErrCode(t, IFELSE().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected all inputs to be consumed, stack len=%d", state.Stack.Len())
		}
	})
}

func TestIfRefElseStackEffects(t *testing.T) {
	ref := cell.BeginCell().EndCell()
	cases := []struct {
		name string
		op   vm.OP
	}{
		{name: "IFREFELSE", op: IFREFELSE(ref)},
		{name: "IFELSEREF", op: IFELSEREF(ref)},
	}

	for _, tc := range cases {
		t.Run(tc.name+"ShortStackPreservesCondition", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushBool(true); err != nil {
				t.Fatalf("push condition: %v", err)
			}

			before := state.Stack.String()
			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeStackUnderflow)
			if after := state.Stack.String(); after != before {
				t.Fatalf("%s underflow mutated stack:\nbefore:\n%s\nafter:\n%s", tc.name, before, after)
			}
		})

		t.Run(tc.name+"BadContinuationConsumesTopOnly", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushBool(true); err != nil {
				t.Fatalf("push condition: %v", err)
			}
			if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
				t.Fatalf("push bad continuation: %v", err)
			}

			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 1 {
				t.Fatalf("expected bad continuation only to be consumed, stack len=%d", state.Stack.Len())
			}
			cond, err := state.Stack.PopBool()
			if err != nil || !cond {
				t.Fatalf("unexpected remaining condition: %v err=%v", cond, err)
			}
		})

		t.Run(tc.name+"BadConditionConsumesBothInputs", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
				t.Fatalf("push bad condition: %v", err)
			}
			if err := state.Stack.PushContinuation(&testContinuation{name: "alt"}); err != nil {
				t.Fatalf("push continuation: %v", err)
			}

			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 0 {
				t.Fatalf("expected condition and continuation to be consumed, stack len=%d", state.Stack.Len())
			}
		})
	}
}

func TestSetContCtrAdditionalCoverage(t *testing.T) {
	t.Run("RoundTripAndTupleSave", func(t *testing.T) {
		src := SETCONTCTR(7)
		dst := SETCONTCTR(0)
		if err := dst.Deserialize(src.Serialize().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}
		if got := dst.SerializeText(); got != "c7 SETCONTCTR" {
			t.Fatalf("unexpected text: %q", got)
		}
		if got := dst.InstructionBits(); got != 16 {
			t.Fatalf("unexpected bits: %d", got)
		}

		state := newTestState()
		if err := state.Stack.PushTuple(tuple.NewTupleValue(big.NewInt(7))); err != nil {
			t.Fatalf("push tuple: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		if err := dst.Interpret(state); err != nil {
			t.Fatalf("setcontctr failed: %v", err)
		}
		gotCont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop continuation: %v", err)
		}
		if gotCont.GetControlData().Save.C7.Len() != 1 {
			t.Fatalf("expected c7 to be saved in continuation")
		}
	})

	t.Run("TypeAndRangeChecks", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push cell: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		assertVMErrCode(t, SETCONTCTR(0).Interpret(state), vmerr.CodeTypeCheck)

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
			t.Fatalf("push idx: %v", err)
		}
		assertVMErrCode(t, PUSHCTRX().Interpret(state), vmerr.CodeRangeCheck)

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
			t.Fatalf("push idx: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeStackUnderflow)

		state = newTestState()
		c4Val := cell.BeginCell().MustStoreUInt(0xC4, 8).EndCell()
		if err := state.Stack.PushCell(c4Val); err != nil {
			t.Fatalf("push c4 value: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
			t.Fatalf("push invalid idx: %v", err)
		}
		assertVMErrCode(t, POPCTRX().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected POPCTRX invalid idx to consume idx only, stack len=%d", state.Stack.Len())
		}
		gotCell, err := state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop c4 value: %v", err)
		}
		if string(gotCell.Hash()) != string(c4Val.Hash()) {
			t.Fatalf("unexpected remaining c4 value")
		}

		state = newTestState()
		if err := state.Stack.PushCell(c4Val); err != nil {
			t.Fatalf("push c0 bad value: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push c0 idx: %v", err)
		}
		assertVMErrCode(t, POPCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected POPCTRX c0 type check to consume value and idx, stack len=%d", state.Stack.Len())
		}

		state = newTestState()
		if err := state.Stack.PushCell(c4Val); err != nil {
			t.Fatalf("push c4 value: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(6)); err != nil {
			t.Fatalf("push invalid idx: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected SETCONTCTRX invalid idx to consume idx only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target continuation: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected remaining target continuation")
		}
		gotCell, err = state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop c4 value: %v", err)
		}
		if string(gotCell.Hash()) != string(c4Val.Hash()) {
			t.Fatalf("unexpected remaining c4 value")
		}

		state = newTestState()
		if err := state.Stack.PushCell(c4Val); err != nil {
			t.Fatalf("push c4 value: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push c4 idx: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected SETCONTCTRX bad continuation to consume idx and bad continuation, stack len=%d", state.Stack.Len())
		}
		gotCell, err = state.Stack.PopCell()
		if err != nil {
			t.Fatalf("pop c4 value: %v", err)
		}
		if string(gotCell.Hash()) != string(c4Val.Hash()) {
			t.Fatalf("unexpected remaining c4 value")
		}

		state = newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad c4 value: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push target continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
			t.Fatalf("push c4 idx: %v", err)
		}
		assertVMErrCode(t, SETCONTCTRX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected SETCONTCTRX bad saved value to consume all inputs, stack len=%d", state.Stack.Len())
		}
	})
}

func TestSetContCtrManyXStackEffects(t *testing.T) {
	t.Run("FixedFormRejectsTruncatedSuffix", func(t *testing.T) {
		op := SETCONTCTRMANY(0)
		if err := op.Deserialize(op.GetPrefixes()[0]); err == nil {
			t.Fatal("expected truncated SETCONTCTRMANY suffix to fail")
		}
	})

	t.Run("ShortStackPreservesMask", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 9
		if err := state.Stack.PushInt(big.NewInt(64)); err != nil {
			t.Fatalf("push lone mask: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, SETCONTCTRMANYX().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("SETCONTCTRMANYX underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("BadMaskRangeConsumesMaskOnly", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 9
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("push bad mask: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRMANYX().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only mask to be consumed, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
	})

	t.Run("C6MaskConsumesMaskOnly", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 9
		target := &testContinuation{name: "target"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1 << 6)); err != nil {
			t.Fatalf("push c6 mask: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRMANYX().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected only mask to be consumed, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop target: %v", err)
		}
		if continuationName(t, cont) != "target" {
			t.Fatalf("unexpected target continuation")
		}
	})

	t.Run("BadContinuationConsumesMaskAndValue", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 9
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push mask: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRMANYX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected mask and bad continuation to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("DuplicateSaveConsumesContinuation", func(t *testing.T) {
		state := newTestState()
		state.GlobalVersion = 9
		target := &testContinuation{
			name: "target",
			data: &vm.ControlData{
				NumArgs: vm.ControlDataAllArgs,
				CP:      vm.CP,
			},
		}
		target.data.Save.C[1] = &testContinuation{name: "saved"}
		if err := state.Stack.PushContinuation(target); err != nil {
			t.Fatalf("push target: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1 << 1)); err != nil {
			t.Fatalf("push duplicate mask: %v", err)
		}

		assertVMErrCode(t, SETCONTCTRMANYX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected continuation to be consumed on duplicate save, stack len=%d", state.Stack.Len())
		}
	})
}

func TestConditionalReturnStackEffects(t *testing.T) {
	cases := []struct {
		name     string
		op       vm.OP
		skipCond bool
	}{
		{name: "IFRET", op: IFRET(), skipCond: false},
		{name: "IFRETALT", op: IFRETALT(), skipCond: false},
		{name: "IFNOTRETALT", op: IFNOTRETALT(), skipCond: true},
		{name: "IFNOTRET", op: IFNOTRET(), skipCond: true},
	}

	for _, tc := range cases {
		t.Run(tc.name+"BadConditionConsumesTop", func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
				t.Fatalf("push bad condition: %v", err)
			}

			assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 0 {
				t.Fatalf("expected bad condition to be consumed, stack len=%d", state.Stack.Len())
			}
		})

		t.Run(tc.name+"SkipBranchConsumesConditionOnly", func(t *testing.T) {
			state := newTestState()
			retCalls := 0
			altCalls := 0
			state.Reg.C[0] = &testContinuation{name: "ret", onJump: func(*vm.State) (vm.Continuation, error) {
				retCalls++
				return nil, nil
			}}
			state.Reg.C[1] = &testContinuation{name: "alt", onJump: func(*vm.State) (vm.Continuation, error) {
				altCalls++
				return nil, nil
			}}
			if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
				t.Fatalf("push residual: %v", err)
			}
			if err := state.Stack.PushBool(tc.skipCond); err != nil {
				t.Fatalf("push condition: %v", err)
			}

			if err := tc.op.Interpret(state); err != nil {
				t.Fatalf("%s skip branch failed: %v", tc.name, err)
			}
			if retCalls != 0 || altCalls != 0 {
				t.Fatalf("expected skip branch not to return, ret=%d alt=%d", retCalls, altCalls)
			}
			if state.Stack.Len() != 1 {
				t.Fatalf("expected only residual value to remain, stack len=%d", state.Stack.Len())
			}
			residual, err := state.Stack.PopIntFinite()
			if err != nil || residual.Int64() != 11 {
				t.Fatalf("unexpected residual: %v err=%v", residual, err)
			}
		})
	}
}

func TestJumpAndReturnGuardCoverage(t *testing.T) {
	t.Run("ExecuteAndJmpxBadContinuationConsumeTop", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			op   vm.OP
		}{
			{name: "EXECUTE", op: EXECUTE()},
			{name: "JMPX", op: JMPX()},
		} {
			t.Run(tc.name, func(t *testing.T) {
				state := newTestState()
				if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
					t.Fatalf("push bad continuation: %v", err)
				}

				assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
				if state.Stack.Len() != 0 {
					t.Fatalf("%s bad continuation should consume top, stack len=%d", tc.name, state.Stack.Len())
				}
			})
		}
	})

	t.Run("JmpxdataErrors", func(t *testing.T) {
		assertVMErrCode(t, JMPXDATA().Interpret(newTestState()), vmerr.CodeStackUnderflow)

		state := newTestState()
		state.CurrentCode = nil
		if err := state.Stack.PushContinuation(&testContinuation{name: "target"}); err != nil {
			t.Fatalf("push continuation: %v", err)
		}
		assertVMErrCode(t, JMPXDATA().Interpret(state), vmerr.CodeTypeCheck)
	})

	t.Run("RetFamilyNoOpAndErrorBranches", func(t *testing.T) {
		state := newTestState()
		state.CurrentCode = nil
		assertVMErrCode(t, RETDATA().Interpret(state), vmerr.CodeTypeCheck)

		truncated := RETARGS(0)
		code := cell.BeginCell().MustStoreSlice(truncated.BitPrefix.Data, truncated.BitPrefix.Bits).EndCell()
		if err := truncated.DeserializeMatched(code.MustBeginParse()); err == nil {
			t.Fatal("expected RETARGS truncated suffix to fail")
		}

		state = newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad RETBOOL condition: %v", err)
		}
		assertVMErrCode(t, RETBOOL().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected RETBOOL bad condition to be consumed, stack len=%d", state.Stack.Len())
		}

		retCalls := 0
		state = newTestState()
		state.Reg.C[0] = &testContinuation{
			name: "ret",
			onJump: func(*vm.State) (vm.Continuation, error) {
				retCalls++
				return nil, nil
			},
		}
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push IFRET cond: %v", err)
		}
		if err := IFRET().Interpret(state); err != nil {
			t.Fatalf("ifret false branch failed: %v", err)
		}
		if retCalls != 0 {
			t.Fatalf("expected IFRET false branch to skip return, got %d calls", retCalls)
		}

		altCalls := 0
		state = newTestState()
		state.Reg.C[1] = &testContinuation{
			name: "alt",
			onJump: func(*vm.State) (vm.Continuation, error) {
				altCalls++
				return nil, nil
			},
		}
		if err := state.Stack.PushBool(false); err != nil {
			t.Fatalf("push IFRETALT cond: %v", err)
		}
		if err := IFRETALT().Interpret(state); err != nil {
			t.Fatalf("ifretalt false branch failed: %v", err)
		}
		if err := state.Stack.PushBool(true); err != nil {
			t.Fatalf("push IFNOTRETALT cond: %v", err)
		}
		if err := IFNOTRETALT().Interpret(state); err != nil {
			t.Fatalf("ifnotretalt true branch failed: %v", err)
		}
		if altCalls != 0 {
			t.Fatalf("expected alternate returns to be skipped, got %d calls", altCalls)
		}
	})
}

func TestCallJumpArgsDecodeAndContinuationErrors(t *testing.T) {
	t.Run("ShortAdvancedSuffixes", func(t *testing.T) {
		for _, op := range []*helpers.AdvancedOP{
			CALLXARGS(1, 2),
			CALLXARGSP(3),
			JMPXARGS(4),
		} {
			if err := op.DeserializeMatched(cell.BeginCell().MustStoreSlice(op.BitPrefix.Data, op.BitPrefix.Bits).EndCell().MustBeginParse()); err == nil {
				t.Fatalf("expected short suffix error for %s", op.SerializeText())
			}
		}
	})

	t.Run("BadContinuationConsumesTop", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			op   vm.OP
		}{
			{name: "CALLXARGS", op: CALLXARGS(0, 0)},
			{name: "CALLXARGSP", op: CALLXARGSP(0)},
			{name: "JMPXARGS", op: JMPXARGS(0)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				state := newTestState()
				if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
					t.Fatalf("push bad continuation: %v", err)
				}

				assertVMErrCode(t, tc.op.Interpret(state), vmerr.CodeTypeCheck)
				if state.Stack.Len() != 0 {
					t.Fatalf("%s bad continuation should consume top, stack len=%d", tc.name, state.Stack.Len())
				}
			})
		}
	})

	t.Run("JmpXVarArgsShortStackPreservesParam", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push param: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, JMPXVARARGS().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("JMPXVARARGS short-stack path mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("JmpXVarArgsBadContinuationConsumesParamAndValue", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push params: %v", err)
		}

		assertVMErrCode(t, JMPXVARARGS().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad continuation and params to be consumed, stack len=%d", state.Stack.Len())
		}
	})
}

func TestControlRegisterHelperBranches(t *testing.T) {
	t.Run("IndexValidation", func(t *testing.T) {
		valid := []int{0, 1, 2, 3, 4, 5, 7}
		for _, idx := range valid {
			if !validControlRegisterIndex(idx) {
				t.Fatalf("expected index %d to be valid", idx)
			}
		}
		for _, idx := range []int{-1, 6, 8} {
			if validControlRegisterIndex(idx) {
				t.Fatalf("expected index %d to be invalid", idx)
			}
		}
	})

	t.Run("CloneControlRegisterValue", func(t *testing.T) {
		origCont := &testContinuation{name: "orig"}
		clonedCont, ok := cloneControlRegisterValue(origCont).(vm.Continuation)
		if !ok {
			t.Fatalf("expected continuation clone")
		}
		if clonedCont == origCont {
			t.Fatal("expected continuation clone to be a copy")
		}

		origTuple := tuple.NewTupleValue(big.NewInt(9))
		clonedTuple, ok := cloneControlRegisterValue(origTuple).(tuple.Tuple)
		if !ok || clonedTuple.Len() != 1 {
			t.Fatalf("expected tuple clone")
		}

		var nilCont vm.Continuation = (*testContinuation)(nil)
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected typed nil continuation clone to panic")
				}
			}()
			_ = cloneControlRegisterValue(nilCont)
		}()

		val := big.NewInt(123)
		if got := cloneControlRegisterValue(val); got != val {
			t.Fatal("expected default clone branch to return original value")
		}
	})

	t.Run("PushCtrClonesContinuation", func(t *testing.T) {
		state := newTestState()
		state.Reg.C[1] = &testContinuation{name: "alt"}
		if err := PUSHCTR(1).Interpret(state); err != nil {
			t.Fatalf("pushctr failed: %v", err)
		}
		got, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop continuation: %v", err)
		}
		if got == state.Reg.C[1] {
			t.Fatal("expected PUSHCTR to clone continuation values")
		}
	})
}

func TestDictJumpMissingC3StackEffects(t *testing.T) {
	cases := []struct {
		name string
		op   vm.OP
		id   int64
	}{
		{name: "CallDictShort", op: CALLDICT(5), id: 5},
		{name: "CallDictLong", op: CALLDICT(300), id: 300},
		{name: "JmpDict", op: JMPDICT(8), id: 8},
		{name: "PrepareDict", op: PREPAREDICT(9), id: 9},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			state := newTestState()
			if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
				t.Fatalf("push residual: %v", err)
			}

			assertVMErrCode(t, tt.op.Interpret(state), vmerr.CodeTypeCheck)
			if state.Stack.Len() != 2 {
				t.Fatalf("expected residual and dict id to remain, stack len=%d", state.Stack.Len())
			}
			id, err := state.Stack.PopIntFinite()
			if err != nil || id.Int64() != tt.id {
				t.Fatalf("unexpected pushed dict id: %v err=%v", id, err)
			}
			residual, err := state.Stack.PopIntFinite()
			if err != nil || residual.Int64() != 11 {
				t.Fatalf("unexpected residual: %v err=%v", residual, err)
			}
		})
	}

	t.Run("TruncatedSuffixes", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			op   *helpers.AdvancedOP
		}{
			{name: "call short", op: callDictShort(0)},
			{name: "call long", op: callDictLong(0)},
			{name: "jump", op: jmpDict(0)},
			{name: "prepare", op: prepareDict(0)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				prefix := tt.op.GetPrefixes()[0]
				if err := tt.op.Deserialize(prefix); err == nil {
					t.Fatal("expected truncated suffix to fail")
				}
			})
		}
	})
}

func TestBoolOrErrorStackEffects(t *testing.T) {
	t.Run("UnderflowPreservesStack", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "only"}); err != nil {
			t.Fatalf("push only continuation: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, BOOLOR().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("BOOLOR underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("TopNotContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "base"}); err != nil {
			t.Fatalf("push base continuation: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad top: %v", err)
		}

		assertVMErrCode(t, BOOLOR().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad top to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop base continuation: %v", err)
		}
		if continuationName(t, cont) != "base" {
			t.Fatalf("unexpected remaining continuation")
		}
	})

	t.Run("SecondNotContinuationConsumesBoth", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad second: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "value"}); err != nil {
			t.Fatalf("push value continuation: %v", err)
		}

		assertVMErrCode(t, BOOLOR().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected both inputs to be consumed, stack len=%d", state.Stack.Len())
		}
	})
}

func TestLoopEntryErrorStackEffects(t *testing.T) {
	t.Run("RepeatUnderflowPreservesStack", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push count: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, REPEAT().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("REPEAT underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("RepeatTopNotContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("push count: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad body: %v", err)
		}

		assertVMErrCode(t, REPEAT().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad body to be consumed only, stack len=%d", state.Stack.Len())
		}
		count, err := state.Stack.PopIntFinite()
		if err != nil || count.Int64() != 3 {
			t.Fatalf("unexpected remaining count: %v err=%v", count, err)
		}
	})

	t.Run("RepeatBadCountConsumesBodyAndCount", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad count: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push body: %v", err)
		}

		assertVMErrCode(t, REPEAT().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad count and body to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("RepeatCountRangeConsumesBodyAndCount", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(1 << 31)); err != nil {
			t.Fatalf("push out-of-range count: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push body: %v", err)
		}

		assertVMErrCode(t, REPEAT().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad count and body to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("WhileUnderflowPreservesStack", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push residual: %v", err)
		}

		before := state.Stack.String()
		assertVMErrCode(t, WHILE().Interpret(state), vmerr.CodeStackUnderflow)
		if after := state.Stack.String(); after != before {
			t.Fatalf("WHILE underflow mutated stack:\nbefore:\n%s\nafter:\n%s", before, after)
		}
	})

	t.Run("WhileTopNotContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushContinuation(&testContinuation{name: "cond"}); err != nil {
			t.Fatalf("push cond: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad body: %v", err)
		}

		assertVMErrCode(t, WHILE().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad body to be consumed only, stack len=%d", state.Stack.Len())
		}
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			t.Fatalf("pop cond: %v", err)
		}
		if continuationName(t, cont) != "cond" {
			t.Fatalf("unexpected remaining continuation")
		}
	})

	t.Run("WhileCondNotContinuationConsumesBoth", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push bad cond: %v", err)
		}
		if err := state.Stack.PushContinuation(&testContinuation{name: "body"}); err != nil {
			t.Fatalf("push body: %v", err)
		}

		assertVMErrCode(t, WHILE().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected bad cond and body to be consumed, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("UntilTopNotContinuationConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push bad body: %v", err)
		}

		assertVMErrCode(t, UNTIL().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad body to be consumed only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

}

func TestLoopEndAdditionalErrorStackEffects(t *testing.T) {
	t.Run("AgainBadBodyConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(22)); err != nil {
			t.Fatalf("push bad body: %v", err)
		}

		assertVMErrCode(t, AGAIN().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad body to be consumed only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("RepeatEndBadCountConsumesCountOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad count: %v", err)
		}

		assertVMErrCode(t, REPEATEND().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad count to be consumed only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("WhileEndBadConditionConsumesTopOnly", func(t *testing.T) {
		state := newTestState()
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad cond: %v", err)
		}

		assertVMErrCode(t, WHILEEND().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected bad condition to be consumed only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("WhileEndConditionWithSavedC0KeepsCurrentC0", func(t *testing.T) {
		state := newTestState()
		oldC0 := &testContinuation{name: "old_c0"}
		state.Reg.C[0] = oldC0
		condData := &vm.ControlData{NumArgs: vm.ControlDataAllArgs, CP: vm.CP}
		condData.Save.C[0] = &testContinuation{name: "saved_c0"}
		cond := &testContinuation{
			name: "cond",
			data: condData,
		}
		if err := state.Stack.PushContinuation(cond); err != nil {
			t.Fatalf("push cond: %v", err)
		}

		if err := WHILEEND().Interpret(state); err != nil {
			t.Fatalf("whileend failed: %v", err)
		}
		if state.Reg.C[0] != oldC0 {
			t.Fatalf("expected WHILEEND not to overwrite c0 when condition saves c0")
		}
	})
}
