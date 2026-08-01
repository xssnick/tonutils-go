package cell

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

var traceAllocationSink *Trace

func TestTraceCompactBackendsAvoidExtraBoxing(t *testing.T) {
	hooks := TraceHooks{OnLoad: func(*Cell) {}}
	if allocs := testing.AllocsPerRun(1000, func() {
		traceAllocationSink = NewTrace(hooks)
	}); allocs != 1 {
		t.Fatalf("hook trace allocations: got=%v want=1", allocs)
	}

	left := NewTrace(hooks)
	right := NewTrace(hooks)
	if allocs := testing.AllocsPerRun(1000, func() {
		traceAllocationSink = CombineTraces(left, right)
	}); allocs != 1 {
		t.Fatalf("combined trace allocations: got=%v want=1", allocs)
	}
}

func TestCombineTracesReusesEquivalentComposite(t *testing.T) {
	left := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	right := NewTrace(TraceHooks{OnCreate: func() {}})
	pair := CombineTraces(left, right)

	for name, traces := range map[string][]*Trace{
		"pair then member": {pair, right},
		"first then pair":  {left, pair},
		"pair repeated":    {pair, pair},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CombineTraces(traces...); got != pair {
				t.Fatalf("equivalent combination was rebuilt: got=%p want=%p", got, pair)
			}
		})
	}

	if got := CombineTraces(right, pair); got == pair {
		t.Fatal("combination with a different notification order reused the pair")
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		traceAllocationSink = CombineTraces(pair, right)
	}); allocs != 0 {
		t.Fatalf("idempotent combination allocations: got=%v want=0", allocs)
	}
}

func TestUsageTreeCompactLayoutAndStableTraceAddresses(t *testing.T) {
	wordSize := unsafe.Sizeof(uintptr(0))
	wantTraceSize := uintptr(24)
	if wordSize == 4 {
		wantTraceSize = 16
	}
	if got := unsafe.Sizeof(Trace{}); got != wantTraceSize {
		t.Fatalf("unexpected Trace size: got=%d want=%d", got, wantTraceSize)
	}

	wantNodeSize := wantTraceSize + wordSize + 4*unsafe.Sizeof(atomic.Uint32{}) + 2*unsafe.Sizeof(TraceNode(0))
	if got := unsafe.Sizeof(usageTreeNode{}); got != wantNodeSize {
		t.Fatalf("unexpected usageTreeNode size: got=%d want=%d", got, wantNodeSize)
	}

	tree := NewCellUsageTree()
	type savedTrace struct {
		node  TraceNode
		trace *Trace
	}
	saved := []savedTrace{{node: tree.RootNode(), trace: tree.RootTrace()}}
	node := tree.RootNode()
	for i := 0; i < usageTreeInitialChunkSize+3*usageTreeChunkSize; i++ {
		node = tree.CreateChild(node, 0)
		if i%73 == 0 {
			saved = append(saved, savedTrace{node: node, trace: tree.Trace(node)})
		}
	}
	for _, item := range saved {
		if got := tree.Trace(item.node); got != item.trace {
			t.Fatalf("trace address changed for node %d: got=%p want=%p", item.node, got, item.trace)
		}
	}
}

func TestUsageTreeCellIndexVerifiesFingerprintCollisions(t *testing.T) {
	tree := NewCellUsageTree()
	nodeA := tree.RootNode()
	nodeB := tree.CreateChild(nodeA, 0)
	nodeDuplicate := tree.CreateChild(nodeA, 1)

	cellA := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	cellB := BeginCell().MustStoreUInt(0xB, 4).EndCell()
	cellA.hash0 = Hash{0x11, 0x22, 0x33, 0x44, 0xA1}
	cellB.hash0 = Hash{0x11, 0x22, 0x33, 0x44, 0xB2}
	duplicateA := cellA.copy()

	tree.OnLoad(nodeA, cellA)
	tree.OnLoad(nodeB, cellB)
	tree.OnLoad(nodeDuplicate, duplicateA)
	for i := 0; i < 128; i++ {
		node := tree.allocNode(tree.RootNode())
		tree.OnLoad(node, duplicateA)
	}

	index := newUsageTreeCellIndex(tree)
	for _, want := range []*Cell{cellA, cellB} {
		got, ok := index.loadedCellByHash(want.HashKey())
		if !ok {
			t.Fatalf("indexed cell %x was not found", want.Hash())
		}
		if got.HashKey() != want.HashKey() {
			t.Fatalf("indexed cell mismatch: got=%x want=%x", got.Hash(), want.Hash())
		}
	}
	if got, ok := index.loadedCellByHash(Hash{0x11, 0x22, 0x33, 0x44, 0xC3}); ok {
		t.Fatalf("fingerprint collision returned unrelated cell %x", got.Hash())
	}
	if got := len(index.slots); got != usageCellIndexMinSlots {
		t.Fatalf("duplicate hashes inflated the compact index: slots=%d want=%d", got, usageCellIndexMinSlots)
	}
}

func TestUsageTreeConcurrentOnLoadUsesNodeAtomics(t *testing.T) {
	tree := NewCellUsageTree()
	cell := BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	var callbacks atomic.Int32
	tree.SetCellLoadCallback(func(*Cell) {
		callbacks.Add(1)
	})

	const goroutines = 64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				tree.OnLoad(tree.RootNode(), cell)
			}
		}()
	}
	wg.Wait()

	if got := callbacks.Load(); got != 1 {
		t.Fatalf("load callback count mismatch: got=%d want=1", got)
	}
	index := newUsageTreeCellIndex(tree)
	if got, ok := index.loadedCellByHash(cell.HashKey()); !ok || got != cell {
		t.Fatal("concurrently loaded cell was not available in the post-tracking index")
	}
}

