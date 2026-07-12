package exec

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func DICTIGETJMPZ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 3); err != nil {
				return err
			}

			i0, err := state.Stack.PopIntRangeInt64(0, 1023)
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
				if v, err := c1.AsDict(uint(i0)).SetTrace(state.Cells.Trace()).LoadValueByIntKey(i2); err == nil &&
					v != nil {
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

			return state.Stack.PushOwnedInt(i2)
		},
		Name:      "DICTIGETJMPZ",
		BitPrefix: helpers.BytesPrefix(0xF4, 0xBC),
	}
}
