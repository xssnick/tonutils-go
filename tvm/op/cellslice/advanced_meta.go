package cellslice

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return SDBEGINSX() },
		func() vm.OP { return SDBEGINSXQ() },
		func() vm.OP { return CHASHIX() },
		func() vm.OP { return CDEPTHIX() },
	)
	vm.ArgList = append(vm.ArgList, chashiOp, cdepthiOp)
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
			if err := checkStackDepth(state, 2); err != nil {
				return err
			}

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
				if err = state.Stack.PushOwnedSlice(cs); err != nil {
					return err
				}
				return state.Stack.PushBool(false)
			}
			if err = cs.SkipBits(needle.BitsLeft()); err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if err = state.Stack.PushOwnedSlice(cs); err != nil {
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

var chashiOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed:   helpers.SinglePrefixed(helpers.UIntPrefix(0xD768>>2, 14)),
	ArgBits:    2,
	MinVersion: 6,
	Name: func(args uint64) string {
		return fmt.Sprintf("CHASHI %d", int(args))
	},
	Action: func(state *vm.State, args uint64) error {
		cl, err := state.Stack.PopCell()
		if err != nil {
			return err
		}
		hash := cl.HashKeyAt(int(args))
		return state.Stack.PushOwnedInt(new(big.Int).SetBytes(hash[:]))
	},
})

var cdepthiOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed:   helpers.SinglePrefixed(helpers.UIntPrefix(0xD76C>>2, 14)),
	ArgBits:    2,
	MinVersion: 6,
	Name: func(args uint64) string {
		return fmt.Sprintf("CDEPTHI %d", int(args))
	},
	Action: func(state *vm.State, args uint64) error {
		cl, err := state.Stack.PopCell()
		if err != nil {
			return err
		}
		return pushSmallInt(state, int64(cl.Depth(int(args))))
	},
})

func CHASHI(i int) vm.OP { return vm.Bind(chashiOp, uint64(i)) }

func CDEPTHI(i int) vm.OP { return vm.Bind(cdepthiOp, uint64(i)) }

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
			return state.Stack.PushOwnedInt(new(big.Int).SetBytes(cl.Hash(int(idx))))
		},
		Name:       "CHASHIX",
		BitPrefix:  helpers.BytesPrefix(0xD7, 0x70),
		MinVersion: 6,
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
			return pushSmallInt(state, int64(cl.Depth(int(idx))))
		},
		Name:       "CDEPTHIX",
		BitPrefix:  helpers.BytesPrefix(0xD7, 0x71),
		MinVersion: 6,
	}
}
