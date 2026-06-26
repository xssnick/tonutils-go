package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAdvancedBuilderQuietStoreTypeAndDepthEdges(t *testing.T) {
	for _, tt := range []struct {
		name  string
		op    cellSliceTransformOp
		kinds []string
	}{
		{name: "STREFQ", op: STREFQ(), kinds: []string{"cell", "builder"}},
		{name: "STBREFQ", op: STBREFQ(), kinds: []string{"builder", "builder"}},
		{name: "STSLICEQ", op: STSLICEQ(), kinds: []string{"slice", "builder"}},
		{name: "STBQ", op: STBQ(), kinds: []string{"builder", "builder"}},
		{name: "STBREFRQ", op: STBREFRQ(), kinds: []string{"builder", "builder"}},
		{name: "STREFRQ", op: STREFRQ(), kinds: []string{"builder", "cell"}},
		{name: "STBRQ", op: STBRQ(), kinds: []string{"builder", "builder"}},
		{name: "STSLICERQ", op: STSLICERQ(), kinds: []string{"builder", "slice"}},
	} {
		t.Run(tt.name+"TopType", func(t *testing.T) {
			st := newCellSliceState()
			pushQuietStoreKinds(t, st, tt.kinds[0], wrongQuietStoreKind(tt.kinds[1]))
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})

		t.Run(tt.name+"BottomType", func(t *testing.T) {
			st := newCellSliceState()
			pushQuietStoreKinds(t, st, wrongQuietStoreKind(tt.kinds[0]), tt.kinds[1])
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	t.Run("STBREFQSourceDepthErrorIsNotQuiet", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STBREFQ().Interpret(st), vmerr.CodeCellOverflow)
	})

	t.Run("STBREFRQSourceDepthErrorIsNotQuiet", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		assertCellSliceVMErrorCode(t, STBREFRQ().Interpret(st), vmerr.CodeCellOverflow)
	})
}

func FuzzTVMQuietBuilderStoreShapeRules(f *testing.F) {
	for _, seed := range []struct {
		op      byte
		dstBits uint16
		dstRefs byte
		srcBits uint16
		srcRefs byte
	}{
		{op: 0, dstBits: 0, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 0, dstBits: 1016, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 0, dstBits: 0, dstRefs: 4, srcBits: 0, srcRefs: 1},
		{op: 1, dstBits: 0, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 1, dstBits: 1016, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 2, dstBits: 0, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 2, dstBits: 0, dstRefs: 4, srcBits: 0, srcRefs: 1},
		{op: 3, dstBits: 0, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 3, dstBits: 1016, dstRefs: 0, srcBits: 8, srcRefs: 0},
	} {
		f.Add(seed.op, seed.dstBits, seed.dstRefs, seed.srcBits, seed.srcRefs)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawDstBits uint16, rawDstRefs byte, rawSrcBits uint16, rawSrcRefs byte) {
		dstBits := uint(rawDstBits % 1024)
		dstRefs := int(rawDstRefs % 5)
		srcBits := uint(rawSrcBits % 1024)
		srcRefs := int(rawSrcRefs % 5)
		dst := quietStoreEdgeBuilder(t, dstBits, dstRefs)
		srcBuilder := quietStoreEdgeBuilder(t, srcBits, srcRefs)
		srcSlice := srcBuilder.ToSlice()
		wantFail := !dst.CanExtendBy(srcBits, uint(srcRefs))

		st := newCellSliceState()
		var op cellSliceTransformOp
		switch rawOp % 4 {
		case 0:
			op = STSLICEQ()
			pushCellSliceSlice(t, st, srcSlice.Copy())
			pushCellSliceBuilder(t, st, dst.Copy())
		case 1:
			op = STSLICERQ()
			pushCellSliceBuilder(t, st, dst.Copy())
			pushCellSliceSlice(t, st, srcSlice.Copy())
		case 2:
			op = STBQ()
			pushCellSliceBuilder(t, st, srcBuilder.Copy())
			pushCellSliceBuilder(t, st, dst.Copy())
		default:
			op = STBRQ()
			pushCellSliceBuilder(t, st, dst.Copy())
			pushCellSliceBuilder(t, st, srcBuilder.Copy())
		}

		if err := op.Interpret(st); err != nil {
			t.Fatalf("quiet store op=%d dst=(%d,%d) src=(%d,%d): %v", rawOp%4, dstBits, dstRefs, srcBits, srcRefs, err)
		}
		status := popCellSliceInt(t, st)
		if wantFail {
			if status != -1 {
				t.Fatalf("failure status = %d, want -1", status)
			}
			if st.Stack.Len() != 2 {
				t.Fatalf("failure stack depth = %d, want 2", st.Stack.Len())
			}
			return
		}
		if status != 0 {
			t.Fatalf("success status = %d, want 0", status)
		}
		if st.Stack.Len() != 1 {
			t.Fatalf("success stack depth = %d, want 1", st.Stack.Len())
		}
		got := popCellSliceBuilder(t, st)
		if got.BitsUsed() != dstBits+srcBits || got.RefsUsed() != dstRefs+srcRefs {
			t.Fatalf("stored builder shape = (%d,%d), want (%d,%d)", got.BitsUsed(), got.RefsUsed(), dstBits+srcBits, dstRefs+srcRefs)
		}
	})
}

func pushQuietStoreKinds(t *testing.T, st *vm.State, kinds ...string) {
	t.Helper()
	for _, kind := range kinds {
		switch kind {
		case "cell":
			pushCellSliceCell(t, st, cell.BeginCell().EndCell())
		case "builder":
			pushCellSliceBuilder(t, st, cell.BeginCell())
		case "slice":
			pushCellSliceSlice(t, st, cell.BeginCell().ToSlice())
		default:
			t.Fatalf("unknown quiet store kind %q", kind)
		}
	}
}

func wrongQuietStoreKind(kind string) string {
	if kind == "builder" {
		return "cell"
	}
	return "builder"
}

func quietStoreEdgeBuilder(t *testing.T, bits uint, refs int) *cell.Builder {
	t.Helper()
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(make([]byte, (bits+7)/8), bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
	}
	return b
}
