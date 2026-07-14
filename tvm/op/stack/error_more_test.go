package stack

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestStackGuardAndDeserializeErrors(t *testing.T) {
	t.Run("ShortStackGuards", func(t *testing.T) {
		tests := []struct {
			name string
			op   vm.OP
			st   *vm.State
		}{
			{name: "DUP2", op: DUP2(), st: newStackState(1)},
			{name: "OVER2", op: OVER2(), st: newStackState(1, 2, 3)},
			{name: "TUCK", op: TUCK(), st: newStackState(1)},
			{name: "DROP2", op: DROP2(), st: newStackState(1)},
			{name: "SWAP2", op: SWAP2(), st: newStackState(1, 2, 3)},
			{name: "ROT", op: ROT(), st: newStackState(1, 2)},
			{name: "ROTREV", op: ROTREV(), st: newStackState(1, 2)},
			{name: "PUSH2", op: PUSH2(1, 2), st: newStackState(1)},
			{name: "XCPU", op: XCPU(1, 0), st: newStackState(1)},
			{name: "XCHG2", op: XCHG2(1, 2), st: newStackState(1, 2)},
			{name: "PUXC", op: PUXC(2, 1), st: newStackState(1)},
			{name: "XCHG3", op: XCHG3(3, 1, 0), st: newStackState(1, 2)},
			{name: "XC2PU", op: XC2PU(2, 0, 1), st: newStackState(1)},
			{name: "XCPUXC", op: XCPUXC(2, 1, 3), st: newStackState(1)},
			{name: "XCPU2", op: XCPU2(1, 0, 2), st: newStackState(1)},
			{name: "PUXC2", op: PUXC2(2, 1, 3), st: newStackState(1)},
			{name: "PUXCPU", op: PUXCPU(2, 1, 0), st: newStackState(1)},
			{name: "PU2XC", op: PU2XC(2, 1, 3), st: newStackState(1)},
			{name: "PUSH3", op: PUSH3(0, 1, 2), st: newStackState(1)},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				before := tt.st.Stack.String()
				if err := tt.op.Interpret(tt.st); err == nil {
					t.Fatalf("%s should fail on a short stack", tt.name)
				}
				if after := tt.st.Stack.String(); after != before {
					t.Fatalf("%s mutated stack before reporting underflow:\nbefore:\n%s\nafter:\n%s", tt.name, before, after)
				}
			})
		}
	})

	t.Run("PushAndPopDeserializeRejectCorruptedPrefixes", func(t *testing.T) {
		if err := PUSH(0).Deserialize(cell.BeginCell().MustStoreUInt(0x10, 8).EndCell().MustBeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for PUSH, got %v", err)
		}
		if err := POP(0).Deserialize(cell.BeginCell().MustStoreUInt(0x20, 8).EndCell().MustBeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for POP, got %v", err)
		}
	})

	t.Run("XchgLongInvalidArgumentsFailOnInterpret", func(t *testing.T) {
		for _, raw := range []uint64{0x1001, 0x1021, 0x1022} {
			code := cell.BeginCell().MustStoreUInt(raw, 16).EndCell().MustBeginParse()
			op := XCHG(0, 0)
			if err := op.Deserialize(code); err != nil {
				t.Fatalf("deserialize XCHG %#x failed: %v", raw, err)
			}
			if got := op.InstructionBits(); got != 16 {
				t.Fatalf("unexpected XCHG %#x instruction bits: got %d want 16", raw, got)
			}

			err := op.Interpret(newStackState(1, 2, 3))
			var tvmErr vmerr.VMError
			if !errors.As(err, &tvmErr) || tvmErr.Code != vmerr.CodeInvalidOpcode || tvmErr.Msg != "invalid XCHG arguments" {
				t.Fatalf("expected invalid XCHG arguments for %#x, got %v", raw, err)
			}
		}
	})

	t.Run("Blkdrop2DeserializeRejectsZeroFirstCount", func(t *testing.T) {
		code := cell.BeginCell().MustStoreUInt(0x6c05, 16).EndCell().MustBeginParse()
		if err := BLKDROP2(0, 0).Deserialize(code); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for BLKDROP2, got %v", err)
		}
	})

	t.Run("PushContDeserializeErrors", func(t *testing.T) {
		if err := PUSHCONT(nil).Deserialize(cell.BeginCell().MustStoreUInt(0x0, 8).EndCell().MustBeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for PUSHCONT, got %v", err)
		}

		bigCont := cell.BeginCell().
			MustStoreUInt(0x47, 7).
			MustStoreUInt(1, 2).
			MustStoreUInt(1, 7).
			MustStoreUInt(0xAB, 8).
			EndCell()
		if err := PUSHCONT(nil).Deserialize(bigCont.MustBeginParse()); err == nil {
			t.Fatal("expected PUSHCONT big form without ref to fail")
		}

		refCont := cell.BeginCell().MustStoreUInt(0x8A, 8).EndCell()
		if err := PUSHCONT(nil).Deserialize(refCont.MustBeginParse()); err == nil {
			t.Fatal("expected PUSHCONT ref form without ref to fail")
		}
	})

	t.Run("DictPushConstDeserializeWithoutRefFails", func(t *testing.T) {
		op := DICTPUSHCONST(nil)
		code := cell.BeginCell().
			MustStoreSlice([]byte{0xF4, 0xA4}, 13).
			MustStoreBoolBit(false).
			MustStoreUInt(0, 10).
			EndCell()
		if err := op.Deserialize(code.MustBeginParse()); err == nil {
			t.Fatal("expected DICTPUSHCONST without ref to fail")
		}
	})

	t.Run("DictPushConstDeserializeEdgeFailures", func(t *testing.T) {
		tests := []struct {
			name string
			code *cell.Cell
		}{
			{
				name: "short_prefix",
				code: cell.BeginCell().EndCell(),
			},
			{
				name: "missing_root_flag",
				code: cell.BeginCell().
					MustStoreSlice([]byte{0xF4, 0xA4}, 13).
					EndCell(),
			},
			{
				name: "missing_prefix_bits",
				code: cell.BeginCell().
					MustStoreSlice([]byte{0xF4, 0xA4}, 13).
					MustStoreBoolBit(true).
					MustStoreRef(cell.BeginCell().EndCell()).
					EndCell(),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if err := DICTPUSHCONST(nil).Deserialize(tt.code.MustBeginParse()); err == nil {
					t.Fatal("expected DICTPUSHCONST deserialize to fail")
				}
			})
		}
	})

	t.Run("DictPushConstSerializeWithoutRefPanics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected DICTPUSHCONST serialize without ref to panic")
			}
		}()
		_ = DICTPUSHCONST(nil).Serialize()
	})
}
