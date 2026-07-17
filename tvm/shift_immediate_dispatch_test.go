package tvm

import (
	"bytes"
	"fmt"
	"testing"

	opmath "github.com/xssnick/tonutils-go/tvm/op/math"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// TestTVMShiftImmediateCodeRoundTrip pins the unsigned shift-1 encoding of the
// 8-bit immediate opcodes: the full 1..256 range must serialize (256 -> 0xFF,
// 128 -> 0x7F), dispatch and decode back to the same text and bytes.
func TestTVMShiftImmediateCodeRoundTrip(t *testing.T) {
	machine := NewTVM()

	cases := []struct {
		text       string
		prefix     uint64
		prefixBits uint
		build      func(int) vm.OP
	}{
		{text: "LSHIFT#", prefix: 0xAA, prefixBits: 8, build: func(v int) vm.OP { return opmath.LSHIFTCODE(v) }},
		{text: "RSHIFT#", prefix: 0xAB, prefixBits: 8, build: func(v int) vm.OP { return opmath.RSHIFTCODE(v) }},
		{text: "MULADDRSHIFT#MOD", prefix: 0xA9B0, prefixBits: 16, build: func(v int) vm.OP { return opmath.MULADDRSHIFTCODEMOD(v) }},
	}

	for _, tc := range cases {
		for _, shift := range []int{1, 127, 128, 255, 256} {
			t.Run(fmt.Sprintf("%s_%d", tc.text, shift), func(t *testing.T) {
				code := tc.build(shift).Serialize().EndCell()

				slice := code.MustBeginParse()
				if got := slice.MustLoadUInt(tc.prefixBits); got != tc.prefix {
					t.Fatalf("prefix = %#x, want %#x", got, tc.prefix)
				}
				if got := slice.MustLoadUInt(8); got != uint64(shift-1) {
					t.Fatalf("shift %d suffix byte = %#x, want %#x", shift, got, shift-1)
				}
				if left := slice.BitsLeft(); left != 0 {
					t.Fatalf("unexpected %d trailing bits after suffix", left)
				}

				getter := machine.matchOpcode(code.MustBeginParse())
				if getter == nil {
					t.Fatalf("dispatcher did not match %s shift %d", tc.text, shift)
				}
				decoded := getter()
				if err := decoded.Deserialize(code.MustBeginParse()); err != nil {
					t.Fatalf("deserialize %s shift %d: %v", tc.text, shift, err)
				}

				wantText := fmt.Sprintf("%d %s", shift, tc.text)
				if got := decoded.SerializeText(); got != wantText {
					t.Fatalf("decoded text = %q, want %q", got, wantText)
				}

				reserialized := decoded.Serialize().EndCell()
				if !bytes.Equal(code.Hash(), reserialized.Hash()) {
					t.Fatalf("round-trip bytes mismatch: %s vs %s", code.Dump(), reserialized.Dump())
				}
			})
		}
	}
}

// TestTVMShiftImmediateCodeRejectsOutOfRange pins the constructor contract:
// the shift immediate is 1..256, anything else must panic instead of silently
// wrapping (previously int8 wrapped 128 into shift 1).
func TestTVMShiftImmediateCodeRejectsOutOfRange(t *testing.T) {
	for _, shift := range []int{-128, 0, 257} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("LSHIFTCODE(%d) did not panic", shift)
				}
			}()
			opmath.LSHIFTCODE(shift)
		}()
	}
}
