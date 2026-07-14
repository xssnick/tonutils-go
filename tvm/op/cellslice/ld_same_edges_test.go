package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestLDSameStackTypeRangeAndShapeEdges(t *testing.T) {
	t.Run("StackTypeAndRange", func(t *testing.T) {
		assertCellSliceVMErrorCode(t, LDZEROES().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st := newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDONES().Interpret(st), vmerr.CodeTypeCheck)

		assertCellSliceVMErrorCode(t, LDSAME().Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, ldSameBitsSlice(4, 0b0011))
		pushCellSliceInt(t, st, 2)
		assertCellSliceVMErrorCode(t, LDSAME().Interpret(st), vmerr.CodeRangeCheck)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, ldSameBitsSlice(4, 0b0011))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, LDSAME().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceInt(t, st, 0)
		assertCellSliceVMErrorCode(t, LDSAME().Interpret(st), vmerr.CodeTypeCheck)
	})

	for _, tt := range []struct {
		name      string
		op        interface{ Interpret(*vm.State) error }
		bitArg    *int64
		srcBits   uint
		srcValue  uint64
		wantCount int64
		wantRest  uint
	}{
		{name: "LDZEROES", op: LDZEROES(), srcBits: 5, srcValue: 0b00101, wantCount: 2, wantRest: 3},
		{name: "LDONES", op: LDONES(), srcBits: 4, srcValue: 0b1110, wantCount: 3, wantRest: 1},
		{name: "LDSAMEZero", op: LDSAME(), bitArg: int64Ptr(0), srcBits: 4, srcValue: 0b0001, wantCount: 3, wantRest: 1},
		{name: "LDSAMEOneNoMatch", op: LDSAME(), bitArg: int64Ptr(1), srcBits: 4, srcValue: 0b0111, wantCount: 0, wantRest: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, ldSameBitsSlice(tt.srcBits, tt.srcValue))
			if tt.bitArg != nil {
				pushCellSliceInt(t, st, *tt.bitArg)
			}
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			rest := popCellSliceSlice(t, st)
			if rest.BitsLeft() != tt.wantRest {
				t.Fatalf("%s rest bits = %d, want %d", tt.name, rest.BitsLeft(), tt.wantRest)
			}
			if got := popCellSliceInt(t, st); got != tt.wantCount {
				t.Fatalf("%s count = %d, want %d", tt.name, got, tt.wantCount)
			}
		})
	}

	t.Run("FixedResultPushOverflow", func(t *testing.T) {
		st := newCellSliceState()
		fillLDSameStack(t, st, 1)
		pushCellSliceSlice(t, st, ldSameBitsSlice(1, 0))
		assertCellSliceVMErrorCode(t, LDZEROES().Interpret(st), vmerr.CodeStackOverflow)
	})
}

func FuzzTVMLDSameRules(f *testing.F) {
	for _, seed := range []struct {
		op    byte
		bits  byte
		value uint64
		bit   byte
	}{
		{op: 0, bits: 5, value: 0b00101},
		{op: 1, bits: 4, value: 0b1110},
		{op: 2, bits: 4, value: 0b0001, bit: 0},
		{op: 2, bits: 4, value: 0b0111, bit: 1},
		{op: 2, bits: 4, value: 0b0111, bit: 2},
	} {
		f.Add(seed.op, seed.bits, seed.value, seed.bit)
	}

	f.Fuzz(func(t *testing.T, rawOp, rawBits byte, rawValue uint64, rawBit byte) {
		opKind := rawOp % 3
		bits := uint(rawBits % 65)
		sl := ldSameBitsSlice(bits, rawValue)

		st := newCellSliceState()
		pushCellSliceSlice(t, st, sl.Copy())

		var (
			op      interface{ Interpret(*vm.State) error }
			wantBit bool
		)
		switch opKind {
		case 0:
			op, wantBit = LDZEROES(), false
		case 1:
			op, wantBit = LDONES(), true
		default:
			arg := int64(rawBit % 3)
			pushCellSliceInt(t, st, arg)
			if arg > 1 {
				assertCellSliceVMErrorCode(t, LDSAME().Interpret(st), vmerr.CodeRangeCheck)
				return
			}
			op, wantBit = LDSAME(), arg == 1
		}

		if err := op.Interpret(st); err != nil {
			t.Fatalf("ldsame op failed: %v", err)
		}
		wantCount := int64(sl.Copy().CountLeading(wantBit))
		rest := popCellSliceSlice(t, st)
		if rest.BitsLeft() != bits-uint(wantCount) {
			t.Fatalf("rest bits = %d, want %d", rest.BitsLeft(), bits-uint(wantCount))
		}
		if got := popCellSliceInt(t, st); got != wantCount {
			t.Fatalf("count = %d, want %d", got, wantCount)
		}
	})
}

func fillLDSameStack(t *testing.T, st *vm.State, spare int) {
	t.Helper()

	for {
		err := st.Stack.PushSmallInt(0)
		if err != nil {
			assertCellSliceVMErrorCode(t, err, vmerr.CodeStackOverflow)
			break
		}
	}
	for i := 0; i < spare; i++ {
		if _, err := st.Stack.PopAny(); err != nil {
			t.Fatalf("failed to free stack slot: %v", err)
		}
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func ldSameBitsSlice(bits uint, value uint64) *cell.Slice {
	b := cell.BeginCell()
	for i := uint(0); i < bits; i++ {
		shift := bits - 1 - i
		b.MustStoreUInt((value>>shift)&1, 1)
	}
	return b.EndCell().MustBeginParse()
}
