package cell

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"testing"
	"unsafe"
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

// buildPackedExtraHashFixture is a graph whose cells cover every shape of
// extra-hash window the packed layout has to size: level masks 001, 010 and
// 100 (one slot each, at three different levels), 011 (two slots) and 111
// (three), next to level-0 cells (no window) and pruned branches (no window,
// their higher hashes live in the payload). The pruned branches are cut from
// the same leaf at different levels, which is what produces the sparse masks.
func buildPackedExtraHashFixture(tb testing.TB) *Cell {
	tb.Helper()

	leaf := BeginCell().MustStoreUInt(0x1ea5, 16).EndCell()
	wrap := func(tag uint64, refs ...*Cell) *Cell {
		b := BeginCell().MustStoreUInt(tag, 32)
		for _, ref := range refs {
			b.MustStoreRef(ref)
		}
		return b.EndCell()
	}
	plain := wrap(0x00, leaf)
	// The boundary is cut from a cell with a reference: a bare leaf is returned
	// as-is by CreatePrunedBranch rather than pruned.
	prunedAt := func(level int) *Cell {
		pruned, err := CreatePrunedBranch(plain, level, 0)
		if err != nil {
			tb.Fatal(err)
		}
		if pruned.GetType() != PrunedCellType || pruned.getLevelMask().Mask != oneLevelMask(level) {
			tb.Fatalf("pruned branch at level %d has mask %03b", level, pruned.getLevelMask().Mask)
		}
		return pruned
	}
	a1 := wrap(0x01, prunedAt(1), plain)
	a2 := wrap(0x02, prunedAt(2), leaf)
	a3 := wrap(0x03, prunedAt(3))
	b12 := wrap(0x12, a1, a2, plain)
	c123 := wrap(0x123, b12, a3, a1)
	root := wrap(0xf00, c123, a2, prunedAt(2), leaf)

	seen := map[byte]bool{}
	for _, c := range []*Cell{plain, a1, a2, a3, b12, c123, root} {
		seen[c.getLevelMask().Mask] = true
	}
	for _, mask := range []byte{0b000, 0b001, 0b010, 0b100, 0b011, 0b111} {
		if !seen[mask] {
			tb.Fatalf("fixture has no cell with level mask %03b", mask)
		}
	}
	return root
}

// TestParsedExtraHashesArePackedAndExact is the gate on the packed window
// layout of prewireParsedExtraHashes. Two things are held over a graph with
// every window shape, through both finalize modes and both payload modes:
// every hash and depth of every cell at every level equals the builder's,
// which is what proves no cell's finalization wrote past its own window into
// a neighbour's; and the windows really are laid end to end by popcount, so
// the slab is as small as the layout claims.
func TestParsedExtraHashesArePackedAndExact(t *testing.T) {
	root := buildPackedExtraHashFixture(t)
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, WithIntHashes: true})

	for _, tc := range []struct {
		name    string
		options BOCParseOptions
	}{
		{"Copy", BOCParseOptions{AllowNonZeroLevelRoot: true}},
		{"NoCopy", BOCParseOptions{AllowNonZeroLevelRoot: true, NoCopyPayload: true}},
		{"Trusted", BOCParseOptions{AllowNonZeroLevelRoot: true, TrustedHashes: true}},
	} {
		for _, threshold := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, threshold == 1), func(t *testing.T) {
				parsed := parseWithFinalizeThreshold(t, boc, tc.options, threshold)
				assertParsedGraphHashParity(t, root, parsed)
				if got := parsed.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true, WithIntHashes: true}); !bytes.Equal(got, boc) {
					t.Fatal("parsed graph does not serialize back to the same bytes")
				}
				assertExtraHashWindowsPacked(t, parsed)
			})
		}
	}
}

// assertParsedGraphHashParity walks two graphs of the same shape in step and
// compares level mask, every level's hash and depth, and the payload.
func assertParsedGraphHashParity(t *testing.T, want, got *Cell) {
	t.Helper()

	type pair struct{ want, got *Cell }
	queue := []pair{{want, got}}
	seen := map[*Cell]bool{}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if seen[cur.got] {
			continue
		}
		seen[cur.got] = true
		if cur.want.getLevelMask() != cur.got.getLevelMask() || cur.want.IsSpecial() != cur.got.IsSpecial() ||
			cur.want.bitsSz != cur.got.bitsSz || !bytes.Equal(cur.want.data, cur.got.data) {
			t.Fatalf("parsed cell shape differs: mask %03b/%03b", cur.want.getLevelMask().Mask, cur.got.getLevelMask().Mask)
		}
		for level := 0; level <= _DataCellMaxLevel; level++ {
			if !bytes.Equal(cur.want.getHash(level), cur.got.getHash(level)) {
				t.Fatalf("hash at level %d differs on a cell with mask %03b", level, cur.want.getLevelMask().Mask)
			}
			if cur.want.getDepth(level) != cur.got.getDepth(level) {
				t.Fatalf("depth at level %d differs on a cell with mask %03b", level, cur.want.getLevelMask().Mask)
			}
		}
		if cur.want.refsCount() != cur.got.refsCount() {
			t.Fatal("parsed cell reference count differs")
		}
		for i := 0; i < cur.want.refsCount(); i++ {
			queue = append(queue, pair{cur.want.refs[i], cur.got.refs[i]})
		}
	}
}

// assertExtraHashWindowsPacked checks that the extra-hash windows of a parsed
// graph are consecutive slices of one slab, each exactly as long as the number
// of slots its cell's level mask uses.
func assertExtraHashWindowsPacked(t *testing.T, root *Cell) {
	t.Helper()

	var windows []*Cell
	for c := range collectDetachedTestGraph(root) {
		if c.meta != nil && c.meta.extraHashes != nil {
			if c.GetType() == PrunedCellType || c.getLevelMask().Mask == 0 {
				t.Fatal("a cell without extra hashes was given a window")
			}
			windows = append(windows, c)
		}
	}
	if len(windows) < 5 {
		t.Fatalf("fixture yielded %d windows, want at least the five window-bearing masks", len(windows))
	}
	sort.Slice(windows, func(i, j int) bool {
		return uintptr(unsafe.Pointer(windows[i].meta.extraHashes)) < uintptr(unsafe.Pointer(windows[j].meta.extraHashes))
	})
	for i := 1; i < len(windows); i++ {
		prev, next := windows[i-1], windows[i]
		gap := uintptr(unsafe.Pointer(next.meta.extraHashes)) - uintptr(unsafe.Pointer(prev.meta.extraHashes))
		if want := uintptr(prev.extraHashSlots()) * hashSize; gap != want {
			t.Fatalf("window after a mask-%03b cell starts %d bytes later, want %d: the slab is not packed",
				prev.getLevelMask().Mask, gap, want)
		}
	}
}
