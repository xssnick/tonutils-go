package cellslice

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
		func() vm.OP { return SDBEGINSX() },
		func() vm.OP { return SDBEGINSXQ() },
		func() vm.OP { return CHASHI(0) },
		func() vm.OP { return CDEPTHI(0) },
		func() vm.OP { return CHASHIX() },
		func() vm.OP { return CDEPTHIX() },
	)
}

func sdbeginsXOp(quiet bool) *helpers.SimpleOP {
	name := "SDBEGINSX"
	prefix := helpers.BytesPrefix(0xD7, 0x26)
	if quiet {
		name = "SDBEGINSXQ"
		prefix = helpers.BytesPrefix(0xD7, 0x27)
	}

	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			needle, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.HasPrefix(needle) {
				if !quiet {
					return vmerr.Error(vmerr.CodeCellUnderflow, "slice does not begin with expected data bits")
				}
				if err = state.Stack.PushSlice(cs); err != nil {
					return err
				}
				return state.Stack.PushBool(false)
			}
			if err = cs.Advance(needle.BitsLeft()); err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if err = state.Stack.PushSlice(cs); err != nil {
				return err
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func SDBEGINSX() *helpers.SimpleOP  { return sdbeginsXOp(false) }
func SDBEGINSXQ() *helpers.SimpleOP { return sdbeginsXOp(true) }

func CHASHI(i int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("CHASHI %d", i)
		},
		BitPrefix:     helpers.UIntPrefix(0xD768>>2, 14),
		FixedSizeBits: 2,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 2)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(2)
			if err != nil {
				return err
			}
			i = int(v)
			return nil
		},
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return state.Stack.PushInt(new(big.Int).SetBytes(cl.Hash(i)))
		},
	}
}

func CDEPTHI(i int) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("CDEPTHI %d", i)
		},
		BitPrefix:     helpers.UIntPrefix(0xD76C>>2, 14),
		FixedSizeBits: 2,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(i), 2)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(2)
			if err != nil {
				return err
			}
			i = int(v)
			return nil
		},
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cl.Depth(i)))
		},
	}
}

func CHASHIX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := popRange(state, 3)
			if err != nil {
				return err
			}
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return state.Stack.PushInt(new(big.Int).SetBytes(cl.Hash(int(idx))))
		},
		Name:      "CHASHIX",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x70),
	}
}

func CDEPTHIX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := popRange(state, 3)
			if err != nil {
				return err
			}
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cl.Depth(int(idx))))
		},
		Name:      "CDEPTHIX",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x71),
	}
}
