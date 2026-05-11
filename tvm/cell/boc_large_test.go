package cell

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

const testLargeBOCLoadBatchSize = 1 << 17

type testBatchRecordLoader struct {
	records map[Hash]testLargeBOCRecord
	batches [][]Hash
	buf     []LargeBOCPayloadRecord
	metaBuf []LargeBOCMetaRecord
}

func (l *testBatchRecordLoader) LoadMeta(hashes []Hash, dst []LargeBOCMetaRecord) ([]LargeBOCMetaRecord, error) {
	if l.batches != nil {
		l.batches = append(l.batches, append([]Hash(nil), hashes...))
	}

	records := dst[:0]
	for _, hash := range hashes {
		record, ok := l.records[hash]
		if !ok {
			continue
		}
		records = append(records, record.meta)
	}
	l.metaBuf = records
	return records, nil
}

func (l *testBatchRecordLoader) LoadPayload(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error) {
	records := dst[:0]
	for _, hash := range hashes {
		record, ok := l.records[hash]
		if !ok {
			continue
		}
		records = append(records, record.payload)
	}
	l.buf = records
	return records, nil
}

func TestToLargeBOCMatchesToBOCWithOptions(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	left := BeginCell().MustStoreUInt(0x12, 8).MustStoreRef(leaf).EndCell()
	right := BeginCell().MustStoreUInt(0x34, 8).MustStoreRef(leaf).EndCell()
	root := BeginCell().
		MustStoreUInt(0x99, 8).
		MustStoreRef(left).
		MustStoreRef(right).
		MustStoreRef(leaf).
		EndCell()

	for _, opts := range []BOCSerializeOptions{
		{},
		{WithCRC32C: true},
		{WithIndex: true},
		{
			WithCRC32C:    true,
			WithIndex:     true,
			WithCacheBits: true,
			WithTopHash:   true,
			WithIntHashes: true,
		},
	} {
		loader := &testBatchRecordLoader{
			records: testCellRecordsForTree(root),
		}
		want := ToBOCWithOptions([]*Cell{root, leaf}, opts)
		if len(want) == 0 {
			t.Fatal("expected non-empty boc")
		}

		var got bytes.Buffer
		if err := ToLargeBOC(&got, []Hash{root.HashKey(), leaf.HashKey()}, opts, loader, uint64(len(loader.records)), testLargeBOCLoadBatchSize); err != nil {
			t.Fatalf("failed to write large boc: %v", err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("large boc diverges from ordinary serialization with opts %+v", opts)
		}
	}
}

func TestToLargeBOCLoadsLazyRefsAsRecords(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	left := BeginCell().MustStoreUInt(0x12, 8).MustStoreRef(leaf).EndCell()
	right := BeginCell().MustStoreUInt(0x34, 8).MustStoreRef(leaf).EndCell()
	root := BeginCell().
		MustStoreUInt(0x99, 8).
		MustStoreRef(left).
		MustStoreRef(right).
		MustStoreRef(leaf).
		EndCell()
	lazyRoot := cellWithLazyRefsFromCell(root)
	loader := &testBatchRecordLoader{
		records: testCellRecordsForTree(root),
		batches: [][]Hash{},
	}

	opts := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	}

	want := root.ToBOCWithOptions(opts)
	var got bytes.Buffer
	if err := ToLargeBOC(&got, []Hash{lazyRoot.HashKey()}, opts, loader, uint64(len(loader.records)), testLargeBOCLoadBatchSize); err != nil {
		t.Fatalf("failed to write large lazy boc: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatal("large lazy boc diverges from eager serialization")
	}
	if len(loader.batches) != 2 || len(loader.batches[0]) != 1 || len(loader.batches[1]) != 3 {
		t.Fatalf("unexpected lazy batches: %+v", loader.batches)
	}
}

func TestToLargeBOCLoadsMultiLevelLazyRefsAsRecords(t *testing.T) {
	base := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	ref, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	root := BeginCell().MustStoreRef(ref).MustStoreRef(ref).EndCell()
	lazyRoot := cellWithLazyRefsFromCell(root)
	loader := &testBatchRecordLoader{
		records: testCellRecordsForTree(root),
		batches: [][]Hash{},
	}

	opts := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	}

	want := root.ToBOCWithOptions(opts)
	var got bytes.Buffer
	if err = ToLargeBOC(&got, []Hash{lazyRoot.HashKey()}, opts, loader, uint64(len(loader.records)), testLargeBOCLoadBatchSize); err != nil {
		t.Fatalf("failed to write large multi-level lazy boc: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatal("large multi-level lazy boc diverges from eager serialization")
	}
	if len(loader.batches) != 2 || len(loader.batches[0]) != 1 || len(loader.batches[1]) != 1 {
		t.Fatalf("unexpected lazy batches: %+v", loader.batches)
	}
}

func TestToLargeBOCUsesLoadBatchSize(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
	left := BeginCell().MustStoreUInt(0x12, 8).MustStoreRef(leaf).EndCell()
	right := BeginCell().MustStoreUInt(0x34, 8).MustStoreRef(leaf).EndCell()
	root := BeginCell().
		MustStoreUInt(0x99, 8).
		MustStoreRef(left).
		MustStoreRef(right).
		MustStoreRef(leaf).
		EndCell()
	lazyRoot := cellWithLazyRefsFromCell(root)
	loader := &testBatchRecordLoader{
		records: testCellRecordsForTree(root),
		batches: [][]Hash{},
	}

	var got bytes.Buffer
	if err := ToLargeBOC(&got, []Hash{lazyRoot.HashKey()}, BOCSerializeOptions{}, loader, uint64(len(loader.records)), 2); err != nil {
		t.Fatalf("failed to write large lazy boc: %v", err)
	}
	if len(loader.batches) != 3 {
		t.Fatalf("unexpected batches count: %+v", loader.batches)
	}
	for _, batch := range loader.batches {
		if len(batch) > 2 {
			t.Fatalf("batch is larger than requested size: %+v", loader.batches)
		}
	}
}

func TestToLargeBOCReturnsMissingRecordError(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	root := BeginCell().MustStoreRef(ref).EndCell()
	lazyRoot := cellWithLazyRefsFromCell(root)
	loader := &testBatchRecordLoader{records: map[Hash]testLargeBOCRecord{}}

	var got bytes.Buffer
	err := ToLargeBOC(&got, []Hash{lazyRoot.HashKey()}, BOCSerializeOptions{}, loader, 0, testLargeBOCLoadBatchSize)
	if !errors.Is(err, ErrLazyRefNotFound) {
		t.Fatalf("expected missing record error, got %v", err)
	}
}

type testLargeBOCRecord struct {
	meta    LargeBOCMetaRecord
	payload LargeBOCPayloadRecord
}

func testCellRecordsForTree(root *Cell) map[Hash]testLargeBOCRecord {
	records := map[Hash]testLargeBOCRecord{}
	var walk func(*Cell)
	walk = func(c *Cell) {
		if c == nil {
			return
		}
		c = c.rawCell()
		hash := c.HashKey()
		if _, ok := records[hash]; ok {
			return
		}

		records[hash] = testCellRecordFromCell(c)
		for _, ref := range c.rawRefs() {
			walk(ref)
		}
	}
	walk(root)
	return records
}

func testCellRecordFromCell(c *Cell) testLargeBOCRecord {
	levelMask := c.getLevelMask()
	hashesList, depthsList := collectMetadataHashesDepths(c, levelMask)

	var hashes [4]Hash
	copy(hashes[:], hashesList)

	var depths [4]uint16
	copy(depths[:], depthsList)

	var refs [4]Hash
	refView := newCellRefView(c)
	refsNum := c.refsCount()
	for i := 0; i < refsNum; i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			panic(err)
		}
		refs[i] = ref.HashKey()
	}

	bodySize := c.SerializedBOCBodySize()
	data := make([]byte, bodySize)
	copy(data, c.data[:bodySize])

	d1 := byte(refsNum) + levelMask.Mask*32
	if c.IsSpecial() {
		d1 += 8
	}

	return testLargeBOCRecord{
		meta: LargeBOCMetaRecord{
			D1:     d1,
			BitsSz: c.bitsSz,
			Refs:   refs,
			Hashes: hashes,
			Depths: depths,
		},
		payload: LargeBOCPayloadRecord{Data: data},
	}
}

