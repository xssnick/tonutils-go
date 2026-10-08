//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// This file covers the two library-load-limit policies wired from
// tlb.SizeLimitsConfigV2.MaxTransactionLibraryLoads:
//   - the get-method / message-emulation dry-run path, always capped at
//     min(8, config) (see dryRunLibraryLoadLimit in vm.go);
//   - the real transaction/tick-tock path, config-only and unlimited when the
//     field is absent (see executeTransactionMessage/executeTickTockTransaction
//     in transaction_emulation.go).
//
// It also covers the companion max_vm_data_depth fix that now flows the
// configured value into vm.State.SetMaxDataDepth instead of a hardcoded 512.

// libraryLoadLimitSizeLimitsCell mirrors transactionVersionSizeLimitsCell
// (see transaction_version_cross_emulator_test.go) extended with the
// max_transaction_library_loads override (nil = field absent, i.e.
// unlimited on the transaction/tick-tock path) and an optional
// max_vm_data_depth override (0 = keep the 512 default). Kept as its own
// standalone helper here, rather than extending the shared one in place, so
// this file's additions cannot shift line numbers the reference-mismatch
// inventory audits track in that file.
func libraryLoadLimitSizeLimitsCell(t *testing.T, accStateCellsForStorageDict uint32, maxTransactionLibraryLoads *uint32, maxVMDataDepth uint16) *cell.Cell {
	t.Helper()

	if maxVMDataDepth == 0 {
		maxVMDataDepth = 512
	}

	limitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV2{
		MaxMsgBits:                  1 << 21,
		MaxMsgCells:                 1 << 13,
		MaxLibraryCells:             1000,
		MaxVMDataDepth:              maxVMDataDepth,
		MaxExtMsgSize:               65535,
		MaxExtMsgDepth:              512,
		MaxAccStateCells:            1 << 16,
		MaxMCAccStateCells:          1 << 11,
		MaxAccPublicLibraries:       256,
		DeferOutQueueSizeLimit:      256,
		MaxMsgExtraCurrencies:       2,
		MaxAccFixedPrefixLength:     8,
		AccStateCellsForStorageDict: accStateCellsForStorageDict,
		MaxTransactionLibraryLoads:  maxTransactionLibraryLoads,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}
	return limitsCell
}

func libraryLoadLimitSkipIfReferenceUnavailable(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
}

// libraryLoadLimitTargets builds n distinct leaf cells to publish as
// libraries: each stores its own index so the hashes are pairwise distinct.
func libraryLoadLimitTargets(t *testing.T, n int) []*cell.Cell {
	t.Helper()
	out := make([]*cell.Cell, n)
	for i := 0; i < n; i++ {
		out[i] = cell.BeginCell().MustStoreUInt(0xA1100000|uint64(i), 32).EndCell()
	}
	return out
}

// libraryLoadLimitSpecialCells builds the special library-reference cells
// (see mustCrossLibraryCellForHash) that XLOAD resolves against the
// libraries collection, one per target, in the same order.
func libraryLoadLimitSpecialCells(t *testing.T, targets []*cell.Cell) []*cell.Cell {
	t.Helper()
	out := make([]*cell.Cell, len(targets))
	for i, target := range targets {
		out[i] = mustCrossLibraryCellForHash(t, target.Hash())
	}
	return out
}

// libraryLoadOpsCode builds n sequential (XLOAD, DROP) pairs: each pops one
// cell already sitting on the stack, resolves it through the libraries
// collection and discards the result. Used by the get-method-style scenarios,
// where the library cells are supplied as initial stack values instead of
// code refs (a code cell can hold at most 4 refs, but the stack has no such
// limit).
func libraryLoadOpsCode(t *testing.T, n int) *cell.Cell {
	t.Helper()
	builders := make([]*cell.Builder, 0, n*2)
	for i := 0; i < n; i++ {
		builders = append(builders, cellsliceop.XLOAD().Serialize(), stackop.DROP().Serialize())
	}
	return codeFromBuilders(t, builders...)
}

