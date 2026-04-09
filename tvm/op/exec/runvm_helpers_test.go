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
	if err := parsed.Deserialize(serialized.BeginParse()); err != nil {
		t.Fatalf("deserialize failed: %v", err)
	}
	if parsed.SerializeText() != "RUNVM 37" {
		t.Fatalf("deserialize should update mode, got %q", parsed.SerializeText())
	}
	if got := parsed.Serialize().EndCell().BeginParse().MustLoadUInt(24); got != serialized.BeginParse().MustLoadUInt(24) {
		t.Fatalf("unexpected round-trip encoding: %#x", got)
	}
}

func TestRunChildVMWithModeRejectsInvalidFlags(t *testing.T) {
	state := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.NewGas(), nil, tuple.Tuple{}, vm.NewStack())
	err := runChildVMWithMode(state, 512)
	if err == nil {
		t.Fatal("expected invalid flag error")
	}

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeRangeCheck {
		t.Fatalf("expected range check, got %v", err)
	}
}

func TestRunChildVMWithModeSuccess(t *testing.T) {
	state := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
	state.CurrentCode = cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell().BeginParse()

	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
	data := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	retData := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	retActions := cell.BeginCell().MustStoreUInt(0xDD, 8).EndCell()
	c7 := *tuple.NewTuple(big.NewInt(9))

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
	state := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
	if err := state.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push stack size: %v", err)
	}
	if err := state.Stack.PushSlice(cell.BeginCell().EndCell().BeginParse()); err != nil {
		t.Fatalf("push code: %v", err)
	}

	err := runChildVMWithMode(state, 0)
	if err == nil {
		t.Fatal("expected invalid stack size")
	}
}

func TestRUNVMXUsesDynamicMode(t *testing.T) {
	state := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.GasWithLimit(100), nil, tuple.Tuple{}, vm.NewStack())
	state.CurrentCode = cell.BeginCell().EndCell().BeginParse()
	state.SetChildRunner(func(child *vm.State) (int64, error) {
		child.Stack.Clear()
		return 0, nil
	})

	if err := state.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push stack size: %v", err)
	}
	if err := state.Stack.PushSlice(cell.BeginCell().EndCell().BeginParse()); err != nil {
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
		return state.Stack.PushInt(big.NewInt(int64(refs[0].BeginParse().MustLoadUInt(8) + refs[1].BeginParse().MustLoadUInt(8))))
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
	if op.refs[0] == nil || op.refs[1] != nil {
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
	if err := decoded.Deserialize(serialized.BeginParse()); err != nil {
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

	if !sameStackValueType(nil, nil) {
		t.Fatal("nil values should match")
	}
	if !sameStackValueType(vm.NaN{}, &vm.NaN{}) {
		t.Fatal("NaN values should match")
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
	if got := serialize().EndCell().BeginParse().MustLoadUInt(4); got != 5 {
		t.Fatalf("unexpected serialized control register index: %d", got)
	}

	var decoded int
	if err := deserializeControlRegisterIndex(&decoded)(cell.BeginCell().MustStoreUInt(5, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("deserialize control register index failed: %v", err)
	}
	if decoded != 5 {
		t.Fatalf("unexpected decoded control register index: %d", decoded)
	}
	if err := deserializeControlRegisterIndex(&decoded)(cell.BeginCell().MustStoreUInt(6, 4).EndCell().BeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
		t.Fatalf("expected corrupted opcode, got %v", err)
	}

	state := newTestState()
	state.Reg.C[1] = &testContinuation{name: "alt"}
	state.Reg.D[0] = cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	state.Reg.C7 = *tuple.NewTuple(big.NewInt(7))

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
