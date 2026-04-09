package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestLoadIntAndSliceFamilies(t *testing.T) {
	t.Run("PLDIFIXLoadsSignedBitWithoutConsumingSlice", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}

		if err := PLDIFIX(1, false, true, false).Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("PLDIFIX failed: %v", err)
		}

		if st.Len() != 1 {
			t.Fatalf("unexpected stack depth: %d", st.Len())
		}
		got, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop signed value: %v", err)
		}
		if got.Cmp(big.NewInt(-1)) != 0 {
			t.Fatalf("unexpected signed value: %s", got.String())
		}
	})

	t.Run("LDIXQFailureKeepsOriginalSliceAndPushesFalse", func(t *testing.T) {
		st := vm.NewStack()
		src := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
		if err := st.PushSlice(src); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}
		if err := st.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("failed to push bit size: %v", err)
		}

		if err := LDIXQ().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("LDIXQ failed: %v", err)
		}

		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop quiet flag: %v", err)
		}
		if ok {
			t.Fatal("expected quiet load to report failure")
		}

		rest, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop original slice: %v", err)
		}
		if rest.BitsLeft() != 1 || rest.MustLoadUInt(1) != 1 {
			t.Fatal("quiet LDIXQ should preserve original slice on failure")
		}
	})

	t.Run("PLDUZPreservesSliceAndLoadsZeroExtendedValue", func(t *testing.T) {
		st := vm.NewStack()
		src := cell.BeginCell().MustStoreUInt(0xAABBCCDD, 32).ToSlice()
		if err := st.PushSlice(src); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}

		if err := PLDUZ(32).Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("PLDUZ failed: %v", err)
		}

		got, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop zero-extended value: %v", err)
		}
		if got.Cmp(new(big.Int).SetUint64(0xAABBCCDD)) != 0 {
			t.Fatalf("unexpected zero-extended value: %s", got.String())
		}

		rest, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop preserved slice: %v", err)
		}
		if rest.BitsLeft() != 32 || rest.MustLoadUInt(32) != 0xAABBCCDD {
			t.Fatal("PLDUZ should preserve original slice")
		}
	})

	t.Run("PLDSLICEXQFailureReturnsOnlyFalse", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushSlice(cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice()); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}
		if err := st.PushInt(big.NewInt(5)); err != nil {
			t.Fatalf("failed to push requested bits: %v", err)
		}

		if err := PLDSLICEXQ().Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("PLDSLICEXQ failed: %v", err)
		}

		if st.Len() != 1 {
			t.Fatalf("unexpected stack depth: %d", st.Len())
		}
		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop quiet flag: %v", err)
		}
		if ok {
			t.Fatal("expected quiet preload slice op to report failure")
		}
	})

	t.Run("LDSLICEFIXReturnsLoadedPartAndRemainder", func(t *testing.T) {
		st := vm.NewStack()
		if err := st.PushSlice(cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()); err != nil {
			t.Fatalf("failed to push slice: %v", err)
		}

		if err := LDSLICEFIX(4, false, false).Interpret(&vm.State{Stack: st}); err != nil {
			t.Fatalf("LDSLICEFIX failed: %v", err)
		}

		rest, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop remainder: %v", err)
		}
		part, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop loaded part: %v", err)
		}

		if part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("unexpected loaded subslice")
		}
		if rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("unexpected remainder subslice")
		}
	})
}
