package cell

import (
	"testing"
)

// Witnesses from the 2026-08 parity sweep of the bag-of-cells reader: the
// header bounds must accept and reject exactly what the reference does.

// buildRawBOCHeader assembles a bag-of-cells with one root, one-byte cell
// indices and one-byte offsets, so tests can state the declared counters and
// payload independently of each other.
func buildRawBOCHeader(t *testing.T, cells, roots, absent, dataLen int, rootIdx byte, payload []byte) []byte {
	t.Helper()

	out := []byte{0xB5, 0xEE, 0x9C, 0x72}
	// has_idx=0, has_crc32c=0, has_cache_bits=0, flags=0, size=1
	out = append(out, 0x01)
	out = append(out, 0x01) // off_bytes
	out = append(out, byte(cells), byte(roots), byte(absent), byte(dataLen))
	out = append(out, rootIdx)
	return append(out, payload...)
}

func TestBOCHeaderDataSizeLowerBound(t *testing.T) {
	// cell descriptors: d1 = refs (plus flags), d2 = data length marker
	root1Ref := []byte{0x01, 0x00, 0x01} // one ref, no data -> child index 1
	root2Refs := []byte{0x02, 0x00, 0x01, 0x02}
	emptyLeaf := []byte{0x00, 0x00}

	// three cells whose payload is one byte below "every cell carries its
	// descriptors plus a reference slot", which the reference rejects while
	// reading the header
	short := append(append(append([]byte(nil), root1Ref...), emptyLeaf...), emptyLeaf...)
	if len(short) != 7 {
		t.Fatalf("unexpected short payload length %d", len(short))
	}
	if _, err := FromBOC(buildRawBOCHeader(t, 3, 1, 0, len(short), 0, short)); err == nil {
		t.Fatal("a payload below the per-cell lower bound must be rejected")
	}

	// the same three cells at exactly the bound parse
	atBound := append(append(append([]byte(nil), root2Refs...), emptyLeaf...), emptyLeaf...)
	if len(atBound) != 8 {
		t.Fatalf("unexpected bound payload length %d", len(atBound))
	}
	if _, err := FromBOC(buildRawBOCHeader(t, 3, 1, 0, len(atBound), 0, atBound)); err != nil {
		t.Fatalf("a payload at the lower bound must parse: %v", err)
	}
}

func TestBOCHeaderAbsentCounterBound(t *testing.T) {
	payload := []byte{0x00, 0x00} // a single empty leaf

	// the reference bounds the absent counter by the cell count alone
	if _, err := FromBOC(buildRawBOCHeader(t, 1, 1, 1, len(payload), 0, payload)); err != nil {
		t.Fatalf("absent counter equal to the cell count must be accepted: %v", err)
	}

	// above the cell count it is still rejected
	if _, err := FromBOC(buildRawBOCHeader(t, 1, 1, 2, len(payload), 0, payload)); err == nil {
		t.Fatal("absent counter above the cell count must be rejected")
	}
}

// A declared level mask that does not match the references is rejected on the
// default parsing path, matching the reference's cell checker.
func TestBOCRejectsMismatchedLevelMask(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(1, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()
	boc := root.ToBOCWithFlags(false)

	// locate the root cell descriptor and claim level 1 for an ordinary cell
	idx := -1
	for i := 0; i+1 < len(boc); i++ {
		if boc[i] == 0x01 && boc[i+1] == 0x00 {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("could not locate the root descriptor in the serialized bag")
	}
	tampered := append([]byte(nil), boc...)
	tampered[idx] |= 0x20 // level mask 1 in the high bits of d1

	if _, err := FromBOC(tampered); err == nil {
		t.Fatal("a level mask that does not match the references must be rejected")
	}
}

func TestBOCHeaderIndexTailIsIgnored(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell()

	withIndex := root.ToBOCWithFlags(false, true)
	if _, err := FromBOC(withIndex); err != nil {
		t.Fatalf("indexed bag must parse: %v", err)
	}
}
