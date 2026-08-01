package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const (
	maxContinuationInt32 = int64(1<<31 - 1)
	minContinuationInt32 = -1 << 31
	maxControlDataArgs   = 1<<13 - 1
	maxControlDataCP     = 1<<15 - 1
	minControlDataCP     = -1 << 15
)

type continuationEncoder struct {
	active map[vm.Continuation]struct{}
}

func newContinuationEncoder() *continuationEncoder {
	return &continuationEncoder{}
}

func (e *continuationEncoder) serialize(b *cell.Builder, cont vm.Continuation) error {
	if err := validateContinuation(cont); err != nil {
		return err
	}
	if _, ok := e.active[cont]; ok {
		return fmt.Errorf("cyclic continuation graph")
	}
	if e.active == nil {
		e.active = make(map[vm.Continuation]struct{})
	}

	e.active[cont] = struct{}{}
	defer delete(e.active, cont)

	switch c := cont.(type) {
	case *vm.OrdinaryContinuation:
		if err := b.StoreUInt(0, 2); err != nil {
			return fmt.Errorf("failed to store ordinary continuation tag: %w", err)
		}
		if err := e.serializeControlData(b, &c.Data); err != nil {
			return fmt.Errorf("failed to serialize ordinary continuation control data: %w", err)
		}
		if c.Code == nil {
			return fmt.Errorf("ordinary continuation code is nil")
		}
		if err := serializeStackSlice(b, c.Code, false); err != nil {
			return fmt.Errorf("failed to serialize ordinary continuation code: %w", err)
		}
	case *vm.ArgExtContinuation:
		if err := b.StoreUInt(1, 2); err != nil {
			return fmt.Errorf("failed to store envelope continuation tag: %w", err)
		}
		if err := e.serializeControlData(b, &c.Data); err != nil {
			return fmt.Errorf("failed to serialize envelope continuation control data: %w", err)
		}
		if err := e.serializeRef(b, c.Ext); err != nil {
			return fmt.Errorf("failed to serialize envelope continuation next: %w", err)
		}
	case *vm.QuitContinuation:
		if c.ExitCode < minContinuationInt32 || c.ExitCode > maxContinuationInt32 {
			return fmt.Errorf("quit continuation exit code does not fit int32")
		}
		if err := b.StoreUInt(8, 4); err != nil {
			return fmt.Errorf("failed to store quit continuation tag: %w", err)
		}
		if err := b.StoreInt(c.ExitCode, 32); err != nil {
			return fmt.Errorf("failed to store quit continuation exit code: %w", err)
		}
	case *vm.ExcQuitContinuation:
		if err := b.StoreUInt(9, 4); err != nil {
			return fmt.Errorf("failed to store exception quit continuation tag: %w", err)
		}
	case *vm.RepeatContinuation:
		if c.Count < 0 {
			return fmt.Errorf("repeat continuation count does not fit uint63")
		}
		if err := b.StoreUInt(0x14, 5); err != nil {
			return fmt.Errorf("failed to store repeat continuation tag: %w", err)
		}
		if err := b.StoreUInt(uint64(c.Count), 63); err != nil {
			return fmt.Errorf("failed to store repeat continuation count: %w", err)
		}
		if err := e.serializeRef(b, c.Body); err != nil {
			return fmt.Errorf("failed to serialize repeat continuation body: %w", err)
		}
		if err := e.serializeRef(b, c.After); err != nil {
			return fmt.Errorf("failed to serialize repeat continuation after: %w", err)
		}
	case *vm.UntilContinuation:
		if err := b.StoreUInt(0x30, 6); err != nil {
			return fmt.Errorf("failed to store until continuation tag: %w", err)
		}
		if err := e.serializeRef(b, c.Body); err != nil {
			return fmt.Errorf("failed to serialize until continuation body: %w", err)
		}
		if err := e.serializeRef(b, c.After); err != nil {
			return fmt.Errorf("failed to serialize until continuation after: %w", err)
		}
	case *vm.AgainContinuation:
		if err := b.StoreUInt(0x31, 6); err != nil {
			return fmt.Errorf("failed to store again continuation tag: %w", err)
		}
		if err := e.serializeRef(b, c.Body); err != nil {
			return fmt.Errorf("failed to serialize again continuation body: %w", err)
		}
	case *vm.WhileContinuation:
		tag := uint64(0x33)
		if c.CheckCond {
			tag = 0x32
		}
		if err := b.StoreUInt(tag, 6); err != nil {
			return fmt.Errorf("failed to store while continuation tag: %w", err)
		}
		if err := e.serializeRef(b, c.Cond); err != nil {
			return fmt.Errorf("failed to serialize while continuation condition: %w", err)
		}
		if err := e.serializeRef(b, c.Body); err != nil {
			return fmt.Errorf("failed to serialize while continuation body: %w", err)
		}
		if err := e.serializeRef(b, c.After); err != nil {
			return fmt.Errorf("failed to serialize while continuation after: %w", err)
		}
	case *vm.PushIntContinuation:
		if c.Int < minContinuationInt32 || c.Int > maxContinuationInt32 {
			return fmt.Errorf("push-int continuation value does not fit int32")
		}
		if err := b.StoreUInt(0x0f, 4); err != nil {
			return fmt.Errorf("failed to store push-int continuation tag: %w", err)
		}
		if err := b.StoreInt(c.Int, 32); err != nil {
			return fmt.Errorf("failed to store push-int continuation value: %w", err)
		}
		if err := e.serializeRef(b, c.Next); err != nil {
			return fmt.Errorf("failed to serialize push-int continuation next: %w", err)
		}
	}

	return nil
}

