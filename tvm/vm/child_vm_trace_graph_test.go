package vm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestRunChildVMIncomingContinuationDropsParentTrace(t *testing.T) {
	parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	parent.InitForExecution()
	parentTrace := parent.Cells.Trace()

	code := cell.BeginCell().MustStoreUInt(0, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).EndCell().MustBeginParse().SetTrace(parentTrace)
	capturedSlice := cell.BeginCell().MustStoreUInt(3, 2).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).EndCell().MustBeginParse().SetTrace(parentTrace)
	capturedBuilder := cell.BeginCell().MustStoreUInt(1, 1).SetTrace(parentTrace)
	capturedInt := big.NewInt(1000)
	captured := NewStack()
	captured.SetTrace(parentTrace)
	if err := captured.PushOwnedSlice(capturedSlice); err != nil {
		t.Fatal(err)
	}
	if err := captured.PushOwnedBuilder(capturedBuilder); err != nil {
		t.Fatal(err)
	}
	if err := captured.PushOwnedInt(capturedInt); err != nil {
		t.Fatal(err)
	}

	cont := &OrdinaryContinuation{
		Data: ControlData{Stack: captured, NumArgs: 0, CP: 0},
		Code: code,
	}
	nested := &OrdinaryContinuation{
		Data: ControlData{NumArgs: ControlDataAllArgs, CP: 0},
		Code: code,
	}
	cont.Data.Save.C[0] = nested
	nested.Data.Save.C[1] = cont
	cont.Data.Save.D[0] = cell.BeginCell().MustStoreUInt(1, 1).EndCell().WithTrace(parentTrace)
	cont.Data.Save.C7 = tuple.NewTupleOwnedBound([]any{capturedSlice, capturedBuilder}, parentTrace)

	input := NewStack()
	input.SetTrace(parentTrace)
	if err := input.PushOwnedContinuation(cont); err != nil {
		t.Fatal(err)
	}
	c7 := tuple.NewTupleOwnedBound([]any{tuple.NewTupleOwnedBound([]any{cont}, parentTrace)}, parentTrace)
	var childGasUsed int64
	parent.SetChildRunner(func(child *State) (int64, error) {
		incoming, err := child.Stack.PopContinuation()
		if err != nil {
			return 0, err
		}
		copied := incoming.(*OrdinaryContinuation)
		if copied == cont || copied.Code == code || copied.Data.Stack == captured {
			t.Fatal("incoming continuation retained parent-owned objects")
		}
		if copied.Code.Trace() != nil || copied.Data.Stack.trace != nil || copied.Data.Save.D[0].Trace() != nil {
			t.Fatal("incoming continuation retained parent trace")
		}
		integer, err := copied.Data.Stack.PopInt()
		if err != nil {
			return 0, err
		}
		integer.SetInt64(2000)
		if capturedInt.Int64() != 1000 {
			t.Fatal("incoming continuation shared mutable captured integer with parent")
		}
		for _, val := range copied.Data.Stack.elems {
			switch v := val.(type) {
			case *cell.Slice:
				if v.Trace() != nil {
					t.Fatal("captured slice retained parent trace")
				}
			case *cell.Builder:
				if v.Trace() != nil {
					t.Fatal("captured builder retained parent trace")
				}
			}
		}
		if copied.Data.Save.C7.BindingID() != nil {
			t.Fatal("saved c7 retained parent trace binding")
		}
		for i := 0; i < copied.Data.Save.C7.Len(); i++ {
			val, err := copied.Data.Save.C7.RawIndex(i)
			if err != nil {
				return 0, err
			}
			switch v := val.(type) {
			case *cell.Slice:
				if v.Trace() != nil {
					t.Fatal("saved c7 slice retained parent trace")
				}
			case *cell.Builder:
				if v.Trace() != nil {
					t.Fatal("saved c7 builder retained parent trace")
				}
			}
		}
		copiedNested := copied.Data.Save.C[0].(*OrdinaryContinuation)
		if copiedNested == nested || copiedNested.Code.Trace() != nil || copiedNested.Data.Save.C[1] != copied {
			t.Fatal("saved continuation cycle was not copied independently")
		}
		inner, err := child.Reg.C7.RawIndex(0)
		if err != nil {
			return 0, err
		}
		innerTuple := inner.(tuple.Tuple)
		global, err := innerTuple.RawIndex(0)
		if err != nil {
			return 0, err
		}
		globalCont := global.(*OrdinaryContinuation)
		if globalCont != copied || globalCont.Code.Trace() != nil {
			t.Fatal("continuation shared by input stack and c7 was not copied as one graph")
		}

		parentGasBefore := parent.Gas.Used()
		if err := child.JumpArgs(copied, 0); err != nil {
			return 0, err
		}
		childTrace := child.Cells.Trace()
		if child.CurrentCode.Trace() != childTrace || child.Stack.trace != childTrace {
			t.Fatal("incoming continuation was not rebound when executed in child")
		}
		childGasBefore := child.Gas.Used()
		if _, err := child.Cells.LoadRef(child.CurrentCode); err != nil {
			return 0, err
		}
		if _, err := child.Cells.LoadRef(child.Stack.elems[0].(*cell.Slice)); err != nil {
			return 0, err
		}
		if err := child.Stack.elems[1].(*cell.Builder).StoreUInt(1, 1); err != nil {
			return 0, err
		}
		if child.Gas.Used() <= childGasBefore {
			t.Fatal("child cell loads did not consume child gas")
		}
		if parent.Gas.Used() != parentGasBefore {
			t.Fatalf("child cell loads immediately charged parent: before=%d after=%d", parentGasBefore, parent.Gas.Used())
		}
		childGasUsed = child.Gas.Used()
		child.Stack.Clear()
		return 0, nil
	})

	if err := parent.RunChildVM(ChildVMConfig{
		Code: cell.BeginCell().EndCell().MustBeginParse(), Stack: input, C7: c7,
		Gas: GasWithLimit(10_000), ReturnValues: 0,
	}); err != nil {
		t.Fatalf("run child VM: %v", err)
	}
	if parent.Gas.Used() != childGasUsed {
		t.Fatalf("parent gas = %d, want child gas charged once (%d)", parent.Gas.Used(), childGasUsed)
	}
	if code.Trace() != parentTrace || code.RefsNum() != 1 || captured.trace != parentTrace || capturedSlice.Trace() != parentTrace || capturedSlice.RefsNum() != 1 || capturedBuilder.Trace() != parentTrace || capturedBuilder.BitsUsed() != 1 {
		t.Fatal("child execution mutated parent continuation code or captured values")
	}
	if cont.Data.Save.C[0] != nested || nested.Data.Save.C[1] != cont || cont.Data.Save.D[0].Trace() != parentTrace || cont.Data.Save.C7.BindingID() != parentTrace {
		t.Fatal("child execution mutated parent saved registers")
	}
}

