package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, blkDrop2Op)
}

func blkdrop2Prefixes() []helpers.BitPrefix {
	prefixes := make([]helpers.BitPrefix, 15)
	for i := uint64(1); i <= 15; i++ {
		prefixes[i-1] = helpers.UIntPrefix((0x6C<<4)|i, 12)
	}
	return prefixes
}

// constant prefixes, computed once instead of on every decode
var blkdrop2PrefixList = blkdrop2Prefixes()

// The first count is part of the registered prefix — a zero there is not this
// opcode — so only the second one follows it, but both travel in the operand.
var blkDrop2Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.NewPrefixed(blkdrop2PrefixList...),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		i, j := unpackArgs2(args)
		return state.Stack.DropMany(i, j)
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(8); err != nil {
			return 0, err
		}
		args, err := code.LoadUInt(8)
		if err != nil {
			return 0, err
		}
		if args>>4 == 0 {
			return 0, vm.ErrCorruptedOpcode
		}
		return args, nil
	},
	Serializer: func(args uint64) *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0x6C, 8).MustStoreUInt(args, 8)
	},
	Name: func(args uint64) string {
		i, j := unpackArgs2(args)
		return fmt.Sprintf("%d,%d BLKDROP2", i, j)
	},
})

func BLKDROP2(i, j uint8) vm.OP {
	return vm.Bind(blkDrop2Op, packArgs2(i, j))
}
