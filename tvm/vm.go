package tvm

import (
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	_ "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	_ "github.com/xssnick/tonutils-go/tvm/op/dict"
	_ "github.com/xssnick/tonutils-go/tvm/op/exec"
	_ "github.com/xssnick/tonutils-go/tvm/op/funcs"
	_ "github.com/xssnick/tonutils-go/tvm/op/math"
	_ "github.com/xssnick/tonutils-go/tvm/op/stack"
	_ "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
)

type trieNode struct {
	next [2]*trieNode
	op   vm.OPGetter
}

type matchedDeserializer interface {
	DeserializeMatched(code *cell.Slice) error
}

type TVM struct {
	trie          *trieNode
	maxPrefixLen  uint
	globalVersion int
}

func NewTVM() *TVM {
	tvm := &TVM{
		trie:          &trieNode{},
		globalVersion: vm.DefaultGlobalVersion,
	}

	for _, op := range vm.List {
		for _, s := range op().GetPrefixes() {
			tvm.addTriePrefix(s, op)
		}
	}

	return tvm
}

type ExecutionResult struct {
	ExitCode int64
	GasUsed  int64
	Steps    uint32
	Gas      vm.Gas
	Stack    *vm.Stack
	Code     *cell.Cell
	Data     *cell.Cell
	Actions  *cell.Cell
}

func bitAt(data []byte, bit uint) uint8 {
	return (data[bit/8] >> (7 - (bit % 8))) & 1
}

func (tvm *TVM) addTriePrefix(prefix *cell.Slice, op vm.OPGetter) {
	n := tvm.trie
	bits := prefix.BitsLeft()
	raw := prefix.MustPreloadSlice(bits)

	if bits > tvm.maxPrefixLen {
		tvm.maxPrefixLen = bits
	}

	for i := uint(0); i < bits; i++ {
		b := bitAt(raw, i)
		if n.next[b] == nil {
			n.next[b] = &trieNode{}
		}
		n = n.next[b]
	}

	n.op = op
}

func (tvm *TVM) matchOpcode(code *cell.Slice) vm.OPGetter {
	limit := code.BitsLeft()
	if limit == 0 {
		return nil
	}
	if limit > tvm.maxPrefixLen {
		limit = tvm.maxPrefixLen
	}

	n := tvm.trie
	var matched vm.OPGetter

	for i := uint(0); i < limit; i++ {
		bit, err := code.BitAt(i)
		if err != nil {
			break
		}
		n = n.next[bit]
		if n == nil {
			break
		}
		if n.op != nil {
			matched = n.op
		}
	}

	return matched
}

func (tvm *TVM) Execute(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack) error {
	_, err := tvm.ExecuteDetailed(code, data, c7, gas, stack)
	return err
}

func (tvm *TVM) ExecuteDetailed(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack) (*ExecutionResult, error) {
	return tvm.ExecuteDetailedWithLibraries(code, data, c7, gas, stack)
}

func (tvm *TVM) ExecuteDetailedWithLibraries(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, libraries ...*cell.Cell) (*ExecutionResult, error) {
	state := vm.NewExecutionState(tvm.globalVersion, gas, data, c7, stack, libraries...)
	state.SetChildRunner(tvm.runState)
	state.PrepareExecution(code.BeginParseNoCopy())

	exitCode, err := tvm.runState(state)

	dataRes := state.Reg.D[0]
	actionsRes := state.Reg.D[1]
	if state.Committed.Committed {
		dataRes = state.Committed.Data
		actionsRes = state.Committed.Actions
	}

	return &ExecutionResult{
		ExitCode: exitCode,
		GasUsed:  state.Gas.Used(),
		Steps:    state.Steps,
		Gas:      state.Gas,
		Stack:    state.Stack,
		Code:     code,
		Data:     dataRes,
		Actions:  actionsRes,
	}, err
}

func (tvm *TVM) runState(state *vm.State) (exitCode int64, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			exitCode = vmerr.CodeFatal
			err = vmerr.Error(vmerr.CodeFatal, fmt.Sprintf("vm panic: %v", recovered))
		}
	}()

	if err = tvm.execute(state); err != nil {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) {
			return 0, err
		}

		exitCode = vmErr.Code
		if !vm.IsSuccessExitCode(exitCode) {
			return exitCode, err
		}
	}

	if state.TryCommitCurrent() {
		return exitCode, nil
	}
	return vmerr.CodeCellOverflow, vmerr.Error(vmerr.CodeCellOverflow, "cannot commit too deep cells as new data/actions")
}

func (tvm *TVM) execute(state *vm.State) (err error) {
	for {
		for state.CurrentCode.BitsLeft() > 0 || state.CurrentCode.RefsNum() > 0 {
			if state.CurrentCode.BitsLeft() == 0 {
				cc, err := state.Cells.LoadRef(state.CurrentCode)
				if err != nil {
					return err
				}

				c := &vm.OrdinaryContinuation{
					Data: vm.ControlData{
						CP:      vm.CP,
						NumArgs: vm.ControlDataAllArgs,
					},
					Code: cc,
				}

				if err = state.ConsumeGas(vm.ImplicitJmprefGasPrice); err != nil {
					return err
				}

				// implicit JMPREF
				vm.Tracef("implicit JMPREF")
				if err = state.Jump(c); err != nil {
					return err
				}
			}

			if err = tvm.step(state); err != nil {
				var e vmerr.VMError
				var handled vm.HandledException
				if errors.As(err, &e) && e.Code == vmerr.CodeOutOfGas {
					if stackErr := state.HandleOutOfGas(); stackErr != nil {
						return stackErr
					}
					return err
				}
				if errors.As(err, &handled) {
					return err
				}
				if state.Reg.C[2] != nil && errors.As(err, &e) && !vm.IsSuccessExitCode(e.Code) {
					vm.Tracef("[EXCEPTION] %d %s", e.Code, e.Msg)

					if err = state.ThrowException(big.NewInt(e.Code)); err == nil {
						continue
					}
				}

				return err
			}
			state.Steps++
		}

		vm.Tracef("implicit RET")
		if err = state.ConsumeGas(vm.ImplicitRetGasPrice); err != nil {
			return err
		}
		vm.Tracef("gas remaining: %d", state.Gas.Remaining)

		if err = state.Return(); err != nil {
			return err
		}
	}
}

func (tvm *TVM) step(state *vm.State) (err error) {
	px := tvm.matchOpcode(state.CurrentCode)
	if px == nil {
		return fmt.Errorf("opcode not found: %w (%s)", vm.ErrCorruptedOpcode, state.CurrentCode.String())
	}

	op := px()

	if fast, ok := op.(matchedDeserializer); ok {
		err = fast.DeserializeMatched(state.CurrentCode)
	} else {
		err = op.Deserialize(state.CurrentCode)
	}
	if err != nil {
		return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeText(), err)
	}
	if gasOp, ok := op.(vm.GasPricedOp); ok {
		if err = state.ConsumeGas(vm.InstructionBaseGasPrice + gasOp.InstructionBits()); err != nil {
			return err
		}
	}
	if err = state.CheckGas(); err != nil {
		return err
	}

	vm.Tracef("%s", op.SerializeText())
	err = op.Interpret(state)
	if err != nil {
		return err
	}
	if err = state.CheckGas(); err != nil {
		return err
	}
	vm.Tracef("gas remaining: %d", state.Gas.Remaining)

	return nil
}
