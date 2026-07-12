package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func pfxDictSwitchZeroRootFlagCode(bits uint64) *cell.Cell {
	return cell.BeginCell().
		MustStoreSlice([]byte{0xF4, 0xAC}, 13).
		MustStoreBoolBit(false).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(bits, 10).
		EndCell()
}

func TestPFXDICTSWITCHZeroRootFlagUsesPFXDICTGETDispatch(t *testing.T) {
	tests := []struct {
		name string
		bits uint64
		want string
	}{
		{name: "PFXDICTGETQ", bits: 0x000, want: "PFXDICTGETQ"},
		{name: "PFXDICTGET", bits: 0x100, want: "PFXDICTGET"},
		{name: "PFXDICTGETJMP", bits: 0x200, want: "PFXDICTGETJMP"},
		{name: "PFXDICTGETEXEC", bits: 0x300, want: "PFXDICTGETEXEC"},
	}

	machine := NewTVM()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := pfxDictSwitchZeroRootFlagCode(tt.bits).MustBeginParse()
			getter := machine.matchOpcode(code)
			if getter == nil {
				t.Fatal("opcode did not match")
			}

			op := getter()
			if err := op.Deserialize(code); err != nil {
				t.Fatalf("deserialize matched opcode: %v", err)
			}
			if got := op.SerializeText(); got != tt.want {
				t.Fatalf("matched opcode = %s, want %s", got, tt.want)
			}
			if code.BitsLeft() != 8 || code.RefsNum() != 1 {
				t.Fatalf("remaining code = (%d bits, %d refs), want (8, 1)", code.BitsLeft(), code.RefsNum())
			}
		})
	}
}
