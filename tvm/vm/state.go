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

type State struct {
	GlobalVersion int
	CP            int
	CurrentCode   *cell.Slice
	Reg           Register
	Gas           Gas
	Cells         CellManager
	Stack         *Stack
	Steps         uint32
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

func (s *State) ConsumeTupleGas(t tuple.Tuple) error {
	return s.ConsumeTupleGasLen(t.Len())
}

func (s *State) PushTupleCharged(t tuple.Tuple) error {
	if err := s.ConsumeTupleGas(t); err != nil {
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
