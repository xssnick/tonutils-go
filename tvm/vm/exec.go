package vm

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const (
	continuationActionCall = "calling"
	continuationActionJump = "jumping"
)

type continuationStackPlan struct {
	data     *ControlData
	passArgs int
	cp       int
	skip     int
}

func (s *State) Return(args ...int) error {
	return s.returnTo(0, 0, args...)
}

func (s *State) ReturnAlt(args ...int) error {
	return s.returnTo(1, 1, args...)
}

func (s *State) returnTo(regIdx int, exitCode int64, args ...int) error {
	cont := Continuation(&QuitContinuation{ExitCode: exitCode})
	s.Reg.C[regIdx], cont = cont, s.Reg.C[regIdx]
	if len(args) == 1 {
		return s.JumpArgs(cont, args[0])
	} else if len(args) == 0 {
		return s.Jump(cont)
	}
	return fmt.Errorf("only one arg supported")
}

func planContinuationStack(current *Stack, data *ControlData, passArgs int, action string) (continuationStackPlan, error) {
	plan := continuationStackPlan{
		data:     data,
		passArgs: passArgs,
		cp:       -1,
	}

	depth := current.Len()
	if data != nil {
		if passArgs > depth || data.NumArgs > depth {
			return plan, vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while "+action+" a continuation: not enough arguments on stack")
		}
		if data.NumArgs > passArgs && passArgs >= 0 {
			return plan, vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while "+action+" a closure continuation: not enough arguments passed")
		}

		plan.cp = data.NumArgs
		if passArgs >= 0 {
			if plan.cp >= 0 {
				plan.skip = passArgs - plan.cp
			} else {
				plan.cp = passArgs
			}
		}
		return plan, nil
	}

	if passArgs > depth {
		return plan, vmerr.Error(vmerr.CodeStackUnderflow, "stack underflow while "+action+" a continuation: not enough arguments on stack")
	}
	if passArgs >= 0 {
		plan.cp = passArgs
	}
	return plan, nil
}

func (p continuationStackPlan) hasCapturedStack() bool {
	return p.data != nil && p.data.Stack != nil && p.data.Stack.Len() > 0
}

func (p continuationStackPlan) consumeAdjustedStack(s *State, stk *Stack) error {
	return s.ConsumeStackGas(stk)
}

func (p continuationStackPlan) buildCallStack(s *State) (*Stack, error) {
	if p.data != nil {
		// copy=-1 : pass whole stack, else pass top `cp` elements, drop next `skip` elements.
		if p.hasCapturedStack() {
			newStack := p.data.Stack.Copy()
			if p.skip > 0 {
				if err := s.Stack.Drop(p.skip); err != nil {
					return nil, err
				}
			}
			if err := p.consumeAdjustedStack(s, newStack); err != nil {
				return nil, err
			}
			return newStack, nil
		}

		if p.cp >= 0 {
			newStack, err := s.Stack.SplitTop(p.cp, p.skip)
			if err != nil {
				return nil, err
			}
			if err = p.consumeAdjustedStack(s, newStack); err != nil {
				return nil, err
			}
			return newStack, nil
		}
		return s.Stack, nil
	}

	if p.cp >= 0 {
		newStack, err := s.Stack.SplitTop(p.cp, 0)
		if err != nil {
			return nil, err
		}
		if err = p.consumeAdjustedStack(s, newStack); err != nil {
			return nil, err
		}
		return newStack, nil
	}
	return s.Stack, nil
}

func (p continuationStackPlan) applyJumpStack(s *State) error {
	if p.data != nil {
		if p.hasCapturedStack() {
			cp := p.cp
			var newStack *Stack
			if cp < 0 || cp == p.data.Stack.Len() {
				newStack = p.data.Stack.Copy()
			} else {
				newStack = NewStack()
				if err := newStack.MoveFrom(s.Stack, cp); err != nil {
					return err
				}
			}

			if err := p.consumeAdjustedStack(s, newStack); err != nil {
				return err
			}
			s.Stack = newStack
			return nil
		}

		if p.cp >= 0 && p.cp < s.Stack.Len() {
			if err := s.Stack.DropAfter(p.cp); err != nil {
				return err
			}
			if err := p.consumeAdjustedStack(s, s.Stack); err != nil {
				return err
			}
		}
		return nil
	}

	if p.passArgs >= 0 && p.passArgs < s.Stack.Len() {
		if err := s.Stack.DropAfter(p.passArgs); err != nil {
			return err
		}
		if err := p.consumeAdjustedStack(s, s.Stack); err != nil {
			return err
		}
	}
	return nil
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
	data := c.GetControlData()
	if data != nil {
		if data.Save.C[0] != nil {
			// call reduces to a jump
			return s.JumpArgs(c, passArgs)
		}
	}

	plan, err := planContinuationStack(s.Stack, data, passArgs, continuationActionCall)
	if err != nil {
		return err
	}

	newStack, err := plan.buildCallStack(s)
	if err != nil {
		return err
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
	traceStack("[JUMP]", s.Stack)

	var iter int
	for c != nil {
		c, err = c.Jump(s)
		if err != nil {
			return err
		}
		iter++

		if iter > FreeNestedContJump {
			if err = s.ConsumeGas(1); err != nil {
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
	tracef("ADJUST JUMP CONT: %d", passArgs)

	plan, err := planContinuationStack(s.Stack, c.GetControlData(), passArgs, continuationActionJump)
	if err != nil {
		return nil, err
	}

	if err = plan.applyJumpStack(s); err != nil {
		return nil, err
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

		if err = s.ConsumeStackGas(newStack); err != nil {
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
