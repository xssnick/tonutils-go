package exec

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return callDictShort(0) },
		func() vm.OP { return callDictLong(0) },
		func() vm.OP { return jmpDict(0) },
		func() vm.OP { return prepareDict(0) },
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
	if state.Reg.C[3] == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return state.Reg.C[3], nil
}

func callDictShort(id int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			if err := state.Stack.PushInt(big.NewInt(int64(id))); err != nil {
				return err
			}
			cont, err := currentCodeDict(state)
			if err != nil {
				return err
			}
			return state.Call(cont)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("CALLDICT %d", id)
		},
		BitPrefix: helpers.BytesPrefix(0xF0),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(id), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			id = int(val)
			return nil
		},
	}
}

func callDictLong(id int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 14,
		Action: func(state *vm.State) error {
			if err := state.Stack.PushInt(big.NewInt(int64(id))); err != nil {
				return err
			}
			cont, err := currentCodeDict(state)
			if err != nil {
				return err
			}
			return state.Call(cont)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("CALLDICT %d", id)
		},
		BitPrefix: helpers.SlicePrefix(10, []byte{0xF1, 0x00}),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(id), 14)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(14)
			if err != nil {
				return err
			}
			id = int(val)
			return nil
		},
	}
}

func jmpDict(id int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 14,
		Action: func(state *vm.State) error {
			if err := state.Stack.PushInt(big.NewInt(int64(id))); err != nil {
				return err
			}
			cont, err := currentCodeDict(state)
			if err != nil {
				return err
			}
			return state.Jump(cont)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("JMPDICT %d", id)
		},
		BitPrefix: helpers.SlicePrefix(10, []byte{0xF1, 0x40}),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(id), 14)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(14)
			if err != nil {
				return err
			}
			id = int(val)
			return nil
		},
	}
}

func prepareDict(id int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 14,
		Action: func(state *vm.State) error {
			if err := state.Stack.PushInt(big.NewInt(int64(id))); err != nil {
				return err
			}
			cont, err := currentCodeDict(state)
			if err != nil {
				return err
			}
			return state.Stack.PushContinuation(cont)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("PREPAREDICT %d", id)
		},
		BitPrefix: helpers.SlicePrefix(10, []byte{0xF1, 0x80}),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(id), 14)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(14)
			if err != nil {
				return err
			}
			id = int(val)
			return nil
		},
	}
}
