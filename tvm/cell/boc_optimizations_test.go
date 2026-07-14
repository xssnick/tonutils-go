package cell

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"testing"
)

var benchmarkBOCViewSink *BOCView

func TestBOCAdaptiveBufferSize(t *testing.T) {
	tests := []struct {
		name  string
		total uint64
		limit int
		want  int
	}{
		{name: "zero uses limit", total: 0, limit: bocStreamBufferSize, want: bocStreamBufferSize},
		{name: "tiny", total: 17, limit: bocStreamBufferSize, want: 17},
		{name: "below normal limit", total: bocStreamBufferSize - 1, limit: bocStreamBufferSize, want: bocStreamBufferSize - 1},
		{name: "normal limit", total: bocStreamBufferSize, limit: bocStreamBufferSize, want: bocStreamBufferSize},
		{name: "above normal limit", total: bocStreamBufferSize + 1, limit: bocStreamBufferSize, want: bocStreamBufferSize},
		{name: "below large limit", total: largeBOCStreamBufferSize - 1, limit: largeBOCStreamBufferSize, want: largeBOCStreamBufferSize - 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := bocAdaptiveBufferSize(test.total, test.limit); got != test.want {
				t.Fatalf("unexpected buffer size: got %d want %d", got, test.want)
			}
		})
	}
}

func TestBOCStreamAndFileHashParityAllModes(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xab, 8).EndCell()
	left := BeginCell().MustStoreUInt(0x05, 3).MustStoreRef(shared).EndCell()
	right := BeginCell().MustStoreUInt(0x11, 5).MustStoreRef(shared).EndCell()
	root := BeginCell().MustStoreUInt(0x77, 7).MustStoreRef(left).MustStoreRef(right).EndCell()
	roots := []*Cell{right, root, shared}
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

		t.Run(testBOCViewModeName(opts), func(t *testing.T) {
			want, err := ToBOCWithOptionsErr(roots, opts)
			if err != nil {
				t.Fatalf("serialize boc: %v", err)
			}

			var streamed bytes.Buffer
			if err = WriteBOCWithOptions(&streamed, roots, opts); err != nil {
				t.Fatalf("stream boc: %v", err)
			}
			if !bytes.Equal(streamed.Bytes(), want) {
				t.Fatal("streamed boc differs from slice serialization")
			}

			parsedRoots, err := FromBOCMultiRoot(want)
			if err != nil {
				t.Fatalf("parse multi-root boc: %v", err)
			}
			if len(parsedRoots) != len(roots) {
				t.Fatalf("unexpected roots count: got %d want %d", len(parsedRoots), len(roots))
			}
			for i := range roots {
				if parsedRoots[i].HashKey() != roots[i].HashKey() {
					t.Fatalf("root %d changed after in-place reorder", i)
				}
			}

			bag, err := newBOCSerializer(roots, 0)
			if err != nil {
				t.Fatalf("create serializer: %v", err)
			}
			gotHash, ok := bag.computeFileHash(mode)
			if !ok {
				t.Fatal("compute file hash failed")
			}
			wantHash := sha256.Sum256(want)
			if !bytes.Equal(gotHash, wantHash[:]) {
				t.Fatalf("file hash mismatch: got %x want %x", gotHash, wantHash)
			}
		})
	}
}

func TestWriteBOCAdaptiveBufferPropagatesShortWrite(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xab, 8).EndCell()
	err := WriteBOCWithOptions(shortWriter{}, []*Cell{root}, mode31Options())
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short writer returned %v, want %v", err, io.ErrShortWrite)
	}
}

func TestBOCViewReverseMetaMatchesPrunedMultiLevelCells(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xabcdef, 24).EndCell()
	base := BeginCell().MustStoreUInt(0x42, 8).MustStoreRef(leaf).EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatalf("create pruned branch: %v", err)
	}
	root := BeginCell().MustStoreUInt(0x17, 5).MustStoreRef(pruned).EndCell()
	opts := mode31Options()
	boc := root.ToBOCWithOptions(opts)

	for _, trusted := range []bool{false, true} {
		t.Run(fmt.Sprintf("trusted=%t", trusted), func(t *testing.T) {
			roots, parsed, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{TrustedHashes: trusted})
			if err != nil {
				t.Fatalf("parse boc: %v", err)
			}
			view, err := OpenBOCView(bytes.NewReader(boc), int64(len(boc)), BOCViewOptions{
				TrustedHashes: trusted,
				RequireIndex:  true,
				ValidateCRC:   true,
			})
			if err != nil {
				t.Fatalf("open boc view: %v", err)
			}
			assertBOCViewMatchesParsed(t, view, roots, parsed)

			rootMeta, err := view.CellMeta(view.Roots()[0])
			if err != nil {
				t.Fatalf("read root meta: %v", err)
			}
			if rootMeta.Count <= 1 {
				t.Fatalf("expected multi-level root metadata, got %d hash", rootMeta.Count)
			}
			if trusted && !rootMeta.Stored {
				t.Fatal("trusted view did not retain serialized root metadata")
			}
		})
	}
}

