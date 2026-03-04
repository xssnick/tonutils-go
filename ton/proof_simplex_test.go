package ton

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
)

func makeSimplexValidator(t *testing.T, seedByte byte, weight uint64) (*tlb.ValidatorAddr, ed25519.PrivateKey, []byte) {
	t.Helper()

	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	nodeID, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		t.Fatalf("failed to build node id short: %v", err)
	}

	return &tlb.ValidatorAddr{
		PublicKey: tlb.SigPubKeyED25519{Key: pub},
		Weight:    weight,
		ADNLAddr:  bytes.Repeat([]byte{seedByte + 1}, 32),
	}, priv, nodeID
}

func buildSimplexCandidate(t *testing.T, block *BlockIDExt) []byte {
	t.Helper()

	candidate, err := tl.Serialize(ConsensusCandidateHashDataOrdinary{
		Block:            *block,
		CollatedFileHash: bytes.Repeat([]byte{0x77}, 32),
		Parent:           ConsensusCandidateWithoutParents{},
	}, true)
	if err != nil {
		t.Fatalf("failed to build simplex candidate: %v", err)
	}
	return candidate
}

func TestCheckBlockSignaturesSimplex_Valid(t *testing.T) {
	v1, pk1, id1 := makeSimplexValidator(t, 0x11, 40)
	v2, pk2, id2 := makeSimplexValidator(t, 0x22, 35)
	v3, _, _ := makeSimplexValidator(t, 0x33, 25)
	validators := []*tlb.ValidatorAddr{v1, v2, v3}

	const ccSeqno uint32 = 17
	setHash, err := calcValidatorSetHash(ccSeqno, validators)
	if err != nil {
		t.Fatalf("failed to calc validator set hash: %v", err)
	}

	block := &BlockIDExt{
		Workchain: -1,
		Shard:     -9223372036854775808,
		SeqNo:     100500,
		RootHash:  bytes.Repeat([]byte{0x41}, 32),
		FileHash:  bytes.Repeat([]byte{0x42}, 32),
	}
	sessionID := bytes.Repeat([]byte{0x51}, 32)
	const slot int32 = 77
	candidate := buildSimplexCandidate(t, block)

	toSign, err := buildSimplexToSignPayload(block, sessionID, slot, candidate)
	if err != nil {
		t.Fatalf("failed to build simplex payload: %v", err)
	}

	sigs := []Signature{
		{NodeIDShort: id1, Signature: ed25519.Sign(pk1, toSign)},
		{NodeIDShort: id2, Signature: ed25519.Sign(pk2, toSign)},
	}

	if err = CheckBlockSignaturesSimplex(block, ccSeqno, setHash, sessionID, slot, candidate, sigs, validators); err != nil {
		t.Fatalf("simplex signatures check failed: %v", err)
	}
}

func TestCheckBlockSignaturesSimplex_RejectsBlockIDPayload(t *testing.T) {
	v1, pk1, id1 := makeSimplexValidator(t, 0x10, 40)
	v2, pk2, id2 := makeSimplexValidator(t, 0x20, 35)
	v3, _, _ := makeSimplexValidator(t, 0x30, 25)
	validators := []*tlb.ValidatorAddr{v1, v2, v3}

	const ccSeqno uint32 = 8
	setHash, err := calcValidatorSetHash(ccSeqno, validators)
	if err != nil {
		t.Fatalf("failed to calc validator set hash: %v", err)
	}

	block := &BlockIDExt{
		Workchain: -1,
		Shard:     -9223372036854775808,
		SeqNo:     42,
		RootHash:  bytes.Repeat([]byte{0x31}, 32),
		FileHash:  bytes.Repeat([]byte{0x32}, 32),
	}
	sessionID := bytes.Repeat([]byte{0x61}, 32)
	const slot int32 = 33
	candidate := buildSimplexCandidate(t, block)

	legacyPayload, err := tl.Serialize(BlockID{RootHash: block.RootHash, FileHash: block.FileHash}, true)
	if err != nil {
		t.Fatalf("failed to serialize block id payload: %v", err)
	}

	sigs := []Signature{
		{NodeIDShort: id1, Signature: ed25519.Sign(pk1, legacyPayload)},
		{NodeIDShort: id2, Signature: ed25519.Sign(pk2, legacyPayload)},
	}

	err = CheckBlockSignaturesSimplex(block, ccSeqno, setHash, sessionID, slot, candidate, sigs, validators)
	if err == nil {
		t.Fatal("expected simplex check to fail for legacy block-id payload")
	}
	if !strings.Contains(err.Error(), "incorrect signature") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildSimplexToSignPayload_CandidateBlockMismatch(t *testing.T) {
	block := &BlockIDExt{
		Workchain: -1,
		Shard:     -9223372036854775808,
		SeqNo:     100,
		RootHash:  bytes.Repeat([]byte{0x21}, 32),
		FileHash:  bytes.Repeat([]byte{0x22}, 32),
	}

	wrongCandidateBlock := *block
	wrongCandidateBlock.SeqNo++

	candidate, err := tl.Serialize(ConsensusCandidateHashDataOrdinary{
		Block:            wrongCandidateBlock,
		CollatedFileHash: bytes.Repeat([]byte{0x77}, 32),
		Parent:           ConsensusCandidateWithoutParents{},
	}, true)
	if err != nil {
		t.Fatalf("failed to build candidate: %v", err)
	}

	_, err = buildSimplexToSignPayload(block, bytes.Repeat([]byte{0x01}, 32), 1, candidate)
	if err == nil {
		t.Fatal("expected block id mismatch error")
	}
	if !strings.Contains(err.Error(), "block id mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
