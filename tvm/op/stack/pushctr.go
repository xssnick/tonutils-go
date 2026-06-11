package stack

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type OpPUSHCTR struct {
	helpers.Prefixed
	ctrIndex uint8
}

func init() {
	vm.List = append(vm.List, func() vm.OP { return PUSHCTR(0) })
}

var pushCtrPrefixed = helpers.NewPrefixed(pushCtrPrefixes()...)

func pushCtrPrefixes() []helpers.BitPrefix {
	prefixes := make([]helpers.BitPrefix, 0, 7)
	for _, idx := range []uint64{0, 1, 2, 3, 4, 5, 7} {
		prefixes = append(prefixes, helpers.UIntPrefix(0xED40|idx, 16))
	}
	return prefixes
}

func PUSHCTR(ctrIndex uint8) *OpPUSHCTR {
	return &OpPUSHCTR{
		Prefixed: pushCtrPrefixed,
		ctrIndex: ctrIndex,
	}
}

func (op *OpPUSHCTR) Deserialize(code *cell.Slice) error {
	data, err := code.LoadUInt(16)
	if err != nil {
		return err
	}
	op.ctrIndex = uint8(data & 0xF)
	if op.ctrIndex == 6 || op.ctrIndex > 7 {
		return vm.ErrCorruptedOpcode
	}
	return nil
}

func (op *OpPUSHCTR) Serialize() *cell.Builder {
	return helpers.Builder([]byte{0xED, 0x40 | (op.ctrIndex & 0xF)})
}

func (op *OpPUSHCTR) SerializeText() string {
	return fmt.Sprintf("c%d PUSH", op.ctrIndex)
}

func (op *OpPUSHCTR) InstructionBits() int64 {
	return 16
}

func (op *OpPUSHCTR) Interpret(state *vm.State) error {
	val := state.Reg.Get(int(op.ctrIndex))
	if op.ctrIndex == 4 || op.ctrIndex == 5 {
		return state.Stack.PushOwnedValue(val)
	}
	return state.Stack.PushAny(cloneLegacyControlRegisterValue(val))
}

func cloneLegacyControlRegisterValue(v any) any {
	switch val := v.(type) {
	case vm.Continuation:
		if val == nil {
			return nil
		}
		return val.Copy()
	case tuple.Tuple:
		return val.Copy()
	default:
		return v
	}
}
