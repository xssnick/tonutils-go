package cellslice

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestCompareFamiliesUseBitOnlySemantics(t *testing.T) {
	st := vm.NewStack()
	left := cell.BeginCell().MustStoreUInt(0b1010, 4).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()
	right := cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse()

	if err := st.PushSlice(left); err != nil {
		t.Fatalf("push left: %v", err)
	}
	if err := st.PushSlice(right); err != nil {
		t.Fatalf("push right: %v", err)
	}
	if err := SDEQ().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("sdeq: %v", err)
	}
	ok, err := st.PopBool()
	if err != nil {
		t.Fatalf("pop sdeq result: %v", err)
	}
	if !ok {
		t.Fatalf("sdeq should ignore refs")
	}

	st = vm.NewStack()
	if err = st.PushSlice(cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("push left cmp: %v", err)
	}
	if err = st.PushSlice(cell.BeginCell().MustStoreUInt(0b1011, 4).EndCell().BeginParse()); err != nil {
		t.Fatalf("push right cmp: %v", err)
	}
	if err = SDLEXCMP().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("sdlexcmp: %v", err)
	}
	cmp, err := st.PopIntFinite()
	if err != nil {
		t.Fatalf("pop sdlexcmp result: %v", err)
	}
	if cmp.Int64() != -1 {
		t.Fatalf("expected -1, got %d", cmp.Int64())
	}
}

func TestSDBeginsAndLittleEndianOps(t *testing.T) {
	st := vm.NewStack()
	hay := cell.BeginCell().MustStoreUInt(0b101101, 6).EndCell().BeginParse()
	needle := cell.BeginCell().MustStoreUInt(0b111, 3).EndCell().BeginParse()
	if err := st.PushSlice(hay); err != nil {
		t.Fatalf("push hay: %v", err)
	}
	if err := st.PushSlice(needle); err != nil {
		t.Fatalf("push needle: %v", err)
	}
	if err := SDBEGINSXQ().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("sdbeginsxq: %v", err)
	}
	ok, err := st.PopBool()
	if err != nil {
		t.Fatalf("pop sdbeginsxq result: %v", err)
	}
	if ok {
		t.Fatalf("sdbeginsxq should fail quietly")
	}
	rest, err := st.PopSlice()
	if err != nil {
		t.Fatalf("pop preserved slice: %v", err)
	}
	if rest.BitsLeft() != 6 {
		t.Fatalf("expected original slice back")
	}

	st = vm.NewStack()
	if err = st.PushSlice(cell.BeginCell().MustStoreSlice([]byte{0xFE, 0xFF, 0xFF, 0xFF}, 32).EndCell().BeginParse()); err != nil {
		t.Fatalf("push ldile4 slice: %v", err)
	}
	if err = LDILE4().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("ldile4: %v", err)
	}
	rest, err = st.PopSlice()
	if err != nil {
		t.Fatalf("pop ldile4 rest: %v", err)
	}
	val, err := st.PopIntFinite()
	if err != nil {
		t.Fatalf("pop ldile4 value: %v", err)
	}
	if val.Int64() != -2 || rest.BitsLeft() != 0 {
		t.Fatalf("expected -2 and empty slice, got %d and %d bits", val.Int64(), rest.BitsLeft())
	}

	st = vm.NewStack()
	if err = st.PushInt(big.NewInt(0x11223344)); err != nil {
		t.Fatalf("push value: %v", err)
	}
	if err = st.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("push builder: %v", err)
	}
	if err = STULE4().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("stule4: %v", err)
	}
	builder, err := st.PopBuilder()
	if err != nil {
		t.Fatalf("pop stule4 builder: %v", err)
	}
	got, err := builder.ToSlice().LoadSlice(32)
	if err != nil {
		t.Fatalf("load stule4 bytes: %v", err)
	}
	if !bytes.Equal(got, []byte{0x44, 0x33, 0x22, 0x11}) {
		t.Fatalf("unexpected little-endian bytes %x", got)
	}
}

func TestConstStoreRemainingAndIndexedDepth(t *testing.T) {
	refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	refB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()

	st := vm.NewStack()
	if err := st.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("push builder: %v", err)
	}
	if err := STREF2CONST(refA, refB).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("stref2const: %v", err)
	}
	builder, err := st.PopBuilder()
	if err != nil {
		t.Fatalf("pop stref2const builder: %v", err)
	}
	if builder.RefsUsed() != 2 {
		t.Fatalf("expected 2 refs, got %d", builder.RefsUsed())
	}

	st = vm.NewStack()
	src := cell.BeginCell().MustStoreUInt(0b10101, 5).MustStoreRef(refA).EndCell().BeginParse()
	if err = st.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("push stsliceconst builder: %v", err)
	}
	if err = STSLICECONST(src).Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("stsliceconst: %v", err)
	}
	builder, err = st.PopBuilder()
	if err != nil {
		t.Fatalf("pop stsliceconst builder: %v", err)
	}
	if builder.BitsUsed() != 5 || builder.RefsUsed() != 1 {
		t.Fatalf("expected 5 bits/1 ref, got %d/%d", builder.BitsUsed(), builder.RefsUsed())
	}

	st = vm.NewStack()
	if err = st.PushBuilder(cell.BeginCell().MustStoreUInt(0xA, 4).MustStoreRef(refA)); err != nil {
		t.Fatalf("push brem builder: %v", err)
	}
	if err = BREMBITREFS().Interpret(&vm.State{Stack: st}); err != nil {
		t.Fatalf("brembitrefs: %v", err)
	}
	refsLeft, err := st.PopIntFinite()
	if err != nil {
		t.Fatalf("pop refs left: %v", err)
	}
	bitsLeft, err := st.PopIntFinite()
	if err != nil {
		t.Fatalf("pop bits left: %v", err)
	}
	if bitsLeft.Int64() != 1019 || refsLeft.Int64() != 3 {
		t.Fatalf("expected 1019/3, got %d/%d", bitsLeft.Int64(), refsLeft.Int64())
	}

	complex := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 1).EndCell()).EndCell()
	st = vm.NewStack()
	if err = st.PushCell(complex); err != nil {
		t.Fatalf("push cell: %v", err)
	}
	if err = st.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("push index: %v", err)
	}
	if err = CDEPTHIX().Interpret(&vm.State{Stack: st, GlobalVersion: 9}); err != nil {
		t.Fatalf("cdepthix: %v", err)
	}
	depth, err := st.PopIntFinite()
	if err != nil {
		t.Fatalf("pop cdepthix: %v", err)
	}
	if depth.Int64() != int64(complex.Depth(0)) {
		t.Fatalf("expected depth %d, got %d", complex.Depth(0), depth.Int64())
	}

}
