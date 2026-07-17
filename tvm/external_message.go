package tvm

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const (
	DefaultExternalMessageGasMax     = int64(1_000_000)
	DefaultExternalMessageGasCredit  = int64(10_000)
	DefaultInternalMessageGasMax     = int64(1_000_000)
	DefaultTickTockTransactionGasMax = int64(1_000_000)
	InternalMessageGasAmountFactor   = int64(1_000)
)

var internalEmulationSrcAddr = address.MustParseRawAddr("-1:0000000000000000000000000000000000000000000000000000000000000000")

// MessageEmulationConfig drives the direct message-emulation entry points
// (EmulateExternalMessage/EmulateInternalMessage): code is executed with a
// caller-controlled synthetic c7 context. Config must be a prepared config;
// the remaining c7 fields override the derived values when set.
type MessageEmulationConfig struct {
	Address                     *address.Address
	Now                         uint32
	BlockLT                     int64
	LogicalTime                 int64
	Balance                     *big.Int
	RandSeed                    []byte
	Config                      *PreparedBlockchainConfig
	IncomingValue               tuple.Tuple
	StorageFees                 int64
	PrevBlocks                  any
	UnpackedConfig              tuple.Tuple
	DuePayment                  any
	PrecompiledGasUsage         *big.Int
	InMsgParams                 tuple.Tuple
	Globals                     map[int]any
	GlobalID                    int32
	Libraries                   []*cell.Cell
	Gas                         vm.Gas
	StopOnAccept                bool
	SignatureCheckAlwaysSucceed bool
	BuildProof                  bool
	AccountRoot                 *cell.Cell
	TraceHook                   vm.TraceHook
}

type EmulateExternalMessageConfig = MessageEmulationConfig
type EmulateInternalMessageConfig = MessageEmulationConfig

type MessageExecutionResult struct {
	ExecutionResult
	Accepted bool
}

func (tvm *TVM) EmulateExternalMessage(code, data *cell.Cell, msg *tlb.ExternalMessage, cfg EmulateExternalMessageConfig) (*MessageExecutionResult, error) {
	if cfg.Config == nil {
		return nil, errConfigRootRequired
	}

	addr := cfg.Address
	if addr == nil {
		addr = msg.DstAddr
	}

	proof, code, data, libraries, addr, err := prepareMessageExecutionProof(code, data, cfg, addr)
	if err != nil {
		return nil, err
	}

	normalized := *msg
	normalized.DstAddr = addr
	body := messageBodyCell(normalized.Body)
	normalized.Body = body

	msgCell, err := tlb.ToCell(&normalized)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := messageEmulationBalance(cfg.Balance)
	if err = stack.PushOwnedInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushSmallInt(0); err != nil {
		return nil, err
	}
	if err = stack.PushCell(msgCell); err != nil {
		return nil, err
	}
	bodySlice, err := body.BeginParse()
	if err != nil {
		return nil, err
	}
	if err = stack.PushOwnedSlice(bodySlice); err != nil {
		return nil, err
	}
	if err = stack.PushSmallInt(-1); err != nil {
		return nil, err
	}

	globalVersion, err := messageExecutionGlobalVersion(cfg)
	if err != nil {
		return nil, err
	}
	c7In, err := messageEmulationC7Input(addr, code, cfg, balance, uint32(globalVersion))
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7In, defaultExternalMessageGas(cfg.Gas), stack, cfg.StopOnAccept, cfg.SignatureCheckAlwaysSucceed, proof, cfg.TraceHook, cfg.Config, ptrTo(dryRunLibraryLoadLimit(cfg.Config)), libraries...)
}

func (tvm *TVM) EmulateInternalMessage(code, data, body *cell.Cell, amount uint64, cfg EmulateInternalMessageConfig) (*MessageExecutionResult, error) {
	if cfg.Config == nil {
		return nil, errConfigRootRequired
	}

	addr := cfg.Address
	proof, code, data, libraries, addr, err := prepareMessageExecutionProof(code, data, cfg, addr)
	if err != nil {
		return nil, err
	}

	body = messageBodyCell(body)
	msgCell, err := buildInternalMessageForEmulation(addr, body, amount)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := messageEmulationBalance(cfg.Balance)
	if err = stack.PushOwnedInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushOwnedInt(new(big.Int).SetUint64(amount)); err != nil {
		return nil, err
	}
	if err = stack.PushCell(msgCell); err != nil {
		return nil, err
	}
	bodySlice, err := body.BeginParse()
	if err != nil {
		return nil, err
	}
	if err = stack.PushOwnedSlice(bodySlice); err != nil {
		return nil, err
	}
	if err = stack.PushSmallInt(0); err != nil {
		return nil, err
	}

	globalVersion, err := messageExecutionGlobalVersion(cfg)
	if err != nil {
		return nil, err
	}
	c7In, err := messageEmulationC7Input(addr, code, cfg, balance, uint32(globalVersion))
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7In, defaultInternalMessageGas(cfg.Gas, amount), stack, cfg.StopOnAccept, cfg.SignatureCheckAlwaysSucceed, proof, cfg.TraceHook, cfg.Config, ptrTo(dryRunLibraryLoadLimit(cfg.Config)), libraries...)
}

