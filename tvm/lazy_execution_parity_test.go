package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// Lazy-loaded BOCs are a Go-side optimization, so lazy and eager loads of the
// same tree must be observably identical: same exit code, same gas, same
// steps. These witnesses pin the 2026-08 audit findings where the lazy
// placeholder's pruned-looking shape leaked into VM behavior.

func lazyParityReload(t *testing.T, root *cell.Cell, lazy bool) *cell.Cell {
	t.Helper()

	boc := root.ToBOCWithFlags(false)
	parsed, err := cell.FromBOCWithOptions(boc, cell.BOCParseOptions{Lazy: lazy, AllowNonZeroLevelRoot: true})
	if err != nil {
		t.Fatalf("parse boc (lazy=%v): %v", lazy, err)
	}
	return parsed
}

func lazyParityExecute(t *testing.T, code *cell.Cell, globalVersion int, stackVals ...any) *ExecutionResult {
	t.Helper()

	stack := vmcore.NewStack()
	for _, v := range stackVals {
		if err := stack.PushHostValue(v); err != nil {
			t.Fatalf("push stack value: %v", err)
		}
	}
	res, err := NewTVM().Execute(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.GasWithLimit(1_000_000), stack, testExecutionConfigWithVersion(t, uint32(globalVersion)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res
}

// A lazy root code cell that is really an ordinary cell must start execution
// exactly like its eager twin — no implicit JMPREF step and no code load gas
// (both below and since v9 the conversion runs outside gas accounting).
func TestLazyRootCodeConversionMatchesEager(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0x75, 8).MustStoreUInt(0x30, 8).EndCell() // PUSHINT 5; DROP
	container := cell.BeginCell().MustStoreRef(code).EndCell()

	for _, version := range []int{4, 8, 9, 13} {
		eagerCode, err := lazyParityReload(t, container, false).PeekRef(0)
		if err != nil {
			t.Fatalf("eager peek: %v", err)
		}
		lazyCode, err := lazyParityReload(t, container, true).PeekRef(0)
		if err != nil {
			t.Fatalf("lazy peek: %v", err)
		}
		if !lazyCode.IsLazy() {
			t.Skip("lazy BOC loader materialized the ref eagerly; nothing to compare")
		}

		eagerRes := lazyParityExecute(t, eagerCode, version)
		lazyRes := lazyParityExecute(t, lazyCode, version)

		if eagerRes.ExitCode != lazyRes.ExitCode {
			t.Fatalf("v%d exit mismatch: eager=%d lazy=%d", version, eagerRes.ExitCode, lazyRes.ExitCode)
		}
		if eagerRes.GasUsed != lazyRes.GasUsed {
			t.Fatalf("v%d gas mismatch: eager=%d lazy=%d", version, eagerRes.GasUsed, lazyRes.GasUsed)
		}
		if eagerRes.Steps != lazyRes.Steps {
			t.Fatalf("v%d steps mismatch: eager=%d lazy=%d", version, eagerRes.Steps, lazyRes.Steps)
		}
	}
}

// Since v9 startup code conversion uses a dummy VM interface and catches only
// ordinary VmError. A virtualized pruned root therefore aborts before the
// first VM step; below v9 NoVmOrd rejects it quietly and execution reaches the
// same abort through the implicit JMPREF wrapper.
func TestVirtualizedRootCodeConversionVersionParity(t *testing.T) {
	_, code := mustVirtualizedPrunedCellForTVM(t)
	data := cell.BeginCell().EndCell()

	flows := []struct {
		name string
		run  func(version int) (*ExecutionResult, error)
	}{
		{
			name: "transaction",
			run: func(version int) (*ExecutionResult, error) {
				return NewTVM().Execute(code, data, tuple.Tuple{}, vmcore.GasWithLimit(1_000_000), vmcore.NewStack(), testExecutionConfigWithVersion(t, uint32(version)))
			},
		},
		{
			name: "get-method",
			run: func(version int) (*ExecutionResult, error) {
				return NewTVM().ExecuteGetMethod(code, data, tuple.Tuple{}, vmcore.GasWithLimit(1_000_000), vmcore.NewStack(), testExecutionConfigWithVersion(t, uint32(version)))
			},
		},
	}

	for _, flow := range flows {
		for _, version := range []int{8, 9, 13} {
			t.Run(fmt.Sprintf("%s-v%d", flow.name, version), func(t *testing.T) {
				res, err := flow.run(version)
				if err != nil {
					t.Fatalf("execute: %v", err)
				}
				if res.ExitCode != ^int64(vmerr.CodeVirtualization) {
					t.Fatalf("exit = %d, want %d", res.ExitCode, ^int64(vmerr.CodeVirtualization))
				}
				if version >= 9 {
					if res.Steps != 0 || res.GasUsed != 0 {
						t.Fatalf("startup abort consumed steps/gas = %d/%d, want 0/0", res.Steps, res.GasUsed)
					}
				} else if res.Steps == 0 {
					t.Fatal("pre-v9 wrapper flow must reach the implicit JMPREF step")
				}
			})
		}
	}
}

// XLOAD of a lazy cell that materializes into a virtualized pruned branch is
// a plain special-cell failure (a catchable cell underflow), identical to
// eager: that opcode never raises a virtualization abort.
func TestLazyXLoadVirtualizedPrunedMatchesEager(t *testing.T) {
	rawBody, virtBody := lazyParityProofBody(t)

	xload := cell.BeginCell().MustStoreUInt(0xD73A, 16).EndCell() // XLOAD

	eagerPruned, err := virtBody.PeekRef(0)
	if err != nil {
		t.Fatalf("peek eager pruned: %v", err)
	}
	if eagerPruned.GetType() != cell.PrunedCellType || !eagerPruned.IsVirtualized() {
		t.Fatalf("fixture degenerated: eager child type=%v virt=%v", eagerPruned.GetType(), eagerPruned.IsVirtualized())
	}
	eagerRes := lazyParityExecute(t, xload, 13, eagerPruned)

	lazyBody := lazyParityMaterialize(t, lazyParityWrapReload(t, rawBody))
	lazyPruned, err := lazyBody.Virtualize(0).PeekRef(0)
	if err != nil {
		t.Fatalf("peek lazy pruned: %v", err)
	}
	if !lazyPruned.IsLazy() || !lazyPruned.IsVirtualized() {
		t.Skipf("fixture degenerated: lazy=%v virt=%v", lazyPruned.IsLazy(), lazyPruned.IsVirtualized())
	}
	lazyRes := lazyParityExecute(t, xload, 13, lazyPruned)

	if eagerRes.ExitCode != lazyRes.ExitCode || eagerRes.GasUsed != lazyRes.GasUsed {
		t.Fatalf("mismatch: eager=%d/%d lazy=%d/%d",
			eagerRes.ExitCode, eagerRes.GasUsed, lazyRes.ExitCode, lazyRes.GasUsed)
	}
	if eagerRes.ExitCode != 9 {
		t.Fatalf("XLOAD on a virtualized pruned branch must fail with catchable cell_und(9), got %d", eagerRes.ExitCode)
	}
}

// lazyParityProofBody builds a usage-proof body whose child is a genuine
// pruned branch: raw (level 1) and virtualized-to-0 flavors.
func lazyParityProofBody(t *testing.T) (*cell.Cell, *cell.Cell) {
	t.Helper()

	branch := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreRef(branch).
		EndCell()

	proof := mustUsageProofWithLoadedRoot(t, root)
	rawBody, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap raw proof: %v", err)
	}
	virtBody, err := cell.UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap virtualized proof: %v", err)
	}
	return rawBody, virtBody
}

