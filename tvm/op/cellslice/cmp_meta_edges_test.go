package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type compareMetaOp interface {
	Interpret(*vm.State) error
}

func TestExtendedSliceCompareStackTypeAndValueEdges(t *testing.T) {
	ops := []struct {
		name string
		op   compareMetaOp
	}{
		{name: "SDLEXCMP", op: SDLEXCMP()},
		{name: "SDPFX", op: SDPFX()},
		{name: "SDPFXREV", op: SDPFXREV()},
		{name: "SDPPFX", op: SDPPFX()},
		{name: "SDPPFXREV", op: SDPPFXREV()},
		{name: "SDSFX", op: SDSFX()},
		{name: "SDSFXREV", op: SDSFXREV()},
		{name: "SDPSFX", op: SDPSFX()},
		{name: "SDPSFXREV", op: SDPSFXREV()},
	}

	for _, tt := range ops {
		t.Run(tt.name+"StackUnderflow", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, compareMetaBitsSlice(1, 1))
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"TopType", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, compareMetaBitsSlice(1, 1))
			pushCellSliceCell(t, st, cell.BeginCell().EndCell())
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"BottomType", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceCell(t, st, cell.BeginCell().EndCell())
			pushCellSliceSlice(t, st, compareMetaBitsSlice(1, 1))
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	runLexCompare := func(name string, left, right *cell.Slice, want int64) {
		t.Helper()

		st := newCellSliceState()
		pushCellSliceSlice(t, st, left)
		pushCellSliceSlice(t, st, right)
		if err := SDLEXCMP().Interpret(st); err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		if got := popCellSliceInt(t, st); got != want {
			t.Fatalf("%s result = %d, want %d", name, got, want)
		}
	}

	runLexCompare("Equal", compareMetaBitsSlice(6, 0b101100), compareMetaBitsSlice(6, 0b101100), 0)
	runLexCompare("Greater", compareMetaBitsSlice(6, 0b101101), compareMetaBitsSlice(6, 0b101100), 1)
	runLexCompare("ShorterPrefix", compareMetaBitsSlice(3, 0b101), compareMetaBitsSlice(6, 0b101100), -1)

	for _, tt := range []struct {
		name  string
		op    compareMetaOp
		left  *cell.Slice
		right *cell.Slice
		want  bool
	}{
		{name: "SDPFXEqual", op: SDPFX(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(6, 0b101100), want: true},
		{name: "SDPPFXEqualFalse", op: SDPPFX(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(6, 0b101100), want: false},
		{name: "SDPFXREVHit", op: SDPFXREV(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(3, 0b101), want: true},
		{name: "SDPPFXREVHit", op: SDPPFXREV(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(3, 0b101), want: true},
		{name: "SDSFXEqual", op: SDSFX(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(6, 0b101100), want: true},
		{name: "SDPSFXEqualFalse", op: SDPSFX(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(6, 0b101100), want: false},
		{name: "SDSFXREVHit", op: SDSFXREV(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(3, 0b100), want: true},
		{name: "SDPSFXREVHit", op: SDPSFXREV(), left: compareMetaBitsSlice(6, 0b101100), right: compareMetaBitsSlice(3, 0b100), want: true},
		{name: "SDPFXMiss", op: SDPFX(), left: compareMetaBitsSlice(3, 0b100), right: compareMetaBitsSlice(6, 0b101100), want: false},
		{name: "SDSFXMiss", op: SDSFX(), left: compareMetaBitsSlice(3, 0b101), right: compareMetaBitsSlice(6, 0b101100), want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, tt.left.Copy())
			pushCellSliceSlice(t, st, tt.right.Copy())
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			if got := popCellSliceBool(t, st); got != tt.want {
				t.Fatalf("%s result = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestAdvancedMetaStackTypeRangeAndDeserializeEdges(t *testing.T) {
	needle := compareMetaBitsSlice(3, 0b101)

	t.Run("SDBEGINSXStackAndTypes", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, compareMetaBitsSlice(5, 0b10111))
		assertCellSliceVMErrorCode(t, SDBEGINSX().Interpret(st), vmerr.CodeStackUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, compareMetaBitsSlice(5, 0b10111))
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		assertCellSliceVMErrorCode(t, SDBEGINSX().Interpret(st), vmerr.CodeTypeCheck)

		st = newCellSliceState()
		pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		pushCellSliceSlice(t, st, needle.Copy())
		assertCellSliceVMErrorCode(t, SDBEGINSX().Interpret(st), vmerr.CodeTypeCheck)
	})

	t.Run("SDBEGINSXQuietAndNonQuietMiss", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceSlice(t, st, compareMetaBitsSlice(5, 0b00111))
		pushCellSliceSlice(t, st, needle.Copy())
		assertCellSliceVMErrorCode(t, SDBEGINSX().Interpret(st), vmerr.CodeCellUnderflow)

		st = newCellSliceState()
		pushCellSliceSlice(t, st, compareMetaBitsSlice(3, 0b101))
		pushCellSliceSlice(t, st, needle.Copy())
		if err := SDBEGINSXQ().Interpret(st); err != nil {
			t.Fatalf("SDBEGINSXQ exact match failed: %v", err)
		}
		if ok := popCellSliceBool(t, st); !ok {
			t.Fatal("SDBEGINSXQ exact match flag = false")
		}
		if rest := popCellSliceSlice(t, st); rest.BitsLeft() != 0 {
			t.Fatalf("SDBEGINSXQ exact match remainder bits = %d, want 0", rest.BitsLeft())
		}
	})

	for _, tt := range []struct {
		name string
		op   compareMetaOp
	}{
		{name: "CHASHI", op: CHASHI(3)},
		{name: "CDEPTHI", op: CDEPTHI(3)},
		{name: "CHASHIX", op: CHASHIX()},
		{name: "CDEPTHIX", op: CDEPTHIX()},
	} {
		t.Run(tt.name+"TypeErrors", func(t *testing.T) {
			st := newCellSliceState()
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)

			st = newCellSliceState()
			pushCellSliceSlice(t, st, compareMetaBitsSlice(1, 1))
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	for _, tt := range []struct {
		name   string
		op     *helpers.AdvancedOP
		prefix uint64
	}{
		{name: "CHASHI", op: CHASHI(3), prefix: 0xD768 >> 2},
		{name: "CDEPTHI", op: CDEPTHI(3), prefix: 0xD76C >> 2},
	} {
		t.Run(tt.name+"StaticMetadata", func(t *testing.T) {
			if got := tt.op.MinGlobalVersion(); got != 6 {
				t.Fatalf("%s min version = %d, want 6", tt.name, got)
			}
			if got := tt.op.InstructionBits(); got != 16 {
				t.Fatalf("%s instruction bits = %d, want 16", tt.name, got)
			}
			if err := tt.op.Deserialize(cell.BeginCell().MustStoreUInt(tt.prefix, 14).EndCell().MustBeginParse()); err == nil {
				t.Fatalf("%s short suffix decoded", tt.name)
			}
		})
	}

	for _, tt := range []struct {
		name string
		op   compareMetaOp
	}{
		{name: "CHASHIX", op: CHASHIX()},
		{name: "CDEPTHIX", op: CDEPTHIX()},
	} {
		t.Run(tt.name+"RangePrecedesMissingCell", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceInt(t, st, 4)
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeRangeCheck)
		})

		t.Run(tt.name+"MissingCellAfterValidIndex", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceInt(t, st, 0)
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"CellTypeAfterValidIndex", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, compareMetaBitsSlice(1, 1))
			pushCellSliceInt(t, st, 0)
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}
}

func FuzzTVMExtendedSliceCompareRules(f *testing.F) {
	for _, seed := range []struct {
		op        byte
		leftBits  byte
		rightBits byte
		left      uint64
		right     uint64
	}{
		{op: 0, leftBits: 6, rightBits: 6, left: 0b101100, right: 0b101101},
		{op: 0, leftBits: 6, rightBits: 6, left: 0b101100, right: 0b101100},
		{op: 1, leftBits: 3, rightBits: 6, left: 0b101, right: 0b101100},
		{op: 3, leftBits: 6, rightBits: 6, left: 0b101100, right: 0b101100},
		{op: 5, leftBits: 3, rightBits: 6, left: 0b100, right: 0b101100},
		{op: 7, leftBits: 6, rightBits: 6, left: 0b101100, right: 0b101100},
	} {
		f.Add(seed.op, seed.leftBits, seed.rightBits, seed.left, seed.right)
	}

	f.Fuzz(func(t *testing.T, rawOp, rawLeftBits, rawRightBits byte, rawLeft, rawRight uint64) {
		leftBits := uint(rawLeftBits % 17)
		rightBits := uint(rawRightBits % 17)
		left := compareMetaBitsSlice(leftBits, rawLeft)
		right := compareMetaBitsSlice(rightBits, rawRight)

		opKind := rawOp % 9
		st := newCellSliceState()
		pushCellSliceSlice(t, st, left.Copy())
		pushCellSliceSlice(t, st, right.Copy())

		switch opKind {
		case 0:
			if err := SDLEXCMP().Interpret(st); err != nil {
				t.Fatalf("SDLEXCMP failed: %v", err)
			}
			if got, want := popCellSliceInt(t, st), int64(left.Copy().LexCompare(right.Copy())); got != want {
				t.Fatalf("SDLEXCMP result = %d, want %d", got, want)
			}
		default:
			op, want := compareBoolOpAndExpected(opKind, left.Copy(), right.Copy())
			if err := op.Interpret(st); err != nil {
				t.Fatalf("op=%d failed: %v", opKind, err)
			}
			if got := popCellSliceBool(t, st); got != want {
				t.Fatalf("op=%d result = %v, want %v", opKind, got, want)
			}
		}

		if st.Stack.Len() != 0 {
			t.Fatalf("stack depth = %d, want 0", st.Stack.Len())
		}
	})
}

func compareBoolOpAndExpected(kind byte, left, right *cell.Slice) (compareMetaOp, bool) {
	switch kind {
	case 1:
		return SDPFX(), left.IsPrefixOf(right)
	case 2:
		return SDPFXREV(), right.IsPrefixOf(left)
	case 3:
		return SDPPFX(), left.IsProperPrefixOf(right)
	case 4:
		return SDPPFXREV(), right.IsProperPrefixOf(left)
	case 5:
		return SDSFX(), left.IsSuffixOf(right)
	case 6:
		return SDSFXREV(), right.IsSuffixOf(left)
	case 7:
		return SDPSFX(), left.IsProperSuffixOf(right)
	default:
		return SDPSFXREV(), right.IsProperSuffixOf(left)
	}
}

func compareMetaBitsSlice(bits uint, value uint64) *cell.Slice {
	b := cell.BeginCell()
	for i := uint(0); i < bits; i++ {
		shift := bits - 1 - i
		b.MustStoreUInt((value>>shift)&1, 1)
	}
	return b.ToSlice()
}
