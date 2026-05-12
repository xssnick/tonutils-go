package cell

import (
	"bytes"
	"errors"
	"io"
	"math/big"
	"testing"
)

type oneByteReader struct {
	data []byte
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestBOCPayloadArenaChunkSizing(t *testing.T) {
	small := newBOCPayloadArena(3)
	smallPayload := small.alloc(1)
	if cap(small.current) != 3 {
		t.Fatalf("small arena chunk should stay bounded by input size: got cap %d", cap(small.current))
	}
	if cap(smallPayload) != len(smallPayload) {
		t.Fatalf("payload slice capacity should match length: got cap %d len %d", cap(smallPayload), len(smallPayload))
	}

	large := newBOCPayloadArena(bocPayloadArenaMinChunkSize * 4)
	firstPayload := large.alloc(1)
	if cap(large.current) != bocPayloadArenaMinChunkSize {
		t.Fatalf("large arena should start with min chunk: got cap %d", cap(large.current))
	}
	if cap(firstPayload) != len(firstPayload) {
		t.Fatalf("payload slice capacity should match length: got cap %d len %d", cap(firstPayload), len(firstPayload))
	}

	if bocPayloadArenaMaxChunkSize != 128<<20 {
		t.Fatalf("unexpected max arena chunk size: got %d", bocPayloadArenaMaxChunkSize)
	}
}

func TestParseCellsRejectsIndexedBoundaryOverflow(t *testing.T) {
	boc := BeginCell().MustStoreUInt(0xAB, 8).EndCell().ToBOCWithOptions(BOCSerializeOptions{WithIndex: true})
	if len(boc) < 12 {
		t.Fatalf("unexpected short indexed boc: %d", len(boc))
	}

	corrupted := append([]byte{}, boc...)
	corrupted[11] = 0xFF

	if _, err := FromBOC(corrupted); err == nil {
		t.Fatal("expected indexed boc with out-of-bounds offset to fail")
	}
	if _, err := FromBOCMultiRoot(corrupted); err == nil {
		t.Fatal("expected indexed boc with out-of-bounds offset to fail for multi-root parser too")
	}
}

func TestParseCellsRejectsIndexedCycles(t *testing.T) {
	boc := []byte{
		0xB5, 0xEE, 0x9C, 0x72,
		0x81, 0x01,
		0x02, 0x01, 0x00, 0x06,
		0x00,
		0x03, 0x06,
		0x01, 0x00, 0x01,
		0x01, 0x00, 0x00,
	}

	if _, err := FromBOC(boc); err == nil {
		t.Fatal("expected cyclic indexed boc to fail")
	}
}

func TestParseCellsRejectsIndexedBackwardRefs(t *testing.T) {
	boc := []byte{
		0xB5, 0xEE, 0x9C, 0x72,
		0x81, 0x01,
		0x02, 0x01, 0x00, 0x05,
		0x01,
		0x02, 0x05,
		0x00, 0x00,
		0x01, 0x00, 0x00,
	}

	if _, err := FromBOC(boc); err == nil {
		t.Fatal("expected indexed backward ref to fail")
	}
}

func TestFromBOCRejectsMultipleRoots(t *testing.T) {
	roots := []*Cell{
		BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
	}

	if _, err := FromBOC(ToBOCWithOptions(roots, BOCSerializeOptions{})); err == nil {
		t.Fatal("expected single-root parser to reject multiple roots")
	}
}

func TestFromBOCRejectsWrongCacheBits(t *testing.T) {
	boc := BeginCell().MustStoreUInt(0xAB, 8).EndCell().ToBOCWithOptions(BOCSerializeOptions{
		WithIndex:     true,
		WithCacheBits: true,
	})
	if len(boc) == 0 {
		t.Fatal("expected non-empty boc")
	}

	refByteSize := int(boc[4] & 0b111)
	offsetByteSize := int(boc[5])
	rootsNum := dynInt(boc[6+refByteSize : 6+2*refByteSize])
	rootsOffset := 6 + 3*refByteSize + offsetByteSize
	indexOffset := rootsOffset + rootsNum*refByteSize

	corrupted := append([]byte{}, boc...)
	corrupted[indexOffset+offsetByteSize-1] ^= 1

	if _, err := FromBOC(corrupted); err == nil {
		t.Fatal("expected wrong cache bit to fail")
	}
}

func TestFromBOCRejectsOverlongPartialByteEncoding(t *testing.T) {
	tests := []struct {
		name string
		tail byte
	}{
		{name: "missing-terminator", tail: 0x00},
		{name: "terminator-only", tail: 0x80},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			boc := []byte{
				0xB5, 0xEE, 0x9C, 0x72,
				0x01, 0x01,
				0x01, 0x01, 0x00, 0x03,
				0x00,
				0x00, 0x01, tc.tail,
			}

			if _, err := FromBOC(boc); err == nil {
				t.Fatal("expected overlong partial-byte encoding to fail")
			}
		})
	}
}