func BenchmarkToLargeBOC(b *testing.B) {
	const targetSize = 128 << 20

	root, _, _ := buildLargeBOCBenchmarkRoot(b, targetSize)
	lazyRoot := cellWithLazyRefsFromCell(root)
	records := testCellRecordsForTree(root)
	opts := BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	}

	b.Run("ToBOC", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchmarkBOCSink = root.ToBOCWithOptions(opts)
		}
	})

	b.Run("WriteBOC", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := WriteBOCWithOptions(io.Discard, []*Cell{root}, opts); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("ToLargeBOC", func(b *testing.B) {
		loader := &testBatchRecordLoader{records: records}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := ToLargeBOC(io.Discard, []Hash{lazyRoot.HashKey()}, opts, loader, uint64(len(records)), testLargeBOCLoadBatchSize); err != nil {
				b.Fatal(err)
			}
		}
	})

	benchmarkBOCSink = nil
}

func BenchmarkLargeBOCHashIndex(b *testing.B) {
	const targetSize = 128 << 20

	root, _, _ := buildLargeBOCBenchmarkRoot(b, targetSize)
	recordMap := testCellRecordsForTree(root)
	hashes := make([]Hash, 0, len(recordMap))
	for hash := range recordMap {
		hashes = append(hashes, hash)
	}

	b.Run("GoMap", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m := make(map[Hash]uint32, len(hashes))
			for j, hash := range hashes {
				m[hash] = uint32(j)
			}
			for _, hash := range hashes {
				if _, ok := m[hash]; !ok {
					b.Fatal("missing record")
				}
			}
		}
	})

	b.Run("OpenAddress", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			s := &largeBOCSerializer{
				cellIndex: newLargeBOCHashIndex(len(hashes)),
				cellList:  make([]largeBOCItem, 0, len(hashes)),
			}
			for j, hash := range hashes {
				s.cellList = append(s.cellList, largeBOCItem{hash: hash})
				s.cellIndex.set(hash, uint32(j), s)
			}
			for _, hash := range hashes {
				if _, ok := s.cellIndex.get(hash, s); !ok {
					b.Fatal("missing record")
				}
			}
		}
	})
}
