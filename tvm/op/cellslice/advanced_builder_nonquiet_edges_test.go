package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAdvancedBuilderNonQuietTypeAndDepthEdges(t *testing.T) {
	ref := cell.BeginCell().EndCell()
	slice := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()

	tests := []struct {
		name     string
		op       interface{ Interpret(*vm.State) error }
		pushTop  func(*testing.T, *vm.State)
		pushBase func(*testing.T, *vm.State)
	}{
		{name: "STBREFTop", op: STBREF(), pushBase: pushStoreEdgeBuilder, pushTop: pushStoreEdgeCell},
		{name: "STBREFBottom", op: STBREF(), pushBase: pushStoreEdgeCell, pushTop: pushStoreEdgeBuilder},
		{name: "STBREFRTop", op: STBREFR(), pushBase: pushStoreEdgeBuilder, pushTop: pushStoreEdgeCell},
		{name: "STBREFRBottom", op: STBREFR(), pushBase: pushStoreEdgeCell, pushTop: pushStoreEdgeBuilder},
		{name: "STREFRTop", op: STREFR(), pushBase: pushStoreEdgeBuilder, pushTop: pushStoreEdgeBuilder},
		{name: "STREFRBottom", op: STREFR(), pushBase: pushStoreEdgeCell, pushTop: func(t *testing.T, st *vm.State) { pushCellSliceCell(t, st, ref) }},
		{name: "STSLICERTop", op: STSLICER(), pushBase: pushStoreEdgeBuilder, pushTop: pushStoreEdgeCell},
		{name: "STSLICERBottom", op: STSLICER(), pushBase: pushStoreEdgeCell, pushTop: func(t *testing.T, st *vm.State) { pushCellSliceSlice(t, st, slice.Copy()) }},
		{name: "STBRTop", op: STBR(), pushBase: pushStoreEdgeBuilder, pushTop: pushStoreEdgeCell},
		{name: "STBRBottom", op: STBR(), pushBase: pushStoreEdgeCell, pushTop: pushStoreEdgeBuilder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newCellSliceState()
			tt.pushBase(t, st)
			tt.pushTop(t, st)
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	t.Run("STBREFSourceDepthError", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		pushCellSliceBuilder(t, st, cell.BeginCell())
		assertCellSliceVMErrorCode(t, STBREF().Interpret(st), vmerr.CodeCellOverflow)
	})

	t.Run("STBREFRSourceDepthError", func(t *testing.T) {
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, cell.BeginCell())
		pushCellSliceBuilder(t, st, builderWithDeepUncheckedRef(t))
		assertCellSliceVMErrorCode(t, STBREFR().Interpret(st), vmerr.CodeCellOverflow)
	})
}

func FuzzTVMReverseBuilderStoreRules(f *testing.F) {
	for _, seed := range []struct {
		op      byte
		dstBits uint16
		dstRefs byte
		srcBits uint16
		srcRefs byte
	}{
		{op: 0, dstBits: 0, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 1, dstBits: 8, dstRefs: 0, srcBits: 8, srcRefs: 0},
		{op: 0, dstBits: 1023, dstRefs: 0, srcBits: 1, srcRefs: 0},
		{op: 1, dstBits: 0, dstRefs: 4, srcBits: 0, srcRefs: 1},
	} {
		f.Add(seed.op, seed.dstBits, seed.dstRefs, seed.srcBits, seed.srcRefs)
	}

	f.Fuzz(func(t *testing.T, rawOp byte, rawDstBits uint16, rawDstRefs byte, rawSrcBits uint16, rawSrcRefs byte) {
		dstBits := uint(rawDstBits % 1024)
		dstRefs := int(rawDstRefs % 5)
		srcBits := uint(rawSrcBits % 1024)
		srcRefs := int(rawSrcRefs % 5)

		dst := reverseStoreEdgeBuilder(dstBits, dstRefs)
		st := newCellSliceState()
		pushCellSliceBuilder(t, st, dst.Copy())

		if rawOp%2 == 0 {
			src := reverseStoreEdgeBuilder(srcBits, srcRefs)
			pushCellSliceBuilder(t, st, src)
			err := STBR().Interpret(st)
			checkReverseStoreResult(t, st, err, dst, srcBits, srcRefs)
			return
		}

		src := reverseStoreEdgeBuilder(srcBits, srcRefs).ToSlice()
		pushCellSliceSlice(t, st, src)
		err := STSLICER().Interpret(st)
		checkReverseStoreResult(t, st, err, dst, srcBits, srcRefs)
	})
}

func checkReverseStoreResult(t *testing.T, st *vm.State, err error, dst *cell.Builder, srcBits uint, srcRefs int) {
	t.Helper()

	if !dst.CanExtendBy(srcBits, uint(srcRefs)) {
		assertCellSliceVMErrorCode(t, err, vmerr.CodeCellOverflow)
		return
	}
	if err != nil {
		t.Fatalf("reverse store failed: %v", err)
	}
	got := popCellSliceBuilder(t, st)
	if got.BitsUsed() != dst.BitsUsed()+srcBits || got.RefsUsed() != dst.RefsUsed()+srcRefs {
		t.Fatalf("reverse store result=(%d,%d), want=(%d,%d)", got.BitsUsed(), got.RefsUsed(), dst.BitsUsed()+srcBits, dst.RefsUsed()+srcRefs)
	}
}

func pushStoreEdgeBuilder(t *testing.T, st *vm.State) {
	t.Helper()
	pushCellSliceBuilder(t, st, cell.BeginCell())
}

func pushStoreEdgeCell(t *testing.T, st *vm.State) {
	t.Helper()
	pushCellSliceCell(t, st, cell.BeginCell().EndCell())
}

func reverseStoreEdgeBuilder(bits uint, refs int) *cell.Builder {
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(make([]byte, (bits+7)/8), bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().EndCell())
	}
	return b
}
