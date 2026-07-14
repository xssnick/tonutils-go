package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestBaseBuilderStoreStackTypeAndOverflowEdges(t *testing.T) {
	t.Run("ENDCStackAndType", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, ENDC().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, ENDC().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		assertCellSliceVMErrorCode(t, ENDC().Interpret(st), vmerr.CodeCellOverflow)
	})

	t.Run("CTOSStackTypeAndSpecialError", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, CTOS().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, CTOS().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, mustLibraryCell(t))
		if err := CTOS().Interpret(st); err == nil {
			t.Fatal("CTOS library special unexpectedly succeeded")
		}
	})

	t.Run("ENDSStackAndType", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, ENDS().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, ENDS().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("STBStackTypesAndOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STB().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, STB().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STB().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell().MustStoreUInt(1, 1))
		pushCellSliceBuilder(t, st, fullBitsStoreBaseBuilder())
		if err := STB().Interpret(st); err == nil {
			t.Fatal("STB overflow unexpectedly succeeded")
		}
	})

	t.Run("STREFStackTypesAndOverflow", func(t *testing.T) {
		ref := cell.BeginCell().EndCell()

		st := newCellSliceState()
		pushCellSliceCell(t, st, ref)
		assertCellSliceVMErrorCode(t, STREF().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceCell(t, st, ref)
		pushCellSliceCell(t, st, ref)
		assertCellSliceVMErrorCode(t, STREF().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STREF().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, ref)
		pushCellSliceBuilder(t, st, mustFullBuilder(t))
		if err := STREF().Interpret(st); err == nil {
			t.Fatal("STREF overflow unexpectedly succeeded")
		}
	})

	t.Run("STSLICEStackTypesAndOverflow", func(t *testing.T) {
		sl := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()

		st := newCellSliceState()
		pushCellSliceSlice(t, st, sl.Copy())
		assertCellSliceVMErrorCode(t, STSLICE().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, sl.Copy())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, STSLICE().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STSLICE().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, sl.Copy())
		pushCellSliceBuilder(t, st, fullBitsStoreBaseBuilder())
		if err := STSLICE().Interpret(st); err == nil {
			t.Fatal("STSLICE overflow unexpectedly succeeded")
		}
	})

	t.Run("STUIntStackTypesRangeAndOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STU(8).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, STU(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STU(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceInt(t, st, -1)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := STU(8).Interpret(st); err == nil {
			t.Fatal("STU negative value unexpectedly succeeded")
		}

		st = newCellSliceState()
		pushCellSliceInt(t, st, 256)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := STU(8).Interpret(st); err == nil {
			t.Fatal("STU too-large value unexpectedly succeeded")
		}

		st = newCellSliceState()
		pushCellSliceInt(t, st, 256)
		pushCellSliceBuilder(t, st, fullBitsStoreBaseBuilder())
		assertCellSliceVMErrorCode(t, STU(8).Interpret(st), vmerr.CodeCellOverflow)
	})

	t.Run("STIStackTypesRangeAndOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STI(8).Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 1)
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, STI(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STI(8).Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceInt(t, st, 128)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := STI(8).Interpret(st); err == nil {
			t.Fatal("STI too-large positive value unexpectedly succeeded")
		}

		st = newCellSliceState()
		pushCellSliceInt(t, st, -129)
		pushCellSliceBuilder(t, st, cell.BeginCell())
		if err := STI(8).Interpret(st); err == nil {
			t.Fatal("STI too-small negative value unexpectedly succeeded")
		}

		st = newCellSliceState()
		pushCellSliceInt(t, st, 128)
		pushCellSliceBuilder(t, st, fullBitsStoreBaseBuilder())
		assertCellSliceVMErrorCode(t, STI(8).Interpret(st), vmerr.CodeCellOverflow)
	})
}

