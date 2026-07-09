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

func TestClampRunvmGasLimit(t *testing.T) {
	if got := clampRunvmGasLimit(20, 10); got != 10 {
		t.Fatalf("expected clamped limit 10, got %d", got)
	}
	if got := clampRunvmGasLimit(5, 10); got != 5 {
		t.Fatalf("expected unchanged limit 5, got %d", got)
	}
}

func TestRUNVMSerializeDeserialize(t *testing.T) {
	op := RUNVM(37)
	if op.SerializeText() != "RUNVM 37" {
		t.Fatalf("unexpected name: %q", op.SerializeText())
	}
	if op.InstructionBits() != 24 {
		t.Fatalf("unexpected instruction bits: %d", op.InstructionBits())
	}

	serialized := op.Serialize().EndCell()
	parsed := RUNVM(0)
	if err := parsed.Deserialize(serialized.MustBeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
	if parsed.SerializeText() != "RUNVM 37" {
		t.Fatalf("deserialize should update mode, got %q", parsed.SerializeText())
	}
	if got := parsed.Serialize().EndCell().MustBeginParse().MustLoadUInt(24); got != serialized.MustBeginParse().MustLoadUInt(24) {
		t.Fatalf("unexpected round-trip encoding: %#x", got)
	}

	truncated := RUNVM(0)
	if err := truncated.DeserializeMatched(cell.BeginCell().MustStoreSlice(truncated.BitPrefix.Data, truncated.BitPrefix.Bits).EndCell().MustBeginParse()); err == nil {
		t.Fatal("expected short RUNVM suffix error")
	}
}

func TestRUNVMWrapperErrorStackEffects(t *testing.T) {
	t.Run("RUNVMActionBadCodeConsumesCodeOnly", func(t *testing.T) {
		state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
		if err := state.Stack.PushInt(big.NewInt(11)); err != nil {
			t.Fatalf("push residual: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("push bad code: %v", err)
		}

		assertVMErrCode(t, RUNVM(0).Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 1 {
			t.Fatalf("expected RUNVM bad code to consume code only, stack len=%d", state.Stack.Len())
		}
		residual, err := state.Stack.PopIntFinite()
		if err != nil || residual.Int64() != 11 {
			t.Fatalf("unexpected residual: %v err=%v", residual, err)
		}
	})

	t.Run("RUNVMXBadModeConsumesMode", func(t *testing.T) {
		state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
		if err := state.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push bad mode: %v", err)
		}

		assertVMErrCode(t, RUNVMX().Interpret(state), vmerr.CodeTypeCheck)
		if state.Stack.Len() != 0 {
			t.Fatalf("expected RUNVMX bad mode to consume top, stack len=%d", state.Stack.Len())
		}
	})

	t.Run("RUNVMXInvalidFlagsPreservesOperands", func(t *testing.T) {
		state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
		code := cell.BeginCell().EndCell().MustBeginParse()
		if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push stack size: %v", err)
		}
		if err := state.Stack.PushSlice(code); err != nil {
			t.Fatalf("push code: %v", err)
		}
		if err := state.Stack.PushInt(big.NewInt(512)); err != nil {
			t.Fatalf("push invalid mode: %v", err)
		}

		assertVMErrCode(t, RUNVMX().Interpret(state), vmerr.CodeRangeCheck)
		if state.Stack.Len() != 2 {
			t.Fatalf("expected RUNVMX invalid flags to consume mode only, stack len=%d", state.Stack.Len())
		}
		if _, err := state.Stack.PopSlice(); err != nil {
			t.Fatalf("expected code slice to remain: %v", err)
		}
		stackSize, err := state.Stack.PopIntFinite()
		if err != nil || stackSize.Int64() != 0 {
			t.Fatalf("unexpected remaining stack size: %v err=%v", stackSize, err)
		}
	})
}

func TestRunChildVMWithModeRejectsInvalidFlags(t *testing.T) {
	state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.NewGas(), nil, tuple.Tuple{}, vm.NewStack())
	err := runChildVMWithMode(state, 512)
	if err == nil {
		t.Fatal("expected invalid flag error")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeRangeCheck {
		t.Fatalf("expected range check, got %v", err)
	}
}

func TestRunChildVMWithModeOperandErrors(t *testing.T) {
	emptyCode := func() *cell.Slice {
		return cell.BeginCell().EndCell().MustBeginParse()
	}
	pushInt := func(t *testing.T, st *vm.Stack, v int64) {
		t.Helper()
		if err := st.PushInt(big.NewInt(v)); err != nil {
			t.Fatalf("push int: %v", err)
		}
	}
	pushCell := func(t *testing.T, st *vm.Stack) {
		t.Helper()
		if err := st.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("push cell: %v", err)
		}
	}
	pushSlice := func(t *testing.T, st *vm.Stack) {
		t.Helper()
		if err := st.PushSlice(emptyCode()); err != nil {
			t.Fatalf("push slice: %v", err)
		}
	}

	tests := []struct {
		name   string
		mode   int
		gas    vm.Gas
		setup  func(t *testing.T, st *vm.Stack)
		code   int64
		leftOn int
	}{
		{
			name:   "out of gas before operands",
			mode:   0,
			gas:    vm.Gas{},
			setup:  func(*testing.T, *vm.Stack) {},
			code:   vmerr.CodeOutOfGas,
			leftOn: 0,
		},
		{
			name: "bad gas max",
			mode: 64,
			setup: func(t *testing.T, st *vm.Stack) {
				pushCell(t, st)
			},
			code: vmerr.CodeTypeCheck,
		},
		{
			name: "bad gas limit",
			mode: 8,
			setup: func(t *testing.T, st *vm.Stack) {
				pushCell(t, st)
			},
			code: vmerr.CodeTypeCheck,
		},
		{
			name: "bad c7",
			mode: 16,
			setup: func(t *testing.T, st *vm.Stack) {
				pushInt(t, st, 7)
			},
			code: vmerr.CodeTypeCheck,
		},
		{
			name: "bad data",
			mode: 4,
			setup: func(t *testing.T, st *vm.Stack) {
				pushInt(t, st, 1)
			},
			code: vmerr.CodeTypeCheck,
		},
		{
			name: "bad return count",
			mode: 256,
			setup: func(t *testing.T, st *vm.Stack) {
				pushCell(t, st)
			},
			code: vmerr.CodeTypeCheck,
		},
		{
			name: "bad code",
			mode: 0,
			setup: func(t *testing.T, st *vm.Stack) {
				pushInt(t, st, 0)
			},
			code: vmerr.CodeTypeCheck,
		},
		{
			name: "missing stack size after code",
			mode: 0,
			setup: func(t *testing.T, st *vm.Stack) {
				pushSlice(t, st)
			},
			code: vmerr.CodeStackUnderflow,
		},
		{
			name: "child stack gas out of gas",
			mode: 0,
			gas:  vm.GasWithLimit(vm.RunvmGasPrice),
			setup: func(t *testing.T, st *vm.Stack) {
				for i := int64(0); i < vm.FreeStackDepth+1; i++ {
					pushInt(t, st, i)
				}
				pushInt(t, st, vm.FreeStackDepth+1)
				pushSlice(t, st)
			},
			code: vmerr.CodeOutOfGas,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gas := tt.gas
			if gas.Remaining == 0 && tt.code != vmerr.CodeOutOfGas {
				gas = vm.GasWithLimit(100)
			}
			state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, gas, nil, tuple.Tuple{}, vm.NewStack())
			tt.setup(t, state.Stack)

			assertVMErrCode(t, runChildVMWithMode(state, tt.mode), tt.code)
			if state.Stack.Len() != tt.leftOn {
				t.Fatalf("stack len after error = %d, want %d", state.Stack.Len(), tt.leftOn)
			}
		})
	}
}

