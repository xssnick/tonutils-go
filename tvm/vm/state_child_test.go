package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func assertVMErrorCode(t *testing.T, err error, code int64) {
	t.Helper()
	got, ok := vmerr.ErrorCode(err)
	if !ok {
		t.Fatalf("expected VM-like error code %d, got %T (%v)", code, err, err)
	}
	if got != code {
		t.Fatalf("vm error code = %d, want %d", got, code)
	}
}

func buildDeepCell(depth int) *cell.Cell {
	cl := cell.BeginCell().EndCell()
	for i := 0; i < depth; i++ {
		cl = cell.BeginCell().MustStoreRef(cl).EndCell()
	}
	return cl
}

func mustPopInt64(t *testing.T, s *Stack) int64 {
	t.Helper()

	v, err := s.PopInt()
	if err != nil {
		t.Fatalf("pop int: %v", err)
	}
	return v.Int64()
}

func TestRegisterHelpersAndNewExecutionState(t *testing.T) {
	data := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	lib := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	c7 := tuple.NewTupleValue("params")

	reg := Register{}
	if !reg.Define(0, &QuitContinuation{ExitCode: 7}) {
		t.Fatal("expected continuation define to succeed")
	}
	if !reg.Define(4, data) {
		t.Fatal("expected cell define to succeed")
	}
	if !reg.Define(7, c7) {
		t.Fatal("expected tuple define to succeed")
	}
	if reg.Define(1, "bad") {
		t.Fatal("unexpected success for invalid continuation type")
	}
	if reg.Define(-1, data) {
		t.Fatal("unexpected success for negative register index")
	}

	if _, ok := reg.Get(0).(Continuation); !ok {
		t.Fatalf("register 0 has unexpected type %T", reg.Get(0))
	}
	if got := reg.Get(4); got != data {
		t.Fatalf("register 4 = %v, want data cell", got)
	}
	if got := reg.Get(7).(tuple.Tuple); got.Len() != 1 {
		t.Fatalf("register 7 len = %d, want 1", got.Len())
	}
	if _, ok := reg.Get(99).(Null); !ok {
		t.Fatalf("unexpected invalid register value type %T", reg.Get(99))
	}

	cp := reg.Copy()
	cpCont := cp.C[0].(*QuitContinuation)
	cpCont.ExitCode = 99
	if reg.C[0].(*QuitContinuation).ExitCode != 7 {
		t.Fatal("register copy should clone continuations")
	}

	update := Register{}
	update.C[1] = &QuitContinuation{ExitCode: 8}
	update.D[1] = lib
	update.C7 = tuple.NewTupleValue("globals")
	reg.AdjustWith(&update)

	if reg.C[1] == nil || reg.D[1] != lib || reg.C7.Len() != 1 {
		t.Fatal("adjust with should copy only non-empty fields")
	}

	libs := []*cell.Cell{lib}
	st := NewExecutionState(0, GasWithLimit(1000), nil, c7, NewStack(), libs...)
	libs[0] = nil

	if st.Reg.D[0] == nil || st.Reg.D[0].BitsSize() != 0 {
		t.Fatal("state should replace nil data with an empty cell")
	}
	if st.Reg.D[1] == nil || st.Reg.D[1].BitsSize() != 0 {
		t.Fatal("state should initialize actions cell")
	}
	if len(st.Libraries) != 1 || st.Libraries[0] != lib {
		t.Fatal("state should copy libraries slice")
	}
	if st.Reg.C[2] == nil || st.Reg.C[3] == nil {
		t.Fatal("state should initialize control continuations")
	}
}