func TestFromBOCAcceptsOrdinaryLevelMaskMismatchForCompat(t *testing.T) {
	boc := []byte{
		0xB5, 0xEE, 0x9C, 0x72,
		0x01, 0x01,
		0x02, 0x01, 0x00, 0x05,
		0x00,
		0x21, 0x00, 0x01,
		0x00, 0x00,
	}

	if _, err := FromBOC(boc); err != nil {
		t.Fatalf("generic BOC parser should accept ordinary level masks without an allow_nonzero_level API split: %v", err)
	}
}

func TestFromBOCCopiesCellPayload(t *testing.T) {
	tests := []struct {
		name string
		opts BOCSerializeOptions
	}{
		{name: "sequential", opts: BOCSerializeOptions{}},
		{name: "indexed", opts: BOCSerializeOptions{WithIndex: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boc := BeginCell().MustStoreUInt(0xABCD, 16).EndCell().ToBOCWithOptions(tt.opts)
			c, err := FromBOC(boc)
			if err != nil {
				t.Fatal(err)
			}

			wantData := append([]byte{}, c.data...)
			for i := range boc {
				boc[i] ^= 0xFF
			}

			if !bytes.Equal(c.data, wantData) {
				t.Fatal("parsed cell data should not alias source boc")
			}

			value, err := c.MustBeginParse().LoadUInt(16)
			if err != nil {
				t.Fatal(err)
			}
			if value != 0xABCD {
				t.Fatalf("unexpected parsed value after source boc mutation: %x", value)
			}
		})
	}
}

