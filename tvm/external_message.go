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

type MessageEmulationConfig struct {
	Address             *address.Address
	Now                 uint32
	BlockLT             int64
	LogicalTime         int64
	Balance             *big.Int
	RandSeed            []byte
	ConfigRoot          *cell.Cell
	IncomingValue       tuple.Tuple
	StorageFees         int64
	PrevBlocks          any
	UnpackedConfig      tuple.Tuple
	DuePayment          any
	PrecompiledGasUsage *big.Int
	InMsgParams         tuple.Tuple
	Globals             map[int]any
	GlobalID            int32
	Libraries           []*cell.Cell
	Gas                 vm.Gas
	StopOnAccept        bool
	ChksigAlwaysSucceed bool
	BuildProof          bool
	AccountRoot         *cell.Cell
	AccountStorageStat  *cell.Cell
	TraceHook           vm.TraceHook
}

type EmulateExternalMessageConfig = MessageEmulationConfig
type EmulateInternalMessageConfig = MessageEmulationConfig

type MessageExecutionResult struct {
	ExecutionResult
	Accepted bool
}

func (tvm *TVM) EmulateExternalMessage(code, data *cell.Cell, msg *tlb.ExternalMessage, cfg EmulateExternalMessageConfig) (*MessageExecutionResult, error) {
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
	c7, err := buildMessageEmulationC7(addr, code, cfg, balance, uint32(globalVersion))
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7, defaultExternalMessageGas(cfg.Gas), stack, cfg.StopOnAccept, cfg.ChksigAlwaysSucceed, proof, cfg.TraceHook, globalVersion, libraries...)
}

func (tvm *TVM) EmulateInternalMessage(code, data, body *cell.Cell, amount uint64, cfg EmulateInternalMessageConfig) (*MessageExecutionResult, error) {
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
	c7, err := buildMessageEmulationC7(addr, code, cfg, balance, uint32(globalVersion))
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7, defaultInternalMessageGas(cfg.Gas, amount), stack, cfg.StopOnAccept, cfg.ChksigAlwaysSucceed, proof, cfg.TraceHook, globalVersion, libraries...)
}

func (tvm *TVM) executeMessageEmulation(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, stopOnAccept bool, chksigAlwaysSucceed bool, proof *cell.MerkleProofBuilder, traceHook vm.TraceHook, globalVersion int, libraries ...*cell.Cell) (*MessageExecutionResult, error) {
	res, execErr := tvm.executeWithOptions(code, data, c7, gas, stack, executeOptions{
		stopOnAccept:        stopOnAccept,
		proof:               proof,
		traceHook:           traceHook,
		globalVersion:       globalVersion,
		globalVersionSet:    true,
		chksigAlwaysSucceed: chksigAlwaysSucceed,
	}, libraries...)
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

func buildMessageEmulationC7(addr *address.Address, code *cell.Cell, cfg MessageEmulationConfig, balance *big.Int, globalVersion uint32) (tuple.Tuple, error) {
	seed, err := messageEmulationSeed(cfg.RandSeed)
	if err != nil {
		return tuple.Tuple{}, err
	}
	return buildEmulationC7(addr, code, cfg, balance, seed, globalVersion)
}

func buildEmulationC7(addr *address.Address, code *cell.Cell, cfg MessageEmulationConfig, balance, seed *big.Int, globalVersion uint32) (tuple.Tuple, error) {
	now := cfg.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}

	myAddr := cell.BeginCell().MustStoreAddr(addr).ToSlice()
	values := make([]any, 18)
	idx := 0
	values[idx] = messageTupleUint(0x076ef1ea)
	idx++
	values[idx] = messageTupleInt(0)
	idx++
	values[idx] = messageTupleInt(0)
	idx++
	values[idx] = messageTupleUint(uint64(now))
	idx++
	values[idx] = messageTupleInt(cfg.BlockLT)
	idx++
	values[idx] = messageTupleInt(cfg.LogicalTime)
	idx++
	values[idx] = seed
	idx++
	values[idx] = tuple.NewTupleValue(new(big.Int).Set(balance), nil)
	idx++
	values[idx] = myAddr
	idx++
	values[idx] = cfg.ConfigRoot
	idx++
	if globalVersion >= 4 {
		values[idx] = code
		idx++
		values[idx] = messageIncomingValue(cfg.IncomingValue)
		idx++
		values[idx] = messageTupleInt(cfg.StorageFees)
		idx++
		values[idx] = cfg.PrevBlocks
		idx++
	}
	if globalVersion >= 6 {
		values[idx] = messageUnpackedConfig(cfg, now)
		idx++
		values[idx] = cfg.DuePayment
		idx++
		values[idx] = messageTupleMaybeInt(cfg.PrecompiledGasUsage)
		idx++
	}
	if globalVersion >= 11 {
		values[idx] = messageInMsgParams(cfg.InMsgParams)
		idx++
	}
	values = values[:idx]

	for i, val := range values {
		values[i] = normalizeMessageTupleValue(val)
	}

	inner := tuple.NewTupleOwned(values)
	topLen := 1
	for idx := range cfg.Globals {
		if idx <= 0 {
			return tuple.Tuple{}, errors.New("c7 global index 0 is reserved")
		}
		if idx+1 > topLen {
			topLen = idx + 1
		}
	}

	top := make([]any, topLen)
	top[0] = inner
	for idx, val := range cfg.Globals {
		top[idx] = normalizeMessageTupleValue(val)
	}
	return tuple.NewTupleOwned(top), nil
}

