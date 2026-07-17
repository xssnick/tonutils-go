//go:build cgo && tvm_cross_emulator

package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// This file pins two historical library-resolution behaviours against the
// reference emulator (see load_cell_slice_impl in the C++ CellSlice.cpp and
// VmState::load_library in the C++ vm.cpp):
//   - nested library cells (a library cell whose published target is itself a
//     library cell) resolve transparently before global version 5 and are
//     rejected with cell_underflow from v5 on;
//   - before global version 4 the dictionary lookup performed by the library
//     loader is metered per actually-loaded tree node (load price for new
//     cells, reload price for repeats, failed lookups included), while from v4
//     on the lookup itself is free of charge.

// assertLibraryParityCase runs `code` as a get method on both engines at the
// given global version, with `collection` published as the block libraries
// dictionary, asserting both agree with each other on gas and report wantExit.
// The agreed gas amount is returned so callers can additionally compare
// scenarios against each other.
func assertLibraryParityCase(t *testing.T, code, collection *cell.Cell, globalVersion int, wantExit int32) int64 {
	t.Helper()

	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, []*cell.Cell{collection}, goStack, globalVersion, referenceDefaultMaxGas)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.Libs = collection
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("exit code mismatch: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
	return goRes.gasUsed
}

// TestTVMCrossEmulatorLibraryChainParity resolves a library cell whose
// published target is itself a library cell (entry -> mid -> leaf): before
// global version 5 the chain resolves all the way down to the ordinary leaf,
// from v5 on the second hop is rejected with cell_underflow. Exit code and
// gas must agree with the reference emulator in both regimes.
func TestTVMCrossEmulatorLibraryChainParity(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	leaf := cell.BeginCell().MustStoreUInt(0xF00D, 16).EndCell()
	mid := mustCrossLibraryCellForHash(t, leaf.Hash())
	entry := mustCrossLibraryCellForHash(t, mid.Hash())
	collection := mustCrossLibraryCollection(t, mid, leaf)

	code := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHREF(entry).Serialize(),
		cellsliceop.CTOS().Serialize(),
		stackop.DROP().Serialize(),
	))

	t.Run("gv4_nested_chain_resolves", func(t *testing.T) {
		assertLibraryParityCase(t, code, collection, 4, 0)
	})
	t.Run("gv5_nested_chain_rejected", func(t *testing.T) {
		assertLibraryParityCase(t, code, collection, 5, vmerr.CodeCellUnderflow)
	})
}

// libraryLookupGasParityCode resolves the same published library twice (the
// repeat re-walks the collection tree at reload prices below v4) and then a
// second library sitting on another branch of the tree.
func libraryLookupGasParityCode(t *testing.T, targets []*cell.Cell) *cell.Cell {
	t.Helper()

	first := mustCrossLibraryCellForHash(t, targets[0].Hash())
	second := mustCrossLibraryCellForHash(t, targets[5].Hash())

	return prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHREF(first).Serialize(),
		cellsliceop.CTOS().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(first).Serialize(),
		cellsliceop.CTOS().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHREF(second).Serialize(),
		cellsliceop.CTOS().Serialize(),
		stackop.DROP().Serialize(),
	))
}

// TestTVMCrossEmulatorLibraryLookupGasParity publishes enough libraries for
// the 256-bit collection dictionary to branch into internal nodes and checks
// the gas billed for library lookups against the reference emulator: metered
// per loaded tree node before global version 4 (successful, repeated and
// failed lookups alike), free from v4 on.
func TestTVMCrossEmulatorLibraryLookupGasParity(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	targets := libraryLoadLimitTargets(t, 8)
	collection := mustCrossLibraryCollection(t, targets...)
	code := libraryLookupGasParityCode(t, targets)

	var gasV3, gasV4 int64
	t.Run("gv3_metered_lookup", func(t *testing.T) {
		gasV3 = assertLibraryParityCase(t, code, collection, 3, 0)
	})
	t.Run("gv4_free_lookup", func(t *testing.T) {
		gasV4 = assertLibraryParityCase(t, code, collection, 4, 0)
	})
	if gasV3 != 0 && gasV4 != 0 && gasV3 <= gasV4 {
		// Same code, same collection: the pre-v4 runs must additionally pay
		// for the dictionary nodes walked by the lookups, so equality would
		// mean the scenario stopped covering the metered path.
		t.Fatalf("expected metered v3 lookups to cost more than free v4 lookups: v3=%d v4=%d", gasV3, gasV4)
	}

	missing := cell.BeginCell().MustStoreUInt(0xDEAD_BEEF, 32).EndCell()
	missingEntry := mustCrossLibraryCellForHash(t, missing.Hash())
	missingCode := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHREF(missingEntry).Serialize(),
		cellsliceop.CTOS().Serialize(),
		stackop.DROP().Serialize(),
	))

	t.Run("gv3_missing_library_lookup_still_billed", func(t *testing.T) {
		assertLibraryParityCase(t, missingCode, collection, 3, vmerr.CodeCellUnderflow)
	})
	t.Run("gv4_missing_library_lookup_free", func(t *testing.T) {
		assertLibraryParityCase(t, missingCode, collection, 4, vmerr.CodeCellUnderflow)
	})
}

// FuzzTVMCrossEmulatorLibraryNestedChainGlobalVersion is the cross-emulator anchor
// for the nested-library version gate in vm/cell_manager.go: across every
// supported global version, resolving a library cell whose target is another
// library cell must agree with the reference emulator (resolves before v5,
// cell_underflow from v5 on).
func FuzzTVMCrossEmulatorLibraryNestedChainGlobalVersion(f *testing.F) {
	for version := 0; version <= int(vmcore.MaxSupportedGlobalVersion); version++ {
		f.Add(uint8(version), uint16(version))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawSeed uint16) {
		libraryLoadLimitSkipIfReferenceUnavailable(t)

		version := tvmFuzzGlobalVersionByte(rawVersion)
		leaf := cell.BeginCell().MustStoreUInt(uint64(rawSeed), 16).EndCell()
		mid := mustCrossLibraryCellForHash(t, leaf.Hash())
		entry := mustCrossLibraryCellForHash(t, mid.Hash())
		collection := mustCrossLibraryCollection(t, mid, leaf)

		code := prependRawMethodDrop(codeFromBuilders(t,
			stackop.PUSHREF(entry).Serialize(),
			cellsliceop.CTOS().Serialize(),
			stackop.DROP().Serialize(),
		))

		wantExit := int32(0)
		if version >= 5 {
			wantExit = vmerr.CodeCellUnderflow
		}
		assertLibraryParityCase(t, code, collection, version, wantExit)
	})
}