func TestSameStoreStackTypeRangeAndShapeEdges(t *testing.T) {
	t.Run("StackAndTypePriority", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STZEROES().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STSAME().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, STONES().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STZEROES().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("RangeAndOverflow", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 1024)
		assertCellSliceVMErrorCode(t, STZEROES().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceInt(t, st, 1)
		pushCellSliceInt(t, st, 2)
		assertCellSliceVMErrorCode(t, STSAME().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceBuilder(t, st, fullBitsStoreBaseBuilder())
		pushCellSliceInt(t, st, 1)
		assertCellSliceVMErrorCode(t, STONES().Interpret(st), vmerr.CodeCellOverflow)
	})

	for _, tt := range []struct {
		name      string
		op        interface{ Interpret(*vm.State) error }
		bitArg    *int64
		bits      int64
		wantValue uint64
	}{
		{name: "STZEROESZeroBits", op: STZEROES(), bits: 0},
		{name: "STONESThreeBits", op: STONES(), bits: 3, wantValue: 0b111},
		{name: "STSAMEZeroes", op: STSAME(), bitArg: int64Ptr(0), bits: 4},
		{name: "STSAMEOnes", op: STSAME(), bitArg: int64Ptr(1), bits: 4, wantValue: 0b1111},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceBuilder(t, st, cell.BeginCell())
			pushCellSliceInt(t, st, tt.bits)
			if tt.bitArg != nil {
				pushCellSliceInt(t, st, *tt.bitArg)
			}
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			got := popCellSliceBuilder(t, st).EndCell().MustBeginParse()
			if got.BitsLeft() != uint(tt.bits) {
				t.Fatalf("%s bits = %d, want %d", tt.name, got.BitsLeft(), tt.bits)
			}
			if tt.bits > 0 && got.MustLoadUInt(uint(tt.bits)) != tt.wantValue {
				t.Fatalf("%s value mismatch", tt.name)
			}
		})
	}
}

func FuzzTVMSameStoreRules(f *testing.F) {
	for _, seed := range []struct {
		op          byte
		initialBits uint16
		bits        uint16
		bit         byte
	}{
		{op: 0, bits: 0},
		{op: 1, bits: 3},
		{op: 2, bits: 4, bit: 1},
		{op: 2, bits: 1, bit: 2},
		{op: 1, initialBits: 1023, bits: 1},
		{op: 0, bits: 1024},
	} {
		f.Add(seed.op, seed.initialBits, seed.bits, seed.bit)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawInitialBits uint16, rawBits uint16, rawBit byte) {
		opKind := rawOp % 3
		initialBits := uint(rawInitialBits % 1024)
		bits := int64(rawBits % 1030)

		st := newCellSliceState()
		pushCellSliceBuilder(t, st, storeBaseBitsBuilder(initialBits))
		pushCellSliceInt(t, st, bits)

		var (
			op      interface{ Interpret(*vm.State) error }
			wantBit bool
		)
		switch opKind {
		case 0:
			op, wantBit = STZEROES(), false
		case 1:
			op, wantBit = STONES(), true
		default:
			bit := int64(rawBit % 3)
			pushCellSliceInt(t, st, bit)
			if bit > 1 {
				assertCellSliceVMErrorCode(t, STSAME().Interpret(st), vmerr.CodeRangeCheck)
				return
			}
			op, wantBit = STSAME(), bit == 1
		}

		err := op.Interpret(st)
		if bits > 1023 {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeRangeCheck)
			return
		}
		if !cell.BeginCell().MustStoreSlice(make([]byte, (initialBits+7)/8), initialBits).CanExtendBy(uint(bits), 0) {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeCellOverflow)
			return
		}
		if err != nil {
			t.Fatalf("same store failed: %v", err)
		}
		builder := popCellSliceBuilder(t, st)
		if builder.BitsUsed() != initialBits+uint(bits) {
			t.Fatalf("bits used = %d, want %d", builder.BitsUsed(), initialBits+uint(bits))
		}
		if bits == 0 {
			return
		}
		sl := builder.EndCell().MustBeginParse()
		if initialBits > 0 {
			if err := sl.SkipBits(initialBits); err != nil {
				t.Fatalf("skip initial bits: %v", err)
			}
		}
		want := uint64(0)
		if wantBit {
			want = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1)).Uint64()
		}
		if uint(bits) <= 64 && sl.MustLoadUInt(uint(bits)) != want {
			t.Fatalf("stored same-bit value mismatch")
		}
	})
}

func fullBitsStoreBaseBuilder() *cell.Builder {
	return storeBaseBitsBuilder(1023)
}

func storeBaseBitsBuilder(bits uint) *cell.Builder {
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(make([]byte, (bits+7)/8), bits)
	}
	return b
}