func TestRunChildVMIncomingTupleDAG(t *testing.T) {
	for _, location := range []string{"stack", "c7"} {
		t.Run(location, func(t *testing.T) {
			parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
			parent.InitForExecution()
			original, originalSlice, originalBuilder := traceTupleDAG(parent.Cells.Trace(), 16)
			cfg := ChildVMConfig{
				Code: cell.BeginCell().EndCell().MustBeginParse(), Stack: NewStack(),
				Gas: GasWithLimit(10_000), ReturnValues: 0,
			}
			if location == "stack" {
				cfg.Stack.SetTrace(parent.Cells.Trace())
				if err := cfg.Stack.PushOwnedValue(original); err != nil {
					t.Fatal(err)
				}
			} else {
				cfg.C7 = original
			}
			parent.SetChildRunner(func(child *State) (int64, error) {
				var transferred tuple.Tuple
				if location == "stack" {
					var err error
					transferred, err = child.Stack.PopTuple()
					if err != nil {
						return 0, err
					}
				} else {
					if err := child.SetC7(child.Reg.C7); err != nil {
						return 0, err
					}
					transferred = child.Reg.C7
				}
				slice, builder := checkTraceTupleDAG(t, transferred, 16, child.Cells.Trace())
				if transferred == original || slice == originalSlice || builder == originalBuilder {
					t.Fatal("incoming tuple retained parent-owned mutable values")
				}
				slice.MustLoadUInt(1)
				builder.MustStoreUInt(1, 1)
				return 0, nil
			})
			if err := parent.RunChildVM(cfg); err != nil {
				t.Fatalf("run child VM: %v", err)
			}
			checkTraceTupleDAG(t, original, 16, parent.Cells.Trace())
			if originalSlice.BitsLeft() != 2 || originalBuilder.BitsUsed() != 1 {
				t.Fatal("child mutation changed original tuple leaves")
			}
		})
	}
}

