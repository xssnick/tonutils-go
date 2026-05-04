package cell

import (
	"errors"
	"math/big"
	"testing"
)

type testLazyLoader struct {
	cells    map[Hash]*Cell
	lastHash Hash
	calls    int
}

func (l *testLazyLoader) LoadCell(hash Hash) (*Cell, error) {
	l.calls++
	l.lastHash = hash
	return l.cells[hash], nil
}

func optionalLazyLoader(loaders []LazyCellLoader) LazyCellLoader {
	if len(loaders) == 0 {
		return nil
	}
	return loaders[0]
}

func mustCreateWithLazyRefsUnsafe(tb testing.TB, descriptors uint16, data, hashes []byte, depths []uint16, refs []LazyRef, loaders ...LazyCellLoader) *Cell {
	tb.Helper()

	c, err := CreateWithLazyRefsUnsafe(descriptors, data, hashes, depths, refs, optionalLazyLoader(loaders))
	if err != nil {
		tb.Fatal(err)
	}
	return c
}

func mustCreateLazyPrunedRef(tb testing.TB, ref LazyRef, loaders ...LazyCellLoader) *Cell {
	tb.Helper()

	c, err := createLazyPrunedRef(ref, optionalLazyLoader(loaders))
	if err != nil {
		tb.Fatal(err)
	}
	return c
}

func TestCreateWithLazyRefsUnsafeCreatesRegularCellWithLazyRef(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}

	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	data := serializedCellData(src)
	cell := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		data,
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	if cell.IsLazy() {
		t.Fatal("parent cell should not be lazy")
	}
	stored := cell.ref(0)
	if !stored.IsLazy() || stored.GetType() != PrunedCellType {
		t.Fatal("expected lazy pruned ref")
	}
	if cell.HashKey() != src.HashKey() {
		t.Fatalf("unexpected hash: got=%x want=%x", cell.Hash(), src.Hash())
	}
	if cell.Depth() != src.Depth() {
		t.Fatalf("unexpected depth: got=%d want=%d", cell.Depth(), src.Depth())
	}
	if cell.BitsSize() != src.BitsSize() {
		t.Fatalf("unexpected bits size: got=%d want=%d", cell.BitsSize(), src.BitsSize())
	}
	if len(data) > 0 && &cell.data[0] != &data[0] {
		t.Fatal("cell data was copied")
	}

	loaded, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected loaded ref")
	}
	if loader.lastHash != ref.HashKey(0) {
		t.Fatalf("unexpected load hash: got=%x want=%x", loader.lastHash, ref.HashKey(0))
	}
	loaded, err = cell.BeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected loaded ref from slice")
	}
}

func TestCreateWithLazyRefsUnsafeReturnsErrors(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreRef(ref).EndCell()
	dsc1, dsc2 := src.descriptors(src.getLevelMask())

	if _, err := CreateWithLazyRefsUnsafe(uint16(dsc1)<<8|uint16(dsc2), nil, significantHashes(src), significantDepths(src), nil, nil); err == nil {
		t.Fatal("expected refs count error")
	}

	withData := BeginCell().MustStoreUInt(0b101, 3).EndCell()
	dsc1, dsc2 = withData.descriptors(withData.getLevelMask())
	if _, err := CreateWithLazyRefsUnsafe(uint16(dsc1)<<8|uint16(dsc2), nil, significantHashes(withData), significantDepths(withData), nil, nil); err == nil {
		t.Fatal("expected data size error")
	}

	if _, err := CreateWithLazyRefsUnsafe(1, []byte{0}, nil, nil, nil, nil); err == nil {
		t.Fatal("expected invalid payload error")
	}
}

