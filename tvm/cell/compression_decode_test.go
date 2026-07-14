package cell

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildRichStateAwareFixture builds a block-shaped root with a real
// MerkleUpdate over a fanout-4 state tree. Changed leaves include both
// scattered singles (pruned-heavy left side) and one fully-changed subtree
// (exercises whole-subtree state reuse where no pruned boundary remains).
func buildRichStateAwareFixture(tb testing.TB, depth int) (root *Cell, oldState *Cell, mode31BOC []byte) {
	tb.Helper()

	var build func(level int, path uint64) *Cell
	build = func(level int, path uint64) *Cell {
		b := BeginCell()
		if level == 0 {
			var data [104]byte
			binary.BigEndian.PutUint64(data[:], path*0x9E3779B97F4A7C15)
			for i := 8; i+8 <= len(data); i += 8 {
				binary.BigEndian.PutUint64(data[i:], (path+uint64(i))*0xBF58476D1CE4E5B9)
			}
			if err := b.StoreSlice(data[:100], 100*8); err != nil {
				tb.Fatal(err)
			}
			return b.EndCell()
		}
		if err := b.StoreUInt(path, 64); err != nil {
			tb.Fatal(err)
		}
		for i := uint64(0); i < 4; i++ {
			b.MustStoreRef(build(level-1, path*4+i))
		}
		return b.EndCell()
	}
	oldState = build(depth, 1)

	changedLeaf := func(path uint64) bool {
		// scattered singles plus one dense stretch (fully-changed subtree)
		return path%37 == 0 || (path%64 >= 16 && path%64 < 32 && path%512 < 64)
	}
	newLeaf := func(path uint64) *Cell {
		nb := BeginCell()
		var data [100]byte
		binary.BigEndian.PutUint64(data[:], path^0xDEADBEEF)
		if err := nb.StoreSlice(data[:], uint(len(data))*8); err != nil {
			tb.Fatal(err)
		}
		return nb.EndCell()
	}

	var rebuild func(c *Cell, level int, path uint64) *Cell
	rebuild = func(c *Cell, level int, path uint64) *Cell {
		if level == 0 {
			if changedLeaf(path) {
				return newLeaf(path)
			}
			return c
		}
		var refs [4]*Cell
		changed := false
		for i := 0; i < 4; i++ {
			ref, err := c.PeekRef(i)
			if err != nil {
				tb.Fatal(err)
			}
			refs[i] = rebuild(ref, level-1, path*4+uint64(i))
			if refs[i] != ref {
				changed = true
			}
		}
		if !changed {
			return c
		}
		nb := BeginCell()
		if err := nb.StoreUInt(path, 64); err != nil {
			tb.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			nb.MustStoreRef(refs[i])
		}
		return nb.EndCell()
	}
	newState := rebuild(oldState, depth, 1)
	if newState == oldState {
		tb.Fatal("fixture produced no state changes")
	}

	prune := func(c *Cell) *Cell {
		pb, err := CreatePrunedBranch(c, 1, 0)
		if err != nil {
			tb.Fatalf("pruned branch: %v", err)
		}
		return pb
	}

	var buildSide func(oc, nc *Cell, level int, path uint64, right bool) *Cell
	buildSide = func(oc, nc *Cell, level int, path uint64, right bool) *Cell {
		if oc == nc {
			if oc.RefsNum() == 0 {
				return oc
			}
			return prune(oc)
		}
		if level == 0 {
			if right {
				return nc
			}
			return oc
		}
		nb := BeginCell()
		if err := nb.StoreUInt(path, 64); err != nil {
			tb.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			or, err := oc.PeekRef(i)
			if err != nil {
				tb.Fatal(err)
			}
			nr, err := nc.PeekRef(i)
			if err != nil {
				tb.Fatal(err)
			}
			nb.MustStoreRef(buildSide(or, nr, level-1, path*4+uint64(i), right))
		}
		return nb.EndCell()
	}

	mu, err := CreateMerkleUpdate(
		buildSide(oldState, newState, depth, 1, false),
		buildSide(oldState, newState, depth, 1, true),
	)
	if err != nil {
		tb.Fatalf("create MU: %v", err)
	}

	info := BeginCell().MustStoreUInt(0x9bc7a987, 32).EndCell()
	vf := BeginCell().MustStoreUInt(2, 8).EndCell()
	extra := build(3, 7)
	root = BeginCell().
		MustStoreUInt(0x11ef55aa, 32).
		MustStoreRef(info).
		MustStoreRef(vf).
		MustStoreRef(mu).
		MustStoreRef(extra).
		EndCell()

	return root, oldState, root.ToBOCWithOptions(mode31Options())
}

