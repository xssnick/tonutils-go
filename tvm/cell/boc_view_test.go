package cell

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

func TestBOCViewReadAtCrossesWindowBoundary(t *testing.T) {
	data := make([]byte, bocViewReadWindowSize+64)
	for i := range data {
		data[i] = byte(i)
	}

	view := &BOCView{
		r:    bytes.NewReader(data),
		size: int64(len(data)),
	}

	offset := uint64(bocViewReadWindowSize - 8)
	buf := make([]byte, 32)
	if err := view.readAt(buf, offset); err != nil {
		t.Fatalf("read across window boundary: %v", err)
	}
	if !bytes.Equal(buf, data[offset:offset+uint64(len(buf))]) {
		t.Fatalf("cross-window read mismatch:\n got %x\nwant %x", buf, data[offset:offset+uint64(len(buf))])
	}

	if err := view.readAt(buf, uint64(len(data)-len(buf))); err != nil {
		t.Fatalf("read at end: %v", err)
	}

	err := view.readAt(buf, uint64(len(data)-len(buf)+1))
	if err == nil {
		t.Fatal("expected short read past end")
	}
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("unexpected short read error: %v", err)
	}
}

func TestBOCViewCrossMatchesParser(t *testing.T) {
	root := testBOCViewCellTree()

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
			boc, err := ToBOCWithOptionsErr([]*Cell{root}, opts)
			if err != nil {
				t.Fatalf("serialize boc: %v", err)
			}

			roots, parsed, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{})
			if err != nil {
				t.Fatalf("parse boc: %v", err)
			}
			view, err := OpenBOCView(bytes.NewReader(boc), int64(len(boc)), BOCViewOptions{
				ValidateCRC: true,
				BuildIndex:  true,
			})
			if err != nil {
				t.Fatalf("open boc view: %v", err)
			}

			assertBOCViewMatchesParsed(t, view, roots, parsed)
		})
	}
}

func TestBOCViewReadersReadConcurrently(t *testing.T) {
	const readers = 4

	root := testBOCViewCellTree()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithIndex: true})
	source := &parallelReaderAt{
		data:   boc,
		target: readers,
		gate:   make(chan struct{}),
	}
	view, err := OpenBOCView(bytes.NewReader(boc), int64(len(boc)), BOCViewOptions{RequireIndex: true})
	if err != nil {
		t.Fatalf("open expected boc view: %v", err)
	}
	expected := make([]BOCCellView, readers)
	for i := range expected {
		expected[i], err = view.Cell(uint32(i))
		if err != nil {
			t.Fatalf("load expected cell %d: %v", i, err)
		}
		expected[i].Body = bytes.Clone(expected[i].Body)
	}

	view, err = OpenBOCView(source, int64(len(boc)), BOCViewOptions{RequireIndex: true})
	if err != nil {
		t.Fatalf("open concurrent boc view: %v", err)
	}
	source.parallel.Store(true)

	actual := make([]BOCCellView, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			actual[i], errs[i] = view.NewReader().ReadCell(uint32(i))
			actual[i].Body = bytes.Clone(actual[i].Body)
		}(i)
	}
	wg.Wait()

	for i := range readers {
		if errs[i] != nil {
			t.Fatalf("reader %d: %v", i, errs[i])
		}
		if actual[i].Index != expected[i].Index ||
			actual[i].D1 != expected[i].D1 ||
			actual[i].D2 != expected[i].D2 ||
			actual[i].Bits != expected[i].Bits ||
			actual[i].Refs != expected[i].Refs ||
			actual[i].Meta != expected[i].Meta ||
			!bytes.Equal(actual[i].Body, expected[i].Body) {
			t.Fatalf("reader %d cell mismatch", i)
		}
	}
	if got := source.maxActive.Load(); got < readers {
		t.Fatalf("concurrent reads = %d, want at least %d", got, readers)
	}
}

func TestBOCViewTrustedHashesAcceptsStoredHashMismatch(t *testing.T) {
	root := testBOCViewCellTree()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithIndex:     true,
		WithTopHash:   true,
		WithIntHashes: true,
	})

	rootHash := root.Hash()
	hashOffset := bytes.Index(boc, rootHash)
	if hashOffset < 0 {
		t.Fatal("serialized root hash not found in boc")
	}
	mutated := append([]byte(nil), boc...)
	mutated[hashOffset] ^= 0xff

	if _, err := OpenBOCView(bytes.NewReader(mutated), int64(len(mutated)), BOCViewOptions{RequireIndex: true}); err == nil {
		t.Fatal("expected untrusted BOC view to reject mutated stored hash")
	}

	view, err := OpenBOCView(bytes.NewReader(mutated), int64(len(mutated)), BOCViewOptions{
		TrustedHashes: true,
		RequireIndex:  true,
	})
	if err != nil {
		t.Fatalf("trusted BOC view should accept stored hash: %v", err)
	}
	cell, err := view.Cell(view.Roots()[0])
	if err != nil {
		t.Fatalf("load root cell: %v", err)
	}
	if cell.Meta.Hash == root.HashKey() {
		t.Fatal("trusted BOC view did not use mutated stored hash")
	}
}

