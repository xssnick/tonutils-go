package cell

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
	"unsafe"
)

func TestBOCPayloadCellInfoCompactOffsets(t *testing.T) {
	wantSize := uintptr(12)
	if unsafe.Sizeof(int(0)) == 8 {
		wantSize = 16
	}
	if got := unsafe.Sizeof(bocPayloadCellInfo{}); got != wantSize {
		t.Fatalf("payload metadata size = %d, want %d", got, wantSize)
	}

	for descriptor := 0; descriptor < 256; descriptor++ {
		for refSize := 1; refSize <= 4; refSize++ {
			for _, withHashes := range []bool{false, true} {
				const begin = 9
				bodySize := cellBodyBytesSize(byte(descriptor))
				d1 := byte(4)
				bodyOffset := begin + 2
				if withHashes {
					d1 |= 0x10 | 0x60
					bodyOffset += 3 * (hashSize + depthSize)
				}
				end := bodyOffset + bodySize + 4*refSize
				payload := make([]byte, end+7)
				payload[begin], payload[begin+1] = d1, byte(descriptor)
				for i := 0; i < bodySize; i++ {
					payload[bodyOffset+i] = 0xA5
				}
				if descriptor&1 != 0 {
					payload[bodyOffset+bodySize-1] = 1
				}
				for ref := 0; ref < 4; ref++ {
					offset := bodyOffset + bodySize + ref*refSize
					storeUintTo(payload[offset:offset+refSize], uint64(31+ref), refSize)
				}

				info, next, err := parseBOCPayloadCellInfo(payload, begin, end, refSize, true)
				if err != nil {
					t.Fatalf("descriptor %d, ref size %d, hashes %t: %v", descriptor, refSize, withHashes, err)
				}
				body := info.body(payload)
				if next != end || info.bodyOffset != bodyOffset || len(body) != bodySize || cap(body) != bodySize {
					t.Fatalf("unexpected body bounds for descriptor %d: %+v, next %d, len/cap %d/%d", descriptor, info, next, len(body), cap(body))
				}
				if !bytes.Equal(body, payload[bodyOffset:bodyOffset+bodySize]) {
					t.Fatalf("body changed for descriptor %d", descriptor)
				}
				for ref := 0; ref < 4; ref++ {
					if got := info.refIndex(payload, ref, refSize); got != 31+ref {
						t.Fatalf("descriptor %d ref %d = %d, want %d", descriptor, ref, got, 31+ref)
					}
				}
			}
		}
	}
}

type testZeroPayloadLoader struct {
	testBatchRecordLoader
	payloadBatches [][]Hash
	loadPayload    func(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error)
}

func (l *testZeroPayloadLoader) LoadPayload(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error) {
	if len(hashes) == 0 {
		return nil, errors.New("empty payload batch requested")
	}
	for _, hash := range hashes {
		if l.records[hash].meta.BitsSz == 0 {
			return nil, errors.New("zero-bit payload requested")
		}
	}
	l.payloadBatches = append(l.payloadBatches, append([]Hash(nil), hashes...))
	if l.loadPayload != nil {
		return l.loadPayload(hashes, dst)
	}
	return l.testBatchRecordLoader.LoadPayload(hashes, dst)
}

