package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return DICTIGETJMPZ() })
}

func DICTIGETJMPZ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			i0, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}

			c1, err := state.Stack.PopMaybeCell()
			if err != nil {
				return err
			}

			i2, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			if c1 != nil {
				if v, err := c1.AsDict(uint(i0.Uint64())).LoadValueByIntKey(i2); err == nil &&
					(v.RefsNum() > 0 || v.BitsLeft() > 0) {
					cnt := &vm.OrdinaryContinuation{
						Data: vm.ControlData{
							NumArgs: vm.ControlDataAllArgs,
							CP:      state.CP,
						},
						Code: v,
					}
					return state.Jump(cnt)
				} else if err != nil {
					return err
				}
			}

			return state.Stack.PushInt(i2)
		},
		Name:   "DICTIGETJMPZ",
		Prefix: []byte{0xF4, 0xBC},
	}
}
