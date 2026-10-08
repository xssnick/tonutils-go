//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorDebugOpcodeSpace(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	instructions := make([]*cell.Builder, 0, 256)
	for arg := uint64(0); arg < 0xf0; arg++ {
		instructions = append(instructions, stackRawOp(0xfe00|arg, 16))
	}
	for lengthCode := uint64(0); lengthCode < 16; lengthCode++ {
		data := make([]byte, lengthCode+1)
		for i := range data {
			data[i] = byte(lengthCode<<4) | byte(i)
		}
		instructions = append(instructions, cell.BeginCell().
			MustStoreUInt(0xfef0|lengthCode, 16).
			MustStoreSlice(data, uint(len(data))*8))
	}

	code := buildStackProgram(t, instructions)
	runStackOpParityProgram(t, code, nil, 0)
}

func TestTVMCrossEmulatorDebugStrTruncation(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for lengthCode := uint64(0); lengthCode < 16; lengthCode++ {
		lengthCode := lengthCode
		t.Run(fmt.Sprintf("length_%d", lengthCode+1), func(t *testing.T) {
			data := make([]byte, lengthCode)
			code := prependRawMethodDrop(cell.BeginCell().
				MustStoreUInt(0xfef0|lengthCode, 16).
				MustStoreSlice(data, uint(len(data))*8).
				EndCell())
			runStackOpParityProgram(t, code, nil, int32(vmerr.CodeInvalidOpcode))
		})
	}
}
