package funcs

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return SETCP(0) },
		func() vm.OP { return SETCPX() },
	)
}

func setCodepage(state *vm.State, cp int) error {
	if cp != 0 {
		return vmerr.Error(vmerr.CodeInvalidOpcode, "unsupported codepage")
	}
	state.CP = cp
	return nil
}

// OpSETCP is a struct-based opcode: one allocation per executed instruction
// instead of an AdvancedOP carrying per-instance closures.
type OpSETCP struct {
	cp int
}

var setcpPrefix = helpers.BytesPrefix(0xFF)

func SETCP(cp int) *OpSETCP {
	return &OpSETCP{cp: cp}
}

func (op *OpSETCP) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(setcpPrefix)
}

func (op *OpSETCP) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(setcpPrefix.Bits); err != nil {
		return err
	}
	v, err := code.LoadUInt(8)
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
	}
	op.cp = int((v+0x10)&0xFF) - 0x10
	return nil
}

func (op *OpSETCP) Serialize() *cell.Builder {
	var raw uint8
	if op.cp >= 0 {
		raw = uint8(op.cp)
	} else {
		raw = uint8(op.cp + 256)
	}
	return cell.BeginCell().
		MustStoreSlice(setcpPrefix.Data, setcpPrefix.Bits).
		MustStoreUInt(uint64(raw), 8)
}

func (op *OpSETCP) SerializeText() string {
	return fmt.Sprintf("SETCP %d", op.cp)
}

func (op *OpSETCP) InstructionBits() int64 {
	return int64(setcpPrefix.Bits) + 8
}

func (op *OpSETCP) Interpret(state *vm.State) error {
	return setCodepage(state, op.cp)
}

func SETCPX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: "SETCPX",
		Action: func(state *vm.State) error {
			cp, err := state.Stack.PopIntRangeInt64(-0x8000, 0x7fff)
			if err != nil {
				return err
			}
			return setCodepage(state, int(cp))
		},
		BitPrefix: helpers.BytesPrefix(0xFF, 0xF0),
	}
}
