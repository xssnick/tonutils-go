package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestExecutionResultReportsMissingLibraryHash(t *testing.T) {
	missing := cell.BeginCell().MustStoreUInt(0xDEAD_BEEF, 32).EndCell()
	missingRef := mustLibraryCellForHash(t, missing.Hash())
	code := codeFromBuilders(t,
		stackop.PUSHREF(missingRef).Serialize(),
		cellsliceop.XLOADQ().Serialize(),
	)

	res, err := NewTVM().ExecuteGetMethod(
		code,
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		vm.GasWithLimit(1_000_000),
		vm.NewStack(),
		testExecutionConfigWithVersion(t, vm.MaxSupportedGlobalVersion),
	)
	if err != nil {
		t.Fatalf("execute get method: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}

	want := missing.HashKey()
	if res.MissingLibrary == nil || *res.MissingLibrary != want {
		t.Fatalf("missing library = %v, want %x", res.MissingLibrary, want)
	}
}

func TestStartupLibraryLookupDoesNotReportDummyStateMiss(t *testing.T) {
	missing := cell.BeginCell().MustStoreUInt(0xBAD_C0DE, 32).EndCell()
	code := mustLibraryCellForHash(t, missing.Hash())
	data := cell.BeginCell().EndCell()
	state := vm.NewExecutionState(9, vm.GasWithLimit(1_000_000), data, tuple.Tuple{}, vm.NewStack())
	limit := uint32(0)

	res, err := NewTVM().executeState(state, code, data, executeOptions{
		maxVMDataDepth:   vm.MaxDataDepth,
		libraryLoadLimit: &limit,
	})
	if err != nil {
		if _, ok := vmerr.ErrorCode(err); !ok {
			t.Fatalf("execute state: %v", err)
		}
	}
	if res == nil {
		t.Fatal("missing execution result")
	}
	if res.ExitCode != vmerr.CodeCellUnderflow {
		t.Fatalf("exit code = %d, want cell underflow", res.ExitCode)
	}
	if res.MissingLibrary != nil {
		t.Fatalf("missing library = %x, want nil: startup uses an isolated DummyVmState and the runtime lookup is refused by limit 0", *res.MissingLibrary)
	}
}
