package cell

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBOCHashIndexHandlesFingerprintCollision(t *testing.T) {
	var h1, h2 Hash
	binary.LittleEndian.PutUint32(h1[:4], 0x55667788)
	binary.LittleEndian.PutUint32(h2[:4], 0x55667788)
	h1[4] = 1
	h2[4] = 2

	c1 := &Cell{hash0: h1}
	c2 := &Cell{hash0: h2}
	items := []bocSerializeItem{
		{cell: c1},
		{cell: c2},
	}

	var idx bocHashIndex
	h1Bytes := c1.getHash(_DataCellMaxLevel)
	h2Bytes := c2.getHash(_DataCellMaxLevel)
	if bocHashFingerprint(h1Bytes) != bocHashFingerprint(h2Bytes) || h1 == h2 {
		t.Fatal("test hashes do not form a 32-bit fingerprint collision")
	}
	idx.set(bocHashFingerprint(h1Bytes), 0)
	if got, ok := idx.get(bocHashFingerprint(h2Bytes), h2Bytes, c2, items[:1]); ok {
		t.Fatalf("different full hash matched colliding fingerprint at index %d", got)
	}

	idx.set(bocHashFingerprint(h2Bytes), 1)
	if got, ok := idx.get(bocHashFingerprint(h1Bytes), h1Bytes, c1, items); !ok || got != 0 {
		t.Fatalf("first colliding hash lookup mismatch: got (%d, %v)", got, ok)
	}
	if got, ok := idx.get(bocHashFingerprint(h2Bytes), h2Bytes, c2, items); !ok || got != 1 {
		t.Fatalf("second colliding hash lookup mismatch: got (%d, %v)", got, ok)
	}
}

func TestBOCHashIndexZeroFingerprintSurvivesGrowth(t *testing.T) {
	const cellsCount = 100

	items := make([]bocSerializeItem, cellsCount)
	var index bocHashIndex
	for i := range items {
		var hash Hash
		binary.LittleEndian.PutUint32(hash[4:8], uint32(i+1))
		items[i].cell = &Cell{hash0: hash}
		index.set(bocHashFingerprint(hash[:]), uint32(i))
	}

	for i, item := range items {
		hash := item.cell.getHash(_DataCellMaxLevel)
		if fingerprint := bocHashFingerprint(hash); fingerprint != 0 {
			t.Fatalf("cell %d has unexpected fingerprint %x", i, fingerprint)
		}
		if got, ok := index.get(0, hash, item.cell, items); !ok || got != uint32(i) {
			t.Fatalf("cell %d lookup failed after growth: got %d, found %t", i, got, ok)
		}
	}
}

func TestBOCHashIndexSerializationRoundTrip(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	left := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(shared).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(shared).EndCell()
	root := BeginCell().MustStoreRef(left).MustStoreRef(right).EndCell()

	bag, err := newBOCSerializer([]*Cell{root}, 0)
	if err != nil {
		t.Fatalf("failed to import cells: %v", err)
	}
	if bag.cellCount != 4 {
		t.Fatalf("shared cell was not deduplicated: got %d cells, want 4", bag.cellCount)
	}
	if bag.cellIndex != nil {
		t.Fatal("serializer retained hash index after import")
	}
	if bag.cellList[bag.roots[0].idx].cell != root.rawCell() {
		t.Fatal("in-place reorder did not preserve root remap")
	}

	for _, opts := range []BOCSerializeOptions{
		{},
		{WithCRC32C: true},
		{WithIndex: true},
		{WithCRC32C: true, WithIndex: true, WithCacheBits: true},
		{WithCRC32C: true, WithIndex: true, WithTopHash: true, WithIntHashes: true},
		{WithCRC32C: true, WithIndex: true, WithCacheBits: true, WithTopHash: true, WithIntHashes: true},
	} {
		boc, err := ToBOCWithOptionsErr([]*Cell{root}, opts)
		if err != nil {
			t.Fatalf("failed to serialize boc for %+v: %v", opts, err)
		}

		parsed, err := FromBOC(boc)
		if err != nil {
			t.Fatalf("failed to parse boc for %+v: %v", opts, err)
		}
		if parsed.HashKey() != root.HashKey() {
			t.Fatalf("parsed root hash mismatch for %+v", opts)
		}

		serializedAgain, err := ToBOCWithOptionsErr([]*Cell{parsed}, opts)
		if err != nil {
			t.Fatalf("failed to reserialize boc for %+v: %v", opts, err)
		}
		if !bytes.Equal(serializedAgain, boc) {
			t.Fatalf("boc round trip changed serialization for %+v", opts)
		}
	}
}