func TestBOCViewReaderAcceptsFullReadWithError(t *testing.T) {
	boc := testFlatBOC(t, 1, true)
	reader := &fullReadErrorReaderAt{data: boc}
	view, err := OpenBOCView(reader, int64(len(boc)), BOCViewOptions{RequireIndex: true})
	if err != nil {
		t.Fatalf("full ReaderAt reads with a non-nil error should succeed: %v", err)
	}
	if view.Cells() != 1 {
		t.Fatalf("unexpected cells count: %d", view.Cells())
	}
}

func TestBOCViewReaderRejectsShortReadWithoutError(t *testing.T) {
	boc := testFlatBOC(t, 1, true)
	reader := &shortReadNilErrorReaderAt{data: boc}
	if _, err := OpenBOCView(reader, int64(len(boc)), BOCViewOptions{RequireIndex: true}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short ReaderAt read returned %v, want %v", err, io.ErrUnexpectedEOF)
	}

	view := &BOCView{r: reader, size: int64(len(boc))}
	var buf [4]byte
	if err := view.readDirectAt(buf[:], 1); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short direct read returned %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestBOCViewCellInfoDoesNotAllocateBody(t *testing.T) {
	tests := []struct {
		name      string
		dsc2      byte
		last      byte
		wantBits  uint16
		maxAllocs float64
	}{
		{name: "byte aligned", dsc2: 254, wantBits: 127 * 8, maxAllocs: 0},
		{name: "partial byte", dsc2: 253, last: 0x40, wantBits: 126*8 + 1, maxAllocs: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bodySize := cellBodyBytesSize(test.dsc2)
			payload := make([]byte, 2+bodySize)
			payload[1] = test.dsc2
			payload[len(payload)-1] = test.last

			view := &BOCView{
				r:           bytes.NewReader(payload),
				size:        int64(len(payload)),
				payloadSize: uint64(len(payload)),
				refSize:     1,
				window:      append([]byte(nil), payload...),
			}
			var scratch [3]byte

			info, size, err := view.readCellInfoAt(0, uint64(len(payload)), &scratch)
			if err != nil {
				t.Fatalf("read cell info: %v", err)
			}
			if info.bitsSz != test.wantBits || size != uint64(len(payload)) {
				t.Fatalf("unexpected cell info: bits=%d size=%d", info.bitsSz, size)
			}

			allocs := testing.AllocsPerRun(100, func() {
				if _, _, err = view.readCellInfoAt(0, uint64(len(payload)), &scratch); err != nil {
					t.Fatalf("read cell info: %v", err)
				}
			})
			if allocs > test.maxAllocs {
				t.Fatalf("cell info scan allocated body memory: got %.2f allocs want <= %.0f", allocs, test.maxAllocs)
			}
		})
	}
}

func TestBOCViewCellInfoRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "truncated stored hashes", payload: []byte{0x10, 0x00}},
		{name: "truncated body", payload: []byte{0x00, 0x02}},
		{name: "missing terminator", payload: []byte{0x00, 0x01, 0x00}},
		{name: "terminator only", payload: []byte{0x00, 0x01, 0x80}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := &BOCView{
				r:           bytes.NewReader(test.payload),
				size:        int64(len(test.payload)),
				payloadSize: uint64(len(test.payload)),
				refSize:     1,
				window:      append([]byte(nil), test.payload...),
			}
			var scratch [3]byte
			if _, _, err := view.readCellInfoAt(0, uint64(len(test.payload)), &scratch); err == nil {
				t.Fatal("expected malformed cell info to fail")
			}
		})
	}
}

func TestBOCViewReadAmplification(t *testing.T) {
	const cellRecordSize = 2 + 127
	cellCount := bocViewReadWindowSize/cellRecordSize + 8192

	reads := make(map[string]int64, 2)
	for _, indexed := range []bool{true, false} {
		name := "unindexed"
		if indexed {
			name = "indexed"
		}
		t.Run(name, func(t *testing.T) {
			boc := testFlatBOC(t, cellCount, indexed)
			reader := &countingReaderAt{data: boc}
			view, err := OpenBOCView(reader, int64(len(boc)), BOCViewOptions{
				RequireIndex: indexed,
				BuildIndex:   !indexed,
			})
			if err != nil {
				t.Fatalf("open boc view: %v", err)
			}
			if view.Cells() != uint32(cellCount) {
				t.Fatalf("unexpected cells count: got %d want %d", view.Cells(), cellCount)
			}
			if len(view.meta.hashes) != cellCount || len(view.meta.depths) != cellCount {
				t.Fatalf("metadata is not compact: hashes=%d depths=%d cells=%d", len(view.meta.hashes), len(view.meta.depths), cellCount)
			}

			reads[name] = reader.bytesRead
			amplification := float64(reader.bytesRead) / float64(len(boc))
			maxAmplification := 2.1
			if indexed {
				maxAmplification = 1.2
			}
			if amplification > maxAmplification {
				t.Fatalf("read amplification is too high: got %.3fx want <= %.1fx", amplification, maxAmplification)
			}
			t.Logf("reader bytes=%d boc bytes=%d amplification=%.3fx", reader.bytesRead, len(boc), amplification)
		})
	}

	if reads["unindexed"] <= reads["indexed"] {
		t.Fatalf("unindexed scan should read more than indexed scan: indexed=%d unindexed=%d", reads["indexed"], reads["unindexed"])
	}
}