// lazyParityMaterialize materializes a lazy placeholder (its own refs then
// come back as lazy placeholders themselves).
func lazyParityMaterialize(t *testing.T, c *cell.Cell) *cell.Cell {
	t.Helper()
	sl, err := c.BeginParse()
	if err != nil {
		t.Fatalf("materialize lazy: %v", err)
	}
	return sl.BaseCell()
}

// lazyParityWrapReload round-trips a cell through a lazy BOC behind a wrapper
// ref so the interesting cell itself comes back as a lazy placeholder.
func lazyParityWrapReload(t *testing.T, c *cell.Cell) *cell.Cell {
	t.Helper()

	wrapper := cell.BeginCell().MustStoreRef(c).EndCell()
	reloaded := lazyParityReload(t, wrapper, true)
	inner, err := reloaded.PeekRef(0)
	if err != nil {
		t.Fatalf("peek lazy inner: %v", err)
	}
	return inner
}

// CTOS of a virtualized lazy ref pointing at an ordinary higher-level cell
// must succeed like the eager equivalent; the virtualization check applies to
// the materialized cell, not to the pruned-shaped lazy placeholder.
func TestLazyVirtualizedRefLoadMatchesEager(t *testing.T) {
	rawBody, virtBody := lazyParityProofBody(t)
	if virtBody.GetType() != cell.OrdinaryCellType || !virtBody.IsVirtualized() {
		t.Fatalf("fixture degenerated: body type=%v virt=%v", virtBody.GetType(), virtBody.IsVirtualized())
	}

	ctos := cell.BeginCell().MustStoreUInt(0xD0, 8).MustStoreUInt(0x30, 8).EndCell() // CTOS; DROP

	eagerRes := lazyParityExecute(t, ctos, 13, virtBody)

	lazyBody := lazyParityWrapReload(t, rawBody)
	if !lazyBody.IsLazy() {
		t.Skip("lazy BOC loader materialized the ref eagerly; nothing to compare")
	}
	lazyRes := lazyParityExecute(t, ctos, 13, lazyBody.Virtualize(0))

	if eagerRes.ExitCode != lazyRes.ExitCode {
		t.Fatalf("exit mismatch: eager=%d lazy=%d", eagerRes.ExitCode, lazyRes.ExitCode)
	}
	if eagerRes.GasUsed != lazyRes.GasUsed {
		t.Fatalf("gas mismatch: eager=%d lazy=%d", eagerRes.GasUsed, lazyRes.GasUsed)
	}
	if eagerRes.ExitCode != 0 {
		t.Fatalf("loading a virtualized ordinary cell must succeed, got exit %d", eagerRes.ExitCode)
	}

	// Control: a really-pruned child above the effective level still aborts
	// identically on both loading modes (uncatchable virtualization error).
	eagerPruned, err := virtBody.PeekRef(0)
	if err != nil {
		t.Fatalf("peek eager pruned: %v", err)
	}
	eagerCtl := lazyParityExecute(t, ctos, 13, eagerPruned)
	lazyPruned, err := lazyParityMaterialize(t, lazyParityWrapReload(t, rawBody)).Virtualize(0).PeekRef(0)
	if err != nil {
		t.Fatalf("peek lazy pruned: %v", err)
	}
	lazyCtl := lazyParityExecute(t, ctos, 13, lazyPruned)
	if eagerCtl.ExitCode != lazyCtl.ExitCode || eagerCtl.GasUsed != lazyCtl.GasUsed {
		t.Fatalf("pruned control mismatch: eager=%d/%d lazy=%d/%d",
			eagerCtl.ExitCode, eagerCtl.GasUsed, lazyCtl.ExitCode, lazyCtl.GasUsed)
	}
}

