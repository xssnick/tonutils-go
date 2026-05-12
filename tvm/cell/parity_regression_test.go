package cell

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func convertIndexedSingleRootBOCToLegacy(tb testing.TB, generic []byte, magic []byte) []byte {
	tb.Helper()

	if len(generic) < 6 {
		tb.Fatalf("generic boc too short: %d", len(generic))
	}

	refByteSize := int(generic[4] & 0b111)
	if generic[4]&(1<<7) == 0 || refByteSize < 1 || refByteSize > 4 {
		tb.Fatalf("generic boc is not single-root indexed format: flags=%08b", generic[4])
	}

	offsetByteSize := int(generic[5])
	headerSize := 6 + 3*refByteSize + offsetByteSize
	if len(generic) < headerSize+refByteSize {
		tb.Fatalf("generic boc missing root index")
	}

	legacy := make([]byte, 0, len(generic)-refByteSize)
	legacy = append(legacy, magic...)
	legacy = append(legacy, byte(refByteSize))
	legacy = append(legacy, generic[5:headerSize]...)
	legacy = append(legacy, generic[headerSize+refByteSize:]...)

	if bytes.Equal(magic, bocIdxCRC32CMagic) {
		if len(legacy) < 4 {
			tb.Fatalf("legacy boc too short for crc")
		}
		crc := crc32.Checksum(legacy[:len(legacy)-4], castTable)
		binary.LittleEndian.PutUint32(legacy[len(legacy)-4:], crc)
	}

	return legacy
}

func TestFromBOCLegacyIndexedMagics(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(leaf).EndCell()

	tests := []struct {
		name  string
		magic []byte
		opts  BOCSerializeOptions
	}{
		{
			name:  "legacy-index",
			magic: bocIdxMagic,
			opts:  BOCSerializeOptions{WithIndex: true},
		},
		{
			name:  "legacy-index-crc32c",
			magic: bocIdxCRC32CMagic,
			opts: BOCSerializeOptions{
				WithIndex:  true,
				WithCRC32C: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			generic := root.ToBOCWithOptions(tc.opts)
			legacy := convertIndexedSingleRootBOCToLegacy(t, generic, tc.magic)

			parsed, err := FromBOC(legacy)
			if err != nil {
				t.Fatalf("failed to parse legacy boc: %v", err)
			}

			if !bytes.Equal(parsed.Hash(), root.Hash()) {
				t.Fatalf("root hash mismatch after legacy parse")
			}

			if got := parsed.ToBOCWithOptions(tc.opts); !bytes.Equal(got, generic) {
				t.Fatalf("legacy parse did not normalize back to generic indexed boc")
			}
		})
	}
}

func TestSliceLoadAddrRejectsInvalidAnycastDepth(t *testing.T) {
	tests := []struct {
		name  string
		vars  bool
		depth uint64
	}{
		{name: "std-zero", depth: 0},
		{name: "std-too-large", depth: 31},
		{name: "var-zero", vars: true, depth: 0},
		{name: "var-too-large", vars: true, depth: 31},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := BeginCell()
			if tc.vars {
				b.MustStoreUInt(0b11, 2)
			} else {
				b.MustStoreUInt(0b10, 2)
			}
			b.MustStoreUInt(1, 1)
			b.MustStoreUInt(tc.depth, 5)
			if tc.depth > 0 {
				b.MustStoreSlice(make([]byte, 4), uint(tc.depth))
			}
			if tc.vars {
				b.MustStoreUInt(32, 9)
				b.MustStoreInt(-1, 32)
				b.MustStoreSlice([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 32)
			} else {
				b.MustStoreUInt(0xFF, 8)
				b.MustStoreSlice(make([]byte, 32), 256)
			}

			if _, err := b.EndCell().MustBeginParse().LoadAddr(); err == nil {
				t.Fatalf("expected invalid anycast depth %d to be rejected", tc.depth)
			}
		})
	}
}

func TestCreateProofPrunesOmittedLeafRef(t *testing.T) {
	left := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	root := BeginCell().MustStoreUInt(0x33, 8).MustStoreRef(left).MustStoreRef(right).EndCell()

	sk := CreateProofSkeleton()
	sk.ProofRef(0).SetRecursive()

	proof, err := root.CreateProof(sk)
	if err != nil {
		t.Fatalf("failed to create proof: %v", err)
	}

	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("failed to unwrap proof: %v", err)
	}

	kept, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("failed to load kept ref: %v", err)
	}
	if kept.IsSpecial() {
		t.Fatalf("included leaf ref should stay ordinary")
	}

	pruned, err := body.PeekRef(1)
	if err != nil {
		t.Fatalf("failed to load pruned ref: %v", err)
	}
	if !pruned.IsSpecial() || pruned.GetType() != PrunedCellType {
		t.Fatalf("omitted leaf ref should be pruned, got special=%v type=%v", pruned.IsSpecial(), pruned.GetType())
	}
	if !bytes.Equal(pruned.Hash(0), right.Hash()) {
		t.Fatalf("pruned ref must preserve omitted leaf hash")
	}
}
