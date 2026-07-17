package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestReportParityVirtualizedBOCSerialization(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	body := BeginCell().MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(body, 1)
	if err != nil {
		t.Fatalf("create pruned branch: %v", err)
	}
	virtual := pruned.Virtualize(0)
	if !virtual.IsVirtualized() {
		t.Fatal("test fixture is not virtualized")
	}

	if _, err = virtual.ToBOCWithOptionsErr(BOCSerializeOptions{}); !errors.Is(err, ErrVirtualizedCell) {
		t.Fatalf("virtualized root serialization error = %v, want ErrVirtualizedCell", err)
	}
	parent := BeginCell().MustStoreRef(virtual).EndCell()
	if _, err = parent.ToBOCWithOptionsErr(BOCSerializeOptions{}); !errors.Is(err, ErrVirtualizedCell) {
		t.Fatalf("virtualized nested serialization error = %v, want ErrVirtualizedCell", err)
	}
}

func TestReportParityAugmentedExtraUsesTopRefIdentity(t *testing.T) {
	common := bytes.Repeat([]byte{0x11}, hashSize)
	leftRef := reportParityPrunedRef(t, common, bytes.Repeat([]byte{0x22}, hashSize))
	rightRef := reportParityPrunedRef(t, common, bytes.Repeat([]byte{0x33}, hashSize))
	if leftRef.HashKey(0) != rightRef.HashKey(0) {
		t.Fatal("fixture refs must have the same level-0 hash")
	}
	if leftRef.HashKey() == rightRef.HashKey() {
		t.Fatal("fixture refs must have different top hashes")
	}

	left := BeginCell().MustStoreUInt(0xA, 4).MustStoreRef(leftRef).EndCell()
	right := BeginCell().MustStoreUInt(0xA, 4).MustStoreRef(rightRef).EndCell()
	if equalCellContents(left, right) {
		t.Fatal("cell contents with different top-level reference identities were treated as equal")
	}
	if !equalCellContents(left, left) {
		t.Fatal("identical cell contents were treated as different")
	}

	trusted := BeginCell().MustStoreUInt(0xB, 4).MustStoreRef(leftRef).EndCell()
	trusted.setHashAt(trusted.getLevelMask().getHashIndex(), left.Hash())
	if trusted.HashKey() != left.HashKey() {
		t.Fatal("fixture whole-cell hashes differ")
	}
	if equalCellContents(left, trusted) {
		t.Fatal("trusted whole-cell hash hid different extra bits")
	}

	payload := append([]byte{byte(LibraryCellType)}, bytes.Repeat([]byte{0x44}, hashSize)...)
	ordinary := BeginCell().MustStoreSlice(payload, uint(len(payload)*8)).EndCell()
	library, err := BeginCell().MustStoreSlice(payload, uint(len(payload)*8)).EndCellSpecial(true)
	if err != nil {
		t.Fatalf("build library fixture: %v", err)
	}
	if ordinary.HashKey() == library.HashKey() {
		t.Fatal("different ordinary/library descriptors must change the parent hash")
	}
	if !equalCellContents(ordinary, library) {
		t.Fatal("parent-hash mismatch hid equal C++ cell contents")
	}
}

func reportParityPrunedRef(tb testing.TB, level0, level1 []byte) *Cell {
	tb.Helper()

	const storedHashes = 2
	data := make([]byte, 2+storedHashes*(hashSize+depthSize))
	data[0] = byte(PrunedCellType)
	data[1] = 0b011
	copy(data[2:2+hashSize], level0)
	copy(data[2+hashSize:2+2*hashSize], level1)
	depthOff := 2 + storedHashes*hashSize
	binary.BigEndian.PutUint16(data[depthOff:depthOff+depthSize], 0)
	binary.BigEndian.PutUint16(data[depthOff+depthSize:depthOff+2*depthSize], 0)

	ref := &Cell{data: data, bitsSz: uint16(len(data) * 8)}
	ref.setSpecial(true)
	ref.setLevelMask(LevelMask{Mask: 0b011})
	if err := validateBoundaryCell(ref); err != nil {
		tb.Fatalf("validate pruned fixture: %v", err)
	}
	if err := ref.calculateHashes(); err != nil {
		tb.Fatalf("calculate pruned fixture hashes: %v", err)
	}
	return ref
}

func TestTrustedHashesTrustsStoredDepths(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	root := BeginCell().MustStoreRef(child).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithIndex:   true,
		WithTopHash: true,
	})
	cellOffset := firstBOCCellOffset(t, boc)
	depthOffset := cellOffset + 2 + hashSize
	mutated := append([]byte(nil), boc...)
	binary.BigEndian.PutUint16(mutated[depthOffset:depthOffset+depthSize], root.Depth()+1)

	if _, err := FromBOC(mutated); err == nil {
		t.Fatal("untrusted parse accepted a structurally invalid stored depth")
	}

	for _, tt := range []struct {
		name string
		opts BOCParseOptions
	}{
		{name: "eager", opts: BOCParseOptions{TrustedHashes: true}},
		{name: "lazy", opts: BOCParseOptions{TrustedHashes: true, Lazy: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := FromBOCWithOptions(mutated, tt.opts); err != nil {
				t.Fatalf("trusted metadata was structurally revalidated: %v", err)
			}
		})
	}

	if _, err := OpenBOCView(bytes.NewReader(mutated), int64(len(mutated)), BOCViewOptions{
		TrustedHashes: true,
		RequireIndex:  true,
	}); err != nil {
		t.Fatalf("trusted-hash BOC view structurally revalidated depths: %v", err)
	}
}