func TestUsageProofSparseArenaMatchesCompactArena(t *testing.T) {
	build := func(spareNodes int) *Cell {
		leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		branch := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
		root := BeginCell().MustStoreUInt(0xCC, 8).MustStoreRef(branch).EndCell()
		builder := NewMerkleProofBuilder(root)
		if _, err := builder.Root().MustBeginParse().LoadRef(); err != nil {
			t.Fatal(err)
		}

		for i := 0; i < spareNodes; i++ {
			builder.UsageTree().allocNode(builder.UsageTree().RootNode())
		}
		proof, err := builder.CreateProof()
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}

	compact := build(0)
	sparse := build(1 << 15)
	if !bytes.Equal(compact.ToBOCWithOptions(BOCSerializeOptions{}), sparse.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatal("sparse usage arena changed proof output")
	}
}

func TestCreateMerkleUpdateMarkJournalRestoresOnError(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xE, 4).EndCell()
	reused := BeginCell().MustStoreUInt(0xC0DE, 16).MustStoreRef(leaf).EndCell()
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(reused).EndCell()

	tree := NewCellUsageTree()
	usageFrom := from.WithTrace(tree.RootTrace())
	reusedRef, err := usageFrom.MustBeginParse().PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	preexisting := tree.allocNode(tree.RootNode())
	tree.SetMark(preexisting, true)
	for i := 0; i < 1<<15; i++ {
		tree.allocNode(tree.RootNode())
	}

	loadErr := errors.New("forced lazy load failure")
	failing := mustCreateLazyPrunedRef(t, lazyRefFromCell(leaf), func(Hash) (*Cell, error) {
		return nil, loadErr
	})
	to := BeginCell().MustStoreRef(reusedRef).MustStoreRef(failing).EndCell()
	if _, _, err = tree.createMerkleUpdateRaw(usageFrom, to); !errors.Is(err, loadErr) {
		t.Fatalf("unexpected update error: got=%v want=%v", err, loadErr)
	}

	if tree.useMark {
		t.Fatal("temporary mark mode was not restored")
	}
	if tree.HasMark(tree.RootNode()) {
		t.Fatal("journaled root mark was not cleared")
	}
	if !tree.HasMark(preexisting) {
		t.Fatal("preexisting mark was not preserved")
	}
}

func TestCreateMerkleUpdateLoadedReuseKeepsUsageBoundary(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xE, 4).EndCell()
	reused := BeginCell().MustStoreUInt(0xC0DE, 16).MustStoreRef(leaf).EndCell()
	from := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(reused).EndCell()

	tree := NewCellUsageTree()
	usageFrom := from.WithTrace(tree.RootTrace())
	loaded, err := usageFrom.MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	reusedRef := loaded.BaseCell()
	to := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(reusedRef).EndCell()

	update, err := tree.CreateMerkleUpdate(usageFrom, to)
	if err != nil {
		t.Fatal(err)
	}
	prunedReused, err := createPrunedBranchFromCell(reused, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedFrom := BeginCell().MustStoreUInt(0xA1, 8).MustStoreRef(prunedReused).EndCell()
	expectedTo := BeginCell().MustStoreUInt(0xB2, 8).MustStoreRef(prunedReused).EndCell()
	expected, err := CreateMerkleUpdate(expectedFrom, expectedTo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(update.ToBOCWithOptions(BOCSerializeOptions{}), expected.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatal("loaded reuse lost its usage-tree boundary during hash-index lookup")
	}
}

func BenchmarkUsageProofSparseArena(b *testing.B) {
	for _, spareNodes := range []int{0, 1 << 15} {
		b.Run(fmt.Sprintf("spare_nodes_%d", spareNodes), func(b *testing.B) {
			leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
			root := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
			builder := NewMerkleProofBuilder(root)
			if _, err := builder.Root().BeginParse(); err != nil {
				b.Fatal(err)
			}
			for i := 0; i < spareNodes; i++ {
				builder.UsageTree().allocNode(builder.UsageTree().RootNode())
			}

			b.ReportAllocs()
			for b.Loop() {
				if _, err := builder.CreateProof(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkUsageProofDenseArenaMapSizing(b *testing.B) {
	tc := newUsageTreeConcurrentCase(b, 1024, 2026073101)
	builder := NewMerkleProofBuilder(tc.from)

	var walk func(*Cell) error
	walk = func(c *Cell) error {
		loader, err := c.BeginParse()
		if err != nil {
			return err
		}
		for loader.RefsNum() > 0 {
			ref, err := loader.LoadRefCell()
			if err != nil {
				return err
			}
			if err = walk(ref); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(builder.Root()); err != nil {
		b.Fatal(err)
	}

	build := func(mode string) (*Cell, error) {
		state := newUsageProofBuildState(builder.UsageTree())
		if mode == "node_count" {
			hint := builder.UsageTree().NodeCount()
			state.cells = make(map[Hash]*Cell, hint)
			state.built = make(map[usageProofBuildKey]*Cell, hint)
		}
		if err := collectUsageProofHashes(tc.from, builder.UsageTree(), builder.UsageTree().RootNode(), state); err != nil {
			return nil, err
		}
		if mode == "visited" && len(state.cells) > 0 {
			state.built = make(map[usageProofBuildKey]*Cell, len(state.cells))
		}
		body, err := buildUsageProofBody(tc.from, state, tc.from.Level())
		if err != nil {
			return nil, err
		}
		return CreateMerkleProof(body)
	}

	for _, mode := range []string{"lazy", "visited", "node_count"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				proof, err := build(mode)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkCellSink = proof
			}
		})
	}
}
