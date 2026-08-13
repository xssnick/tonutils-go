package exec

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, setContCtrOp)
}

var setContCtrOp = newControlRegisterOp(setContCtrBitPrefix, setContCtrPrefixes, "SETCONTCTR", func(state *vm.State, i int) error {
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

	return state.Stack.PushOwnedContinuation(cont0)
})

func SETCONTCTR(i int) vm.OP {
	return vm.Bind(setContCtrOp, uint64(i))
}