func TestSliceToCellPreservesLazyRef(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(src),
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	cell, err := cellWithLazyRef.BeginParse().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cell.IsLazy() {
		t.Fatal("cell with lazy refs should not inherit lazy marker")
	}
	if cell.HashKey() != src.HashKey() {
		t.Fatalf("unexpected hash: got=%x want=%x", cell.Hash(), src.Hash())
	}

	loaded, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected ref in cell with lazy ref")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
	if loader.lastHash != ref.HashKey(0) {
		t.Fatalf("unexpected load hash: got=%x want=%x", loader.lastHash, ref.HashKey(0))
	}
}

func TestSliceToCellPreservesLazyRefForPartialSlice(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(src),
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	slice := cellWithLazyRef.BeginParse()
	if err := slice.Advance(1); err != nil {
		t.Fatal(err)
	}

	cell, err := slice.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cell.IsLazy() {
		t.Fatal("partial slice with lazy refs should not inherit lazy marker")
	}

	loaded, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected ref in partial cell")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
}

func TestCellToBuilderPreservesLazyRef(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(src),
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	cell := cellWithLazyRef.ToBuilder().EndCell()
	if cell.IsLazy() {
		t.Fatal("builder cell with lazy refs should not inherit lazy marker")
	}

	loaded, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected ref in builder cell")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
}

func TestCellToBuilderMaterializesLazyPrunedCell(t *testing.T) {
	src := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	lazyPruned := mustCreateLazyPrunedRef(t, lazyRefFromCell(src))

	cell := lazyPruned.ToBuilder().EndCell()
	if cell.GetType() != OrdinaryCellType {
		t.Fatalf("expected ordinary cell, got %v", cell.GetType())
	}
	if cell.IsLazy() {
		t.Fatal("builder result should not preserve lazy marker from source cell")
	}
	if cell.HashKey() == lazyPruned.HashKey() {
		t.Fatal("builder result should be rehashed as materialized ordinary cell")
	}
}

func TestSliceToCellMaterializesLazyPrunedCell(t *testing.T) {
	src := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	lazyPruned := mustCreateLazyPrunedRef(t, lazyRefFromCell(src))

	cell, err := lazyPruned.BeginParse().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cell.GetType() != PrunedCellType {
		t.Fatalf("expected pruned cell, got %v", cell.GetType())
	}
	if cell.IsLazy() {
		t.Fatal("slice cell conversion should not preserve lazy marker from source cell")
	}
	if cell.HashKey() == lazyPruned.HashKey() {
		t.Fatal("slice cell conversion should be rehashed as materialized pruned cell")
	}
}

func TestStoreBuilderDoesNotPropagateLazyPrunedCellMarker(t *testing.T) {
	src := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	lazyPruned := mustCreateLazyPrunedRef(t, lazyRefFromCell(src))

	cell := BeginCell().MustStoreBuilder(lazyPruned.ToBuilder()).EndCell()
	if cell.GetType() != OrdinaryCellType {
		t.Fatalf("expected ordinary cell, got %v", cell.GetType())
	}
	if cell.IsLazy() {
		t.Fatal("inline builder store should not propagate lazy marker")
	}
}

func TestStoreBuilderPreservesLazyRef(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(src),
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	cell := BeginCell().MustStoreUInt(1, 1).MustStoreBuilder(cellWithLazyRef.ToBuilder()).EndCell()
	if cell.IsLazy() {
		t.Fatal("builder merge with lazy refs should not inherit lazy marker")
	}

	loaded, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected ref in merged builder cell")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
}

func TestCreateWithLazyRefsUnsafeLoadsMultiLevelRefByRepresentedHash(t *testing.T) {
	base := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	ref, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	src := BeginCell().MustStoreRef(ref).EndCell()

	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(ref.Level()): ref}}
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		nil,
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	loaded, err := cellWithLazyRef.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != ref {
		t.Fatal("unexpected loaded ref")
	}
	if loader.lastHash != ref.HashKey(ref.Level()) {
		t.Fatalf("unexpected load hash: got=%x want=%x", loader.lastHash, ref.HashKey(ref.Level()))
	}
}