func TestStateParamsGlobalsAndGasHelpers(t *testing.T) {
	params := tuple.NewTupleSized(15)
	if err := params.Set(1, big.NewInt(17)); err != nil {
		t.Fatalf("set param 1: %v", err)
	}
	cfgTuple := tuple.NewTupleValue("cfg")
	if err := params.Set(14, cfgTuple); err != nil {
		t.Fatalf("set param 14: %v", err)
	}

	st := NewExecutionState(0, GasWithLimit(1_000_000), cell.BeginCell().EndCell(), tuple.NewTupleValue(params), NewStack())
	st.PrepareExecution(cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse())

	if st.GlobalVersion != DefaultGlobalVersion {
		t.Fatalf("global version = %d, want %d", st.GlobalVersion, DefaultGlobalVersion)
	}
	if st.CurrentCode == nil {
		t.Fatal("current code should be prepared")
	}

	paramAny, err := st.GetParam(1)
	if err != nil {
		t.Fatalf("get param: %v", err)
	}
	param := paramAny.(*big.Int)
	param.Add(param, big.NewInt(1))

	rawParamsAny, err := st.Reg.C7.Index(0)
	if err != nil {
		t.Fatalf("get params tuple: %v", err)
	}
	rawParams := rawParamsAny.(tuple.Tuple)
	rawParamAny, err := rawParams.RawIndex(1)
	if err != nil {
		t.Fatalf("raw param index: %v", err)
	}
	if rawParamAny.(*big.Int).Int64() != 17 {
		t.Fatalf("raw param changed to %d", rawParamAny.(*big.Int).Int64())
	}

	unpacked, err := st.GetUnpackedConfigTuple()
	if err != nil {
		t.Fatalf("get unpacked config tuple: %v", err)
	}
	if unpacked.Len() != 1 {
		t.Fatalf("config tuple len = %d, want 1", unpacked.Len())
	}

	badParamState := NewExecutionState(DefaultGlobalVersion, GasWithLimit(1000), nil, tuple.NewTupleValue("not-tuple"), NewStack())
	if _, err = badParamState.GetParam(0); err == nil {
		t.Fatal("expected type check for non-tuple params")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}
	if _, err = badParamState.GetUnpackedConfigTuple(); err == nil {
		t.Fatal("expected get unpacked config tuple to fail")
	}

	if _, err = st.GetGlobal(-1); err == nil {
		t.Fatal("expected range check for negative global index")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeRangeCheck)
	}
	if got, err := st.GetGlobal(200); err != nil || got != nil {
		t.Fatalf("get missing global = (%v, %v), want (nil, nil)", got, err)
	}

	if err = st.SetGlobal(3, big.NewInt(33)); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if used := st.Gas.Used(); used != 4 {
		t.Fatalf("gas used after set global = %d, want 4", used)
	}

	globalAny, err := st.GetGlobal(3)
	if err != nil {
		t.Fatalf("get global 3: %v", err)
	}
	globalVal := globalAny.(*big.Int)
	globalVal.Add(globalVal, big.NewInt(1))

	globalAny, err = st.GetGlobal(3)
	if err != nil {
		t.Fatalf("get global 3 again: %v", err)
	}
	if globalAny.(*big.Int).Int64() != 33 {
		t.Fatalf("stored global changed to %d", globalAny.(*big.Int).Int64())
	}

	if err = st.SetGlobal(10, nil); err != nil {
		t.Fatalf("set missing nil global: %v", err)
	}
	if used := st.Gas.Used(); used != 4 {
		t.Fatalf("gas used after nil no-op = %d, want 4", used)
	}

	updateErr := errors.New("update failed")
	if err = st.UpdateC7(func(tuple.Tuple) (tuple.Tuple, error) {
		return tuple.Tuple{}, updateErr
	}); !errors.Is(err, updateErr) {
		t.Fatalf("update c7 error = %v, want %v", err, updateErr)
	}

	if err = st.UpdateC7(func(t tuple.Tuple) (tuple.Tuple, error) {
		if err := t.Set(1, big.NewInt(99)); err != nil {
			return tuple.Tuple{}, err
		}
		return t, nil
	}); err != nil {
		t.Fatalf("update c7 success: %v", err)
	}
	updatedAny, err := st.GetGlobal(1)
	if err != nil {
		t.Fatalf("get updated global: %v", err)
	}
	if updatedAny.(*big.Int).Int64() != 99 {
		t.Fatalf("updated global = %d, want 99", updatedAny.(*big.Int).Int64())
	}

	st.ConsumeFreeGas(5)
	if err = st.FlushFreeGas(); err != nil {
		t.Fatalf("flush free gas: %v", err)
	}
	if used := st.Gas.Used(); used != 9 {
		t.Fatalf("gas used after flush = %d, want 9", used)
	}

	for i := 0; i < ChksgnFreeCount; i++ {
		if err = st.RegisterChksgnCall(); err != nil {
			t.Fatalf("register chksgn call %d: %v", i, err)
		}
	}
	if st.Gas.FreeConsumed != int64(ChksgnFreeCount)*ChksgnGasPrice {
		t.Fatalf("free consumed = %d, want %d", st.Gas.FreeConsumed, int64(ChksgnFreeCount)*ChksgnGasPrice)
	}

	if err = st.RegisterChksgnCall(); err != nil {
		t.Fatalf("register paid chksgn call: %v", err)
	}
	if used := st.Gas.Used(); used != 9+ChksgnGasPrice {
		t.Fatalf("gas used after paid chksgn = %d, want %d", used, 9+ChksgnGasPrice)
	}

	if err = st.ConsumeStackGas(nil); err != nil {
		t.Fatalf("consume nil stack gas: %v", err)
	}
	lowGasState := NewExecutionState(DefaultGlobalVersion, Gas{}, nil, tuple.Tuple{}, NewStack())
	if err = lowGasState.PushTupleCharged(tuple.NewTupleValue("x")); err == nil {
		t.Fatal("expected tuple push to fail when gas is exhausted")
	}
	if err = st.SetGasLimit(st.Gas.Used() - 1); err == nil {
		t.Fatal("expected set gas limit below used gas to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeOutOfGas)
	}
	if err = st.SetGasLimit(10_000); err != nil {
		t.Fatalf("set gas limit: %v", err)
	}
	if st.Gas.Limit != 10_000 {
		t.Fatalf("gas limit = %d, want 10000", st.Gas.Limit)
	}

	st.Cells.pendingErr = errors.New("pending cell error")
	if err = st.CheckGas(); err == nil || err.Error() != "pending cell error" {
		t.Fatalf("check gas pending error = %v", err)
	}
	st.Cells.pendingErr = nil
	st.Gas.Remaining = -1
	if err = st.CheckGas(); err == nil {
		t.Fatal("expected out of gas check failure")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeOutOfGas)
	}

	pushInts(t, st.Stack, 1, 2, 3)
	if err = st.HandleOutOfGas(); err != nil {
		t.Fatalf("handle out of gas: %v", err)
	}
	if st.Stack.Len() != 1 {
		t.Fatalf("stack len after handle out of gas = %d, want 1", st.Stack.Len())
	}
	if got := mustPopInt64(t, st.Stack); got != st.Gas.Used() {
		t.Fatalf("reported used gas = %d, want %d", got, st.Gas.Used())
	}
}

