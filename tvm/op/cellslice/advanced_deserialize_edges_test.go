package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type cellSliceTransformOp interface {
	Interpret(*vm.State) error
}

func TestAdvancedSliceTransformRefsAndBoundaries(t *testing.T) {
	refA := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()

	for _, tt := range []struct {
		name      string
		op        cellSliceTransformOp
		args      []int64
		wantBits  uint
		wantValue uint64
		wantRefs  []*cell.Cell
	}{
		{name: "SDCUTFIRSTZeroBitsDropsRefs", op: SDCUTFIRST(), args: []int64{0}, wantBits: 0},
		{name: "SDCUTLASTKeepsLastBitsDropsRefs", op: SDCUTLAST(), args: []int64{3}, wantBits: 3, wantValue: 0b101},
		{name: "SDSKIPLASTKeepsPrefixAndRefs", op: SDSKIPLAST(), args: []int64{2}, wantBits: 4, wantValue: 0b1101, wantRefs: []*cell.Cell{refA, refB}},
		{name: "SDSUBSTRSkipsThenDropsRefs", op: SDSUBSTR(), args: []int64{1, 3}, wantBits: 3, wantValue: 0b101},
		{name: "SCUTFIRSTKeepsFirstRef", op: SCUTFIRST(), args: []int64{4, 1}, wantBits: 4, wantValue: 0b1101, wantRefs: []*cell.Cell{refA}},
		{name: "SSKIPFIRSTSkipsFirstRef", op: SSKIPFIRST(), args: []int64{2, 1}, wantBits: 4, wantValue: 0b0101, wantRefs: []*cell.Cell{refB}},
		{name: "SCUTLASTKeepsLastRef", op: SCUTLAST(), args: []int64{3, 1}, wantBits: 3, wantValue: 0b101, wantRefs: []*cell.Cell{refB}},
		{name: "SSKIPLASTDropsLastRef", op: SSKIPLAST(), args: []int64{3, 1}, wantBits: 3, wantValue: 0b110, wantRefs: []*cell.Cell{refA}},
		{name: "SUBSLICESkipsAndKeepsRequestedRefs", op: SUBSLICE(), args: []int64{2, 1, 3, 1}, wantBits: 3, wantValue: 0b010, wantRefs: []*cell.Cell{refB}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, advancedTransformSlice(refA, refB))
			for _, arg := range tt.args {
				pushCellSliceInt(t, st, arg)
			}

			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}

			got := popCellSliceSlice(t, st)
			assertAdvancedTransformSlice(t, got, tt.wantBits, tt.wantValue, tt.wantRefs)
			if st.Stack.Len() != 0 {
				t.Fatalf("%s stack depth = %d, want 0", tt.name, st.Stack.Len())
			}
		})
	}
}

