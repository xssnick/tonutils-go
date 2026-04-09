package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func makeStateC7WithParams(params tuple.Tuple) tuple.Tuple {
	return *tuple.NewTuple(params)
}

func makeStateWithParams(t *testing.T, params ...any) *State {
	t.Helper()
	inner := tuple.NewTupleSized(len(params))
	for i, val := range params {
		if err := inner.Set(i, val); err != nil {
			t.Fatalf("set param %d: %v", i, err)
		}
	}
	return NewExecutionState(0, NewGas(GasConfig{Max: 1_000, Limit: 1_000}), cell.BeginCell().EndCell(), makeStateC7WithParams(inner), NewStack())
}

func TestStateRunChildAndParamHelpers(t *testing.T) {
	t.Run("RunChildValidationAndInheritance", func(t *testing.T) {
		parent := makeStateWithParams(t)
		parent.Libraries = []*cell.Cell{cell.BeginCell().MustStoreUInt(1, 1).EndCell()}

		if _, err := parent.RunChild(nil); err == nil {
			t.Fatal("expected nil child to fail")
		}

		child := NewExecutionState(0, NewGas(), nil, tuple.Tuple{}, NewStack())
		child.CurrentCode = cell.BeginCell().EndCell().BeginParseNoCopy()
		if _, err := child.RunChild(child); err == nil {
			t.Fatal("expected missing child runner to fail")
		}

		called := false
		parent.SetChildRunner(func(ch *State) (int64, error) {
			called = true
			if ch.GlobalVersion != DefaultGlobalVersion {
				t.Fatalf("child global version = %d, want %d", ch.GlobalVersion, DefaultGlobalVersion)
			}
			if len(ch.Libraries) != 1 {
				t.Fatalf("child libraries len = %d, want 1", len(ch.Libraries))
			}
			if ch.CurrentCode == nil {
				t.Fatal("child code should be prepared before runner")
			}
			return 17, nil
		})

		got, err := parent.RunChild(child)
		if err != nil {
			t.Fatal(err)
		}
		if !called || got != 17 {
			t.Fatalf("unexpected child result: called=%v exit=%d", called, got)
		}
	})

	t.Run("ParamsAndGlobals", func(t *testing.T) {
		params := tuple.NewTupleSized(15)
		if err := params.Set(4, "blocklt"); err != nil {
			t.Fatal(err)
		}
		if err := params.Set(14, *tuple.NewTuple("cfg")); err != nil {
			t.Fatal(err)
		}

		state := NewExecutionState(0, NewGas(GasConfig{Max: 1_000, Limit: 1_000}), cell.BeginCell().EndCell(), makeStateC7WithParams(params), NewStack())

		param, err := state.GetParam(4)
		if err != nil {
			t.Fatal(err)
		}
		if param.(string) != "blocklt" {
			t.Fatalf("unexpected param: %v", param)
		}

		cfg, err := state.GetUnpackedConfigTuple()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Len() != 1 {
			t.Fatalf("unexpected unpacked config len: %d", cfg.Len())
		}

		if got, err := state.GetGlobal(99); err != nil || got != nil {
			t.Fatalf("unexpected absent global: got=%v err=%v", got, err)
		}
		if _, err := state.GetGlobal(-1); err == nil {
			t.Fatal("negative global index should fail")
		}

		if err := state.SetGlobal(2, "global"); err != nil {
			t.Fatal(err)
		}
		got, err := state.GetGlobal(2)
		if err != nil {
			t.Fatal(err)
		}
		if got.(string) != "global" {
			t.Fatalf("unexpected global value: %v", got)
		}

		before := state.Reg.C7.Len()
		if err := state.SetGlobal(20, nil); err != nil {
			t.Fatal(err)
		}
		if state.Reg.C7.Len() != before {
			t.Fatalf("nil global assignment should not resize tuple: before=%d after=%d", before, state.Reg.C7.Len())
		}
		if err := state.SetGlobal(300, 1); err == nil {
			t.Fatal("out-of-range global index should fail")
		}
	})
}