func TestStateCommitThrowAndRunChild(t *testing.T) {
	st := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), cell.BeginCell().MustStoreUInt(1, 8).EndCell(), tuple.Tuple{}, NewStack())
	st.Reg.D[1] = cell.BeginCell().MustStoreUInt(2, 8).EndCell()

	if !st.TryCommitCurrent() {
		t.Fatal("expected commit to succeed")
	}
	if !st.Committed.Committed || st.Committed.Data == nil || st.Committed.Actions == nil {
		t.Fatal("committed state should be populated")
	}

	deepState := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), buildDeepCell(MaxDataDepth+1), tuple.Tuple{}, NewStack())
	deepState.Reg.D[1] = cell.BeginCell().EndCell()
	if deepState.TryCommitCurrent() {
		t.Fatal("expected deep data commit to fail")
	}
	if err := deepState.ForceCommitCurrent(); err == nil {
		t.Fatal("expected force commit of deep data to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeCellOverflow)
	}

	levelCell := mustPrunedCell(t)
	levelState := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), levelCell, tuple.Tuple{}, NewStack())
	levelState.Reg.D[1] = cell.BeginCell().EndCell()
	if levelState.TryCommitCurrent() {
		t.Fatal("expected non-zero level commit to fail")
	}

	throwState := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	pushInts(t, throwState.Stack, 7, 8, 9)
	err := throwState.ThrowException(big.NewInt(77), big.NewInt(5))
	if err == nil {
		t.Fatal("expected handled exception")
	}
	var handled HandledException
	if !errors.As(err, &handled) {
		t.Fatalf("expected handled exception, got %T", err)
	}
	if throwState.Stack.Len() != 1 || mustPopInt64(t, throwState.Stack) != 5 {
		t.Fatal("throw exception should keep only the provided argument on stack")
	}

	noArgState := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	if err = noArgState.ThrowException(big.NewInt(88)); err == nil {
		t.Fatal("expected handled exception without argument")
	}
	if noArgState.Stack.Len() != 1 || mustPopInt64(t, noArgState.Stack) != 0 {
		t.Fatal("throw exception without argument should leave zero on stack")
	}

	tooManyArgsState := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	if err = tooManyArgsState.ThrowException(big.NewInt(1), big.NewInt(1), big.NewInt(2)); err == nil {
		t.Fatal("expected too many arguments error")
	}

	if !IsSuccessExitCode(0) || !IsSuccessExitCode(1) || IsSuccessExitCode(2) {
		t.Fatal("unexpected success exit code classification")
	}

	forced := ForceControlData(&QuitContinuation{ExitCode: 1})
	argExt, ok := forced.(*ArgExtContinuation)
	if !ok || argExt.Data.NumArgs != ControlDataAllArgs || argExt.Data.CP != CP {
		t.Fatalf("force control data returned %#v", forced)
	}
	ordinary := &OrdinaryContinuation{}
	if ForceControlData(ordinary) != ordinary {
		t.Fatal("continuation with control data should be returned as-is")
	}

	parent := NewExecutionState(77, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack(), cell.BeginCell().MustStoreUInt(3, 8).EndCell())
	if _, err = parent.RunChild(nil); err == nil {
		t.Fatal("expected nil child to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeFatal)
	}

	child := NewExecutionState(0, GasWithLimit(1000), nil, tuple.Tuple{}, NewStack())
	child.CurrentCode = cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell().MustBeginParse()
	if _, err = child.RunChild(child); err == nil {
		t.Fatal("expected child runner to be required")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeFatal)
	}

	parent.SetChildRunner(func(child *State) (int64, error) {
		if child.GlobalVersion != 77 {
			t.Fatalf("child global version = %d, want 77", child.GlobalVersion)
		}
		if len(child.Libraries) != 1 {
			t.Fatalf("child libraries len = %d, want 1", len(child.Libraries))
		}
		if child.childRunner == nil {
			t.Fatal("child runner should be inherited")
		}
		v, err := child.CurrentCode.Copy().LoadUInt(8)
		if err != nil {
			t.Fatalf("read child current code: %v", err)
		}
		if v != 0xEF {
			t.Fatalf("child opcode = %x, want ef", v)
		}
		return 9, nil
	})

	exitCode, err := parent.RunChild(child)
	if err != nil {
		t.Fatalf("run child: %v", err)
	}
	if exitCode != 9 {
		t.Fatalf("child exit code = %d, want 9", exitCode)
	}
}