func TestCreateWithLazyRefsUnsafeWithoutLoader(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreRef(ref).EndCell()
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		nil,
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
	)

	if _, err := cellWithLazyRef.PeekRef(0); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := cellWithLazyRef.BeginParse().Depth(); got != src.BeginParse().Depth() {
		t.Fatalf("unexpected lazy slice depth: got=%d want=%d", got, src.BeginParse().Depth())
	}
}

func TestCreateProofLoadsLazyRef(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	dsc1, dsc2 := src.descriptors(src.getLevelMask())
	cellWithLazyRef := mustCreateWithLazyRefsUnsafe(t,
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(src),
		significantHashes(src),
		significantDepths(src),
		[]LazyRef{lazyRefFromCell(ref)},
		loader.LoadCell,
	)

	if _, err := cellWithLazyRef.CreateProof(CreateProofSkeleton()); err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
}

func TestDictionaryTreatsLazyPrunedRootAsPruned(t *testing.T) {
	dict := NewDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x11, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x22, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	root := dict.AsCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{root.HashKey(): root}}
	lazyRoot := mustCreateLazyPrunedRef(t, lazyRefFromCell(root), loader.LoadCell)
	if !lazyRoot.IsSpecial() {
		t.Fatal("expected lazy pruned root")
	}
	if lazyRoot.HashKey() != root.HashKey() {
		t.Fatalf("unexpected lazy root hash: got=%x want=%x", lazyRoot.Hash(), root.Hash())
	}

	if _, err := lazyRoot.AsDict(8).LoadAll(); err == nil {
		t.Fatal("LoadAll should fail on pruned root without skip")
	}

	items, err := lazyRoot.AsDict(8).LoadAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("pruned root should be skipped, got %d items", len(items))
	}

	if _, err = lazyRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x11)); err == nil {
		t.Fatal("LoadValue should fail on pruned root")
	}

	if loader.calls != 0 {
		t.Fatalf("lazy pruned root should not be loaded, calls=%d", loader.calls)
	}
}

func TestDictionaryLoadAllSkipPrunedLazyChildDoesNotLoad(t *testing.T) {
	dict := NewDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x00, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x80, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	root := dict.AsCell()
	left := root.ref(0)
	lazyLeft := mustCreateLazyPrunedRef(t, lazyRefFromCell(left))
	rootWithLazyChild, err := root.cloneWithRefObserved(0, lazyLeft, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := rootWithLazyChild.AsDict(8).LoadAll(); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("expected lazy loader error without skip, got %v", err)
	}

	items, err := rootWithLazyChild.AsDict(8).LoadAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only non-pruned side, got %d items", len(items))
	}
}

func TestToBOCLoadsLazyRefs(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	cellWithLazyRef := cellWithLazyRefsFromCell(src, loader.LoadCell)

	boc := cellWithLazyRef.ToBOC()
	if boc == nil {
		t.Fatal("expected cell with lazy refs to serialize")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}

	decoded, err := FromBOC(boc)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.HashKey() != src.HashKey() {
		t.Fatalf("unexpected decoded hash: got=%x want=%x", decoded.Hash(), src.Hash())
	}

	decodedRef, err := decoded.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if decodedRef.IsLazy() {
		t.Fatal("serialized boc kept lazy ref")
	}
	if decodedRef.GetType() != OrdinaryCellType {
		t.Fatalf("expected ordinary decoded ref, got %v", decodedRef.GetType())
	}
	if decodedRef.HashKey() != ref.HashKey() {
		t.Fatalf("unexpected decoded ref hash: got=%x want=%x", decodedRef.Hash(), ref.Hash())
	}
}

func TestToBOCWithOptionsErrReturnsLazyError(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	cellWithLazyRef := cellWithLazyRefsFromCell(src)

	if _, err := cellWithLazyRef.ToBOCWithOptionsErr(BOCSerializeOptions{}); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("expected lazy loader error, got %v", err)
	}
	if boc := cellWithLazyRef.ToBOCWithOptions(BOCSerializeOptions{}); boc != nil {
		t.Fatal("legacy ToBOCWithOptions wrapper should return nil on lazy load error")
	}
}

