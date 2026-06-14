package cellslice

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return LDU(0) })
}

func LDU(sz uint) (op *helpers.AdvancedOP) {
	op = &helpers.AdvancedOP{
		FixedSizeBits: 8,
		Action: func(state *vm.State) error {
			s0, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}

			if sz <= 64 {
				v, err := s0.LoadUInt(sz)
				if err != nil {
					return err
				}
				if v <= 1<<63-1 {
					err = state.Stack.PushSmallInt(int64(v))
				} else {
					err = state.Stack.PushOwnedInt(new(big.Int).SetUint64(v))
				}
				if err != nil {
					return err
				}

				return state.Stack.PushOwnedSlice(s0)
			}

			i, err := s0.LoadBigUInt(sz)
			if err != nil {
				return err
			}

			err = state.Stack.PushOwnedInt(i)
			if err != nil {
				return err
			}

			return state.Stack.PushOwnedSlice(s0)
		},
		NameSerializer: func() string {
			return fmt.Sprintf("%d LDU", sz)
		},
		BitPrefix: helpers.BytesPrefix(0xD3),
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(sz-1), 8)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			sz = uint(val) + 1
			return nil
		},
	}
	return op
}
