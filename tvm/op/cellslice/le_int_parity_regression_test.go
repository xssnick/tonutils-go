package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestWriteLEIntBoundariesAndAllocations(t *testing.T) {
	tests := []struct {
		name     string
		value    *big.Int
		bytes    int
		unsigned bool
		want     bool
	}{
		{name: "signed32_min", value: big.NewInt(-1 << 31), bytes: 4, want: true},
		{name: "signed32_max", value: big.NewInt(1<<31 - 1), bytes: 4, want: true},
		{name: "signed32_below", value: big.NewInt(-1<<31 - 1), bytes: 4},
		{name: "signed32_above", value: big.NewInt(1 << 31), bytes: 4},
		{name: "signed64_min", value: big.NewInt(-1 << 63), bytes: 8, want: true},
		{name: "signed64_max", value: big.NewInt(1<<63 - 1), bytes: 8, want: true},
		{name: "signed64_below", value: new(big.Int).Sub(big.NewInt(-1<<63), big.NewInt(1)), bytes: 8},
		{name: "signed64_above", value: new(big.Int).Lsh(big.NewInt(1), 63), bytes: 8},
		{name: "unsigned32_max", value: new(big.Int).SetUint64(1<<32 - 1), bytes: 4, unsigned: true, want: true},
		{name: "unsigned32_above", value: new(big.Int).SetUint64(1 << 32), bytes: 4, unsigned: true},
		{name: "unsigned64_max", value: new(big.Int).SetUint64(^uint64(0)), bytes: 8, unsigned: true, want: true},
		{name: "unsigned64_above", value: new(big.Int).Lsh(big.NewInt(1), 64), bytes: 8, unsigned: true},
		{name: "unsigned_negative", value: big.NewInt(-1), bytes: 8, unsigned: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out [8]byte
			if got := writeLEInt(out[:tt.bytes], tt.value, tt.unsigned); got != tt.want {
				t.Fatalf("writeLEInt() = %v, want %v", got, tt.want)
			}
		})
	}

	value := big.NewInt(-2)
	var out [8]byte
	if allocs := testing.AllocsPerRun(1000, func() {
		if !writeLEInt(out[:], value, false) {
			panic("writeLEInt rejected valid value")
		}
	}); allocs != 0 {
		t.Fatalf("writeLEInt allocations = %v, want 0", allocs)
	}
}

func TestStoreLEIntNaNErrorPriority(t *testing.T) {
	tests := []struct {
		name string
		op   func() interface{ Interpret(*vm.State) error }
	}{
		{name: "STILE4", op: func() interface{ Interpret(*vm.State) error } { return STILE4() }},
		{name: "STULE4", op: func() interface{ Interpret(*vm.State) error } { return STULE4() }},
		{name: "STILE8", op: func() interface{ Interpret(*vm.State) error } { return STILE8() }},
		{name: "STULE8", op: func() interface{ Interpret(*vm.State) error } { return STULE8() }},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/range", func(t *testing.T) {
			state := newCellSliceState()
			if err := state.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushCellSliceBuilder(t, state, cell.BeginCell())

			assertCellSliceVMErrorCode(t, tt.op().Interpret(state), vmerr.CodeRangeCheck)
		})

		t.Run(tt.name+"/capacity_precedes_range", func(t *testing.T) {
			state := newCellSliceState()
			if err := state.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("push NaN: %v", err)
			}
			pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023))

			assertCellSliceVMErrorCode(t, tt.op().Interpret(state), vmerr.CodeCellOverflow)
		})
	}
}

func BenchmarkStoreLEIntInterpret(b *testing.B) {
	op := STILE8()

	for b.Loop() {
		state := &vm.State{Stack: vm.NewStack()}
		if err := state.Stack.PushSmallInt(1); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushOwnedBuilder(cell.BeginCell()); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSTGRAMSInterpret(b *testing.B) {
	op := STGRAMS()

	for b.Loop() {
		state := &vm.State{Stack: vm.NewStack()}
		if err := state.Stack.PushOwnedBuilder(cell.BeginCell()); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushSmallInt(1); err != nil {
			b.Fatal(err)
		}
		if err := op.Interpret(state); err != nil {
			b.Fatal(err)
		}
	}
}