func TestToBOCLoadsLazyPrunedRoot(t *testing.T) {
	child := BeginCell().MustStoreUInt(0x55, 8).EndCell()
	src := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(child).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{src.HashKey(): src}}
	lazyRoot := mustCreateLazyPrunedRef(t, lazyRefFromCell(src), loader.LoadCell)

	boc := lazyRoot.ToBOC()
	if boc == nil {
		t.Fatal("expected lazy pruned root to serialize")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}

	decoded, err := FromBOC(boc)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.IsLazy() {
		t.Fatal("serialized boc kept lazy root")
	}
	if decoded.GetType() != OrdinaryCellType {
		t.Fatalf("expected ordinary decoded root, got %v", decoded.GetType())
	}
	if decoded.HashKey() != src.HashKey() {
		t.Fatalf("unexpected decoded hash: got=%x want=%x", decoded.Hash(), src.Hash())
	}

	decodedRef, err := decoded.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if decodedRef.HashKey() != child.HashKey() {
		t.Fatalf("unexpected decoded child hash: got=%x want=%x", decodedRef.Hash(), child.Hash())
	}
}

func TestCellRefViewModes(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	cellWithLazyRef := cellWithLazyRefsFromCell(src)

	view := newCellRefView(cellWithLazyRef)
	stored, err := view.refAt(0, refStored)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.IsLazy() || stored.GetType() != PrunedCellType {
		t.Fatal("stored mode should return lazy pruned boundary")
	}

	boundary, err := view.refAt(0, refBoundary)
	if err != nil {
		t.Fatal(err)
	}
	if boundary != stored {
		t.Fatal("boundary mode should preserve stored lazy boundary")
	}

	if _, err = view.refAt(0, refResolved); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("unexpected resolved error: %v", err)
	}

	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(): ref}}
	view = newCellRefView(cellWithLazyRefsFromCell(src, loader.LoadCell))
	resolved, err := view.refAt(0, refResolved)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != ref {
		t.Fatal("resolved mode should load full ref")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
}

func serializedCellData(c *Cell) []byte {
	data := make([]byte, c.SerializedBOCBodySize())
	c.SerializeBOCBodyTo(data)
	return data
}

func significantHashes(c *Cell) []byte {
	levelMask := c.getLevelMask()
	hashes := make([]byte, 0, levelMask.getHashesCount()*hashSize)
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if levelMask.IsSignificant(level) {
			hashes = append(hashes, c.getHash(level)...)
		}
	}
	return hashes
}

func significantDepths(c *Cell) []uint16 {
	levelMask := c.getLevelMask()
	depths := make([]uint16, 0, levelMask.getHashesCount())
	for level := 0; level <= levelMask.GetLevel(); level++ {
		if levelMask.IsSignificant(level) {
			depths = append(depths, c.getDepth(level))
		}
	}
	return depths
}

func lazyRefFromCell(c *Cell) LazyRef {
	return LazyRef{
		LevelMask: c.getLevelMask(),
		Hashes:    significantHashes(c),
		Depths:    significantDepths(c),
	}
}

func cellWithLazyRefsFromCell(c *Cell, loaders ...LazyCellLoader) *Cell {
	dsc1, dsc2 := c.descriptors(c.getLevelMask())
	refs := make([]LazyRef, c.refsCount())
	for i, ref := range c.rawRefs() {
		refs[i] = lazyRefFromCell(ref)
	}

	cellWithLazyRef, err := CreateWithLazyRefsUnsafe(
		uint16(dsc1)<<8|uint16(dsc2),
		serializedCellData(c),
		significantHashes(c),
		significantDepths(c),
		refs,
		optionalLazyLoader(loaders),
	)
	if err != nil {
		panic(err)
	}
	return cellWithLazyRef
}
