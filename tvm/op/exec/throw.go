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
	vm.ArgList = append(vm.ArgList,
		newThrowFixedOp(throwShortCfg),
		newThrowFixedOp(throwIfShortCfg),
		newThrowFixedOp(throwIfNotShortCfg),
		newThrowFixedOp(throwLongCfg),
		newThrowFixedOp(throwArgCfg),
		newThrowFixedOp(throwIfLongCfg),
		newThrowFixedOp(throwArgIfCfg),
		newThrowFixedOp(throwIfNotLongCfg),
		newThrowFixedOp(throwArgIfNotCfg),
		throwAnyOp,
	)
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

func newThrowFixedOp(cfg *throwFixedCfg) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(cfg.prefix),
		ArgBits:  cfg.immBits,
		Action: func(state *vm.State, args uint64) error {
			return throwFixed(state, cfg, args)
		},
		Name: func(args uint64) string {
			return fmt.Sprintf("%s %d", cfg.name, args)
		},
	})
}

func throwFixed(state *vm.State, cfg *throwFixedCfg, exc uint64) error {
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

			return state.ThrowException(big.NewInt(int64(exc)), arg)
		}

		if cond != cfg.expected {
			return nil
		}

		return state.ThrowException(big.NewInt(int64(exc)))
	}

	if cfg.withArg {
		arg, err := state.Stack.PopAny()
		if err != nil {
			return err
		}

		return state.ThrowException(big.NewInt(int64(exc)), arg)
	}

	return state.ThrowException(big.NewInt(int64(exc)))
}

// throwAnyOp is dispatched on 16-bit prefixes that already carry the 3-bit mode
// — the six defined modes are the only ones that reach it — while the
// instruction is a 13-bit opcode plus that mode.
var throwAnyOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.NewPrefixed(throwAnyPrefixesConst...),
	Action: func(state *vm.State, args uint64) error {
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
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(throwAnyBitPrefix.Bits); err != nil {
			return 0, err
		}
		val, err := code.LoadUInt(3)
		if err != nil {
			return 0, err
		}
		if val > 5 {
			return 0, vm.ErrCorruptedOpcode
		}
		return val, nil
	},
	Serializer: func(args uint64) *cell.Builder {
		return cell.BeginCell().
			MustStoreSlice(throwAnyBitPrefix.Data, throwAnyBitPrefix.Bits).
			MustStoreUInt(args, 3)
	},
	Name: func(args uint64) string {
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
})
