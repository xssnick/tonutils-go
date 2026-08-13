package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, index2Op, index3Op)
}

// The operand is the encoded suffix itself: INDEX2 packs its two indices as
// i:j, two bits each, and INDEX3 packs three the same way.
var index2Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6fb, 12)),
	ArgBits:  4,
	Name: func(args uint64) string {
		return fmt.Sprintf("INDEX2 %d,%d", (args>>2)&3, args&3)
	},
	Action: func(state *vm.State, args uint64) error {
		return execIndex2(state, int((args>>2)&3), int(args&3))
	},
})

var index3Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0x6fc>>2, 10)),
	ArgBits:  6,
	Name: func(args uint64) string {
		return fmt.Sprintf("INDEX3 %d,%d,%d", (args>>4)&3, (args>>2)&3, args&3)
	},
	Action: func(state *vm.State, args uint64) error {
		return execIndex3(state, int((args>>4)&3), int((args>>2)&3), int(args&3))
	},
})

func INDEX2(i, j uint8) vm.OP {
	return vm.Bind(index2Op, (uint64(i&3)<<2)|uint64(j&3))
}

func INDEX3(i, j, k uint8) vm.OP {
	return vm.Bind(index3Op, (uint64(i&3)<<4)|(uint64(j&3)<<2)|uint64(k&3))
}

func execIndex2(state *vm.State, i, j int) error {
	current, err := state.Stack.PopTupleRange(255)
	if err != nil {
		return err
	}

	current, err = indexIntermediateTuple(current, i)
	if err != nil {
		return err
	}

	val, err := current.Index(j)
	if err != nil {
		return err
	}

	return state.Stack.PushAny(val)
}

func execIndex3(state *vm.State, i, j, k int) error {
	current, err := state.Stack.PopTupleRange(255)
	if err != nil {
		return err
	}

	current, err = indexIntermediateTuple(current, i)
	if err != nil {
		return err
	}
	current, err = indexIntermediateTuple(current, j)
	if err != nil {
		return err
	}

	val, err := current.Index(k)
	if err != nil {
		return err
	}

	return state.Stack.PushAny(val)
}

func indexIntermediateTuple(current tuplepkg.Tuple, idx int) (tuplepkg.Tuple, error) {
	val, err := current.Index(idx)
	if err != nil {
		var zero tuplepkg.Tuple
		return zero, err
	}
	nested, ok := val.(tuplepkg.Tuple)
	if !ok || nested.IsNull() || nested.Len() > 255 {
		var zero tuplepkg.Tuple
		return zero, vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a tuple")
	}

	return nested, nil
}
