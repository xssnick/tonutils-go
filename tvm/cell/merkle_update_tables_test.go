package cell

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
)

func TestMerkleUpdateExactTablesKeepFingerprintCollisionsDistinct(t *testing.T) {
	var first, second Hash
	copy(first[:4], []byte{1, 2, 3, 4})
	copy(second[:4], []byte{1, 2, 3, 4})
	first[31] = 1
	second[31] = 2

	hashes := newMerkleUpdateHashTable[int](1)
	hashes.store(first, 11)
	hashes.store(second, 22)
	if got, ok := hashes.lookup(first); !ok || got != 11 {
		t.Fatalf("first colliding hash = %d, %v", got, ok)
	}
	if got, ok := hashes.lookup(second); !ok || got != 22 {
		t.Fatalf("second colliding hash = %d, %v", got, ok)
	}

	visits := newMerkleUpdateVisitTable[int](1)
	visits.store(merkleUpdateVisitKey{hash: first, merkleDepth: 0}, 1)
	visits.store(merkleUpdateVisitKey{hash: first, merkleDepth: 1}, 2)
	visits.store(merkleUpdateVisitKey{hash: second, merkleDepth: 0}, 3)
	for key, want := range map[merkleUpdateVisitKey]int{
		{hash: first, merkleDepth: 0}:  1,
		{hash: first, merkleDepth: 1}:  2,
		{hash: second, merkleDepth: 0}: 3,
	} {
		if got, ok := visits.lookup(key); !ok || got != want {
			t.Fatalf("visit %#v = %d, %v; want %d", key, got, ok, want)
		}
	}

	left := BeginCell().MustStoreUInt(7, 8).EndCell()
	right, err := FromBOC(left.ToBOC())
	if err != nil {
		t.Fatal(err)
	}
	if left == right || left.HashKey() != right.HashKey() {
		t.Fatal("test requires distinct pointers with equal hashes")
	}
	pointers := newMerkleUpdateCellVisitTable(1)
	pointers.store(left, 0)
	if pointers.contains(right, 0) {
		t.Fatal("pointer visit table collapsed a distinct equal-hash cell")
	}
	pointers.store(right, 0)
	if !pointers.contains(left, 0) || !pointers.contains(right, 0) {
		t.Fatal("pointer visit table lost an exact entry")
	}
}

func TestValidateMerkleUpdateDescendsDistinctEqualHashPointers(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	branch := BeginCell().MustStoreRef(leaf).EndCell()
	good := testLazyLoaderForCells(leaf)
	badCalls := 0
	bad := func(Hash) (*Cell, error) {
		badCalls++
		return nil, errors.New("second equal-hash branch is unavailable")
	}
	first := cellWithLazyRefsFromCell(branch, good.LoadCell)
	second := cellWithLazyRefsFromCell(branch, bad)
	if first == second || first.HashKey() != second.HashKey() {
		t.Fatal("test requires distinct equal-hash branch pointers")
	}

	body := BeginCell().MustStoreRef(first).MustStoreRef(second).EndCell()
	update := mustMerkleUpdateCell(t, body, body)
	if err := ValidateMerkleUpdate(update); err == nil {
		t.Fatal("validation skipped the second equal-hash pointer's failing descendant")
	}
	if badCalls == 0 {
		t.Fatal("the second equal-hash pointer was not descended")
	}
}

func TestMerkleUpdateApplyArenaOutputSurvivesLaterApplies(t *testing.T) {
	tc := newMerkleUpdateLargeDictCase(t, 512, 20, 2026082501)
	first, err := ApplyMerkleUpdate(tc.from, tc.update)
	if err != nil {
		t.Fatal(err)
	}
	wantHash := first.HashKey()
	wantBOC := append([]byte(nil), first.ToBOC()...)

	for range 32 {
		if _, err = ApplyMerkleUpdate(tc.from, tc.update); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()

	if first.HashKey() != wantHash {
		t.Fatal("an arena-backed output changed after later Apply calls")
	}
	if got := first.ToBOC(); !bytes.Equal(got, wantBOC) {
		t.Fatal("an arena-backed output BOC changed after later Apply calls")
	}
}