func validateContinuation(cont vm.Continuation) error {
	if cont == nil {
		return fmt.Errorf("continuation is nil")
	}

	switch c := cont.(type) {
	case *vm.OrdinaryContinuation:
		if c == nil {
			return fmt.Errorf("ordinary continuation is nil")
		}
	case *vm.ArgExtContinuation:
		if c == nil {
			return fmt.Errorf("envelope continuation is nil")
		}
	case *vm.QuitContinuation:
		if c == nil {
			return fmt.Errorf("quit continuation is nil")
		}
	case *vm.ExcQuitContinuation:
		if c == nil {
			return fmt.Errorf("exception quit continuation is nil")
		}
	case *vm.RepeatContinuation:
		if c == nil {
			return fmt.Errorf("repeat continuation is nil")
		}
	case *vm.UntilContinuation:
		if c == nil {
			return fmt.Errorf("until continuation is nil")
		}
	case *vm.AgainContinuation:
		if c == nil {
			return fmt.Errorf("again continuation is nil")
		}
	case *vm.WhileContinuation:
		if c == nil {
			return fmt.Errorf("while continuation is nil")
		}
	case *vm.PushIntContinuation:
		if c == nil {
			return fmt.Errorf("push-int continuation is nil")
		}
	default:
		return fmt.Errorf("unsupported continuation type %T", cont)
	}

	return nil
}

func (e *continuationEncoder) serializeRef(b *cell.Builder, cont vm.Continuation) error {
	nested := cell.BeginCell()
	if err := e.serialize(nested, cont); err != nil {
		return err
	}
	if err := b.StoreRef(nested.EndCell()); err != nil {
		return fmt.Errorf("failed to store continuation ref: %w", err)
	}

	return nil
}

