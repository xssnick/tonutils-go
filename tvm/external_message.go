package tvm

import (
	"errors"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
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
	PrecompiledGasUsage any
	InMsgParams         tuple.Tuple
	Globals             map[int]any
	GlobalID            int32
	Libraries           []*cell.Cell
	Gas                 vm.Gas
}

type EmulateExternalMessageConfig = MessageEmulationConfig
type EmulateInternalMessageConfig = MessageEmulationConfig
type EmulateTickTockTransactionConfig = MessageEmulationConfig

type MessageExecutionResult struct {
	ExecutionResult
	Accepted bool
}

func (tvm *TVM) EmulateExternalMessage(code, data *cell.Cell, msg *tlb.ExternalMessage, cfg EmulateExternalMessageConfig) (*MessageExecutionResult, error) {
	addr := cfg.Address
	if addr == nil {
		addr = msg.DstAddr
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
	if err = stack.PushInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushInt(big.NewInt(0)); err != nil {
		return nil, err
	}
	if err = stack.PushCell(msgCell); err != nil {
		return nil, err
	}
	if err = stack.PushSlice(body.BeginParse()); err != nil {
		return nil, err
	}
	if err = stack.PushInt(big.NewInt(-1)); err != nil {
		return nil, err
	}

	c7, err := buildMessageEmulationC7(addr, code, cfg, balance)
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7, defaultExternalMessageGas(cfg.Gas), stack, cfg.Libraries...)
}

func (tvm *TVM) EmulateInternalMessage(code, data, body *cell.Cell, amount uint64, cfg EmulateInternalMessageConfig) (*MessageExecutionResult, error) {
	body = messageBodyCell(body)
	msgCell, err := buildInternalMessageForEmulation(cfg.Address, body, amount)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := messageEmulationBalance(cfg.Balance)
	if err = stack.PushInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushInt(new(big.Int).SetUint64(amount)); err != nil {
		return nil, err
	}
	if err = stack.PushCell(msgCell); err != nil {
		return nil, err
	}
	if err = stack.PushSlice(body.BeginParse()); err != nil {
		return nil, err
	}
	if err = stack.PushInt(big.NewInt(0)); err != nil {
		return nil, err
	}

	c7, err := buildMessageEmulationC7(cfg.Address, code, cfg, balance)
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7, defaultInternalMessageGas(cfg.Gas, amount), stack, cfg.Libraries...)
}

func (tvm *TVM) EmulateTickTransaction(code, data *cell.Cell, cfg EmulateTickTockTransactionConfig) (*MessageExecutionResult, error) {
	return tvm.emulateTickTockTransaction(code, data, false, cfg)
}

func (tvm *TVM) EmulateTockTransaction(code, data *cell.Cell, cfg EmulateTickTockTransactionConfig) (*MessageExecutionResult, error) {
	return tvm.emulateTickTockTransaction(code, data, true, cfg)
}

func (tvm *TVM) emulateTickTockTransaction(code, data *cell.Cell, isTock bool, cfg EmulateTickTockTransactionConfig) (*MessageExecutionResult, error) {
	addr := cfg.Address
	if addr == nil {
		return nil, errors.New("tick/tock emulation address is required")
	}

	accAddr, err := messageEmulationAccountAddr(addr)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := messageEmulationBalance(cfg.Balance)
	if err = stack.PushInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushInt(accAddr); err != nil {
		return nil, err
	}
	if err = stack.PushBool(isTock); err != nil {
		return nil, err
	}
	if err = stack.PushInt(big.NewInt(-2)); err != nil {
		return nil, err
	}

	c7, err := buildMessageEmulationC7(addr, code, cfg, balance)
	if err != nil {
		return nil, err
	}

	return tvm.executeMessageEmulation(code, data, c7, defaultTickTockTransactionGas(cfg.Gas), stack, cfg.Libraries...)
}