func TestAdvancedSliceTransformStackTypeAndRangeErrors(t *testing.T) {
	for _, tt := range []struct {
		name  string
		op    cellSliceTransformOp
		depth int
		args  []int64
	}{
		{name: "SDCUTFIRST", op: SDCUTFIRST(), depth: 2, args: []int64{0}},
		{name: "SDCUTLAST", op: SDCUTLAST(), depth: 2, args: []int64{0}},
		{name: "SDSKIPLAST", op: SDSKIPLAST(), depth: 2, args: []int64{0}},
		{name: "SDSUBSTR", op: SDSUBSTR(), depth: 3, args: []int64{0, 0}},
		{name: "SCUTFIRST", op: SCUTFIRST(), depth: 3, args: []int64{0, 0}},
		{name: "SSKIPFIRST", op: SSKIPFIRST(), depth: 3, args: []int64{0, 0}},
		{name: "SCUTLAST", op: SCUTLAST(), depth: 3, args: []int64{0, 0}},
		{name: "SSKIPLAST", op: SSKIPLAST(), depth: 3, args: []int64{0, 0}},
		{name: "SUBSLICE", op: SUBSLICE(), depth: 5, args: []int64{0, 0, 0, 0}},
	} {
		t.Run(tt.name+"StackUnderflow", func(t *testing.T) {
			st := newCellSliceState()
			for i := 0; i < tt.depth-1; i++ {
				pushCellSliceInt(t, st, 0)
			}
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"PopSliceType", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceCell(t, st, cell.BeginCell().EndCell())
			for _, arg := range tt.args {
				pushCellSliceInt(t, st, arg)
			}
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	for _, tt := range []struct {
		name string
		op   cellSliceTransformOp
		args []int64
	}{
		{name: "SDCUTFIRSTBitsTooLarge", op: SDCUTFIRST(), args: []int64{1024}},
		{name: "SDCUTFIRSTNegativeBits", op: SDCUTFIRST(), args: []int64{-1}},
		{name: "SDCUTLASTBitsTooLarge", op: SDCUTLAST(), args: []int64{1024}},
		{name: "SDSKIPLASTBitsTooLarge", op: SDSKIPLAST(), args: []int64{1024}},
		{name: "SDSUBSTRBitsTooLarge", op: SDSUBSTR(), args: []int64{0, 1024}},
		{name: "SDSUBSTROffsetTooLarge", op: SDSUBSTR(), args: []int64{1024, 0}},
		{name: "SCUTFIRSTRefsTooLarge", op: SCUTFIRST(), args: []int64{0, 5}},
		{name: "SCUTFIRSTNegativeRefs", op: SCUTFIRST(), args: []int64{0, -1}},
		{name: "SCUTFIRSTBitsTooLarge", op: SCUTFIRST(), args: []int64{1024, 0}},
		{name: "SSKIPFIRSTRefsTooLarge", op: SSKIPFIRST(), args: []int64{0, 5}},
		{name: "SSKIPFIRSTBitsTooLarge", op: SSKIPFIRST(), args: []int64{1024, 0}},
		{name: "SCUTLASTRefsTooLarge", op: SCUTLAST(), args: []int64{0, 5}},
		{name: "SCUTLASTBitsTooLarge", op: SCUTLAST(), args: []int64{1024, 0}},
		{name: "SSKIPLASTRefsTooLarge", op: SSKIPLAST(), args: []int64{0, 5}},
		{name: "SSKIPLASTBitsTooLarge", op: SSKIPLAST(), args: []int64{1024, 0}},
		{name: "SUBSLICER2TooLarge", op: SUBSLICE(), args: []int64{0, 0, 0, 5}},
		{name: "SUBSLICENegativeR2", op: SUBSLICE(), args: []int64{0, 0, 0, -1}},
		{name: "SUBSLICEL2TooLarge", op: SUBSLICE(), args: []int64{0, 0, 1024, 0}},
		{name: "SUBSLICER1TooLarge", op: SUBSLICE(), args: []int64{0, 5, 0, 0}},
		{name: "SUBSLICEL1TooLarge", op: SUBSLICE(), args: []int64{1024, 0, 0, 0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, advancedTransformSlice())
			for _, arg := range tt.args {
				pushCellSliceInt(t, st, arg)
			}
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeRangeCheck)
		})
	}
}

func TestAdvancedSliceTransformCellUnderflowEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   cellSliceTransformOp
		args []int64
	}{
		{name: "SDCUTFIRSTBits", op: SDCUTFIRST(), args: []int64{7}},
		{name: "SDCUTLASTBits", op: SDCUTLAST(), args: []int64{7}},
		{name: "SDSKIPLASTBits", op: SDSKIPLAST(), args: []int64{7}},
		{name: "SDSUBSTRSkipBits", op: SDSUBSTR(), args: []int64{7, 0}},
		{name: "SDSUBSTRTakeBitsAfterSkip", op: SDSUBSTR(), args: []int64{4, 3}},
		{name: "SCUTFIRSTBits", op: SCUTFIRST(), args: []int64{7, 0}},
		{name: "SCUTFIRSTRefs", op: SCUTFIRST(), args: []int64{0, 3}},
		{name: "SSKIPFIRSTBits", op: SSKIPFIRST(), args: []int64{7, 0}},
		{name: "SSKIPFIRSTRefs", op: SSKIPFIRST(), args: []int64{0, 3}},
		{name: "SCUTLASTBits", op: SCUTLAST(), args: []int64{7, 0}},
		{name: "SCUTLASTRefs", op: SCUTLAST(), args: []int64{0, 3}},
		{name: "SSKIPLASTBits", op: SSKIPLAST(), args: []int64{7, 0}},
		{name: "SSKIPLASTRefs", op: SSKIPLAST(), args: []int64{0, 3}},
		{name: "SUBSLICESkipBits", op: SUBSLICE(), args: []int64{7, 0, 0, 0}},
		{name: "SUBSLICESkipRefs", op: SUBSLICE(), args: []int64{0, 3, 0, 0}},
		{name: "SUBSLICETakeBitsAfterSkip", op: SUBSLICE(), args: []int64{4, 0, 3, 0}},
		{name: "SUBSLICETakeRefsAfterSkip", op: SUBSLICE(), args: []int64{0, 2, 0, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, advancedTransformSlice(
				cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell(),
				cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell(),
			))
			for _, arg := range tt.args {
				pushCellSliceInt(t, st, arg)
			}
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeCellUnderflow)
		})
	}
}

