package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList,
		xc2puOp,
		xcpuxcOp,
		xcpu2Op,
		puxc2Op,
		puxcpuOp,
		pu2xcOp,
		push3Op,
	)
}

var xc2puOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x541, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, 2, i, j, k); err != nil {
			return err
		}

		if err := state.Stack.Exchange(1, i); err != nil {
			return err
		}
		if err := state.Stack.Exchange(0, j); err != nil {
			return err
		}
		val, err := state.Stack.Get(k)
		if err != nil {
			return err
		}
		return state.Stack.PushAny(val)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d XC2PU", i, j, k)
	},
})

func XC2PU(i, j, k uint8) vm.OP {
	return vm.Bind(xc2puOp, packArgs3(i, j, k))
}

var xcpuxcOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x542, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, maxStackDepthCount(k, 2), i, j); err != nil {
			return err
		}

		if err := state.Stack.Exchange(1, i); err != nil {
			return err
		}
		val, err := state.Stack.Get(j)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		if err := state.Stack.Exchange(0, 1); err != nil {
			return err
		}
		return state.Stack.Exchange(0, k)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d XCPUXC", i, j, k)
	},
})

func XCPUXC(i, j, k uint8) vm.OP {
	return vm.Bind(xcpuxcOp, packArgs3(i, j, k))
}

var xcpu2Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x543, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, 0, i, j, k); err != nil {
			return err
		}

		if err := state.Stack.Exchange(0, i); err != nil {
			return err
		}
		val, err := state.Stack.Get(j)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		val, err = state.Stack.Get(k + 1)
		if err != nil {
			return err
		}
		return state.Stack.PushAny(val)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d XCPU2", i, j, k)
	},
})

func XCPU2(i, j, k uint8) vm.OP {
	return vm.Bind(xcpu2Op, packArgs3(i, j, k))
}

var puxc2Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x544, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, maxStackDepthCount(j, k), i, 1); err != nil {
			return err
		}

		val, err := state.Stack.Get(i)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		if err := state.Stack.Exchange(2, 0); err != nil {
			return err
		}
		if err := state.Stack.Exchange(1, j); err != nil {
			return err
		}
		return state.Stack.Exchange(0, k)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d PUXC2", i, j, k)
	},
})

func PUXC2(i, j, k uint8) vm.OP {
	return vm.Bind(puxc2Op, packArgs3(i, j, k))
}

var puxcpuOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x545, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, maxStackDepthCount(j, k), i); err != nil {
			return err
		}

		val, err := state.Stack.Get(i)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		if err := state.Stack.Exchange(0, 1); err != nil {
			return err
		}
		if err := state.Stack.Exchange(0, j); err != nil {
			return err
		}
		val, err = state.Stack.Get(k)
		if err != nil {
			return err
		}
		return state.Stack.PushAny(val)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d PUXCPU", i, j, k)
	},
})

func PUXCPU(i, j, k uint8) vm.OP {
	return vm.Bind(puxcpuOp, packArgs3(i, j, k))
}

var pu2xcOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x546, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, maxStackDepthCount(j, k-1), i); err != nil {
			return err
		}

		val, err := state.Stack.Get(i)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		if err := state.Stack.Exchange(1, 0); err != nil {
			return err
		}
		val, err = state.Stack.Get(j)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		if err := state.Stack.Exchange(1, 0); err != nil {
			return err
		}
		return state.Stack.Exchange(0, k)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d PU2XC", i, j, k)
	},
})

func PU2XC(i, j, k uint8) vm.OP {
	return vm.Bind(pu2xcOp, packArgs3(i, j, k))
}

var push3Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x547, 12)),
	ArgBits:  12,
	Action: func(state *vm.State, args uint64) error {
		i, j, k := unpackArgs3(args)
		if err := requireStackDepth(state, 0, i, j, k); err != nil {
			return err
		}

		val, err := state.Stack.Get(i)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		val, err = state.Stack.Get(j + 1)
		if err != nil {
			return err
		}
		if err := state.Stack.PushAny(val); err != nil {
			return err
		}
		val, err = state.Stack.Get(k + 2)
		if err != nil {
			return err
		}
		return state.Stack.PushAny(val)
	},
	Name: func(args uint64) string {
		i, j, k := unpackArgs3(args)
		return fmt.Sprintf("%d,%d,%d PUSH3", i, j, k)
	},
})

func PUSH3(i, j, k uint8) vm.OP {
	return vm.Bind(push3Op, packArgs3(i, j, k))
}