func (tvm *TVM) executeMessageEmulation(code, data *cell.Cell, c7In emulationC7Input, gas vm.Gas, stack *vm.Stack, stopOnAccept bool, signatureCheckAlwaysSucceed bool, proof *cell.MerkleProofBuilder, traceHook vm.TraceHook, cfg *PreparedBlockchainConfig, libraryLoadLimit *uint32, libraries ...*cell.Cell) (*MessageExecutionResult, error) {
	// the state is created before c7 so the context tuple can be built
	// already bound to the state's gas trace: InitForExecution then skips the
	// per-execution deep rebuild of c7
	state := vm.NewExecutionState(int(cfg.GlobalVersion()), gas, data, tuple.Tuple{}, stack, libraries...)
	c7, err := buildEmulationC7(c7In, state.Cells.Trace())
	if err != nil {
		return nil, err
	}
	state.Reg.C7 = c7

	res, execErr := tvm.executeState(state, code, data, executeOptions{
		stopOnAccept:                stopOnAccept,
		proof:                       proof,
		traceHook:                   traceHook,
		signatureCheckAlwaysSucceed: signatureCheckAlwaysSucceed,
		maxVMDataDepth:              cfg.sizeLimits.maxVMDataDepth,
		libraryLoadLimit:            libraryLoadLimit,
	})
	if execErr != nil {
		if _, ok := vmerr.ErrorCode(execErr); !ok {
			return nil, execErr
		}
	}

	out := &MessageExecutionResult{
		ExecutionResult: *res,
		Accepted:        res.Gas.Credit == 0,
	}
	if !out.Accepted || !out.Committed {
		out.Code = code
		out.Data = data
		out.Actions = nil
	}
	return out, nil
}

func prepareMessageExecutionProof(code, data *cell.Cell, cfg MessageEmulationConfig, addr *address.Address) (*cell.MerkleProofBuilder, *cell.Cell, *cell.Cell, []*cell.Cell, *address.Address, error) {
	if !cfg.BuildProof {
		return nil, code, data, cfg.Libraries, addr, nil
	}
	if cfg.AccountRoot == nil {
		return nil, nil, nil, nil, nil, errors.New("account root is required to build execution proof")
	}

	proof, acc, err := prepareAccountExecutionProof(cfg.AccountRoot)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if !accountCanExecute(acc) {
		return nil, nil, nil, nil, nil, errors.New("account root is not an active executable account")
	}
	addr, err = executionProofAddress(addr, acc.Address)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if code != nil && !sameCellHash(code, acc.StateInit.Code) {
		return nil, nil, nil, nil, nil, errors.New("account root code differs from execution code")
	}
	if data != nil && !sameCellHash(data, acc.StateInit.Data) {
		return nil, nil, nil, nil, nil, errors.New("account root data differs from execution data")
	}
	libraries := cfg.Libraries
	if acc.StateInit.Lib != nil && acc.StateInit.Lib.AsCell() != nil {
		libraries = append(append([]*cell.Cell(nil), libraries...), acc.StateInit.Lib.AsCell())
	}
	return proof, acc.StateInit.Code, acc.StateInit.Data, libraries, addr, nil
}

func executionProofAddress(selected, account *address.Address) (*address.Address, error) {
	if account == nil {
		return nil, errors.New("account root address is missing")
	}
	if selected == nil {
		return account, nil
	}
	if selected.Type() != account.Type() || selected.BitsLen() != account.BitsLen() || !selected.Equals(account) {
		return nil, fmt.Errorf("account root address %s differs from execution address %s", account.String(), selected.String())
	}
	return selected, nil
}

func sameCellHash(a, b *cell.Cell) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.HashKey() == b.HashKey()
}

func messageBodyCell(body *cell.Cell) *cell.Cell {
	if body == nil {
		return cell.BeginCell().EndCell()
	}
	return body
}

