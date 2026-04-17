package cell

import "testing"

func TestFromBOCMultiRoot_RoundTripsWithHashesAfterCanonicalizingTerminatorOnlyTail(t *testing.T) {
	raw := []byte{
		0xB5, 0xEE, 0x9C, 0x72,
		0x81, 0x01,
		0x02, 0x01, 0x00, 0x06,
		0x00,
		0x03, 0x06,
		0x41, 0x00, 0x01,
		0x20, 0x01, 0x80,
	}

	roots, err := FromBOCMultiRoot(raw)
	if err != nil {
		t.Fatalf("parse original boc: %v", err)
	}

	opts := BOCOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	}
	reboc := ToBOCWithOptions(roots, opts)
	if len(reboc) == 0 {
		t.Fatal("expected canonical boc with hashes")
	}

	reparsed, err := FromBOCMultiRoot(reboc)
	if err != nil {
		t.Fatalf("parse canonical boc with hashes: %v", err)
	}
	if len(reparsed) != 1 {
		t.Fatalf("expected one root after round-trip, got %d", len(reparsed))
	}
}
