package vm

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"reflect"
)

func (s *State) Return(args ...int) error {
	cont := Continuation(&QuitContinuation{ExitCode: 0})
	s.Reg.C[0], cont = cont, s.Reg.C[0]
	if len(args) == 1 {
		return s.JumpArgs(cont, args[0])
	} else if len(args) == 0 {
		println("RETURN", reflect.TypeOf(cont).String())
		return s.Jump(cont)
	}
	return fmt.Errorf("only one arg supported")
}

func (s *State) ReturnAlt(args ...int) error {
	cont := Continuation(&QuitContinuation{ExitCode: 1})
	s.Reg.C[1], cont = cont, s.Reg.C[1]
	if len(args) == 1 {
		return s.JumpArgs(cont, args[0])
	} else if len(args) == 0 {
		return s.Jump(cont)
	}
	return fmt.Errorf("only one arg supported")
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
			return vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while calling a continuation: not enough arguments on stack")
		}
		if data.NumArgs > passArgs && passArgs >= 0 {
			return vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while calling a closure continuation: not enough arguments passed")
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
			newStack = s.Stack
		}
	} else {
		depth := s.Stack.Len()
		if passArgs > depth {
			return vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while calling a continuation: not enough arguments on stack")
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
	println(s.Stack.String())
	println("[JUMP]")

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
	println("ADJUST JUMP CONT:", passArgs)
	// println(s.Stack.String())

	data := c.GetControlData()
	if data != nil {
		depth := s.Stack.Len()
		if passArgs > depth || data.NumArgs > depth {
			return nil, vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while jumping to a continuation: not enough arguments on stack")
		}
		if data.NumArgs > passArgs && passArgs >= 0 {
			return nil, vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while jumping to closure continuation: not enough arguments passed")
		}

		cp := data.NumArgs
		if passArgs >= 0 && cp < 0 {
			cp = passArgs
		}

		if data.Stack != nil && data.Stack.Len() > 0 {
			var newStack *Stack
			if cp < 0 || cp == data.Stack.Len() {
				cp = data.Stack.Len()
				newStack = data.Stack.Copy()
			} else {
				newStack = NewStack()
				if err := newStack.MoveFrom(s.Stack, cp); err != nil {
					return nil, err
				}
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
			return nil, vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while jumping to a continuation: not enough arguments on stack")
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

func (s *State) ExtractCurrentContinuation(saveCR, stackCopy, ccArgs int) (*OrdinaryContinuation, error) {
	var newStack *Stack
	if stackCopy < 0 || stackCopy == s.Stack.Len() {
		newStack = s.Stack
		s.Stack = nil
	} else if stackCopy > 0 {
		ns, err := s.Stack.SplitTop(stackCopy, 0)
		if err != nil {
			return nil, err
		}
		newStack = ns

		if err = s.Gas.ConsumeStackGas(newStack); err != nil {
			return nil, err
		}
	} else {
		newStack = NewStack()
	}

	cc := &OrdinaryContinuation{
		Data: ControlData{
			NumArgs: ccArgs,
			CP:      s.CP,
			Stack:   s.Stack,
		},
		Code: s.CurrentCode,
	}
	s.Stack = newStack

	if saveCR&7 != 0 {
		cData := cc.GetControlData()
		if saveCR&1 != 0 {
			cData.Save.C[0] = s.Reg.C[0]
			s.Reg.C[0] = &QuitContinuation{ExitCode: 0}
		}
		if saveCR&2 != 0 {
			cData.Save.C[1] = s.Reg.C[1]
			s.Reg.C[1] = &QuitContinuation{ExitCode: 1}
		}
		if saveCR&4 != 0 {
			cData.Save.C[2] = s.Reg.C[2]
		}
	}
	return cc, nil
}