func TestChildVMHelpersAndExecution(t *testing.T) {
	parent := NewStack()
	refCell := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()

	if err := pushMaybeCell(parent, nil); err != nil {
		t.Fatalf("push maybe nil cell: %v", err)
	}
	if err := pushMaybeCell(parent, refCell); err != nil {
		t.Fatalf("push maybe cell: %v", err)
	}
	gotCell, err := parent.PopCell()
	if err != nil {
		t.Fatalf("pop pushed cell: %v", err)
	}
	if gotCell != refCell {
		t.Fatal("unexpected cell popped from stack")
	}
	gotNil, err := parent.PopAny()
	if err != nil {
		t.Fatalf("pop nil marker: %v", err)
	}
	if gotNil != nil {
		t.Fatalf("expected nil marker, got %T", gotNil)
	}

	childStack := NewStack()
	pushInts(t, childStack, 1, 2, 3)
	if err = copyTopValuesToParent(parent, childStack, 0); err != nil {
		t.Fatalf("copy zero top values: %v", err)
	}
	if err = copyTopValuesToParent(parent, childStack, 2); err != nil {
		t.Fatalf("copy top values: %v", err)
	}
	assertPopInts(t, parent, 3, 2)
	assertPopInts(t, childStack, 3, 2, 1)

	if err = pushCommittedResultCell(parent, false, refCell); err != nil {
		t.Fatalf("push uncommitted result: %v", err)
	}
	if v, err := parent.PopAny(); err != nil || v != nil {
		t.Fatalf("uncommitted result = (%v, %v), want (nil, nil)", v, err)
	}

	successParent := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	successParent.ChksigAlwaysSucceed = true
	successParent.SetChildRunner(func(child *State) (int64, error) {
		if child.Stack.Len() != 1 {
			t.Fatalf("child stack len before execution = %d, want 1", child.Stack.Len())
		}
		if !child.ChksigAlwaysSucceed {
			t.Fatal("child should inherit chksig-always-succeed flag")
		}
		if mustPopInt64(t, child.Stack) != 0 {
			t.Fatal("child stack should receive zero when PushZero is set")
		}
		if _, ok := child.Reg.C[3].(*OrdinaryContinuation); !ok {
			t.Fatalf("child c3 type = %T, want *OrdinaryContinuation", child.Reg.C[3])
		}

		pushInts(t, child.Stack, 10, 20)
		child.Committed = CommittedState{
			Data:      cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell(),
			Actions:   cell.BeginCell().MustStoreUInt(0xA0, 8).EndCell(),
			Committed: true,
		}
		child.Gas.Remaining = child.Gas.Base - 4
		child.Steps = 3
		child.ChksgnCounter = 2
		child.Gas.FreeConsumed = 7
		return 0, nil
	})

	if err = successParent.RunChildVM(ChildVMConfig{
		Code:          cell.BeginCell().MustStoreUInt(1, 8).EndCell().MustBeginParse(),
		Gas:           GasWithLimit(50),
		SameC3:        true,
		PushZero:      true,
		ReturnValues:  2,
		ReturnData:    true,
		ReturnActions: true,
		ReturnGas:     true,
	}); err != nil {
		t.Fatalf("run child vm success: %v", err)
	}

	if successParent.Steps != 3 || successParent.ChksgnCounter != 2 || successParent.Gas.FreeConsumed != 7 {
		t.Fatal("parent state should inherit child counters")
	}
	if used := successParent.Gas.Used(); used != 4 {
		t.Fatalf("parent gas used = %d, want 4", used)
	}

	if got := mustPopInt64(t, successParent.Stack); got != 4 {
		t.Fatalf("returned gas = %d, want 4", got)
	}
	actionCell, err := successParent.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop actions: %v", err)
	}
	if bits, _, _ := actionCell.MustBeginParse().RestBits(); bits != 8 {
		t.Fatalf("unexpected actions bits = %d", bits)
	}
	dataCell, err := successParent.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop data: %v", err)
	}
	if bits, _, _ := dataCell.MustBeginParse().RestBits(); bits != 8 {
		t.Fatalf("unexpected data bits = %d", bits)
	}
	if got := mustPopInt64(t, successParent.Stack); got != 0 {
		t.Fatalf("child exit code = %d, want 0", got)
	}
	assertPopInts(t, successParent.Stack, 20, 10)

	underflowParent := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	underflowParent.SetChildRunner(func(child *State) (int64, error) {
		pushInts(t, child.Stack, 99)
		return 0, nil
	})
	if err = underflowParent.RunChildVM(ChildVMConfig{
		Code:         cell.BeginCell().EndCell().MustBeginParse(),
		Gas:          GasWithLimit(10),
		ReturnValues: 2,
	}); err != nil {
		t.Fatalf("run child vm underflow branch: %v", err)
	}
	if got := mustPopInt64(t, underflowParent.Stack); got != ^int64(vmerr.CodeStackUnderflow) {
		t.Fatalf("underflow exit code = %d, want %d", got, ^int64(vmerr.CodeStackUnderflow))
	}
	if got := mustPopInt64(t, underflowParent.Stack); got != 0 {
		t.Fatalf("underflow marker = %d, want 0", got)
	}

	if err = underflowParent.RunChildVM(ChildVMConfig{}); err == nil {
		t.Fatal("expected nil child code to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeTypeCheck)
	}
	if err = underflowParent.RunChildVM(ChildVMConfig{
		Code:         cell.BeginCell().EndCell().MustBeginParse(),
		ReturnValues: -2,
	}); err == nil {
		t.Fatal("expected invalid return values count to fail")
	} else {
		assertVMErrorCode(t, err, vmerr.CodeRangeCheck)
	}

	propParent := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	expectedErr := errors.New("child failed")
	propParent.SetChildRunner(func(*State) (int64, error) {
		return 0, expectedErr
	})
	if err = propParent.RunChildVM(ChildVMConfig{
		Code: cell.BeginCell().EndCell().MustBeginParse(),
		Gas:  GasWithLimit(10),
	}); !errors.Is(err, expectedErr) {
		t.Fatalf("propagated child error = %v, want %v", err, expectedErr)
	}

	outOfGasParent := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	outOfGasParent.SetChildRunner(func(child *State) (int64, error) {
		pushInts(t, child.Stack, 42)
		child.Gas.Remaining = child.Gas.Base - (child.Gas.Limit + 5)
		return 5, vmerr.Error(vmerr.CodeOutOfGas)
	})
	if err = outOfGasParent.RunChildVM(ChildVMConfig{
		Code: cell.BeginCell().EndCell().MustBeginParse(),
		Gas:  GasWithLimit(5),
	}); err != nil {
		t.Fatalf("run child vm out of gas: %v", err)
	}
	if used := outOfGasParent.Gas.Used(); used != 6 {
		t.Fatalf("parent charged gas = %d, want 6", used)
	}
	if got := mustPopInt64(t, outOfGasParent.Stack); got != ^int64(5) {
		t.Fatalf("out of gas exit code = %d, want %d", got, ^int64(5))
	}
	if got := mustPopInt64(t, outOfGasParent.Stack); got != 42 {
		t.Fatalf("returned child value = %d, want 42", got)
	}

	isolatedParent := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	isolatedParent.ConsumeFreeGas(9)
	isolatedParent.ChksgnCounter = 12
	isolatedParent.SetChildRunner(func(child *State) (int64, error) {
		if child.ChksgnCounter != 0 {
			t.Fatalf("isolated child chksgn counter = %d, want 0", child.ChksgnCounter)
		}
		if child.Gas.FreeConsumed != 0 {
			t.Fatalf("isolated child free gas = %d, want 0", child.Gas.FreeConsumed)
		}
		return 0, nil
	})
	if err = isolatedParent.RunChildVM(ChildVMConfig{
		Code:       cell.BeginCell().EndCell().MustBeginParse(),
		Gas:        GasWithLimit(50),
		IsolateGas: true,
	}); err != nil {
		t.Fatalf("run isolated child vm: %v", err)
	}
	if isolatedParent.Gas.Used() != 9 {
		t.Fatalf("isolated parent gas used = %d, want 9", isolatedParent.Gas.Used())
	}
	if isolatedParent.ChksgnCounter != 0 {
		t.Fatalf("isolated parent chksgn counter = %d, want 0", isolatedParent.ChksgnCounter)
	}
	if got := mustPopInt64(t, isolatedParent.Stack); got != 0 {
		t.Fatalf("isolated child exit code = %d, want 0", got)
	}
}

