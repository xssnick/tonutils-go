package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestBasicSliceCompareOps(t *testing.T) {
	t.Run("SEMPTYTrueOnlyForNoBitsAndNoRefs", func(t *testing.T) {
		state := &vm.State{Stack: vm.NewStack()}
		if err := state.Stack.PushSlice(cell.BeginCell().EndCell().BeginParse()); err != nil {
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
		withRef := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()
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
		sl := cell.BeginCell().MustStoreUInt(0b0011100, 7).EndCell().BeginParse()

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
