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
	"strings"
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

const MinSupportedGlobalVersion = 13

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

func (tvm *TVM) SetGlobalVersion(version int) error {
	if version < MinSupportedGlobalVersion {
		return fmt.Errorf("unsupported global version %d, minimum supported is %d", version, MinSupportedGlobalVersion)
	}

	tvm.globalVersion = version
	return nil
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
	_, err := tvm.executeDetailedWithLibrariesRaw(code, data, c7, gas, stack)
	return err
}

func (tvm *TVM) ExecuteDetailed(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack) (*ExecutionResult, error) {
	return tvm.ExecuteDetailedWithLibraries(code, data, c7, gas, stack)
}

func (tvm *TVM) ExecuteDetailedWithLibraries(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, libraries ...*cell.Cell) (*ExecutionResult, error) {
	res, err := tvm.executeDetailedWithLibrariesRaw(code, data, c7, gas, stack, libraries...)
	if err != nil {
		if _, ok := vmerr.ErrorCode(err); ok {
			return res, nil
		}
		return nil, err
	}
	return res, nil
}

func (tvm *TVM) executeDetailedWithLibrariesRaw(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, libraries ...*cell.Cell) (*ExecutionResult, error) {
	return tvm.executeDetailedWithLibrariesRawOptions(code, data, c7, gas, stack, false, libraries...)
}

func (tvm *TVM) executeDetailedWithLibrariesRawOptions(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, stopOnAccept bool, libraries ...*cell.Cell) (*ExecutionResult, error) {
	state := vm.NewExecutionState(tvm.globalVersion, gas, data, c7, stack, libraries...)
	state.StopOnAccept = stopOnAccept
	state.SetChildRunner(tvm.runState)
	state.InitForExecution()
	currentCode, err := state.Cells.BeginParse(code)
	if err != nil {
		return executionResultFromState(vmerrCode(err), state, code, data), err
	}
	state.CurrentCode = currentCode

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

func executionResultFromState(exitCode int64, state *vm.State, code, data *cell.Cell) *ExecutionResult {
	dataRes := state.Reg.D[0]
	actionsRes := state.Reg.D[1]
	if state.Committed.Committed {
		dataRes = state.Committed.Data
		actionsRes = state.Committed.Actions
	}
	if dataRes == nil {
		dataRes = data
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
	}
}

func vmerrCode(err error) int64 {
	if code, ok := vmerr.ErrorCode(err); ok {
		return code
	}
	return vmerr.CodeFatal
}

func (tvm *TVM) runState(state *vm.State) (exitCode int64, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			exitCode = vmerr.CodeFatal
			err = fmt.Errorf("vm panic: %v", recovered)
		}
	}()

	if err = tvm.execute(state); err != nil {
		if code, ok := vmerr.ErrorCode(err); ok {
			exitCode = code
			if !vm.IsSuccessExitCode(exitCode) {
				return exitCode, err
			}
		} else {
			return 0, err
		}
	}

	if state.TryCommitCurrent() {
		return exitCode, nil
	}
	state.Stack.Clear()
	if stackErr := state.Stack.PushInt(big.NewInt(0)); stackErr != nil {
		return 0, stackErr
	}
	return vmerr.CodeCellOverflow, vmerr.Error(vmerr.CodeCellOverflow, "cannot commit too deep cells as new data/actions")
}

func (tvm *TVM) execute(state *vm.State) (err error) {
	for {
		if err = tvm.stepAny(state); err != nil {
			if errors.Is(err, vm.ErrStopOnAccept) {
				return nil
			}
			var e vmerr.VMError
			var virt vmerr.VirtualizationError
			var handled vm.HandledException
			if errors.As(err, &e) && e.Code == vmerr.CodeOutOfGas {
				state.Steps++
				if stackErr := state.HandleOutOfGas(); stackErr != nil {
					return stackErr
				}
				return err
			}
			if errors.As(err, &virt) {
				return err
			}
			if errors.As(err, &handled) {
				return err
			}
			if state.Reg.C[2] != nil && errors.As(err, &e) && !vm.IsSuccessExitCode(e.Code) {
				vm.Tracef("[EXCEPTION] %d %s", e.Code, e.Msg)

				state.Steps++
				if err = state.ThrowException(big.NewInt(e.Code)); err == nil {
					continue
				}
				if errors.As(err, &e) && e.Code == vmerr.CodeOutOfGas {
					state.Steps++
					if stackErr := state.HandleOutOfGas(); stackErr != nil {
						return stackErr
					}
					return err
				}
			}

			return err
		}
	}
}

