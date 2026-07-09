package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func makeStateC7WithParams(params tuple.Tuple) tuple.Tuple {
	return tuple.NewTupleValue(params)
}

func makeStateWithParams(t *testing.T, params ...any) *State {
	t.Helper()
	inner := tuple.NewTupleSized(len(params))
	for i, val := range params {
		if err := inner.Set(i, val); err != nil {
			t.Fatalf("set param %d: %v", i, err)
		}
	}
	return NewExecutionState(MaxSupportedGlobalVersion, NewGas(GasConfig{Max: 1_000, Limit: 1_000}), cell.BeginCell().EndCell(), makeStateC7WithParams(inner), NewStack())
}

func TestNewExecutionStateKeepsExplicitZero(t *testing.T) {
	explicitZero := NewExecutionState(0, NewGas(), nil, tuple.Tuple{}, NewStack())
	explicitZero.InitForExecution()
	if explicitZero.GlobalVersion != 0 {
		t.Fatalf("explicit zero global version = %d, want 0", explicitZero.GlobalVersion)
	}
}

func TestRunChildInheritsExplicitZeroGlobalVersion(t *testing.T) {
	parent := NewExecutionState(0, NewGas(), nil, tuple.Tuple{}, NewStack())
	parent.SetChildRunner(func(child *State) (int64, error) {
		if child.GlobalVersion != 0 {
			t.Fatalf("child global version = %d, want explicit 0", child.GlobalVersion)
		}
		return 0, nil
	})

	child := NewExecutionState(0, NewGas(), nil, tuple.Tuple{}, NewStack())
	child.CurrentCode = cell.BeginCell().EndCell().MustBeginParse()
	if _, err := parent.RunChild(child); err != nil {
		t.Fatalf("run child: %v", err)
	}
}

