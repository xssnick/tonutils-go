//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func TestTVMCrossEmulatorStackDepthBoundary64K(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	dupBody := codeFromBuilders(t, stackop.DUP().Serialize())

	t.Run("grow_to_65536_then_reduce", func(t *testing.T) {
		code := buildStackProgram(t, []*cell.Builder{
			stackop.PUSHINT(big.NewInt(1)).Serialize(),
			stackop.PUSHINT(big.NewInt((1 << 16) - 1)).Serialize(),
			stackop.PUSHCONT(dupBody).Serialize(),
			execop.REPEAT().Serialize(),
			stackRawOp(0x6a, 8),
		})

		runStackOpParityProgram(t, code, nil, 0)
	})

	t.Run("grow_above_65536_caught_and_reduced", func(t *testing.T) {
		handler := codeFromBuilders(t, stackRawOp(0x6a, 8))
		code := buildStackProgram(t, []*cell.Builder{
			stackop.PUSHINT(big.NewInt(1)).Serialize(),
			stackop.PUSHINT(big.NewInt(1 << 16)).Serialize(),
			stackop.PUSHCONT(dupBody).Serialize(),
			stackop.PUSHCONT(handler).Serialize(),
			execop.TRY().Serialize(),
		})

		runStackOpParityProgram(t, code, nil, 0)
	})
}
