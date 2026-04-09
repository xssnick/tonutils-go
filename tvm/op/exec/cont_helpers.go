package exec

import (
	"fmt"
	"reflect"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func loadContinuationFromCodeCell(state *vm.State, code *cell.Cell) (vm.Continuation, error) {
	sl, err := state.Cells.BeginParse(code)
	if err != nil {
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

func pushCurrentCode(state *vm.State) error {
	if state.CurrentCode == nil {
		return vmerr.Error(vmerr.CodeTypeCheck, "current code is nil")
	}
	return state.Stack.PushSlice(state.CurrentCode.Copy())
}

func defineSavedContinuation(reg *vm.Register, idx int, cont vm.Continuation) {
	if reg.C[idx] == nil {
		reg.C[idx] = cont
	}
}

func c1Envelope(state *vm.State, cont vm.Continuation, save bool) vm.Continuation {
	if save {
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
		c0 := vm.ForceControlData(state.Reg.C[0])
		defineSavedContinuation(&c0.GetControlData().Save, 1, state.Reg.C[1])
		state.Reg.C[0] = c0
	}
	state.Reg.C[1] = state.Reg.C[0]
}

type refCodeOp struct {
	prefix            helpers.BitPrefix
	name              string
	refs              []*cell.Cell
	fixedBits         int64
	serializeSuffix   func() *cell.Builder
	deserializeSuffix func(*cell.Slice) error
	action            func(*vm.State, []*cell.Cell) error
}

func newRefCodeOp(name string, prefix helpers.BitPrefix, refsCount int, action func(*vm.State, []*cell.Cell) error) *refCodeOp {
	return &refCodeOp{
		prefix: prefix,
		name:   name,
		refs:   make([]*cell.Cell, refsCount),
		action: action,
	}
}

func bindRefCodeOp(op *refCodeOp, refs ...*cell.Cell) vm.OP {
	for i, ref := range refs {
		if i >= len(op.refs) || ref == nil {
			continue
		}
		op.refs[i] = ref
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
	if _, err := code.LoadSlice(op.prefix.Bits); err != nil {
		return err
	}
	if op.deserializeSuffix != nil {
		if err := op.deserializeSuffix(code); err != nil {
			return err
		}
	}

	for i := range op.refs {
		ref, err := code.PeekRefCell()
		if err != nil {
			op.refs[i] = nil
			continue
		}
		if err = code.AdvanceExt(0, 1); err != nil {
			return err
		}
		op.refs[i] = ref
	}
	return nil
}

func (op *refCodeOp) Serialize() *cell.Builder {
	builder := cell.BeginCell().MustStoreSlice(op.prefix.Data, op.prefix.Bits)
	if op.serializeSuffix != nil {
		builder.MustStoreBuilder(op.serializeSuffix())
	}
	for _, ref := range op.refs {
		builder.MustStoreRef(ref)
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
	for _, ref := range op.refs {
		if ref == nil {
			return vmerr.Error(vmerr.CodeInvalidOpcode, fmt.Sprintf("no references left for a %s instruction", op.name))
		}
	}
	return op.action(state, op.refs)
}

func sameStackValueType(x, y any) bool {
	switch x.(type) {
	case nil:
		return y == nil
	case vm.NaN, *vm.NaN:
		switch y.(type) {
		case vm.NaN, *vm.NaN:
			return true
		default:
			return false
		}
	case vm.Continuation:
		_, ok := y.(vm.Continuation)
		return ok
	default:
		return reflect.TypeOf(x) == reflect.TypeOf(y)
	}
}