func TestRunChildVMWithModeSuccess(t *testing.T) {
	state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell().MustBeginParse()

	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse()
	data := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	retData := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	retActions := cell.BeginCell().MustStoreUInt(0xDD, 8).EndCell()
	c7 := tuple.NewTupleValue(big.NewInt(9))

	state.SetChildRunner(func(child *vm.State) (int64, error) {
		if child.Gas.Limit != 50 || child.Gas.Max != 50 {
			t.Fatalf("unexpected child gas limits: max=%d limit=%d", child.Gas.Max, child.Gas.Limit)
		}
		if child.Reg.D[0] == nil || string(child.Reg.D[0].Hash()) != string(data.Hash()) {
			t.Fatalf("expected child data register to be passed through")
		}
		if child.Reg.C7.Len() != 1 {
			t.Fatalf("expected c7 tuple to be passed through")
		}
		if child.Reg.C[3] == nil {
			t.Fatal("expected same-c3 continuation to be installed")
		}

		zero, err := child.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop pushed zero: %v", err)
		}
		if zero.Int64() != 0 {
			t.Fatalf("expected pushed zero, got %s", zero.String())
		}
		arg, err := child.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop child arg: %v", err)
		}
		if arg.Int64() != 77 {
			t.Fatalf("unexpected moved child arg: %s", arg.String())
		}

		child.Stack.Clear()
		if err = child.Stack.PushInt(big.NewInt(123)); err != nil {
			t.Fatalf("push child result: %v", err)
		}
		child.Committed = vm.CommittedState{
			Data:      retData,
			Actions:   retActions,
			Committed: true,
		}
		child.Gas.Remaining = 30
		return 0, nil
	})

	if err := state.Stack.PushInt(big.NewInt(77)); err != nil {
		t.Fatalf("push arg: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push stack size: %v", err)
	}
	if err := state.Stack.PushSlice(code); err != nil {
		t.Fatalf("push code: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push return values: %v", err)
	}
	if err := state.Stack.PushCell(data); err != nil {
		t.Fatalf("push data: %v", err)
	}
	if err := state.Stack.PushTuple(c7); err != nil {
		t.Fatalf("push c7: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(50)); err != nil {
		t.Fatalf("push gas limit: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("push gas max: %v", err)
	}

	if err := runChildVMWithMode(state, 511); err != nil {
		t.Fatalf("run child vm failed: %v", err)
	}

	usedGas, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop returned gas: %v", err)
	}
	if usedGas.Int64() != 20 {
		t.Fatalf("unexpected returned gas: %s", usedGas.String())
	}

	actions, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop returned actions: %v", err)
	}
	if string(actions.Hash()) != string(retActions.Hash()) {
		t.Fatalf("unexpected returned actions")
	}

	gotData, err := state.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop returned data: %v", err)
	}
	if string(gotData.Hash()) != string(retData.Hash()) {
		t.Fatalf("unexpected returned data")
	}

	exitCode, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop exit code: %v", err)
	}
	if exitCode.Int64() != 0 {
		t.Fatalf("unexpected exit code: %s", exitCode.String())
	}

	retValue, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop return value: %v", err)
	}
	if retValue.Int64() != 123 {
		t.Fatalf("unexpected return value: %s", retValue.String())
	}
}