func TestBOCViewValidatesCRC(t *testing.T) {
	root := testBOCViewCellTree()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithCRC32C: true,
		WithIndex:  true,
	})
	mutated := append([]byte(nil), boc...)
	mutated[len(mutated)-1] ^= 0xff

	if _, err := OpenBOCView(bytes.NewReader(mutated), int64(len(mutated)), BOCViewOptions{
		RequireIndex: true,
		ValidateCRC:  true,
	}); err == nil {
		t.Fatal("expected CRC validation to fail")
	}

	if _, err := OpenBOCView(bytes.NewReader(mutated), int64(len(mutated)), BOCViewOptions{
		RequireIndex: true,
	}); err != nil {
		t.Fatalf("CRC validation disabled should ignore trailer mismatch: %v", err)
	}
}

func testBOCViewCellTree() *Cell {
	shared := BeginCell().MustStoreUInt(0xab, 8).EndCell()
	left := BeginCell().MustStoreUInt(0x05, 3).MustStoreRef(shared).EndCell()
	right := BeginCell().MustStoreUInt(0x11, 5).MustStoreRef(shared).EndCell()
	return BeginCell().
		MustStoreUInt(0x77, 7).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
}

func assertBOCViewMatchesParsed(t *testing.T, view *BOCView, roots []*Cell, parsed []Cell) {
	t.Helper()

	if view.Cells() != uint32(len(parsed)) {
		t.Fatalf("cells count mismatch: got %d want %d", view.Cells(), len(parsed))
	}

	parsedIndex := make(map[*Cell]uint32, len(parsed))
	for i := range parsed {
		parsedIndex[&parsed[i]] = uint32(i)
	}

	viewRoots := view.Roots()
	if len(viewRoots) != len(roots) {
		t.Fatalf("roots count mismatch: got %d want %d", len(viewRoots), len(roots))
	}
	for i, root := range roots {
		if viewRoots[i] != parsedIndex[root] {
			t.Fatalf("root %d index mismatch: got %d want %d", i, viewRoots[i], parsedIndex[root])
		}
	}

	var scratch []byte
	for i := range parsed {
		viewCell, nextScratch, err := view.ReadCell(uint32(i), scratch)
		scratch = nextScratch
		if err != nil {
			t.Fatalf("load view cell %d: %v", i, err)
		}

		parsedCell := &parsed[i]
		d1, d2 := parsedCell.descriptors(parsedCell.LevelMask())
		if viewCell.D1 != d1 || viewCell.D2 != d2 {
			t.Fatalf("cell %d descriptors mismatch: got %02x/%02x want %02x/%02x", i, viewCell.D1, viewCell.D2, d1, d2)
		}
		if viewCell.Bits != uint16(parsedCell.BitsSize()) {
			t.Fatalf("cell %d bits mismatch: got %d want %d", i, viewCell.Bits, parsedCell.BitsSize())
		}

		body := make([]byte, parsedCell.SerializedBOCBodySize())
		parsedCell.SerializeBOCBodyTo(body)
		if !bytes.Equal(viewCell.Body, body) {
			t.Fatalf("cell %d body mismatch: got %x want %x", i, viewCell.Body, body)
		}

		meta := parsedCell.GetMetadata()
		if viewCell.Meta.Hash != meta.Hash || viewCell.Meta.LevelMask != meta.LevelMask {
			t.Fatalf("cell %d meta mismatch", i)
		}
		if int(viewCell.Meta.Count) != len(meta.Hashes) {
			t.Fatalf("cell %d hashes count mismatch: got %d want %d", i, viewCell.Meta.Count, len(meta.Hashes))
		}
		for j := range meta.Hashes {
			if viewCell.Meta.Hashes[j] != meta.Hashes[j] || viewCell.Meta.Depths[j] != meta.Depths[j] {
				t.Fatalf("cell %d meta[%d] mismatch", i, j)
			}
		}

		if int(viewCell.Refs.Count) != int(parsedCell.RefsNum()) {
			t.Fatalf("cell %d refs count mismatch: got %d want %d", i, viewCell.Refs.Count, parsedCell.RefsNum())
		}
		for refIdx := 0; refIdx < int(viewCell.Refs.Count); refIdx++ {
			ref, err := parsedCell.PeekRef(refIdx)
			if err != nil {
				t.Fatalf("peek parsed ref %d/%d: %v", i, refIdx, err)
			}
			if viewCell.Refs.Items[refIdx] != parsedIndex[ref] {
				t.Fatalf("cell %d ref %d mismatch: got %d want %d", i, refIdx, viewCell.Refs.Items[refIdx], parsedIndex[ref])
			}
		}
	}
}

func testBOCViewModeName(opts BOCSerializeOptions) string {
	name := ""
	if opts.WithIndex {
		name += "idx"
	} else {
		name += "noidx"
	}
	if opts.WithCRC32C {
		name += "_crc"
	}
	if opts.WithCacheBits {
		name += "_cache"
	}
	if opts.WithTopHash {
		name += "_top"
	}
	if opts.WithIntHashes {
		name += "_int"
	}
	return name
}

type parallelReaderAt struct {
	data   []byte
	target int32
	gate   chan struct{}
	once   sync.Once

	parallel  atomic.Bool
	active    atomic.Int32
	maxActive atomic.Int32
}

func (r *parallelReaderAt) ReadAt(dst []byte, offset int64) (int, error) {
	if r.parallel.Load() {
		active := r.active.Add(1)
		defer r.active.Add(-1)

		for maxActive := r.maxActive.Load(); active > maxActive; maxActive = r.maxActive.Load() {
			if r.maxActive.CompareAndSwap(maxActive, active) {
				break
			}
		}
		if active >= r.target {
			r.once.Do(func() { close(r.gate) })
		}
		<-r.gate
	}

	if offset < 0 || offset >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(dst, r.data[offset:])
	if n != len(dst) {
		return n, io.EOF
	}
	return n, nil
}
