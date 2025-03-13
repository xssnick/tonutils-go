package funcs

import (
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SENDRAWMSG() })
}

func SENDRAWMSG() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntRange(0, 255)
			if err != nil {
				return err
			}

			c1, err := state.Stack.PopCell()
			if err != nil {
				return err
			}

			list := tlb.OutList{
				Prev: state.Reg.D[1],
				Out: tlb.ActionSendMsg{
					Mode: uint8(i0.Uint64()),
					Msg:  c1,
				},
			}

			res, err := tlb.ToCell(list)
			if err != nil {
				return vmerr.VMError{
					Code: vmerr.ErrCellOverflow.Code,
					Msg:  "cannot serialize raw output message into an output action cell; " + err.Error(),
				}
			}
			state.Reg.D[1] = res
			return nil
		},
		Name:   "SENDRAWMSG",
		Prefix: []byte{0xFB, 0x00},
	}
}