func TestCompressBOC_ImprovedStructureLZ4WithState_RichRoundTrip(t *testing.T) {
	root, oldState, wantBOC := buildRichStateAwareFixture(t, 6)

	compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4WithState, oldState)
	if err != nil {
		t.Fatalf("failed to compress boc with state: %v", err)
	}

	roots, err := DecompressBOC(compressed, len(wantBOC)+4096, oldState)
	if err != nil {
		t.Fatalf("failed to decompress boc with state: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count, got %d want 1", len(roots))
	}
	if !bytes.Equal(roots[0].Hash(), root.Hash()) {
		t.Fatal("state-aware decompression changed the root hash")
	}
	if got := roots[0].ToBOCWithOptions(mode31Options()); !bytes.Equal(got, wantBOC) {
		t.Fatal("state-aware compression roundtrip changed the boc")
	}
}

func TestCompressBOC_ImprovedStructureLZ4WithState_LazyStateRoot(t *testing.T) {
	root, oldState, wantBOC := buildRichStateAwareFixture(t, 6)

	compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4WithState, oldState)
	if err != nil {
		t.Fatalf("failed to compress boc with state: %v", err)
	}

	// reparse the state lazily so decompression sees lazy stubs, like a
	// celldb-backed state root
	stateBOC := oldState.ToBOCWithOptions(mode31Options())
	lazyRoots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(stateBOC), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatalf("failed to parse state lazily: %v", err)
	}
	if len(lazyRoots) != 1 {
		t.Fatalf("unexpected lazy state roots count: %d", len(lazyRoots))
	}
	lazyChildren := 0
	for i := 0; i < int(lazyRoots[0].RefsNum()); i++ {
		ref, err := lazyRoots[0].PeekRef(i)
		if err != nil {
			t.Fatal(err)
		}
		if ref.IsLazy() {
			lazyChildren++
		}
	}
	if lazyChildren == 0 {
		t.Fatal("lazy state root has no lazy children, fixture does not exercise lazy loading")
	}

	roots, err := DecompressBOC(compressed, len(wantBOC)+4096, lazyRoots[0])
	if err != nil {
		t.Fatalf("failed to decompress boc with lazy state root: %v", err)
	}
	if !bytes.Equal(roots[0].Hash(), root.Hash()) {
		t.Fatal("lazy-state decompression changed the root hash")
	}
	if got := roots[0].ToBOCWithOptions(mode31Options()); !bytes.Equal(got, wantBOC) {
		t.Fatal("lazy-state decompression roundtrip changed the boc")
	}
}

func TestDecompressBOCSerialized_MatchesToBOCWithOptions(t *testing.T) {
	blockOpts := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithIntHashes: true,
	}

	check := func(t *testing.T, compressed []byte, state *Cell, maxSize int, opts BOCSerializeOptions) {
		roots, boc, err := DecompressBOCSerialized(compressed, maxSize, state, opts)
		if err != nil {
			t.Fatalf("DecompressBOCSerialized: %v", err)
		}

		wantRoots, err := DecompressBOC(compressed, maxSize, state)
		if err != nil {
			t.Fatalf("DecompressBOC: %v", err)
		}
		if len(roots) != len(wantRoots) {
			t.Fatalf("roots count mismatch: %d != %d", len(roots), len(wantRoots))
		}
		for i := range roots {
			if !bytes.Equal(roots[i].Hash(), wantRoots[i].Hash()) {
				t.Fatalf("root %d hash mismatch", i)
			}
		}

		want, err := ToBOCWithOptionsErr(wantRoots, opts)
		if err != nil {
			t.Fatalf("ToBOCWithOptionsErr: %v", err)
		}
		if !bytes.Equal(boc, want) {
			t.Fatal("serialized graph bytes differ from ToBOCWithOptions")
		}
	}

	t.Run("ImprovedNoState_ReferenceFixture", func(t *testing.T) {
		root, rawBOC := loadReferenceFixtureRoot(t)
		compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4, nil)
		if err != nil {
			t.Fatal(err)
		}
		check(t, compressed, nil, len(rawBOC)+4096, blockOpts)
		check(t, compressed, nil, len(rawBOC)+4096, mode31Options())
	})

	t.Run("WithState_RichFixture", func(t *testing.T) {
		root, oldState, wantBOC := buildRichStateAwareFixture(t, 6)
		compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4WithState, oldState)
		if err != nil {
			t.Fatal(err)
		}
		check(t, compressed, oldState, len(wantBOC)+4096, blockOpts)
		check(t, compressed, oldState, len(wantBOC)+4096, mode31Options())

		// exact-bytes roundtrip against the original serialization
		_, boc, err := DecompressBOCSerialized(compressed, len(wantBOC)+4096, oldState, mode31Options())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(boc, wantBOC) {
			t.Fatal("serialized graph bytes differ from the original mode31 boc")
		}
	})

	t.Run("Baseline_ReferenceFixture", func(t *testing.T) {
		root, rawBOC := loadReferenceFixtureRoot(t)
		compressed, err := CompressBOC([]*Cell{root}, CompressionBaselineLZ4, nil)
		if err != nil {
			t.Fatal(err)
		}
		check(t, compressed, nil, len(rawBOC)+4096, blockOpts)
	})
}
