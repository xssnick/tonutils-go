package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type Continuation interface {
	GetControlData() *ControlData
	Jump(state *State) (Continuation, error)
}

type QuitContinuation struct {
	ExitCode int64
}

func (c *QuitContinuation) GetControlData() *ControlData {
	return nil
}

func (c *QuitContinuation) Jump(state *State) (Continuation, error) {
	return nil, vmerr.VMError{
		Code: c.ExitCode,
	}
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

	return nil, vmerr.VMError{
		Code: ^v.Int64(),
	}
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
