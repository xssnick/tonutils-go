package exec

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SAVECTR(0) })
}

func SAVECTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
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
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0xA0}),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}
