package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, indexOp)
}

var indexOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f1, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d INDEX", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		tup, err := state.Stack.PopTupleRange(255)
		if err != nil {
			return err
		}
		v, err := tup.Index(int(uint8(args)))
		if err != nil {
			return err
		}
		return state.Stack.PushAny(v)
	},
})

func INDEX(n uint8) vm.OP {
	return vm.Bind(indexOp, uint64(n))
}
