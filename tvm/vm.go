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
	"sync"
)

type trieNode struct {
	next [2]*trieNode
	op   vm.OPGetter
}

const opcodeDispatchIndexBits = 16

type opcodeDispatch struct {
	root         *trieNode
	maxPrefixLen uint
	index        [1 << opcodeDispatchIndexBits]opcodeDispatchPrefix
}

type opcodeDispatchPrefix struct {
	node    *trieNode
	matched vm.OPGetter
}

type matchedDeserializer interface {
	DeserializeMatched(code *cell.Slice) error
}

type reusableOP interface {
	Reusable() bool
}

type TVM struct {
	dispatches [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch
}

var (
	sharedOpcodeDispatchesOnce sync.Once
	sharedOpcodeDispatches     [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch
	sharedOpcodeDispatchesLen  int
)

func NewTVM() *TVM {
	return &TVM{dispatches: getOpcodeDispatches()}
}

func getOpcodeDispatches() [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch {
	dispatches := getSharedOpcodeDispatches()
	if len(vm.List) == sharedOpcodeDispatchesLen {
		return dispatches
	}
	return buildOpcodeDispatches()
}

func getSharedOpcodeDispatches() [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch {
	sharedOpcodeDispatchesOnce.Do(buildSharedOpcodeDispatches)
	return sharedOpcodeDispatches
}

func buildSharedOpcodeDispatches() {
	sharedOpcodeDispatches = buildOpcodeDispatches()
	sharedOpcodeDispatchesLen = len(vm.List)
}

func buildOpcodeDispatches() [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch {
	var dispatches [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch
	for ver := 0; ver <= vm.MaxSupportedGlobalVersion; ver++ {
		dispatches[ver] = newOpcodeDispatch()
	}

	for _, opGetter := range vm.List {
		op := opGetter()
		getter := opGetter
		if reusable, ok := op.(reusableOP); ok && reusable.Reusable() {
			getter = cachedOPGetter(op)
		}
		prefixes := op.GetPrefixes()
		minVersion := opcodeMinVersion(op)
		if minVersion < 0 {
			minVersion = 0
		}
		var invalidGetter vm.OPGetter
		for _, s := range prefixes {
			if minVersion > 0 && opcodeIsHistoricalQuietCompoundPrefix(s) {
				if invalidGetter == nil {
					invalidGetter = historicalInvalidOpcodeGetter(op)
				}
				for ver := 0; ver < minVersion && ver <= vm.MaxSupportedGlobalVersion; ver++ {
					dispatches[ver].addPrefix(s, invalidGetter)
				}
			}
			for ver := minVersion; ver <= vm.MaxSupportedGlobalVersion; ver++ {
				dispatches[ver].addPrefix(s, getter)
			}
		}
	}
	for ver := 0; ver <= vm.MaxSupportedGlobalVersion; ver++ {
		dispatches[ver].buildFastTable()
	}
	return dispatches
}

func cachedOPGetter(op vm.OP) vm.OPGetter {
	return func() vm.OP {
		return op
	}
}

func opcodeMinVersion(op vm.OP) int {
	if versioned, ok := op.(vm.VersionedOp); ok {
		return versioned.MinGlobalVersion()
	}
	return 0
}

func opcodeIsHistoricalQuietCompoundPrefix(prefix *cell.Slice) bool {
	if prefix == nil || prefix.BitsLeft() != 24 {
		return false
	}

	value, err := prefix.PreloadUInt(24)
	if err != nil {
		return false
	}

	args := uint8(value & 0x0f)
	if args&3 == 3 || (args>>2)&3 != 0 {
		return false
	}

	switch value >> 4 {
	case 0xb7a90, 0xb7a92, 0xb7a98, 0xb7a9a, 0xb7a9c:
		return true
	default:
		return false
	}
}

type historicalInvalidOpcode struct {
	op vm.OP
}

func historicalInvalidOpcodeGetter(op vm.OP) vm.OPGetter {
	invalid := historicalInvalidOpcode{op: op}
	if _, ok := op.(vm.GasPricedOp); ok {
		return cachedOPGetter(historicalInvalidOpcodeGas{historicalInvalidOpcode: invalid})
	}
	return cachedOPGetter(invalid)
}

func (op historicalInvalidOpcode) GetPrefixes() []*cell.Slice {
	return op.op.GetPrefixes()
}

func (op historicalInvalidOpcode) Deserialize(code *cell.Slice) error {
	return op.op.Deserialize(code)
}

func (op historicalInvalidOpcode) DeserializeMatched(code *cell.Slice) error {
	if matched, ok := op.op.(matchedDeserializer); ok {
		return matched.DeserializeMatched(code)
	}
	return op.op.Deserialize(code)
}

func (op historicalInvalidOpcode) Serialize() *cell.Builder {
	return op.op.Serialize()
}

func (op historicalInvalidOpcode) SerializeText() string {
	return op.op.SerializeText()
}

func (op historicalInvalidOpcode) Interpret(state *vm.State) error {
	return vmerr.Error(vmerr.CodeInvalidOpcode)
}

type historicalInvalidOpcodeGas struct {
	historicalInvalidOpcode
}

func (op historicalInvalidOpcodeGas) InstructionBits() int64 {
	return op.op.(vm.GasPricedOp).InstructionBits()
}

// AllowHigherVersionExecUsingLatest allows preparing configs with a global
// version higher than vm.MaxSupportedGlobalVersion. When enabled, execution
// uses the latest supported version.
var AllowHigherVersionExecUsingLatest = true

func validateGlobalVersion(version int) error {
	if version < 0 {
		return fmt.Errorf("unsupported global version %d, minimum supported is %d", version, 0)
	}
	if version > vm.MaxSupportedGlobalVersion && !AllowHigherVersionExecUsingLatest {
		return fmt.Errorf("unsupported global version %d, maximum supported is %d", version, vm.MaxSupportedGlobalVersion)
	}
	return nil
}

func effectiveGlobalVersion(version uint32) uint32 {
	if version > uint32(vm.MaxSupportedGlobalVersion) {
		return uint32(vm.MaxSupportedGlobalVersion)
	}
	return version
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

type ExecutionConfig struct {
	AccountRoot                 *cell.Cell
	Libraries                   []*cell.Cell
	SignatureCheckAlwaysSucceed bool
	Config                      *PreparedBlockchainConfig
}

func bitAt(data []byte, bit uint) uint8 {
	return (data[bit/8] >> (7 - (bit % 8))) & 1
}

func newOpcodeDispatch() *opcodeDispatch {
	return &opcodeDispatch{root: &trieNode{}}
}

func (tvm *TVM) addTriePrefix(prefix *cell.Slice, op vm.OPGetter) {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	dispatch.addPrefix(prefix, op)
	dispatch.buildFastTable()
}

func (dispatch *opcodeDispatch) addPrefix(prefix *cell.Slice, op vm.OPGetter) {
	n := dispatch.root
	bits := prefix.BitsLeft()
	raw := prefix.MustPreloadSlice(bits)

	if bits > dispatch.maxPrefixLen {
		dispatch.maxPrefixLen = bits
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

func (dispatch *opcodeDispatch) buildFastTable() {
	for raw := range dispatch.index {
		n := dispatch.root
		var matched vm.OPGetter

		for bit := uint(0); bit < opcodeDispatchIndexBits; bit++ {
			b := uint8((uint(raw) >> (opcodeDispatchIndexBits - 1 - bit)) & 1)
			n = n.next[b]
			if n == nil {
				break
			}
			if n.op != nil {
				matched = n.op
			}
		}

		dispatch.index[raw] = opcodeDispatchPrefix{
			node:    n,
			matched: matched,
		}
	}
}

func (tvm *TVM) matchOpcode(code *cell.Slice) vm.OPGetter {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	return matchOpcode(dispatch, code)
}

func (tvm *TVM) matchOpcodeFast(code *cell.Slice, available uint) vm.OPGetter {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	return matchOpcodeFast(dispatch, code, available)
}

func (tvm *TVM) matchOpcodeSlow(code *cell.Slice, available uint) vm.OPGetter {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	return matchOpcodeSlow(dispatch.root, dispatch.maxPrefixLen, code, available)
}

func (dispatch *opcodeDispatch) match(code *cell.Slice) vm.OPGetter {
	return matchOpcode(dispatch, code)
}

func matchOpcode(dispatch *opcodeDispatch, code *cell.Slice) vm.OPGetter {
	available := code.BitsLeft()
	if available == 0 {
		return nil
	}

	maxPrefixLen := dispatch.maxPrefixLen
	if maxPrefixLen <= 64 {
		return matchOpcodeFast(dispatch, code, available)
	}
	return matchOpcodeSlow(dispatch.root, maxPrefixLen, code, available)
}

func matchOpcodeFast(dispatch *opcodeDispatch, code *cell.Slice, available uint) vm.OPGetter {
	maxPrefixLen := dispatch.maxPrefixLen
	indexBits := uint(opcodeDispatchIndexBits)
	preloadBits := indexBits
	if available < preloadBits {
		preloadBits = available
	}
	raw, err := code.PreloadUInt(preloadBits)
	if err != nil {
		return nil
	}

	var idx uint64
	if preloadBits >= indexBits {
		idx = raw & ((1 << opcodeDispatchIndexBits) - 1)
	} else {
		idx = raw << (indexBits - preloadBits)
	}

	entry := dispatch.index[idx]
	n := entry.node
	matched := entry.matched
	if n == nil {
		return matched
	}

	if available > preloadBits && maxPrefixLen > preloadBits {
		preloadBits = maxPrefixLen
		if available < preloadBits {
			preloadBits = available
		}
		raw, err = code.PreloadUInt(preloadBits)
		if err != nil {
			return nil
		}
	}

	for i := indexBits; i < maxPrefixLen; i++ {
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

func (dispatch *opcodeDispatch) matchSlow(code *cell.Slice, available uint) vm.OPGetter {
	return matchOpcodeSlow(dispatch.root, dispatch.maxPrefixLen, code, available)
}

func matchOpcodeSlow(root *trieNode, maxPrefixLen uint, code *cell.Slice, available uint) vm.OPGetter {
	n := root
	var matched vm.OPGetter

	for i := uint(0); i < maxPrefixLen; i++ {
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

func (tvm *TVM) Execute(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, cfg ExecutionConfig) (*ExecutionResult, error) {
	return tvm.executeWithConfig(code, data, c7, gas, stack, cfg, executeOptionsFromConfig(cfg))
}

func (tvm *TVM) ExecuteGetMethod(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, cfg ExecutionConfig) (*ExecutionResult, error) {
	options := executeOptionsFromConfig(cfg)
	options.skipFinalCommit = true
	return tvm.executeWithConfig(code, data, c7, gas, stack, cfg, options)
}

func (tvm *TVM) executeWithConfig(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, cfg ExecutionConfig, options executeOptions) (*ExecutionResult, error) {
	if cfg.Config == nil {
		return nil, errConfigRootRequired
	}

	libraries := cfg.Libraries
	if cfg.AccountRoot != nil {
		var res *ExecutionResult
		var err error
		code, data, libraries, options.proof, res, err = prepareAccountExecution(code, data, gas, stack, cfg)
		if err != nil || res != nil {
			return res, err
		}
	}

	res, err := tvm.executeWithOptions(code, data, c7, gas, stack, cfg.Config, options, libraries...)
	return finishExecutionResult(res, err)
}

func finishExecutionResult(res *ExecutionResult, err error) (*ExecutionResult, error) {
	if err != nil {
		if _, ok := vmerr.ErrorCode(err); ok {
			return res, nil
		}
		return nil, err
	}
	return res, nil
}

type executeOptions struct {
	stopOnAccept                bool
	proof                       *cell.MerkleProofBuilder
	skipFinalCommit             bool
	traceHook                   vm.TraceHook
	signatureCheckAlwaysSucceed bool
	maxVMDataDepth              uint16
	libraryLoadLimit            *uint32
}

func executeOptionsFromConfig(cfg ExecutionConfig) executeOptions {
	return executeOptions{
		signatureCheckAlwaysSucceed: cfg.SignatureCheckAlwaysSucceed,
		maxVMDataDepth:              vm.MaxDataDepth,
	}
}

// dryRunLibraryLoadLimit implements the tonlib SmartContract get-method /
// external-message / internal-message dry-run policy (max_smc_library_loads
// in crypto/smc-envelope/SmartContract.cpp): the cap always starts at 8 and
// the blockchain config can only lower it, never raise or remove it.
func dryRunLibraryLoadLimit(cfg *PreparedBlockchainConfig) uint32 {
	const maxSmcLibraryLoads = 8
	limit := uint32(maxSmcLibraryLoads)
	if cfg != nil && cfg.sizeLimits.maxTransactionLibraryLoads != nil && *cfg.sizeLimits.maxTransactionLibraryLoads < limit {
		limit = *cfg.sizeLimits.maxTransactionLibraryLoads
	}
	return limit
}

func ptrTo[T any](v T) *T {
	return &v
}

func (tvm *TVM) executeWithOptions(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, cfg *PreparedBlockchainConfig, options executeOptions, libraries ...*cell.Cell) (*ExecutionResult, error) {
	if cfg == nil {
		return nil, errConfigRootRequired
	}

	options.maxVMDataDepth = cfg.sizeLimits.maxVMDataDepth
	options.libraryLoadLimit = ptrTo(dryRunLibraryLoadLimit(cfg))

	state := vm.NewExecutionState(int(cfg.GlobalVersion()), gas, data, c7, stack, libraries...)
	return tvm.executeState(state, code, data, options)
}

// executeState runs an already-constructed execution state. Message emulation
// enters here directly with a c7 pre-bound to the state's gas trace, so the
// c7 bind in InitForExecution hits the fast path.
func (tvm *TVM) executeState(state *vm.State, code, data *cell.Cell, options executeOptions) (*ExecutionResult, error) {
	state.StopOnAccept = options.stopOnAccept
	state.TraceHook = options.traceHook
	state.SignatureCheckAlwaysSucceed = options.signatureCheckAlwaysSucceed
	state.SetChildRunner(tvm.runState)
	state.SetMaxDataDepth(options.maxVMDataDepth)
	if options.libraryLoadLimit != nil {
		state.SetMaxLibraryLoads(*options.libraryLoadLimit)
	}
	state.InitForExecution()
	currentCode, err := tvm.convertExecutionCodeCell(state, code)
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

func (tvm *TVM) convertExecutionCodeCell(state *vm.State, code *cell.Cell) (*cell.Slice, error) {
	if code == nil {
		return state.Cells.BeginParseAlreadyLoaded(code)
	}

	if state.GlobalVersion >= 9 {
		currentCode, err := state.Cells.BeginParseAlreadyLoaded(code)
		if err == nil {
			return currentCode, nil
		}
		if _, ok := vmerr.ErrorCode(err); !ok {
			return nil, err
		}
		return state.Cells.BeginParseAlreadyLoaded(executionCodeRefWrapper(code))
	}

	if !code.IsSpecial() {
		currentCode, err := state.Cells.BeginParseAlreadyLoaded(code)
		if err == nil {
			return currentCode, nil
		}
		if _, ok := vmerr.ErrorCode(err); !ok {
			return nil, err
		}
	}

	return state.Cells.BeginParseAlreadyLoaded(executionCodeRefWrapper(code))
}

func executionCodeRefWrapper(code *cell.Cell) *cell.Cell {
	return cell.BeginCell().MustStoreRef(code).EndCell()
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
			if exitCode == vmerr.CodeOutOfGas && !vm.IsHandledException(err) {
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
	if stackErr := state.Stack.PushSmallInt(0); stackErr != nil {
		return 0, stackErr
	}
	return vmerr.CodeCellOverflow, vmerr.Error(vmerr.CodeCellOverflow, "cannot commit too deep cells as new data/actions")
}

func (tvm *TVM) execute(state *vm.State) error {
	dispatch := tvm.dispatchForVersion(state.GlobalVersion)
	for {
		if err := tvm.stepAnyWithDispatch(dispatch, state); err != nil {
			retry, err := tvm.handleStepError(state, err)
			if retry {
				continue
			}
			return err
		}
	}
}

func (tvm *TVM) handleStepError(state *vm.State, err error) (bool, error) {
	if errors.Is(err, vm.ErrStopOnAccept) {
		return false, nil
	}
	if vm.IsHandledException(err) {
		return false, err
	}
	e, isVMErr := vmerr.AsVMError(err)
	if isVMErr && e.Code == vmerr.CodeOutOfGas {
		state.Steps++
		if stackErr := state.HandleOutOfGas(); stackErr != nil {
			return false, stackErr
		}
		return false, err
	}
	if _, isVirt := vmerr.AsVirtualization(err); isVirt {
		return false, err
	}
	if state.Reg.C[2] != nil && isVMErr && !vm.IsSuccessExitCode(e.Code) {
		if state.TraceEnabled() {
			state.Tracef("[EXCEPTION] %d %s", e.Code, e.Msg)
		}

		state.Steps++
		if err = state.ThrowException(big.NewInt(e.Code)); err == nil {
			return true, nil
		}
		if e, isVMErr = vmerr.AsVMError(err); isVMErr && e.Code == vmerr.CodeOutOfGas {
			state.Steps++
			if stackErr := state.HandleOutOfGas(); stackErr != nil {
				return false, stackErr
			}
			return false, err
		}
	}

	return false, err
}

func (tvm *TVM) dispatchForVersion(version int) *opcodeDispatch {
	return tvm.dispatches[version]
}

func (tvm *TVM) stepAny(state *vm.State) error {
	return tvm.stepAnyWithDispatch(tvm.dispatchForVersion(state.GlobalVersion), state)
}

func (tvm *TVM) stepAnyWithDispatch(dispatch *opcodeDispatch, state *vm.State) error {
	if state.CurrentCode.BitsLeft() > 0 {
		state.Steps++
		return tvm.stepWithDispatch(dispatch, state)
	}

	if state.CurrentCode.RefsNum() > 0 {
		state.Steps++

		if err := state.Gas.Consume(vm.ImplicitJmprefGasPrice); err != nil {
			return err
		}

		cc, err := state.Cells.LoadRef(state.CurrentCode)
		if err != nil {
			return err
		}
		if state.GlobalVersion < 4 {
			// Pre-v4 gas checks are usually deferred, but the reference VM
			// checks after loading the implicit JMPREF target before executing it.
			if err := state.CheckGas(); err != nil {
				return err
			}
		}

		c := &vm.OrdinaryContinuation{
			Data: vm.ControlData{
				CP:      vm.CP,
				NumArgs: vm.ControlDataAllArgs,
			},
			Code: cc,
		}

		state.TraceOpcode("implicit JMPREF")
		return state.Jump(c)
	}

	state.Steps++
	state.TraceOpcode("implicit RET")
	if err := state.Gas.Consume(vm.ImplicitRetGasPrice); err != nil {
		return err
	}

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
	case cell.IsNotEnoughDataError(err),
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
		cell.IsNotEnoughDataError(err) {
		return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("deserialize opcode [%s] failed", op.SerializeText()))
	}
	return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeText(), err)
}

func (tvm *TVM) step(state *vm.State) (err error) {
	return tvm.stepWithDispatch(tvm.dispatchForVersion(state.GlobalVersion), state)
}

func (tvm *TVM) stepWithDispatch(dispatch *opcodeDispatch, state *vm.State) (err error) {
	px := matchOpcode(dispatch, state.CurrentCode)
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
	if state.GlobalVersion >= 4 {
		if err = state.CheckGas(); err != nil {
			return err
		}
	}
	if state.TraceEnabled() {
		state.TraceOpcode(op.SerializeText())
	}

	stackBefore := state.Stack.Checkpoint()
	err = op.Interpret(state)
	if err != nil {
		if code, ok := vmerr.ErrorCode(err); ok && code == vmerr.CodeStackUnderflow && !vm.IsHandledException(err) {
			state.Stack.RestoreCheckpoint(stackBefore)
		}
		err = normalizeCellError(err)
		return err
	}
	if err = state.CheckGas(); err != nil {
		return err
	}

	return nil
}

func consumeInstructionGas(state *vm.State, op vm.OP) error {
	gasOp, ok := op.(vm.GasPricedOp)
	if !ok {
		return nil
	}
	return state.ConsumeGas(vm.InstructionBaseGasPrice + gasOp.InstructionBits())
}
