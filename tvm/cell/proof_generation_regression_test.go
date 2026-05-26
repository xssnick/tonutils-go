package cell

import "testing"

func mustEndCell(t testing.TB, b *Builder) *Cell {
	t.Helper()
	return b.EndCell()
}

func mustCellResult(t testing.TB, c *Cell, err error) *Cell {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return c
}

func TestCreateProofNestedMerkleProofUsesMerkleDepth(t *testing.T) {
	keepLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x11, 8))
	hiddenLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x22, 8))
	hiddenBranch := mustEndCell(t, BeginCell().MustStoreRef(hiddenLeaf))
	innerBody := mustEndCell(t, BeginCell().MustStoreRef(keepLeaf).MustStoreRef(hiddenBranch))
	innerProofCell, err := CreateMerkleProof(innerBody)
	innerProof := mustCellResult(t, innerProofCell, err)
	root := mustEndCell(t, BeginCell().MustStoreRef(innerProof).MustStoreUInt(0x33, 8))

	sk := CreateProofSkeleton()
	sk.ProofRef(0).ProofRef(0).ProofRef(0).SetRecursive()

	proof, err := root.CreateProof(sk)
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	if err = validateLoadedCell(proof); err != nil {
		t.Fatalf("proof validation failed: %v", err)
	}

	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}

	nestedProof := body.ref(0)
	if nestedProof == nil || nestedProof.GetType() != MerkleProofCellType {
		t.Fatalf("expected nested merkle proof, got %v", nestedProof)
	}
	if err = validateLoadedCell(nestedProof); err != nil {
		t.Fatalf("nested merkle proof validation failed: %v", err)
	}

	nestedBody, err := UnwrapProof(nestedProof, innerBody.Hash())
	if err != nil {
		t.Fatalf("unwrap nested proof: %v", err)
	}

	pruned := nestedBody.ref(1)
	if pruned == nil || pruned.GetType() != PrunedCellType {
		t.Fatalf("expected pruned branch in nested proof body, got %v", pruned)
	}
	if pruned.Level() != 2 {
		t.Fatalf("unexpected nested proof pruned level: got %d want %d", pruned.Level(), 2)
	}
}

func TestCreateProofNestedMerkleUpdateUsesMerkleDepth(t *testing.T) {
	fromLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x44, 8))
	fromBranch := mustEndCell(t, BeginCell().MustStoreRef(fromLeaf))
	toLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x55, 8))
	toBranch := mustEndCell(t, BeginCell().MustStoreRef(toLeaf))
	nestedUpdateCell, err := CreateMerkleUpdate(fromBranch, toBranch)
	nestedUpdate := mustCellResult(t, nestedUpdateCell, err)
	root := mustEndCell(t, BeginCell().MustStoreRef(nestedUpdate))

	sk := CreateProofSkeleton()
	sk.ProofRef(0).ProofRef(0).SetRecursive()

	proof, err := root.CreateProof(sk)
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	if err = validateLoadedCell(proof); err != nil {
		t.Fatalf("proof validation failed: %v", err)
	}

	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}

	updateCell := body.ref(0)
	if updateCell == nil || updateCell.GetType() != MerkleUpdateCellType {
		t.Fatalf("expected nested merkle update, got %v", updateCell)
	}
	if err = validateLoadedCell(updateCell); err != nil {
		t.Fatalf("nested merkle update validation failed: %v", err)
	}

	pruned := updateCell.ref(1)
	if pruned == nil || pruned.GetType() != PrunedCellType {
		t.Fatalf("expected pruned branch in nested update, got %v", pruned)
	}
	if pruned.Level() != 2 {
		t.Fatalf("unexpected nested update pruned level: got %d want %d", pruned.Level(), 2)
	}
}

func TestCreateProofRecursiveFromVirtualizedRootMaterializesBody(t *testing.T) {
	leaf := mustEndCell(t, BeginCell().MustStoreUInt(0x66, 8))
	hiddenLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x77, 8))
	branch := mustEndCell(t, BeginCell().MustStoreRef(hiddenLeaf))
	prunedCell, err := createPrunedBranchFromCell(branch, 1)
	pruned := mustCellResult(t, prunedCell, err)
	root := mustEndCell(t, BeginCell().MustStoreRef(leaf).MustStoreRef(pruned))
	if root.Level() == 0 {
		t.Fatal("expected non-zero raw root level")
	}

	virtualized := root.Virtualize(0)
	if !virtualized.IsVirtualized() {
		t.Fatal("expected virtualized root")
	}

	sk := CreateProofSkeleton()
	sk.SetRecursive()

	proof, err := virtualized.CreateProof(sk)
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	if err = validateLoadedCell(proof); err != nil {
		t.Fatalf("proof validation failed: %v", err)
	}

	body, err := UnwrapProof(proof, virtualized.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}
	if body.IsVirtualized() {
		t.Fatal("expected materialized proof body for virtualized root")
	}
	if body.Level() != 0 {
		t.Fatalf("unexpected proof body level: got %d want %d", body.Level(), 0)
	}
}