func TestStateGasCommitAndThrowHelpers(t *testing.T) {
	state := makeStateWithParams(t, "param0")

	t.Run("GasHelpers", func(t *testing.T) {
		state.Cells.pendingErr = errors.New("pending")
		if err := state.CheckGas(); err == nil || err.Error() != "pending" {
			t.Fatalf("unexpected pending error: %v", err)
		}
		state.Cells.pendingErr = nil

		before := state.Gas.Remaining
		state.ConsumeFreeGas(7)
		if err := state.FlushFreeGas(); err != nil {
			t.Fatal(err)
		}
		if state.Gas.Remaining != before-7 {
			t.Fatalf("flush free gas remaining = %d, want %d", state.Gas.Remaining, before-7)
		}

		state.Gas.SetLimits(100_000, 100_000)
		for i := 0; i < ChksgnFreeCount; i++ {
			if err := state.RegisterChksgnCall(); err != nil {
				t.Fatalf("free chksgn call %d failed: %v", i, err)
			}
		}
		before = state.Gas.Remaining
		if err := state.RegisterChksgnCall(); err != nil {
			t.Fatal(err)
		}
		if state.Gas.Remaining != before-ChksgnGasPrice {
			t.Fatalf("paid chksgn should consume gas: before=%d after=%d", before, state.Gas.Remaining)
		}

		state.Gas.SetLimits(100, 100)
		if err := state.ConsumeGas(10); err != nil {
			t.Fatal(err)
		}
		if err := state.SetGasLimit(5); err == nil {
			t.Fatal("lowering gas below used amount should fail")
		}
		if err := state.SetGasLimit(40); err != nil {
			t.Fatal(err)
		}
		if state.Gas.Limit != 40 || state.Gas.Credit != 0 {
			t.Fatalf("unexpected gas state after limit change: %+v", state.Gas)
		}

		if err := state.Stack.PushInt(big.NewInt(99)); err != nil {
			t.Fatal(err)
		}
		if err := state.HandleOutOfGas(); err != nil {
			t.Fatal(err)
		}
		if state.Stack.Len() != 1 {
			t.Fatalf("stack len after out-of-gas = %d, want 1", state.Stack.Len())
		}
		used, err := state.Stack.PopInt()
		if err != nil {
			t.Fatal(err)
		}
		if used.Int64() != state.Gas.Used() {
			t.Fatalf("unexpected used gas on stack: got=%d want=%d", used.Int64(), state.Gas.Used())
		}
	})

	t.Run("CommitHelpers", func(t *testing.T) {
		commitState := makeStateWithParams(t)
		commitState.Reg.D[0] = nil
		if commitState.TryCommitCurrent() {
			t.Fatal("nil data register should prevent commit")
		}

		levelCell := cell.FromRawUnsafe(cell.RawUnsafeCell{
			LevelMask: cell.LevelMask{Mask: 1},
		})
		commitState.Reg.D[0] = levelCell
		commitState.Reg.D[1] = cell.BeginCell().EndCell()
		if commitState.TryCommitCurrent() {
			t.Fatal("non-zero level should prevent commit")
		}
		if err := commitState.ForceCommitCurrent(); err == nil {
			t.Fatal("force commit should fail for invalid data")
		}

		commitState.Reg.D[0] = cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		commitState.Reg.D[1] = cell.BeginCell().EndCell()
		if !commitState.TryCommitCurrent() {
			t.Fatal("ordinary cells should commit")
		}
		if !commitState.Committed.Committed {
			t.Fatal("committed flag should be set")
		}
		if err := commitState.ForceCommitCurrent(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ThrowException", func(t *testing.T) {
		throwState := makeStateWithParams(t)
		throwState.CurrentCode = cell.BeginCell().MustStoreUInt(0xFF, 8).ToSlice()

		if err := throwState.ThrowException(big.NewInt(7), "a", "b"); err == nil {
			t.Fatal("too many exception args should fail")
		}

		payload := big.NewInt(44)
		err := throwState.ThrowException(big.NewInt(33), payload)
		var handled HandledException
		if !errors.As(err, &handled) || handled.Code != 33 {
			t.Fatalf("unexpected exception result: %v", err)
		}
		if throwState.CurrentCode.BitsLeft() != 0 {
			t.Fatal("throw should reset current code slice")
		}

		got, err := throwState.Stack.PopAny()
		if err != nil {
			t.Fatal(err)
		}
		if got.(*big.Int).Cmp(payload) != 0 {
			t.Fatalf("unexpected exception payload: %v", got)
		}
	})
}

func TestChildVMHelpersAndForceControlData(t *testing.T) {
	t.Run("PrimitiveHelpers", func(t *testing.T) {
		parent := NewStack()
		if err := pushMaybeCell(parent, nil); err != nil {
			t.Fatal(err)
		}
		if got, err := parent.PopAny(); err != nil || got != nil {
			t.Fatalf("unexpected nil push result: got=%v err=%v", got, err)
		}

		cl := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		if err := pushMaybeCell(parent, cl); err != nil {
			t.Fatal(err)
		}
		popped, err := parent.PopCell()
		if err != nil {
			t.Fatal(err)
		}
		if string(popped.Hash()) != string(cl.Hash()) {
			t.Fatal("pushed cell should round-trip")
		}

		src := NewStack()
		for _, v := range []int64{1, 2, 3} {
			if err := src.PushInt(big.NewInt(v)); err != nil {
				t.Fatal(err)
			}
		}
		if err := copyTopValuesToParent(parent, src, 2); err != nil {
			t.Fatal(err)
		}
		if parent.Len() != 2 {
			t.Fatalf("unexpected copied values count: %d", parent.Len())
		}
		top, _ := parent.PopInt()
		next, _ := parent.PopInt()
		if top.Int64() != 3 || next.Int64() != 2 {
			t.Fatalf("unexpected copied order: top=%d next=%d", top.Int64(), next.Int64())
		}

		if err := pushCommittedResultCell(parent, false, cl); err != nil {
			t.Fatal(err)
		}
		if got, _ := parent.PopAny(); got != nil {
			t.Fatalf("uncommitted result should push nil, got %v", got)
		}
	})

	t.Run("RunChildVMValidation", func(t *testing.T) {
		state := makeStateWithParams(t)
		if err := state.RunChildVM(ChildVMConfig{}); err == nil {
			t.Fatal("nil child code should fail")
		}
		if err := state.RunChildVM(ChildVMConfig{
			Code:         cell.BeginCell().EndCell().BeginParse(),
			ReturnValues: -2,
		}); err == nil {
			t.Fatal("invalid return values should fail")
		}
	})

	t.Run("ForceControlDataWrapsPlainContinuation", func(t *testing.T) {
		plain := &ExcQuitContinuation{}
		wrapped := ForceControlData(plain)
		if _, ok := wrapped.(*ArgExtContinuation); !ok {
			t.Fatalf("expected plain continuation to be wrapped, got %T", wrapped)
		}
		if ForceControlData(wrapped) != wrapped {
			t.Fatal("continuation with control data should not be wrapped again")
		}
	})
}