func TestRunChildVMWithModeRejectsInvalidStackSize(t *testing.T) {
	state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push stack size: %v", err)
	}
	if err := state.Stack.PushSlice(cell.BeginCell().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("push code: %v", err)
	}

	err := runChildVMWithMode(state, 0)
	if err == nil {
		t.Fatal("expected invalid stack size")
	}
}

func TestRUNVMXUsesDynamicMode(t *testing.T) {
	state := vm.NewExecutionState(vm.MaxSupportedGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
	state.CurrentCode = cell.BeginCell().EndCell().MustBeginParse()
	state.SetChildRunner(func(child *vm.State) (int64, error) {
		child.Stack.Clear()
		return 0, nil
	})

	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push stack size: %v", err)
	}
	if err := state.Stack.PushSlice(cell.BeginCell().EndCell().MustBeginParse()); err != nil {
		t.Fatalf("push code: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push mode: %v", err)
	}

	if err := RUNVMX().Interpret(state); err != nil {
		t.Fatalf("runvmx failed: %v", err)
	}

	exitCode, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop exit code: %v", err)
	}
	if exitCode.Int64() != 0 {
		t.Fatalf("unexpected exit code: %s", exitCode.String())
	}
}

func TestContinuationHelpers(t *testing.T) {
	state := newTestState()
	state.CP = 9
	state.InitForExecution()

	codeCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	cont, err := loadContinuationFromCodeCell(state, codeCell)
	if err != nil {
		t.Fatalf("load continuation: %v", err)
	}
	ord, ok := cont.(*vm.OrdinaryContinuation)
	if !ok {
		t.Fatalf("expected ordinary continuation, got %T", cont)
	}
	if ord.Data.CP != 9 || ord.Code.MustLoadUInt(8) != 0xAB {
		t.Fatalf("unexpected continuation contents")
	}

	if err = pushCurrentCode(state); err != nil {
		t.Fatalf("push current code: %v", err)
	}
	pushed, err := state.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop current code: %v", err)
	}
	if pushed == state.CurrentCode {
		t.Fatal("pushCurrentCode should push a copy")
	}
	if pushed.BitsLeft() != state.CurrentCode.BitsLeft() {
		t.Fatalf("unexpected pushed code size: %d", pushed.BitsLeft())
	}

	nilCode := newTestState()
	nilCode.CurrentCode = nil
	if err = pushCurrentCode(nilCode); err == nil {
		t.Fatal("expected nil current code error")
	}

	saveTarget := &vm.Register{}
	first := &testContinuation{name: "first"}
	second := &testContinuation{name: "second"}
	defineSavedContinuation(saveTarget, 0, first)
	defineSavedContinuation(saveTarget, 0, second)
	if continuationName(t, saveTarget.C[0]) != "first" {
		t.Fatalf("defineSavedContinuation should keep the first value")
	}

	state.Reg.C[0] = &testContinuation{name: "c0"}
	state.Reg.C[1] = &testContinuation{name: "c1"}
	enveloped := c1Envelope(state, &testContinuation{name: "body"}, true)
	argExt, ok := state.Reg.C[1].(*vm.ArgExtContinuation)
	if !ok {
		t.Fatalf("expected c1 to hold control-data wrapper, got %T", state.Reg.C[1])
	}
	if continuationName(t, argExt.Ext) != "body" {
		t.Fatalf("expected body continuation to be wrapped")
	}
	envData := vm.ForceControlData(enveloped).GetControlData()
	if continuationName(t, envData.Save.C[0]) != "c0" || continuationName(t, envData.Save.C[1]) != "c1" {
		t.Fatalf("expected c0/c1 to be saved in envelope")
	}

	plain := &testContinuation{name: "plain"}
	if c1EnvelopeIf(state, false, plain) != plain {
		t.Fatal("disabled envelope should return original continuation")
	}

	state.Reg.C[0] = &testContinuation{name: "ret"}
	state.Reg.C[1] = &testContinuation{name: "alt"}
	c1SaveSet(state, true)
	if state.Reg.C[1].GetControlData() == nil {
		t.Fatalf("expected c1 to mirror c0 with control data")
	}
	save := vm.ForceControlData(state.Reg.C[0]).GetControlData().Save
	if continuationName(t, save.C[1]) != "alt" {
		t.Fatalf("expected c1 to be saved in c0")
	}
}

