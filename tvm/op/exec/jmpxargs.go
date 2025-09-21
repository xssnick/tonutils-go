package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return JMPXARGS(0) })
}

func JMPXARGS(params int) *helpers.AdvancedOP {
	op := &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			cont, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			return state.JumpArgs(cont, params)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("JMPXARGS %d", params)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xDB, 0x10}, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(params), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			params = int(val)
			return nil
		},
	}
	return op
}
