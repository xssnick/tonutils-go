package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, indexQOp)
}

var indexQOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f6, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d INDEXQ", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		return execIndexQuiet(state, int(uint8(args)))
	},
})

func INDEXQ(n uint8) vm.OP {
	return vm.Bind(indexQOp, uint64(n))
}

func execIndexQuiet(state *vm.State, idx int) error {
	tup, err := state.Stack.PopMaybeTupleRange(255)
	if err != nil {
		return err
	}
	if tup == nil || idx >= tup.Len() || idx < 0 {
		return state.Stack.PushAny(nil)
	}
	val, err := tup.Index(idx)
	if err != nil {
		return err
	}
	return state.Stack.PushAny(val)
}
