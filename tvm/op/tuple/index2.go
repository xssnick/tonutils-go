package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return INDEX2(0, 0) },
		func() vm.OP { return INDEX3(0, 0, 0) },
	)
}

func INDEX2(i, j uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6fb, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("INDEX2 %d,%d", i, j)
		},
		SerializeSuffix: func() *cell.Builder {
			value := (uint64(i&3) << 2) | uint64(j&3)
			return cell.BeginCell().MustStoreUInt(value, 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8((val >> 2) & 3)
			j = uint8(val & 3)
			return nil
		},
		Action: func(state *vm.State) error {
			return execIndexMulti(state, []int{int(i), int(j)})
		},
	}
}

func INDEX3(i, j, k uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6fc>>2, 10).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("INDEX3 %d,%d,%d", i, j, k)
		},
		SerializeSuffix: func() *cell.Builder {
			value := (uint64(i&3) << 4) | (uint64(j&3) << 2) | uint64(k&3)
			return cell.BeginCell().MustStoreUInt(value, 6)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(6)
			if err != nil {
				return err
			}
			i = uint8((val >> 4) & 3)
			j = uint8((val >> 2) & 3)
			k = uint8(val & 3)
			return nil
		},
		Action: func(state *vm.State) error {
			return execIndexMulti(state, []int{int(i), int(j), int(k)})
		},
	}
}

func execIndexMulti(state *vm.State, indices []int) error {
	tup, err := state.Stack.PopTupleRange(255)
	if err != nil {
		return err
	}

	current := tup
	for idx := 0; idx < len(indices)-1; idx++ {
		val, err := current.Index(indices[idx])
		if err != nil {
			return err
		}
		nested, ok := val.(tuplepkg.Tuple)
		if !ok {
			return vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a tuple")
		}
		if nested.Len() > 255 {
			return vmerr.Error(vmerr.CodeTypeCheck, "not a tuple of valid size")
		}
		current = nested
	}

	val, err := current.Index(indices[len(indices)-1])
	if err != nil {
		return err
	}

	return state.Stack.PushAny(val)
}