func TestRebuildWithRefsMaterializesVirtualizedCell(t *testing.T) {
	hiddenLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x10, 8))
	hiddenBranch := mustEndCell(t, BeginCell().MustStoreRef(hiddenLeaf))
	prunedCell, err := createPrunedBranchFromCell(hiddenBranch, 1)
	if err != nil {
		t.Fatal(err)
	}
	root := mustEndCell(t, BeginCell().MustStoreRef(prunedCell))
	virtualized := root.Virtualize(0)
	if !virtualized.IsVirtualized() {
		t.Fatal("expected virtualized root")
	}

	replacement := mustEndCell(t, BeginCell().MustStoreUInt(0x20, 8))
	rebuilt, err := virtualized.RebuildWithRefs([]*Cell{replacement})
	if err != nil {
		t.Fatalf("rebuild with refs: %v", err)
	}
	if rebuilt.IsVirtualized() {
		t.Fatal("rebuilt cell should be materialized")
	}
	if rebuilt.Level() != 0 {
		t.Fatalf("unexpected rebuilt level: got %d want %d", rebuilt.Level(), 0)
	}

	ref, err := rebuilt.MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := ref.MustLoadUInt(8); got != 0x20 {
		t.Fatalf("unexpected rebuilt ref value: got %x", got)
	}
}

func TestMerkleProofBuilderVirtualizedMerkleUpdateUsesLoadedBoundaryView(t *testing.T) {
	hiddenLeaf := mustEndCell(t, BeginCell().MustStoreUInt(0x88, 8))
	hiddenBranch := mustEndCell(t, BeginCell().MustStoreRef(hiddenLeaf))
	hiddenBoundary, err := createPrunedBranchFromCell(hiddenBranch, 2)
	if err != nil {
		t.Fatal(err)
	}

	oldState := mustEndCell(t, BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(hiddenBoundary))
	newState := mustEndCell(t, BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(hiddenBoundary))
	if oldState.Level() != 2 || newState.Level() != 2 {
		t.Fatalf("expected level 2 state roots, got old=%d new=%d", oldState.Level(), newState.Level())
	}

	update := mustMerkleUpdateCell(t, oldState, newState)
	if update.Level() != 1 {
		t.Fatalf("expected level 1 merkle update, got %d", update.Level())
	}

	info := mustEndCell(t, BeginCell().MustStoreUInt(0x33, 8))
	valueFlow := mustEndCell(t, BeginCell().MustStoreUInt(0x44, 8))
	extra := mustEndCell(t, BeginCell().MustStoreUInt(0x55, 8))
	root := mustEndCell(t, BeginCell().
		MustStoreUInt(0xAABBCCDD, 32).
		MustStoreRef(info).
		MustStoreRef(valueFlow).
		MustStoreRef(update).
		MustStoreRef(extra))

	loader := testLazyLoaderForCells(info, valueFlow, update, extra, oldState, newState, hiddenBoundary)
	lazyRoot := cellWithLazyRefsFromCell(root, loader.LoadCell)
	virtualRoot := lazyRoot.Virtualize(0)
	if !virtualRoot.IsVirtualized() {
		t.Fatal("expected virtualized root")
	}

	builder := NewMerkleProofBuilder(virtualRoot)
	rootSlice := builder.Root().MustBeginParse()
	if got := rootSlice.MustLoadUInt(32); got != 0xAABBCCDD {
		t.Fatalf("unexpected root magic: got %x", got)
	}

	infoRef, err := rootSlice.PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := infoRef.MustBeginParse().MustLoadUInt(8); got != 0x33 {
		t.Fatalf("unexpected info value: got %x", got)
	}

	updateRef, err := rootSlice.PeekRefCellAt(2)
	if err != nil {
		t.Fatal(err)
	}
	updateSlice := updateRef.MustBeginParse()
	newStateRef, err := updateSlice.PeekRefCellAt(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := newStateRef.MustBeginParse().MustLoadUInt(8); got != 0x22 {
		t.Fatalf("unexpected new state value: got %x", got)
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	if err = validateLoadedCell(proof); err != nil {
		t.Fatalf("proof validation failed: %v", err)
	}
	if _, err = FromBOC(proof.ToBOCWithFlags(false)); err != nil {
		t.Fatalf("proof BOC roundtrip failed: %v", err)
	}

	body, err := UnwrapProof(proof, virtualRoot.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}
	if body.ref(2).GetType() != MerkleUpdateCellType {
		t.Fatalf("expected merkle update in proof body, got %v", body.ref(2).GetType())
	}
}
