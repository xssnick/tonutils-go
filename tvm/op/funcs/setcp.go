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

func SETCP(cp int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("SETCP %d", cp)
		},
		BitPrefix:     helpers.BytesPrefix(0xFF),
		FixedSizeBits: 8,
		SerializeSuffix: func() *cell.Builder {
			var raw uint8
			if cp >= 0 {
				raw = uint8(cp)
			} else {
				raw = uint8(cp + 256)
			}
			return cell.BeginCell().MustStoreUInt(uint64(raw), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			cp = int((v + 0x10) & 0xFF)
			cp -= 0x10
			return nil
		},
		Action: func(state *vm.State) error {
			return setCodepage(state, cp)
		},
	}
}

func SETCPX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: "SETCPX",
		Action: func(state *vm.State) error {
			cp, err := state.Stack.PopIntRange(-0x8000, 0x7fff)
			if err != nil {
				return err
			}
			return setCodepage(state, int(cp.Int64()))
		},
		BitPrefix: helpers.BytesPrefix(0xFF, 0xF0),
	}
}
