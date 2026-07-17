package cell

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"math/big"
	"testing"
)

func loadReferenceFixtureRoot(tb testing.TB) (*Cell, []byte) {
	tb.Helper()

	fixture := loadBOCReferenceFixture(tb)
	rawBOC, err := base64.StdEncoding.DecodeString(fixture.RawBOCBase64)
	if err != nil {
		tb.Fatalf("failed to decode raw boc: %v", err)
	}

	root, err := FromBOC(rawBOC)
	if err != nil {
		tb.Fatalf("failed to parse raw boc: %v", err)
	}
	return root, root.ToBOCWithOptions(mode31Options())
}

func mode31Options() BOCSerializeOptions {
	return BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	}
}

func mustMerkleUpdateCell(tb testing.TB, left, right *Cell) *Cell {
	tb.Helper()

	builder := BeginCell().
		MustStoreUInt(uint64(MerkleUpdateCellType), 8).
		MustStoreSlice(left.getHash(0), hashSize*8).
		MustStoreSlice(right.getHash(0), hashSize*8).
		MustStoreUInt(uint64(left.getDepth(0)), depthSize*8).
		MustStoreUInt(uint64(right.getDepth(0)), depthSize*8).
		MustStoreRef(left).
		MustStoreRef(right)

	cell, err := finalizeCellFromBuilder(builder, true)
	if err != nil {
		tb.Fatalf("failed to build merkle update cell: %v", err)
	}
	return cell
}

func buildStateAwareCompressionFixture(tb testing.TB) (*Cell, *Cell, []byte) {
	tb.Helper()

	leftLeaf := BeginCell().MustStoreUInt(0x1234, 16).EndCell()
	left := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(leftLeaf).EndCell()

	rightLeaf := BeginCell().MustStoreUInt(0x5678, 16).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(rightLeaf).EndCell()

	mu := mustMerkleUpdateCell(tb, left, right)
	first := BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	second := BeginCell().MustStoreUInt(0xB2, 8).EndCell()
	root := BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreRef(first).
		MustStoreRef(second).
		MustStoreRef(mu).
		EndCell()

	return root, left, root.ToBOCWithOptions(mode31Options())
}

func TestDecompressWithSizeHeaderRequiresExactSize(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB, 0xCD, 0xEF, 0x01}, 64)
	compressed, err := compressWithSizeHeader(payload)
	if err != nil {
		t.Fatalf("failed to compress payload: %v", err)
	}

	out, err := decompressWithSizeHeader(compressed, len(payload)*2)
	if err != nil {
		t.Fatalf("exact-size decompression failed: %v", err)
	}
	if !bytes.Equal(out, payload) {
		t.Fatal("decompressed payload mismatch")
	}

	// A header smaller than the actual block must fail as well: accepting a
	// truncated destination would diverge from the reference exact-size check.
	deflated := append([]byte(nil), compressed...)
	binary.BigEndian.PutUint32(deflated, uint32(len(payload)-1))
	if _, err = decompressWithSizeHeader(deflated, len(payload)*2); err == nil {
		t.Fatal("expected declared size smaller than actual to fail")
	}

	// declared size larger than the actual decompressed size must fail, like
	// the reference "decompressed size mismatch" check
	inflated := append([]byte(nil), compressed...)
	binary.BigEndian.PutUint32(inflated, uint32(len(payload)+1))
	if _, err = decompressWithSizeHeader(inflated, len(payload)*2); err == nil {
		t.Fatal("expected declared size larger than actual to fail")
	}

	// declared size above the allocation limit must fail before decompression
	if _, err = decompressWithSizeHeader(compressed, len(payload)-1); err == nil {
		t.Fatal("expected declared size above the limit to fail")
	}
}

func TestDecompressBOCRejectsInflatedSizeHeader(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAABB, 16).EndCell()
	compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4, nil)
	if err != nil {
		t.Fatalf("failed to compress boc: %v", err)
	}

	if _, err = DecompressBOC(compressed, 1<<20, nil); err != nil {
		t.Fatalf("untampered decompression failed: %v", err)
	}

	// data[0] is the algorithm byte, the size header follows it
	tampered := append([]byte(nil), compressed...)
	declared := binary.BigEndian.Uint32(tampered[1 : 1+kDecompressedSizeBytes])
	binary.BigEndian.PutUint32(tampered[1:], declared+1)

	if _, err = DecompressBOC(tampered, 1<<20, nil); err == nil {
		t.Fatal("expected inflated declared size to fail decompression")
	}
}

