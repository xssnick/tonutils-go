package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return XC2PU(0, 0, 0) })
	vm.List = append(vm.List, func() vm.OP { return XCPUXC(0, 0, 0) })
	vm.List = append(vm.List, func() vm.OP { return XCPU2(0, 0, 0) })
	vm.List = append(vm.List, func() vm.OP { return PUXC2(0, 0, 0) })
	vm.List = append(vm.List, func() vm.OP { return PUXCPU(0, 0, 0) })
	vm.List = append(vm.List, func() vm.OP { return PU2XC(0, 0, 0) })
	vm.List = append(vm.List, func() vm.OP { return PUSH3(0, 0, 0) })
}

func XC2PU(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.Exchange(1, int(i)); err != nil {
				return err
			}
			if err := state.Stack.Exchange(0, int(j)); err != nil {
				return err
			}
			val, err := state.Stack.Get(int(k))
			if err != nil {
				return err
			}
			return state.Stack.PushAny(val)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d XC2PU", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x541, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}

func XCPUXC(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.Exchange(1, int(i)); err != nil {
				return err
			}
			val, err := state.Stack.Get(int(j))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			if err := state.Stack.Exchange(0, 1); err != nil {
				return err
			}
			return state.Stack.Exchange(0, int(k))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d XCPUXC", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x542, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}

func XCPU2(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			if err := state.Stack.Exchange(0, int(i)); err != nil {
				return err
			}
			val, err := state.Stack.Get(int(j))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			val, err = state.Stack.Get(int(k) + 1)
			if err != nil {
				return err
			}
			return state.Stack.PushAny(val)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d XCPU2", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x543, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}

func PUXC2(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.Get(int(i))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			if err := state.Stack.Exchange(2, 0); err != nil {
				return err
			}
			if err := state.Stack.Exchange(1, int(j)); err != nil {
				return err
			}
			return state.Stack.Exchange(0, int(k))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d PUXC2", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x544, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}

func PUXCPU(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.Get(int(i))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			if err := state.Stack.Exchange(0, 1); err != nil {
				return err
			}
			if err := state.Stack.Exchange(0, int(j)); err != nil {
				return err
			}
			val, err = state.Stack.Get(int(k))
			if err != nil {
				return err
			}
			return state.Stack.PushAny(val)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d PUXCPU", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x545, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}

func PU2XC(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.Get(int(i))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			if err := state.Stack.Exchange(1, 0); err != nil {
				return err
			}
			val, err = state.Stack.Get(int(j))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			if err := state.Stack.Exchange(1, 0); err != nil {
				return err
			}
			return state.Stack.Exchange(0, int(k))
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d PU2XC", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x546, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}

func PUSH3(i, j, k uint8) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		Action: func(state *vm.State) error {
			val, err := state.Stack.Get(int(i))
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			val, err = state.Stack.Get(int(j) + 1)
			if err != nil {
				return err
			}
			if err := state.Stack.PushAny(val); err != nil {
				return err
			}
			val, err = state.Stack.Get(int(k) + 2)
			if err != nil {
				return err
			}
			return state.Stack.PushAny(val)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d,%d,%d PUSH3", i, j, k)
		},
		Prefix: cell.BeginCell().MustStoreUInt(0x547, 12).EndCell(),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(i), 4).
				MustStoreUInt(uint64(j), 4).
				MustStoreUInt(uint64(k), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			ival, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			jval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			kval, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			i = uint8(ival)
			j = uint8(jval)
			k = uint8(kval)
			return nil
		},
	}
	return op
}
