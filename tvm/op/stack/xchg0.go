package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.ArgList = append(vm.ArgList, xchg0Op)
}

// Besides the nibble-wide prefix, every index from 2 up is registered as a whole
// byte, so the dispatch table has the same entries the reference one has; all of
// them decode to the same 4-bit operand.
func xchg0Prefixes() []helpers.BitPrefix {
	prefixes := []helpers.BitPrefix{helpers.SlicePrefix(4, []byte{0x0})}
	for i := uint64(2); i < 16; i++ {
		prefixes = append(prefixes, helpers.UIntPrefix(i, 8))
	}
	return prefixes
}

var xchg0Op = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.NewPrefixed(xchg0Prefixes()...),
	ArgBits:  4,
	Action: func(state *vm.State, args uint64) error {
		return state.Stack.Exchange(0, int(args))
	},
	Decode: func(_ *vm.State, code *cell.Slice) (uint64, error) {
		if err := code.SkipBits(4); err != nil {
			return 0, err
		}
		return code.LoadUInt(4)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d XCHG0", args)
	},
})

func XCHG0(i uint8) vm.OP {
	return vm.Bind(xchg0Op, uint64(i))
}
