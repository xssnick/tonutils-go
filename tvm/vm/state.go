package vm

import (
	"errors"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
)

const ControlDataAllArgs = -1
const CP = -1

type Register struct {
	C  [4]Continuation
	D  [2]*cell.Cell
	C7 tuple.Tuple
}

func (r *Register) AdjustWith(r2 *Register) {
	for i := 0; i < 4; i++ {
		if r2.C[i] == nil {
			continue
		}
		r.C[i] = r2.C[i]
	}
	for i := 0; i < 2; i++ {
		if r2.D[i] == nil {
			continue
		}
		r.D[i] = r2.D[i]
	}
	if !r2.C7.IsNull() {
		r.C7 = r2.C7
	}
}

func (r *Register) Get(i int) any {
	if i < 4 {
		return r.C[i]
	}
	if i < 6 {
		return r.D[i-4]
	}
	if i == 7 {
		return r.C7
	}
	return Null{}
}

func (r *Register) Copy() Register {
	rg := Register{}
	for i := 0; i < 4; i++ {
		if r.C[i] == nil {
			continue
		}
		rg.C[i] = r.C[i].Copy()
	}
	for i := 0; i < 2; i++ {
		rg.D[i] = r.D[i]
	}
	rg.C7 = r.C7.Copy()
	return rg
}

func (r *Register) Define(i int, val any) bool {
	if i < 0 {
		return false
	}

	if i < 4 {
		if r.C[i] != nil {
			return false
		}

		return r.Set(i, val)
	}

	if i < 6 {
		if r.D[i-4] != nil {
			return false
		}

		return r.Set(i, val)
	}

	if i == 7 {
		c, ok := val.(tuple.Tuple)
		if !ok || c.IsNull() {
			return false
		}

		if r.C7.IsNull() {
			r.C7 = c
		}
		return true
	}

	return false
}

func (r *Register) Set(i int, val any) bool {
	if i < 0 {
		return false
	}

	if i < 4 {
		c, ok := val.(Continuation)
		if !ok || c == nil {
			return false
		}

		r.C[i] = c
		return true
	}

	if i < 6 {
		c, ok := val.(*cell.Cell)
		if !ok || c == nil {
			return false
		}

		r.D[i-4] = c
		return true
	}

	if i == 7 {
		c, ok := val.(tuple.Tuple)
		if !ok || c.IsNull() {
			return false
		}

		r.C7 = c
		return true
	}

	return false
}

type ControlData struct {
	Save    Register
	Stack   *Stack
	NumArgs int
	CP      int
}

type CommittedState struct {
	Data      *cell.Cell
	Actions   *cell.Cell
	Committed bool
}

type State struct {
	GlobalVersion           int
	GlobalVersionConfigured bool
	CP                      int
	CurrentCode             *cell.Slice
	Reg                     Register
	Gas                     Gas
	Cells                   CellManager
	Libraries               []*cell.Cell
	libraryCache            map[cell.Hash]*cell.Cell
	Stack                   *Stack
	Steps                   uint32
	StopOnAccept            bool
	TraceHook               TraceHook
	ChksigAlwaysSucceed     bool
	ChksgnCounter           uint32
	GetExtraBalanceCounter  uint32
	Committed               CommittedState
	childRunner             ChildRunner
}

var ErrStopOnAccept = errors.New("stop on accept")

var sharedEmptyCell = cell.BeginCell().EndCell()

func emptyCell() *cell.Cell {
	return sharedEmptyCell
}

func IsSuccessExitCode(code int64) bool {
	return code == 0 || code == 1
}

func NewExecutionState(globalVersion int, gas Gas, data *cell.Cell, c7 tuple.Tuple, stack *Stack, libraries ...*cell.Cell) *State {
	return newExecutionState(globalVersion, globalVersion != 0, gas, data, c7, stack, libraries...)
}

func NewExecutionStateWithGlobalVersion(globalVersion int, gas Gas, data *cell.Cell, c7 tuple.Tuple, stack *Stack, libraries ...*cell.Cell) *State {
	return newExecutionState(globalVersion, true, gas, data, c7, stack, libraries...)
}

func newExecutionState(globalVersion int, globalVersionConfigured bool, gas Gas, data *cell.Cell, c7 tuple.Tuple, stack *Stack, libraries ...*cell.Cell) *State {
	if data == nil {
		data = emptyCell()
	}
	if c7.IsNull() {
		c7 = tuple.NewTupleValue()
	}

	return &State{
		GlobalVersion:           globalVersion,
		GlobalVersionConfigured: globalVersionConfigured,
		Gas:                     gas,
		Reg: Register{
			C: [4]Continuation{
				quitCont0,
				quitCont1,
				&ExcQuitContinuation{},
				quitContUnknown,
			},
			D: [2]*cell.Cell{
				data,
				emptyCell(),
			},
			C7: c7,
		},
		Stack:     stack,
		Libraries: append([]*cell.Cell{}, libraries...),
	}
}

