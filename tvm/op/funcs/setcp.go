package funcs

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, setCPOp)

	vm.List = append(vm.List,
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

var setcpPrefix = helpers.BytesPrefix(0xFF)

// setCPOp carries the codepage as a two's-complement int64 in the operand, not
// as the byte it is stored in: the encoding is asymmetric, since the byte only
// spans -16..239 while the constructor accepts any int and reproduces the
// reference wraparound for the rest.
var setCPOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(setcpPrefix),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		return setCodepage(state, int(int64(args)))
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(setcpPrefix.Bits); err != nil {
			return 0, err
		}
		v, err := code.LoadUInt(8)
		if err != nil {
			return 0, vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}
		return uint64(int64((v+0x10)&0xFF) - 0x10), nil
	},
	Serializer: func(args uint64) *cell.Builder {
		cp := int64(args)
		var raw uint8
		if cp >= 0 {
			raw = uint8(cp)
		} else {
			raw = uint8(cp + 256)
		}
		return cell.BeginCell().
			MustStoreSlice(setcpPrefix.Data, setcpPrefix.Bits).
			MustStoreUInt(uint64(raw), 8)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("SETCP %d", int64(args))
	},
})

func SETCP(cp int) vm.OP {
	return vm.Bind(setCPOp, uint64(int64(cp)))
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