func TestRunChildVMReturnedTupleDAG(t *testing.T) {
	for _, location := range []string{"stack", "continuation"} {
		t.Run(location, func(t *testing.T) {
			parent := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
			parent.InitForExecution()
			var childState *State
			var original tuple.Tuple
			var originalSlice *cell.Slice
			var originalBuilder *cell.Builder
			parent.SetChildRunner(func(child *State) (int64, error) {
				childState = child
				original, originalSlice, originalBuilder = traceTupleDAG(child.Cells.Trace(), 16)
				if location == "stack" {
					return 0, child.Stack.PushOwnedValue(original)
				}
				captured := NewStack()
				captured.SetTrace(child.Cells.Trace())
				if err := captured.PushOwnedValue(original); err != nil {
					return 0, err
				}
				cont := &OrdinaryContinuation{
					Data: ControlData{Stack: captured, NumArgs: 0, CP: 0},
					Code: cell.BeginCell().EndCell().MustBeginParse().SetTrace(child.Cells.Trace()),
				}
				cont.Data.Save.C7 = original
				return 0, child.Stack.PushOwnedContinuation(cont)
			})
			if err := parent.RunChildVM(ChildVMConfig{
				Code: cell.BeginCell().EndCell().MustBeginParse(), Stack: NewStack(),
				Gas: GasWithLimit(10_000), ReturnValues: 1,
			}); err != nil {
				t.Fatalf("run child VM: %v", err)
			}
			exitCode, err := parent.Stack.PopInt()
			if err != nil || exitCode.Int64() != 0 {
				t.Fatalf("child exit = %v, error = %v", exitCode, err)
			}
			var transferred tuple.Tuple
			var expectedTrace *cell.Trace
			if location == "stack" {
				transferred, err = parent.Stack.PopTuple()
				if err != nil {
					t.Fatal(err)
				}
				expectedTrace = parent.Cells.Trace()
			} else {
				returned, err := parent.Stack.PopContinuation()
				if err != nil {
					t.Fatal(err)
				}
				cont := returned.(*OrdinaryContinuation)
				transferred = cont.Data.Stack.elems[0].(tuple.Tuple)
				if cont.Data.Save.C7 != transferred {
					t.Fatal("tuple shared by captured stack and saved c7 was copied separately")
				}
				checkTraceTupleDAG(t, cont.Data.Save.C7, 16, nil)
			}
			slice, builder := checkTraceTupleDAG(t, transferred, 16, expectedTrace)
			if transferred == original || slice == originalSlice || builder == originalBuilder {
				t.Fatal("returned tuple retained child-owned mutable values")
			}
			slice.MustLoadUInt(1)
			builder.MustStoreUInt(1, 1)
			checkTraceTupleDAG(t, original, 16, childState.Cells.Trace())
			if originalSlice.BitsLeft() != 2 || originalBuilder.BitsUsed() != 1 {
				t.Fatal("parent mutation changed completed child's tuple leaves")
			}
		})
	}
}

func traceTupleDAG(trace *cell.Trace, depth int) (tuple.Tuple, *cell.Slice, *cell.Builder) {
	slice := cell.BeginCell().MustStoreUInt(3, 2).EndCell().MustBeginParse().SetTrace(trace)
	builder := cell.BeginCell().MustStoreUInt(1, 1).SetTrace(trace)
	root := tuple.NewTupleOwnedBound([]any{slice, builder}, trace)
	for i := 0; i < depth; i++ {
		root = tuple.NewTupleOwnedBound([]any{root, root}, trace)
	}
	return root, slice, builder
}

func checkTraceTupleDAG(t *testing.T, root tuple.Tuple, depth int, trace *cell.Trace) (*cell.Slice, *cell.Builder) {
	t.Helper()
	for level := 0; level < depth; level++ {
		left, err := root.RawIndex(0)
		if err != nil {
			t.Fatalf("DAG level %d left: %v", level, err)
		}
		right, err := root.RawIndex(1)
		if err != nil {
			t.Fatalf("DAG level %d right: %v", level, err)
		}
		leftTuple, leftOK := left.(tuple.Tuple)
		rightTuple, rightOK := right.(tuple.Tuple)
		if !leftOK || !rightOK || leftTuple != rightTuple {
			t.Fatalf("DAG level %d duplicated its shared tuple node", level)
		}
		root = leftTuple
	}
	sliceValue, err := root.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	builderValue, err := root.RawIndex(1)
	if err != nil {
		t.Fatal(err)
	}
	slice, sliceOK := sliceValue.(*cell.Slice)
	builder, builderOK := builderValue.(*cell.Builder)
	if !sliceOK || !builderOK {
		t.Fatalf("DAG leaves = %T and %T, want slice and builder", sliceValue, builderValue)
	}
	if slice.Trace() != trace || builder.Trace() != trace {
		t.Fatal("DAG mutable leaves have incorrect execution traces")
	}
	return slice, builder
}
