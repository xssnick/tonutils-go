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
	if r2.C7.Len() > 0 {
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
		if !ok {
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
	GlobalVersion int
	CP            int
	CurrentCode   *cell.Slice
	Reg           Register
	Gas           Gas
	Cells         CellManager
	Libraries     []*cell.Cell
	Stack         *Stack
	Steps         uint32
	ChksgnCounter uint32
	Committed     CommittedState
	childRunner   ChildRunner
}

func emptyCell() *cell.Cell {
	return cell.BeginCell().EndCell()
}

func IsSuccessExitCode(code int64) bool {
	return code == 0 || code == 1
}

func NewExecutionState(globalVersion int, gas Gas, data *cell.Cell, c7 tuple.Tuple, stack *Stack, libraries ...*cell.Cell) *State {
	if data == nil {
		data = emptyCell()
	}

	return &State{
		GlobalVersion: globalVersion,
		Gas:           gas,
		Reg: Register{
			C: [4]Continuation{
				&QuitContinuation{ExitCode: 0},
				&QuitContinuation{ExitCode: 1},
				&ExcQuitContinuation{},
				&QuitContinuation{ExitCode: vmerr.CodeUnknown},
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
	if s.GlobalVersion == 0 {
		s.GlobalVersion = DefaultGlobalVersion
	}
	s.Cells.Init(s)
	if s.Stack != nil {
		s.Stack.SetObserver(&s.Cells)
	}
	s.Reg.C7 = bindTupleObserver(s.Reg.C7, &s.Cells)
}

func (s *State) PrepareExecution(code *cell.Slice) {
	s.InitForExecution()
	if code != nil {
		s.CurrentCode = code.SetObserver(&s.Cells)
	}
}

type ChildRunner func(*State) (int64, error)

func (s *State) SetChildRunner(fn ChildRunner) {
	s.childRunner = fn
}

func (s *State) RunChild(child *State) (int64, error) {
	if child == nil {
		return 0, vmerr.Error(vmerr.CodeFatal, "child state is nil")
	}
	if s.childRunner == nil {
		return 0, vmerr.Error(vmerr.CodeFatal, "child runner is not configured")
	}
	if child.GlobalVersion == 0 {
		child.GlobalVersion = s.GlobalVersion
	}
	if len(child.Libraries) == 0 && len(s.Libraries) > 0 {
		child.Libraries = append([]*cell.Cell{}, s.Libraries...)
	}
	child.childRunner = s.childRunner
	child.PrepareExecution(child.CurrentCode)
	return s.childRunner(child)
}

func (s *State) ConsumeGas(amount int64) error {
	return s.Gas.Consume(amount)
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
	s.ChksgnCounter++
	if s.ChksgnCounter > ChksgnFreeCount {
		return s.ConsumeGas(ChksgnGasPrice)
	}
	s.ConsumeFreeGas(ChksgnGasPrice)
	return nil
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
	s.Reg.C7 = bindTupleObserver(t, &s.Cells)
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
	if !ok {
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
	if !ok {
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
	s.Gas.ChangeLimit(limit)
	return nil
}

func (s *State) HandleOutOfGas() error {
	s.Stack.Clear()
	return s.Stack.PushInt(big.NewInt(s.Gas.Used()))
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
		if err := s.Stack.PushAny(big.NewInt(0)); err != nil {
			return err
		}
	} else {
		return errors.New("too many arguments")
	}

	if err := s.Stack.PushInt(code); err != nil {
		return err
	}

	s.CurrentCode = cell.BeginCell().ToSlice()
	if err := s.ConsumeGas(ExceptionGasPrice); err != nil {
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