func TestExtractBalanceFromDepthBalanceCellAcceptsEmptyExtraDict(t *testing.T) {
	grams := big.NewInt(123456789)
	left := BeginCell().MustStoreUInt(1, 1).EndCell()
	right := BeginCell().MustStoreUInt(0, 1).EndCell()
	depthBalance := BeginCell().
		MustStoreUInt(0, 7).
		MustStoreBigCoins(grams).
		MustStoreDict(nil).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()

	got := extractBalanceFromDepthBalanceCell(depthBalance)
	if got == nil {
		t.Fatal("failed to extract grams from depth-balance cell with empty extra currencies")
	}
	if got.Cmp(grams) != 0 {
		t.Fatalf("unexpected grams, got %s want %s", got, grams)
	}
}

func TestCompressBOC_BaselineLZ4_RoundTripReferenceFixture(t *testing.T) {
	root, rawBOC := loadReferenceFixtureRoot(t)

	compressed, err := CompressBOC([]*Cell{root}, CompressionBaselineLZ4, nil)
	if err != nil {
		t.Fatalf("failed to compress boc: %v", err)
	}

	needState, err := NeedStateForDecompression(compressed)
	if err != nil {
		t.Fatalf("failed to detect decompression requirements: %v", err)
	}
	if needState {
		t.Fatal("baseline compression unexpectedly requires state")
	}

	roots, err := DecompressBOC(compressed, 1<<20, nil)
	if err != nil {
		t.Fatalf("failed to decompress boc: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count, got %d want 1", len(roots))
	}

	if got := roots[0].ToBOCWithOptions(mode31Options()); !bytes.Equal(got, rawBOC) {
		t.Fatal("baseline compression roundtrip changed the reference boc")
	}
}

func TestCompressBOC_ImprovedStructureLZ4_RoundTripReferenceFixture(t *testing.T) {
	root, rawBOC := loadReferenceFixtureRoot(t)

	compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4, nil)
	if err != nil {
		t.Fatalf("failed to compress boc: %v", err)
	}

	needState, err := NeedStateForDecompression(compressed)
	if err != nil {
		t.Fatalf("failed to detect decompression requirements: %v", err)
	}
	if needState {
		t.Fatal("improved compression without state unexpectedly requires state")
	}

	roots, err := DecompressBOC(compressed, 1<<20, nil)
	if err != nil {
		t.Fatalf("failed to decompress boc: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count, got %d want 1", len(roots))
	}

	if got := roots[0].ToBOCWithOptions(mode31Options()); !bytes.Equal(got, rawBOC) {
		t.Fatal("improved compression roundtrip changed the reference boc")
	}
}

func TestCompressBOC_ImprovedStructureLZ4WithState_RoundTrip(t *testing.T) {
	root, left, wantBOC := buildStateAwareCompressionFixture(t)

	compressed, err := CompressBOC([]*Cell{root}, CompressionImprovedStructureLZ4WithState, left)
	if err != nil {
		t.Fatalf("failed to compress boc with state: %v", err)
	}

	needState, err := NeedStateForDecompression(compressed)
	if err != nil {
		t.Fatalf("failed to detect decompression requirements: %v", err)
	}
	if !needState {
		t.Fatal("state-aware compression unexpectedly does not require state")
	}

	if _, err = DecompressBOC(compressed, 1<<20, nil); err == nil {
		t.Fatal("expected decompression without state to fail")
	}

	roots, err := DecompressBOC(compressed, 1<<20, left)
	if err != nil {
		t.Fatalf("failed to decompress boc with state: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected roots count, got %d want 1", len(roots))
	}

	if got := roots[0].ToBOCWithOptions(mode31Options()); !bytes.Equal(got, wantBOC) {
		t.Fatal("state-aware compression roundtrip changed the boc")
	}
}