func TestRefCodeOpLifecycleAndHelpers(t *testing.T) {
	refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	suffixValue := uint64(0)

	op := newRefCodeOp("REFOP", vmPrefixForTest(), 2, func(state *vm.State, refs []*cell.Cell) error {
		if len(refs) != 2 {
			t.Fatalf("unexpected refs len: %d", len(refs))
		}
		return state.Stack.PushInt(big.NewInt(int64(refs[0].MustBeginParse().MustLoadUInt(8) + refs[1].MustBeginParse().MustLoadUInt(8))))
	})
	op.fixedBits = 3
	op.serializeSuffix = func() *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0x5, 3)
	}
	op.deserializeSuffix = func(code *cell.Slice) error {
		val, err := code.LoadUInt(3)
		if err != nil {
			return err
		}
		suffixValue = val
		return nil
	}
	bindRefCodeOp(op, refA, nil)
	if op.refsNum != 2 || op.refs[0] == nil || op.refs[1] != nil {
		t.Fatalf("bindRefCodeOp should ignore nil refs")
	}
	bindRefCodeOp(op, nil, refB)

	if len(op.GetPrefixes()) != 1 {
		t.Fatalf("unexpected prefix count")
	}
	if op.SerializeText() != "REFOP" {
		t.Fatalf("unexpected op name: %q", op.SerializeText())
	}
	if op.InstructionBits() != 19 {
		t.Fatalf("unexpected instruction bits: %d", op.InstructionBits())
	}

	serialized := op.Serialize().EndCell()
	decoded := newRefCodeOp("REFOP", vmPrefixForTest(), 2, func(*vm.State, []*cell.Cell) error { return nil })
	decoded.fixedBits = 3
	decoded.deserializeSuffix = op.deserializeSuffix
	if err := decoded.Deserialize(serialized.MustBeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
	if suffixValue != 0x5 {
		t.Fatalf("unexpected decoded suffix: %d", suffixValue)
	}
	if decoded.refs[0] == nil || decoded.refs[1] == nil {
		t.Fatalf("expected both refs to be decoded")
	}

	state := newTestState()
	if err := op.Interpret(state); err != nil {
		t.Fatalf("interpret failed: %v", err)
	}
	sum, err := state.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("pop sum: %v", err)
	}
	if sum.Int64() != 0x165 {
		t.Fatalf("unexpected sum: %s", sum.String())
	}

	missing := newRefCodeOp("MISSING", vmPrefixForTest(), 1, func(*vm.State, []*cell.Cell) error { return nil })
	err = missing.Interpret(newTestState())
	if err == nil {
		t.Fatal("expected missing refs error")
	}

	if cloneContinuation(nil) != nil {
		t.Fatal("nil continuation clone should stay nil")
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected ref count panic")
			}
		}()
		_ = newRefCodeOp("BADREFS", vmPrefixForTest(), 5, func(*vm.State, []*cell.Cell) error { return nil })
	}()

	oneRef := newRefCodeOp("ONEREF", vmPrefixForTest(), 1, func(*vm.State, []*cell.Cell) error { return nil })
	bindRefCodeOp(oneRef, refA, refB)
	if oneRef.refs[0] != refA || oneRef.refs[1] != nil {
		t.Fatal("bindRefCodeOp should ignore references past refsNum")
	}

	shortPrefix := newRefCodeOp("SHORT", vmPrefixForTest(), 0, func(*vm.State, []*cell.Cell) error { return nil })
	if err = shortPrefix.DeserializeMatched(cell.BeginCell().EndCell().MustBeginParse()); err == nil {
		t.Fatal("expected short prefix deserialize error")
	}

	suffixErr := errors.New("suffix")
	badSuffix := newRefCodeOp("BADSUFFIX", vmPrefixForTest(), 0, func(*vm.State, []*cell.Cell) error { return nil })
	badSuffix.deserializeSuffix = func(*cell.Slice) error {
		return suffixErr
	}
	err = badSuffix.DeserializeMatched(cell.BeginCell().MustStoreSlice(vmPrefixForTest().Data, vmPrefixForTest().Bits).EndCell().MustBeginParse())
	if !errors.Is(err, suffixErr) {
		t.Fatalf("expected suffix error, got %v", err)
	}

	partialRefs := newRefCodeOp("PARTIALREFS", vmPrefixForTest(), 2, func(*vm.State, []*cell.Cell) error { return nil })
	partialRefs.refs[1] = refB
	partialCell := cell.BeginCell().
		MustStoreSlice(vmPrefixForTest().Data, vmPrefixForTest().Bits).
		MustStoreRef(refA).
		EndCell()
	if err = partialRefs.DeserializeMatched(partialCell.MustBeginParse()); err != nil {
		t.Fatalf("partial refs deserialize failed: %v", err)
	}
	if partialRefs.refs[0] != refA || partialRefs.refs[1] != nil {
		t.Fatal("partial refs deserialize should keep decoded refs and clear missing refs")
	}

	if !sameStackValueType(nil, nil) {
		t.Fatal("nil values should match")
	}
	if !sameStackValueType(vm.NaN{}, vm.NaN{}) {
		t.Fatal("NaN values should match")
	}
	if sameStackValueType(vm.NaN{}, &vm.NaN{}) {
		t.Fatal("NaN pointer should not match NaN value")
	}
	if !sameStackValueType(&testContinuation{name: "x"}, &testContinuation{name: "y"}) {
		t.Fatal("continuations should match by interface type")
	}
	if sameStackValueType(big.NewInt(1), cell.BeginCell().EndCell()) {
		t.Fatal("different stack value types should not match")
	}
}

