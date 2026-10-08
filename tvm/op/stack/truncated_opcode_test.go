package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func truncatedCode(t *testing.T, bits uint64, n uint) *cell.Slice {
	t.Helper()
	code, err := cell.BeginCell().MustStoreUInt(bits, n).EndCell().BeginParse()
	if err != nil {
		t.Fatalf("failed to build truncated code: %v", err)
	}
	return code
}

// A truncated instruction is zero-padded to the width of its opcode-table key,
// and the reference VM charges gas for the entry that padded key selects before
// throwing inv_opcode. The mapping is consensus-critical, so every reachable
// padded form is pinned here.
//
// Zero padding can never produce a trailing one bit, so opcode keys ending in 1
// (PUSHINT 0x81, PUSHSLICE 0x8D, PUSHCONT 0x47) are unreachable from a truncated
// read and can only be selected by a complete one — the PUSHCONT cases below
// cover that difference explicitly.
func TestTruncatedOpcodeZeroPadding(t *testing.T) {
	t.Run("PUSHINT", func(t *testing.T) {
		tests := []struct {
			name   string
			bits   uint64
			n      uint
			padded uint64
			want   int64
		}{
			{name: "Int8From1Bit", bits: 0b1, n: 1, padded: 0x80, want: 16},
			{name: "Int8From4Bits", bits: 0b1000, n: 4, padded: 0x80, want: 16},
			{name: "Int8From7Bits", bits: 0b1000000, n: 7, padded: 0x80, want: 16},
			{name: "LongFrom7Bits", bits: 0b1000001, n: 7, padded: 0x82, want: 13},
			{name: "TinyFrom5Bits", bits: 0b10001, n: 5, padded: 0x88, want: 8},
			{name: "Empty", bits: 0, n: 0, padded: 0, want: 8},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code := truncatedCode(t, tt.bits, tt.n)
				op := PUSHINT(nil)
				if err := op.Deserialize(code); err == nil {
					t.Fatal("expected truncated PUSHINT to fail")
				}
				if got := op.InstructionBits(); got != tt.want {
					t.Fatalf("padded opcode %#x: unexpected charged bits: got %d want %d", tt.padded, got, tt.want)
				}
				if left := code.BitsLeft(); left != 0 {
					t.Fatalf("truncated remainder must be consumed, %d bits left", left)
				}
			})
		}
	})

	t.Run("PUSHSLICE", func(t *testing.T) {
		tests := []struct {
			name   string
			bits   uint64
			n      uint
			padded uint64
			form   string
			want   int64
		}{
			{name: "RefFrom6Bits", bits: 0b100011, n: 6, padded: 0x8C, form: "REF", want: 15},
			{name: "RefFrom7Bits", bits: 0b1000110, n: 7, padded: 0x8C, form: "REF", want: 15},
			{name: "ShortFrom4Bits", bits: 0b1000, n: 4, padded: 0x80, form: "SHORT", want: 12},
			{name: "ShortFrom7Bits", bits: 0b1000101, n: 7, padded: 0x8A, form: "SHORT", want: 12},
			{name: "Empty", bits: 0, n: 0, padded: 0, form: "SHORT", want: 12},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code := truncatedCode(t, tt.bits, tt.n)
				op := PUSHSLICEINLINE(cell.BeginCell().ToSlice())
				if err := op.Deserialize(code); err == nil {
					t.Fatal("expected truncated PUSHSLICE to fail")
				}
				if op.form != tt.form {
					t.Fatalf("padded opcode %#x: unexpected form: got %q want %q", tt.padded, op.form, tt.form)
				}
				if got := op.InstructionBits(); got != tt.want {
					t.Fatalf("padded opcode %#x: unexpected charged bits: got %d want %d", tt.padded, got, tt.want)
				}
				if left := code.BitsLeft(); left != 0 {
					t.Fatalf("truncated remainder must be consumed, %d bits left", left)
				}
			})
		}
	})

	t.Run("PUSHCONT", func(t *testing.T) {
		tests := []struct {
			name string
			bits uint64
			n    uint
			typ  string
			want int64
			left uint
		}{
			// The 3-bit read fails and the padded key never reaches 0x47, so the
			// ref form is charged for every truncation of the second nibble.
			{name: "RefFrom4Bits", bits: 0b1000, n: 4, typ: "REF", want: 8},
			{name: "RefFrom5Bits", bits: 0b10001, n: 5, typ: "REF", want: 8},
			{name: "RefFrom6Bits", bits: 0b100011, n: 6, typ: "REF", want: 8},
			// A complete 7-bit read does select the big form, truncating only the
			// ref count that follows it.
			{name: "BigFrom7Bits", bits: 0b1000111, n: 7, typ: "BIG", want: 16},
			{name: "RefFrom7Bits", bits: 0b1000101, n: 7, typ: "REF", want: 8},
			// The first nibble itself is truncated: nothing is decoded and the
			// remainder is left untouched.
			{name: "UndecodedFrom3Bits", bits: 0b100, n: 3, typ: "", want: 8, left: 3},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code := truncatedCode(t, tt.bits, tt.n)
				op := PUSHCONT(nil)
				if err := op.Deserialize(code); err == nil {
					t.Fatal("expected truncated PUSHCONT to fail")
				}
				if op.typ != tt.typ {
					t.Fatalf("unexpected form: got %q want %q", op.typ, tt.typ)
				}
				if got := op.InstructionBits(); got != tt.want {
					t.Fatalf("unexpected charged bits: got %d want %d", got, tt.want)
				}
				if left := code.BitsLeft(); left != tt.left {
					t.Fatalf("unexpected remainder: got %d bits want %d", left, tt.left)
				}
			})
		}
	})

	t.Run("XCHG", func(t *testing.T) {
		tests := []struct {
			name string
			bits uint64
			n    uint
			long bool
			want int64
			left uint
		}{
			// s1 followed by a zero-padded second nibble selects the 16-bit long
			// form, which is what gets charged.
			{name: "LongFrom4Bits", bits: 0b0001, n: 4, long: true, want: 16},
			{name: "LongFrom5Bits", bits: 0b00010, n: 5, long: true, want: 16},
			{name: "LongFrom7Bits", bits: 0b0001000, n: 7, long: true, want: 16},
			// A padded second nibble that is not zero stays on the short form.
			{name: "ShortFrom5Bits", bits: 0b00011, n: 5, want: 8},
			{name: "ShortFrom6Bits", bits: 0b000101, n: 6, want: 8},
			// A first nibble other than s1 is never padded, so the remainder is
			// left in place.
			{name: "ShortFrom5BitsOtherIndex", bits: 0b00100, n: 5, want: 8, left: 1},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				code := truncatedCode(t, tt.bits, tt.n)
				args, err := xchgOp.DecodeArgs(nil, code)
				if err == nil {
					t.Fatal("expected truncated XCHG to fail")
				}
				if _, _, long := unpackXchgArgs(args); long != tt.long {
					t.Fatalf("unexpected long form flag: got %v want %v", long, tt.long)
				}
				if got := xchgOp.ArgInstructionBits(args); got != tt.want {
					t.Fatalf("unexpected charged bits: got %d want %d", got, tt.want)
				}
				if left := code.BitsLeft(); left != tt.left {
					t.Fatalf("unexpected remainder: got %d bits want %d", left, tt.left)
				}
			})
		}
	})
}
