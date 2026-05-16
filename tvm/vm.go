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
	"os"
	"runtime/debug"
	"strings"
)

type trieNode struct {
	next [2]*trieNode
	op   vm.OPGetter
}

type matchedDeserializer interface {
	DeserializeMatched(code *cell.Slice) error
}

type reusableOP interface {
	Reusable() bool
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

	for _, opGetter := range vm.List {
		op := opGetter()
		getter := opGetter
		if reusable, ok := op.(reusableOP); ok && reusable.Reusable() {
			getter = cachedOPGetter(op)
		}
		for _, s := range op.GetPrefixes() {
			tvm.addTriePrefix(s, getter)
		}
	}

	return tvm
}

func cachedOPGetter(op vm.OP) vm.OPGetter {
	return func() vm.OP {
		return op
	}
}

func (tvm *TVM) SetGlobalVersion(version int) error {
	if version < MinSupportedGlobalVersion {
		return fmt.Errorf("unsupported global version %d, minimum supported is %d", version, MinSupportedGlobalVersion)
	}

	tvm.globalVersion = version
	return nil
}

type ExecutionResult struct {
	ExitCode  int64
	GasUsed   int64
	Steps     uint32
	Gas       vm.Gas
	Stack     *vm.Stack
	Code      *cell.Cell
	Data      *cell.Cell
	Actions   *cell.Cell
	Committed bool
	Proof     *cell.Cell
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
	available := code.BitsLeft()
	if available == 0 {
		return nil
	}

	if tvm.maxPrefixLen <= 64 {
		return tvm.matchOpcodeFast(code, available)
	}
	return tvm.matchOpcodeSlow(code, available)
}

func (tvm *TVM) matchOpcodeFast(code *cell.Slice, available uint) vm.OPGetter {
	preloadBits := tvm.maxPrefixLen
	if available < preloadBits {
		preloadBits = available
	}
	raw, err := code.PreloadUInt(preloadBits)
	if err != nil {
		return nil
	}

	n := tvm.trie
	var matched vm.OPGetter

	for i := uint(0); i < tvm.maxPrefixLen; i++ {
		bit := uint8(0)
		if i < preloadBits {
			bit = uint8((raw >> (preloadBits - 1 - i)) & 1)
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

func (tvm *TVM) matchOpcodeSlow(code *cell.Slice, available uint) vm.OPGetter {
	n := tvm.trie
	var matched vm.OPGetter

	for i := uint(0); i < tvm.maxPrefixLen; i++ {
		bit := uint8(0)
		if i < available {
			var err error
			bit, err = code.BitAt(i)
			if err != nil {
				break
			}
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
	return tvm.executeDetailedWithLibrariesRawOptions(code, data, c7, gas, stack, executeOptions{}, libraries...)
}

type executeOptions struct {
	stopOnAccept    bool
	proof           *cell.MerkleProofBuilder
	skipFinalCommit bool
}

func (tvm *TVM) executeDetailedWithLibrariesRawOptions(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, options executeOptions, libraries ...*cell.Cell) (*ExecutionResult, error) {
	state := vm.NewExecutionState(tvm.globalVersion, gas, data, c7, stack, libraries...)
	state.StopOnAccept = options.stopOnAccept
	state.SetChildRunner(tvm.runState)
	state.InitForExecution()
	currentCode, err := state.Cells.BeginParseAlreadyLoaded(code)
	if err != nil {
		res := executionResultFromState(vmerrCode(err), state, code, data)
		if proofErr := attachExecutionProof(res, state, options.proof); proofErr != nil {
			return res, proofErr
		}
		return res, err
	}
	state.CurrentCode = currentCode
	state.Reg.C[3] = &vm.OrdinaryContinuation{
		Data: vm.ControlData{
			CP:      vm.CP,
			NumArgs: vm.ControlDataAllArgs,
		},
		Code: currentCode.Copy(),
	}

	exitCode, err := tvm.runStateWithOptions(state, options.skipFinalCommit)

	dataRes := state.Reg.D[0]
	actionsRes := state.Reg.D[1]
	if state.Committed.Committed {
		dataRes = state.Committed.Data
		actionsRes = state.Committed.Actions
	}

	res := &ExecutionResult{
		ExitCode:  exitCode,
		GasUsed:   state.Gas.Used(),
		Steps:     state.Steps,
		Gas:       state.Gas,
		Stack:     state.Stack,
		Code:      code,
		Data:      dataRes,
		Actions:   actionsRes,
		Committed: state.Committed.Committed,
	}
	if proofErr := attachExecutionProof(res, state, options.proof); proofErr != nil {
		return res, proofErr
	}
	return res, err
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
		ExitCode:  exitCode,
		GasUsed:   state.Gas.Used(),
		Steps:     state.Steps,
		Gas:       state.Gas,
		Stack:     state.Stack,
		Code:      code,
		Data:      dataRes,
		Actions:   actionsRes,
		Committed: state.Committed.Committed,
	}
}

func vmerrCode(err error) int64 {
	if code, ok := vmerr.ErrorCode(err); ok {
		return code
	}
	return vmerr.CodeFatal
}

func (tvm *TVM) runState(state *vm.State) (exitCode int64, err error) {
	return tvm.runStateWithOptions(state, false)
}

func (tvm *TVM) runStateWithOptions(state *vm.State, skipFinalCommit bool) (exitCode int64, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			exitCode = vmerr.CodeFatal
			if os.Getenv("TVM_PANIC_STACK") != "" {
				err = fmt.Errorf("vm panic: %v\n%s", recovered, debug.Stack())
				return
			}
			err = fmt.Errorf("vm panic: %v", recovered)
		}
	}()

	if err = tvm.execute(state); err != nil {
		if code, ok := vmerr.ErrorCode(err); ok {
			exitCode = code
			var handled vm.HandledException
			if exitCode == vmerr.CodeOutOfGas && !errors.As(err, &handled) {
				exitCode = ^exitCode
			}
			if !vm.IsSuccessExitCode(exitCode) {
				return exitCode, err
			}
		} else {
			return 0, err
		}
	}

	if skipFinalCommit {
		return exitCode, nil
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
			if errors.As(err, &handled) {
				return err
			}
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

		if err := state.ConsumeGas(vm.ImplicitJmprefGasPrice); err != nil {
			return err
		}

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

func normalizeOpcodeDeserializeError(err error, op vm.OP) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, vm.ErrCorruptedOpcode) ||
		errors.Is(err, cell.ErrNoMoreRefs) ||
		errors.Is(err, cell.ErrSmallSlice) ||
		strings.Contains(err.Error(), "not enough data") {
		return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("deserialize opcode [%s] failed", op.SerializeText()))
	}
	return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeText(), err)
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
		return normalizeOpcodeDeserializeError(err, op)
	}
	if err = consumeInstructionGas(state, op); err != nil {
		return err
	}
	if err = state.CheckGas(); err != nil {
		return err
	}

	if vm.TraceHook != nil {
		vm.Tracef("%s", op.SerializeText())
	}

	stackBefore := state.Stack.Checkpoint()
	err = op.Interpret(state)
	if err != nil {
		var handled vm.HandledException
		if code, ok := vmerr.ErrorCode(err); ok && code == vmerr.CodeStackUnderflow && !errors.As(err, &handled) {
			state.Stack.RestoreCheckpoint(stackBefore)
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
