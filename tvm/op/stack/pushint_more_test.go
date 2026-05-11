package stack

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestPUSHINTAdditionalForms(t *testing.T) {
	t.Run("DeserializeCoversAllEncodings", func(t *testing.T) {
		tests := []struct {
			name string
			code *cell.Slice
			want int64
		}{
			{
				name: "Int8",
				code: cell.BeginCell().MustStoreUInt(0x80, 8).MustStoreInt(-7, 8).EndCell().BeginParse(),
				want: -7,
			},
			{
				name: "Int16",
				code: cell.BeginCell().MustStoreUInt(0x81, 8).MustStoreInt(300, 16).EndCell().BeginParse(),
				want: 300,
			},
			{
				name: "SmallImmediate",
				code: cell.BeginCell().MustStoreUInt(0x75, 8).EndCell().BeginParse(),
				want: 5,
			},
			{
				name: "Long",
				code: PUSHINT(big.NewInt(70000)).Serialize().EndCell().BeginParse(),
				want: 70000,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				op := PUSHINT(nil)
				if err := op.Deserialize(tt.code); err != nil {
					t.Fatalf("deserialize failed: %v", err)
				}
				if op.value.Int64() != tt.want {
					t.Fatalf("unexpected decoded value: %d", op.value.Int64())
				}
			})
		}

		op := PUSHINT(nil)
		err := op.Deserialize(cell.BeginCell().MustStoreUInt(0x83, 8).EndCell().BeginParse())
		if !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode, got %v", err)
		}
	})

	t.Run("SerializeBranchesRoundTripAndInterpret", func(t *testing.T) {
		roundedLong := new(big.Int).Lsh(big.NewInt(1), 254)
		tests := []struct {
			name string
			val  *big.Int
			bits int64
		}{
			{name: "SmallImmediate", val: big.NewInt(-5), bits: 8},
			{name: "Int8", val: big.NewInt(100), bits: 16},
			{name: "Int16", val: big.NewInt(300), bits: 24},
			{name: "RoundedLong", val: roundedLong, bits: 13},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				op := PUSHINT(tt.val)
				decoded := PUSHINT(nil)
				if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
					t.Fatalf("deserialize failed: %v", err)
				}
				if decoded.value.Cmp(tt.val) != 0 {
					t.Fatalf("unexpected decoded value: %s", decoded.value.String())
				}
				if got := op.InstructionBits(); got != tt.bits {
					t.Fatalf("unexpected instruction bits: %d", got)
				}
			})
		}

		op := PUSHINT(big.NewInt(70000))
		if got := op.SerializeText(); got != "70000 PUSHINT" {
			t.Fatalf("unexpected text: %q", got)
		}

		state := &vm.State{Stack: vm.NewStack(), Gas: vm.NewGas()}
		if err := op.Interpret(state); err != nil {
			t.Fatalf("interpret failed: %v", err)
		}
		got, err := state.Stack.PopInt()
		if err != nil {
			t.Fatalf("failed to pop PUSHINT result: %v", err)
		}
		if got.Int64() != 70000 {
			t.Fatalf("unexpected interpreted value: %d", got.Int64())
		}
	})
}

func TestDUMPSTKInterpret(t *testing.T) {
	state := newStackState(1, 2, 3)
	if err := DUMPSTK().Interpret(state); err != nil {
		t.Fatalf("DUMPSTK failed: %v", err)
	}
	if got := popInts(t, state.Stack, 3); !equalInts(got, []int64{3, 2, 1}) {
		t.Fatalf("unexpected stack after DUMPSTK: %v", got)
	}
}
