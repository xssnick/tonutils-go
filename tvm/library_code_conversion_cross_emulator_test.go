//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// Witness from the 2026-08 parity audit: resolving a library root code cell
// happens outside the execution's library accounting, so it neither consumes
// a max-library-loads slot nor marks the hash as loaded. With a limit of 1 the
// runtime must still be able to resolve one more library during execution.
func TestTVMCrossEmulatorLibraryRootCodeConversionSlotParity(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	runtimeLib := cell.BeginCell().MustStoreUInt(0xAA55, 16).EndCell()
	runtimeLibCell := mustLibraryCellForHash(t, runtimeLib.Hash())

	// The real code pushed behind the library root: XCTOS the runtime library
	// cell (resolves through the in-VM path, consuming the only slot), DROP
	// both results and finish.
	realCode := codeFromBuilders(t,
		stackop.PUSHREF(runtimeLibCell).Serialize(),
		cellsliceop.XCTOS().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
	)
	rootLibraryCode := mustLibraryCellForHash(t, realCode.Hash())

	collection := mustCrossLibraryCollection(t, realCode, runtimeLib)

	limit := uint32(1)
	sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, &limit, 0)
	configRoot := libraryLoadLimitGetMethodConfigRoot(t, sizeLimits)

	data := cell.BeginCell().EndCell()
	cfg, err := prepareBlockchainConfigLenient(configRoot)
	if err != nil {
		t.Fatalf("prepare config: %v", err)
	}
	machine := NewTVM()

	// Get-method flow: the reference emulator registers the library
	// collection only after the machine is constructed, so the library root is
	// wrapped and resolves during execution (paying the implicit JMPREF, the
	// code load and the only slot). ExecuteGetMethod must match it exactly.
	t.Run("get_method_host", func(t *testing.T) {
		goStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("go stack: %v", err)
		}
		execStack := goStack.Copy()
		if err = execStack.PushSmallInt(0); err != nil {
			t.Fatalf("push method id: %v", err)
		}
		goRes, err := machine.ExecuteGetMethod(rootLibraryCode, data, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
			Libraries: []*cell.Cell{collection},
			Config:    cfg,
		})
		if err != nil {
			t.Fatalf("go run: %v", err)
		}
		goStackCell, err := stackToCell(goRes.Stack)
		if err != nil {
			t.Fatalf("serialize go stack: %v", err)
		}

		refStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("ref stack: %v", err)
		}
		refRes, err := runReferenceLibraryLoadLimitCrossCode(rootLibraryCode, data, collection, refStack, configRoot)
		if err != nil {
			t.Fatalf("reference run: %v", err)
		}

		if goRes.ExitCode != int64(refRes.exitCode) {
			t.Fatalf("exit mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
		}
		if goRes.GasUsed != refRes.gasUsed {
			t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
		}
		if !bytes.Equal(goStackCell.Hash(), refRes.stack.Hash()) {
			t.Fatalf("stack mismatch:\ngo:  %s\nref: %s", goStackCell.Dump(), refRes.stack.Dump())
		}
	})

	// Transaction flow: the libraries are available up front, so the
	// conversion resolves the root for free, keeping the single slot for the
	// runtime XCTOS.
	t.Run("transaction_host", func(t *testing.T) {
		goStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("go stack: %v", err)
		}
		execStack := goStack.Copy()
		if err = execStack.PushSmallInt(0); err != nil {
			t.Fatalf("push method id: %v", err)
		}
		goRes, err := machine.Execute(rootLibraryCode, data, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
			Libraries: []*cell.Cell{collection},
			Config:    cfg,
		})
		if err != nil {
			t.Fatalf("go run: %v", err)
		}
		if goRes.ExitCode != 0 {
			t.Fatalf("conversion must not consume the slot on the transaction path, got exit %d", goRes.ExitCode)
		}
	})
}
