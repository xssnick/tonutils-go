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
		func() vm.OP { return LDREFRTOS() },
		func() vm.OP { return SDCUTFIRST() },
		func() vm.OP { return SDCUTLAST() },
		func() vm.OP { return SDSKIPLAST() },
		func() vm.OP { return SDSUBSTR() },
		func() vm.OP { return SCUTFIRST() },
		func() vm.OP { return SSKIPFIRST() },
		func() vm.OP { return SCUTLAST() },
		func() vm.OP { return SSKIPLAST() },
		func() vm.OP { return SUBSLICE() },
		func() vm.OP { return SPLIT() },
		func() vm.OP { return SPLITQ() },
		func() vm.OP { return XCTOS() },
		func() vm.OP { return XLOAD() },
		func() vm.OP { return XLOADQ() },
		func() vm.OP { return SCHKBITS() },
		func() vm.OP { return SCHKREFS() },
		func() vm.OP { return SCHKBITREFS() },
		func() vm.OP { return SCHKBITSQ() },
		func() vm.OP { return SCHKREFSQ() },
		func() vm.OP { return SCHKBITREFSQ() },
		func() vm.OP { return PLDREFVAR() },
		func() vm.OP { return PLDREFIDX(0) },
		func() vm.OP { return SBITS() },
		func() vm.OP { return SREFS() },
		func() vm.OP { return SBITREFS() },
		func() vm.OP { return LDZEROES() },
		func() vm.OP { return LDONES() },
		func() vm.OP { return LDSAME() },
		func() vm.OP { return SDEPTH() },
		func() vm.OP { return CDEPTH() },
		func() vm.OP { return CLEVEL() },
		func() vm.OP { return CLEVELMASK() },
	)
}

func popRange(state *vm.State, max int64) (uint64, error) {
	v, err := state.Stack.PopIntRange(0, max)
	if err != nil {
		return 0, err
	}
	return v.Uint64(), nil
}

func pushInt64(state *vm.State, v int64) error {
	return state.Stack.PushInt(big.NewInt(v))
}

func parseLoadedCellSlice(state *vm.State, cl *cell.Cell) (*cell.Slice, error) {
	return state.Cells.BeginParse(cl)
}

func beginLoadedCellSlice(state *vm.State, cl *cell.Cell) (*cell.Slice, error) {
	return state.Cells.BeginParse(cl)
}

func LDREFRTOS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			child, err := state.Cells.LoadRef(s0)
			if err != nil {
				return err
			}

			if err = state.Stack.PushSlice(s0); err != nil {
				return err
			}
			return state.Stack.PushSlice(child)
		},
		Name:      "LDREFRTOS",
		BitPrefix: helpers.BytesPrefix(0xD5),
	}
}

func SDCUTFIRST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.OnlyFirst(uint(bits), 0) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SDCUTFIRST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x20),
	}
}

func SDCUTLAST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.OnlyLast(uint(bits), 0) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SDCUTLAST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x22),
	}
}

func SDSKIPLAST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.SkipLast(uint(bits), 0) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SDSKIPLAST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x23),
	}
}

func SDSUBSTR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			offset, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.SkipFirst(uint(offset), 0) || !cs.OnlyFirst(uint(bits), 0) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SDSUBSTR",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x24),
	}
}

func SCUTFIRST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			refs, err := popRange(state, 4)
			if err != nil {
				return err
			}
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.OnlyFirst(uint(bits), int(refs)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SCUTFIRST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x30),
	}
}

func SSKIPFIRST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			refs, err := popRange(state, 4)
			if err != nil {
				return err
			}
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.SkipFirst(uint(bits), int(refs)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SSKIPFIRST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x31),
	}
}

func SCUTLAST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			refs, err := popRange(state, 4)
			if err != nil {
				return err
			}
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.OnlyLast(uint(bits), int(refs)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SCUTLAST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x32),
	}
}

func SSKIPLAST() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			refs, err := popRange(state, 4)
			if err != nil {
				return err
			}
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.SkipLast(uint(bits), int(refs)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SSKIPLAST",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x33),
	}
}