type OPGetter func() OP
type GasPricedOp interface {
	InstructionBits() int64
}
type VersionedOp interface {
	MinGlobalVersion() int
}
type OP interface {
	GetPrefixes() []*cell.Slice
	Deserialize(code *cell.Slice) error
	Serialize() *cell.Builder
	SerializeText() string
	Interpret(state *State) error
}

var List []OPGetter

var ErrCorruptedOpcode = errors.New("corrupted opcode")

const MaxDataDepth = 512

func (s *State) InitForExecution() {
	if !s.GlobalVersionConfigured {
		s.GlobalVersion = DefaultGlobalVersion
		s.GlobalVersionConfigured = true
	}
	s.Cells.Init(s)
	if s.Stack != nil {
		s.Stack.SetTrace(s.Cells.Trace())
	}
	s.Reg.C7 = bindTupleTrace(s.Reg.C7, s.Cells.Trace())
}

func (s *State) effectiveGlobalVersion() int {
	if !s.GlobalVersionConfigured {
		return DefaultGlobalVersion
	}
	return s.GlobalVersion
}

func (s *State) PrepareExecution(code *cell.Slice) {
	s.InitForExecution()
	if code != nil {
		s.CurrentCode = code.SetTrace(cell.CombineTraces(code.Trace(), s.Cells.Trace()))
	}
}

type ChildRunner func(*State) (int64, error)

func (s *State) SetChildRunner(fn ChildRunner) {
	s.childRunner = fn
}

func (s *State) prepareChildForRun(child *State) error {
	if child == nil {
		return vmerr.Error(vmerr.CodeFatal, "child state is nil")
	}
	if s.childRunner == nil {
		return vmerr.Error(vmerr.CodeFatal, "child runner is not configured")
	}
	if !child.GlobalVersionConfigured {
		child.GlobalVersion = s.effectiveGlobalVersion()
		child.GlobalVersionConfigured = true
	}
	child.GetExtraBalanceCounter = s.GetExtraBalanceCounter
	child.TraceHook = s.TraceHook
	child.ChksigAlwaysSucceed = s.ChksigAlwaysSucceed
	if len(child.Libraries) == 0 && len(s.Libraries) > 0 {
		child.Libraries = append([]*cell.Cell{}, s.Libraries...)
	}
	child.childRunner = s.childRunner
	child.PrepareExecution(child.CurrentCode)
	return nil
}

func (s *State) runChildRaw(child *State) (int64, error) {
	if err := s.prepareChildForRun(child); err != nil {
		return 0, err
	}
	exitCode, err := s.childRunner(child)
	s.GetExtraBalanceCounter = child.GetExtraBalanceCounter
	return exitCode, err
}

func (s *State) RunChild(child *State) (int64, error) {
	if err := s.prepareChildForRun(child); err != nil {
		return 0, err
	}
	exitCode, err := s.childRunner(child)
	s.GetExtraBalanceCounter = child.GetExtraBalanceCounter
	if err != nil {
		if _, ok := vmerr.ErrorCode(err); ok {
			return exitCode, nil
		}
		return 0, err
	}
	return exitCode, nil
}

func (s *State) ConsumeGas(amount int64) error {
	if !s.checkGasOnConsume() {
		s.Gas.Remaining -= amount
		return nil
	}
	return s.Gas.Consume(amount)
}

func (s *State) consumeGasChecked(amount int64) error {
	return s.Gas.Consume(amount)
}

func (s *State) checkGasOnConsume() bool {
	if !s.GlobalVersionConfigured && s.GlobalVersion == 0 {
		return true
	}
	return s.GlobalVersion >= 4
}

func (s *State) CheckGas() error {
	if err := s.Cells.PendingError(); err != nil {
		return err
	}
	return s.Gas.Check()
}

func (s *State) ConsumeFreeGas(amount int64) {
	s.Gas.ConsumeFree(amount)
}

func (s *State) FlushFreeGas() error {
	return s.Gas.FlushFree()
}

func (s *State) RegisterChksgnCall() error {
	if !s.checkGasOnConsume() {
		return nil
	}

	s.ChksgnCounter++
	if s.ChksgnCounter > ChksgnFreeCount {
		return s.ConsumeGas(ChksgnGasPrice)
	}
	s.ConsumeFreeGas(ChksgnGasPrice)
	return nil
}

func (s *State) RegisterGetExtraBalanceCall() bool {
	s.GetExtraBalanceCounter++
	return s.GetExtraBalanceCounter <= GetExtraBalanceCheapCount
}

func (s *State) RegisterCellLoadFreeKey(key cell.Hash) bool {
	s.Cells.Init(s)
	if s.Cells.loaded == nil {
		s.Cells.loaded = map[cell.Hash]struct{}{}
	}
	_, ok := s.Cells.loaded[key]
	if !ok {
		s.Cells.loaded[key] = struct{}{}
		return true
	}
	return false
}

func (s *State) ConsumeStackGasLen(depth int) error {
	amt := int64(depth)
	if amt < FreeStackDepth {
		amt = FreeStackDepth
	}
	return s.ConsumeGas((amt - FreeStackDepth) * StackEntryGasPrice)
}

