package tuple

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return NULLSWAPIF() },
		func() vm.OP { return NULLSWAPIFNOT() },
		func() vm.OP { return NULLROTRIF() },
		func() vm.OP { return NULLROTRIFNOT() },
		func() vm.OP { return NULLSWAPIF2() },
		func() vm.OP { return NULLSWAPIFNOT2() },
		func() vm.OP { return NULLROTRIF2() },
		func() vm.OP { return NULLROTRIFNOT2() },
	)
}

func NULLSWAPIF() *helpers.SimpleOP {
	return makeNullOp("NULLSWAPIF", 0x6fa0, true, 0, 1)
}

func NULLSWAPIFNOT() *helpers.SimpleOP {
	return makeNullOp("NULLSWAPIFNOT", 0x6fa1, false, 0, 1)
}

func NULLROTRIF() *helpers.SimpleOP {
	return makeNullOp("NULLROTRIF", 0x6fa2, true, 1, 1)
}

func NULLROTRIFNOT() *helpers.SimpleOP {
	return makeNullOp("NULLROTRIFNOT", 0x6fa3, false, 1, 1)
}

func NULLSWAPIF2() *helpers.SimpleOP {
	return makeNullOp("NULLSWAPIF2", 0x6fa4, true, 0, 2)
}

func NULLSWAPIFNOT2() *helpers.SimpleOP {
	return makeNullOp("NULLSWAPIFNOT2", 0x6fa5, false, 0, 2)
}

func NULLROTRIF2() *helpers.SimpleOP {
	return makeNullOp("NULLROTRIF2", 0x6fa6, true, 1, 2)
}

func NULLROTRIFNOT2() *helpers.SimpleOP {
	return makeNullOp("NULLROTRIFNOT2", 0x6fa7, false, 1, 2)
}

func makeNullOp(name string, prefix uint64, cond bool, depth, count int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name:   name,
		Prefix: []byte{byte(prefix >> 8), byte(prefix)},
		Action: func(state *vm.State) error {
			if state.Stack.Len() < depth+1 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}

			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			should := (x.Sign() == 0) != cond
			if should {
				for i := 0; i < count; i++ {
					if err = state.Stack.PushAny(nil); err != nil {
						return err
					}
				}
				for i := 0; i < depth; i++ {
					if err = state.Stack.Exchange(i, i+count); err != nil {
						return err
					}
				}
			}

			return state.Stack.PushInt(x)
		},
	}
}
