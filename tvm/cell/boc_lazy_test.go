package cell

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestFromBOCMultiRootReaderLazyRequiresTrustedHashes(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	boc := root.ToBOCWithOptions(mode31Options())

	_, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{Lazy: true})
	if err == nil || !strings.Contains(err.Error(), "trusted hashes") {
		t.Fatalf("expected trusted hashes error, got %v", err)
	}
}

func TestFromBOCMultiRootReaderLazyIndexesBOCWithoutRootHashes(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	root := BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(child).EndCell()

	for name, options := range map[string]BOCSerializeOptions{
		"indexed":    {WithIndex: true},
		"sequential": {},
	} {
		t.Run(name, func(t *testing.T) {
			boc := root.ToBOCWithOptions(options)

			roots, unique, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{
				Lazy:          true,
				TrustedHashes: true,
			})
			if err != nil {
				t.Fatalf("lazy parse boc without root hashes: %v", err)
			}
			if unique != nil {
				t.Fatalf("lazy parse should not return eager unique cells, got %d", len(unique))
			}
			if len(roots) != 1 {
				t.Fatalf("unexpected roots count: got %d want 1", len(roots))
			}
			if roots[0].HashKey() != root.HashKey() {
				t.Fatalf("unexpected root hash: got=%x want=%x", roots[0].Hash(), root.Hash())
			}

			stored := roots[0].rawRefs()
			if len(stored) != 1 || !stored[0].IsLazy() {
				t.Fatal("expected root ref to be a lazy indexed boundary")
			}

			ref, err := roots[0].PeekRef(0)
			if err != nil {
				t.Fatalf("load lazy indexed ref: %v", err)
			}
			if ref.HashKey() != child.HashKey() {
				t.Fatalf("unexpected child hash: got=%x want=%x", ref.Hash(), child.Hash())
			}
		})
	}
}

func TestFromBOCMultiRootReaderLazyMode31MultiRootCache(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	root := BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	boc := ToBOCWithOptions([]*Cell{root, shared}, mode31Options())
	roots, unique, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unique != nil {
		t.Fatalf("lazy parse should not return eager unique cells, got %d", len(unique))
	}
	if len(roots) != 2 {
		t.Fatalf("unexpected roots count: got %d want 2", len(roots))
	}
	if roots[0].IsLazy() || roots[1].IsLazy() {
		t.Fatal("roots should be materialized data cells")
	}

	stored := roots[0].rawRefs()
	if len(stored) != 2 || !stored[0].IsLazy() || !stored[1].IsLazy() {
		t.Fatal("expected root refs to be lazy pruned boundaries")
	}

	first, err := roots[0].PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := roots[0].PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("shared refs should resolve to the same cached cell")
	}
	if first != roots[1] {
		t.Fatal("shared ref should resolve to the materialized second root")
	}
	if first.HashKey() != shared.HashKey() {
		t.Fatalf("unexpected shared hash: got=%x want=%x", first.Hash(), shared.Hash())
	}
}

func TestFromBOCMultiRootReaderLazyMode31ReferenceBlockFixture(t *testing.T) {
	wantRoot, mode31BOC := loadReferenceFixtureRoot(t)

	roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(mode31BOC), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count: got %d want 1", len(roots))
	}
	if roots[0].HashKey() != wantRoot.HashKey() {
		t.Fatalf("unexpected root hash: got=%x want=%x", roots[0].Hash(), wantRoot.Hash())
	}
	if !hasLazyBOCPrunedRefs(roots[0], map[Hash]struct{}{}) {
		t.Fatal("expected lazy parse to keep hash-bearing subtrees lazy")
	}
	if got := roots[0].ToBOCWithOptions(mode31Options()); !bytes.Equal(got, mode31BOC) {
		t.Fatal("lazy block fixture reserialization changed mode31 boc")
	}
}

