package exec

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return RET() })
	vm.List = append(vm.List, func() vm.OP { return RETARGS(0) })
	vm.List = append(vm.List, func() vm.OP { return RETBOOL() })
	vm.List = append(vm.List, func() vm.OP { return RETDATA() })
	vm.List = append(vm.List, func() vm.OP { return IFRET() })
	vm.List = append(vm.List, func() vm.OP { return IFRETALT() })
	vm.List = append(vm.List, func() vm.OP { return IFNOTRETALT() })
}

func RET() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return state.Return()
		},
		Name:      "RET",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x30),
	}
}

func RETARGS(params int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		FixedSizeBits: 4,
		Action: func(state *vm.State) error {
			return state.Return(params)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("RETARGS %d", params)
		},
		BitPrefix: helpers.SlicePrefix(12, []byte{0xDB, 0x20}),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(params), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			params = int(val)
			return nil
		},
	}
}

func RETBOOL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cond, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			if cond {
				return state.Return()
			}
			return state.ReturnAlt()
		},
		Name:      "RETBOOL",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x32),
	}
}

func RETDATA() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if err := pushCurrentCode(state); err != nil {
				return err
			}
			return state.Return()
		},
		Name:      "RETDATA",
		BitPrefix: helpers.BytesPrefix(0xDB, 0x3F),
	}
}

func IFRET() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cond, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			if cond {
				return state.Return()
			}
			return nil
		},
		Name:      "IFRET",
		BitPrefix: helpers.BytesPrefix(0xDC),
	}
}

func IFRETALT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cond, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			if cond {
				return state.ReturnAlt()
			}
			return nil
		},
		Name:      "IFRETALT",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x08),
	}
}

func IFNOTRETALT() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cond, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			if !cond {
				return state.ReturnAlt()
			}
			return nil
		},
		Name:      "IFNOTRETALT",
		BitPrefix: helpers.BytesPrefix(0xE3, 0x09),
	}
}
