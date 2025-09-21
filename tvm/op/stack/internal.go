package stack

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const maxSmallIndex = (1 << 30) - 1

func popSmallIndex(state *vm.State) (int, error) {
	val, err := state.Stack.PopIntRange(0, maxSmallIndex)
	if err != nil {
		return 0, err
	}

	return int(val.Int64()), nil
}
