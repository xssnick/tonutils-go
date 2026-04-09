package stack

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
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
			{name: "PUSH2", op: PUSH2(1, 2), st: newStackState(1)},
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
				if err := tt.op.Interpret(tt.st); err == nil {
					t.Fatalf("%s should fail on a short stack", tt.name)
				}
			})
		}
	})

	t.Run("PushAndPopDeserializeRejectCorruptedPrefixes", func(t *testing.T) {
		if err := PUSH(0).Deserialize(cell.BeginCell().MustStoreUInt(0x10, 8).EndCell().BeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for PUSH, got %v", err)
		}
		if err := POP(0).Deserialize(cell.BeginCell().MustStoreUInt(0x20, 8).EndCell().BeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for POP, got %v", err)
		}
	})

	t.Run("PushContDeserializeErrors", func(t *testing.T) {
		if err := PUSHCONT(nil).Deserialize(cell.BeginCell().MustStoreUInt(0x0, 8).EndCell().BeginParse()); !errors.Is(err, vm.ErrCorruptedOpcode) {
			t.Fatalf("expected corrupted opcode for PUSHCONT, got %v", err)
		}

		bigCont := cell.BeginCell().
			MustStoreUInt(0x47, 7).
			MustStoreUInt(1, 2).
			MustStoreUInt(1, 7).
			MustStoreUInt(0xAB, 8).
			EndCell()
		if err := PUSHCONT(nil).Deserialize(bigCont.BeginParse()); err == nil {
			t.Fatal("expected PUSHCONT big form without ref to fail")
		}

		refCont := cell.BeginCell().MustStoreUInt(0x8A, 8).EndCell()
		if err := PUSHCONT(nil).Deserialize(refCont.BeginParse()); err == nil {
			t.Fatal("expected PUSHCONT ref form without ref to fail")
		}
	})

	t.Run("DictPushConstDeserializeWithoutRefStillWorks", func(t *testing.T) {
		op := DICTPUSHCONST(nil)
		if err := op.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("DICTPUSHCONST nil-ref deserialize failed: %v", err)
		}
		if op.cont != nil || op.pfx != 0 {
			t.Fatalf("unexpected DICTPUSHCONST decoded state: cont=%v pfx=%d", op.cont, op.pfx)
		}
	})
}