func (s *State) ConsumeStackGas(stk *Stack) error {
	if stk == nil {
		return nil
	}
	return s.ConsumeStackGasLen(stk.Len())
}

func (s *State) ConsumeTupleGasLen(length int) error {
	return s.ConsumeGas(int64(length) * TupleEntryGasPrice)
}

func (s *State) PushTupleCharged(t tuple.Tuple) error {
	if err := s.ConsumeTupleGasLen(t.Len()); err != nil {
		return err
	}
	return s.Stack.PushTuple(t)
}

func (s *State) SetC7(t tuple.Tuple) error {
	s.Cells.Init(s)
	s.Reg.C7 = bindTupleTrace(t, s.Cells.Trace())
	return nil
}

func (s *State) UpdateC7(fn func(tuple.Tuple) (tuple.Tuple, error)) error {
	next, err := fn(s.Reg.C7.Copy())
	if err != nil {
		return err
	}
	return s.SetC7(next)
}

func (s *State) GetParam(idx int) (any, error) {
	params, err := s.Reg.C7.Index(0)
	if err != nil {
		return nil, err
	}

	p, ok := params.(tuple.Tuple)
	if !ok || p.Len() > 255 {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}

	v, err := p.Index(idx)
	if err != nil {
		return nil, err
	}

	return v, nil
}

func (s *State) GetUnpackedConfigTuple() (tuple.Tuple, error) {
	v, err := s.GetParam(14)
	if err != nil {
		return tuple.Tuple{}, err
	}
	t, ok := v.(tuple.Tuple)
	if !ok || t.Len() > 255 {
		return tuple.Tuple{}, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return t, nil
}

func (s *State) GetGlobal(idx int) (any, error) {
	if idx < 0 || idx >= 255 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}
	if idx >= s.Reg.C7.Len() {
		return nil, nil
	}
	return s.Reg.C7.Index(idx)
}

func (s *State) SetGlobal(idx int, val any) error {
	if idx < 0 || idx >= 255 {
		return vmerr.Error(vmerr.CodeRangeCheck, "tuple index out of range")
	}

	tup := s.Reg.C7.Copy()
	if idx >= tup.Len() {
		if val == nil {
			return nil
		}
		tup.Resize(idx + 1)
	}

	if err := s.ConsumeTupleGasLen(tup.Len()); err != nil {
		return err
	}
	if err := tup.Set(idx, val); err != nil {
		return err
	}
	return s.SetC7(tup)
}

func (s *State) SetGasLimit(limit int64) error {
	if limit < s.Gas.Used() {
		return vmerr.Error(vmerr.CodeOutOfGas)
	}
	accepting := s.StopOnAccept && s.Gas.Credit > 0
	s.Gas.ChangeLimit(limit)
	if accepting && s.Gas.Credit == 0 {
		return ErrStopOnAccept
	}
	return nil
}

func (s *State) HandleOutOfGas() error {
	s.Stack.Clear()
	return s.Stack.PushSmallInt(s.Gas.Used())
}

func (s *State) TryCommitCurrent() bool {
	if s.Reg.D[0] == nil || s.Reg.D[1] == nil {
		return false
	}
	if s.Reg.D[0].Depth() > MaxDataDepth || s.Reg.D[1].Depth() > MaxDataDepth {
		return false
	}
	if s.Reg.D[0].Level() != 0 || s.Reg.D[1].Level() != 0 {
		return false
	}
	s.Committed = CommittedState{
		Data:      s.Reg.D[0],
		Actions:   s.Reg.D[1],
		Committed: true,
	}
	return true
}

func (s *State) ForceCommitCurrent() error {
	if !s.TryCommitCurrent() {
		return vmerr.Error(vmerr.CodeCellOverflow, "cannot commit too deep cells as new data/actions")
	}
	return nil
}

func (s *State) ThrowException(code *big.Int, arg ...any) error {
	s.Stack.Clear()
	if len(arg) == 1 {
		if err := s.Stack.PushAny(arg[0]); err != nil {
			return err
		}
	} else if len(arg) == 0 {
		if err := s.Stack.PushSmallInt(0); err != nil {
			return err
		}
	} else {
		return errors.New("too many arguments")
	}

	if err := s.Stack.PushInt(code); err != nil {
		return err
	}

	s.CurrentCode = cell.BeginCell().ToSlice()
	if err := s.consumeGasChecked(ExceptionGasPrice); err != nil {
		return err
	}

	return s.Jump(s.Reg.C[2])
}

func (c ControlData) Copy() ControlData {
	var stk *Stack
	if c.Stack != nil {
		stk = c.Stack.Copy()
	}

	return ControlData{
		Save:    c.Save.Copy(),
		Stack:   stk,
		NumArgs: c.NumArgs,
		CP:      c.CP,
	}
}

func ForceControlData(cont Continuation) Continuation {
	if cont.GetControlData() == nil {
		cont = &ArgExtContinuation{
			Data: ControlData{
				NumArgs: ControlDataAllArgs,
				CP:      CP,
			},
			Ext: cont,
		}
	}
	return cont
}
