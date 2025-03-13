package vm

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func (s *State) Return() error {
	if s.Reg.C[0] == nil {
		return fmt.Errorf("something wrong, c0 is nil")
	}
	return s.Jump(s.Reg.C[0])
}

func (s *State) ReturnAlt() error {
	if s.Reg.C[1] == nil {
		return fmt.Errorf("something wrong, c1 is nil")
	}
	return s.Jump(s.Reg.C[1])
}

func (s *State) Call(c Continuation) error {
	data := c.GetControlData()
	if data != nil {
		if data.Save.C[0] != nil {
			return s.Jump(c)
		}
		if data.Stack != nil || data.NumArgs >= 0 {
			// if cont has non-empty stack or expects fixed number of arguments, call is not simple
			return s.CallArgs(c, -1, -1)
		}
	}

	ret := &OrdinaryContinuation{
		Data: ControlData{
			NumArgs: ControlDataAllArgs,
			CP:      s.CP,
		},
		Code: s.CurrentCode,
	}
	ret.Data.Save.C[0] = s.Reg.C[0]
	s.Reg.C[0] = ret

	return s.JumpTo(c)
}

func (s *State) CallArgs(c Continuation, passArgs, retArgs int) error {
	var newStack *Stack

	data := c.GetControlData()
	if data != nil {
		if data.Save.C[0] != nil {
			// call reduces to a jump
			return s.JumpArgs(c, passArgs)
		}

		depth := s.Stack.Len()
		if passArgs > depth || data.NumArgs > depth {
			return vmerr.VMError{
				Code: vmerr.ErrStackUnderflow.Code,
				Msg:  "stack underflow while calling a continuation: not enough arguments on stack",
			}
		}
		if data.NumArgs > passArgs && passArgs >= 0 {
			return vmerr.VMError{
				Code: vmerr.ErrStackUnderflow.Code,
				Msg:  "stack underflow while calling a closure continuation: not enough arguments passed",
			}
		}

		cp := data.NumArgs
		skip := 0
		if passArgs >= 0 {
			if cp >= 0 {
				skip = passArgs - cp
			} else {
				cp = passArgs
			}
		}

		// copy=-1 : pass whole stack, else pass top `cp` elements, drop next `skip` elements.
		if data.Stack != nil && data.Stack.Len() > 0 {
			if cp < 0 {
				cp = s.Stack.Len()
			}

			newStack = data.Stack.Copy()
			if skip > 0 {
				if err := s.Stack.Drop(skip); err != nil {
					return err
				}
			}

			if err := s.Gas.ConsumeStackGas(newStack); err != nil {
				return err
			}
		} else if cp >= 0 {
			ns, err := s.Stack.SplitTop(cp, skip)
			if err != nil {
				return err
			}
			newStack = ns

			if err = s.Gas.ConsumeStackGas(newStack); err != nil {
				return err
			}
		} else {
			s.Stack.Clear()
			newStack = s.Stack
		}
	} else {
		depth := s.Stack.Len()
		if passArgs > depth {
			return vmerr.VMError{
				Code: vmerr.ErrStackUnderflow.Code,
				Msg:  "stack underflow while calling a continuation: not enough arguments on stack",
			}
		}

		newStack = s.Stack
		if passArgs >= 0 {
			ns, err := s.Stack.SplitTop(passArgs, 0)
			if err != nil {
				return err
			}
			newStack = ns

			if err = s.Gas.ConsumeStackGas(newStack); err != nil {
				return err
			}
		}
	}

	ret := &OrdinaryContinuation{
		Data: ControlData{
			Stack:   s.Stack,
			NumArgs: retArgs,
			CP:      s.CP,
		},
		Code: s.CurrentCode,
	}
	ret.Data.Save.C[0] = s.Reg.C[0]

	s.Stack = newStack
	s.Reg.C[0] = ret
	return s.JumpTo(c)
}

func (s *State) Jump(c Continuation) error {
	data := c.GetControlData()
	if data != nil && data.Stack != nil && data.NumArgs > 0 {
		return s.JumpArgs(c, -1)
	}
	return s.JumpTo(c)
}

func (s *State) JumpArgs(c Continuation, passArgs int) error {
	c, err := s.adjustJumpCont(c, passArgs)
	if err != nil {
		return err
	}
	return s.JumpTo(c)
}

func (s *State) JumpTo(c Continuation) (err error) {
	const freeIterations = 8

	var iter int
	for c != nil {
		c, err = c.Jump(s)
		if err != nil {
			return err
		}
		iter++

		if iter > freeIterations {
			if err = s.Gas.Consume(1); err != nil {
				return err
			}
		}

		if c != nil {
			if data := c.GetControlData(); data != nil && (data.Stack != nil || data.NumArgs >= 0) {
				// if cont has non-empty stack or expects fixed number of arguments, jump is not simple
				c, err = s.adjustJumpCont(c, -1)
				if err != nil {
					return err
				}
			}
		}
	}

	return
}

func (s *State) adjustJumpCont(c Continuation, passArgs int) (Continuation, error) {
	data := c.GetControlData()
	if data != nil {
		depth := s.Stack.Len()
		if passArgs > depth || data.NumArgs > depth {
			return nil, vmerr.VMError{
				Code: vmerr.ErrStackUnderflow.Code,
				Msg:  "stack underflow while jumping to a continuation: not enough arguments on stack",
			}
		}
		if data.NumArgs > passArgs && passArgs >= 0 {
			return nil, vmerr.VMError{
				Code: vmerr.ErrStackUnderflow.Code,
				Msg:  "stack underflow while jumping to closure continuation: not enough arguments passed",
			}
		}

		cp := data.NumArgs
		if passArgs >= 0 && cp < 0 {
			cp = passArgs
		}

		if data.Stack != nil && data.Stack.Len() > 0 {
			if cp < 0 {
				cp = data.Stack.Len()
			}
			newStack := data.Stack.Copy()
			if err := newStack.MoveFrom(s.Stack, cp); err != nil {
				return nil, err
			}
			if err := s.Gas.ConsumeStackGas(newStack); err != nil {
				return nil, err
			}

			s.Stack = newStack
		} else {
			if cp >= 0 && cp < s.Stack.Len() {
				if err := s.Stack.DropAfter(cp); err != nil {
					return nil, err
				}
				if err := s.Gas.ConsumeStackGas(s.Stack); err != nil {
					return nil, err
				}
			}
		}

		return c, nil
	}

	if passArgs >= 0 {
		depth := s.Stack.Len()
		if passArgs > depth {
			return nil, vmerr.VMError{
				Code: vmerr.ErrStackUnderflow.Code,
				Msg:  "stack underflow while jumping to a continuation: not enough arguments on stack",
			}
		} else if passArgs < depth {
			if err := s.Stack.DropAfter(passArgs); err != nil {
				return nil, err
			}
			if err := s.Gas.ConsumeStackGas(s.Stack); err != nil {
				return nil, err
			}
		}
	}

	return c, nil
}
