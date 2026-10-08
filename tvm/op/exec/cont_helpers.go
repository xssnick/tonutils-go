package exec

import (
	"fmt"
	"math/big"
	"reflect"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// packArgPair carries two signed operand fields in a single uint64 — the first
// in the high half, the second in the low half, each a 32-bit two's complement
// word. The pair survives values outside the nibbles the wire format holds,
// which is what lets a constructor keep behaving like the old per-instruction
// object when it is handed an out-of-range argument.
func packArgPair(first, second int) uint64 {
	return uint64(uint32(first))<<32 | uint64(uint32(second))
}

func unpackArgPair(args uint64) (first, second int) {
	return int(int32(args >> 32)), int(int32(args))
}

func loadContinuationFromCodeCell(state *vm.State, code *cell.Cell, trace *cell.Trace) (vm.Continuation, error) {
	sl := new(cell.Slice)
	if err := state.Cells.BeginParseIntoWithTrace(code, trace, sl); err != nil {
		return nil, err
	}
	return &vm.OrdinaryContinuation{
		Data: vm.ControlData{
			NumArgs: vm.ControlDataAllArgs,
			CP:      state.CP,
		},
		Code: sl,
	}, nil
}

func jumpToCodeCell(state *vm.State, code *cell.Cell, trace *cell.Trace) error {
	var target cell.Slice
	if err := state.Cells.BeginParseIntoWithTrace(code, trace, &target); err != nil {
		return err
	}
	return state.JumpToCode(&target, state.CP)
}

func pushCurrentCode(state *vm.State) error {
	if state.CurrentCode == nil {
		return vmerr.Error(vmerr.CodeTypeCheck, "current code is nil")
	}
	return state.Stack.PushOwnedSlice(state.CurrentCode.Copy())
}

func checkStackDepth(state *vm.State, depth int) error {
	if state.Stack.Len() < depth {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return nil
}

func defineSavedContinuation(reg *vm.Register, idx int, cont vm.Continuation) {
	if vm.IsNullContinuation(reg.C[idx]) {
		reg.C[idx] = cloneContinuation(cont)
	}
}

func cloneContinuation(cont vm.Continuation) vm.Continuation {
	if cont == nil {
		return nil
	}
	return cont.Copy()
}

func c1Envelope(state *vm.State, cont vm.Continuation, save bool) vm.Continuation {
	if save {
		cont = cloneContinuation(cont)
		cont = vm.ForceControlData(cont)
		data := cont.GetControlData()
		defineSavedContinuation(&data.Save, 1, state.Reg.C[1])
		defineSavedContinuation(&data.Save, 0, state.Reg.C[0])
	}
	state.Reg.C[1] = cont
	return cont
}

func c1EnvelopeIf(state *vm.State, enabled bool, cont vm.Continuation) vm.Continuation {
	if !enabled {
		return cont
	}
	return c1Envelope(state, cont, true)
}

func c1SaveSet(state *vm.State, save bool) {
	if save {
		c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
		defineSavedContinuation(&c0.GetControlData().Save, 1, state.Reg.C[1])
		state.Reg.C[0] = c0
	}
	state.Reg.C[1] = cloneContinuation(state.Reg.C[0])
}

type refCodeOp struct {
	prefix            helpers.BitPrefix
	name              string
	refs              [4]*cell.Cell
	refsNum           int
	fixedBits         int64
	serializeSuffix   func() *cell.Builder
	deserializeSuffix func(*cell.Slice) error
	action            func(*vm.State, []*cell.Cell, []*cell.Trace) error
	refTraces         [4]*cell.Trace
}

func newRefCodeOp(name string, prefix helpers.BitPrefix, refsCount int, action func(*vm.State, []*cell.Cell, []*cell.Trace) error) *refCodeOp {
	if refsCount < 0 || refsCount > 4 {
		panic("refCodeOp supports at most 4 references")
	}
	return &refCodeOp{
		prefix:  prefix,
		name:    name,
		refsNum: refsCount,
		action:  action,
	}
}

func bindRefCodeOp(op *refCodeOp, refs ...*cell.Cell) vm.OP {
	for i, ref := range refs {
		if i >= op.refsNum {
			break
		}
		if ref == nil {
			continue
		}
		op.refs[i] = ref
		op.refTraces[i] = ref.Trace()
	}
	return op
}

func (op *refCodeOp) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(op.prefix)
}

func (op *refCodeOp) Deserialize(code *cell.Slice) error {
	return op.DeserializeMatched(code)
}

func (op *refCodeOp) DeserializeMatched(code *cell.Slice) error {
	if err := code.SkipBits(op.prefix.Bits); err != nil {
		return err
	}
	if op.deserializeSuffix != nil {
		if err := op.deserializeSuffix(code); err != nil {
			return err
		}
	}

	for i := 0; i < op.refsNum; i++ {
		ref, trace, err := code.PeekRefCellAtWithTrace(0)
		if err != nil {
			for ; i < op.refsNum; i++ {
				op.refs[i] = nil
				op.refTraces[i] = nil
			}
			return nil
		}
		if err = code.SkipBitsAndRefs(0, 1); err != nil {
			return err
		}
		op.refs[i] = ref
		op.refTraces[i] = trace
	}
	return nil
}

func (op *refCodeOp) Serialize() *cell.Builder {
	builder := cell.BeginCell().MustStoreSlice(op.prefix.Data, op.prefix.Bits)
	if op.serializeSuffix != nil {
		builder.MustStoreBuilder(op.serializeSuffix())
	}
	for i := 0; i < op.refsNum; i++ {
		builder.MustStoreRef(op.refs[i])
	}
	return builder
}

func (op *refCodeOp) SerializeText() string {
	return op.name
}

func (op *refCodeOp) InstructionBits() int64 {
	return int64(op.prefix.Bits) + op.fixedBits
}

func (op *refCodeOp) Interpret(state *vm.State) error {
	for i := 0; i < op.refsNum; i++ {
		if op.refs[i] == nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("no references left for a %s instruction", op.name))
		}
	}
	return op.action(state, op.refs[:op.refsNum], op.refTraces[:op.refsNum])
}

func sameStackValueType(x, y any) bool {
	switch x.(type) {
	case nil:
		return y == nil
	case vm.NaN:
		switch y.(type) {
		case vm.NaN, *big.Int:
			return true
		default:
			return false
		}
	case *big.Int:
		switch y.(type) {
		case vm.NaN, *big.Int:
			return true
		default:
			return false
		}
	case vm.Continuation:
		_, ok := y.(vm.Continuation)
		return ok
	case *cell.Cell:
		switch y.(type) {
		case *cell.Cell:
			return true
		default:
			return false
		}
	default:
		return reflect.TypeOf(x) == reflect.TypeOf(y)
	}
}
