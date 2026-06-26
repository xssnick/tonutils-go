package math

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type invalidCompoundCase struct {
	name       string
	prefix     helpers.BitPrefix
	suffixBits uint
	suffix     uint64
}

func invalidCompoundCases() []invalidCompoundCase {
	return []invalidCompoundCase{
		{name: "DIV/MOD<invalid>", prefix: helpers.UIntPrefix(0xA90, 12), suffixBits: 4, suffix: 0x3},
		{name: "SHR/MOD<invalid>", prefix: helpers.UIntPrefix(0xA92, 12), suffixBits: 4, suffix: 0x7},
		{name: "SHR#/MOD<invalid>", prefix: helpers.UIntPrefix(0xA93, 12), suffixBits: 12, suffix: 0x300},
		{name: "MULDIV/MOD<invalid>", prefix: helpers.UIntPrefix(0xA98, 12), suffixBits: 4, suffix: 0x7},
		{name: "MULSHR/MOD<invalid>", prefix: helpers.UIntPrefix(0xA9A, 12), suffixBits: 4, suffix: 0x7},
		{name: "MULSHR#/MOD<invalid>", prefix: helpers.UIntPrefix(0xA9B, 12), suffixBits: 12, suffix: 0x300},
		{name: "SHLDIV/MOD<invalid>", prefix: helpers.UIntPrefix(0xA9C, 12), suffixBits: 4, suffix: 0x7},
		{name: "SHLDIV#/MOD<invalid>", prefix: helpers.UIntPrefix(0xA9D, 12), suffixBits: 12, suffix: 0x300},
	}
}

func TestInvalidCompoundFamilies(t *testing.T) {
	for _, tt := range invalidCompoundCases() {
		t.Run(tt.name, func(t *testing.T) {
			op := invalidCompoundFamily(tt.name, tt.prefix, tt.suffixBits, tt.suffix)
			if got := op.SerializeText(); got != tt.name {
				t.Fatalf("text = %q, want %q", got, tt.name)
			}
			if got := op.InstructionBits(); got != int64(tt.prefix.Bits+tt.suffixBits) {
				t.Fatalf("instruction bits = %d, want %d", got, tt.prefix.Bits+tt.suffixBits)
			}
			if got := op.MinGlobalVersion(); got != 0 {
				t.Fatalf("min version = %d, want 0", got)
			}

			encoded := op.Serialize().EndCell()
			decoded := invalidCompoundFamily(tt.name, tt.prefix, tt.suffixBits, 0)
			if err := decoded.Deserialize(encoded.MustBeginParse()); err != nil {
				t.Fatalf("deserialize serialized invalid compound: %v", err)
			}

			state := newMathCoverageState()
			var vmErr vmerr.VMError
			if err := decoded.Interpret(state); !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeInvalidOpcode {
				t.Fatalf("interpret error = %v, want invalid opcode", err)
			}
		})
	}
}

func TestInvalidCompoundDeserializeSuffixEdges(t *testing.T) {
	for _, tt := range invalidCompoundCases() {
		t.Run(tt.name, func(t *testing.T) {
			op := invalidCompoundFamily(tt.name, tt.prefix, tt.suffixBits, tt.suffix)

			short := cell.BeginCell().MustStoreSlice(tt.prefix.Data, tt.prefix.Bits).EndCell()
			if err := op.Deserialize(short.MustBeginParse()); err == nil {
				t.Fatal("expected truncated invalid compound suffix to fail")
			}

			acceptedSuffixes := []uint64{0, tt.suffix}
			if tt.suffixBits <= 8 {
				acceptedSuffixes = append(acceptedSuffixes, (uint64(1)<<tt.suffixBits)-1)
			} else {
				acceptedSuffixes = append(acceptedSuffixes, 0xff, (uint64(1)<<tt.suffixBits)-1)
			}
			for _, suffix := range acceptedSuffixes {
				code := cell.BeginCell().
					MustStoreSlice(tt.prefix.Data, tt.prefix.Bits).
					MustStoreUInt(suffix, tt.suffixBits).
					EndCell()
				if err := op.Deserialize(code.MustBeginParse()); err != nil {
					t.Fatalf("suffix %#x deserialize failed: %v", suffix, err)
				}
			}
		})
	}
}

func FuzzTVMInvalidCompoundDeserializeSuffixes(f *testing.F) {
	for idx, tt := range invalidCompoundCases() {
		f.Add(uint8(idx), uint16(0))
		f.Add(uint8(idx), uint16(tt.suffix))
		if tt.suffixBits <= 8 {
			f.Add(uint8(idx), uint16((uint64(1)<<tt.suffixBits)-1))
		} else {
			f.Add(uint8(idx), uint16(0xff))
			f.Add(uint8(idx), uint16((uint64(1)<<tt.suffixBits)-1))
		}
	}

	cases := invalidCompoundCases()
	f.Fuzz(func(t *testing.T, rawIdx uint8, rawSuffix uint16) {
		tt := cases[int(rawIdx)%len(cases)]
		mask := uint64(1<<tt.suffixBits) - 1
		suffix := uint64(rawSuffix) & mask

		op := invalidCompoundFamily(tt.name, tt.prefix, tt.suffixBits, tt.suffix)
		code := cell.BeginCell().
			MustStoreSlice(tt.prefix.Data, tt.prefix.Bits).
			MustStoreUInt(suffix, tt.suffixBits).
			EndCell()

		if err := op.Deserialize(code.MustBeginParse()); err != nil {
			t.Fatalf("family=%s suffix=%#x deserialize failed: %v", tt.name, suffix, err)
		}

		var vmErr vmerr.VMError
		if err := op.Interpret(newMathCoverageState()); !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeInvalidOpcode {
			t.Fatalf("family=%s suffix=%#x interpret error = %v, want invalid opcode", tt.name, suffix, err)
		}
	})
}
