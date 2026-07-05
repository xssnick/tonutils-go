package vm

import (
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

	start := child.Len() - count
	if start < 0 {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	for _, val := range child.elems[start:] {
		val = unbindValueTrace(val, child.trace)
		if err := parent.PushAny(val); err != nil {
			return err
		}
	}
	return nil
}

func unbindCellTrace(cl *cell.Cell, trace *cell.Trace) *cell.Cell {
	if cl == nil || trace == nil {
		return cl
	}
	return cl.WithTrace(cl.Trace().WithoutTrace(trace))
}

func pushCommittedResultCell(parent *Stack, committed bool, value *cell.Cell) error {
	if !committed {
		return parent.PushAny(nil)
	}
	return pushMaybeCell(parent, value)
}

func childResultRegisterValue(child *State, committedValue, currentValue *cell.Cell) *cell.Cell {
	if child.Committed.Committed {
		return committedValue
	}
	if child.effectiveGlobalVersion() < 11 {
		return currentValue
	}
	return nil
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

	parentTrace := s.Cells.Trace()
	childStack := cfg.Stack.WithoutTrace(parentTrace)
	childC7 := unbindTupleTrace(cfg.C7, parentTrace)
	childCode := cfg.Code.Copy().SetTrace(cfg.Code.Trace().WithoutTrace(parentTrace))

	child := NewExecutionStateWithGlobalVersion(s.effectiveGlobalVersion(), cfg.Gas, cfg.Data, childC7, childStack)
	child.CurrentCode = childCode

	if cfg.IsolateGas {
		if err := s.FlushFreeGas(); err != nil {
			return err
		}
		s.ChksgnCounter = 0
		s.GetExtraBalanceCounter = 0
		s.Gas.FreeConsumed = 0
	} else {
		child.Cells.loaded = s.Cells.loaded
	}

	child.ChksgnCounter = s.ChksgnCounter
	child.GetExtraBalanceCounter = s.GetExtraBalanceCounter
	child.Gas.FreeConsumed = s.Gas.FreeConsumed

	if s.effectiveGlobalVersion() >= 10 {
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
	}

	if cfg.SameC3 {
		child.Reg.C[3] = &OrdinaryContinuation{
			Data: ControlData{
				NumArgs: ControlDataAllArgs,
				CP:      child.CP,
			},
			Code: child.CurrentCode.Copy(),
		}
		if cfg.PushZero {
			if err := child.Stack.PushSmallInt(0); err != nil {
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
	s.GetExtraBalanceCounter = child.GetExtraBalanceCounter
	s.Gas.FreeConsumed = child.Gas.FreeConsumed

	if childErr != nil {
		vmErr, vmErrOk := vmerr.AsVMError(childErr)
		if !vmErrOk && !IsHandledException(childErr) {
			return childErr
		}
		if vmErrOk && vmErr.Code == vmerr.CodeOutOfGas && exitCode >= 0 {
			exitCode = ^exitCode
		}
	}

	childGasUsed := child.Gas.Used()
	chargeGas := childGasUsed
	maxCharge := child.Gas.Limit + 1
	if chargeGas > maxCharge {
		chargeGas = maxCharge
	}
	if err := s.ConsumeGas(chargeGas); err != nil {
		return err
	}

	retCnt := 0
	if IsSuccessExitCode(exitCode) {
		if cfg.ReturnValues >= 0 {
			if child.Stack.Len() >= cfg.ReturnValues {
				retCnt = cfg.ReturnValues
			} else {
				exitCode = ^int64(vmerr.CodeStackUnderflow)
				if err := s.Stack.PushSmallInt(0); err != nil {
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
	if err := s.Stack.PushSmallInt(exitCode); err != nil {
		return err
	}

	childTrace := child.Cells.Trace()
	if cfg.ReturnData {
		data := childResultRegisterValue(child, child.Committed.Data, child.Reg.D[0])
		data = unbindCellTrace(data, childTrace)
		if err := pushMaybeCell(s.Stack, data); err != nil {
			return err
		}
	}

	if cfg.ReturnActions {
		actions := childResultRegisterValue(child, child.Committed.Actions, child.Reg.D[1])
		actions = unbindCellTrace(actions, childTrace)
		if err := pushMaybeCell(s.Stack, actions); err != nil {
			return err
		}
	}

	if cfg.ReturnGas {
		if err := s.Stack.PushSmallInt(childGasUsed); err != nil {
			return err
		}
	}

	return nil
}