func SUBSLICE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			r2, err := popRange(state, 4)
			if err != nil {
				return err
			}
			l2, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			r1, err := popRange(state, 4)
			if err != nil {
				return err
			}
			l1, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !cs.SkipFirst(uint(l1), int(r1)) || !cs.OnlyFirst(uint(l2), int(r2)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      "SUBSLICE",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x34),
	}
}

func splitOp(quiet bool) *helpers.SimpleOP {
	name := "SPLIT"
	prefix := helpers.BytesPrefix(0xD7, 0x36)
	if quiet {
		name = "SPLITQ"
		prefix = helpers.BytesPrefix(0xD7, 0x37)
	}

	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			refs, err := popRange(state, 4)
			if err != nil {
				return err
			}
			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if cs.BitsLeft() < uint(bits) || cs.RefsNum() < int(refs) {
				if !quiet {
					return vmerr.Error(vmerr.CodeCellUnderflow)
				}
				if err = state.Stack.PushSlice(cs); err != nil {
					return err
				}
				return state.Stack.PushBool(false)
			}

			first := cs.Copy()
			if !first.OnlyFirst(uint(bits), int(refs)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if !cs.SkipFirst(uint(bits), int(refs)) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}

			if err = state.Stack.PushSlice(first); err != nil {
				return err
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

func SPLIT() *helpers.SimpleOP  { return splitOp(false) }
func SPLITQ() *helpers.SimpleOP { return splitOp(true) }

func XCTOS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			sl, special, err := state.Cells.BeginParseSpecial(cl)
			if err != nil {
				return err
			}
			if err = state.Stack.PushSlice(sl); err != nil {
				return err
			}
			return state.Stack.PushBool(special)
		},
		Name:      "XCTOS",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x39),
	}
}

func xloadOp(quiet bool) *helpers.SimpleOP {
	name := "XLOAD"
	prefix := helpers.BytesPrefix(0xD7, 0x3A)
	if quiet {
		name = "XLOADQ"
		prefix = helpers.BytesPrefix(0xD7, 0x3B)
	}

	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}

			resolved, err := state.ResolveXLoadCell(cl)
			if err != nil {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return err
			}

			if err = state.Stack.PushCell(resolved); err != nil {
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

func XLOAD() *helpers.SimpleOP  { return xloadOp(false) }
func XLOADQ() *helpers.SimpleOP { return xloadOp(true) }

func sliceCheckOp(name string, prefix helpers.BitPrefix, quiet bool, fn func(*vm.State) (bool, error)) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			ok, err := fn(state)
			if err != nil {
				return err
			}
			if quiet {
				return state.Stack.PushBool(ok)
			}
			if !ok {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func SCHKBITS() *helpers.SimpleOP {
	return sliceCheckOp("SCHKBITS", helpers.BytesPrefix(0xD7, 0x41), false, func(state *vm.State) (bool, error) {
		bits, err := popRange(state, 1023)
		if err != nil {
			return false, err
		}
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.BitsLeft() >= uint(bits), nil
	})
}

func SCHKREFS() *helpers.SimpleOP {
	return sliceCheckOp("SCHKREFS", helpers.BytesPrefix(0xD7, 0x42), false, func(state *vm.State) (bool, error) {
		refs, err := popRange(state, 1023)
		if err != nil {
			return false, err
		}
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.RefsNum() >= int(refs), nil
	})
}

func SCHKBITREFS() *helpers.SimpleOP {
	return sliceCheckOp("SCHKBITREFS", helpers.BytesPrefix(0xD7, 0x43), false, func(state *vm.State) (bool, error) {
		refs, err := popRange(state, 4)
		if err != nil {
			return false, err
		}
		bits, err := popRange(state, 1023)
		if err != nil {
			return false, err
		}
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.BitsLeft() >= uint(bits) && cs.RefsNum() >= int(refs), nil
	})
}

func SCHKBITSQ() *helpers.SimpleOP {
	return sliceCheckOp("SCHKBITSQ", helpers.BytesPrefix(0xD7, 0x45), true, func(state *vm.State) (bool, error) {
		bits, err := popRange(state, 1023)
		if err != nil {
			return false, err
		}
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.BitsLeft() >= uint(bits), nil
	})
}

func SCHKREFSQ() *helpers.SimpleOP {
	return sliceCheckOp("SCHKREFSQ", helpers.BytesPrefix(0xD7, 0x46), true, func(state *vm.State) (bool, error) {
		refs, err := popRange(state, 1023)
		if err != nil {
			return false, err
		}
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.RefsNum() >= int(refs), nil
	})
}

func SCHKBITREFSQ() *helpers.SimpleOP {
	return sliceCheckOp("SCHKBITREFSQ", helpers.BytesPrefix(0xD7, 0x47), true, func(state *vm.State) (bool, error) {
		refs, err := popRange(state, 4)
		if err != nil {
			return false, err
		}
		bits, err := popRange(state, 1023)
		if err != nil {
			return false, err
		}
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.BitsLeft() >= uint(bits) && cs.RefsNum() >= int(refs), nil
	})
}

func PLDREFVAR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			idx, err := popRange(state, 3)
			if err != nil {
				return err
			}
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			ref, err := cs.PeekRefCellAt(int(idx))
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushCell(ref)
		},
		Name:      "PLDREFVAR",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x48),
	}
}

