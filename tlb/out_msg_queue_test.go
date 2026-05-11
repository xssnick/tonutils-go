package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestMsgEnvelopeV2Metadata(t *testing.T) {
	addr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
	msg := cell.BeginCell().EndCell()
	envelope := cell.BeginCell().
		MustStoreUInt(5, 4).
		MustStoreUInt(0, 8).
		MustStoreUInt(0, 8).
		MustStoreBigCoins(big.NewInt(0)).
		MustStoreRef(msg).
		MustStoreUInt(0, 1).
		MustStoreUInt(1, 1).
		MustStoreUInt(0, 4).
		MustStoreUInt(7, 32).
		MustStoreAddr(addr).
		MustStoreUInt(123, 64).
		EndCell()

	var parsed MsgEnvelope
	if err := parsed.LoadFromCell(envelope.BeginParse()); err != nil {
		t.Fatal(err)
	}
	if parsed.Msg == nil || !bytes.Equal(parsed.Msg.Hash(), msg.Hash()) {
		t.Fatal("message ref mismatch")
	}
	if parsed.Metadata == nil {
		t.Fatal("metadata not loaded")
	}
	if parsed.Metadata.Depth != 7 || parsed.Metadata.InitiatorLT != 123 || parsed.Metadata.Initiator.String() != addr.String() {
		t.Fatalf("unexpected metadata: %+v", parsed.Metadata)
	}
}

func TestMsgMetadataRejectsAnycast(t *testing.T) {
	addr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32)).
		WithAnycast(address.NewAnycast(1, []byte{0x80}))
	metadata := cell.BeginCell().
		MustStoreUInt(0, 4).
		MustStoreUInt(0, 32).
		MustStoreAddr(addr).
		MustStoreUInt(1, 64).
		EndCell()

	var parsed MsgMetadata
	if err := parsed.LoadFromCell(metadata.BeginParse()); err == nil {
		t.Fatal("expected anycast metadata initiator to be rejected")
	}
}

func TestMsgEnvelopeRejectsNonRegularIntermediateAddress(t *testing.T) {
	envelope := cell.BeginCell().
		MustStoreUInt(5, 4).
		MustStoreUInt(128, 8).
		EndCell()

	var parsed MsgEnvelope
	if err := parsed.LoadFromCell(envelope.BeginParse()); err == nil {
		t.Fatal("expected non-regular intermediate address to be rejected")
	}
}
