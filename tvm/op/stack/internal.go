package stack

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const maxSmallIndex = (1 << 30) - 1

func popSmallIndex(state *vm.State) (int, error) {
	max := int64(maxSmallIndex)
	if state.GlobalVersion < 4 {
		max = 255
	}

	val, err := state.Stack.PopIntRangeInt64(0, max)
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

// The permutation opcodes carry their operands as consecutive 4-bit fields, so
// a single packed value stands in for the whole operand list. The fields keep
// the order they have in the instruction: the first operand takes the high
// nibble.
// An operand too wide for its field would carry into its neighbour and quietly
// assemble a different but perfectly valid instruction, so it is rejected here
// rather than silently encoded. Decoding never reaches this: the fields are
// four bits wide by construction.
func packArgs2(i, j uint8) uint64 {
	requireNibbles(i, j)
	return uint64(i)<<4 | uint64(j)
}

func unpackArgs2(args uint64) (int, int) {
	return int(args>>4) & 0xF, int(args) & 0xF
}

func packArgs3(i, j, k uint8) uint64 {
	requireNibbles(i, j, k)
	return uint64(i)<<8 | uint64(j)<<4 | uint64(k)
}

func requireNibbles(values ...uint8) {
	for _, v := range values {
		if v > 0xF {
			panic(fmt.Sprintf("stack: operand %d does not fit the instruction's 4-bit field", v))
		}
	}
}

func unpackArgs3(args uint64) (int, int, int) {
	return int(args>>8) & 0xF, int(args>>4) & 0xF, int(args) & 0xF
}

func maxStackDepthCount(a, b int) int {
	if a > b {
		return a
	}

	return b
}
