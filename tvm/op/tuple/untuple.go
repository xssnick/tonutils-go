package tuple

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return UNTUPLE(0) },
		func() vm.OP { return UNPACKFIRST(0) },
	)
}

func UNTUPLE(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f2, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d UNTUPLE", n)
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
			return execUntuple(state, int(n), true)
		},
	}
}

func UNPACKFIRST(n uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x6f3, 12).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%d UNPACKFIRST", n)
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
			return execUntuple(state, int(n), false)
		},
	}
}

func execUntuple(state *vm.State, count int, exact bool) error {
	max := 255
	min := 0
	if exact {
		max = count
		min = count
	} else {
		min = count
	}

	tup, err := state.Stack.PopTupleRange(max, min)
	if err != nil {
		return err
	}

	limit := count
	if !exact && limit > tup.Len() {
		limit = tup.Len()
	}

	for i := 0; i < limit; i++ {
		val, err := tup.Index(i)
		if err != nil {
			return err
		}
		if err = state.Stack.PushAny(val); err != nil {
			return err
		}
	}

	return nil
}
