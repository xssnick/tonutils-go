package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList, stiOp)
}

// The operand is the encoded width minus one, exactly as it sits in the code.
var stiOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xCA)),
	ArgBits:  8,
	Action: func(state *vm.State, args uint64) error {
		if err := checkStackDepth(state, 2); err != nil {
			return err
		}

		sz := uint(args) + 1

		b0, err := state.Stack.PopBuilder()
		if err != nil {
			return err
		}

		i1, err := state.Stack.PopInt()
		if err != nil {
			return err
		}

		if !b0.CanExtendBy(sz, 0) {
			return vmerr.Error(vmerr.CodeCellOverflow)
		}
		if !signedStoreFits(i1, sz) {
			return cell.ErrTooBigValue
		}
		if err := b0.StoreBigInt(i1, sz); err != nil {
			return err
		}
		return state.Stack.PushOwnedBuilder(b0)
	},
	Name: func(args uint64) string {
		return fmt.Sprintf("%d STI", uint(args)+1)
	},
})

func STI(sz uint) vm.OP {
	return vm.Bind(stiOp, uint64(sz-1))
}
