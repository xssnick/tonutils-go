package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, callXArgsOp, callXArgsPOp)
}

var callXArgsOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xDA)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		params, retvals := unpackArgPair(args)

		cont, err := state.Stack.PopContinuation()
		if err != nil {
			return err
		}
		return state.CallArgs(cont, params, retvals)
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(8); err != nil {
			return 0, err
		}
		val, err := code.LoadUInt(8)
		if err != nil {
			return 0, err
		}
		return packArgPair(int((val>>4)&0x0F), int(val&0x0F)), nil
	},
	Serializer: func(args uint64) *cell.Builder {
		params, retvals := unpackArgPair(args)
		return cell.BeginCell().MustStoreUInt(0xDA, 8).MustStoreUInt(uint64((params<<4)|retvals), 8)
	},
	Name: func(args uint64) string {
		params, retvals := unpackArgPair(args)
		return fmt.Sprintf("CALLXARGS %d,%d", params, retvals)
	},
})

var callXArgsPOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(12, []byte{0xDB, 0x00})),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			return err
		}
		return state.CallArgs(cont, int(int32(args)), -1)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("CALLXARGS %d,-1", int32(args))
	},
})

func CALLXARGS(params, retvals int) vm.OP {
	return vm.Bind(callXArgsOp, packArgPair(params, retvals))
}

func CALLXARGSP(params int) vm.OP {
	return vm.Bind(callXArgsPOp, uint64(uint32(params)))
}
