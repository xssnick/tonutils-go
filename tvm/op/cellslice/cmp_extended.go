package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return SDLEXCMP() },
		func() vm.OP { return SDPFX() },
		func() vm.OP { return SDPFXREV() },
		func() vm.OP { return SDPPFX() },
		func() vm.OP { return SDPPFXREV() },
		func() vm.OP { return SDSFX() },
		func() vm.OP { return SDSFXREV() },
		func() vm.OP { return SDPSFX() },
		func() vm.OP { return SDPSFXREV() },
	)
}

func binarySliceBoolOp(name string, prefix helpers.BitPrefix, fn func(a, b *cell.Slice) bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: name,
		Action: func(state *vm.State) error {
			cs2, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			cs1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			return state.Stack.PushBool(fn(cs1, cs2))
		},
		BitPrefix: prefix,
	}
}

func binarySliceIntOp(name string, prefix helpers.BitPrefix, fn func(a, b *cell.Slice) int64) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: name,
		Action: func(state *vm.State) error {
			cs2, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			cs1, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			return pushInt64(state, fn(cs1, cs2))
		},
		BitPrefix: prefix,
	}
}

func SDLEXCMP() *helpers.SimpleOP {
	return binarySliceIntOp("SDLEXCMP", helpers.BytesPrefix(0xC7, 0x04), func(a, b *cell.Slice) int64 {
		return int64(a.LexCompare(b))
	})
}

func SDPFX() *helpers.SimpleOP {
	return binarySliceBoolOp("SDPFX", helpers.BytesPrefix(0xC7, 0x08), func(a, b *cell.Slice) bool {
		return a.IsPrefixOf(b)
	})
}

func SDPFXREV() *helpers.SimpleOP {
	return binarySliceBoolOp("SDPFXREV", helpers.BytesPrefix(0xC7, 0x09), func(a, b *cell.Slice) bool {
		return b.IsPrefixOf(a)
	})
}

func SDPPFX() *helpers.SimpleOP {
	return binarySliceBoolOp("SDPPFX", helpers.BytesPrefix(0xC7, 0x0A), func(a, b *cell.Slice) bool {
		return a.IsProperPrefixOf(b)
	})
}

func SDPPFXREV() *helpers.SimpleOP {
	return binarySliceBoolOp("SDPPFXREV", helpers.BytesPrefix(0xC7, 0x0B), func(a, b *cell.Slice) bool {
		return b.IsProperPrefixOf(a)
	})
}

func SDSFX() *helpers.SimpleOP {
	return binarySliceBoolOp("SDSFX", helpers.BytesPrefix(0xC7, 0x0C), func(a, b *cell.Slice) bool {
		return a.IsSuffixOf(b)
	})
}

func SDSFXREV() *helpers.SimpleOP {
	return binarySliceBoolOp("SDSFXREV", helpers.BytesPrefix(0xC7, 0x0D), func(a, b *cell.Slice) bool {
		return b.IsSuffixOf(a)
	})
}

func SDPSFX() *helpers.SimpleOP {
	return binarySliceBoolOp("SDPSFX", helpers.BytesPrefix(0xC7, 0x0E), func(a, b *cell.Slice) bool {
		return a.IsProperSuffixOf(b)
	})
}

func SDPSFXREV() *helpers.SimpleOP {
	return binarySliceBoolOp("SDPSFXREV", helpers.BytesPrefix(0xC7, 0x0F), func(a, b *cell.Slice) bool {
		return b.IsProperSuffixOf(a)
	})
}
