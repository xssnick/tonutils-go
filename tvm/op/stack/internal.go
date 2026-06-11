package stack

import (
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const maxSmallIndex = (1 << 30) - 1

func popSmallIndex(state *vm.State) (int, error) {
	val, err := state.Stack.PopIntRangeInt64(0, maxSmallIndex)
	if err != nil {
		return 0, err
	}

	return int(val), nil
}

func consumeLargeStackMoveGas(state *vm.State, count int) error {
	if count <= 255 {
		return nil
	}
	return state.ConsumeGas(int64(count - 255))
}

func requireStackDepth(state *vm.State, count int, indices ...int) error {
	need := count
	for _, idx := range indices {
		if idx < 0 {
			continue
		}
		if idx+1 > need {
			need = idx + 1
		}
	}
	if state.Stack.Len() < need {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}

	return nil
}

func maxStackDepthCount(a, b int) int {
	if a > b {
		return a
	}

	return b
}