// A dictionary walk loads its nodes like any other cell: a pruned branch above
// the effective level aborts the whole VM (uncatchable, reported as -15) and
// never degrades to a catchable cell underflow.
func TestDictWalkVirtualizedPrunedAborts(t *testing.T) {
	d := cell.NewDict(8)
	for i := 0; i < 8; i++ {
		if err := d.SetIntKey(big.NewInt(int64(i*16)), cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell()); err != nil {
			t.Fatalf("seed dict: %v", err)
		}
	}
	root := d.AsCell()

	builder := cell.NewMerkleProofBuilder(root)
	if _, err := builder.Root().BeginParse(); err != nil {
		t.Fatalf("touch root: %v", err)
	}
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	virtRoot, err := cell.UnwrapProofVirtualized(proof, root.Hash())
	if err != nil {
		t.Fatalf("virtualize proof: %v", err)
	}

	// DICTUGET over a root whose children are pruned above the effective level
	code := cell.BeginCell().MustStoreUInt(0xF40E, 16).EndCell()
	res := lazyParityExecute(t, code, 13, big.NewInt(0), virtRoot, big.NewInt(8))

	if res.ExitCode != ^int64(vmerr.CodeVirtualization) {
		t.Fatalf("dict walk over a virtualized pruned node = exit %d, want %d (uncatchable virtualization abort)",
			res.ExitCode, ^int64(vmerr.CodeVirtualization))
	}

	// The abort must bypass c2: wrapping it in TRY changes nothing.
	// DROP; PUSHCONT{DICTUGET}; PUSHCONT{}; TRY
	tryCode := cell.BeginCell().
		MustStoreUInt(0x92, 8).MustStoreUInt(0xF40E, 16). // PUSHCONT 2 bytes {DICTUGET}
		MustStoreUInt(0x90, 8).                           // PUSHCONT {}
		MustStoreUInt(0xF2FF, 16).                        // TRY
		EndCell()
	tryRes := lazyParityExecute(t, tryCode, 13, big.NewInt(0), virtRoot, big.NewInt(8))
	if tryRes.ExitCode != ^int64(vmerr.CodeVirtualization) {
		t.Fatalf("TRY must not catch the virtualization abort, got exit %d", tryRes.ExitCode)
	}
}
