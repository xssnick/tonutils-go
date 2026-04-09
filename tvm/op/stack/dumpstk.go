package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return DUMPSTK() },
		func() vm.OP { return STRDUMP() },
	)
}

func DUMPSTK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			// TODO: log
			println("DUMPSTK:\n", state.Stack.String())
			return nil
		},
		Name:      "DUMPSTK",
		BitPrefix: helpers.BytesPrefix(0xFE, 0x00),
	}
}

func STRDUMP() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.Stack.Len() == 0 {
				fmt.Println("STRDUMP: s0 is absent")
				return nil
			}

			val, err := state.Stack.Get(0)
			if err != nil {
				return nil
			}

			sl, ok := val.(*cell.Slice)
			if !ok {
				fmt.Println("STRDUMP: is not a slice")
				return nil
			}

			if sl.BitsLeft()%8 != 0 {
				fmt.Println("STRDUMP: slice contains not valid bits count")
				return nil
			}

			cp := sl.Copy()
			data, err := cp.LoadSlice(cp.BitsLeft())
			if err != nil {
				fmt.Println("STRDUMP: failed to load slice")
				return nil
			}

			fmt.Println("STRDUMP:", string(data))
			return nil
		},
		Name:      "STRDUMP",
		BitPrefix: helpers.BytesPrefix(0xFE, 0x14),
	}
}