func (tvm *TVM) executeMessageEmulation(code, data *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, libraries ...*cell.Cell) (*MessageExecutionResult, error) {
	res, execErr := tvm.ExecuteDetailedWithLibraries(code, data, c7, gas, stack, libraries...)
	if execErr != nil {
		return nil, execErr
	}

	out := &MessageExecutionResult{
		ExecutionResult: *res,
		Accepted:        res.Gas.Credit == 0,
	}
	if !out.Accepted || !vm.IsSuccessExitCode(out.ExitCode) {
		out.Code = code
		out.Data = data
		out.Actions = nil
	}
	return out, nil
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

func buildMessageEmulationC7(addr *address.Address, code *cell.Cell, cfg MessageEmulationConfig, balance *big.Int) (tuple.Tuple, error) {
	seed, err := messageEmulationSeed(cfg.RandSeed)
	if err != nil {
		return tuple.Tuple{}, err
	}

	now := cfg.Now
	if now == 0 {
		now = uint32(time.Now().Unix())
	}

	myAddr := cell.BeginCell().MustStoreAddr(addr).ToSlice()
	values := []any{
		uint32(0x076ef1ea),
		uint8(0),
		uint8(0),
		int64(now),
		cfg.BlockLT,
		cfg.LogicalTime,
		seed,
		*tuple.NewTuple(new(big.Int).Set(balance), nil),
		myAddr,
		cfg.ConfigRoot,
		code,
		messageIncomingValue(cfg.IncomingValue),
		cfg.StorageFees,
		cfg.PrevBlocks,
		messageUnpackedConfig(cfg),
		cfg.DuePayment,
		cfg.PrecompiledGasUsage,
		messageInMsgParams(cfg.InMsgParams),
	}

	normalizedValues := make([]any, len(values))
	for i, val := range values {
		normalizedValues[i] = normalizeMessageTupleValue(val)
	}

	inner := *tuple.NewTuple(normalizedValues...)
	topLen := 1
	for idx := range cfg.Globals {
		if idx <= 0 {
			return tuple.Tuple{}, errors.New("c7 global index 0 is reserved")
		}
		if idx+1 > topLen {
			topLen = idx + 1
		}
	}

	top := tuple.NewTupleSized(topLen)
	top.Set(0, inner)
	for idx, val := range cfg.Globals {
		if err = top.Set(idx, normalizeMessageTupleValue(val)); err != nil {
			return tuple.Tuple{}, err
		}
	}
	return top, nil
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
		return *tuple.NewTuple(big.NewInt(0), nil)
	}
	return value
}

func messageUnpackedConfig(cfg MessageEmulationConfig) any {
	if cfg.UnpackedConfig.Len() > 0 {
		return cfg.UnpackedConfig
	}
	if cfg.GlobalID == 0 {
		return nil
	}
	return *tuple.NewTuple(nil, cell.BeginCell().MustStoreUInt(uint64(uint32(cfg.GlobalID)), 32).ToSlice())
}

func messageInMsgParams(params tuple.Tuple) tuple.Tuple {
	if params.Len() > 0 {
		return params
	}
	return *tuple.NewTuple(
		int64(0),
		int64(0),
		cell.BeginCell().MustStoreUInt(0, 2).ToSlice(),
		int64(0),
		int64(0),
		int64(0),
		int64(0),
		int64(0),
		nil,
		nil,
	)
}

func normalizeMessageTupleValue(val any) any {
	switch v := val.(type) {
	case int:
		return big.NewInt(int64(v))
	case int8:
		return big.NewInt(int64(v))
	case int16:
		return big.NewInt(int64(v))
	case int32:
		return big.NewInt(int64(v))
	case int64:
		return big.NewInt(v)
	case uint8:
		return new(big.Int).SetUint64(uint64(v))
	case uint16:
		return new(big.Int).SetUint64(uint64(v))
	case uint32:
		return new(big.Int).SetUint64(uint64(v))
	case uint64:
		return new(big.Int).SetUint64(v)
	case *big.Int:
		if v == nil {
			return nil
		}
		return new(big.Int).Set(v)
	default:
		return val
	}
}