// libraryLoadLimitGetMethodConfigRoot builds a minimal config root (global
// version pinned to the latest supported version, plus an optional
// size-limits param) for the get-method-style raw-code scenarios.
func libraryLoadLimitGetMethodConfigRoot(t *testing.T, sizeLimits *cell.Cell) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: uint32(vm.MaxSupportedGlobalVersion)})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	dict := cell.NewDict(32)
	if err = dict.SetIntKey(big.NewInt(int64(tlb.ConfigParamGlobalVersion)), cell.BeginCell().MustStoreRef(versionCell).EndCell()); err != nil {
		t.Fatalf("failed to store global version config: %v", err)
	}
	if sizeLimits != nil {
		if err = dict.SetIntKey(big.NewInt(int64(tlb.ConfigParamSizeLimits)), cell.BeginCell().MustStoreRef(sizeLimits).EndCell()); err != nil {
			t.Fatalf("failed to store size limits config: %v", err)
		}
	}
	return dict.AsCell()
}

// runGoLibraryLoadLimitCrossCode mirrors runGoCrossCodeWithVersionGasAndLibs
// but takes a fully custom config root, so the size-limits override under
// test reaches the prepared blockchain config that machine.Execute reads the
// library-load cap and max data depth from.
func runGoLibraryLoadLimitCrossCode(code, data *cell.Cell, libs []*cell.Cell, stack *vm.Stack, configRoot *cell.Cell) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushSmallInt(0); err != nil {
		return nil, err
	}

	cfg, err := prepareBlockchainConfigLenient(configRoot)
	if err != nil {
		return nil, err
	}

	machine := NewTVM()
	res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack, ExecutionConfig{
		Libraries: libs,
		Config:    cfg,
	})
	if err != nil {
		return nil, err
	}

	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}
	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, nil
}

func runReferenceLibraryLoadLimitCrossCode(code, data *cell.Cell, libs *cell.Cell, stack *vm.Stack, configRoot *cell.Cell) (*crossRunResult, error) {
	return runReferenceCrossCodeViaEmulator(code, data, stack, referenceGetMethodConfig{
		Address:    tonopsTestAddr,
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    uint64(tonopsTestBalance.Int64()),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
		Libs:       libs,
	})
}

