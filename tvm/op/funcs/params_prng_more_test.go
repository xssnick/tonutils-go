package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestParamsPRNGMoreEdges(t *testing.T) {
	t.Run("prev blocks alias propagates missing indexes", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{
			paramIdxPrevBlocksInfo: *tuple.NewTuple(big.NewInt(1)),
		})
		if err := PREVMCBLOCKS_100().Interpret(st); err == nil {
			t.Fatal("PREVMCBLOCKS_100 should fail when the tuple is too short")
		}
	})

	t.Run("push in msg param propagates index errors", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{
			paramIdxInMsgParams: *tuple.NewTuple(big.NewInt(1)),
		})
		if err := pushInMsgParam(st, 5); err == nil {
			t.Fatal("pushInMsgParam should fail when the tuple is too short")
		}

		st = newFuncTestState(t, map[int]any{
			paramIdxInMsgParams: *tuple.NewTuple(big.NewInt(1)),
		})
		if err := INMSGPARAM(9).Interpret(st); err == nil {
			t.Fatal("INMSGPARAM should propagate tuple index errors")
		}
	})

	t.Run("in msg params reject unsupported host values", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{
			paramIdxInMsgParams: "bad-value",
		})
		if err := INMSGPARAMS().Interpret(st); err == nil {
			t.Fatal("INMSGPARAMS should reject unsupported host values")
		}
	})

	t.Run("set rand seed rejects malformed c7 shapes", func(t *testing.T) {
		c7 := tuple.NewTupleSized(1)
		if err := c7.Set(0, big.NewInt(1)); err != nil {
			t.Fatalf("failed to build malformed c7: %v", err)
		}

		st := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.NewGas(), nil, c7, vm.NewStack())
		st.InitForExecution()
		if err := setRandSeed(st, big.NewInt(1)); err == nil {
			t.Fatal("setRandSeed should reject non-tuple params slots")
		}

		emptyC7 := tuple.NewTupleSized(0)
		st = vm.NewExecutionState(vm.DefaultGlobalVersion, vm.NewGas(), nil, emptyC7, vm.NewStack())
		st.InitForExecution()
		if err := setRandSeed(st, big.NewInt(1)); err == nil {
			t.Fatal("setRandSeed should reject missing params slots")
		}
	})

	t.Run("rand seed helpers propagate validation errors", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{
			paramIdxRandomSeed: "bad-seed",
		})
		if _, err := randSeedBytes(st); err == nil {
			t.Fatal("randSeedBytes should reject non-integer seeds")
		}
		if _, err := generateRandU256(st); err == nil {
			t.Fatal("generateRandU256 should reject non-integer seeds")
		}
	})

	t.Run("generate rand rejects missing random seed params", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if _, err := generateRandU256(st); err == nil {
			t.Fatal("generateRandU256 should fail when the random seed param is missing")
		}
	})

	t.Run("set rand ops validate inputs and existing seed state", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{
			paramIdxRandomSeed: big.NewInt(1),
		})
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SETRAND().Interpret(st); err == nil {
			t.Fatal("SETRAND should reject negative seeds")
		}

		st = newFuncTestState(t, map[int]any{
			paramIdxRandomSeed: "bad-seed",
		})
		if err := st.Stack.PushInt(big.NewInt(5)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := ADDRAND().Interpret(st); err == nil {
			t.Fatal("ADDRAND should propagate random-seed validation errors")
		}
	})
}
