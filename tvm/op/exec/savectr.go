package exec

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SAVECTR(0) })
}

func SAVECTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			c0 := vm.ForceControlData(state.Reg.C[0])
			if !c0.GetControlData().Save.Define(i, state.Reg.Get(i)) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			state.Reg.C[0] = c0
			return nil
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SAVECTR", i)
		},
		Prefix: cell.BeginCell().MustStoreSlice([]byte{0xED, 0xA0}, 12).EndCell(),
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