func TestFromBOCMultiRootReaderLazyMode31BlockchainConfigFixture(t *testing.T) {
	raw, err := os.ReadFile("../../tlb/testdata/blockchain_config_mainnet.boc")
	if err != nil {
		t.Fatal(err)
	}

	eager, err := FromBOC(raw)
	if err != nil {
		t.Fatal(err)
	}
	mode31BOC := eager.ToBOCWithOptions(mode31Options())

	roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(mode31BOC), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count: got %d want 1", len(roots))
	}
	if roots[0].HashKey() != eager.HashKey() {
		t.Fatalf("unexpected root hash: got=%x want=%x", roots[0].Hash(), eager.Hash())
	}
	if err = materializeTestCellTree(roots[0]); err != nil {
		t.Fatal(err)
	}
}

func TestFromBOCMultiRootReaderLazyConcurrentAccess(t *testing.T) {
	wantRoot, mode31BOC := loadReferenceFixtureRoot(t)
	roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(mode31BOC), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 8; j++ {
				if roots[0].HashKey() != wantRoot.HashKey() {
					errs <- errors.New("unexpected root hash")
					return
				}
				if err := materializeTestCellTree(roots[0]); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFromBOCMultiRootReaderLazyNoIndexWithTopHash(t *testing.T) {
	child := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	root := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(child).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithTopHash:   true,
		WithIntHashes: true,
	})

	roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count: got %d want 1", len(roots))
	}
	if roots[0].HashKey() != root.HashKey() {
		t.Fatalf("unexpected root hash: got=%x want=%x", roots[0].Hash(), root.Hash())
	}
}

func TestFromBOCMultiRootReaderLazyIndexesCPPBocHandsWithoutHashes(t *testing.T) {
	fixtures := [][]byte{
		[]byte("h\377e\363\001\001\002\001\000*\004*\201\001P\001\210H\001\004\024\271\313\264\253\277\265\350dN\250{,\372\021\012:I\354\322|\255\245\330\204+&\345\214\026\300\064\000\001"),
		[]byte("\254\303\247(\001\001\002\001\000*\004*\201\001P\001\210H\001\004\024\271\313\264\253\277\265\350dN\250{,\372\021\012:I\354\322|\255\245\330\204+&\345\214\026\300\064\000\001\032\231\063\274"),
	}

	for _, fixture := range fixtures {
		eager, err := FromBOC(fixture)
		if err != nil {
			t.Fatalf("C++ BocHands fixture should parse eagerly: %v", err)
		}

		roots, _, err := FromBOCMultiRootReader(NewBOCNoCopyReader(fixture), BOCParseOptions{
			Lazy:          true,
			TrustedHashes: true,
		})
		if err != nil {
			t.Fatalf("lazy parse C++ BocHands fixture: %v", err)
		}
		if len(roots) != 1 {
			t.Fatalf("unexpected roots count: got %d want 1", len(roots))
		}
		if roots[0].HashKey() != eager.HashKey() {
			t.Fatalf("unexpected root hash: got=%x want=%x", roots[0].Hash(), eager.Hash())
		}
	}
}

func materializeTestCellTree(root *Cell) error {
	visited := map[Hash]struct{}{}

	var walk func(c *Cell) error
	walk = func(c *Cell) error {
		if c == nil {
			return nil
		}
		key := c.HashKey()
		if _, ok := visited[key]; ok {
			return nil
		}
		visited[key] = struct{}{}

		for i := 0; i < int(c.RefsNum()); i++ {
			ref, err := c.PeekRef(i)
			if err != nil {
				return err
			}
			if err = walk(ref); err != nil {
				return err
			}
		}
		return nil
	}

	return walk(root)
}

func hasLazyBOCPrunedRefs(root *Cell, visited map[Hash]struct{}) bool {
	if root == nil {
		return false
	}
	hash := root.HashKey()
	if _, ok := visited[hash]; ok {
		return false
	}
	visited[hash] = struct{}{}

	for _, ref := range root.rawRefs() {
		if ref.IsLazy() || hasLazyBOCPrunedRefs(ref, visited) {
			return true
		}
	}
	return false
}