func TestRunChildVMVersionedGasClamp(t *testing.T) {
	tests := []struct {
		name      string
		version   int
		wantLimit int64
		wantMax   int64
	}{
		{
			name:      "v9_keeps_child_gas",
			version:   9,
			wantLimit: 120,
			wantMax:   150,
		},
		{
			name:      "v10_clamps_to_parent_remaining",
			version:   10,
			wantLimit: 40,
			wantMax:   40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := runChildVMObservedGas(t, tt.version, GasWithLimit(40), GasWithLimit(120, 150))
			if observed.Limit != tt.wantLimit {
				t.Fatalf("child gas limit = %d, want %d", observed.Limit, tt.wantLimit)
			}
			if observed.Max != tt.wantMax {
				t.Fatalf("child gas max = %d, want %d", observed.Max, tt.wantMax)
			}
			if observed.Base != tt.wantLimit {
				t.Fatalf("child gas base = %d, want %d", observed.Base, tt.wantLimit)
			}
			if observed.Remaining != observed.Base {
				t.Fatalf("child gas remaining = %d, want base %d", observed.Remaining, observed.Base)
			}
		})
	}
}

func FuzzRunChildVMVersionedGasClamp(f *testing.F) {
	for version := uint8(0); version <= uint8(DefaultGlobalVersion); version++ {
		f.Add(version, uint16(40), uint16(120), uint16(150))
		f.Add(version, uint16(80), uint16(20), uint16(70))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawParentLimit, rawChildLimit, rawChildMax uint16) {
		version := int(rawVersion % uint8(DefaultGlobalVersion+1))
		parentLimit := int64(rawParentLimit%200) + 1
		childLimit := int64(rawChildLimit%300) + 1
		childMax := int64(rawChildMax%300) + 1
		if childMax < childLimit {
			childMax = childLimit
		}

		observed := runChildVMObservedGas(t, version, GasWithLimit(parentLimit), GasWithLimit(childLimit, childMax))

		wantLimit := childLimit
		wantMax := childMax
		if version >= 10 {
			if wantLimit > parentLimit {
				wantLimit = parentLimit
			}
			if wantMax > parentLimit {
				wantMax = parentLimit
			}
			if wantMax < wantLimit {
				wantMax = wantLimit
			}
		}

		if observed.Limit != wantLimit {
			t.Fatalf("v%d parent=%d child=%d/%d child gas limit = %d, want %d", version, parentLimit, childLimit, childMax, observed.Limit, wantLimit)
		}
		if observed.Max != wantMax {
			t.Fatalf("v%d parent=%d child=%d/%d child gas max = %d, want %d", version, parentLimit, childLimit, childMax, observed.Max, wantMax)
		}
		if observed.Base != wantLimit {
			t.Fatalf("v%d parent=%d child=%d/%d child gas base = %d, want %d", version, parentLimit, childLimit, childMax, observed.Base, wantLimit)
		}
		if observed.Remaining != observed.Base {
			t.Fatalf("v%d child gas remaining = %d, want base %d", version, observed.Remaining, observed.Base)
		}
	})
}

