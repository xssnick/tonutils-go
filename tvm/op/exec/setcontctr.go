package exec

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETCONTCTR(0) })
}

func SETCONTCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			cont0, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			v1, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			cont0 = vm.ForceControlData(cont0)
			if !cont0.GetControlData().Save.Define(i, v1) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			return state.Stack.PushContinuation(cont0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SETCONTCTR", i)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xED, 0x60}, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = int(val)
			return nil
		},
	}
	return op
}
