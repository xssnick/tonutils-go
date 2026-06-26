package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestBasicSliceCompareOps(t *testing.T) {
	t.Run("SEMPTYTrueOnlyForNoBitsAndNoRefs", func(t *testing.T) {
		state := &vm.State{Stack: vm.NewStack()}
		if err := state.Stack.PushSlice(cell.BeginCell().EndCell().MustBeginParse()); err != nil {
			t.Fatalf("push slice: %v", err)
		}
		if err := SEMPTY().Interpret(state); err != nil {
			t.Fatalf("sempty failed: %v", err)
		}
		ok, err := state.Stack.PopBool()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if !ok {
			t.Fatal("expected empty slice without refs to be sempty")
		}

		state = &vm.State{Stack: vm.NewStack()}
		withRef := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell().MustBeginParse()
		if err := state.Stack.PushSlice(withRef); err != nil {
			t.Fatalf("push slice with ref: %v", err)
		}
		if err := SEMPTY().Interpret(state); err != nil {
			t.Fatalf("sempty failed: %v", err)
		}
		ok, err = state.Stack.PopBool()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		if ok {
			t.Fatal("expected slice with refs to be non-empty")
		}
	})

	t.Run("SDFIRSTAndCountsUseRemainingBits", func(t *testing.T) {
		state := &vm.State{Stack: vm.NewStack()}
		sl := cell.BeginCell().MustStoreUInt(0b0011100, 7).EndCell().MustBeginParse()

		if err := state.Stack.PushSlice(sl.Copy()); err != nil {
			t.Fatalf("push slice for sdfirst: %v", err)
		}
		if err := SDFIRST().Interpret(state); err != nil {
			t.Fatalf("sdfirst failed: %v", err)
		}
		first, err := state.Stack.PopBool()
		if err != nil {
			t.Fatalf("pop sdfirst: %v", err)
		}
		if first {
			t.Fatal("expected first bit to be zero")
		}

		if err := state.Stack.PushSlice(sl.Copy()); err != nil {
			t.Fatalf("push slice for lead0: %v", err)
		}
		if err := SDCNTLEAD0().Interpret(state); err != nil {
			t.Fatalf("sdcntlead0 failed: %v", err)
		}
		lead0, err := state.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop lead0: %v", err)
		}
		if lead0.Cmp(big.NewInt(2)) != 0 {
			t.Fatalf("expected 2 leading zeroes, got %s", lead0.String())
		}

		if err := state.Stack.PushSlice(sl.Copy()); err != nil {
			t.Fatalf("push slice for trail0: %v", err)
		}
		if err := SDCNTTRAIL0().Interpret(state); err != nil {
			t.Fatalf("sdcnttrail0 failed: %v", err)
		}
		trail0, err := state.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop trail0: %v", err)
		}
		if trail0.Cmp(big.NewInt(2)) != 0 {
			t.Fatalf("expected 2 trailing zeroes, got %s", trail0.String())
		}
	})
}

func TestBasicSliceCompareErrorAndEmptyEdges(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   interface{ Interpret(*vm.State) error }
	}{
		{name: "SEMPTY", op: SEMPTY()},
		{name: "SDEMPTY", op: SDEMPTY()},
		{name: "SREMPTY", op: SREMPTY()},
		{name: "SDFIRST", op: SDFIRST()},
		{name: "SDCNTLEAD0", op: SDCNTLEAD0()},
		{name: "SDCNTLEAD1", op: SDCNTLEAD1()},
		{name: "SDCNTTRAIL0", op: SDCNTTRAIL0()},
		{name: "SDCNTTRAIL1", op: SDCNTTRAIL1()},
	} {
		t.Run(tt.name+"StackUnderflow", func(t *testing.T) {
			assertCellSliceVMErrorCode(t, tt.op.Interpret(newCellSliceState()), vmerr.CodeStackUnderflow)
		})

		t.Run(tt.name+"TypeCheck", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceCell(t, st, cell.BeginCell().EndCell())
			assertCellSliceVMErrorCode(t, tt.op.Interpret(st), vmerr.CodeTypeCheck)
		})
	}

	st := newCellSliceState()
	pushCellSliceSlice(t, st, cell.BeginCell().EndCell().MustBeginParse())
	if err := SDFIRST().Interpret(st); err != nil {
		t.Fatalf("SDFIRST empty failed: %v", err)
	}
	if popCellSliceBool(t, st) {
		t.Fatal("SDFIRST empty result = true")
	}

	for _, tt := range []struct {
		name string
		op   interface{ Interpret(*vm.State) error }
	}{
		{name: "SDCNTLEAD0", op: SDCNTLEAD0()},
		{name: "SDCNTLEAD1", op: SDCNTLEAD1()},
		{name: "SDCNTTRAIL0", op: SDCNTTRAIL0()},
		{name: "SDCNTTRAIL1", op: SDCNTTRAIL1()},
	} {
		t.Run(tt.name+"Empty", func(t *testing.T) {
			st := newCellSliceState()
			pushCellSliceSlice(t, st, cell.BeginCell().EndCell().MustBeginParse())
			if err := tt.op.Interpret(st); err != nil {
				t.Fatalf("%s empty failed: %v", tt.name, err)
			}
			if got := popCellSliceInt(t, st); got != 0 {
				t.Fatalf("%s empty count = %d, want 0", tt.name, got)
			}
		})
	}
}

