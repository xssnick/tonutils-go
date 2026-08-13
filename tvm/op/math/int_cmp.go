package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// int cmp result masks: bit 0 set means "true when x < value",
// bit 1 - "when x == value", bit 2 - "when x > value".
const (
	intCmpMaskLess    = 0b001
	intCmpMaskEqual   = 0b010
	intCmpMaskGreater = 0b100
)

// intCmpOp builds one of the <int8-argument> comparison opcodes
// (LESSINT/EQINT/GTINT/NEQINT). The compared constant is signed, so it is
// serialized with MustStoreInt while the decoder reads the same eight bits
// unsigned and casts them back.
func intCmpOp(name string, prefix byte, mask uint8) *helpers.ArgOP {
	return helpers.NewArgOP(&helpers.ArgOP{
		Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(prefix)),
		ArgBits:  8,
		Action: func(state *vm.State, args uint64) error {
			i0, err := state.Stack.PopIntRead()
			if err != nil {
				return err
			}
			if i0 == nil {
				return pushNaNOrOverflow(state, false)
			}

			bit := uint8(1) << (compareBigIntInt64(i0, int64(int8(args))) + 1)
			return state.Stack.PushBool(mask&bit != 0)
		},
		Serializer: func(args uint64) *cell.Builder {
			return cell.BeginCell().
				MustStoreUInt(uint64(prefix), 8).
				MustStoreInt(int64(int8(args)), 8)
		},
		Name: func(args uint64) string {
			return fmt.Sprintf("%d %s", int8(args), name)
		},
	})
}