func PLDREFIDX(idx int) *helpers.AdvancedOP {
	op := &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("PLDREFIDX %d", idx)
		},
		BitPrefix:     helpers.UIntPrefix(0xD74C>>2, 14),
		FixedSizeBits: 2,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx), 2)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(2)
			if err != nil {
				return err
			}
			idx = int(v)
			return nil
		},
		Action: func(state *vm.State) error {
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			ref, err := cs.PeekRefCellAt(idx)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			return state.Stack.PushCell(ref)
		},
	}
	return op
}

func SBITS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cs.BitsLeft()))
		},
		Name:      "SBITS",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x49),
	}
}

func SREFS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cs.RefsNum()))
		},
		Name:      "SREFS",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x4A),
	}
}

func SBITREFS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if err = pushInt64(state, int64(cs.BitsLeft())); err != nil {
				return err
			}
			return pushInt64(state, int64(cs.RefsNum()))
		},
		Name:      "SBITREFS",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x4B),
	}
}

func ldSameOp(name string, prefix helpers.BitPrefix, fixed *bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			var bit bool
			if fixed == nil {
				v, err := popRange(state, 1)
				if err != nil {
					return err
				}
				bit = v == 1
			} else {
				bit = *fixed
			}

			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			count := cs.CountLeading(bit)
			if count > 0 && !cs.SkipFirst(uint(count), 0) {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if err = pushInt64(state, int64(count)); err != nil {
				return err
			}
			return state.Stack.PushSlice(cs)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func LDZEROES() *helpers.SimpleOP {
	v := false
	return ldSameOp("LDZEROES", helpers.BytesPrefix(0xD7, 0x60), &v)
}

func LDONES() *helpers.SimpleOP {
	v := true
	return ldSameOp("LDONES", helpers.BytesPrefix(0xD7, 0x61), &v)
}

func LDSAME() *helpers.SimpleOP {
	return ldSameOp("LDSAME", helpers.BytesPrefix(0xD7, 0x62), nil)
}

func SDEPTH() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cs.Depth()))
		},
		Name:      "SDEPTH",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x64),
	}
}

func CDEPTH() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopMaybeCell()
			if err != nil {
				return err
			}
			if cl == nil {
				return pushInt64(state, 0)
			}
			return pushInt64(state, int64(cl.Depth()))
		},
		Name:      "CDEPTH",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x65),
	}
}

func CLEVEL() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cl.Level()))
		},
		Name:      "CLEVEL",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x66),
	}
}

func CLEVELMASK() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return pushInt64(state, int64(cl.LevelMask().Mask))
		},
		Name:      "CLEVELMASK",
		BitPrefix: helpers.BytesPrefix(0xD7, 0x67),
	}
}