func FuzzTVMBasicSliceUnaryRules(f *testing.F) {
	for _, seed := range []struct {
		op   byte
		bits byte
		fill byte
		refs byte
	}{
		{op: 0, bits: 0, refs: 0},
		{op: 0, bits: 0, refs: 1},
		{op: 3, bits: 0},
		{op: 3, bits: 1, fill: 0x80},
		{op: 4, bits: 7, fill: 0x38},
		{op: 5, bits: 7, fill: 0xE0},
		{op: 6, bits: 7, fill: 0x1C},
		{op: 7, bits: 7, fill: 0xE0},
	} {
		f.Add(seed.op, seed.bits, seed.fill, seed.refs)
	}

	f.Fuzz(func(t *testing.T, rawOp, rawBits, fill, rawRefs byte) {
		opKind := rawOp % 8
		bits := uint(rawBits % 65)
		refs := int(rawRefs % 3)
		sl := basicSliceCompareFuzzSlice(t, bits, fill, refs)

		st := newCellSliceState()
		pushCellSliceSlice(t, st, sl.Copy())

		switch opKind {
		case 0:
			if err := SEMPTY().Interpret(st); err != nil {
				t.Fatalf("SEMPTY failed: %v", err)
			}
			if got, want := popCellSliceBool(t, st), bits == 0 && refs == 0; got != want {
				t.Fatalf("SEMPTY result = %v, want %v", got, want)
			}
		case 1:
			if err := SDEMPTY().Interpret(st); err != nil {
				t.Fatalf("SDEMPTY failed: %v", err)
			}
			if got, want := popCellSliceBool(t, st), bits == 0; got != want {
				t.Fatalf("SDEMPTY result = %v, want %v", got, want)
			}
		case 2:
			if err := SREMPTY().Interpret(st); err != nil {
				t.Fatalf("SREMPTY failed: %v", err)
			}
			if got, want := popCellSliceBool(t, st), refs == 0; got != want {
				t.Fatalf("SREMPTY result = %v, want %v", got, want)
			}
		case 3:
			if err := SDFIRST().Interpret(st); err != nil {
				t.Fatalf("SDFIRST failed: %v", err)
			}
			want := bits > 0 && sl.Copy().MustPreloadUInt(1) == 1
			if got := popCellSliceBool(t, st); got != want {
				t.Fatalf("SDFIRST result = %v, want %v", got, want)
			}
		case 4:
			checkBasicSliceCountOp(t, st, SDCNTLEAD0(), int64(sl.Copy().CountLeading(false)))
		case 5:
			checkBasicSliceCountOp(t, st, SDCNTLEAD1(), int64(sl.Copy().CountLeading(true)))
		case 6:
			checkBasicSliceCountOp(t, st, SDCNTTRAIL0(), int64(sl.Copy().CountTrailing(false)))
		default:
			checkBasicSliceCountOp(t, st, SDCNTTRAIL1(), int64(sl.Copy().CountTrailing(true)))
		}

		if st.Stack.Len() != 0 {
			t.Fatalf("stack depth = %d, want 0", st.Stack.Len())
		}
	})
}

func checkBasicSliceCountOp(t *testing.T, st *vm.State, op interface{ Interpret(*vm.State) error }, want int64) {
	t.Helper()

	if err := op.Interpret(st); err != nil {
		t.Fatalf("count op failed: %v", err)
	}
	if got := popCellSliceInt(t, st); got != want {
		t.Fatalf("count result = %d, want %d", got, want)
	}
}

func basicSliceCompareFuzzSlice(t *testing.T, bits uint, fill byte, refs int) *cell.Slice {
	t.Helper()

	b := cell.BeginCell()
	if bits > 0 {
		buf := make([]byte, (bits+7)/8)
		for i := range buf {
			buf[i] = fill
		}
		b.MustStoreSlice(buf, bits)
	}
	for i := 0; i < refs; i++ {
		b.MustStoreRef(cell.BeginCell().EndCell())
	}
	return b.EndCell().MustBeginParse()
}
