package cell

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCombineMerkleProofComplementaryBranches(t *testing.T) {
	left := BeginCell().MustStoreUInt(0x11, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xa1, 8).EndCell()).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xb2, 8).EndCell()).EndCell()
	root := BeginCell().MustStoreUInt(1, 1).MustStoreRef(left).MustStoreRef(right).EndCell()

	leftSkeleton := CreateProofSkeleton()
	leftSkeleton.ProofRef(0).SetRecursive()
	leftProof, err := root.CreateProof(leftSkeleton)
	if err != nil {
		t.Fatal(err)
	}
	rightSkeleton := CreateProofSkeleton()
	rightSkeleton.ProofRef(1).SetRecursive()
	rightProof, err := root.CreateProof(rightSkeleton)
	if err != nil {
		t.Fatal(err)
	}
	fullSkeleton := CreateProofSkeleton()
	fullSkeleton.SetRecursive()
	fullProof, err := root.CreateProof(fullSkeleton)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(leftProof.ToBOC(), rightProof.ToBOC()) {
		t.Fatal("complementary proof fixtures are identical")
	}
	leftBody, err := UnwrapProof(leftProof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}
	rightBody, err := UnwrapProof(rightProof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if leftBody.ref(1).GetType() != PrunedCellType || rightBody.ref(0).GetType() != PrunedCellType {
		t.Fatal("complementary proof fixtures do not hide opposite branches")
	}

	for _, tc := range []struct {
		name string
		fn   func(*Cell, *Cell) (*Cell, error)
	}{
		{name: "general", fn: CombineMerkleProof},
		{name: "fast", fn: CombineMerkleProofFast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			combined, err := tc.fn(leftProof, rightProof)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(combined.ToBOC(), fullProof.ToBOC()) {
				t.Fatalf("combined proof differs from full proof\ngot:  %s\nwant: %s", combined.Dump(), fullProof.Dump())
			}
			body, err := UnwrapProof(combined, root.Hash())
			if err != nil {
				t.Fatal(err)
			}
			if body.ref(0).HashKey() != left.HashKey() || body.ref(1).HashKey() != right.HashKey() {
				t.Fatal("combined proof did not reveal both branches")
			}
		})
	}

	for _, fn := range []func(*Cell, *Cell) (*Cell, error){CombineMerkleProofRaw, CombineMerkleProofFastRaw} {
		combined, err := fn(leftBody, rightBody)
		if err != nil {
			t.Fatal(err)
		}
		wrapped, err := CreateMerkleProof(combined)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(wrapped.ToBOC(), fullProof.ToBOC()) {
			t.Fatal("raw combined proof differs from full proof")
		}
	}
}

func TestCombineMerkleProofPrunedSlotUsesLastProof(t *testing.T) {
	storedHash := bytes.Repeat([]byte{0x5a}, hashSize)
	left := prunedBranchWithStoredMeta(t, storedHash, 1)
	right := prunedBranchWithStoredMeta(t, storedHash, 2)
	if left.HashKey() == right.HashKey() {
		t.Fatal("pruned fixtures must have different top hashes")
	}

	generalRaw, err := CombineMerkleProofRaw(left, right)
	if err != nil {
		t.Fatal(err)
	}
	fastRaw, err := CombineMerkleProofFastRaw(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if generalRaw != right || fastRaw != right {
		t.Fatalf("raw combine did not retain last pruned proof: general=%p fast=%p right=%p", generalRaw, fastRaw, right)
	}

	leftWrapped, err := CreateMerkleProof(left)
	if err != nil {
		t.Fatal(err)
	}
	rightWrapped, err := CreateMerkleProof(right)
	if err != nil {
		t.Fatal(err)
	}
	general, err := CombineMerkleProof(leftWrapped, rightWrapped)
	if err != nil {
		t.Fatal(err)
	}
	fast, err := CombineMerkleProofFast(leftWrapped, rightWrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(general.ToBOC(), rightWrapped.ToBOC()) || !bytes.Equal(fast.ToBOC(), rightWrapped.ToBOC()) {
		t.Fatal("wrapped combine did not retain last pruned proof")
	}
}

func TestCombineMerkleProofFastLazyEqualityDoesNotLoad(t *testing.T) {
	source := BeginCell().MustStoreUInt(0x5a, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xa5, 8).EndCell()).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{source.HashKey(0): source}}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(source), loader.LoadCell)

	var combinedRaw *Cell
	var combineErr error
	if allocs := testing.AllocsPerRun(1000, func() {
		combinedRaw, combineErr = CombineMerkleProofFastRaw(lazy, lazy)
	}); allocs != 0 {
		t.Fatalf("fast raw lazy equality shortcut allocated: %v", allocs)
	}
	if combineErr != nil {
		t.Fatal(combineErr)
	}
	if combinedRaw != lazy {
		t.Fatal("fast raw combine materialized an equal lazy boundary")
	}
	if loader.calls != 0 {
		t.Fatalf("fast raw equality shortcut loaded %d cells, want 0", loader.calls)
	}

	wrapped, err := CreateMerkleProof(lazy)
	if err != nil {
		t.Fatal(err)
	}
	combined, err := CombineMerkleProofFast(wrapped, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	body, err := UnwrapProof(combined, lazy.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if body != lazy {
		t.Fatal("wrapped fast combine materialized an equal lazy boundary")
	}
	if loader.calls != 0 {
		t.Fatalf("wrapped fast equality shortcut loaded %d cells, want 0", loader.calls)
	}
}

func TestCombineMerkleProofRejectsLazyNonZeroLevelBeforeLoad(t *testing.T) {
	pruned := prunedBranchWithStoredMeta(t, bytes.Repeat([]byte{0x71}, hashSize), 3)
	loadCalls := 0
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(pruned), func(Hash) (*Cell, error) {
		loadCalls++
		return nil, errors.New("unexpected lazy load")
	})

	if _, err := CombineMerkleProof(lazy, lazy); err == nil || !strings.Contains(err.Error(), "level is not zero") {
		t.Fatalf("lazy non-zero-level proof error = %v, want level error", err)
	}
	if loadCalls != 0 {
		t.Fatalf("non-zero-level proof validation loaded %d cells, want 0", loadCalls)
	}
}

