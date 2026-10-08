package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestRunChildVMReturnedContinuationDropsChildTrace(t *testing.T) {
	parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	parent.InitForExecution()
	parentTrace := parent.Cells.Trace()

	var childState *State
	var childTrace *cell.Trace
	parent.SetChildRunner(func(child *State) (int64, error) {
		childState = child
		childTrace = child.Cells.Trace()

		leaf := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		capturedSlice := cell.BeginCell().MustStoreRef(leaf).EndCell().MustBeginParse().SetTrace(childTrace)
		captured := NewStack()
		captured.SetTrace(childTrace)
		if err := captured.PushOwnedSlice(capturedSlice); err != nil {
			return 0, err
		}

		nested := &OrdinaryContinuation{
			Data: ControlData{NumArgs: ControlDataAllArgs, CP: child.CP},
			Code: cell.BeginCell().EndCell().MustBeginParse().SetTrace(childTrace),
		}
		savedSlice := cell.BeginCell().MustStoreUInt(1, 1).EndCell().MustBeginParse().SetTrace(childTrace)
		savedCell := cell.BeginCell().MustStoreUInt(2, 2).EndCell().WithTrace(childTrace)

		cont := &OrdinaryContinuation{
			Data: ControlData{
				Stack:   captured,
				NumArgs: 0,
				CP:      child.CP,
			},
			Code: cell.BeginCell().EndCell().MustBeginParse().SetTrace(childTrace),
		}
		cont.Data.Save.C[0] = nested
		cont.Data.Save.D[0] = savedCell
		cont.Data.Save.C7 = tuple.NewTupleOwnedBound([]any{savedSlice}, childTrace)

		return 0, child.Stack.PushOwnedContinuation(cont)
	})

	err := parent.RunChildVM(ChildVMConfig{
		Code:         cell.BeginCell().EndCell().MustBeginParse(),
		Stack:        NewStack(),
		Gas:          GasWithLimit(10_000),
		ReturnValues: 1,
	})
	if err != nil {
		t.Fatalf("run child VM: %v", err)
	}
	if childState == nil || childTrace == nil || childTrace == parentTrace {
		t.Fatal("child VM did not get an independent cell trace")
	}

	exitCode, err := parent.Stack.PopInt()
	if err != nil {
		t.Fatalf("pop child exit code: %v", err)
	}
	if exitCode.Int64() != 0 {
		t.Fatalf("child exit code = %d, want 0", exitCode.Int64())
	}
	returnedAny, err := parent.Stack.PopContinuation()
	if err != nil {
		t.Fatalf("pop returned continuation: %v", err)
	}
	returned, ok := returnedAny.(*OrdinaryContinuation)
	if !ok {
		t.Fatalf("returned continuation type = %T, want *OrdinaryContinuation", returnedAny)
	}

	if returned.Code.Trace() != nil {
		t.Fatal("returned continuation code retained child trace")
	}
	if returned.Data.Stack.trace != nil {
		t.Fatal("returned continuation stack retained child trace")
	}
	capturedSlice, ok := returned.Data.Stack.elems[0].(*cell.Slice)
	if !ok || capturedSlice.Trace() != nil {
		t.Fatal("returned continuation captured slice retained child trace")
	}
	if returned.Data.Save.D[0].Trace() != nil {
		t.Fatal("returned continuation saved cell retained child trace")
	}
	if returned.Data.Save.C7.BindingID() != nil {
		t.Fatal("returned continuation c7 retained child binding")
	}
	savedAny, err := returned.Data.Save.C7.RawIndex(0)
	if err != nil {
		t.Fatalf("read returned continuation c7: %v", err)
	}
	if savedAny.(*cell.Slice).Trace() != nil {
		t.Fatal("returned continuation c7 value retained child trace")
	}
	nested, ok := returned.Data.Save.C[0].(*OrdinaryContinuation)
	if !ok || nested.Code.Trace() != nil {
		t.Fatal("returned continuation nested saved continuation retained child trace")
	}

	if err = parent.JumpArgs(returned, 0); err != nil {
		t.Fatalf("jump to returned continuation: %v", err)
	}
	if parent.Stack.trace != parentTrace {
		t.Fatal("returned continuation captured stack was not rebound to parent trace")
	}
	if parent.CurrentCode.Trace() != parentTrace {
		t.Fatal("returned continuation code was not rebound to parent trace")
	}

	global, err := parent.GetGlobal(0)
	if err != nil {
		t.Fatalf("read returned continuation c7 in parent: %v", err)
	}
	if global.(*cell.Slice).Trace() != parentTrace {
		t.Fatal("returned continuation c7 value was not rebound to parent trace")
	}

	capturedSlice, err = parent.Stack.PopSlice()
	if err != nil {
		t.Fatalf("pop captured slice after jump: %v", err)
	}
	if capturedSlice.Trace() != parentTrace {
		t.Fatal("captured slice was not rebound to parent trace")
	}

	childGasBefore := childState.Gas.Used()
	parentGasBefore := parent.Gas.Used()
	if _, err = parent.Cells.LoadRef(capturedSlice); err != nil {
		t.Fatalf("load captured slice ref in parent: %v", err)
	}
	if childState.Gas.Used() != childGasBefore {
		t.Fatalf("parent cell load charged completed child: before=%d after=%d", childGasBefore, childState.Gas.Used())
	}
	if parent.Gas.Used() <= parentGasBefore {
		t.Fatal("parent cell load did not charge parent")
	}
}