func TestFromBOCMultiRootReaderNoCopyPayloadSharesInput(t *testing.T) {
	boc := BeginCell().MustStoreUInt(0x1234, 16).EndCell().ToBOCWithOptions(BOCSerializeOptions{
		WithCRC32C: true,
		WithIndex:  true,
	})

	roots, unique, err := FromBOCMultiRootReader(NewBOCNoCopyReader(boc), BOCParseOptions{NoCopyPayload: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if len(unique) != 1 {
		t.Fatalf("expected 1 unique cell, got %d", len(unique))
	}

	value, err := roots[0].MustBeginParse().LoadUInt(16)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0x1234 {
		t.Fatalf("unexpected parsed value: %x", value)
	}

	dataOffset := bytes.Index(boc, []byte{0x12, 0x34})
	if dataOffset < 0 {
		t.Fatal("serialized cell payload not found")
	}
	boc[dataOffset] = 0xAB
	boc[dataOffset+1] = 0xCD

	value, err = roots[0].MustBeginParse().LoadUInt(16)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0xABCD {
		t.Fatalf("parsed cell data should alias source boc with NoCopyPayload, got %x", value)
	}
}

func TestFromBOCMultiRootReaderHandlesShortReads(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := BeginCell().MustStoreUInt(0xCDEF, 16).MustStoreRef(leaf).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithCRC32C: true,
		WithIndex:  true,
	})

	roots, unique, err := FromBOCMultiRootReader(&oneByteReader{data: boc}, BOCParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if len(unique) != 2 {
		t.Fatalf("expected 2 unique cells, got %d", len(unique))
	}
	if roots[0] != &unique[0] {
		t.Fatal("parsed root should point into unique cells")
	}
	if roots[0].refs[0] != &unique[1] {
		t.Fatal("parsed ref should point into unique cells")
	}
	if roots[0].HashKey() != root.HashKey() {
		t.Fatal("parsed root hash mismatch")
	}
}

func TestFromBOCMultiRootReaderAcceptsBOCNoCopyReader(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	root := BeginCell().MustStoreUInt(0xBEEF, 16).MustStoreRef(leaf).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{
		WithCRC32C: true,
		WithIndex:  true,
	})

	reader := NewBOCNoCopyReader(boc)
	roots, unique, err := FromBOCMultiRootReader(reader, BOCParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || len(unique) != 2 {
		t.Fatalf("unexpected parsed cells: roots=%d unique=%d", len(roots), len(unique))
	}
	if roots[0].HashKey() != root.HashKey() {
		t.Fatal("parsed root hash mismatch")
	}
	if reader.Len() != 0 {
		t.Fatalf("reader should be consumed, %d bytes left", reader.Len())
	}
}

func TestFromBOCMultiRootReaderTrustedHashes(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	boc := root.ToBOCWithOptions(BOCSerializeOptions{WithTopHash: true})
	cellOffset := firstBOCCellOffset(t, boc)
	if boc[cellOffset]&0b10000 == 0 {
		t.Fatal("expected serialized root hashes")
	}

	mutated := append([]byte(nil), boc...)
	trustedHash := bytes.Repeat([]byte{0x77}, hashSize)
	copy(mutated[cellOffset+2:cellOffset+2+hashSize], trustedHash)

	if _, _, err := FromBOCMultiRootReader(bytes.NewReader(mutated), BOCParseOptions{}); err == nil {
		t.Fatal("expected default parser to reject mismatched serialized hash")
	}

	roots, _, err := FromBOCMultiRootReader(bytes.NewReader(mutated), BOCParseOptions{TrustedHashes: true})
	if err != nil {
		t.Fatalf("trusted parse failed: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count: %d", len(roots))
	}
	if !bytes.Equal(roots[0].Hash(), trustedHash) {
		t.Fatalf("trusted hash was not applied: got=%x want=%x", roots[0].Hash(), trustedHash)
	}
}

func firstBOCCellOffset(tb testing.TB, boc []byte) int {
	tb.Helper()

	if len(boc) < 6 || !matchBOCMagic(boc[:4], bocMagic) {
		tb.Fatal("invalid test boc")
	}

	flags, cellNumSizeBytes := parseBOCFlags(boc[4])
	dataSizeBytes := int(boc[5])
	pos := 6
	read := func(size int) int {
		if pos+size > len(boc) {
			tb.Fatal("invalid test boc")
		}
		value := dynInt(boc[pos : pos+size])
		pos += size
		return value
	}

	cellsNum := read(cellNumSizeBytes)
	rootsNum := read(cellNumSizeBytes)
	read(cellNumSizeBytes)
	read(dataSizeBytes)
	pos += rootsNum * cellNumSizeBytes
	if flags.hasIndex {
		pos += cellsNum * dataSizeBytes
	}
	if pos >= len(boc) {
		tb.Fatal("invalid test boc")
	}
	return pos
}

func TestBuilderRejectsOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "UIntWidth", err: BeginCell().StoreUInt(256, 8)},
		{name: "IntPositiveWidth", err: BeginCell().StoreInt(128, 8)},
		{name: "IntOneBitPositive", err: BeginCell().StoreInt(1, 1)},
		{name: "BigUIntWidth", err: BeginCell().StoreBigUInt(new(big.Int).Lsh(big.NewInt(1), 8), 8)},
		{name: "BigIntPositiveWidth", err: BeginCell().StoreBigInt(new(big.Int).Lsh(big.NewInt(1), 8), 8)},
		{name: "BigIntNegativeWidth", err: BeginCell().StoreBigInt(big.NewInt(-129), 8)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, ErrTooBigValue) {
				t.Fatalf("expected ErrTooBigValue, got %v", tc.err)
			}
		})
	}
}

func TestBuilderStoreBigValuesRejectNil(t *testing.T) {
	if err := BeginCell().StoreBigVarUInt(nil, 4); err != ErrNilBigInt {
		t.Fatalf("expected ErrNilBigInt from StoreBigVarUInt, got %v", err)
	}
	if err := BeginCell().StoreBigUInt(nil, 4); err != ErrNilBigInt {
		t.Fatalf("expected ErrNilBigInt from StoreBigUInt, got %v", err)
	}
	if err := BeginCell().StoreBigInt(nil, 4); err != ErrNilBigInt {
		t.Fatalf("expected ErrNilBigInt from StoreBigInt, got %v", err)
	}
}