func FuzzChildResultRegisterValueVersionBoundary(f *testing.F) {
	for version := uint8(0); version <= uint8(DefaultGlobalVersion); version++ {
		f.Add(version, false, false, false)
		f.Add(version, false, true, false)
		f.Add(version, false, false, true)
		f.Add(version, true, false, false)
		f.Add(version, true, true, false)
		f.Add(version, true, false, true)
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, committed, hasCommittedValue, hasCurrentValue bool) {
		version := int(rawVersion % uint8(DefaultGlobalVersion+1))

		var committedValue *cell.Cell
		if hasCommittedValue {
			committedValue = cell.BeginCell().MustStoreUInt(0xC0, 8).EndCell()
		}
		var currentValue *cell.Cell
		if hasCurrentValue {
			currentValue = cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell()
		}

		child := NewExecutionStateWithGlobalVersion(version, NewGas(), nil, tuple.Tuple{}, NewStack())
		child.Committed.Committed = committed

		got := childResultRegisterValue(child, committedValue, currentValue)
		var want *cell.Cell
		if committed {
			want = committedValue
		} else if version < 11 {
			want = currentValue
		}

		if got != want {
			t.Fatalf("v%d committed=%v hasCommitted=%v hasCurrent=%v result = %p, want %p", version, committed, hasCommittedValue, hasCurrentValue, got, want)
		}
	})
}

