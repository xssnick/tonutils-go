package exec

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROW", []byte{0xF2, 0x00}, 10, 6, 0, false) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWIF", []byte{0xF2, 0x40}, 10, 6, 3, false) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWIFNOT", []byte{0xF2, 0x80}, 10, 6, 2, false) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROW", []byte{0xF2, 0xC0, 0x00}, 13, 11, 0, false) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWARG", []byte{0xF2, 0xC8, 0x00}, 13, 11, 0, true) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWIF", []byte{0xF2, 0xD0, 0x00}, 13, 11, 3, false) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWARGIF", []byte{0xF2, 0xD8, 0x00}, 13, 11, 3, true) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWIFNOT", []byte{0xF2, 0xE0, 0x00}, 13, 11, 2, false) })
	vm.List = append(vm.List, func() vm.OP { return newThrowFixed("THROWARGIFNOT", []byte{0xF2, 0xE8, 0x00}, 13, 11, 2, true) })
	vm.List = append(vm.List, func() vm.OP { return newThrowAny() })
}

func newThrowFixed(name string, prefix []byte, prefixBits, immBits uint, mode int, withArg bool) *helpers.AdvancedOP {
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

	var exc uint64
	op := &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(prefixValue, prefixBits).EndCell(),
		NameSerializer: func() string {
			return fmt.Sprintf("%s %d", name, exc)
		},
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(exc, immBits)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(immBits)
			if err != nil {
				return err
			}
			exc = val
			return nil
		},
	}

	op.Action = func(state *vm.State) error {
		exception := big.NewInt(int64(exc))
		expected := mode&1 == 1

		if mode != 0 {
			cond, err := state.Stack.PopBool()
			if err != nil {
				return err
			}

			if withArg {
				arg, err := state.Stack.PopAny()
				if err != nil {
					return err
				}

				if cond != expected {
					return nil
				}

				return state.ThrowException(exception, arg)
			}

			if cond != expected {
				return nil
			}

			return state.ThrowException(exception)
		}

		if withArg {
			arg, err := state.Stack.PopAny()
			if err != nil {
				return err
			}

			return state.ThrowException(exception, arg)
		}

		return state.ThrowException(exception)
	}

	return op
}

func newThrowAny() *helpers.AdvancedOP {
	var args uint64
	op := &helpers.AdvancedOP{
		Prefix: cell.BeginCell().MustStoreUInt(0x1E5E, 13).EndCell(),
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
			args = val
			return nil
		},
	}

	op.Action = func(state *vm.State) error {
		hasParam := args&1 != 0
		hasCond := args&6 != 0
		throwCond := args&2 != 0

		stack := state.Stack
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

		exc, err := stack.PopIntRange(0, 0xffff)
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
			return state.ThrowException(exc, arg)
		}

		return state.ThrowException(exc)
	}

	return op
}
