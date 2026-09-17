package math

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func historicalComparisonOps() []struct {
	op    vm.OP
	quiet bool
	want  [3]int64 // finite results for x < y, x == y and x > y
} {
	return []struct {
		op    vm.OP
		quiet bool
		want  [3]int64
	}{
		{LESS(), false, [3]int64{-1, 0, 0}},
		{EQUAL(), false, [3]int64{0, -1, 0}},
		{LEQ(), false, [3]int64{-1, -1, 0}},
		{GREATER(), false, [3]int64{0, 0, -1}},
		{NEQ(), false, [3]int64{-1, 0, -1}},
		{GEQ(), false, [3]int64{0, -1, -1}},
		{CMP(), false, [3]int64{-1, 0, 1}},
		{QLESS(), true, [3]int64{-1, 0, 0}},
		{QEQUAL(), true, [3]int64{0, -1, 0}},
		{QLEQ(), true, [3]int64{-1, -1, 0}},
		{QGREATER(), true, [3]int64{0, 0, -1}},
		{QNEQ(), true, [3]int64{-1, 0, -1}},
		{QGEQ(), true, [3]int64{0, -1, -1}},
		{QCMP(), true, [3]int64{-1, 0, 1}},
	}
}

// The old exec_cmp returned x when either operand was invalid:
// https://github.com/ton-blockchain/ton/blob/9f008b129f1fec6c72a5e67e69ddf9caca02d27f/crypto/vm/arithops.cpp
func TestHistoricalNaNComparisonMatrix(t *testing.T) {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))
	values := []*big.Int{big.NewInt(0), big.NewInt(-1), big.NewInt(7), big.NewInt(4577118987), min, max}
	for _, test := range historicalComparisonOps() {
		for version := -1; version <= vm.MaxSupportedGlobalVersion; version++ {
			for _, enabled := range []bool{false, true} {
				for valueIndex, value := range values {
					for nanMask := range 4 {
						t.Run(fmt.Sprintf("%s/v%d/enabled=%t/value%d/nan%d", test.op.SerializeText(), version, enabled, valueIndex, nanMask), func(t *testing.T) {
							state := newMathCoverageState()
							state.GlobalVersion = version
							state.Historical.NaNComparison = enabled
							for index, operand := range []*big.Int{value, big.NewInt(7)} {
								var err error
								if nanMask&(1<<index) != 0 {
									err = state.Stack.PushAny(vm.NaN{})
								} else {
									err = state.Stack.PushInt(operand)
								}
								if err != nil {
									t.Fatal(err)
								}
							}

							var want *big.Int
							if nanMask == 0 {
								want = big.NewInt(test.want[value.Cmp(big.NewInt(7))+1])
							} else if nanMask == 2 && enabled && version >= 0 && version <= 3 {
								want = value
							}
							err := test.op.Interpret(state)
							if want == nil && !test.quiet {
								assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
							} else {
								if err != nil {
									t.Fatal(err)
								}
								got, err := state.Stack.PopInt()
								if err != nil {
									t.Fatal(err)
								}
								if (want == nil) != (got == nil) || want != nil && got.Cmp(want) != 0 {
									t.Fatalf("result = %v, want %v", got, want)
								}
							}
							if state.Stack.Len() != 0 {
								t.Fatalf("comparison left %d extra operands", state.Stack.Len())
							}
						})
					}
				}
			}
		}
	}
}

func TestHistoricalNaNComparisonOwnsReturnedInt(t *testing.T) {
	for _, test := range historicalComparisonOps() {
		for _, input := range []int64{-1, 0, 7, 4577118987} {
			t.Run(fmt.Sprintf("%s/%d", test.op.SerializeText(), input), func(t *testing.T) {
				state := newMathCoverageState()
				state.GlobalVersion = 2
				state.Historical.NaNComparison = true
				pushMathCoverageInts(t, state, input)
				retained, err := state.Stack.Get(0)
				if err != nil {
					t.Fatal(err)
				}
				if err := state.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatal(err)
				}
				if err := test.op.Interpret(state); err != nil {
					t.Fatal(err)
				}
				got, err := state.Stack.PopIntFinite()
				if err != nil || got.Int64() != input {
					t.Fatalf("result = %v, err = %v", got, err)
				}
				got.SetInt64(123)
				if retained.(*big.Int).Cmp(big.NewInt(input)) != 0 {
					t.Fatalf("returned result aliases the read-only operand: %v", retained)
				}
			})
		}
	}
}

func TestHistoricalNaNComparisonLeavesUnaryOpsUnchanged(t *testing.T) {
	for _, test := range []struct {
		op    vm.OP
		quiet bool
	}{
		{SGN(), false}, {QSGN(), true},
		{EQINT(7), false}, {LESSINT(7), false}, {GTINT(7), false}, {NEQINT(7), false},
		{QEQINT(7), true}, {QLESSINT(7), true}, {QGTINT(7), true}, {QNEQINT(7), true},
	} {
		t.Run(test.op.SerializeText(), func(t *testing.T) {
			for version := 0; version <= 3; version++ {
				state := newMathCoverageState()
				state.GlobalVersion = version
				state.Historical.NaNComparison = true
				if err := state.Stack.PushAny(vm.NaN{}); err != nil {
					t.Fatal(err)
				}
				err := test.op.Interpret(state)
				if !test.quiet {
					assertMathCoverageVMError(t, err, vmerr.CodeIntOverflow)
				} else {
					if err != nil {
						t.Fatal(err)
					}
					requireMathStackNaN(t, state)
				}
			}
		})
	}
}
