package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestBasicSliceComparisonOps(t *testing.T) {
	t.Run("EmptyChecks", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().EndCell().BeginParse())
		if err := SDEMPTY().Interpret(state); err != nil {
			t.Fatalf("SDEMPTY failed: %v", err)
		}
		if !popCellSliceBool(t, state) {
			t.Fatal("expected SDEMPTY to report true")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).ToSlice())
		if err := SREMPTY().Interpret(state); err != nil {
			t.Fatalf("SREMPTY failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected SREMPTY to report false")
		}
	})

	t.Run("LeadAndTrailCounters", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0xE0}, 3).ToSlice())
		if err := SDCNTLEAD1().Interpret(state); err != nil {
			t.Fatalf("SDCNTLEAD1 failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 3 {
			t.Fatalf("unexpected lead ones count: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0xE0}, 3).ToSlice())
		if err := SDCNTTRAIL1().Interpret(state); err != nil {
			t.Fatalf("SDCNTTRAIL1 failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 3 {
			t.Fatalf("unexpected trail ones count: %d", got)
		}
	})
}

func TestExtendedSliceComparisonOps(t *testing.T) {
	left := cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice()
	right := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()

	tests := []struct {
		name string
		op   interface{ Interpret(*vm.State) error }
		want bool
	}{
		{name: "SDPFX", op: SDPFX(), want: true},
		{name: "SDPFXREV", op: SDPFXREV(), want: false},
		{name: "SDPPFX", op: SDPPFX(), want: true},
		{name: "SDPPFXREV", op: SDPPFXREV(), want: false},
		{name: "SDSFX", op: SDSFX(), want: false},
		{name: "SDSFXREV", op: SDSFXREV(), want: false},
		{name: "SDPSFX", op: SDPSFX(), want: false},
		{name: "SDPSFXREV", op: SDPSFXREV(), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newCellSliceState()
			pushCellSliceSlice(t, state, left.Copy())
			pushCellSliceSlice(t, state, right.Copy())
			if err := tt.op.Interpret(state); err != nil {
				t.Fatalf("%s failed: %v", tt.name, err)
			}
			if got := popCellSliceBool(t, state); got != tt.want {
				t.Fatalf("unexpected %s result: %v", tt.name, got)
			}
		})
	}
}
