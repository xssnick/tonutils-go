package tvm

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"runtime/debug"
	"sync"

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
)

type trieNode struct {
	next [2]*trieNode
	op   *dispatchEntry
}

// dispatchEntry is what a matched prefix resolves to. Which of the two shapes
// an opcode has is decided once, when the table is built, so the step loop
// picks between them on a nil check instead of a type assertion per
// instruction.
type dispatchEntry struct {
	// arg is set for operand-carrying opcodes: a single shared instance that
	// executes without allocating.
	arg vm.ArgOP
	// get builds an instance per instruction, for opcodes that still keep
	// their decoded operand inside themselves.
	get vm.OPGetter
}

const opcodeDispatchIndexBits = 16

type opcodeDispatch struct {
	root         *trieNode
	maxPrefixLen uint
	index        [1 << opcodeDispatchIndexBits]opcodeDispatchPrefix
}

type opcodeDispatchPrefix struct {
	node    *trieNode
	matched *dispatchEntry
}

type matchedDeserializer interface {
	DeserializeMatched(code *cell.Slice) error
}

type TVM struct {
	dispatches [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch
}

var (
	sharedOpcodeDispatchesOnce sync.Once
	sharedOpcodeDispatches     [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch
	frozenOpcodeRegistry       []vm.OPGetter
	frozenArgRegistry          []vm.ArgOP
)

func init() {
	// All opcode packages have completed their init functions before tvm is
	// initialized. Keep that registry snapshot immutable, like the finalized
	// cp0 table in the reference VM.
	frozenOpcodeRegistry = append([]vm.OPGetter(nil), vm.List...)
	frozenArgRegistry = append([]vm.ArgOP(nil), vm.ArgList...)
}

func NewTVM() *TVM {
	return newTVM(
		vm.List,
		frozenOpcodeRegistry,
	)
}

func getSharedOpcodeDispatches() [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch {
	sharedOpcodeDispatchesOnce.Do(buildSharedOpcodeDispatches)
	return sharedOpcodeDispatches
}

func buildSharedOpcodeDispatches() {
	sharedOpcodeDispatches = buildOpcodeDispatches(frozenOpcodeRegistry, frozenArgRegistry)
}

func buildOpcodeDispatches(registry []vm.OPGetter, argRegistry []vm.ArgOP) [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch {
	var dispatches [vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch
	for ver := 0; ver <= vm.MaxSupportedGlobalVersion; ver++ {
		dispatches[ver] = newOpcodeDispatch()
	}

	for _, opGetter := range registry {
		op := opGetter()

		// Implementing ArgOP is what makes an opcode shareable: it keeps no
		// decoded operand of its own, so one instance serves every execution
		// and a step allocates nothing. Everything else still gets a fresh
		// instance per instruction.
		entry := &dispatchEntry{get: opGetter}
		invalid := func() *dispatchEntry {
			return &dispatchEntry{get: historicalInvalidOpcodeGetter(op)}
		}
		if arg, ok := op.(vm.ArgOP); ok {
			entry = &dispatchEntry{arg: arg}
			invalid = func() *dispatchEntry {
				return &dispatchEntry{arg: historicalInvalidArgOP{ArgOP: arg}}
			}
		}

		registerOpcodePrefixes(&dispatches, op.GetPrefixes(), opcodeMinVersion(op), entry, invalid)
	}

	for _, argOp := range argRegistry {
		entry := &dispatchEntry{arg: argOp}
		minVersion := 0
		if versioned, ok := argOp.(vm.VersionedOp); ok {
			minVersion = versioned.MinGlobalVersion()
		}
		registerOpcodePrefixes(&dispatches, argOp.GetPrefixes(), minVersion, entry, func() *dispatchEntry {
			return &dispatchEntry{arg: historicalInvalidArgOP{ArgOP: argOp}}
		})
	}

	for ver := 0; ver <= vm.MaxSupportedGlobalVersion; ver++ {
		dispatches[ver].buildFastTable()
	}
	return dispatches
}

// registerOpcodePrefixes installs an opcode for every global version it exists
// in. Below its minimum version a handful of quiet compound prefixes are not
// simply absent: they were dispatched and charged for, then rejected, so they
// keep an entry that fails the same way rather than falling through to the
// generic unknown-opcode path.
func registerOpcodePrefixes(
	dispatches *[vm.MaxSupportedGlobalVersion + 1]*opcodeDispatch,
	prefixes []*cell.Slice,
	minVersion int,
	entry *dispatchEntry,
	invalid func() *dispatchEntry,
) {
	if minVersion < 0 {
		minVersion = 0
	}
	var invalidEntry *dispatchEntry
	for _, s := range prefixes {
		if minVersion > 0 && opcodeIsHistoricalQuietCompoundPrefix(s) {
			if invalidEntry == nil {
				invalidEntry = invalid()
			}
			for ver := 0; ver < minVersion && ver <= vm.MaxSupportedGlobalVersion; ver++ {
				dispatches[ver].addPrefix(s, invalidEntry)
			}
		}
		for ver := minVersion; ver <= vm.MaxSupportedGlobalVersion; ver++ {
			dispatches[ver].addPrefix(s, entry)
		}
	}
}

// historicalInvalidArgOP decodes and charges exactly as the real opcode does,
// then rejects it — the behaviour of a global version that did not have it yet.
type historicalInvalidArgOP struct {
	vm.ArgOP
}

func (op historicalInvalidArgOP) InterpretArgs(*vm.State, uint64) error {
	return vmerr.Error(vmerr.CodeInvalidOpcode)
}

// decodeInstruction decodes one instruction from code and returns its text
// form. It is how anything that reads the table without executing — a
// disassembler, the dispatch tests — gets at an entry regardless of its shape.
func (e *dispatchEntry) decodeInstruction(state *vm.State, code *cell.Slice) (string, error) {
	_, text, err := e.decodeAndEncode(state, code, false)
	return text, err
}

// decodeAndEncode additionally re-encodes the decoded instruction, for the
// round-trip checks. Encoding is opt-in because some opcodes only serialize
// once their operand has been filled in for real.
func (e *dispatchEntry) decodeAndEncode(state *vm.State, code *cell.Slice, encode bool) (*cell.Builder, string, error) {
	if e.arg != nil {
		args, err := e.arg.DecodeArgs(state, code)
		if err != nil {
			return nil, "", err
		}
		var encoded *cell.Builder
		if encode {
			encoded = e.arg.SerializeArgs(args)
		}
		return encoded, e.arg.SerializeArgsText(args), nil
	}

	op := e.get()
	if fast, ok := op.(matchedDeserializer); ok {
		if err := fast.DeserializeMatched(code); err != nil {
			return nil, "", err
		}
	} else if err := op.Deserialize(code); err != nil {
		return nil, "", err
	}
	var encoded *cell.Builder
	if encode {
		encoded = op.Serialize()
	}
	return encoded, op.SerializeText(), nil
}

// instance returns the underlying opcode, shared or freshly built, for callers
// that only want to inspect it. The two shapes have no common interface — an
// ArgOP deliberately has no Deserialize/Interpret pair — and every caller only
// looks at the dynamic type anyway, so it stays untyped rather than binding the
// shared instance to a dummy operand.
func (e *dispatchEntry) instance() any {
	if e.arg != nil {
		return e.arg
	}
	return e.get()
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

func validateGlobalVersion(version int) error {
	if version < 0 {
		return fmt.Errorf("unsupported global version %d, minimum supported is %d", version, 0)
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
	Steps     uint64
	Gas       vm.Gas
	Stack     *vm.Stack
	Code      *cell.Cell
	Data      *cell.Cell
	Actions   *cell.Cell
	Committed bool
	// MissingLibrary is the hash from the last library lookup that searched
	// every registered collection without finding a matching cell.
	MissingLibrary *cell.Hash
	Proof          *cell.Cell
	loadedCells    vm.LoadedCells
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

func (dispatch *opcodeDispatch) addPrefix(prefix *cell.Slice, op *dispatchEntry) {
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
		var matched *dispatchEntry

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

func (tvm *TVM) matchOpcode(code *cell.Slice) *dispatchEntry {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	return matchOpcode(dispatch, code)
}

func (tvm *TVM) matchOpcodeFast(code *cell.Slice, available uint) *dispatchEntry {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	return matchOpcodeFast(dispatch, code, available)
}

func (tvm *TVM) matchOpcodeSlow(code *cell.Slice, available uint) *dispatchEntry {
	dispatch := tvm.dispatches[vm.MaxSupportedGlobalVersion]
	return matchOpcodeSlow(dispatch.root, dispatch.maxPrefixLen, code, available)
}

func matchOpcode(dispatch *opcodeDispatch, code *cell.Slice) *dispatchEntry {
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

func matchOpcodeFast(dispatch *opcodeDispatch, code *cell.Slice, available uint) *dispatchEntry {
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

func matchOpcodeSlow(root *trieNode, maxPrefixLen uint, code *cell.Slice, available uint) *dispatchEntry {
	n := root
	var matched *dispatchEntry

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

// ExecuteGetMethod runs a get method. As in the reference get-method flow,
// the final automatic commit still happens: a successful run leaving a
// too-deep or non-zero-level c4/c5 ends with an inverted cell-overflow exit
// and a cleared stack. That flow also registers the library collection only
// after the machine has converted the code, so a library root code is not
// resolved at startup — it gets wrapped and resolves during execution.
func (tvm *TVM) ExecuteGetMethod(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, cfg ExecutionConfig) (*ExecutionResult, error) {
	options := executeOptionsFromConfig(cfg)
	options.codeConversionWithoutLibraries = true
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

	if code == nil || stack == nil {
		// The reference VM refuses to run with null code or stack as a fatal
		// condition (exit ~fatal) before the normal exception path.
		return &ExecutionResult{
			ExitCode: ^int64(vmerr.CodeFatal),
			Gas:      gas,
			Stack:    stack,
			Code:     code,
			Data:     data,
		}, nil
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
	traceHook                   vm.TraceHook
	signatureCheckAlwaysSucceed bool
	maxVMDataDepth              uint16
	libraryLoadLimit            *uint32
	// codeConversionWithoutLibraries reproduces the get-method flow, where the
	// library collection is registered only after the machine is constructed,
	// so startup code conversion cannot resolve library cells. Transaction
	// flows have the libraries up front and resolve them for free.
	codeConversionWithoutLibraries bool
	// onCellLoad observes the first load of every cell, for callers that must
	// record what the machine read regardless of how the cell reached it.
	onCellLoad func(*cell.Cell)
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
	initialData := state.Reg.D[0]
	initialActions := state.Reg.D[1]

	state.StopOnAccept = options.stopOnAccept
	state.OnCellLoad = options.onCellLoad
	state.TraceHook = options.traceHook
	state.SignatureCheckAlwaysSucceed = options.signatureCheckAlwaysSucceed
	state.SetChildRunner(tvm.runState)
	state.SetMaxDataDepth(options.maxVMDataDepth)
	if options.libraryLoadLimit != nil {
		state.SetMaxLibraryLoads(*options.libraryLoadLimit)
	}
	state.InitForExecution()
	defer state.Cells.FinishExecution()
	currentCode, err := tvm.convertExecutionCodeCell(state, code, !options.codeConversionWithoutLibraries)
	if err != nil {
		exitCode := vmerrCode(err)
		if _, ok := vmerr.AsVirtualization(err); ok {
			// Since v9 a virtualized pruned code root aborts in the C++
			// constructor, before run() starts. Keep the public Execute contract
			// (VM failures are returned as a result) while encoding that external
			// abort exactly like the reference boundary does.
			exitCode = ^exitCode
		}
		res := executionResultFromState(exitCode, state, code, initialData, initialActions)
		if proofErr := attachExecutionProof(res, state, options.proof); proofErr != nil {
			return res, proofErr
		}
		return res, err
	}
	state.CurrentCode = currentCode
	state.Reg.C[3] = &vm.OrdinaryContinuation{
		Data: vm.ControlData{
			CP:      state.CP,
			NumArgs: vm.ControlDataAllArgs,
		},
		Code: currentCode.Copy(),
	}

	exitCode, err := tvm.runState(state)

	dataRes := initialData
	actionsRes := initialActions
	if state.Committed.Committed {
		dataRes = state.Committed.Data
		actionsRes = state.Committed.Actions
	}

	res := &ExecutionResult{
		ExitCode:       exitCode,
		GasUsed:        state.Gas.Used(),
		Steps:          state.Steps,
		Gas:            state.Gas,
		Stack:          state.Stack,
		Code:           code,
		Data:           dataRes,
		Actions:        actionsRes,
		Committed:      state.Committed.Committed,
		MissingLibrary: state.MissingLibrary(),
		loadedCells:    state.Cells.LoadedCells(),
	}
	if proofErr := attachExecutionProof(res, state, options.proof); proofErr != nil {
		return res, proofErr
	}
	return res, err
}

func (tvm *TVM) convertExecutionCodeCell(state *vm.State, code *cell.Cell, resolveLibraries bool) (*cell.Slice, error) {
	if code == nil {
		return state.Cells.BeginParseAlreadyLoaded(code)
	}

	if resolveLibraries && state.GlobalVersion >= 9 {
		// Transaction flows have the library collection up front, so the root
		// is resolved outside gas accounting: neither gas nor a library-load
		// slot is consumed.
		restoreLookupState := state.SuspendLibraryLoadAccounting()
		currentCode, err := state.Cells.BeginParseAlreadyLoaded(code)
		restoreLookupState()
		if err == nil {
			return currentCode, nil
		}
		// The reference catches VmError here, but VmVirtError must escape the
		// constructor before the first VM step.
		if _, ok := vmerr.AsVMError(err); !ok {
			return nil, err
		}
		return state.Cells.BeginParseAlreadyLoaded(executionCodeRefWrapper(code))
	}

	// Below v9 the code slice is built outside gas accounting, and the
	// get-method flow converts before its library collection is registered:
	// specials are not resolved and nothing is charged. A lazy placeholder
	// always looks special, so materialize it for free first and inspect the
	// real cell type.
	currentCode, special, err := state.Cells.BeginParseSpecialAlreadyLoaded(code)
	if err != nil {
		if state.GlobalVersion >= 9 {
			if _, ok := vmerr.AsVMError(err); !ok {
				return nil, err
			}
		} else {
			if _, ok := vmerr.ErrorCode(err); !ok {
				return nil, err
			}
		}
	} else if !special {
		return currentCode, nil
	}

	return state.Cells.BeginParseAlreadyLoaded(executionCodeRefWrapper(code))
}

func executionCodeRefWrapper(code *cell.Cell) *cell.Cell {
	return cell.BeginCell().MustStoreRef(code).EndCell()
}

func executionResultFromState(exitCode int64, state *vm.State, code, data, actions *cell.Cell) *ExecutionResult {
	dataRes := data
	actionsRes := actions
	if state.Committed.Committed {
		dataRes = state.Committed.Data
		actionsRes = state.Committed.Actions
	}

	return &ExecutionResult{
		ExitCode:       exitCode,
		GasUsed:        state.Gas.Used(),
		Steps:          state.Steps,
		Gas:            state.Gas,
		Stack:          state.Stack,
		Code:           code,
		Data:           dataRes,
		Actions:        actionsRes,
		Committed:      state.Committed.Committed,
		MissingLibrary: state.MissingLibrary(),
		loadedCells:    state.Cells.LoadedCells(),
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
			if exitCode == vmerr.CodeVirtualization {
				_, isVirt := vmerr.AsVirtualization(err)
				if isVirt {
					// Like unhandled out-of-gas, a virtualization abort bypasses
					// c2 and is reported inverted (-15).
					exitCode = ^exitCode
				}
			}
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
	if stackErr := state.Stack.PushSmallInt(0); stackErr != nil {
		return 0, stackErr
	}
	return vmerr.CodeCellOverflow, vmerr.Error(vmerr.CodeCellOverflow, "cannot commit too deep cells as new data/actions")
}

func (tvm *TVM) execute(state *vm.State) error {
	dispatch := tvm.dispatchForVersion(state.GlobalVersion)
	uncheckedGas := state.GlobalVersion < 4
	for {
		err := tvm.stepAnyWithDispatch(dispatch, state)
		if uncheckedGas && (err == nil || vm.IsHandledException(err)) {
			// Pre-v4 consumption is unchecked, so the gas check runs after
			// every step that returned normally — including the terminating
			// one — and an overdraft overrides its result. Thrown exceptions
			// are exempt: their checked exception charge covers them.
			if gasErr := state.CheckGas(); gasErr != nil {
				err = gasErr
			}
		}
		if err != nil {
			retry, err := tvm.handleStepError(state, err)
			if retry {
				continue
			}
			return err
		}
	}
}

func (tvm *TVM) handleStepError(state *vm.State, err error) (bool, error) {
	if vm.IsHandledException(err) {
		return false, err
	}
	if errors.Is(err, vm.ErrStopOnAccept) {
		return false, nil
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
	if !vm.IsNullContinuation(state.Reg.C[2]) && isVMErr && !vm.IsSuccessExitCode(e.Code) {
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

		var cc cell.Slice
		if err := state.Cells.LoadRefInto(state.CurrentCode, &cc); err != nil {
			return err
		}

		state.TraceOpcode("implicit JMPREF")
		return state.JumpToCode(&cc, state.CP)
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
	entry := matchOpcode(dispatch, state.CurrentCode)
	if entry == nil {
		if err = state.ConsumeGas(vm.InstructionBaseGasPrice); err != nil {
			return err
		}
		return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("opcode not found: %s", state.CurrentCode.String()))
	}

	if entry.arg != nil {
		return executeArgOpcode(state, entry.arg)
	}

	op := entry.get()

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

	return executeDecodedOpcode(state, op)
}

// executeArgOpcode runs an opcode that keeps no state of its own. It mirrors
// the legacy decode-charge-interpret order exactly, including charging for a
// failed decode: the reference VM has already picked and paid for a table entry
// by the time it discovers the instruction is truncated.
func executeArgOpcode(state *vm.State, op vm.ArgOP) error {
	args, err := op.DecodeArgs(state, state.CurrentCode)
	if err != nil {
		if gasErr := state.ConsumeGas(vm.InstructionBaseGasPrice + op.ArgInstructionBits(args)); gasErr != nil {
			return gasErr
		}
		return normalizeArgOpcodeDeserializeError(err, op, args)
	}

	if err = state.ConsumeGas(vm.InstructionBaseGasPrice + op.ArgInstructionBits(args)); err != nil {
		return err
	}

	return interpretOpcode(state,
		func() error { return op.InterpretArgs(state, args) },
		func() string { return op.SerializeArgsText(args) },
	)
}

func normalizeArgOpcodeDeserializeError(err error, op vm.ArgOP, args uint64) error {
	if errors.Is(err, vm.ErrCorruptedOpcode) ||
		errors.Is(err, cell.ErrNoMoreRefs) ||
		errors.Is(err, cell.ErrSmallSlice) ||
		cell.IsNotEnoughDataError(err) {
		return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("deserialize opcode [%s] failed", op.SerializeArgsText(args)))
	}
	return fmt.Errorf("deserialize opcode [%s] error: %w", op.SerializeArgsText(args), err)
}

func executeDecodedOpcode(state *vm.State, op vm.OP) error {
	if err := consumeInstructionGas(state, op); err != nil {
		return err
	}

	return interpretOpcode(state,
		func() error { return op.Interpret(state) },
		func() string { return op.SerializeText() },
	)
}

// interpretOpcode runs the body of an instruction whose own gas the caller has
// already charged; from there on both opcode shapes behave identically. The two
// closures are only called, never stored, so they stay on the caller's stack and
// a step still allocates nothing.
func interpretOpcode(state *vm.State, run func() error, trace func() string) error {
	// From global version 4 on an exhausted limit aborts before the instruction
	// body rather than after it.
	if state.GlobalVersion >= 4 {
		if err := state.CheckGas(); err != nil {
			return err
		}
	}
	if state.TraceEnabled() {
		state.TraceOpcode(trace())
	}

	if err := run(); err != nil {
		// Cell load/create gas is charged by a trace callback. If it exhausted
		// gas while the opcode later found a semantic error, the synchronous
		// out-of-gas abort from the reference VM takes precedence.
		if gasErr := state.Cells.PendingError(); gasErr != nil {
			return gasErr
		}
		return normalizeCellError(err)
	}

	return state.CheckGas()
}

func consumeInstructionGas(state *vm.State, op vm.OP) error {
	gasOp, ok := op.(vm.GasPricedOp)
	if !ok {
		return nil
	}
	return state.ConsumeGas(vm.InstructionBaseGasPrice + gasOp.InstructionBits())
}

func newTVM(registry, frozenRegistry []vm.OPGetter) *TVM {
	dispatches := getSharedOpcodeDispatches()
	if len(registry) != len(frozenRegistry) || len(vm.ArgList) != len(frozenArgRegistry) {
		// Keep supporting opcode packages registered after tvm initialization:
		// their dispatch is built privately from the extended registry.
		dispatches = buildOpcodeDispatches(
			append([]vm.OPGetter(nil), registry...),
			append([]vm.ArgOP(nil), vm.ArgList...),
		)
	}

	return &TVM{dispatches: dispatches}
}