func TestAdditionalControlRegisterHelpers(t *testing.T) {
	idx := 5
	serialize := serializeControlRegisterIndex(&idx)
	if got := serialize().EndCell().MustBeginParse().MustLoadUInt(4); got != 5 {
		t.Fatalf("unexpected serialized control register index: %d", got)
	}

	var decoded int
	if err := deserializeControlRegisterIndex(&decoded)(cell.BeginCell().MustStoreUInt(5, 4).EndCell().MustBeginParse()); err != nil {
		t.Fatalf("deserialize control register index failed: %v", err)
	}
	if decoded != 5 {
		t.Fatalf("unexpected decoded control register index: %d", decoded)
	}
	if err := deserializeControlRegisterIndex(&decoded)(cell.BeginCell().MustStoreUInt(6, 4).EndCell().MustBeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
		t.Fatalf("expected corrupted opcode, got %v", err)
	}

	state := newTestState()
	state.Reg.C[1] = &testContinuation{name: "alt"}
	state.Reg.D[0] = cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	state.Reg.C7 = tuple.NewTupleValue(big.NewInt(7))

	if err := state.Stack.PushCell(state.Reg.D[0]); err != nil {
		t.Fatalf("push cell: %v", err)
	}
	if err := SETALTCTR(4).Interpret(state); err != nil {
		t.Fatalf("setaltctr failed: %v", err)
	}
	if state.Reg.C[1].GetControlData().Save.D[0] == nil {
		t.Fatalf("expected c4 to be saved in c1")
	}

	if err := SAVEALTCTR(7).Interpret(state); err != nil {
		t.Fatalf("savealtctr failed: %v", err)
	}
	if state.Reg.C[1].GetControlData().Save.C7.Len() != 1 {
		t.Fatalf("expected c7 to be saved in c1")
	}

	state.Reg.C[0] = &testContinuation{name: "ret"}
	if err := SAVEBOTHCTR(4).Interpret(state); err != nil {
		t.Fatalf("savebothctr failed: %v", err)
	}
	if state.Reg.C[0].GetControlData().Save.D[0] == nil || state.Reg.C[1].GetControlData().Save.D[0] == nil {
		t.Fatalf("expected c4 to be saved in both continuations")
	}

	if err := state.Stack.PushInt(big.NewInt(7)); err != nil {
		t.Fatalf("push idx: %v", err)
	}
	if err := PUSHCTRX().Interpret(state); err != nil {
		t.Fatalf("pushctrx failed: %v", err)
	}
	gotTuple, err := state.Stack.PopTuple()
	if err != nil {
		t.Fatalf("pop pushed c7: %v", err)
	}
	if gotTuple.Len() != 1 {
		t.Fatalf("unexpected pushed c7 tuple")
	}

	cont := &testContinuation{name: "target"}
	value := cell.BeginCell().MustStoreUInt(0xEE, 8).EndCell()
	if err := state.Stack.PushCell(value); err != nil {
		t.Fatalf("push value: %v", err)
	}
	if err := state.Stack.PushContinuation(cont); err != nil {
		t.Fatalf("push cont: %v", err)
	}
	if err := state.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("push idx: %v", err)
	}
	if err := SETCONTCTRX().Interpret(state); err != nil {
		t.Fatalf("setcontctrx failed: %v", err)
	}
	gotCont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop continuation: %v", err)
	}
	if gotCont.GetControlData().Save.D[0] == nil {
		t.Fatalf("expected c4 to be saved in continuation")
	}
}

func vmPrefixForTest() helpers.BitPrefix {
	return helpers.BytesPrefix(0xAB, 0xCD)
}
