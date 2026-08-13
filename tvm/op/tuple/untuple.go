package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, untupleOp, unpackFirstOp)
}

var untupleOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f2, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d UNTUPLE", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		return execUntuple(state, int(uint8(args)), true)
	},
})

var unpackFirstOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6f3, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("%d UNPACKFIRST", uint8(args))
	},
	Action: func(state *vm.State, args uint64) error {
		return execUntuple(state, int(uint8(args)), false)
	},
})

func UNTUPLE(n uint8) vm.OP {
	return vm.Bind(untupleOp, uint64(n))
}

func UNPACKFIRST(n uint8) vm.OP {
	return vm.Bind(unpackFirstOp, uint64(n))
}

func execUntuple(state *vm.State, count int, exact bool) error {
	max := 255
	min := 0
	if exact {
		max = count
		min = count
	} else {
		min = count
	}

	tup, err := state.Stack.PopTupleRange(max, min)
	if err != nil {
		return err
	}

	limit := count
	if !exact && limit > tup.Len() {
		limit = tup.Len()
	}

	for i := 0; i < limit; i++ {
		val, err := tup.Index(i)
		if err != nil {
			return err
		}
		if err = state.Stack.PushAny(val); err != nil {
			return err
		}
	}

	return state.ConsumeTupleGasLen(limit)
}
