package math

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestRightShiftErrorAndImmediateTextEdges(t *testing.T) {
	t.Run("RSHIFT_underflow", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 7)
		assertMathCoverageVMError(t, RSHIFT().Interpret(st), vmerr.CodeStackUnderflow)
	})

	t.Run("RSHIFT_shift_type", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageInts(t, st, 7)
		pushMathCoverageNonInt(t, st)
		assertMathCoverageVMError(t, RSHIFT().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("RSHIFT_value_type", func(t *testing.T) {
		st := newMathCoverageState()
		pushMathCoverageNonInt(t, st)
		pushMathCoverageInts(t, st, 1)
		assertMathCoverageVMError(t, RSHIFT().Interpret(st), vmerr.CodeTypeCheck)
	})

	for _, tt := range []struct {
		name   string
		op     *helpers.AdvancedOP
		prefix uint64
		bits   uint
		text   string
	}{
		{name: "RSHIFT#", op: RSHIFTCODE(3), prefix: 0xAB, bits: 8, text: "3 RSHIFT#"},
		{name: "RSHIFTR#", op: RSHIFTRCODE(3), prefix: 0xA935, bits: 16, text: "3 RSHIFTR#"},
		{name: "RSHIFTC#", op: RSHIFTCCODE(3), prefix: 0xA936, bits: 16, text: "3 RSHIFTC#"},
	} {
		t.Run(tt.name+"_text", func(t *testing.T) {
			if got := tt.op.SerializeText(); got != tt.text {
				t.Fatalf("%s text = %q, want %q", tt.name, got, tt.text)
			}
		})

		t.Run(tt.name+"_underflow", func(t *testing.T) {
			st := newMathCoverageState()
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"_type", func(t *testing.T) {
			st := newMathCoverageState()
			pushMathCoverageNonInt(t, st)
			assertMathCoverageVMError(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"_short_suffix", func(t *testing.T) {
			code := cell.BeginCell().MustStoreUInt(tt.prefix, tt.bits).EndCell().MustBeginParse()
			if err := tt.op.Deserialize(code); err == nil {
				t.Fatalf("%s short suffix deserialize unexpectedly succeeded", tt.name)
			}
		})
	}
}
