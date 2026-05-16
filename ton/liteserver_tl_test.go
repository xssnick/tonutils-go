package ton

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func testBlockIDExt(seqno uint32) *BlockIDExt {
	return &BlockIDExt{
		Workchain: 0,
		Shard:     -0x8000000000000000,
		SeqNo:     seqno,
		RootHash:  bytes.Repeat([]byte{byte(seqno)}, 32),
		FileHash:  bytes.Repeat([]byte{byte(seqno + 1)}, 32),
	}
}

func testBlockInfoShort(seqno int32) *BlockInfoShort {
	return &BlockInfoShort{
		Workchain: 0,
		Shard:     -0x8000000000000000,
		Seqno:     seqno,
	}
}

func parseBoxedForTest(t *testing.T, v tl.Serializable) tl.Serializable {
	t.Helper()

	data, err := tl.Serialize(v, true)
	if err != nil {
		t.Fatalf("serialize %T: %v", v, err)
	}

	var parsed tl.Serializable
	rest, err := tl.Parse(&parsed, data, true)
	if err != nil {
		t.Fatalf("parse %T: %v", v, err)
	}
	if len(rest) != 0 {
		t.Fatalf("parse %T left %d trailing bytes", v, len(rest))
	}
	return parsed
}

func TestLiteServerTransactionIDMetadataTL(t *testing.T) {
	hash := bytes.Repeat([]byte{0x11}, 32)
	tx := TransactionID{
		Flags:   1 | 2 | 4 | 1<<8,
		Account: hash,
		LT:      123,
		Hash:    bytes.Repeat([]byte{0x22}, 32),
		Metadata: &TransactionMetadata{
			Depth:       2,
			Initiator:   AccountID{Workchain: -1, ID: bytes.Repeat([]byte{0x33}, 32)},
			InitiatorLT: 456,
		},
	}

	data, err := tl.Serialize(tx, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(data[:4]); got != 0xb12f65af {
		t.Fatalf("unexpected transactionId constructor %#x", got)
	}

	parsed := parseBoxedForTest(t, tx).(TransactionID)
	if parsed.Metadata == nil {
		t.Fatal("metadata was not parsed")
	}
	if parsed.Metadata.Depth != tx.Metadata.Depth || parsed.Metadata.InitiatorLT != tx.Metadata.InitiatorLT {
		t.Fatalf("metadata mismatch: %+v", parsed.Metadata)
	}
	if !bytes.Equal(parsed.Metadata.Initiator.ID, tx.Metadata.Initiator.ID) {
		t.Fatal("metadata initiator mismatch")
	}
}

func TestLiteServerBlockStateTL(t *testing.T) {
	state := BlockState{
		ID:       testBlockIDExt(10),
		RootHash: bytes.Repeat([]byte{0xaa}, 32),
		FileHash: bytes.Repeat([]byte{0xbb}, 32),
		Data:     []byte{1, 2, 3, 4},
	}

	parsed := parseBoxedForTest(t, state).(BlockState)
	if !bytes.Equal(parsed.RootHash, state.RootHash) || !bytes.Equal(parsed.FileHash, state.FileHash) {
		t.Fatal("state hashes mismatch")
	}
	if !bytes.Equal(parsed.Data, state.Data) {
		t.Fatal("state data mismatch")
	}
}

func TestLiteServerAdditionalTLRoundTrip(t *testing.T) {
	block := testBlockIDExt(1)
	short := testBlockInfoShort(1)
	hash := bytes.Repeat([]byte{0x44}, 32)
	lib := &LibraryEntry{Hash: hash, Data: cell.BeginCell().EndCell().ToBOCWithFlags(false)}
	candidateID := &NonfinalCandidateID{
		BlockID:          block,
		Creator:          bytes.Repeat([]byte{0x55}, 32),
		CollatedDataHash: bytes.Repeat([]byte{0x66}, 32),
	}
	candidateInfo := NonfinalCandidateInfo{
		ID:             candidateID,
		Available:      true,
		ApprovedWeight: 1,
		SignedWeight:   2,
		TotalWeight:    3,
	}

	cases := []tl.Serializable{
		LookupBlockWithProof{Mode: 1, ID: short, MCBlockID: block},
		LookupBlockResult{ID: block, Mode: 0, MCBlockID: block, ShardLinks: []ShardBlockLink{{ID: block}}},
		GetValidatorStats{Mode: 0, ID: block, Limit: 10},
		ValidatorStats{Mode: 0, ID: block, Count: 1, Complete: true},
		GetLibrariesWithProof{ID: block, LibraryList: [][]byte{hash}},
		LibraryResultWithProof{ID: block, Result: []*LibraryEntry{lib}},
		DebugVerbosity{Value: 1},
		NonfinalCandidateID{BlockID: block, Creator: candidateID.Creator, CollatedDataHash: candidateID.CollatedDataHash},
		NonfinalCandidate{ID: candidateID, Data: []byte{1}, CollatedData: []byte{2}},
		candidateInfo,
		NonfinalValidatorGroupInfo{NextBlockID: short, Prev: []*BlockIDExt{block}, Candidates: []NonfinalCandidateInfo{candidateInfo}},
		NonfinalValidatorGroups{Groups: []NonfinalValidatorGroupInfo{{NextBlockID: short}}},
		NonfinalPendingShardBlocks{SignedBlocks: []*BlockIDExt{block}, Candidates: []*BlockIDExt{block}},
		NonfinalGetValidatorGroups{},
		NonfinalGetCandidate{ID: candidateID},
		NonfinalGetPendingShardBlocks{},
	}

	for _, tc := range cases {
		if got := parseBoxedForTest(t, tc); got == nil {
			t.Fatalf("nil result for %T", tc)
		}
	}
}