// assertLibraryLoadLimitGetMethodCase runs len(stackCells) sequential
// (XLOAD, DROP) pairs against the given initial stack on both engines and
// asserts they agree on exit code and gas.
func assertLibraryLoadLimitGetMethodCase(t *testing.T, configRoot, libsCollection *cell.Cell, stackCells []*cell.Cell, wantExit int32) {
	t.Helper()

	code := prependRawMethodDrop(libraryLoadOpsCode(t, len(stackCells)))
	values := make([]any, len(stackCells))
	for i, c := range stackCells {
		values[i] = c
	}

	goStack, err := buildCrossStack(values...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(values...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoLibraryLoadLimitCrossCode(code, cell.BeginCell().EndCell(), []*cell.Cell{libsCollection}, goStack, configRoot)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceLibraryLoadLimitCrossCode(code, cell.BeginCell().EndCell(), libsCollection, refStack, configRoot)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("exit code mismatch: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
}

// TestTVMCrossEmulatorLibraryLoadLimitGetMethodDefaultCap proves the
// get-method / dry-run implicit 8-load cap applies when the blockchain
// config carries no max_transaction_library_loads override at all.
func TestTVMCrossEmulatorLibraryLoadLimitGetMethodDefaultCap(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	configRoot := libraryLoadLimitGetMethodConfigRoot(t, nil)
	targets := libraryLoadLimitTargets(t, 9)
	libs := libraryLoadLimitSpecialCells(t, targets)
	libsCollection := mustCrossLibraryCollection(t, targets...)

	t.Run("eight_loads_succeed", func(t *testing.T) {
		assertLibraryLoadLimitGetMethodCase(t, configRoot, libsCollection, libs[:8], 0)
	})
	t.Run("ninth_load_fails", func(t *testing.T) {
		assertLibraryLoadLimitGetMethodCase(t, configRoot, libsCollection, libs[:9], vmerr.CodeCellUnderflow)
	})
	t.Run("repeat_at_limit_does_not_fail", func(t *testing.T) {
		// Re-request an already-loaded hash after the cap is reached: it must
		// stay free (unique-hash accounting), so all 9 XLOAD calls succeed
		// even though only 8 are unique.
		stackCells := append(append([]*cell.Cell{}, libs[:8]...), libs[0])
		assertLibraryLoadLimitGetMethodCase(t, configRoot, libsCollection, stackCells, 0)
	})
}

// TestTVMCrossEmulatorLibraryLoadLimitGetMethodConfigOverride proves the
// config value wins over the implicit 8-cap when it is lower.
func TestTVMCrossEmulatorLibraryLoadLimitGetMethodConfigOverride(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	limit := uint32(3)
	sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, &limit, 0)
	configRoot := libraryLoadLimitGetMethodConfigRoot(t, sizeLimits)

	targets := libraryLoadLimitTargets(t, 4)
	libs := libraryLoadLimitSpecialCells(t, targets)
	libsCollection := mustCrossLibraryCollection(t, targets...)

	t.Run("three_loads_succeed", func(t *testing.T) {
		assertLibraryLoadLimitGetMethodCase(t, configRoot, libsCollection, libs[:3], 0)
	})
	t.Run("fourth_load_fails", func(t *testing.T) {
		assertLibraryLoadLimitGetMethodCase(t, configRoot, libsCollection, libs[:4], vmerr.CodeCellUnderflow)
	})
	t.Run("repeat_at_limit_does_not_fail", func(t *testing.T) {
		stackCells := append(append([]*cell.Cell{}, libs[:3]...), libs[0])
		assertLibraryLoadLimitGetMethodCase(t, configRoot, libsCollection, stackCells, 0)
	})
}

// TestTVMCrossEmulatorLibraryLoadLimitSharedAcrossRunVMChild proves the
// unique-hash counter is shared across a RUNVM child boundary rather than
// reset: with a configured cap of 2, the parent loads library A, hands off
// to a RUNVM child that loads library B (spending the second and last
// slot), and the parent's subsequent attempt to load library C must still
// fail once the child returns.
func TestTVMCrossEmulatorLibraryLoadLimitSharedAcrossRunVMChild(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	limit := uint32(2)
	sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, &limit, 0)
	configRoot := libraryLoadLimitGetMethodConfigRoot(t, sizeLimits)

	targets := libraryLoadLimitTargets(t, 3)
	libs := libraryLoadLimitSpecialCells(t, targets)
	libsCollection := mustCrossLibraryCollection(t, targets...)

	childCode := libraryLoadOpsCode(t, 1)
	parentCode := prependRawMethodDrop(codeFromBuilders(t,
		cellsliceop.XLOAD().Serialize(), stackop.DROP().Serialize(), // load A
		execop.RUNVM(0).Serialize(),
		stackop.DROP().Serialize(),                                  // discard the child's exit code
		cellsliceop.XLOAD().Serialize(), stackop.DROP().Serialize(), // attempt to load C
	))

	// Initial stack, bottom to top: libC, libB, child stack-size (1), child
	// code slice, libA. RUNVM pops the code slice and stack-size off the top,
	// then moves the top `stack-size` remaining items (libB) into the child;
	// libC is left for the parent to reach after RUNVM returns.
	values := []any{libs[2], libs[1], int64(1), childCode.MustBeginParse(), libs[0]}
	goStack, err := buildCrossStack(values...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(values...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoLibraryLoadLimitCrossCode(parentCode, cell.BeginCell().EndCell(), []*cell.Cell{libsCollection}, goStack, configRoot)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceLibraryLoadLimitCrossCode(parentCode, cell.BeginCell().EndCell(), libsCollection, refStack, configRoot)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != vmerr.CodeCellUnderflow || refRes.exitCode != vmerr.CodeCellUnderflow {
		t.Fatalf("exit code mismatch: go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, vmerr.CodeCellUnderflow)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}
}

// libraryLoadLimitChainCode builds compute-phase code for an ordinary
// internal-message transaction: it clears the 5 entry-stack items (balance,
// message value, in-message cell, in-message body slice, selector), then
// XLOADs every entry in libs (each embedded as a code ref, chained across
// cells in groups of 3 so no single cell exceeds the 4-ref limit -- 3 library
// refs plus either a continuation ref or, on the last cell, the new-data
// ref), and finally stores newData as the new persistent data (c4) via
// POPCTR 4.
func libraryLoadLimitChainCode(t *testing.T, libs []*cell.Cell, newData *cell.Cell) *cell.Cell {
	t.Helper()

	const perCell = 3
	entryDrops := []*cell.Builder{
		stackop.DROP().Serialize(), stackop.DROP().Serialize(), stackop.DROP().Serialize(),
		stackop.DROP().Serialize(), stackop.DROP().Serialize(),
	}

	var next *cell.Cell
	end := len(libs)
	for {
		start := end - perCell
		if start < 0 {
			start = 0
		}
		chunk := libs[start:end]

		var builders []*cell.Builder
		if start == 0 {
			builders = append(builders, entryDrops...)
		}
		for _, lib := range chunk {
			builders = append(builders,
				stackop.PUSHREF(lib).Serialize(),
				cellsliceop.XLOAD().Serialize(),
				stackop.DROP().Serialize(),
			)
		}
		if next != nil {
			builders = append(builders, execop.JMPREF(next).Serialize())
		} else {
			builders = append(builders, stackop.PUSHREF(newData).Serialize(), execop.POPCTR(4).Serialize())
		}
		next = codeFromBuilders(t, builders...)

		if start == 0 {
			break
		}
		end = start
	}
	return next
}

// libraryLoadLimitDataChain builds a cell with exactly `depth` levels of
// single-ref nesting (Cell.Depth() == depth).
func libraryLoadLimitDataChain(t *testing.T, depth uint16) *cell.Cell {
	t.Helper()
	c := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	for i := uint16(0); i < depth; i++ {
		c = cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(c).EndCell()
	}
	return c
}

const libraryLoadLimitTransactionBalance = uint64(10_000_000_000)

// runLibraryLoadLimitTransaction emulates a single ordinary internal-message
// transaction against `code` on both engines, with `libsCollection` (may be
// nil) published as the block-level libraries and `sizeLimits` overriding
// ConfigParamSizeLimits. It asserts gas, transaction hash and resulting
// shard-account hash all agree before returning both results so the caller
// can additionally check the compute-phase exit code.
func runLibraryLoadLimitTransaction(t *testing.T, code, libsCollection, sizeLimits *cell.Cell) (*TransactionExecutionResult, *referenceTransactionResult) {
	t.Helper()

	now := uint32(tonopsTestTime.Unix())
	addr := tonopsTestAddr
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	shard := buildTransactionTestShardAccountWithStorageInfo(t, addr, code, origData, libraryLoadLimitTransactionBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithOverrides(t, baseConfigRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSizeLimits): sizeLimits,
	})

	var libraries []*cell.Cell
	if libsCollection != nil {
		libraries = []*cell.Cell{libsCollection}
	}

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
		Libraries:   libraries,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot, referenceTransactionOptions{libs: libsCollection})
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
	}

	return goRes, refRes
}

// TestTVMCrossEmulatorLibraryLoadLimitTransactionUnlimitedByDefault proves
// the real transaction/tick-tock compute path does NOT inherit the
// get-method 8-load cap: with no max_transaction_library_loads override,
// loading 10 distinct libraries during compute must succeed on both sides.
func TestTVMCrossEmulatorLibraryLoadLimitTransactionUnlimitedByDefault(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	targets := libraryLoadLimitTargets(t, 10)
	libs := libraryLoadLimitSpecialCells(t, targets)
	libsCollection := mustCrossLibraryCollection(t, targets...)
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := libraryLoadLimitChainCode(t, libs, newData)

	sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, nil, 0)
	goRes, refRes := runLibraryLoadLimitTransaction(t, code, libsCollection, sizeLimits)

	if goRes.ExitCode != 0 || refRes.exitCode != 0 {
		t.Fatalf("exit code mismatch: go=%d reference=%d, want success", goRes.ExitCode, refRes.exitCode)
	}
}

// TestTVMCrossEmulatorLibraryLoadLimitTransactionConfigCap proves the
// transaction path DOES honour an explicit config cap: with
// max_transaction_library_loads = 2, a compute phase that loads 3 distinct
// libraries must fail at the third on both sides.
func TestTVMCrossEmulatorLibraryLoadLimitTransactionConfigCap(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	targets := libraryLoadLimitTargets(t, 3)
	libs := libraryLoadLimitSpecialCells(t, targets)
	libsCollection := mustCrossLibraryCollection(t, targets...)
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := libraryLoadLimitChainCode(t, libs, newData)

	limit := uint32(2)
	sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, &limit, 0)
	goRes, refRes := runLibraryLoadLimitTransaction(t, code, libsCollection, sizeLimits)

	if goRes.ExitCode != vmerr.CodeCellUnderflow || refRes.exitCode != vmerr.CodeCellUnderflow {
		t.Fatalf("exit code mismatch: go=%d reference=%d want=%d", goRes.ExitCode, refRes.exitCode, vmerr.CodeCellUnderflow)
	}
}

