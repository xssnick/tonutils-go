package exec

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return SETCONTCTR(0) })
}

func SETCONTCTR(i int) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

			cont0, err := state.Stack.PopContinuation()
			if err != nil {
				return err
			}

			v1, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			cont0 = vm.ForceControlData(cont0)
			data := cont0.GetControlData()
			if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(v1)) {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			return state.Stack.PushContinuation(cont0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("c%d SETCONTCTR", i)
		},
		BitPrefix:         helpers.SlicePrefix(12, []byte{0xED, 0x60}),
		Prefixes:          controlRegisterPrefixes(0xED60),
		SerializeSuffix:   serializeControlRegisterIndex(&i),
		DeserializeSuffix: deserializeControlRegisterIndex(&i),
	}
	return op
}