func messageExecutionGlobalVersion(cfg MessageEmulationConfig) (int, error) {
	if cfg.ConfigRoot == nil {
		return 0, errConfigRootRequired
	}

	globalVersion, err := (tlb.BlockchainConfig{Root: cfg.ConfigRoot}).GetGlobalVersion()
	if err != nil {
		return 0, err
	}
	version := int(globalVersion.Version)
	if err := validateGlobalVersion(version); err != nil {
		return 0, err
	}
	return version, nil
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
	return vm.NewGas(vm.GasConfig{
		Max:    DefaultExternalMessageGasMax,
		Limit:  0,
		Credit: DefaultExternalMessageGasCredit,
	})
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

func messageIncomingValue(value tuple.Tuple) tuple.Tuple {
	if value.Len() == 0 {
		return tuple.NewTupleValue(big.NewInt(0), nil)
	}
	return value
}

func messageUnpackedConfig(cfg MessageEmulationConfig, now uint32) any {
	if cfg.UnpackedConfig.Len() > 0 {
		return cfg.UnpackedConfig
	}

	values := make([]any, 7)
	if cfg.ConfigRoot != nil {
		config := tlb.BlockchainConfig{Root: cfg.ConfigRoot}
		values[0] = messageCurrentStoragePricesSlice(config, now)
		values[1] = messageConfigParamSlice(config, tlb.ConfigParamGlobalID)
		values[2] = messageConfigParamSlice(config, tlb.ConfigParamGasPricesMasterchain)
		values[3] = messageConfigParamSlice(config, tlb.ConfigParamGasPricesBasechain)
		values[4] = messageConfigParamSlice(config, tlb.ConfigParamMsgForwardPricesMasterchain)
		values[5] = messageConfigParamSlice(config, tlb.ConfigParamMsgForwardPricesBasechain)
		values[6] = messageConfigParamSlice(config, tlb.ConfigParamSizeLimits)
	}

	if cfg.GlobalID != 0 {
		values[1] = cell.BeginCell().MustStoreUInt(uint64(uint32(cfg.GlobalID)), 32).ToSlice()
	}
	if !messageUnpackedConfigHasValues(values) {
		return nil
	}

	return tuple.NewTupleOwned(values)
}

func messageConfigParamSlice(config tlb.BlockchainConfig, id uint32) *cell.Slice {
	cl, err := config.GetParam(id)
	if err != nil {
		return nil
	}
	sl, err := cl.BeginParse()
	if err != nil {
		return nil
	}
	return sl
}

func messageCurrentStoragePricesSlice(config tlb.BlockchainConfig, now uint32) *cell.Slice {
	prices, err := config.GetStoragePrices(now)
	if err != nil {
		return nil
	}
	cl, err := tlb.ToCell(prices)
	if err != nil {
		return nil
	}
	sl, err := cl.BeginParse()
	if err != nil {
		return nil
	}
	return sl
}

func messageUnpackedConfigHasValues(values []any) bool {
	for _, value := range values {
		if value != nil {
			return true
		}
	}
	return false
}

func messageInMsgParams(params tuple.Tuple) tuple.Tuple {
	if params.Len() > 0 {
		return params
	}
	return tuple.NewTupleValue(
		messageTupleInt(0),
		messageTupleInt(0),
		cell.BeginCell().MustStoreUInt(0, 2).ToSlice(),
		messageTupleInt(0),
		messageTupleInt(0),
		messageTupleInt(0),
		messageTupleInt(0),
		messageTupleInt(0),
		nil,
		nil,
	)
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

// normalizeMessageTupleValue snapshots mutable cursor values (slices and
// builders track a read/write position). Tuples and integers are passed
// through as-is: the VM never mutates them in place, and binding the tuple to
// the VM gas trace on execution copies every nested slice anyway, so a deep
// defensive clone here would only duplicate that work for each transaction.
func normalizeMessageTupleValue(val any) any {
	switch v := val.(type) {
	case *big.Int:
		if v == nil {
			return nil
		}
		return v
	case *cell.Slice:
		if v == nil {
			return nil
		}
		return v.Copy()
	case *cell.Builder:
		if v == nil {
			return nil
		}
		return v.Copy()
	default:
		return val
	}
}