func (e *continuationEncoder) serializeControlData(b *cell.Builder, data *vm.ControlData) error {
	if data.NumArgs < -1 || data.NumArgs > maxControlDataArgs {
		return fmt.Errorf("continuation argument count is outside uint13")
	}
	if data.CP < minControlDataCP || data.CP > maxControlDataCP {
		return fmt.Errorf("continuation codepage is outside int16")
	}

	hasArgs := data.NumArgs >= 0
	if err := b.StoreBoolBit(hasArgs); err != nil {
		return fmt.Errorf("failed to store continuation argument marker: %w", err)
	}
	if hasArgs {
		if err := b.StoreUInt(uint64(data.NumArgs), 13); err != nil {
			return fmt.Errorf("failed to store continuation argument count: %w", err)
		}
	}

	hasStack := data.Stack != nil
	if err := b.StoreBoolBit(hasStack); err != nil {
		return fmt.Errorf("failed to store continuation stack marker: %w", err)
	}
	if hasStack {
		stack, err := newStackFromVMView(data.Stack)
		if err != nil {
			return fmt.Errorf("failed to convert continuation stack: %w", err)
		}
		stackCell, err := stack.toCell(e)
		if err != nil {
			return fmt.Errorf("failed to serialize continuation stack: %w", err)
		}
		if err = b.StoreBuilder(stackCell.ToBuilder()); err != nil {
			return fmt.Errorf("failed to store continuation stack: %w", err)
		}
	}

	if err := e.serializeControlRegisters(b, &data.Save); err != nil {
		return fmt.Errorf("failed to serialize saved control registers: %w", err)
	}

	hasCP := data.CP != vm.CP
	if err := b.StoreBoolBit(hasCP); err != nil {
		return fmt.Errorf("failed to store continuation codepage marker: %w", err)
	}
	if hasCP {
		if err := b.StoreInt(int64(data.CP), 16); err != nil {
			return fmt.Errorf("failed to store continuation codepage: %w", err)
		}
	}

	return nil
}

func (e *continuationEncoder) serializeControlRegisters(b *cell.Builder, registers *vm.Register) error {
	dict := cell.NewDict(4)
	store := func(index int, value any) error {
		encoded := cell.BeginCell()
		if err := serializeStackValue(encoded, value, e); err != nil {
			return fmt.Errorf("failed to serialize c%d: %w", index, err)
		}
		key := cell.BeginCell()
		if err := key.StoreUInt(uint64(index), 4); err != nil {
			return fmt.Errorf("failed to serialize c%d key: %w", index, err)
		}
		if err := dict.SetBuilder(key.EndCell(), encoded); err != nil {
			return fmt.Errorf("failed to store c%d: %w", index, err)
		}
		return nil
	}

	for i, cont := range registers.C {
		if cont != nil {
			if err := store(i, cont); err != nil {
				return err
			}
		}
	}
	for i, data := range registers.D {
		if data != nil {
			if err := store(i+4, data); err != nil {
				return err
			}
		}
	}
	if !registers.C7.IsNull() {
		if err := store(7, registers.C7); err != nil {
			return err
		}
	}

	if err := b.StoreDict(dict); err != nil {
		return fmt.Errorf("failed to store control register dictionary: %w", err)
	}

	return nil
}

func parseContinuation(slice *cell.Slice) (vm.Continuation, error) {
	// VmCont is stored inline after stack tag #06. Only its recursive
	// next/body/condition fields are stored in referenced cells.
	prefix, err := slice.PreloadUInt(2)
	if err != nil {
		return nil, fmt.Errorf("failed to load continuation prefix: %w", err)
	}

	switch prefix {
	case 0:
		return parseOrdinaryContinuation(slice)
	case 1:
		return parseArgExtContinuation(slice)
	case 2:
		prefix, err = slice.PreloadUInt(4)
		if err != nil {
			return nil, fmt.Errorf("failed to load continuation prefix: %w", err)
		}
		switch prefix {
		case 8:
			return parseQuitContinuation(slice)
		case 9:
			return parseExcQuitContinuation(slice)
		}
		prefix, err = slice.PreloadUInt(5)
		if err == nil && prefix == 0x14 {
			return parseRepeatContinuation(slice)
		}
	case 3:
		prefix, err = slice.PreloadUInt(4)
		if err != nil {
			return nil, fmt.Errorf("failed to load continuation prefix: %w", err)
		}
		if prefix == 0x0f {
			return parsePushIntContinuation(slice)
		}
		prefix, err = slice.PreloadUInt(6)
		if err == nil {
			switch prefix {
			case 0x30:
				return parseUntilContinuation(slice)
			case 0x31:
				return parseAgainContinuation(slice)
			case 0x32, 0x33:
				return parseWhileContinuation(slice)
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load continuation prefix: %w", err)
	}
	return nil, fmt.Errorf("unknown continuation prefix")
}

func parseOrdinaryContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(2); err != nil {
		return nil, fmt.Errorf("failed to load ordinary continuation tag: %w", err)
	}
	data, err := parseControlData(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ordinary continuation control data: %w", err)
	}
	code, err := parseStackSlice(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ordinary continuation code: %w", err)
	}

	return &vm.OrdinaryContinuation{Data: data, Code: code}, nil
}

func parseArgExtContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(2); err != nil {
		return nil, fmt.Errorf("failed to load envelope continuation tag: %w", err)
	}
	data, err := parseControlData(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse envelope continuation control data: %w", err)
	}
	next, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse envelope continuation next: %w", err)
	}

	return &vm.ArgExtContinuation{Data: data, Ext: next}, nil
}

func parseQuitContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(4); err != nil {
		return nil, fmt.Errorf("failed to load quit continuation tag: %w", err)
	}
	exitCode, err := slice.LoadInt(32)
	if err != nil {
		return nil, fmt.Errorf("failed to load quit continuation exit code: %w", err)
	}

	return &vm.QuitContinuation{ExitCode: exitCode}, nil
}

func parseExcQuitContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(4); err != nil {
		return nil, fmt.Errorf("failed to load exception quit continuation tag: %w", err)
	}
	return &vm.ExcQuitContinuation{}, nil
}

func parseRepeatContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(5); err != nil {
		return nil, fmt.Errorf("failed to load repeat continuation tag: %w", err)
	}
	count, err := slice.LoadUInt(63)
	if err != nil {
		return nil, fmt.Errorf("failed to load repeat continuation count: %w", err)
	}
	body, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repeat continuation body: %w", err)
	}
	after, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repeat continuation after: %w", err)
	}

	return &vm.RepeatContinuation{Count: int64(count), Body: body, After: after}, nil
}

func parseUntilContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(6); err != nil {
		return nil, fmt.Errorf("failed to load until continuation tag: %w", err)
	}
	body, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse until continuation body: %w", err)
	}
	after, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse until continuation after: %w", err)
	}

	return &vm.UntilContinuation{Body: body, After: after}, nil
}

func parseAgainContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(6); err != nil {
		return nil, fmt.Errorf("failed to load again continuation tag: %w", err)
	}
	body, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse again continuation body: %w", err)
	}

	return &vm.AgainContinuation{Body: body}, nil
}

func parseWhileContinuation(slice *cell.Slice) (vm.Continuation, error) {
	tag, err := slice.LoadUInt(6)
	if err != nil {
		return nil, fmt.Errorf("failed to load while continuation tag: %w", err)
	}
	cond, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse while continuation condition: %w", err)
	}
	body, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse while continuation body: %w", err)
	}
	after, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse while continuation after: %w", err)
	}

	return &vm.WhileContinuation{
		CheckCond: tag == 0x32,
		Cond:      cond,
		Body:      body,
		After:     after,
	}, nil
}

func parsePushIntContinuation(slice *cell.Slice) (vm.Continuation, error) {
	if _, err := slice.LoadUInt(4); err != nil {
		return nil, fmt.Errorf("failed to load push-int continuation tag: %w", err)
	}
	value, err := slice.LoadInt(32)
	if err != nil {
		return nil, fmt.Errorf("failed to load push-int continuation value: %w", err)
	}
	next, err := parseContinuationRef(slice)
	if err != nil {
		return nil, fmt.Errorf("failed to parse push-int continuation next: %w", err)
	}

	return &vm.PushIntContinuation{Int: value, Next: next}, nil
}

func parseContinuationRef(slice *cell.Slice) (vm.Continuation, error) {
	ref, err := slice.LoadRef()
	if err != nil {
		return nil, fmt.Errorf("failed to load continuation ref: %w", err)
	}
	cont, err := parseContinuation(ref)
	if err != nil {
		return nil, err
	}
	if ref.BitsLeft() != 0 || ref.RefsNum() != 0 {
		return nil, fmt.Errorf("continuation ref has trailing data")
	}

	return cont, nil
}

