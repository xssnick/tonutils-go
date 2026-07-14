package cell

import (
	"bytes"
	"sync"
	"testing"
)

func buildFinalizeParityTree(tb testing.TB, depth int, counter *uint64) *Cell {
	tb.Helper()

	b := BeginCell()
	*counter++
	b.MustStoreUInt(*counter, 64)
	b.MustStoreUInt(*counter^0xdeadbeef, 64)
	if depth > 0 {
		for i := 0; i < 4; i++ {
			b.MustStoreRef(buildFinalizeParityTree(tb, depth-1, counter))
		}
	}
	return b.EndCell()
}

func parseWithFinalizeThreshold(tb testing.TB, boc []byte, options BOCParseOptions, threshold int) *Cell {
	tb.Helper()

	old := ParallelBOCMinCells
	ParallelBOCMinCells = threshold
	defer func() {
		ParallelBOCMinCells = old
	}()

	roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), options)
	if err != nil {
		tb.Fatalf("failed to parse boc: %v", err)
	}
	if len(roots) != 1 {
		tb.Fatalf("unexpected roots count: %d", len(roots))
	}
	return roots[0]
}

func TestParallelFinalizeParity(t *testing.T) {
	var counter uint64
	// depth 7 with fanout 4 gives 21845 cells, above the default parallel threshold
	root := buildFinalizeParityTree(t, 7, &counter)
	boc := root.ToBOC()

	for _, tc := range []struct {
		name    string
		options BOCParseOptions
	}{
		{"Default", BOCParseOptions{}},
		{"NoCopy", BOCParseOptions{NoCopyPayload: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sequential := parseWithFinalizeThreshold(t, boc, tc.options, 0)
			parallel := parseWithFinalizeThreshold(t, boc, tc.options, 1)

			if !bytes.Equal(sequential.Hash(), parallel.Hash()) {
				t.Fatalf("parallel finalize root hash mismatch")
			}
			if !bytes.Equal(root.Hash(), parallel.Hash()) {
				t.Fatalf("parsed root hash does not match source")
			}
			if !bytes.Equal(parallel.ToBOC(), boc) {
				t.Fatalf("parallel finalize boc roundtrip mismatch")
			}
		})
	}
}

func TestParallelFinalizeParityProof(t *testing.T) {
	var counter uint64
	root := buildFinalizeParityTree(t, 5, &counter)

	// prune most branches to get a proof full of special and level>0 cells
	skeleton := CreateProofSkeleton()
	skeleton.ProofRef(0).ProofRef(1).ProofRef(2).SetRecursive()
	proof, err := root.CreateProof(skeleton)
	if err != nil {
		t.Fatalf("failed to create proof: %v", err)
	}

	boc := proof.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, WithTopHash: true, WithIntHashes: true})

	for _, options := range []BOCParseOptions{
		{},
		{TrustedHashes: true},
		{NoCopyPayload: true},
	} {
		sequential := parseWithFinalizeThreshold(t, boc, options, 0)
		parallel := parseWithFinalizeThreshold(t, boc, options, 1)

		if !bytes.Equal(sequential.Hash(), parallel.Hash()) {
			t.Fatalf("parallel finalize proof root hash mismatch (options %+v)", options)
		}
		if !bytes.Equal(proof.Hash(), parallel.Hash()) {
			t.Fatalf("parsed proof hash does not match source (options %+v)", options)
		}
		if err = CheckProof(parallel, root.Hash()); err != nil {
			t.Fatalf("parsed proof check failed (options %+v): %v", options, err)
		}
	}
}

func TestParallelFinalizeDepthLimit(t *testing.T) {
	// a hand-crafted chain deeper than maxDepth must be rejected by the
	// parallel path during height computation, same as the sequential path
	// rejects it during hash calculation
	chainLen := maxDepth + 6

	var boc []byte
	boc = append(boc, bocMagic...)
	boc = append(boc, 0x02, 0x02)                        // ref size 2, offset size 2
	boc = append(boc, byte(chainLen>>8), byte(chainLen)) // cells
	boc = append(boc, 0x00, 0x01)                        // roots
	boc = append(boc, 0x00, 0x00)                        // absent
	dataLen := (chainLen-1)*4 + 2
	boc = append(boc, byte(dataLen>>8), byte(dataLen)) // tot_cells_size
	boc = append(boc, 0x00, 0x00)                      // root index
	for i := 0; i < chainLen-1; i++ {
		next := i + 1
		boc = append(boc, 0x01, 0x00, byte(next>>8), byte(next))
	}
	boc = append(boc, 0x00, 0x00) // leaf cell

	for _, threshold := range []int{0, 1} {
		old := ParallelBOCMinCells
		ParallelBOCMinCells = threshold
		_, err := FromBOC(boc)
		ParallelBOCMinCells = old

		if err == nil {
			t.Fatalf("expected depth limit error with threshold %d", threshold)
		}
	}
}

func TestParallelLazyMetaParity(t *testing.T) {
	var counter uint64
	// 21845 cells: above the default parallel threshold for computed lazy meta
	root := buildFinalizeParityTree(t, 7, &counter)
	boc := root.ToBOC()

	for _, threshold := range []int{0, 1} {
		lazyRoot := parseWithFinalizeThreshold(t, boc, BOCParseOptions{Lazy: true}, threshold)

		if !bytes.Equal(root.Hash(), lazyRoot.Hash()) {
			t.Fatalf("lazy root hash mismatch (threshold %d)", threshold)
		}
		if err := materializeTestCellTree(lazyRoot); err != nil {
			t.Fatalf("failed to materialize lazy tree (threshold %d): %v", threshold, err)
		}
	}
}

func TestLazyCacheConcurrentMaterialize(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xCAFE, 32).EndCell()
	b := BeginCell().MustStoreUInt(1, 8)
	for i := 0; i < 4; i++ {
		b.MustStoreRef(BeginCell().MustStoreUInt(uint64(i), 16).MustStoreRef(shared).EndCell())
	}
	root := b.EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, WithIndex: true, WithCacheBits: true, WithTopHash: true, WithIntHashes: true})

	lazyRoot := parseWithFinalizeThreshold(t, boc, BOCParseOptions{Lazy: true, TrustedHashes: true}, 0)

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for g := 0; g < len(errs); g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			errs[g] = materializeTestCellTree(lazyRoot)
		}(g)
	}
	wg.Wait()
	for g, err := range errs {
		if err != nil {
			t.Fatalf("concurrent materialize #%d failed: %v", g, err)
		}
	}

	if !bytes.Equal(root.Hash(), lazyRoot.Hash()) {
		t.Fatalf("lazy root hash mismatch after concurrent materialize")
	}
}

func TestToBOCWithCellsCountHint(t *testing.T) {
	var counter uint64
	root := buildFinalizeParityTree(t, 5, &counter)

	want := root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true})
	for _, hint := range []int{-1, 1, 100, 1365, 1 << 20} {
		got := root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, CellsCountHint: hint})
		if !bytes.Equal(want, got) {
			t.Fatalf("hint %d produced different boc", hint)
		}
	}
}