func TestStoreBigIntDoesNotMutateInput(t *testing.T) {
	value := big.NewInt(-5)
	before := new(big.Int).Set(value)

	if err := BeginCell().StoreBigInt(value, 8); err != nil {
		t.Fatalf("failed to store signed big int: %v", err)
	}
	if value.Cmp(before) != 0 {
		t.Fatalf("StoreBigInt mutated input: got %v want %v", value, before)
	}
}

func TestStoreBigIntSupportsSigned257Boundary(t *testing.T) {
	min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))

	cell := BeginCell().MustStoreBigInt(min, 257).EndCell()
	got, err := cell.MustBeginParse().LoadBigInt(257)
	if err != nil {
		t.Fatalf("failed to load signed 257-bit value: %v", err)
	}
	if got.Cmp(min) != 0 {
		t.Fatalf("unexpected signed 257-bit roundtrip: got %v want %v", got, min)
	}
}

func TestSliceRejectsNarrowLoadOverflow(t *testing.T) {
	tooBigUInt := BeginCell().MustStoreBigUInt(new(big.Int).Lsh(big.NewInt(1), 64), 65).EndCell().MustBeginParse()
	beforeUInt := tooBigUInt.BitsLeft()
	if _, err := tooBigUInt.LoadUInt(65); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from LoadUInt, got %v", err)
	}
	if tooBigUInt.BitsLeft() != beforeUInt {
		t.Fatalf("LoadUInt overflow advanced slice: before=%d after=%d", beforeUInt, tooBigUInt.BitsLeft())
	}
	if _, err := tooBigUInt.PreloadUInt(65); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from PreloadUInt, got %v", err)
	}

	tooBigInt := BeginCell().MustStoreBigInt(new(big.Int).Lsh(big.NewInt(1), 63), 65).EndCell().MustBeginParse()
	beforeInt := tooBigInt.BitsLeft()
	if _, err := tooBigInt.LoadInt(65); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from LoadInt, got %v", err)
	}
	if tooBigInt.BitsLeft() != beforeInt {
		t.Fatalf("LoadInt overflow advanced slice: before=%d after=%d", beforeInt, tooBigInt.BitsLeft())
	}

	tooBigCoins := BeginCell().MustStoreBigCoins(new(big.Int).Lsh(big.NewInt(1), 64)).EndCell().MustBeginParse()
	beforeCoins := tooBigCoins.BitsLeft()
	if _, err := tooBigCoins.LoadCoins(); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from LoadCoins, got %v", err)
	}
	if tooBigCoins.BitsLeft() != beforeCoins {
		t.Fatalf("LoadCoins overflow advanced slice: before=%d after=%d", beforeCoins, tooBigCoins.BitsLeft())
	}
}

func TestSliceLoadBigIntZeroBits(t *testing.T) {
	s := BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()

	before := s.BitsLeft()
	got, err := s.LoadBigInt(0)
	if err != nil {
		t.Fatalf("failed to load zero-bit big int: %v", err)
	}
	if got.Sign() != 0 {
		t.Fatalf("expected zero-bit big int to be zero, got %v", got)
	}
	if s.BitsLeft() != before {
		t.Fatalf("zero-bit LoadBigInt advanced slice: before=%d after=%d", before, s.BitsLeft())
	}

	got, err = s.PreloadBigInt(0)
	if err != nil {
		t.Fatalf("failed to preload zero-bit big int: %v", err)
	}
	if got.Sign() != 0 {
		t.Fatalf("expected zero-bit preloaded big int to be zero, got %v", got)
	}
	if s.BitsLeft() != before {
		t.Fatalf("zero-bit PreloadBigInt advanced slice: before=%d after=%d", before, s.BitsLeft())
	}
}