func (tvm *TVM) stepAny(state *vm.State) error {
	if state.CurrentCode.BitsLeft() > 0 {
		state.Steps++
		return tvm.step(state)
	}

	if state.CurrentCode.RefsNum() > 0 {
		state.Steps++

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

		vm.Tracef("implicit JMPREF")
		return state.Jump(c)
	}

	state.Steps++
	vm.Tracef("implicit RET")
	if err := state.ConsumeGas(vm.ImplicitRetGasPrice); err != nil {
		return err
	}
	vm.Tracef("gas remaining: %d", state.Gas.Remaining)

	return state.Return()
}

func normalizeCellError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := vmerr.ErrorCode(err); ok {
		return err
	}

	switch {
	case strings.Contains(err.Error(), "not enough data"),
		errors.Is(err, cell.ErrNoMoreRefs),
		errors.Is(err, cell.ErrSmallSlice):
		return vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
	case errors.Is(err, cell.ErrNotFit1023),
		errors.Is(err, cell.ErrTooMuchRefs),
		errors.Is(err, cell.ErrCellDepthLimit),
		errors.Is(err, cell.ErrRefCannotBeNil):
		return vmerr.Error(vmerr.CodeCellOverflow, err.Error())
	case errors.Is(err, cell.ErrTooBigValue),
		errors.Is(err, cell.ErrNegative),
		errors.Is(err, cell.ErrInvalidSize),
		errors.Is(err, cell.ErrNilBigInt),
		errors.Is(err, cell.ErrTooBigSize):
		return vmerr.Error(vmerr.CodeRangeCheck, err.Error())
	default:
		return err
	}
}

func (tvm *TVM) step(state *vm.State) (err error) {
	px := tvm.matchOpcode(state.CurrentCode)
	if px == nil {
		if err = state.ConsumeGas(vm.InstructionBaseGasPrice); err != nil {
			return err
		}
		return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("opcode not found: %s", state.CurrentCode.String()))
	}

	op := px()

	if fast, ok := op.(matchedDeserializer); ok {
		err = fast.DeserializeMatched(state.CurrentCode)
	} else {
		err = op.Deserialize(state.CurrentCode)
	}
	if err != nil {
		if gasErr := consumeInstructionGas(state, op); gasErr != nil {
			return gasErr
		}
		if errors.Is(err, vm.ErrCorruptedOpcode) {
			return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("deserialize opcode [%s] failed", op.SerializeText()))
		}
		return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeText(), err)
	}
	if err = consumeInstructionGas(state, op); err != nil {
		return err
	}
	if err = state.CheckGas(); err != nil {
		return err
	}

	vm.Tracef("%s", op.SerializeText())
	stackBefore := state.Stack.Snapshot()
	err = op.Interpret(state)
	if err != nil {
		if code, ok := vmerr.ErrorCode(err); ok && code == vmerr.CodeStackUnderflow {
			state.Stack.Restore(stackBefore)
		}
		err = normalizeCellError(err)
		return err
	}
	if err = state.CheckGas(); err != nil {
		return err
	}
	vm.Tracef("gas remaining: %d", state.Gas.Remaining)

	return nil
}

func consumeInstructionGas(state *vm.State, op vm.OP) error {
	gasOp, ok := op.(vm.GasPricedOp)
	if !ok {
		return nil
	}
	return state.ConsumeGas(vm.InstructionBaseGasPrice + gasOp.InstructionBits())
}