func TestStateRunChildAndParamHelpers(t *testing.T) {
	t.Run("RunChildValidationAndInheritance", func(t *testing.T) {
		parent := makeStateWithParams(t)
		parent.Libraries = []*cell.Cell{cell.BeginCell().MustStoreUInt(1, 1).EndCell()}
		parent.SignatureCheckAlwaysSucceed = true

		if _, err := parent.RunChild(nil); err == nil {
			t.Fatal("expected nil child to fail")
		}

		child := NewExecutionState(0, NewGas(), nil, tuple.Tuple{}, NewStack())
		child.CurrentCode = cell.BeginCell().EndCell().MustBeginParse()
		if _, err := child.RunChild(child); err == nil {
			t.Fatal("expected missing child runner to fail")
		}

		called := false
		parent.SetChildRunner(func(ch *State) (int64, error) {
			called = true
			if ch.GlobalVersion != MaxSupportedGlobalVersion {
				t.Fatalf("child global version = %d, want %d", ch.GlobalVersion, MaxSupportedGlobalVersion)
			}
			if len(ch.Libraries) != 1 {
				t.Fatalf("child libraries len = %d, want 1", len(ch.Libraries))
			}
			if !ch.SignatureCheckAlwaysSucceed {
				t.Fatal("child should inherit signature-check-always-succeed flag")
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

	t.Run("RunChildNormalizesVMExitErrors", func(t *testing.T) {
		parent := makeStateWithParams(t)
		child := NewExecutionState(0, NewGas(), nil, tuple.Tuple{}, NewStack())
		child.CurrentCode = cell.BeginCell().EndCell().MustBeginParse()

		parent.SetChildRunner(func(*State) (int64, error) {
			return 33, vmerr.Error(33)
		})

		got, err := parent.RunChild(child)
		if err != nil {
			t.Fatalf("run child should not return vm exit as error: %v", err)
		}
		if got != 33 {
			t.Fatalf("unexpected child exit code: %d", got)
		}
	})

	t.Run("ParamsAndGlobals", func(t *testing.T) {
		params := tuple.NewTupleSized(15)
		if err := params.Set(4, "blocklt"); err != nil {
			t.Fatal(err)
		}
		if err := params.Set(14, tuple.NewTupleValue("cfg")); err != nil {
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
		for i := 0; i < SignatureCheckFreeCount; i++ {
			if err := state.RegisterSignatureCheckCall(); err != nil {
				t.Fatalf("free signature check call %d failed: %v", i, err)
			}
		}
		before = state.Gas.Remaining
		if err := state.RegisterSignatureCheckCall(); err != nil {
			t.Fatal(err)
		}
		if state.Gas.Remaining != before-SignatureCheckGasPrice {
			t.Fatalf("paid signature check should consume gas: before=%d after=%d", before, state.Gas.Remaining)
		}

		legacySignatureCheck := NewExecutionState(3, GasWithLimit(1000), nil, tuple.Tuple{}, NewStack())
		if err := legacySignatureCheck.RegisterSignatureCheckCall(); err != nil {
			t.Fatalf("pre-v4 signature check call should be free: %v", err)
		}
		if legacySignatureCheck.SignatureCheckCounter != 0 || legacySignatureCheck.Gas.FreeConsumed != 0 || legacySignatureCheck.Gas.Used() != 0 {
			t.Fatalf("pre-v4 signature check mutated state: counter=%d free=%d used=%d", legacySignatureCheck.SignatureCheckCounter, legacySignatureCheck.Gas.FreeConsumed, legacySignatureCheck.Gas.Used())
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

		legacyGas := NewExecutionState(3, GasWithLimit(5), nil, tuple.Tuple{}, NewStack())
		if err := legacyGas.ConsumeGas(10); err != nil {
			t.Fatalf("pre-v4 consume gas should defer out-of-gas: %v", err)
		}
		assertVMErrorCode(t, legacyGas.CheckGas(), vmerr.CodeOutOfGas)

		legacyCheckedGas := NewExecutionState(3, GasWithLimit(5), nil, tuple.Tuple{}, NewStack())
		assertVMErrorCode(t, legacyCheckedGas.consumeGasChecked(10), vmerr.CodeOutOfGas)

		modernGas := NewExecutionState(4, GasWithLimit(5), nil, tuple.Tuple{}, NewStack())
		assertVMErrorCode(t, modernGas.ConsumeGas(10), vmerr.CodeOutOfGas)
	})

	t.Run("CommitHelpers", func(t *testing.T) {
		commitState := makeStateWithParams(t)
		commitState.Reg.D[0] = nil
		if commitState.TryCommitCurrent() {
			t.Fatal("nil data register should prevent commit")
		}

		levelCell := mustPrunedCell(t)
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

func FuzzStateVersionedGasConsumptionBoundary(f *testing.F) {
	for version := uint8(0); version <= uint8(MaxSupportedGlobalVersion); version++ {
		for entrypoint := uint8(0); entrypoint < 4; entrypoint++ {
			f.Add(version, false, entrypoint, uint16(5), uint16(10))
			f.Add(version, false, entrypoint, uint16(10), uint16(5))
			f.Add(version, false, entrypoint, uint16(32), uint16(33))
			f.Add(version, false, entrypoint, uint16(33), uint16(32))
		}
	}
	for entrypoint := uint8(0); entrypoint < 4; entrypoint++ {
		f.Add(uint8(0), true, entrypoint, uint16(5), uint16(10))
		f.Add(uint8(0), true, entrypoint, uint16(10), uint16(5))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, unconfiguredZero bool, rawEntrypoint uint8, rawLimit, rawAmount uint16) {
		version := int(rawVersion % uint8(MaxSupportedGlobalVersion+1))
		limit := int64(rawLimit%64) + 1
		amount := int64(rawAmount % 96)
		entrypoint := rawEntrypoint % 4

		var state *State
		if unconfiguredZero {
			state = NewExecutionState(0, GasWithLimit(limit), nil, tuple.Tuple{}, NewStack())
		} else {
			state = NewExecutionState(version, GasWithLimit(limit), nil, tuple.Tuple{}, NewStack())
		}

		cost, err := consumeVersionedGasFuzzEntry(state, entrypoint, amount)
		checkNow := (!unconfiguredZero && version >= 4) || entrypoint == 3
		wantErr := checkNow && cost > limit

		if wantErr {
			assertVMErrorCode(t, err, vmerr.CodeOutOfGas)
		} else if err != nil {
			t.Fatalf("version=%d unconfigured=%v entry=%d limit=%d amount=%d unexpected consume error: %v", version, unconfiguredZero, entrypoint, limit, amount, err)
		}

		if got := state.Gas.Used(); got != cost {
			t.Fatalf("version=%d unconfigured=%v entry=%d used gas = %d, want %d", version, unconfiguredZero, entrypoint, got, cost)
		}

		checkErr := state.CheckGas()
		if cost > limit {
			assertVMErrorCode(t, checkErr, vmerr.CodeOutOfGas)
			return
		}
		if checkErr != nil {
			t.Fatalf("version=%d unconfigured=%v entry=%d CheckGas unexpected error: %v", version, unconfiguredZero, entrypoint, checkErr)
		}
	})
}

func consumeVersionedGasFuzzEntry(state *State, entrypoint uint8, amount int64) (int64, error) {
	switch entrypoint {
	case 0:
		return amount, state.ConsumeGas(amount)
	case 1:
		depth := int(amount)
		cost := int64(0)
		if depth > FreeStackDepth {
			cost = int64(depth-FreeStackDepth) * StackEntryGasPrice
		}
		return cost, state.ConsumeStackGasLen(depth)
	case 2:
		return amount * TupleEntryGasPrice, state.ConsumeTupleGasLen(int(amount))
	default:
		return amount, state.consumeGasChecked(amount)
	}
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
			Code:         cell.BeginCell().EndCell().MustBeginParse(),
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
