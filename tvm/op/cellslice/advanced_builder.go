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
		func() vm.OP { return STBREF() },
		func() vm.OP { return STBREFR() },
		func() vm.OP { return STREFR() },
		func() vm.OP { return STSLICER() },
		func() vm.OP { return STBR() },
		func() vm.OP { return STREFQ() },
		func() vm.OP { return STBREFQ() },
		func() vm.OP { return STSLICEQ() },
		func() vm.OP { return STBQ() },
		func() vm.OP { return STREFRQ() },
		func() vm.OP { return STBREFRQ() },
		func() vm.OP { return STSLICERQ() },
		func() vm.OP { return STBRQ() },
		func() vm.OP { return ENDXC() },
		func() vm.OP { return BDEPTH() },
		func() vm.OP { return BBITS() },
		func() vm.OP { return BREFS() },
		func() vm.OP { return BBITREFS() },
		func() vm.OP { return BCHKBITSIMM(1, false) },
		func() vm.OP { return BCHKBITS() },
		func() vm.OP { return BCHKREFS() },
		func() vm.OP { return BCHKBITREFS() },
		func() vm.OP { return BCHKBITSIMM(1, true) },
		func() vm.OP { return BCHKBITSQ() },
		func() vm.OP { return BCHKREFSQ() },
		func() vm.OP { return BCHKBITREFSQ() },
		func() vm.OP { return STZEROES() },
		func() vm.OP { return STONES() },
		func() vm.OP { return STSAME() },
		func() vm.OP { return BTOS() },
	)
}

func pushBuilderInt(state *vm.State, v int64) error {
	return state.Stack.PushInt(big.NewInt(v))
}

func pushStoreQuietStatus(state *vm.State, failed bool) error {
	if failed {
		return pushBuilderInt(state, -1)
	}
	return pushBuilderInt(state, 0)
}

func STBREF() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = dst.StoreRef(src.EndCell()); err != nil {
				return err
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      "STBREF",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x11),
	}
}

func STBREFR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = dst.StoreRef(src.EndCell()); err != nil {
				return err
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      "STBREFR",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x15),
	}
}

func STREFR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = dst.StoreRef(cl); err != nil {
				return err
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      "STREFR",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x14),
	}
}

func STREFQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				if err = state.Stack.PushCell(cl); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreRef(cl); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STREFQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x18),
	}
}

func STBREFQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				if err = state.Stack.PushBuilder(src); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreRef(src.EndCell()); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STBREFQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x19),
	}
}

func STSLICEQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			sl, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(sl.BitsLeft(), uint(sl.RefsNum())) {
				if err = state.Stack.PushSlice(sl); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreBuilder(sl.ToBuilder()); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STSLICEQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x1A),
	}
}

func STBQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(src.BitsUsed(), uint(src.RefsUsed())) {
				if err = state.Stack.PushBuilder(src); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreBuilder(src); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STBQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x1B),
	}
}

func STSLICER() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			sl, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(sl.BitsLeft(), uint(sl.RefsNum())) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = dst.StoreBuilder(sl.ToBuilder()); err != nil {
				return err
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      "STSLICER",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x16),
	}
}

func STBR() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(src.BitsUsed(), uint(src.RefsUsed())) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = dst.StoreBuilder(src); err != nil {
				return err
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      "STBR",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x17),
	}
}

func STBREFRQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(src); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreRef(src.EndCell()); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STBREFRQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x1D),
	}
}

func STREFRQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			cl, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(0, 1) {
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				if err = state.Stack.PushCell(cl); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreRef(cl); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STREFRQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x1C),
	}
}

func STBRQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(src.BitsUsed(), uint(src.RefsUsed())) {
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(src); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreBuilder(src); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STBRQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x1F),
	}
}

func STSLICERQ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			sl, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !dst.CanExtendBy(sl.BitsLeft(), uint(sl.RefsNum())) {
				if err = state.Stack.PushBuilder(dst); err != nil {
					return err
				}
				if err = state.Stack.PushSlice(sl); err != nil {
					return err
				}
				return pushStoreQuietStatus(state, true)
			}
			if err = dst.StoreBuilder(sl.ToBuilder()); err != nil {
				return err
			}
			if err = state.Stack.PushBuilder(dst); err != nil {
				return err
			}
			return pushStoreQuietStatus(state, false)
		},
		Name:      "STSLICERQ",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x1E),
	}
}

