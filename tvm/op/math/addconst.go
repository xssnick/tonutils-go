package math

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, addConstOp)
}

// addConstOp is registered once and shared by every execution. The constant it
// adds arrives as an argument, so nothing is written back into it and executing
// the instruction allocates nothing.
var addConstOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xA6)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		i0, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		arg := big.NewInt(int64(int8(args)))
		return pushUnaryIntResult(state, i0, func(x *big.Int) *big.Int {
			return x.Add(x, arg)
		})
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(8); err != nil {
			return 0, err
		}
		val, err := code.LoadUInt(8)
		if err != nil {
			return 0, vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
		}
		return val, nil
	},
	Serializer: func(args uint64) *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0xA6, 8).MustStoreInt(int64(int8(args)), 8)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("ADDINT %d", int8(args))
	},
})

func ADDCONST(value int8) vm.OP {
	return vm.Bind(addConstOp, uint64(uint8(value)))
}

func ADDINT(value int8) vm.OP {
	return ADDCONST(value)
}
