package cellslice

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestSDBEGINSCONST(t *testing.T) {
	t.Run("DeserializeAndMatchAdvancesSlice", func(t *testing.T) {
		needle := cell.BeginCell().MustStoreUInt(0b101011, 6).EndCell().BeginParse()
		op := SDBEGINSCONST(needle, false)
		decoded := SDBEGINSCONST(cell.BeginCell().EndCell().BeginParse(), false)
		if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("deserialize failed: %v", err)
		}

		state := &vm.State{Stack: vm.NewStack()}
		haystack := cell.BeginCell().MustStoreUInt(0b10101111, 8).EndCell().BeginParse()
		if err := state.Stack.PushSlice(haystack); err != nil {
			t.Fatalf("push haystack: %v", err)
		}
		if err := decoded.Interpret(state); err != nil {
			t.Fatalf("interpret failed: %v", err)
		}

		rest, err := state.Stack.PopSlice()
		if err != nil {
			t.Fatalf("pop rest: %v", err)
		}
		if rest.BitsLeft() != 2 || rest.MustLoadUInt(2) != 0b11 {
			t.Fatalf("expected remaining suffix 11, got %s", rest.MustToCell().DumpBits())
		}
	})

	t.Run("QuietFailurePreservesSliceAndReturnsFalse", func(t *testing.T) {
		needle := cell.BeginCell().MustStoreUInt(0b111, 3).EndCell().BeginParse()
		state := &vm.State{Stack: vm.NewStack()}
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse()); err != nil {
			t.Fatalf("push haystack: %v", err)
		}

		if err := SDBEGINSCONST(needle, true).Interpret(state); err != nil {
			t.Fatalf("quiet interpret failed: %v", err)
		}

		ok, err := state.Stack.PopBool()
		if err != nil {
			t.Fatalf("pop flag: %v", err)
		}
		if ok {
			t.Fatal("expected quiet mismatch flag to be false")
		}

		rest, err := state.Stack.PopSlice()
		if err != nil {
			t.Fatalf("pop preserved slice: %v", err)
		}
		if rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0b1010 {
			t.Fatalf("expected original slice to be preserved, got %s", rest.MustToCell().DumpBits())
		}
	})

	t.Run("NonQuietFailureReturnsCellUnderflow", func(t *testing.T) {
		needle := cell.BeginCell().MustStoreUInt(0b111, 3).EndCell().BeginParse()
		state := &vm.State{Stack: vm.NewStack()}
		if err := state.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse()); err != nil {
			t.Fatalf("push haystack: %v", err)
		}

		err := SDBEGINSCONST(needle, false).Interpret(state)
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeCellUnderflow {
			t.Fatalf("expected cell underflow, got %v", err)
		}
	})
}
