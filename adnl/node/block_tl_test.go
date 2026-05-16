package node

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/ton"
)

func testBlockIDExt(seqno uint32) ton.BlockIDExt {
	return ton.BlockIDExt{
		Workchain: 0,
		Shard:     int64(-1 << 63),
		SeqNo:     seqno,
		RootHash:  bytes.Repeat([]byte{0x11}, 32),
		FileHash:  bytes.Repeat([]byte{0x22}, 32),
	}
}

func parseBoxedForTest(t *testing.T, obj tl.Serializable) any {
	t.Helper()

	raw, err := tl.Serialize(obj, true)
	if err != nil {
		t.Fatalf("serialize %T: %v", obj, err)
	}

	var parsed any
	if _, err = tl.Parse(&parsed, raw, true); err != nil {
		t.Fatalf("parse %T: %v", obj, err)
	}
	return parsed
}

func TestNodeAdditionalTLRoundTrip(t *testing.T) {
	block := testBlockIDExt(1)
	signature := BlockSignature{
		Who:       bytes.Repeat([]byte{0x33}, 32),
		Signature: []byte{0x44, 0x55},
	}
	signatureSet := SignatureSetOrdinary{
		CatchainSeqno:    7,
		ValidatorSetHash: 8,
		Signatures:       []BlockSignature{signature},
	}
	candidateHash := ton.ConsensusCandidateHashDataOrdinary{
		Block:            block,
		CollatedFileHash: bytes.Repeat([]byte{0x66}, 32),
		Parent:           ton.ConsensusCandidateWithoutParents{},
	}

	cases := []tl.Serializable{
		SessionID{Workchain: 0, Shard: int64(-1 << 63), CCSeqno: 7, OptsHash: bytes.Repeat([]byte{0x05}, 32)},
		ShardID{Workchain: 0, Shard: int64(-1 << 63)},
		PrivateBlockOverlayID{ZeroStateFileHash: bytes.Repeat([]byte{0x01}, 32), Nodes: [][]byte{bytes.Repeat([]byte{0x02}, 32)}},
		CustomOverlayID{ZeroStateFileHash: bytes.Repeat([]byte{0x01}, 32), Name: "test", Nodes: [][]byte{bytes.Repeat([]byte{0x02}, 32)}},
		FastSyncOverlayID{ZeroStateFileHash: bytes.Repeat([]byte{0x01}, 32), Shard: ShardID{Workchain: 0, Shard: int64(-1 << 63)}},
		ConsensusOverlayID{SessionID: bytes.Repeat([]byte{0x03}, 32), Nodes: [][]byte{bytes.Repeat([]byte{0x04}, 32)}},
		signatureSet,
		SignatureSetSimplex{
			Final:            true,
			CatchainSeqno:    7,
			ValidatorSetHash: 8,
			Signatures:       []BlockSignature{signature},
			SessionID:        bytes.Repeat([]byte{0x77}, 32),
			Slot:             9,
			Candidate:        candidateHash,
		},
		BlockBroadcastCompressed{ID: block, CatchainSeqno: 7, ValidatorSetHash: 8, Compressed: []byte{0x01}},
		BlockBroadcastCompressedData{Signatures: []BlockSignature{signature}, ProofData: []byte{0x02}},
		BlockBroadcastCompressedV2{
			ID:             block,
			SignatureSet:   signatureSet,
			Proof:          []byte{0x03},
			DataCompressed: []byte{0x04},
		},
		NewBlockCandidateBroadcast{ID: block, CatchainSeqno: 7, ValidatorSetHash: 8, CollatorSignature: signature, Data: []byte{0x05}},
		NewBlockCandidateBroadcastCompressed{ID: block, CatchainSeqno: 7, ValidatorSetHash: 8, CollatorSignature: signature, Compressed: []byte{0x06}},
		NewBlockCandidateBroadcastCompressedV2{ID: block, CatchainSeqno: 7, ValidatorSetHash: 8, CollatorSignature: signature, Compressed: []byte{0x07}},
	}

	for _, tc := range cases {
		if got := parseBoxedForTest(t, tc); got == nil {
			t.Fatalf("nil result for %T", tc)
		}
	}
}