func TestCombineMerkleProofBoundaries(t *testing.T) {
	rootA := BeginCell().MustStoreUInt(1, 1).EndCell()
	rootB := BeginCell().MustStoreUInt(0, 1).EndCell()
	proofA, err := rootA.CreateProof(CreateProofSkeleton())
	if err != nil {
		t.Fatal(err)
	}
	proofB, err := rootB.CreateProof(CreateProofSkeleton())
	if err != nil {
		t.Fatal(err)
	}

	if got, err := CombineMerkleProof(nil, proofA); err != nil || got != proofA {
		t.Fatalf("nil-left combine = %p, %v, want original proof", got, err)
	}
	if got, err := CombineMerkleProof(proofA, nil); err != nil || got != proofA {
		t.Fatalf("nil-right combine = %p, %v, want original proof", got, err)
	}
	if _, err := CombineMerkleProof(proofA, proofB); err == nil {
		t.Fatal("different-root proofs combined successfully")
	}
	if _, err := CombineMerkleProofRaw(nil, rootA); err == nil {
		t.Fatal("nil raw proof combined successfully")
	}
}

func TestCombineMerkleProofSharedDiamond(t *testing.T) {
	left := BeginCell().MustStoreUInt(1, 32).EndCell()
	right := BeginCell().MustStoreUInt(2, 32).EndCell()
	prunedLeft, err := buildPrunedBranchFromCellAtDepth(left, 1, _DataCellMaxLevel, nil)
	if err != nil {
		t.Fatal(err)
	}
	prunedRight, err := buildPrunedBranchFromCellAtDepth(right, 1, _DataCellMaxLevel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prunedLeft.GetType() != PrunedCellType || prunedRight.GetType() != PrunedCellType {
		t.Fatal("shared-diamond fixture failed to create pruned branches")
	}

	firstBody := BeginCell().MustStoreRef(prunedLeft).MustStoreRef(right).EndCell()
	secondBody := BeginCell().MustStoreRef(left).MustStoreRef(prunedRight).EndCell()
	fullBody := BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()
	for range 100 {
		firstBody = BeginCell().MustStoreRef(firstBody).MustStoreRef(firstBody).EndCell()
		secondBody = BeginCell().MustStoreRef(secondBody).MustStoreRef(secondBody).EndCell()
		fullBody = BeginCell().MustStoreRef(fullBody).MustStoreRef(fullBody).EndCell()
	}

	first, err := CreateMerkleProof(firstBody)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateMerkleProof(secondBody)
	if err != nil {
		t.Fatal(err)
	}
	full, err := CreateMerkleProof(fullBody)
	if err != nil {
		t.Fatal(err)
	}

	for _, fn := range []func(*Cell, *Cell) (*Cell, error){CombineMerkleProof, CombineMerkleProofFast} {
		combined, err := fn(first, second)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(combined.ToBOC(), full.ToBOC()) {
			t.Fatal("combined shared-DAG proof differs from full proof")
		}
		body, err := UnwrapProof(combined, fullBody.Hash())
		if err != nil {
			t.Fatal(err)
		}
		if body.ref(0) != body.ref(1) {
			t.Fatal("combined proof did not retain shared-DAG references")
		}
	}
}

var proofCombineBenchmarkSink *Cell

func BenchmarkCombineMerkleProofSharedDiamond(b *testing.B) {
	left := BeginCell().MustStoreUInt(1, 32).EndCell()
	right := BeginCell().MustStoreUInt(2, 32).EndCell()
	prunedLeft, err := buildPrunedBranchFromCellAtDepth(left, 1, _DataCellMaxLevel, nil)
	if err != nil {
		b.Fatal(err)
	}
	prunedRight, err := buildPrunedBranchFromCellAtDepth(right, 1, _DataCellMaxLevel, nil)
	if err != nil {
		b.Fatal(err)
	}
	if prunedLeft.GetType() != PrunedCellType || prunedRight.GetType() != PrunedCellType {
		b.Fatal("shared-diamond fixture failed to create pruned branches")
	}
	firstBody := BeginCell().MustStoreRef(prunedLeft).MustStoreRef(right).EndCell()
	secondBody := BeginCell().MustStoreRef(left).MustStoreRef(prunedRight).EndCell()
	for range 32 {
		firstBody = BeginCell().MustStoreRef(firstBody).MustStoreRef(firstBody).EndCell()
		secondBody = BeginCell().MustStoreRef(secondBody).MustStoreRef(secondBody).EndCell()
	}
	first, err := CreateMerkleProof(firstBody)
	if err != nil {
		b.Fatal(err)
	}
	second, err := CreateMerkleProof(secondBody)
	if err != nil {
		b.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		fn   func(*Cell, *Cell) (*Cell, error)
	}{
		{name: "general", fn: CombineMerkleProof},
		{name: "fast", fn: CombineMerkleProofFast},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				combined, err := tc.fn(first, second)
				if err != nil {
					b.Fatal(err)
				}
				proofCombineBenchmarkSink = combined
			}
		})
	}
}
