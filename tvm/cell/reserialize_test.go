package cell

import (
	"bytes"
	"testing"
)

func TestReserializeBOC_MatchesToBOCWithOptions(t *testing.T) {
	blockOpts := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithIntHashes: true,
	}

	check := func(t *testing.T, source []byte, opts BOCSerializeOptions) {
		roots, boc, err := ReserializeBOC(source, opts)
		if err != nil {
			t.Fatalf("ReserializeBOC: %v", err)
		}

		wantRoots, err := FromBOCMultiRoot(source)
		if err != nil {
			t.Fatalf("FromBOCMultiRoot: %v", err)
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
			t.Fatal("reserialized bytes differ from ToBOCWithOptions")
		}
	}

	t.Run("ReferenceFixture_Mode31Source", func(t *testing.T) {
		root, rawMode31 := loadReferenceFixtureRoot(t)
		check(t, rawMode31, blockOpts)
		check(t, rawMode31, mode31Options())
		check(t, root.ToBOC(), blockOpts)
		// mode-2 source, like the C++ compressed candidate payload
		check(t, root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true}), blockOpts)
	})

	t.Run("RichFixture", func(t *testing.T) {
		root, _, wantBOC := buildRichStateAwareFixture(t, 5)
		roots, boc, err := ReserializeBOC(root.ToBOCWithOptions(BOCSerializeOptions{WithCRC32C: true}), mode31Options())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(roots[0].Hash(), root.Hash()) {
			t.Fatal("root hash changed")
		}
		if !bytes.Equal(boc, wantBOC) {
			t.Fatal("reserialized bytes differ from the original mode31 serialization")
		}
	})
}

// A sender is free to ship a BoC with duplicate cells; the graph fast path
// preserves the sender's shape instead of deduplicating, so callers verifying
// an exact file hash must fall back to ToBOCWithOptions on mismatch.
func TestReserializeBOC_KeepsSenderDuplicates(t *testing.T) {
	leafA := BeginCell().MustStoreUInt(0xAAAAAAAA, 32).EndCell()
	leafB := BeginCell().MustStoreUInt(0xBBBBBBBB, 32).EndCell()
	parent := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(leafA).MustStoreRef(leafB).EndCell()

	source := ToBOCWithOptions([]*Cell{parent}, BOCSerializeOptions{})
	// binary-patch leafB's payload to equal leafA's, producing a BoC that
	// carries the same cell twice under different indices
	patched := bytes.Replace(source, []byte{0xBB, 0xBB, 0xBB, 0xBB}, []byte{0xAA, 0xAA, 0xAA, 0xAA}, 1)
	if bytes.Equal(patched, source) {
		t.Fatal("failed to patch duplicate payload")
	}

	roots, fast, err := ReserializeBOC(patched, BOCSerializeOptions{})
	if err != nil {
		t.Fatalf("ReserializeBOC over duplicate-cell boc: %v", err)
	}

	canonical, err := ToBOCWithOptionsErr(roots, BOCSerializeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(fast, canonical) {
		t.Fatal("expected the fast path to keep sender duplicates and differ from the deduplicated serialization")
	}

	// both serializations still describe the same tree
	reparsed, err := FromBOC(fast)
	if err != nil {
		t.Fatalf("fast-path bytes do not parse: %v", err)
	}
	if !bytes.Equal(reparsed.Hash(), roots[0].Hash()) {
		t.Fatal("fast-path bytes parse to a different tree")
	}
}