func messageEmulationBalance(balance *big.Int) *big.Int {
	if balance == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(balance)
}

// emulationC7Input carries the resolved values of the c7 smart-contract
// context tuple; both the transaction executor and the direct message
// emulation entry points feed buildEmulationC7 through it.
type emulationC7Input struct {
	addr                *address.Address
	code                *cell.Cell
	now                 uint32
	blockLT             int64
	logicalTime         int64
	balance             *big.Int
	balanceExtra        *cell.Cell
	seed                *big.Int
	configRoot          *cell.Cell
	incomingValue       tuple.Tuple
	storageFees         *big.Int
	prevBlocks          any
	unpackedConfig      any
	duePayment          any
	precompiledGasUsage *big.Int
	inMsgParams         tuple.Tuple
	globals             map[int]any
	globalVersion       uint32
}

func messageEmulationC7Input(addr *address.Address, code *cell.Cell, cfg MessageEmulationConfig, balance *big.Int, globalVersion uint32) (emulationC7Input, error) {
	seed, err := messageEmulationSeed(cfg.RandSeed)
	if err != nil {
		return emulationC7Input{}, err
	}

	now := cfg.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}
	return emulationC7Input{
		addr:                addr,
		code:                code,
		now:                 now,
		blockLT:             cfg.BlockLT,
		logicalTime:         cfg.LogicalTime,
		balance:             balance,
		seed:                seed,
		configRoot:          cfg.Config.Root(),
		incomingValue:       cfg.IncomingValue,
		storageFees:         big.NewInt(cfg.StorageFees),
		prevBlocks:          cfg.PrevBlocks,
		unpackedConfig:      messageUnpackedConfig(cfg, now),
		duePayment:          cfg.DuePayment,
		precompiledGasUsage: cfg.PrecompiledGasUsage,
		inMsgParams:         cfg.InMsgParams,
		globals:             cfg.Globals,
		globalVersion:       globalVersion,
	}, nil
}

// buildEmulationC7 assembles the c7 tuple bound to the execution state's gas
// trace: internally-built slices and tuples are created carrying the trace,
// caller-supplied values are bound through vm.BindValueTrace, which also
// snapshots mutable cursor values (slices and builders track a read/write
// position). Binding at build time lets the state's c7 bind hit the
// binding-ID fast path instead of deep-rebuilding the tuple tree. A nil trace
// (tests) leaves values unbound; the VM then binds them on execution start.
func buildEmulationC7(in emulationC7Input, trace *cell.Trace) (tuple.Tuple, error) {
	globalVersion := in.globalVersion
	myAddr := cell.BeginCell().MustStoreAddr(in.addr).ToSlice().SetTrace(trace)
	values := make([]any, 18)
	idx := 0
	values[idx] = messageTupleUint(0x076ef1ea)
	idx++
	values[idx] = messageTupleInt(0)
	idx++
	values[idx] = messageTupleInt(0)
	idx++
	values[idx] = messageTupleUint(uint64(in.now))
	idx++
	values[idx] = messageTupleInt(in.blockLT)
	idx++
	values[idx] = messageTupleInt(in.logicalTime)
	idx++
	values[idx] = in.seed
	idx++
	var balanceExtra any
	if in.balanceExtra != nil {
		balanceExtra = in.balanceExtra
	}
	values[idx] = messageBoundTuple([]any{new(big.Int).Set(in.balance), balanceExtra}, trace)
	idx++
	values[idx] = myAddr
	idx++
	values[idx] = in.configRoot
	idx++
	if globalVersion >= 4 {
		values[idx] = in.code
		idx++
		values[idx] = messageIncomingValue(in.incomingValue, trace)
		idx++
		values[idx] = transactionBigOrZero(in.storageFees)
		idx++
		values[idx] = in.prevBlocks
		idx++
	}
	if globalVersion >= 6 {
		values[idx] = in.unpackedConfig
		idx++
		values[idx] = in.duePayment
		idx++
		values[idx] = messageTupleMaybeInt(in.precompiledGasUsage)
		idx++
	}
	if globalVersion >= 11 {
		values[idx] = messageInMsgParams(in.inMsgParams, trace)
		idx++
	}
	values = values[:idx]

	for i, val := range values {
		values[i] = vm.BindValueTrace(val, trace)
	}

	inner := messageBoundTuple(values, trace)
	topLen := 1
	for idx := range in.globals {
		if idx <= 0 {
			return tuple.Tuple{}, errors.New("c7 global index 0 is reserved")
		}
		if idx+1 > topLen {
			topLen = idx + 1
		}
	}

	top := make([]any, topLen)
	top[0] = inner
	for idx, val := range in.globals {
		top[idx] = vm.BindValueTrace(val, trace)
	}
	return messageBoundTuple(top, trace), nil
}

