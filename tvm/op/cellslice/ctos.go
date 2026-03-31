package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CTOS() })
}

func CTOS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			c, err := state.Stack.PopCell()
			if err != nil {
				return err
			}

			if c.GetType() != cell.OrdinaryCellType {
				return vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
			}

			sl, err := state.Cells.BeginParse(c)
			if err != nil {
				return err
			}
			return state.Stack.PushSlice(sl)
		},
		Name:      "CTOS",
		BitPrefix: helpers.BytesPrefix(0xD0),
	}
}
