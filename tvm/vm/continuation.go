package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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
