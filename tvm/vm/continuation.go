package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
)

type Continuation interface {
	GetControlData() *ControlData
	Jump(state *State) (Continuation, error)
	Copy() Continuation
}

type QuitContinuation struct {
	ExitCode int64
}

func (c *QuitContinuation) GetControlData() *ControlData {
	return nil
}

func (c *QuitContinuation) Jump(state *State) (Continuation, error) {
	return nil, vmerr.Error(c.ExitCode)
}

func (c *QuitContinuation) Copy() Continuation {
	cont := *c
	return &cont
}

type ExcQuitContinuation struct{}

func (c *ExcQuitContinuation) GetControlData() *ControlData {
	return nil
}

func (c *ExcQuitContinuation) Jump(state *State) (Continuation, error) {
	v, err := state.Stack.PopIntRange(0, 0xffff)
	if err != nil {
		return nil, err
	}

	return nil, vmerr.Error(^v.Int64())
}

func (c *ExcQuitContinuation) Copy() Continuation {
	cont := *c
	return &cont
}

type OrdinaryContinuation struct {
	Data ControlData
	Code *cell.Slice
}

func (c *OrdinaryContinuation) GetControlData() *ControlData {
	return &c.Data
}

func (c *OrdinaryContinuation) Jump(state *State) (Continuation, error) {
	state.CurrentCode = c.Code.Copy()
	state.Reg.AdjustWith(&c.Data.Save)
	return nil, nil
}

func (c *OrdinaryContinuation) Copy() Continuation {
	cont := &OrdinaryContinuation{
		Data: c.Data.Copy(),
		Code: c.Code.Copy(),
	}
	return cont
}

type ArgExtContinuation struct {
	Data ControlData
	Ext  Continuation
}

func (c *ArgExtContinuation) GetControlData() *ControlData {
	return &c.Data
}

func (c *ArgExtContinuation) Jump(state *State) (Continuation, error) {
	state.Reg.AdjustWith(&c.Data.Save)
	if c.Data.CP != -1 {
		// TODO: custom cp support
		return nil, vmerr.Error(vmerr.CodeInvalidOpcode, "unsupported codepage")
	}
	return c.Ext, nil
}

func (c *ArgExtContinuation) Copy() Continuation {
	cont := &ArgExtContinuation{
		Data: c.Data.Copy(),
		Ext:  c.Ext.Copy(),
	}
	return cont
}

type PushIntContinuation struct {
	Int  int64
	Next Continuation
}

func (c *PushIntContinuation) GetControlData() *ControlData {
	return nil
}

func (c *PushIntContinuation) Jump(state *State) (Continuation, error) {
	if err := state.Stack.PushInt(big.NewInt(c.Int)); err != nil {
		return nil, err
	}
	return c.Next, nil
}

func (c *PushIntContinuation) Copy() Continuation {
	return &PushIntContinuation{
		Int:  c.Int,
		Next: c.Next.Copy(),
	}
}

type RepeatContinuation struct {
	Count int64
	Body  Continuation
	After Continuation
}

func (c *RepeatContinuation) GetControlData() *ControlData {
	return nil
}

func (c *RepeatContinuation) Jump(state *State) (Continuation, error) {
	println("REPEATX", c.Count)

	if c.Count <= 0 {
		println("REPEAT END!")

		return c.After, nil
	}

	if cd := c.Body.GetControlData(); cd != nil && cd.Save.C[0] != nil {
		println("REPEAT HAS C0!")
		return c.Body, nil
	}

	state.Reg.C[0] = &RepeatContinuation{
		Count: c.Count - 1,
		Body:  c.Body.Copy(),
		After: c.After.Copy(),
	}

	println("REPEAT JUMP BODY!")
	return c.Body, nil
}

func (c *RepeatContinuation) Copy() Continuation {
	return &RepeatContinuation{
		Count: c.Count,
		Body:  c.Body.Copy(),
		After: c.After.Copy(),
	}
}

type WhileContinuation struct {
	CheckCond bool
	Body      Continuation
	Cond      Continuation
	After     Continuation
}

func (c *WhileContinuation) GetControlData() *ControlData {
	return nil
}

func (c *WhileContinuation) Jump(state *State) (Continuation, error) {
	if c.CheckCond {
		more, err := state.Stack.PopBool()
		if err != nil {
			return nil, err
		}

		if !more {
			println("WHILE END!")

			return c.After, nil
		}

		if cd := c.Body.GetControlData(); cd == nil || cd.Save.C[0] == nil {
			state.Reg.C[0] = &WhileContinuation{
				CheckCond: false,
				Body:      c.Body.Copy(),
				Cond:      c.Cond.Copy(),
				After:     c.After.Copy(),
			}
		}

		return c.Body, nil
	}

	if cd := c.Cond.GetControlData(); cd == nil || cd.Save.C[0] == nil {
		state.Reg.C[0] = &WhileContinuation{
			CheckCond: true,
			Body:      c.Body.Copy(),
			Cond:      c.Cond.Copy(),
			After:     c.After.Copy(),
		}
	}

	return c.Cond, nil
}

func (c *WhileContinuation) Copy() Continuation {
	return &WhileContinuation{
		CheckCond: c.CheckCond,
		Body:      c.Body.Copy(),
		Cond:      c.Cond.Copy(),
		After:     c.After.Copy(),
	}
}

type UntilContinuation struct {
	Body  Continuation
	After Continuation
}

func (c *UntilContinuation) GetControlData() *ControlData {
	return nil
}

func (c *UntilContinuation) Jump(state *State) (Continuation, error) {
	end, err := state.Stack.PopBool()
	if err != nil {
		return nil, err
	}

	if end {
		println("UNTIL END!")

		return c.After, nil
	}

	if cd := c.Body.GetControlData(); cd == nil || cd.Save.C[0] == nil {
		state.Reg.C[0] = c
	}

	return c.Body, nil
}

func (c *UntilContinuation) Copy() Continuation {
	return &UntilContinuation{
		Body:  c.Body.Copy(),
		After: c.After.Copy(),
	}
}
