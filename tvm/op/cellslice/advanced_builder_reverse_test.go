package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestReverseBuilderStoreOps(t *testing.T) {
	t.Run("STSLICERStoresSliceIntoBuilder", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("failed to push builder: %v", err)
		}
		if err := st.PushSlice(cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}

		if err := STSLICER().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("STSLICER failed: %v", err)
		}

		builder, err := st.PopBuilder()
		if err != nil {
			t.Fatalf("failed to pop builder: %v", err)
		}
		if got := builder.EndCell().BeginParse().MustLoadUInt(8); got != 0xAB {
			t.Fatalf("unexpected stored value: %x", got)
		}
	})

	t.Run("STSLICERQFailurePreservesOrderAndReturnsMinusOne", func(t *testing.T) {
		dst := cell.BeginCell().MustStoreSlice(make([]byte, 127), 1016)

		st := vm.NewStack()
		if err := st.PushBuilder(dst); err != nil {
			t.Fatalf("failed to push builder: %v", err)
		}
		src := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()
		if err := st.PushSlice(src); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}

		if err := STSLICERQ().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("STSLICERQ failed: %v", err)
		}

		status, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop status: %v", err)
		}
		if status.Int64() != -1 {
			t.Fatalf("unexpected quiet status: %s", status.String())
		}

		gotSlice, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop original slice: %v", err)
		}
		if gotSlice.BitsLeft() != src.BitsLeft() || gotSlice.MustLoadUInt(8) != 0xAB {
			t.Fatal("quiet reverse store should preserve original slice")
		}

		builder, err := st.PopBuilder()
		if err != nil {
			t.Fatalf("failed to pop original builder: %v", err)
		}
		if builder.BitsUsed() != dst.BitsUsed() {
			t.Fatal("quiet reverse store should preserve original builder")
		}
	})
}