func ENDXC() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			special, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			cl, err := builder.EndCellSpecial(special)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellOverflow, err.Error())
			}
			return state.Stack.PushCell(cl)
		},
		Name:      "ENDXC",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x23),
	}
}

func BDEPTH() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.Depth()))
		},
		Name:      "BDEPTH",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x30),
	}
}

func BBITS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.BitsUsed()))
		},
		Name:      "BBITS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x31),
	}
}

func BREFS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.RefsUsed()))
		},
		Name:      "BREFS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x32),
	}
}

func BBITREFS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if err = pushBuilderInt(state, int64(builder.BitsUsed())); err != nil {
				return err
			}
			return pushBuilderInt(state, int64(builder.RefsUsed()))
		},
		Name:      "BBITREFS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x33),
	}
}

func BCHKBITSIMM(bits uint, quiet bool) *helpers.AdvancedOP {
	name := "BCHKBITS"
	prefix := helpers.BytesPrefix(0xCF, 0x38)
	if quiet {
		name = "BCHKBITSQ"
		prefix = helpers.BytesPrefix(0xCF, 0x3C)
	}

	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("%s %d", name, bits)
		},
		BitPrefix:     prefix,
		FixedSizeBits: 8,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(bits-1), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			bits = uint(v + 1)
			return nil
		},
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			ok := builder.CanExtendBy(bits, 0)
			if quiet {
				return state.Stack.PushBool(ok)
			}
			if !ok {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			return nil
		},
	}
}

func bchkOp(name string, prefix helpers.BitPrefix, needBits, needRefs, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			var refs uint64
			var bits uint64
			var err error

			if needRefs {
				refs, err = popRange(state, 7)
				if err != nil {
					return err
				}
			}
			if needBits {
				bits, err = popRange(state, 1023)
				if err != nil {
					return err
				}
			}

			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			ok := builder.CanExtendBy(uint(bits), uint(refs))
			if quiet {
				return state.Stack.PushBool(ok)
			}
			if !ok {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func BCHKBITS() *helpers.SimpleOP {
	return bchkOp("BCHKBITS", helpers.BytesPrefix(0xCF, 0x39), true, false, false)
}
func BCHKREFS() *helpers.SimpleOP {
	return bchkOp("BCHKREFS", helpers.BytesPrefix(0xCF, 0x3A), false, true, false)
}
func BCHKBITREFS() *helpers.SimpleOP {
	return bchkOp("BCHKBITREFS", helpers.BytesPrefix(0xCF, 0x3B), true, true, false)
}
func BCHKBITSQ() *helpers.SimpleOP {
	return bchkOp("BCHKBITSQ", helpers.BytesPrefix(0xCF, 0x3D), true, false, true)
}
func BCHKREFSQ() *helpers.SimpleOP {
	return bchkOp("BCHKREFSQ", helpers.BytesPrefix(0xCF, 0x3E), false, true, true)
}
func BCHKBITREFSQ() *helpers.SimpleOP {
	return bchkOp("BCHKBITREFSQ", helpers.BytesPrefix(0xCF, 0x3F), true, true, true)
}

func sameStoreOp(name string, prefix helpers.BitPrefix, fixed *bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			var bit bool
			var err error
			if fixed == nil {
				v, err := popRange(state, 1)
				if err != nil {
					return err
				}
				bit = v == 1
			} else {
				bit = *fixed
			}

			bits, err := popRange(state, 1023)
			if err != nil {
				return err
			}
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if !builder.CanExtendBy(uint(bits), 0) {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			if err = builder.StoreSameBit(bit, uint(bits)); err != nil {
				return err
			}
			return state.Stack.PushBuilder(builder)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func STZEROES() *helpers.SimpleOP {
	v := false
	return sameStoreOp("STZEROES", helpers.BytesPrefix(0xCF, 0x40), &v)
}

func STONES() *helpers.SimpleOP {
	v := true
	return sameStoreOp("STONES", helpers.BytesPrefix(0xCF, 0x41), &v)
}

func STSAME() *helpers.SimpleOP {
	return sameStoreOp("STSAME", helpers.BytesPrefix(0xCF, 0x42), nil)
}

func BTOS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			return state.Stack.PushSlice(builder.ToSlice())
		},
		Name:      "BTOS",
		BitPrefix: helpers.BytesPrefix(0xCF, 0x50),
	}
}
