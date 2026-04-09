package cell

import (
	"encoding/binary"
	"testing"
)

func TestProofReaderCompressionAndSliceEdgeCases(t *testing.T) {
	t.Run("ProofSkeletonAndUnwrapErrors", func(t *testing.T) {
		sk := CreateProofSkeleton()
		child := CreateProofSkeleton()
		child.SetRecursive()
		sk.AttachAt(2, child)
		if sk.branches[2] != child {
			t.Fatal("AttachAt should set branch skeleton")
		}

		cp := sk.Copy()
		if cp == sk || cp.branches[2] != child || !cp.branches[2].recursive {
			t.Fatal("Copy should preserve recursive branches")
		}

		leaf := BeginCell().MustStoreUInt(0x99, 8).EndCell()
		proofSk := CreateProofSkeleton()
		proofSk.SetRecursive()
		proof, err := leaf.CreateProof(proofSk)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = UnwrapProof(leaf, leaf.getHash(0)); err == nil {
			t.Fatal("ordinary cell should not unwrap as a merkle proof")
		}
		if _, err = UnwrapProof(proof, make([]byte, 32)); err == nil {
			t.Fatal("proof with wrong expected hash should fail")
		}

		badDepth := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: true,
			LevelMask: proof.levelMask,
			BitsSz:    proof.bitsSz,
			Data:      append([]byte(nil), proof.data...),
			Refs:      proof.refs,
		})
		badDepth.data[33] ^= 0xff
		if _, err = UnwrapProof(badDepth, proof.data[1:33]); err == nil {
			t.Fatal("proof with wrong stored depth should fail")
		}

		other := BeginCell().MustStoreUInt(0x55, 8).EndCell()
		fakeData := make([]byte, 1+32+2)
		fakeData[0] = byte(MerkleProofCellType)
		copy(fakeData[1:], other.getHash(0))
		binary.BigEndian.PutUint16(fakeData[33:], leaf.getDepth(0))
		fakeProof := FromRawUnsafe(RawUnsafeCell{
			IsSpecial: true,
			BitsSz:    280,
			Data:      fakeData,
			Refs:      []*Cell{leaf},
		})
		if _, err = UnwrapProof(fakeProof, other.getHash(0)); err == nil {
			t.Fatal("proof with mismatched underlying hash should fail")
		}
	})

	t.Run("ReaderHelpers", func(t *testing.T) {
		r := newReader([]byte{0x11, 0x22})
		if r.LeftLen() != 2 {
			t.Fatalf("unexpected initial reader length: %d", r.LeftLen())
		}
		b, err := r.ReadByte()
		if err != nil || b != 0x11 {
			t.Fatalf("unexpected ReadByte result: b=%x err=%v", b, err)
		}
		if r.LeftLen() != 1 {
			t.Fatalf("unexpected remaining reader length: %d", r.LeftLen())
		}
		if b, err = r.ReadByte(); err != nil || b != 0x22 {
			t.Fatalf("unexpected second ReadByte result: b=%x err=%v", b, err)
		}
		if _, err = r.ReadByte(); err == nil {
			t.Fatal("reader should fail when out of data")
		}
	})

	t.Run("CompressionBitHelpers", func(t *testing.T) {
		span := bitSpan{data: []byte{0b10110000}, bitOffset: 1, bitLen: 4}
		if span.Bit(-1) != 0 || span.Bit(4) != 0 {
			t.Fatal("Bit should clamp out-of-range indexes")
		}
		if sub := span.Subspan(-2, 99); sub.Len() != span.Len() {
			t.Fatalf("Subspan should clamp negative offset, got len=%d", sub.Len())
		}
		if sub := span.Subspan(2, 99); sub.Len() != 2 {
			t.Fatalf("Subspan should trim tail length, got len=%d", sub.Len())
		}
		if sub := span.Subspan(99, 1); sub.Len() != 0 {
			t.Fatalf("Subspan past the end should be empty, got len=%d", sub.Len())
		}
		if bytes := span.Bytes(); len(bytes) != 1 || bytes[0] == 0 {
			t.Fatalf("unexpected span bytes: %08b", bytes)
		}

		w := &bitWriter{}
		w.WriteUint(0b101, 3)
		w.AlignByteZero()
		if w.Len() != 8 {
			t.Fatalf("AlignByteZero should pad to full byte, got %d bits", w.Len())
		}
		if data := w.Bytes(); len(data) != 1 || data[0] != 0b10100000 {
			t.Fatalf("unexpected padded writer bytes: %08b", data)
		}

		br := newBitReader([]byte{0b10100000})
		if bit, err := br.ReadBit(); err != nil || bit != 1 {
			t.Fatalf("unexpected ReadBit result: bit=%d err=%v", bit, err)
		}
		if need, err := NeedStateForDecompression([]byte{byte(CompressionImprovedStructureLZ4WithState)}); err != nil || !need {
			t.Fatalf("unexpected stateful compression detection: need=%v err=%v", need, err)
		}
		if _, err := NeedStateForDecompression([]byte{}); err == nil {
			t.Fatal("NeedStateForDecompression should reject empty payloads")
		}
		if _, err := NeedStateForDecompression([]byte{0xff}); err == nil {
			t.Fatal("NeedStateForDecompression should reject unknown algorithms")
		}
	})

	t.Run("SliceOpsNilAndEmptyPaths", func(t *testing.T) {
		var nilSlice *Slice
		empty := mustBitSlice(t, "")
		full := mustBitSlice(t, "101")

		if !nilSlice.BitsEqual(nil) || nilSlice.BitsEqual(full) {
			t.Fatal("BitsEqual should handle nil receivers")
		}
		if !nilSlice.IsPrefixOf(full) || !nilSlice.IsSuffixOf(full) {
			t.Fatal("nil slice should be a prefix and suffix of any slice")
		}
		if nilSlice.IsProperPrefixOf(empty) || nilSlice.IsProperSuffixOf(empty) {
			t.Fatal("nil slice should not be a proper prefix/suffix of an empty slice")
		}
		if full.HasPrefix(nil) != true {
			t.Fatal("HasPrefix(nil) should always match")
		}
		if got := empty.CountLeading(false); got != 0 {
			t.Fatalf("unexpected empty CountLeading: %d", got)
		}
		if got := empty.CountTrailing(true); got != 0 {
			t.Fatalf("unexpected empty CountTrailing: %d", got)
		}

		trim := mustBitSlice(t, "101")
		if !trim.OnlyLast(2, 0) || trim.BitsLeft() != 2 || trim.RefsNum() != 0 {
			t.Fatal("OnlyLast should support slices without refs")
		}
	})
}