func TestBuilderStoreSliceNearCapacityUnaligned(t *testing.T) {
	b := BeginCell().MustStoreSlice(make([]byte, 128), 1020)
	if err := b.StoreSlice([]byte{0x80}, 1); err != nil {
		t.Fatalf("failed to append final unaligned bit: %v", err)
	}
	if bits := b.BitsUsed(); bits != 1021 {
		t.Fatalf("unexpected builder bits: got %d, want 1021", bits)
	}

	s := b.EndCell().MustBeginParse()
	if err := s.SkipBits(1020); err != nil {
		t.Fatalf("failed to skip prefix bits: %v", err)
	}
	bit, err := s.LoadUInt(1)
	if err != nil {
		t.Fatalf("failed to read appended bit: %v", err)
	}
	if bit != 1 {
		t.Fatalf("unexpected appended bit: got %d, want 1", bit)
	}
}

func TestSliceLoadVarUIntRejectsInvalidLengthEncoding(t *testing.T) {
	s := BeginCell().MustStoreUInt(10, 4).EndCell().MustBeginParse()
	if _, err := s.LoadVarUInt(10); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from invalid varuint length, got %v", err)
	}
}

func TestToBOCWithTopHashChangesEncoding(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	plain := root.ToBOCWithOptions(BOCSerializeOptions{WithIndex: true})
	withTopHash := root.ToBOCWithOptions(BOCSerializeOptions{
		WithIndex:   true,
		WithTopHash: true,
	})

	if bytes.Equal(plain, withTopHash) {
		t.Fatal("expected top-hash mode to change serialized boc")
	}

	parsedPlain, err := FromBOC(plain)
	if err != nil {
		t.Fatalf("failed to parse plain boc: %v", err)
	}
	parsedWithTopHash, err := FromBOC(withTopHash)
	if err != nil {
		t.Fatalf("failed to parse top-hash boc: %v", err)
	}
	if !bytes.Equal(parsedPlain.Hash(), parsedWithTopHash.Hash()) {
		t.Fatalf("top-hash mode changed root hash: plain=%x top=%x", parsedPlain.Hash(), parsedWithTopHash.Hash())
	}
}

func TestCalculateHashesSafeRejectsTooDeepCells(t *testing.T) {
	root := BeginCell().EndCell()
	for i := 0; i < maxDepth; i++ {
		root = BeginCell().MustStoreRef(root).EndCell()
	}

	overflow := &Cell{}
	overflow.setRef(0, root)
	overflow.setRefsCount(1)
	overflow.setLevelMask(ordinaryLevelMask(overflow.rawRefs()))

	if err := overflow.calculateHashes(); !errors.Is(err, ErrCellDepthLimit) {
		t.Fatalf("expected ErrCellDepthLimit, got %v", err)
	}
}

func TestFromBOCRejectsConfiguredRootLimit(t *testing.T) {
	prev := MaxBOCRoots
	MaxBOCRoots = 1
	defer func() {
		MaxBOCRoots = prev
	}()

	roots := []*Cell{
		BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
	}

	if _, err := FromBOCMultiRoot(ToBOCWithOptions(roots, BOCSerializeOptions{})); err == nil {
		t.Fatal("expected root-limit violation to fail")
	}
}

func TestFromBOCRejectsConfiguredCellLimit(t *testing.T) {
	prev := MaxBOCCells
	MaxBOCCells = 1
	defer func() {
		MaxBOCCells = prev
	}()

	root := BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xCD, 8).EndCell()).
		EndCell()

	if _, err := FromBOC(root.ToBOCWithOptions(BOCSerializeOptions{})); err == nil {
		t.Fatal("expected cell-limit violation to fail")
	}
}

func TestFromBOCRejectsTrailingDataAfterPayload(t *testing.T) {
	boc := BeginCell().MustStoreUInt(0xAB, 8).EndCell().ToBOCWithOptions(BOCSerializeOptions{})
	boc = append(boc, 0x00, 0x01)

	if _, err := FromBOC(boc); err == nil {
		t.Fatal("expected trailing bytes after payload to fail")
	}
}

func TestStoreBinarySnakeRejectsDepthOverflow(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 127*(maxDepth+1)+1)

	if err := BeginCell().StoreBinarySnake(data); !errors.Is(err, ErrCellDepthLimit) {
		t.Fatalf("expected ErrCellDepthLimit, got %v", err)
	}
}