func TestRunChildVMInheritsExplicitZeroGlobalVersion(t *testing.T) {
	parent := NewExecutionStateWithGlobalVersion(0, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	parent.SetChildRunner(func(child *State) (int64, error) {
		if child.GlobalVersion != 0 {
			t.Fatalf("child global version = %d, want explicit 0", child.GlobalVersion)
		}
		if !child.GlobalVersionConfigured {
			t.Fatal("child global version should stay explicitly configured")
		}
		return 0, nil
	})

	if err := parent.RunChildVM(ChildVMConfig{
		Code: cell.BeginCell().EndCell().MustBeginParse(),
		Gas:  GasWithLimit(1_000),
	}); err != nil {
		t.Fatalf("run child vm: %v", err)
	}
}

func runChildVMObservedGas(t *testing.T, version int, parentGas, childGas Gas) Gas {
	t.Helper()

	parent := NewExecutionStateWithGlobalVersion(version, parentGas, nil, tuple.Tuple{}, NewStack())
	var observed Gas
	parent.SetChildRunner(func(child *State) (int64, error) {
		observed = child.Gas
		return 0, nil
	})

	if err := parent.RunChildVM(ChildVMConfig{
		Code: cell.BeginCell().EndCell().MustBeginParse(),
		Gas:  childGas,
	}); err != nil {
		t.Fatalf("run child vm: %v", err)
	}
	return observed
}

func TestRunChildVMUnbindsReturnedCellTrace(t *testing.T) {
	parent := NewExecutionState(DefaultGlobalVersion, GasWithLimit(100_000), nil, tuple.Tuple{}, NewStack())
	parent.InitForExecution()
	parent.SetChildRunner(func(child *State) (int64, error) {
		childTrace := child.Cells.Trace()
		child.Committed = CommittedState{
			Data:      cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell().WithTrace(childTrace),
			Actions:   cell.BeginCell().MustStoreUInt(0xA0, 8).EndCell().WithTrace(childTrace),
			Committed: true,
		}
		return 0, nil
	})

	if err := parent.RunChildVM(ChildVMConfig{
		Code:          cell.BeginCell().EndCell().MustBeginParse(),
		Gas:           GasWithLimit(1_000),
		ReturnActions: true,
	}); err != nil {
		t.Fatalf("run child vm: %v", err)
	}

	actions, err := parent.Stack.PopCell()
	if err != nil {
		t.Fatalf("pop actions: %v", err)
	}
	if got := mustPopInt64(t, parent.Stack); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}

	before := parent.Gas.Used()
	if _, err = parent.Cells.BeginParse(actions); err != nil {
		t.Fatalf("parse returned actions: %v", err)
	}
	if got := parent.Gas.Used() - before; got != CellLoadGasPrice {
		t.Fatalf("returned action cell load gas = %d, want %d", got, CellLoadGasPrice)
	}
}