func FuzzTVMAdvancedSliceTransformRules(f *testing.F) {
	for _, seed := range []struct {
		op       byte
		haveBits uint16
		haveRefs byte
		a        int64
		b        int64
		c        int64
		d        int64
	}{
		{op: 0, haveBits: 6, haveRefs: 2, a: 3},
		{op: 2, haveBits: 6, haveRefs: 2, a: 2},
		{op: 3, haveBits: 6, haveRefs: 2, a: 2, b: 3},
		{op: 4, haveBits: 6, haveRefs: 2, a: 4, b: 1},
		{op: 8, haveBits: 6, haveRefs: 2, a: 2, b: 1, c: 3, d: 1},
		{op: 8, haveBits: 6, haveRefs: 2, a: 4, b: 0, c: 3, d: 0},
		{op: 8, haveBits: 6, haveRefs: 2, a: 0, b: 3, c: 0, d: 0},
		{op: 4, haveBits: 6, haveRefs: 2, a: 1024, b: 0},
		{op: 4, haveBits: 6, haveRefs: 2, a: 0, b: 5},
		{op: 0, haveBits: 6, haveRefs: 2, a: -1},
	} {
		f.Add(seed.op, seed.haveBits, seed.haveRefs, seed.a, seed.b, seed.c, seed.d)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawHaveBits uint16, rawHaveRefs byte, a, b, c, d int64) {
		haveBits := uint(rawHaveBits % 1024)
		haveRefs := int(rawHaveRefs % 5)
		opKind := rawOp % 9

		st := newCellSliceState()
		pushCellSliceSlice(t, st, advancedTransformFuzzSlice(t, haveBits, haveRefs))

		var op cellSliceTransformOp
		var args []int64
		switch opKind {
		case 0:
			op, args = SDCUTFIRST(), []int64{boundedTransformArg(a, 1030)}
		case 1:
			op, args = SDCUTLAST(), []int64{boundedTransformArg(a, 1030)}
		case 2:
			op, args = SDSKIPLAST(), []int64{boundedTransformArg(a, 1030)}
		case 3:
			op, args = SDSUBSTR(), []int64{boundedTransformArg(a, 1030), boundedTransformArg(b, 1030)}
		case 4:
			op, args = SCUTFIRST(), []int64{boundedTransformArg(a, 1030), boundedTransformArg(b, 8)}
		case 5:
			op, args = SSKIPFIRST(), []int64{boundedTransformArg(a, 1030), boundedTransformArg(b, 8)}
		case 6:
			op, args = SCUTLAST(), []int64{boundedTransformArg(a, 1030), boundedTransformArg(b, 8)}
		case 7:
			op, args = SSKIPLAST(), []int64{boundedTransformArg(a, 1030), boundedTransformArg(b, 8)}
		default:
			op = SUBSLICE()
			args = []int64{
				boundedTransformArg(a, 1030),
				boundedTransformArg(b, 8),
				boundedTransformArg(c, 1030),
				boundedTransformArg(d, 8),
			}
		}

		for _, arg := range args {
			pushCellSliceInt(t, st, arg)
		}

		err := op.Interpret(st)
		wantBits, wantRefs, wantCode := advancedTransformExpected(opKind, haveBits, haveRefs, args)
		if wantCode != 0 {
			assertCellSliceVMErrorCode(t, err, wantCode)
			return
		}
		if err != nil {
			t.Fatalf("op=%d args=%v have=(%d,%d) failed: %v", opKind, args, haveBits, haveRefs, err)
		}
		got := popCellSliceSlice(t, st)
		if got.BitsLeft() != wantBits || got.RefsNum() != wantRefs {
			t.Fatalf("op=%d args=%v result=(%d,%d), want=(%d,%d)", opKind, args, got.BitsLeft(), got.RefsNum(), wantBits, wantRefs)
		}
	})
}