func BenchmarkBOCViewOpen(b *testing.B) {
	for _, cells := range []int{1, 4096} {
		for _, indexed := range []bool{true, false} {
			name := fmt.Sprintf("cells=%d/indexed=%t", cells, indexed)
			b.Run(name, func(b *testing.B) {
				boc := testFlatBOC(b, cells, indexed)
				opts := BOCViewOptions{RequireIndex: indexed, BuildIndex: !indexed}
				b.ReportAllocs()
				b.SetBytes(int64(len(boc)))

				for b.Loop() {
					view, err := OpenBOCView(bytes.NewReader(boc), int64(len(boc)), opts)
					if err != nil {
						b.Fatal(err)
					}
					benchmarkBOCViewSink = view
				}
			})
		}
	}
}

func BenchmarkWriteBOCAdaptiveBuffer(b *testing.B) {
	fixtures := []struct {
		name string
		root *Cell
	}{
		{name: "tiny", root: BeginCell().MustStoreUInt(0xab, 8).EndCell()},
	}
	medium, _, _ := buildLargeBOCBenchmarkRoot(b, 512<<10)
	fixtures = append(fixtures, struct {
		name string
		root *Cell
	}{name: "medium", root: medium})

	opts := mode31Options()
	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := WriteBOCWithOptions(io.Discard, []*Cell{fixture.root}, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type countingReaderAt struct {
	data      []byte
	bytesRead int64
}

type fullReadErrorReaderAt struct {
	data []byte
}

type shortReadNilErrorReaderAt struct {
	data []byte
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	return len(data) - 1, nil
}

func (r *fullReadErrorReaderAt) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(dst, r.data[offset:])
	return n, io.EOF
}

func (r *shortReadNilErrorReaderAt) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(r.data)) || len(dst) == 0 {
		return 0, nil
	}
	if offset == 0 {
		return copy(dst, r.data), nil
	}
	n := min(len(dst)-1, len(r.data)-int(offset))
	return copy(dst, r.data[offset:int(offset)+n]), nil
}

func (r *countingReaderAt) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(r.data)) {
		return 0, io.EOF
	}

	n := copy(dst, r.data[offset:])
	r.bytesRead += int64(n)
	if n != len(dst) {
		return n, io.EOF
	}
	return n, nil
}

func testFlatBOC(tb testing.TB, cellCount int, indexed bool) []byte {
	tb.Helper()
	if cellCount <= 0 {
		tb.Fatal("cell count must be positive")
	}

	const cellBodySize = 127
	const cellRecordSize = 2 + cellBodySize
	payloadSize := uint64(cellCount * cellRecordSize)

	refSize := 1
	for uint64(cellCount) >= uint64(1)<<uint(refSize*8) {
		refSize++
	}
	offsetSize := 1
	for payloadSize >= uint64(1)<<uint(offsetSize*8) {
		offsetSize++
	}

	headerSize := len(bocMagic) + 2 + 3*refSize + offsetSize + refSize
	indexSize := 0
	if indexed {
		indexSize = cellCount * offsetSize
	}
	boc := make([]byte, headerSize+indexSize+int(payloadSize))
	pos := copy(boc, bocMagic)
	flags := byte(refSize)
	if indexed {
		flags |= 1 << 7
	}
	boc[pos] = flags
	pos++
	boc[pos] = byte(offsetSize)
	pos++

	storeUintTo(boc[pos:pos+refSize], uint64(cellCount), refSize)
	pos += refSize
	storeUintTo(boc[pos:pos+refSize], 1, refSize)
	pos += refSize
	storeUintTo(boc[pos:pos+refSize], 0, refSize)
	pos += refSize
	storeUintTo(boc[pos:pos+offsetSize], payloadSize, offsetSize)
	pos += offsetSize
	storeUintTo(boc[pos:pos+refSize], 0, refSize)
	pos += refSize

	if indexed {
		for i := 0; i < cellCount; i++ {
			cellEnd := uint64((i + 1) * cellRecordSize)
			storeUintTo(boc[pos:pos+offsetSize], cellEnd, offsetSize)
			pos += offsetSize
		}
	}

	for i := 0; i < cellCount; i++ {
		boc[pos] = 0
		boc[pos+1] = cellBodySize * 2
		pos += cellRecordSize
	}
	return boc
}