func TestToLargeBOCSkipsEmptyPayloadsAllModes(t *testing.T) {
	empty := BeginCell().EndCell()
	leaf := BeginCell().MustStoreUInt(0x1D, 5).EndCell()
	left := BeginCell().MustStoreRef(leaf).MustStoreRef(empty).EndCell()
	right := BeginCell().MustStoreUInt(0xABC, 12).MustStoreRef(left).EndCell()
	mixed := BeginCell().MustStoreRef(left).MustStoreRef(right).MustStoreRef(leaf).EndCell()
	zeroLeft := BeginCell().MustStoreRef(empty).EndCell()
	zeroRight := BeginCell().MustStoreRef(zeroLeft).MustStoreRef(empty).EndCell()
	zeroRoot := BeginCell().MustStoreRef(zeroLeft).MustStoreRef(zeroRight).EndCell()

	for name, roots := range map[string][]*Cell{
		"mixed":     {mixed, right, empty},
		"all-empty": {zeroRoot, zeroLeft, empty},
		"empty":     {empty},
	} {
		t.Run(name, func(t *testing.T) {
			records := testCellRecordsForTree(roots[0])
			hashes := make([]Hash, len(roots))
			for i, root := range roots {
				hashes[i] = root.HashKey()
			}
			payloads := 0
			for _, record := range records {
				if record.meta.BitsSz > 0 {
					payloads++
				}
			}
			for mode := 0; mode < 32; mode++ {
				opts := BOCSerializeOptions{
					WithIndex:     mode&bocModeWithIndex != 0,
					WithCRC32C:    mode&bocModeWithCRC32C != 0,
					WithTopHash:   mode&bocModeWithTopHash != 0,
					WithIntHashes: mode&bocModeWithIntHashes != 0,
					WithCacheBits: mode&bocModeWithCacheBits != 0,
				}
				if opts.WithCacheBits && !opts.WithIndex {
					continue
				}
				for _, batchSize := range []int{1, 2, 3, 32} {
					t.Run(fmt.Sprintf("mode%d/batch%d", mode, batchSize), func(t *testing.T) {
						loader := &testZeroPayloadLoader{testBatchRecordLoader: testBatchRecordLoader{records: records}}
						var got bytes.Buffer
						if err := ToLargeBOC(&got, hashes, opts, loader, uint64(len(records)), batchSize); err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(got.Bytes(), ToBOCWithOptions(roots, opts)) {
							t.Fatal("large BoC differs from ordinary serialization")
						}
						rows := 0
						for _, batch := range loader.payloadBatches {
							rows += len(batch)
							if len(batch) > batchSize {
								t.Fatalf("payload batch size %d exceeds %d", len(batch), batchSize)
							}
						}
						if rows != payloads || len(loader.payloadBatches) != (payloads+batchSize-1)/batchSize {
							t.Fatalf("loaded %d rows in %d batches, want %d rows in %d batches", rows, len(loader.payloadBatches), payloads, (payloads+batchSize-1)/batchSize)
						}
					})
				}
			}
		})
	}
}

func TestToLargeBOCNonemptyPayloadErrors(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0x1FF, 9).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()
	records := testCellRecordsForTree(root)
	loadErr := errors.New("payload load failed")
	for _, name := range []string{"missing", "extra", "short", "loader"} {
		t.Run(name, func(t *testing.T) {
			loader := &testZeroPayloadLoader{testBatchRecordLoader: testBatchRecordLoader{records: records}}
			loader.loadPayload = func(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error) {
				switch name {
				case "missing":
					return dst, nil
				case "extra":
					return append(dst, records[hashes[0]].payload, LargeBOCPayloadRecord{}), nil
				case "short":
					return append(dst, LargeBOCPayloadRecord{Data: []byte{0xFF}}), nil
				default:
					return dst, loadErr
				}
			}
			err := ToLargeBOC(io.Discard, []Hash{root.HashKey()}, BOCSerializeOptions{}, loader, uint64(len(records)), 1)
			if err == nil {
				t.Fatal("invalid payload accepted")
			}
			if (name == "missing" || name == "extra") && !errors.Is(err, ErrLazyRefNotFound) {
				t.Fatalf("expected missing record error, got %v", err)
			}
			if name == "loader" && !errors.Is(err, loadErr) {
				t.Fatalf("expected loader error, got %v", err)
			}
		})
	}
}

func TestToLargeBOCEmptyPayloadPrefetchCleanup(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(1, 8).EndCell()
	empty := BeginCell().MustStoreRef(leaf).EndCell()
	root := BeginCell().MustStoreUInt(2, 8).MustStoreRef(empty).EndCell()
	records := testCellRecordsForTree(root)
	loader := &testZeroPayloadLoader{testBatchRecordLoader: testBatchRecordLoader{records: records}}
	started := make(chan struct{})
	release := make(chan struct{})
	var prefetched []LargeBOCPayloadRecord
	loader.loadPayload = func(hashes []Hash, dst []LargeBOCPayloadRecord) ([]LargeBOCPayloadRecord, error) {
		if len(loader.payloadBatches) == 1 {
			return append(dst, LargeBOCPayloadRecord{}), nil
		}
		close(started)
		<-release
		prefetched = append(dst, records[hashes[0]].payload)
		return prefetched, nil
	}
	finished := make(chan error, 1)
	go func() {
		finished <- ToLargeBOC(io.Discard, []Hash{root.HashKey()}, BOCSerializeOptions{}, loader, uint64(len(records)), 1)
	}()
	<-started
	select {
	case err := <-finished:
		t.Fatalf("returned before prefetch finished: %v", err)
	default:
	}
	close(release)
	if err := <-finished; err == nil {
		t.Fatal("short first payload accepted")
	}
	if len(prefetched) != 1 || prefetched[0].Data != nil {
		t.Fatal("prefetched payload was not released after the current batch failed")
	}
}