// messageBoundTuple builds an owned tuple already carrying the trace binding
// ID; every value must be bound to the trace by the caller.
func messageBoundTuple(vals []any, trace *cell.Trace) tuple.Tuple {
	if trace == nil {
		return tuple.NewTupleOwned(vals)
	}
	return tuple.NewTupleOwnedBound(vals, trace)
}

func messageExecutionGlobalVersion(cfg MessageEmulationConfig) (int, error) {
	if cfg.Config == nil {
		return 0, errConfigRootRequired
	}
	return int(cfg.Config.GlobalVersion()), nil
}

func buildInternalMessageForEmulation(addr *address.Address, body *cell.Cell, amount uint64) (*cell.Cell, error) {
	msg := &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		Bounced:     false,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(amount),
		IHRFee:      tlb.MustFromNano(big.NewInt(0), 9),
		FwdFee:      tlb.MustFromNano(big.NewInt(0), 9),
		CreatedLT:   0,
		CreatedAt:   0,
		Body:        body,
	}
	return tlb.ToCell(msg)
}

func defaultExternalMessageGas(gas vm.Gas) vm.Gas {
	if gas != (vm.Gas{}) {
		return gas
	}
	// The reference profile is GasLimits(0, max, credit): a literal zero limit
	// with the credit as the whole starting budget, so an exact struct is built
	// here instead of going through the zero-means-unset NewGas constructor.
	return vm.Gas{
		Max:       DefaultExternalMessageGasMax,
		Limit:     0,
		Credit:    DefaultExternalMessageGasCredit,
		Base:      DefaultExternalMessageGasCredit,
		Remaining: DefaultExternalMessageGasCredit,
	}
}

func defaultInternalMessageGas(gas vm.Gas, amount uint64) vm.Gas {
	if gas != (vm.Gas{}) {
		return gas
	}

	limit := int64(amount) * InternalMessageGasAmountFactor
	return vm.Gas{
		Max:       DefaultInternalMessageGasMax,
		Limit:     limit,
		Base:      limit,
		Remaining: limit,
	}
}

func defaultTickTockTransactionGas(gas vm.Gas) vm.Gas {
	if gas != (vm.Gas{}) {
		return gas
	}
	return vm.NewGas(vm.GasConfig{
		Max:   DefaultTickTockTransactionGasMax,
		Limit: DefaultTickTockTransactionGasMax,
	})
}

func messageEmulationSeed(seed []byte) (*big.Int, error) {
	if len(seed) == 0 {
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(seed), nil
}

func messageEmulationAccountAddr(addr *address.Address) (*big.Int, error) {
	if addr == nil {
		return nil, errors.New("address is nil")
	}
	if addr.Type() != address.StdAddress || addr.BitsLen() != 256 {
		return nil, errors.New("tick/tock emulation requires std 256-bit address")
	}
	return new(big.Int).SetBytes(addr.Data()), nil
}

func messageIncomingValue(value tuple.Tuple, trace *cell.Trace) tuple.Tuple {
	if value.Len() == 0 {
		return messageBoundTuple([]any{big.NewInt(0), nil}, trace)
	}
	return value
}

func messageUnpackedConfig(cfg MessageEmulationConfig, now uint32) any {
	if cfg.UnpackedConfig.Len() > 0 {
		return cfg.UnpackedConfig
	}
	return buildUnpackedConfig(cfg.Config, now, cfg.GlobalID)
}

func messageInMsgParams(params tuple.Tuple, trace *cell.Trace) tuple.Tuple {
	if params.Len() > 0 {
		return params
	}
	return messageBoundTuple([]any{
		messageTupleInt(0),
		messageTupleInt(0),
		cell.BeginCell().MustStoreUInt(0, 2).ToSlice().SetTrace(trace),
		messageTupleInt(0),
		messageTupleInt(0),
		messageTupleInt(0),
		messageTupleInt(0),
		messageTupleInt(0),
		nil,
		nil,
	}, trace)
}

func messageTupleInt(v int64) *big.Int {
	return big.NewInt(v)
}

func messageTupleUint(v uint64) *big.Int {
	return new(big.Int).SetUint64(v)
}

func messageTupleMaybeInt(v *big.Int) any {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
