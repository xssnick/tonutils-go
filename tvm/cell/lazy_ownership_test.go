package cell

import (
	"bytes"
	"runtime"
	"testing"
	"weak"
)

// Keep the source graph and the loader outside the GC assertions' stack frame.
//
//go:noinline
func prewarmOwnershipRoot(t *testing.T, depth int) (*Cell, weak.Pointer[testLazyLoader]) {
	t.Helper()
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	child := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{leaf.HashKey(): leaf}}
	lazyChild := cellWithLazyRefsFromCell(child, loader.LoadCell)
	loader.cells[child.HashKey()] = lazyChild
	root := BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(child).MustStoreRef(child).EndCell()
	lazyRoot := cellWithLazyRefsFromCell(root, loader.LoadCell)

	warm, err := lazyRoot.PrewarmRecursive(depth)
	if err != nil {
		t.Fatal(err)
	}
	if warm.HashKey() != root.HashKey() || warm.Depth() != root.Depth() {
		t.Fatal("prewarm changed the root hash or depth")
	}
	if depth == 0 && !bytes.Equal(warm.ToBOC(), root.ToBOC()) {
		t.Fatal("prewarm changed canonical cell bytes")
	}
	return warm, weak.Make(loader)
}

func TestPrewarmRecursiveReleasesMaterializedLoader(t *testing.T) {
	warm, loader := prewarmOwnershipRoot(t, 0)
	runtime.GC()
	runtime.GC()
	if loader.Value() != nil {
		t.Fatal("materialized payload still pins its former lazy loader")
	}
	child := warm.MustPeekRef(0)
	if child != warm.MustPeekRef(1) {
		t.Fatal("prewarm lost shared subtree identity")
	}
	if child.IsLazy() || child.MustPeekRef(0).IsLazy() {
		t.Fatal("unlimited prewarm left a lazy descendant")
	}
	runtime.KeepAlive(warm)
}

func TestPrewarmRecursiveRetainsBoundaryLoader(t *testing.T) {
	warm, loader := prewarmOwnershipRoot(t, 1)
	runtime.GC()
	if loader.Value() == nil {
		t.Fatal("depth-limited prewarm lost its unresolved boundary loader")
	}
	child := warm.MustPeekRef(0)
	boundary := child.MustPeekRef(0)
	if child.IsLazy() || !boundary.IsLazy() {
		t.Fatal("prewarm changed the depth boundary")
	}
	leaf, err := boundary.Prewarm()
	if err != nil {
		t.Fatal(err)
	}
	if leaf.MustBeginParse().MustLoadUInt(8) != 0xCC {
		t.Fatal("boundary loader returned the wrong payload")
	}
	runtime.KeepAlive(warm)
}