func parseControlData(slice *cell.Slice) (vm.ControlData, error) {
	data := vm.ControlData{NumArgs: vm.ControlDataAllArgs, CP: vm.CP}

	hasArgs, err := slice.LoadBoolBit()
	if err != nil {
		return data, fmt.Errorf("failed to load continuation argument marker: %w", err)
	}
	if hasArgs {
		args, err := slice.LoadUInt(13)
		if err != nil {
			return data, fmt.Errorf("failed to load continuation argument count: %w", err)
		}
		data.NumArgs = int(args)
	}

	hasStack, err := slice.LoadBoolBit()
	if err != nil {
		return data, fmt.Errorf("failed to load continuation stack marker: %w", err)
	}
	if hasStack {
		serialized := NewStack()
		if err = serialized.LoadFromCell(slice); err != nil {
			return data, fmt.Errorf("failed to parse continuation stack: %w", err)
		}
		data.Stack, err = stackToVM(serialized)
		if err != nil {
			return data, fmt.Errorf("failed to convert continuation stack: %w", err)
		}
	}

	data.Save, err = parseControlRegisters(slice)
	if err != nil {
		return data, fmt.Errorf("failed to parse saved control registers: %w", err)
	}

	hasCP, err := slice.LoadBoolBit()
	if err != nil {
		return data, fmt.Errorf("failed to load continuation codepage marker: %w", err)
	}
	if hasCP {
		cp, err := slice.LoadInt(16)
		if err != nil {
			return data, fmt.Errorf("failed to load continuation codepage: %w", err)
		}
		if cp == vm.CP {
			return data, fmt.Errorf("present continuation codepage cannot be -1")
		}
		data.CP = int(cp)
	}

	return data, nil
}

func parseControlRegisters(slice *cell.Slice) (vm.Register, error) {
	var registers vm.Register
	dict, err := slice.LoadDict(4)
	if err != nil {
		return registers, fmt.Errorf("failed to load control register dictionary: %w", err)
	}
	items, err := dict.LoadAll()
	if err != nil {
		return registers, fmt.Errorf("failed to load control register values: %w", err)
	}

	for _, item := range items {
		index, err := item.Key.LoadUInt(4)
		if err != nil {
			return registers, fmt.Errorf("failed to load control register index: %w", err)
		}
		value, err := ParseStackValue(item.Value)
		if err != nil {
			return registers, fmt.Errorf("failed to parse c%d: %w", index, err)
		}
		if item.Value.BitsLeft() != 0 || item.Value.RefsNum() != 0 {
			return registers, fmt.Errorf("c%d has trailing data", index)
		}
		value, err = stackValueToVM(value)
		if err != nil {
			return registers, fmt.Errorf("failed to convert c%d: %w", index, err)
		}
		if !registers.Set(int(index), value) {
			return registers, fmt.Errorf("invalid value for c%d", index)
		}
	}

	return registers, nil
}

func vmStackValueToTLB(value any) (any, error) {
	valueTuple, ok := value.(tuple.Tuple)
	if !ok {
		return value, nil
	}
	if valueTuple.IsNull() {
		return nil, nil
	}
	if valueTuple.Len() > 1<<16-1 {
		return nil, fmt.Errorf("tuple length does not fit uint16")
	}

	values := make([]any, valueTuple.Len())
	for i := range values {
		item, err := valueTuple.RawIndex(i)
		if err != nil {
			return nil, fmt.Errorf("failed to load tuple element %d: %w", i, err)
		}
		values[i] = item
	}

	return values, nil
}

func stackValueToVM(value any) (any, error) {
	switch value := value.(type) {
	case StackNaN:
		return vm.NaN{}, nil
	case []any:
		values := make([]any, len(value))
		for i := range value {
			converted, err := stackValueToVM(value[i])
			if err != nil {
				return nil, fmt.Errorf("failed to convert tuple element %d: %w", i, err)
			}
			values[i] = converted
		}
		return tuple.NewTupleOwned(values), nil
	default:
		return value, nil
	}
}

func stackToVM(stack *Stack) (*vm.Stack, error) {
	converted := vm.NewStack()
	for depth := stack.Depth(); depth > 0; depth-- {
		value, err := stack.Pop()
		if err != nil {
			return nil, err
		}
		value, err = stackValueToVM(value)
		if err != nil {
			return nil, err
		}
		if err = converted.PushOwnedValue(value); err != nil {
			return nil, err
		}
	}
	return converted, nil
}
