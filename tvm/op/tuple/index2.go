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

// constant prefixes, computed once instead of on every decode
var (
	index2Prefix = helpers.UIntPrefix(0x6fb, 12)
	index3Prefix = helpers.UIntPrefix(0x6fc>>2, 10)
)

func INDEX2(i, j uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		BitPrefix:     index2Prefix,
		FixedSizeBits: 4,
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
			return execIndex2(state, int(i), int(j))
		},
	}
}

func INDEX3(i, j, k uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		BitPrefix:     index3Prefix,
		FixedSizeBits: 6,
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
			return execIndex3(state, int(i), int(j), int(k))
		},
	}
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
	if !ok || nested.Len() > 255 {
		var zero tuplepkg.Tuple
		return zero, vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a tuple")
	}

	return nested, nil
}