// TestTVMCrossEmulatorLibraryLoadLimitMaxDataDepthTransaction proves
// SizeLimitsConfigV2.MaxVMDataDepth now reaches vm.State.SetMaxDataDepth: a
// configured limit of 4 rejects a persistent-data cell one level deeper, and
// leaving the field at its ordinary value (512) still accepts a chain well
// past the configured-limit case's depth.
func TestTVMCrossEmulatorLibraryLoadLimitMaxDataDepthTransaction(t *testing.T) {
	libraryLoadLimitSkipIfReferenceUnavailable(t)

	t.Run("exceeds_configured_limit_fails", func(t *testing.T) {
		deepData := libraryLoadLimitDataChain(t, 5)
		code := makeTransactionInternalSuccessCode(t, deepData)
		sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, nil, 4)
		goRes, refRes := runLibraryLoadLimitTransaction(t, code, nil, sizeLimits)

		if goRes.ExitCode != vmerr.CodeCellOverflow || refRes.exitCode != vmerr.CodeCellOverflow {
			t.Fatalf("exit code mismatch: go=%d reference=%d want=%d", goRes.ExitCode, refRes.exitCode, vmerr.CodeCellOverflow)
		}
	})

	t.Run("default_limit_unaffected_succeeds", func(t *testing.T) {
		deepData := libraryLoadLimitDataChain(t, 10)
		code := makeTransactionInternalSuccessCode(t, deepData)
		sizeLimits := libraryLoadLimitSizeLimitsCell(t, 2, nil, 0)
		goRes, refRes := runLibraryLoadLimitTransaction(t, code, nil, sizeLimits)

		if goRes.ExitCode != 0 || refRes.exitCode != 0 {
			t.Fatalf("exit code mismatch: go=%d reference=%d, want success", goRes.ExitCode, refRes.exitCode)
		}
	})
}
