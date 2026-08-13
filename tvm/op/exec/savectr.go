package exec

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, saveCtrOp)
}

var saveCtrOp = newControlRegisterOp(saveCtrBitPrefix, saveCtrPrefixes, "SAVECTR", func(state *vm.State, i int) error {
	c0 := vm.ForceControlData(cloneContinuation(state.Reg.C[0]))
	data := c0.GetControlData()
	if !defineControlRegister(state, &data.Save, i, cloneControlRegisterValue(state.Reg.Get(i))) {
		return vmerr.Error(vmerr.CodeTypeCheck)
	}

	state.Reg.C[0] = c0
	return nil
})

func SAVECTR(i int) vm.OP {
	return vm.Bind(saveCtrOp, uint64(i))
}
