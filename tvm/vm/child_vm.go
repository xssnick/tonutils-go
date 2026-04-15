package vm

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type ChildVMConfig struct {
	Code          *cell.Slice
	Stack         *Stack
	Data          *cell.Cell
	C7            tuple.Tuple
	Gas           Gas
	SameC3        bool
	PushZero      bool
	ReturnData    bool
	ReturnActions bool
	ReturnGas     bool
	IsolateGas    bool
	ReturnValues  int
}

func pushMaybeCell(stack *Stack, cl *cell.Cell) error {
	if cl == nil {
		return stack.PushAny(nil)
	}
	return stack.PushCell(cl)
}

func copyTopValuesToParent(parent, child *Stack, count int) error {
	if count <= 0 {
		return nil
	}

	cp := child.Copy()
	if cp.Len() > count {
		if err := cp.DropAfter(count); err != nil {
			return err
		}
	}

	for _, val := range cp.elems {
		if err := parent.PushAny(val); err != nil {
			return err
		}
	}
	return nil
}

func pushCommittedResultCell(parent *Stack, committed bool, value *cell.Cell) error {
	if !committed {
		return parent.PushAny(nil)
	}
	return pushMaybeCell(parent, value)
}

func (s *State) RunChildVM(cfg ChildVMConfig) error {
	if cfg.Code == nil {
		return vmerr.Error(vmerr.CodeTypeCheck, "child code is nil")
	}
	if cfg.Stack == nil {
		cfg.Stack = NewStack()
	}
	if cfg.ReturnValues < -1 {
		return vmerr.Error(vmerr.CodeRangeCheck, "invalid child return values count")
	}

	child := NewExecutionState(s.GlobalVersion, cfg.Gas, cfg.Data, cfg.C7, cfg.Stack)
	child.CurrentCode = cfg.Code.Copy()

	if cfg.IsolateGas {
		if err := s.FlushFreeGas(); err != nil {
			return err
		}
		s.ChksgnCounter = 0
		s.Gas.FreeConsumed = 0
	} else {
		child.Cells.loaded = s.Cells.loaded
	}

	child.ChksgnCounter = s.ChksgnCounter
	child.Gas.FreeConsumed = s.Gas.FreeConsumed

	remaining := s.Gas.Remaining
	if child.Gas.Limit > remaining {
		child.Gas.Limit = remaining
	}
	if child.Gas.Max > remaining {
		child.Gas.Max = remaining
	}
	if child.Gas.Limit < 0 {
		child.Gas.Limit = 0
	}
	if child.Gas.Max < child.Gas.Limit {
		child.Gas.Max = child.Gas.Limit
	}
	child.Gas.Base = child.Gas.Limit
	child.Gas.Remaining = child.Gas.Base

	if cfg.SameC3 {
		child.Reg.C[3] = &OrdinaryContinuation{
			Data: ControlData{
				NumArgs: ControlDataAllArgs,
				CP:      child.CP,
			},
			Code: child.CurrentCode.Copy(),
		}
		if cfg.PushZero {
			if err := child.Stack.PushInt(big.NewInt(0)); err != nil {
				return err
			}
		}
	}

	exitCode, childErr := s.runChildRaw(child)

	s.Steps += child.Steps
	if !cfg.IsolateGas {
		s.Cells.loaded = child.Cells.loaded
	}
	s.ChksgnCounter = child.ChksgnCounter
	s.Gas.FreeConsumed = child.Gas.FreeConsumed

	if childErr != nil {
		var vmErr vmerr.VMError
		var handled HandledException
		if !errors.As(childErr, &vmErr) && !errors.As(childErr, &handled) {
			return childErr
		}
		if errors.As(childErr, &vmErr) && vmErr.Code == vmerr.CodeOutOfGas {
			exitCode = ^exitCode
		}
	}

	used := child.Gas.Used()
	maxCharge := child.Gas.Limit + 1
	if used > maxCharge {
		used = maxCharge
	}
	if err := s.ConsumeGas(used); err != nil {
		return err
	}

	retCnt := 0
	if IsSuccessExitCode(exitCode) {
		if cfg.ReturnValues >= 0 {
			if child.Stack.Len() >= cfg.ReturnValues {
				retCnt = cfg.ReturnValues
			} else {
				exitCode = ^int64(vmerr.CodeStackUnderflow)
				if err := s.Stack.PushInt(big.NewInt(0)); err != nil {
					return err
				}
			}
		} else {
			retCnt = child.Stack.Len()
		}
	} else if child.Stack.Len() > 0 {
		retCnt = 1
	}

	if err := s.ConsumeStackGasLen(retCnt); err != nil {
		return err
	}
	if err := copyTopValuesToParent(s.Stack, child.Stack, retCnt); err != nil {
		return err
	}
	if err := s.Stack.PushInt(big.NewInt(exitCode)); err != nil {
		return err
	}

	if cfg.ReturnData {
		if err := pushCommittedResultCell(s.Stack, child.Committed.Committed, child.Committed.Data); err != nil {
			return err
		}
	}

	if cfg.ReturnActions {
		if err := pushCommittedResultCell(s.Stack, child.Committed.Committed, child.Committed.Actions); err != nil {
			return err
		}
	}

	if cfg.ReturnGas {
		if err := s.Stack.PushInt(big.NewInt(child.Gas.Used())); err != nil {
			return err
		}
	}

	return nil
}
