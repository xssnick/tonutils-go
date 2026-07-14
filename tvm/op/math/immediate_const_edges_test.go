package math

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestImmediateConstAndQuietTypeEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   mathInterpretOp
		push func(t *testing.T, st *vm.State)
	}{
		{name: "ADDINT_type", op: ADDINT(1), push: pushMathCoverageNonInt},
		{name: "MULINT_type", op: MULINT(2), push: pushMathCoverageNonInt},
		{name: "QNEGATE_type", op: QNEGATE(), push: pushMathCoverageNonInt},
		{name: "QADD_type_top", op: QADD(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageInts(t, st, 1)
			pushMathCoverageNonInt(t, st)
		}},
		{name: "QADD_type_bottom", op: QADD(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "QADDINT_type", op: QADDINT(1), push: pushMathCoverageNonInt},
		{name: "RSHIFTFLOOR_type_value", op: RSHIFTFLOOR(), push: func(t *testing.T, st *vm.State) {
			pushMathCoverageNonInt(t, st)
			pushMathCoverageInts(t, st, 1)
		}},
		{name: "RSHIFT#_type_value", op: RSHIFTCODEFLOOR(1), push: pushMathCoverageNonInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newMathCoverageState()
			tt.push(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}
}

func TestImmediateConstShortSuffixEdges(t *testing.T) {
	for _, tt := range []struct {
		name        string
		deserialize func(*cell.Slice) error
	}{
		{name: "ADDINT", deserialize: ADDINT(0).Deserialize},
		{name: "MULINT", deserialize: MULINT(0).Deserialize},
		{name: "QADDINT", deserialize: QADDINT(0).Deserialize},
		{name: "QMULINT", deserialize: QMULINT(0).Deserialize},
		{name: "PUSHPOW2", deserialize: PUSHPOW2(0).Deserialize},
		{name: "PUSHPOW2DEC", deserialize: PUSHPOW2DEC(0).Deserialize},
		{name: "PUSHNEGPOW2", deserialize: PUSHNEGPOW2(0).Deserialize},
		{name: "MODPOW2#", deserialize: MODPOW2CODE(0).Deserialize},
		{name: "MODPOW2R#", deserialize: MODPOW2RCODE(0).Deserialize},
		{name: "MODPOW2C#", deserialize: MODPOW2CCODE(0).Deserialize},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.deserialize(cell.BeginCell().EndCell().MustBeginParse()); err == nil {
				t.Fatalf("%s short suffix deserialize unexpectedly succeeded", tt.name)
			}
		})
	}
}

func TestImmediateModSerializeTextEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		got  func() string
	}{
		{name: "MODPOW2#", text: "3 MODPOW2#", got: MODPOW2CODE(3).SerializeText},
		{name: "MODPOW2R#", text: "3 MODPOW2R#", got: MODPOW2RCODE(3).SerializeText},
		{name: "MODPOW2C#", text: "3 MODPOW2C#", got: MODPOW2CCODE(3).SerializeText},
		{name: "RSHIFT#MOD", text: "3 RSHIFT#MOD", got: RSHIFTCODEMOD(3).SerializeText},
		{name: "RSHIFTR#MOD", text: "3 RSHIFTR#MOD", got: RSHIFTRCODEMOD(3).SerializeText},
		{name: "RSHIFTC#MOD", text: "3 RSHIFTC#MOD", got: RSHIFTCCODEMOD(3).SerializeText},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got(); got != tt.text {
				t.Fatalf("%s text = %q, want %q", tt.name, got, tt.text)
			}
		})
	}
}
