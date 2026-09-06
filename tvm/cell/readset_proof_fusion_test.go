package cell

import (
	"bytes"
	"errors"
	"testing"
)

func TestReadSetProofFusedSelectionMatchesCallbacks(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(1, 8).EndCell()
	shared := BeginCell().MustStoreUInt(2, 8).MustStoreRef(leaf).EndCell()
	tail := BeginCell().MustStoreUInt(3, 8).MustStoreRef(leaf).EndCell()
	root := BeginCell().MustStoreRef(shared).MustStoreRef(shared).MustStoreRef(tail).EndCell()
	for _, storage := range []string{"resident", "lazy"} {
		for _, selection := range []string{"empty", "root", "path", "all"} {
			t.Run(storage+"/"+selection, func(t *testing.T) {
				loader := &countingLazyLoader{cells: make(map[Hash]*Cell)}
				source := root
				if storage == "lazy" {
					source = pageProofTestGraph(root, loader)
				}
				reads := NewReadSet(source)
				if selection != "empty" {
					reads.Record(source)
				}
				if selection == "path" || selection == "all" {
					body := shared
					if storage == "lazy" {
						body = loader.cells[shared.HashKey()]
					}
					reads.Record(body)
					reads.Record(leaf)
				}
				if selection == "all" {
					body := tail
					if storage == "lazy" {
						body = loader.cells[tail.HashKey()]
					}
					reads.Record(body)
				}
				for _, subtree := range []bool{false, true} {
					proofRoot := source
					if subtree {
						proofRoot = source.MustPeekRef(0)
					}
					before := reads.Size()
					var got *Cell
					var err error
					if subtree {
						got, err = reads.ProofOf(proofRoot)
					} else {
						got, err = reads.Proof()
					}
					if err != nil {
						t.Fatal(err)
					}
					body, err := buildMerkleProofBodyByPruneFuncResolved(proofRoot,
						func(_ *Cell, _ int, hash Hash) (*Cell, bool, error) {
							_, read := reads.Contains(hash)
							return nil, !read, nil
						}, reads.recordedCell, 0, 0)
					if err != nil {
						t.Fatal(err)
					}
					want, err := CreateMerkleProof(body)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got.ToBOC(), want.ToBOC()) {
						t.Fatalf("subtree=%v: fused proof differs from separate callbacks", subtree)
					}
					if before != reads.Size() || len(loader.calls) != 0 {
						t.Fatalf("proof changed reads or loaded bodies: size %d -> %d, loads=%v", before, reads.Size(), loader.calls)
					}
				}
			})
		}
	}
}

func TestProofPruneCallbacksKeepAppliedSource(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(1, 8).EndCell()
	source := BeginCell().MustStoreUInt(2, 8).MustStoreRef(leaf).EndCell()
	other := BeginCell().MustStoreUInt(3, 8).EndCell()
	root := BeginCell().MustStoreRef(source).MustStoreRef(other).EndCell()
	wantErr := errors.New("prune callback failed")
	for _, workers := range []int{1, 2} {
		state := merkleProofPruneBuildState{
			shouldPrune: func(c *Cell, _ int, _ Hash) (*Cell, bool, error) {
				if c == source {
					return source, true, nil
				}
				// A source returned for a kept cell is not its loaded body.
				return source, false, nil
			},
			resolveLoaded: func(hash Hash) *Cell {
				if hash == source.HashKey() {
					t.Error("pruned boundary reached the loaded-body resolver")
				}
				return nil
			},
			wantApplied: true,
			parallelism: workers,
			memoHint:    proofParallelMinCells,
			parallel:    newProofParallelCache(proofParallelMinCells),
		}
		proof, applied, err := state.build(root, 0)
		if err != nil {
			t.Fatal(err)
		}
		if proof.ref(0).GetType() != PrunedCellType || applied.ref(0) != source || applied.HashKey() != root.HashKey() {
			t.Fatal("prune source was confused with the loaded body or lost from the applied root")
		}
		state = merkleProofPruneBuildState{
			shouldPrune: func(*Cell, int, Hash) (*Cell, bool, error) { return nil, false, wantErr },
			parallelism: workers,
		}
		if _, _, err := state.build(root, 0); !errors.Is(err, wantErr) {
			t.Fatalf("callback error: got %v, want %v", err, wantErr)
		}
	}
}
