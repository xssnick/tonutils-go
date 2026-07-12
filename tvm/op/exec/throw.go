package exec

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// static per-variant configs, computed once instead of on every decode
var (
	throwShortCfg         = newThrowFixedCfg("THROW", []byte{0xF2, 0x00}, 10, 6, 0, false)
	throwIfShortCfg       = newThrowFixedCfg("THROWIF", []byte{0xF2, 0x40}, 10, 6, 3, false)
	throwIfNotShortCfg    = newThrowFixedCfg("THROWIFNOT", []byte{0xF2, 0x80}, 10, 6, 2, false)
	throwLongCfg          = newThrowFixedCfg("THROW", []byte{0xF2, 0xC0, 0x00}, 13, 11, 0, false)
	throwArgCfg           = newThrowFixedCfg("THROWARG", []byte{0xF2, 0xC8, 0x00}, 13, 11, 0, true)
	throwIfLongCfg        = newThrowFixedCfg("THROWIF", []byte{0xF2, 0xD0, 0x00}, 13, 11, 3, false)
	throwArgIfCfg         = newThrowFixedCfg("THROWARGIF", []byte{0xF2, 0xD8, 0x00}, 13, 11, 3, true)
	throwIfNotLongCfg     = newThrowFixedCfg("THROWIFNOT", []byte{0xF2, 0xE0, 0x00}, 13, 11, 2, false)
	throwArgIfNotCfg      = newThrowFixedCfg("THROWARGIFNOT", []byte{0xF2, 0xE8, 0x00}, 13, 11, 2, true)
	throwAnyBitPrefix     = helpers.UIntPrefix(0x1E5E, 13)
	throwAnyPrefixesConst = throwAnyPrefixes()
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwShortCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwIfShortCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwIfNotShortCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwLongCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwArgCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwIfLongCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwArgIfCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwIfNotLongCfg} })
	vm.List = append(vm.List, func() vm.OP { return &opThrowFixed{cfg: throwArgIfNotCfg} })
	vm.List = append(vm.List, func() vm.OP { return newThrowAny() })
}

func throwAnyPrefixes() []helpers.BitPrefix {
	prefixes := make([]helpers.BitPrefix, 6)
	for args := uint64(0); args <= 5; args++ {
		prefixes[args] = helpers.UIntPrefix(0xF2F0|args, 16)
	}
	return prefixes
}

// throwFixedCfg is the static part of a fixed THROW* opcode variant shared by
// all decoded instances; it must never be mutated after construction.
type throwFixedCfg struct {
	name     string
	prefix   helpers.BitPrefix
	immBits  uint
	need     int
	hasCond  bool
	expected bool
	withArg  bool
}

func newThrowFixedCfg(name string, prefix []byte, prefixBits, immBits uint, mode int, withArg bool) *throwFixedCfg {
	var prefixValue uint64
	for _, b := range prefix {
		prefixValue = (prefixValue << 8) | uint64(b)
	}

	if immBits > 0 {
		if immBits >= 64 {
			panic("immBits must be less than 64")
		}
		mask := (uint64(1) << immBits) - 1
		if prefixValue&mask != 0 {
			panic(fmt.Sprintf("prefix %X for %s has non-zero immediate bits", prefixValue, name))
		}
		prefixValue >>= immBits
	}

	hasCond := mode != 0
	need := 0
	if withArg {
		need = 1
		if hasCond {
			need = 2
		}
	}

	return &throwFixedCfg{
		name:     name,
		prefix:   helpers.UIntPrefix(prefixValue, prefixBits),
		immBits:  immBits,
		need:     need,
		hasCond:  hasCond,
		expected: mode&1 == 1,
		withArg:  withArg,
	}
}

// opThrowFixed is a struct-based opcode: one allocation per executed
// instruction instead of an AdvancedOP carrying per-instance closures.
type opThrowFixed struct {
	cfg *throwFixedCfg
	exc uint64
}

func (op *opThrowFixed) GetPrefixes() []*cell.Slice {
	return helpers.PrefixSlices(op.cfg.prefix)
}

func (op *opThrowFixed) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(op.cfg.prefix.Bits); err != nil {
		return err
	}
	val, err := code.LoadUInt(op.cfg.immBits)
	if err != nil {
		return err
	}
	op.exc = val
	return nil
}

func (op *opThrowFixed) Serialize() *cell.Builder {
	return cell.BeginCell().
		MustStoreSlice(op.cfg.prefix.Data, op.cfg.prefix.Bits).
		MustStoreUInt(op.exc, op.cfg.immBits)
}

func (op *opThrowFixed) SerializeText() string {
	return fmt.Sprintf("%s %d", op.cfg.name, op.exc)
}

func (op *opThrowFixed) InstructionBits() int64 {
	return int64(op.cfg.prefix.Bits) + int64(op.cfg.immBits)
}

func (op *opThrowFixed) Interpret(state *vm.State) error {
	cfg := op.cfg

	if cfg.need > 0 {
		if state.Stack.Len() < cfg.need {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}
	}

	if cfg.hasCond {
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}

		if cfg.withArg {
			arg, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			if cond != cfg.expected {
				return nil
			}

			return state.ThrowException(big.NewInt(int64(op.exc)), arg)
		}

		if cond != cfg.expected {
			return nil
		}

		return state.ThrowException(big.NewInt(int64(op.exc)))
	}

	if cfg.withArg {
		arg, err := state.Stack.PopAny()
		if err != nil {
			return err
		}

		return state.ThrowException(big.NewInt(int64(op.exc)), arg)
	}

	return state.ThrowException(big.NewInt(int64(op.exc)))
}

func newThrowAny() *helpers.AdvancedOP {
	var args uint64
	op := &helpers.AdvancedOP{
		BitPrefix:     throwAnyBitPrefix,
		Prefixes:      throwAnyPrefixesConst,
		FixedSizeBits: 3,
		NameSerializer: func() string {
			name := "THROW"
			if args&1 != 0 {
				name += "ARG"
			}
			name += "ANY"
			if args&6 != 0 {
				if args&2 != 0 {
					name += "IF"
				} else {
					name += "IFNOT"
				}
			}
			return name
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(args, 3)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(3)
			if err != nil {
				return err
			}
			if val > 5 {
				return vm.ErrCorruptedOpcode
			}
			args = val
			return nil
		},
	}

	op.Action = func(state *vm.State) error {
		hasParam := args&1 != 0
		hasCond := args&6 != 0
		throwCond := args&2 != 0

		stack := state.Stack
		need := 1
		if hasCond {
			need++
		}
		if hasParam {
			need++
		}
		if stack.Len() < need {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}

		var cond bool
		var err error

		if hasCond {
			cond, err = stack.PopBool()
			if err != nil {
				return err
			}
		} else {
			cond = throwCond
		}

		exc, err := stack.PopIntRangeInt64(0, 0xffff)
		if err != nil {
			return err
		}

		if cond != throwCond {
			if hasParam {
				if _, err := stack.PopAny(); err != nil {
					return err
				}
			}
			return nil
		}

		if hasParam {
			arg, err := stack.PopAny()
			if err != nil {
				return err
			}
			return state.ThrowException(big.NewInt(exc), arg)
		}

		return state.ThrowException(big.NewInt(exc))
	}

	return op
}
