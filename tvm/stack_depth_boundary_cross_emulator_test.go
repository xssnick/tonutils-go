//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorStackDepthBoundary64K(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Run("grow_to_65536_then_reduce", func(t *testing.T) {
		runStackOpParityProgram(t, stackDepthBoundaryProgram(t, 0), nil, 0)
	})

	t.Run("grow_to_65537_then_reduce", func(t *testing.T) {
		runStackOpParityProgram(t, stackDepthBoundaryProgram(t, 1), nil, 0)
	})
}

func TestTVMCrossEmulatorStackDepthBoundary64KAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_STACK_DEPTH_BOUNDARY_VERSION_AUDIT") {
		for _, rawCase := range []uint8{0, 1} {
			t.Run("global_v"+strconv.Itoa(version)+"_"+stackDepthBoundaryCaseName(rawCase), func(t *testing.T) {
				assertStackDepthBoundaryVersionParity(t, version, rawCase)
			})
		}
	}
}

func FuzzTVMCrossEmulatorStackDepthBoundary64KGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(0))
		f.Add(uint8(version), uint8(1))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)

		assertStackDepthBoundaryVersionParity(t, version, rawCase)
	})
}

func assertStackDepthBoundaryVersionParity(t *testing.T, version int, rawCase uint8) {
	t.Helper()

	runStackOpVersionedParityProgram(t, stackDepthBoundaryProgram(t, rawCase), nil, version, 0)
}

func stackDepthBoundaryProgram(t *testing.T, rawCase uint8) *cell.Cell {
	t.Helper()

	dupBody := codeFromBuilders(t, stackop.DUP().Serialize())
	repeatCount := int64((1 << 16) - 1)
	if rawCase%2 != 0 {
		repeatCount++
	}

	return buildStackProgram(t, []*cell.Builder{
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		stackop.PUSHINT(big.NewInt(repeatCount)).Serialize(),
		stackop.PUSHCONT(dupBody).Serialize(),
		execop.REPEAT().Serialize(),
		stackRawOp(0x6a, 8),
	})
}

func stackDepthBoundaryCaseName(rawCase uint8) string {
	if rawCase%2 == 0 {
		return "grow_to_65536_then_reduce"
	}
	return "grow_to_65537_then_reduce"
}
