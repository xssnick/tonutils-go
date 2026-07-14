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
	if err := parsed.LoadFromCell(envelope.MustBeginParse()); err != nil {
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
	if err := parsed.LoadFromCell(metadata.MustBeginParse()); err == nil {
		t.Fatal("expected anycast metadata initiator to be rejected")
	}
}

func TestMsgEnvelopeRejectsTruncatedIntermediateAddress(t *testing.T) {
	// 8 bits starting with 0b10 announce interm_addr_simple$10 which needs
	// 2+8+64 bits; a truncated cell must be rejected
	envelope := cell.BeginCell().
		MustStoreUInt(5, 4).
		MustStoreUInt(128, 8).
		EndCell()

	var parsed MsgEnvelope
	if err := parsed.LoadFromCell(envelope.MustBeginParse()); err == nil {
		t.Fatal("expected truncated intermediate address to be rejected")
	}
}

func TestMsgEnvelopeV1RoundTrip(t *testing.T) {
	msg := cell.BeginCell().MustStoreUInt(0xDEAD, 16).EndCell()

	env := MsgEnvelope{
		CurAddr:         IntermediateAddress{Type: IntermediateAddressRegular, UseDestBits: 96},
		NextAddr:        IntermediateAddress{Type: IntermediateAddressSimple, Workchain: -1, AddrPfx: 0x8000000000000000},
		FwdFeeRemaining: MustFromTON("1.5"),
		Msg:             msg,
	}

	c, err := env.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	tag, err := c.MustBeginParse().LoadUInt(4)
	if err != nil || tag != 4 {
		t.Fatalf("v1 envelope must serialize with tag 4, got %d (%v)", tag, err)
	}

	var parsed MsgEnvelope
	if err = parsed.LoadFromCell(c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if parsed.V2 {
		t.Fatal("v1 envelope parsed as v2")
	}
	if parsed.CurAddr != env.CurAddr || parsed.NextAddr != env.NextAddr {
		t.Fatalf("intermediate addresses mismatch: %+v / %+v", parsed.CurAddr, parsed.NextAddr)
	}
	if parsed.FwdFeeRemaining.Nano().Cmp(env.FwdFeeRemaining.Nano()) != 0 {
		t.Fatalf("fwd fee mismatch: %s", parsed.FwdFeeRemaining)
	}
	if !bytes.Equal(parsed.Msg.Hash(), msg.Hash()) {
		t.Fatal("message ref mismatch")
	}

	back, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Hash(), c.Hash()) {
		t.Fatal("v1 envelope round-trip mismatch")
	}
}

func TestMsgEnvelopeV2RoundTrip(t *testing.T) {
	msg := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	emitted := uint64(777)
	addr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))

	env := MsgEnvelope{
		CurAddr:         IntermediateAddress{Type: IntermediateAddressExt, Workchain: 555, AddrPfx: 42},
		NextAddr:        IntermediateAddress{Type: IntermediateAddressRegular, UseDestBits: 12},
		FwdFeeRemaining: MustFromTON("0.25"),
		Msg:             msg,
		EmittedLT:       &emitted,
		Metadata: &MsgMetadata{
			Depth:       3,
			Initiator:   addr,
			InitiatorLT: 999,
		},
	}

	c, err := env.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	var parsed MsgEnvelope
	if err = parsed.LoadFromCell(c.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if !parsed.V2 {
		t.Fatal("v2 envelope not marked as v2")
	}
	if parsed.EmittedLT == nil || *parsed.EmittedLT != emitted {
		t.Fatalf("emitted lt mismatch: %v", parsed.EmittedLT)
	}
	if parsed.Metadata == nil || parsed.Metadata.Depth != 3 || parsed.Metadata.InitiatorLT != 999 ||
		parsed.Metadata.Initiator.String() != addr.String() {
		t.Fatalf("metadata mismatch: %+v", parsed.Metadata)
	}
	if parsed.CurAddr != env.CurAddr || parsed.NextAddr != env.NextAddr {
		t.Fatalf("intermediate addresses mismatch: %+v / %+v", parsed.CurAddr, parsed.NextAddr)
	}

	back, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Hash(), c.Hash()) {
		t.Fatal("v2 envelope round-trip mismatch")
	}

	// a v2 envelope without emitted_lt/metadata must keep serializing as v2
	bare := MsgEnvelope{
		FwdFeeRemaining: MustFromTON("0"),
		Msg:             msg,
		V2:              true,
	}
	bareCell, err := bare.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var bareParsed MsgEnvelope
	if err = bareParsed.LoadFromCell(bareCell.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if !bareParsed.V2 || bareParsed.EmittedLT != nil || bareParsed.Metadata != nil {
		t.Fatalf("bare v2 round-trip mismatch: %+v", bareParsed)
	}
	backBare, err := bareParsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backBare.Hash(), bareCell.Hash()) {
		t.Fatal("bare v2 envelope round-trip mismatch")
	}
}

func TestIntermediateAddressVariants(t *testing.T) {
	cases := []IntermediateAddress{
		{Type: IntermediateAddressRegular, UseDestBits: 0},
		{Type: IntermediateAddressRegular, UseDestBits: 96},
		{Type: IntermediateAddressSimple, Workchain: 0, AddrPfx: 0xDEADBEEF00000001},
		{Type: IntermediateAddressSimple, Workchain: -128, AddrPfx: 1},
		{Type: IntermediateAddressExt, Workchain: 100500, AddrPfx: 0xFFFFFFFFFFFFFFFF},
		{Type: IntermediateAddressExt, Workchain: -100500, AddrPfx: 0},
	}
	wantBits := []uint{8, 8, 74, 74, 98, 98}

	for i, a := range cases {
		c, err := a.ToCell()
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if c.BitsSize() != wantBits[i] {
			t.Fatalf("case %d: got %d bits, want %d", i, c.BitsSize(), wantBits[i])
		}
		var parsed IntermediateAddress
		if err = parsed.LoadFromCell(c.MustBeginParse()); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if parsed != a {
			t.Fatalf("case %d round-trip mismatch: %+v != %+v", i, parsed, a)
		}
	}

	// regular with use_dest_bits > 96 must be rejected both ways
	bad := IntermediateAddress{Type: IntermediateAddressRegular, UseDestBits: 97}
	if _, err := bad.ToCell(); err == nil {
		t.Fatal("expected use_dest_bits 97 to be rejected on serialize")
	}
	var parsed IntermediateAddress
	badCell := cell.BeginCell().MustStoreUInt(97, 8).EndCell() // 0b0_1100001
	if err := parsed.LoadFromCell(badCell.MustBeginParse()); err == nil {
		t.Fatal("expected use_dest_bits 97 to be rejected on parse")
	}
}
