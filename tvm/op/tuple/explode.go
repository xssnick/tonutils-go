package tuple

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return EXPLODE(0) })
}

func EXPLODE(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f4, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d EXPLODE", n)
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(n), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			n = uint8(val)
			return nil
		},
		Action: func(state *vm.State) error {
			return execExplode(state, int(n))
		},
	}
}

func execExplode(state *vm.State, max int) error {
	tup, err := state.Stack.PopTupleRange(max)
	if err != nil {
		return err
	}

	length := tup.Len()
	for i := 0; i < length; i++ {
		val, err := tup.Index(i)
		if err != nil {
			return err
		}
		if err = state.Stack.PushAny(val); err != nil {
			return err
		}
	}

	return state.Stack.PushInt(big.NewInt(int64(length)))
}
