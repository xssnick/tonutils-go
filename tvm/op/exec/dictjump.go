package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList,
		callDictShortOp,
		callDictLongOp,
		jmpDictOp,
		prepareDictOp,
	)
}

func CALLDICT(id int) vm.OP {
	if id >= 0 && id <= 0xFF {
		return callDictShort(id)
	}
	return callDictLong(id)
}

func JMPDICT(id int) vm.OP {
	return jmpDict(id)
}

func PREPAREDICT(id int) vm.OP {
	return prepareDict(id)
}

func currentCodeDict(state *vm.State) (vm.Continuation, error) {
	if vm.IsNullContinuation(state.Reg.C[3]) {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return state.Reg.C[3], nil
}

func pushDictIndex(state *vm.State, args uint64) (vm.Continuation, error) {
	if err := state.Stack.PushSmallInt(int64(int32(args))); err != nil {
		return nil, err
	}
	return currentCodeDict(state)
}

var callDictShortOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xF0)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		cont, err := pushDictIndex(state, args)
		if err != nil {
			return err
		}
		return state.Call(cont)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("CALLDICT %d", int32(args))
	},
})

var callDictLongOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(10, []byte{0xF1, 0x00})),
	ArgBits:  14,
	Action: func(state *vm.State, args uint64) error {
		cont, err := pushDictIndex(state, args)
		if err != nil {
			return err
		}
		return state.Call(cont)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("CALLDICT %d", int32(args))
	},
})

var jmpDictOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(10, []byte{0xF1, 0x40})),
	ArgBits:  14,
	Action: func(state *vm.State, args uint64) error {
		cont, err := pushDictIndex(state, args)
		if err != nil {
			return err
		}
		return state.Jump(cont)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("JMPDICT %d", int32(args))
	},
})

var prepareDictOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(10, []byte{0xF1, 0x80})),
	ArgBits:  14,
	Action: func(state *vm.State, args uint64) error {
		cont, err := pushDictIndex(state, args)
		if err != nil {
			return err
		}
		return state.Stack.PushContinuation(cont)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("PREPAREDICT %d", int32(args))
	},
})

func callDictShort(id int) vm.OP {
	return vm.Bind(callDictShortOp, uint64(uint32(id)))
}

func callDictLong(id int) vm.OP {
	return vm.Bind(callDictLongOp, uint64(uint32(id)))
}

func jmpDict(id int) vm.OP {
	return vm.Bind(jmpDictOp, uint64(uint32(id)))
}

func prepareDict(id int) vm.OP {
	return vm.Bind(prepareDictOp, uint64(uint32(id)))
}
