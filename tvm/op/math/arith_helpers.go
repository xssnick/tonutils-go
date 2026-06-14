package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const bitSizeInvalid = 0x7fffffff

var (
	bigIntOne              = big.NewInt(1)
	bigIntMaxShift         = big.NewInt(1023)
	bigIntMaxCompoundShift = big.NewInt(256)
)

func pushNaNOrOverflow(state *vm.State, quiet bool) error {
	if quiet {
		return state.Stack.PushAny(vm.NaN{})
	}
	return vmerr.Error(vmerr.CodeIntOverflow)
}

func pushMaybeInt(state *vm.State, val *big.Int, quiet bool) error {
	if val == nil {
		return pushNaNOrOverflow(state, quiet)
	}
	if quiet {
		return state.Stack.PushOwnedIntQuiet(val)
	}
	return state.Stack.PushOwnedInt(val)
}

func pushSmallInt(state *vm.State, val int64) error {
	return state.Stack.PushSmallInt(val)
}

func checkStackDepth(state *vm.State, depth int) error {
	if state.Stack.Len() < depth {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return nil
}

func popIntFinite(state *vm.State) (*big.Int, error) {
	return state.Stack.PopIntFinite()
}

func popIntRange(state *vm.State, min, max int64) (*big.Int, error) {
	return state.Stack.PopIntRange(min, max)
}

func popInt(state *vm.State) (*big.Int, error) {
	return state.Stack.PopInt()
}

func requireFiniteInts(values ...*big.Int) error {
	for _, value := range values {
		if value == nil {
			return vmerr.Error(vmerr.CodeIntOverflow)
		}
	}
	return nil
}

func popIntOperand(state *vm.State, quiet bool) (*big.Int, error) {
	return state.Stack.PopInt()
}

func unaryIntResult(x *big.Int, fn func(*big.Int) *big.Int) *big.Int {
	if x == nil {
		return nil
	}
	return fn(x)
}

func binaryIntResult(x, y *big.Int, fn func(*big.Int, *big.Int) *big.Int) *big.Int {
	if x == nil || y == nil {
		return nil
	}
	return fn(x, y)
}

func pushUnaryIntResult(state *vm.State, x *big.Int, fn func(*big.Int) *big.Int) error {
	return pushMaybeInt(state, unaryIntResult(x, fn), false)
}

func pushBinaryIntResult(state *vm.State, x, y *big.Int, fn func(*big.Int, *big.Int) *big.Int) error {
	return pushMaybeInt(state, binaryIntResult(x, y, fn), false)
}

func pushCompareResult(state *vm.State, x, y *big.Int, fn func(*big.Int, *big.Int) bool) error {
	if x == nil || y == nil {
		return pushNaNOrOverflow(state, false)
	}
	return state.Stack.PushBool(fn(x, y))
}

func compareBigIntInt64(x *big.Int, y int64) int {
	if x.IsInt64() {
		xv := x.Int64()
		switch {
		case xv < y:
			return -1
		case xv > y:
			return 1
		default:
			return 0
		}
	}
	if x.Sign() < 0 {
		return -1
	}
	return 1
}

func signedFitsBits(x *big.Int, bits int) bool {
	if x == nil {
		return false
	}
	if x.Sign() == 0 {
		return true
	}
	if bits <= 0 {
		return false
	}

	if x.Sign() > 0 {
		return x.BitLen() < bits
	}

	t := new(big.Int).Neg(x)
	t.Sub(t, bigIntOne)
	return t.BitLen() < bits
}

func unsignedFitsBits(x *big.Int, bits int) bool {
	if x == nil {
		return false
	}
	if x.Sign() == 0 {
		return true
	}
	if x.Sign() < 0 || bits < 0 {
		return false
	}

	return x.BitLen() <= bits
}

func signedBitSize(x *big.Int) int {
	if x == nil {
		return bitSizeInvalid
	}
	if x.Sign() == 0 {
		return 0
	}
	if x.Sign() > 0 {
		return x.BitLen() + 1
	}

	// For negative values, TVM uses the minimal signed width such that
	// x fits in the range [-2^(n-1), 2^(n-1)-1].
	t := new(big.Int).Neg(x)
	t.Sub(t, bigIntOne)
	return t.BitLen() + 1
}

func unsignedBitSize(x *big.Int) int {
	if x == nil || x.Sign() < 0 {
		return bitSizeInvalid
	}
	if x.Sign() == 0 {
		return 0
	}
	return x.BitLen()
}

func compareModeValue(mode int, cmp int) int64 {
	return int64(((mode >> (4 + cmp*4)) & 15) - 8)
}
