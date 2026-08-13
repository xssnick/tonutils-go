//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTupleInvalidOpcodeGaps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	var opcodes []uint64
	for opcode := uint64(0x6f8e); opcode <= 0x6f9f; opcode++ {
		opcodes = append(opcodes, opcode)
	}
	for opcode := uint64(0x6fa8); opcode <= 0x6faf; opcode++ {
		opcodes = append(opcodes, opcode)
	}

	for _, opcode := range opcodes {
		opcode := opcode
		t.Run(fmt.Sprintf("%04x", opcode), func(t *testing.T) {
			code := buildStackProgram(t, []*cell.Builder{tupleRawOp(opcode, 16)})
			runStackOpParityProgram(t, code, nil, int32(vmerr.CodeInvalidOpcode))
		})
	}
}

func TestTVMCrossEmulatorTupleOpcodeTruncation(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	families := []struct {
		name    string
		opcode  uint64
		minBits uint
	}{
		{name: "fixed_immediate", opcode: 0x6f0f, minBits: 12},
		{name: "simple", opcode: 0x6f8d, minBits: 8},
		{name: "null_simple", opcode: 0x6fa7, minBits: 8},
		{name: "index2", opcode: 0x6fbf, minBits: 12},
		{name: "index3", opcode: 0x6fff, minBits: 10},
	}

	for _, family := range families {
		family := family
		t.Run(family.name, func(t *testing.T) {
			for bits := family.minBits; bits < 16; bits++ {
				bits := bits
				t.Run(fmt.Sprintf("%d_bits", bits), func(t *testing.T) {
					prefix := family.opcode >> (16 - bits)
					code := buildStackProgram(t, []*cell.Builder{tupleRawOp(prefix, bits)})
					runStackOpParityProgram(t, code, nil, int32(vmerr.CodeInvalidOpcode))
				})
			}
		})
	}
}
