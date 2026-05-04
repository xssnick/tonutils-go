package math

import (
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const bitSizeInvalid = 0x7fffffff

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
		return state.Stack.PushIntQuiet(val)
	}
	return state.Stack.PushInt(val)
}

func pushSmallInt(state *vm.State, val int64) error {
	return state.Stack.PushInt(big.NewInt(val))
}

func popIntFinite(state *vm.State) (*big.Int, error) {
	return state.Stack.PopIntFinite()
}

func popIntRange(state *vm.State, min, max int64) (*big.Int, error) {
	return state.Stack.PopIntRange(min, max)
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

	limit := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	min := new(big.Int).Neg(new(big.Int).Set(limit))
	max := new(big.Int).Sub(limit, big.NewInt(1))
	return x.Cmp(min) >= 0 && x.Cmp(max) <= 0
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

	limit := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	return x.Cmp(limit) < 0
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
	t.Sub(t, big.NewInt(1))
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
