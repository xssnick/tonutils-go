package cell

import (
	"bytes"
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

	boundary, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() || boundary.GetType() != PrunedCellType {
		t.Fatal("expected lazy boundary ref")
	}
	if loader.calls != 0 {
		t.Fatalf("peek ref should not load, calls=%d", loader.calls)
	}

	loaded, err := cell.MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MustLoadUInt(8) != 0xA5 {
		t.Fatal("unexpected loaded ref")
	}
	if loader.lastHash != ref.HashKey(0) {
		t.Fatalf("unexpected load hash: got=%x want=%x", loader.lastHash, ref.HashKey(0))
	}
	loadedRef, err := cell.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if !loadedRef.IsLazy() {
		t.Fatal("LoadRefCell should return boundary ref without parsing it")
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

	cell, err := cellWithLazyRef.MustBeginParse().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cell.IsLazy() {
		t.Fatal("cell with lazy refs should not inherit lazy marker")
	}
	if cell.HashKey() != src.HashKey() {
		t.Fatalf("unexpected hash: got=%x want=%x", cell.Hash(), src.Hash())
	}

	boundary, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() {
		t.Fatal("expected lazy ref in cell copy")
	}
	if loader.calls != 0 {
		t.Fatalf("slice copy should not load refs, calls=%d", loader.calls)
	}
	loaded, err := boundary.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MustLoadUInt(8) != 0xA5 {
		t.Fatal("unexpected loaded ref")
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

	slice := cellWithLazyRef.MustBeginParse()
	if err := slice.SkipBits(1); err != nil {
		t.Fatal(err)
	}

	cell, err := slice.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cell.IsLazy() {
		t.Fatal("partial slice with lazy refs should not inherit lazy marker")
	}

	boundary, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() {
		t.Fatal("expected lazy ref in partial cell")
	}
	if loader.calls != 0 {
		t.Fatalf("partial slice copy should not load refs, calls=%d", loader.calls)
	}
	loaded, err := boundary.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MustLoadUInt(8) != 0xA5 {
		t.Fatal("unexpected loaded ref")
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

	boundary, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() {
		t.Fatal("expected lazy ref in builder cell")
	}
	if loader.calls != 0 {
		t.Fatalf("builder copy should not load refs, calls=%d", loader.calls)
	}
	loaded, err := boundary.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MustLoadUInt(8) != 0xA5 {
		t.Fatal("unexpected loaded ref")
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
	loader := &testLazyLoader{cells: map[Hash]*Cell{src.HashKey(): src}}
	lazyPruned := mustCreateLazyPrunedRef(t, lazyRefFromCell(src), loader.LoadCell)

	cell, err := lazyPruned.MustBeginParse().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if cell.GetType() != OrdinaryCellType {
		t.Fatalf("expected ordinary cell, got %v", cell.GetType())
	}
	if cell.IsLazy() {
		t.Fatal("slice cell conversion should not preserve lazy marker from source cell")
	}
	if cell.HashKey() != src.HashKey() {
		t.Fatal("slice cell conversion should materialize original cell")
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

	boundary, err := cell.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() {
		t.Fatal("expected lazy ref in merged builder cell")
	}
	if loader.calls != 0 {
		t.Fatalf("builder merge should not load refs, calls=%d", loader.calls)
	}
	loaded, err := boundary.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MustLoadUInt(8) != 0xA5 {
		t.Fatal("unexpected loaded ref")
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

	boundary, err := cellWithLazyRef.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() {
		t.Fatal("expected lazy boundary ref")
	}
	loaded, err := boundary.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BitsLeft() != ref.BitsSize() {
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

	boundary, err := cellWithLazyRef.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = boundary.BeginParse(); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := cellWithLazyRef.MustBeginParse().Depth(); got != src.MustBeginParse().Depth() {
		t.Fatalf("unexpected lazy slice depth: got=%d want=%d", got, src.MustBeginParse().Depth())
	}
}

func TestCreateProofLoadsOnlyRequestedLazyRef(t *testing.T) {
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
	if loader.calls != 0 {
		t.Fatalf("proof with pruned ref should not load, calls=%d", loader.calls)
	}

	proof, err := cellWithLazyRef.CreateProof(CreateProofSkeleton())
	if err != nil {
		t.Fatal(err)
	}
	boc := proof.ToBOCWithFlags(false)
	if boc == nil {
		t.Fatal("expected proof boc")
	}
	if loader.calls != 0 {
		t.Fatalf("serializing proof with pruned ref should not load, calls=%d", loader.calls)
	}

	decoded, err := FromBOC(boc)
	if err != nil {
		t.Fatal(err)
	}
	body, err := UnwrapProof(decoded, src.Hash())
	if err != nil {
		t.Fatal(err)
	}
	pruned, err := body.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if pruned.IsLazy() {
		t.Fatal("serialized proof kept lazy pruned ref")
	}
	if pruned.GetType() != PrunedCellType {
		t.Fatalf("serialized proof expanded pruned ref, got %v", pruned.GetType())
	}

	sk := CreateProofSkeleton()
	sk.ProofRef(0)
	if _, err := cellWithLazyRef.CreateProof(sk); err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
}

func TestDictionaryLoadsLazyPrunedRootUnlessSkipPruned(t *testing.T) {
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

	items, err := lazyRoot.AsDict(8).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected loaded items count: got %d want 2", len(items))
	}

	value, err := lazyRoot.AsDict(8).LoadValueByIntKey(big.NewInt(0x11))
	if err != nil {
		t.Fatal(err)
	}
	if value.MustLoadUInt(8) != 0xAA {
		t.Fatal("unexpected lazy root dict value")
	}

	if loader.calls == 0 {
		t.Fatal("lazy pruned root should be loaded for ordinary dictionary reads")
	}

	skipLoader := &testLazyLoader{cells: map[Hash]*Cell{root.HashKey(): root}}
	skipRoot := mustCreateLazyPrunedRef(t, lazyRefFromCell(root), skipLoader.LoadCell)
	items, err = skipRoot.AsDict(8).LoadAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("pruned root should be skipped, got %d items", len(items))
	}
	if skipLoader.calls != 0 {
		t.Fatalf("skip pruned should not load lazy root, calls=%d", skipLoader.calls)
	}
}

func TestDictionaryMutatesLazyPrunedRootAfterMaterialization(t *testing.T) {
	dict := NewDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x11, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x22, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	root := dict.AsCell()
	setLoader := &testLazyLoader{cells: map[Hash]*Cell{root.HashKey(): root}}
	setDict := mustCreateLazyPrunedRef(t, lazyRefFromCell(root), setLoader.LoadCell).AsDict(8)
	if err := setDict.Set(BeginCell().MustStoreUInt(0x33, 8).EndCell(), BeginCell().MustStoreUInt(0xCC, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	value, err := setDict.LoadValueByIntKey(big.NewInt(0x33))
	if err != nil {
		t.Fatal(err)
	}
	if value.MustLoadUInt(8) != 0xCC {
		t.Fatal("unexpected value after lazy root set")
	}

	deleteLoader := &testLazyLoader{cells: map[Hash]*Cell{root.HashKey(): root}}
	deleteDict := mustCreateLazyPrunedRef(t, lazyRefFromCell(root), deleteLoader.LoadCell).AsDict(8)
	if err := deleteDict.Delete(BeginCell().MustStoreUInt(0x11, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if _, err = deleteDict.LoadValueByIntKey(big.NewInt(0x11)); !errors.Is(err, ErrNoSuchKeyInDict) {
		t.Fatalf("deleted lazy root key should be absent, got %v", err)
	}
	value, err = deleteDict.LoadValueByIntKey(big.NewInt(0x22))
	if err != nil {
		t.Fatal(err)
	}
	if value.MustLoadUInt(8) != 0xBB {
		t.Fatal("unexpected surviving value after lazy root delete")
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
	refView := newCellRefView(root)
	rootWithLazyChild, _, err := refView.cloneWithRef(0, lazyLeft, nil)
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

func TestToBOCSkipsDuplicateLazyRefLoad(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(0): ref}}
	cellWithLazyRefs := cellWithLazyRefsFromCell(src, loader.LoadCell)

	boc := cellWithLazyRefs.ToBOC()
	if boc == nil {
		t.Fatal("expected cell with duplicate lazy refs to serialize")
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
}

func TestToBOCSkipsDuplicateMultiLevelLazyRefLoad(t *testing.T) {
	base := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	ref, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	src := BeginCell().MustStoreRef(ref).MustStoreRef(ref).EndCell()
	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(ref.Level()): ref}}
	cellWithLazyRefs := cellWithLazyRefsFromCell(src, loader.LoadCell)

	got := cellWithLazyRefs.ToBOC()
	if got == nil {
		t.Fatal("expected cell with duplicate multi-level lazy refs to serialize")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected load calls: got=%d want=1", loader.calls)
	}
	if loader.lastHash != ref.HashKey(ref.Level()) {
		t.Fatalf("unexpected load hash: got=%x want=%x", loader.lastHash, ref.HashKey(ref.Level()))
	}

	want := src.ToBOC()
	if !bytes.Equal(got, want) {
		t.Fatal("lazy multi-level boc diverges from eager serialization")
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

func TestCellRefViewBoundaryModeDoesNotLoadLazyRef(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	src := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(ref).EndCell()
	cellWithLazyRef := cellWithLazyRefsFromCell(src)

	view := newCellRefView(cellWithLazyRef)
	stored := cellWithLazyRef.ref(0)
	if !stored.IsLazy() || stored.GetType() != PrunedCellType {
		t.Fatal("stored ref should be lazy pruned boundary")
	}

	boundary, err := view.boundaryRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if boundary != stored {
		t.Fatal("boundary mode should preserve stored lazy boundary")
	}

	loader := &testLazyLoader{cells: map[Hash]*Cell{ref.HashKey(): ref}}
	view = newCellRefView(cellWithLazyRefsFromCell(src, loader.LoadCell))
	boundary, err = view.boundaryRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundary.IsLazy() {
		t.Fatal("boundary mode should not load full ref")
	}
	if loader.calls != 0 {
		t.Fatalf("unexpected load calls after boundary read: got=%d want=0", loader.calls)
	}
	loaded, err := boundary.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MustLoadUInt(8) != 0xA5 {
		t.Fatal("begin parse should load full ref")
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