func TestLazyTrustedHashesSkipsNestedDepthValidationAtOpen(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	child := BeginCell().MustStoreRef(leaf).EndCell()
	root := BeginCell().MustStoreRef(child).EndCell()

	serializer, err := newBOCSerializer([]*Cell{root}, 0)
	if err != nil {
		t.Fatal(err)
	}
	serializer.intHashes = 0
	serializer.topHashes = 0
	for i := range serializer.cellList {
		serializer.cellList[i].wt = 0
		serializer.intHashes += int(serializer.cellList[i].hcnt)
	}
	boc := serializer.serialize(bocModeWithIntHashes)

	mutated := append([]byte(nil), boc...)
	offset := firstBOCCellOffset(t, mutated)
	for idx := 0; idx < 3; idx++ {
		info, next, err := parseBOCPayloadCellInfo(mutated, offset, len(mutated), 1, false)
		if err != nil {
			t.Fatalf("parse serialized cell %d: %v", idx, err)
		}
		if !info.withHashes() {
			t.Fatalf("serialized cell %d has no trusted metadata", idx)
		}
		if idx < 2 {
			depthOffset := info.bodyOffset - depthSize
			depth := binary.BigEndian.Uint16(mutated[depthOffset : depthOffset+depthSize])
			binary.BigEndian.PutUint16(mutated[depthOffset:depthOffset+depthSize], depth+1)
		}
		offset = next
	}

	if _, err = FromBOCWithOptions(mutated, BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	}); err != nil {
		t.Fatalf("lazy trusted-hash open traversed consistently forged nested depths: %v", err)
	}
}

func TestLazyTrustedBOCSkipsSamePayloadRefRevalidation(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := BeginCell().MustStoreRef(child).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithIndex:   true,
		WithTopHash: true,
	})

	offset := firstBOCCellOffset(t, boc)
	rootInfo, offset, err := parseBOCPayloadCellInfo(boc, offset, len(boc), 1, false)
	if err != nil {
		t.Fatalf("parse root record: %v", err)
	}
	if !rootInfo.withHashes() {
		t.Fatal("root record must carry trusted metadata")
	}
	childInfo, _, err := parseBOCPayloadCellInfo(boc, offset, len(boc), 1, false)
	if err != nil {
		t.Fatalf("parse child record: %v", err)
	}
	if childInfo.withHashes() {
		t.Fatal("child record must exercise computed lazy metadata")
	}

	parsed, err := FromBOCWithOptions(boc, BOCParseOptions{
		Lazy:          true,
		TrustedHashes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := parsed.rawRefs()
	if len(refs) != 1 || !refs[0].IsLazy() {
		t.Fatal("expected a lazy child boundary")
	}
	if refs[0].meta == nil || !refs[0].meta.skipLazyRefValidation {
		t.Fatal("trusted same-payload loader retained redundant ref validation")
	}
	loaded, err := refs[0].load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HashKey() != child.HashKey() {
		t.Fatal("trusted lazy child changed during materialization")
	}
}

func TestReportParityLazyLoaderValidatesPlaceholderMetadata(t *testing.T) {
	expected := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	wrong := BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(expected), func(Hash) (*Cell, error) {
		return wrong, nil
	})
	if lazy.meta.skipLazyRefValidation {
		t.Fatal("external lazy loader unexpectedly bypassed metadata validation")
	}
	if _, err := lazy.BeginParse(); !errors.Is(err, ErrLazyRefMismatch) {
		t.Fatalf("unrelated lazy load error = %v, want ErrLazyRefMismatch", err)
	}

	badDepth := lazyRefFromCell(expected)
	badDepth.Depths = append([]uint16(nil), badDepth.Depths...)
	badDepth.Depths[len(badDepth.Depths)-1]++
	lazy = mustCreateLazyPrunedRef(t, badDepth, func(Hash) (*Cell, error) {
		return expected, nil
	})
	if _, err := lazy.BeginParse(); !errors.Is(err, ErrLazyRefMismatch) {
		t.Fatalf("lazy depth mismatch error = %v, want ErrLazyRefMismatch", err)
	}
}

func TestReportParityMayApplyDoesNotLoadDestination(t *testing.T) {
	from := BeginCell().MustStoreUInt(0x01, 8).EndCell()
	to := BeginCell().MustStoreUInt(0x02, 8).EndCell()
	update, err := CreateMerkleUpdate(from, to)
	if err != nil {
		t.Fatalf("create merkle update: %v", err)
	}

	loader := &testLazyLoader{cells: map[Hash]*Cell{
		from.HashKey(): from,
	}}
	lazyUpdate := cellWithLazyRefsFromCell(update, loader.LoadCell)
	if err = MayApplyMerkleUpdate(from, lazyUpdate); err != nil {
		t.Fatalf("MayApplyMerkleUpdate loaded the unavailable destination: %v", err)
	}
	if loader.calls != 0 {
		t.Fatalf("MayApplyMerkleUpdate materialized %d refs, want 0", loader.calls)
	}
	if err = ValidateMerkleUpdate(lazyUpdate); !errors.Is(err, ErrLazyRefNotFound) {
		t.Fatalf("validation error = %v, want unavailable destination", err)
	}
}

func TestReportParityBOCRequiresZeroLevelRootByDefault(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	body := BeginCell().MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(body, 1)
	if err != nil {
		t.Fatalf("create pruned root: %v", err)
	}
	boc := pruned.ToBOC()

	if _, err = FromBOC(boc); err == nil {
		t.Fatal("default BOC parse accepted a non-zero-level root")
	}
	if _, err = FromBOCWithOptions(boc, BOCParseOptions{AllowNonZeroLevelRoot: true}); err != nil {
		t.Fatalf("explicit non-zero-level parse failed: %v", err)
	}
}