func advancedTransformSlice(refs ...*cell.Cell) *cell.Slice {
	b := cell.BeginCell().MustStoreUInt(0b110101, 6)
	for _, ref := range refs {
		b.MustStoreRef(ref)
	}
	return b.EndCell().MustBeginParse()
}

func advancedTransformFuzzSlice(t *testing.T, bits uint, refs int) *cell.Slice {
	t.Helper()

	buf := make([]byte, (bits+7)/8)
	for i := range buf {
		buf[i] = byte(0xA5 + i)
	}
	b := cell.BeginCell().MustStoreSlice(buf, bits)
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b.EndCell().MustBeginParse()
}

func assertAdvancedTransformSlice(t *testing.T, got *cell.Slice, wantBits uint, wantValue uint64, wantRefs []*cell.Cell) {
	t.Helper()

	if got.BitsLeft() != wantBits {
		t.Fatalf("bits = %d, want %d", got.BitsLeft(), wantBits)
	}
	if wantBits > 0 {
		if value := got.MustLoadUInt(wantBits); value != wantValue {
			t.Fatalf("value = %b, want %b", value, wantValue)
		}
	}
	if got.RefsNum() != len(wantRefs) {
		t.Fatalf("refs = %d, want %d", got.RefsNum(), len(wantRefs))
	}
	for i, want := range wantRefs {
		ref, err := got.LoadRefCell()
		if err != nil {
			t.Fatalf("load ref %d: %v", i, err)
		}
		if !sameCellHash(ref, want) {
			t.Fatalf("ref %d mismatch", i)
		}
	}
}

func boundedTransformArg(v int64, mod int64) int64 {
	if v < 0 {
		v = -v
	}
	return v % mod
}

func advancedTransformExpected(op byte, haveBits uint, haveRefs int, args []int64) (uint, int, int64) {
	for _, arg := range args {
		if arg < 0 {
			return 0, 0, vmerr.CodeRangeCheck
		}
	}

	switch op {
	case 0, 1:
		bits := args[0]
		if bits > 1023 {
			return 0, 0, vmerr.CodeRangeCheck
		}
		if uint(bits) > haveBits {
			return 0, 0, vmerr.CodeCellUnderflow
		}
		return uint(bits), 0, 0
	case 2:
		bits := args[0]
		if bits > 1023 {
			return 0, 0, vmerr.CodeRangeCheck
		}
		if uint(bits) > haveBits {
			return 0, 0, vmerr.CodeCellUnderflow
		}
		return haveBits - uint(bits), haveRefs, 0
	case 3:
		offset, bits := args[0], args[1]
		if bits > 1023 || offset > 1023 {
			return 0, 0, vmerr.CodeRangeCheck
		}
		if uint(offset) > haveBits || uint(bits) > haveBits-uint(offset) {
			return 0, 0, vmerr.CodeCellUnderflow
		}
		return uint(bits), 0, 0
	case 4, 6:
		bits, refs := args[0], args[1]
		if refs > 4 || bits > 1023 {
			return 0, 0, vmerr.CodeRangeCheck
		}
		if uint(bits) > haveBits || int(refs) > haveRefs {
			return 0, 0, vmerr.CodeCellUnderflow
		}
		return uint(bits), int(refs), 0
	case 5, 7:
		bits, refs := args[0], args[1]
		if refs > 4 || bits > 1023 {
			return 0, 0, vmerr.CodeRangeCheck
		}
		if uint(bits) > haveBits || int(refs) > haveRefs {
			return 0, 0, vmerr.CodeCellUnderflow
		}
		return haveBits - uint(bits), haveRefs - int(refs), 0
	default:
		l1, r1, l2, r2 := args[0], args[1], args[2], args[3]
		if r2 > 4 || l2 > 1023 || r1 > 4 || l1 > 1023 {
			return 0, 0, vmerr.CodeRangeCheck
		}
		if uint(l1) > haveBits || int(r1) > haveRefs || uint(l2) > haveBits-uint(l1) || int(r2) > haveRefs-int(r1) {
			return 0, 0, vmerr.CodeCellUnderflow
		}
		return uint(l2), int(r2), 0
	}
}
