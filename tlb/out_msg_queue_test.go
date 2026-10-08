package tlb

import (
	"bytes"
	"fmt"
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
}

func TestMsgEnvelopeBareV2RoundTripPreservesWireVariant(t *testing.T) {
	msg := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	encoded := cell.BeginCell().
		MustStoreUInt(5, 4).
		MustStoreUInt(0, 8).
		MustStoreUInt(0, 8).
		MustStoreUInt(0, 4).
		MustStoreRef(msg).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		EndCell()

	var parsed MsgEnvelope
	if err := parsed.LoadFromCell(encoded.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if !parsed.V2 {
		t.Fatal("decoded v2 envelope lost its wire variant")
	}
	encodedAgain, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if encodedAgain.HashKey() != encoded.HashKey() {
		t.Fatal("bare v2 envelope changed during round-trip")
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

func TestIntermediateAddressExtRejectsSimpleWorkchains(t *testing.T) {
	for _, workchain := range []int32{-128, -1, 0, 127} {
		t.Run(fmt.Sprint(workchain), func(t *testing.T) {
			addr := IntermediateAddress{Type: IntermediateAddressExt, Workchain: workchain}
			if _, err := addr.ToCell(); err == nil {
				t.Fatal("expected serialization to fail")
			}

			encoded := cell.BeginCell().
				MustStoreUInt(0b11, 2).
				MustStoreInt(int64(workchain), 32).
				MustStoreUInt(0, 64).
				EndCell()
			var parsed IntermediateAddress
			if err := parsed.LoadFromCell(encoded.MustBeginParse()); err == nil {
				t.Fatal("expected parsing to fail")
			}
		})
	}

	for _, workchain := range []int32{-129, 128} {
		addr := IntermediateAddress{Type: IntermediateAddressExt, Workchain: workchain}
		encoded, err := addr.ToCell()
		if err != nil {
			t.Fatalf("workchain %d: %v", workchain, err)
		}
		var parsed IntermediateAddress
		if err = parsed.LoadFromCell(encoded.MustBeginParse()); err != nil {
			t.Fatalf("workchain %d: %v", workchain, err)
		}
		if parsed != addr {
			t.Fatalf("workchain %d: parsed %+v", workchain, parsed)
		}
	}
}

func TestNewDispatchQueueAugDictCanonicalEmpty(t *testing.T) {
	queue, err := NewDispatchQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}
	if !queue.IsEmpty() {
		t.Fatal("new dispatch queue is not empty")
	}

	encoded, err := queue.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if encoded.BitsSize() != 65 || encoded.RefsNum() != 0 {
		t.Fatalf("empty dispatch queue is %d bits and %d refs", encoded.BitsSize(), encoded.RefsNum())
	}
	loader := encoded.MustBeginParse()
	hasRoot, err := loader.LoadBoolBit()
	if err != nil {
		t.Fatal(err)
	}
	if hasRoot {
		t.Fatal("empty dispatch queue has a root")
	}
	extra, err := loader.LoadUInt(64)
	if err != nil {
		t.Fatal(err)
	}
	if extra != 0 || loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		t.Fatalf("empty dispatch queue extra = %d", extra)
	}
}

func TestDispatchQueueAugmentationWritable(t *testing.T) {
	accountKey := func(fill byte) *cell.Cell {
		return cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{fill}, 32), 256).EndCell()
	}
	accountValue := func(lts ...uint64) *cell.Cell {
		messages := cell.NewDict(64)
		for _, lt := range lts {
			enqueued, err := (EnqueuedMsg{EnqueuedLT: lt, Msg: cell.BeginCell().MustStoreUInt(lt, 32).EndCell()}).ToCell()
			if err != nil {
				t.Fatal(err)
			}
			if err = messages.SetIntKey(new(big.Int).SetUint64(lt), enqueued); err != nil {
				t.Fatal(err)
			}
		}
		value, err := (AccountDispatchQueue{Messages: messages, Count: uint64(len(lts))}).ToCell()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	rootExtraLT := func(queue *DispatchQueueAugDict) uint64 {
		extra := queue.GetRootExtra()
		if extra == nil {
			t.Fatal("dispatch queue has no root extra")
		}
		lt, err := extra.MustBeginParse().LoadUInt(64)
		if err != nil {
			t.Fatal(err)
		}
		return lt
	}

	queue, err := NewDispatchQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}

	// The C++ Aug_DispatchQueue leaf rule takes the minimal messages key.
	if err = queue.Set(accountKey(0x11), accountValue(500, 700)); err != nil {
		t.Fatal(err)
	}
	if lt := rootExtraLT(queue); lt != 500 {
		t.Fatalf("root extra lt = %d, want 500", lt)
	}
	if err = queue.Set(accountKey(0x22), accountValue(300)); err != nil {
		t.Fatal(err)
	}
	if lt := rootExtraLT(queue); lt != 300 {
		t.Fatalf("root extra lt = %d, want 300", lt)
	}

	encoded, err := queue.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var reloaded DispatchQueueAugDict
	if err = reloaded.LoadFromCell(encoded.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	back, err := reloaded.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Hash(), encoded.Hash()) {
		t.Fatal("dispatch queue round-trip changed the cell hash")
	}

	if err = queue.Delete(accountKey(0x22)); err != nil {
		t.Fatal(err)
	}
	if lt := rootExtraLT(queue); lt != 500 {
		t.Fatalf("root extra lt after delete = %d, want 500", lt)
	}
	if err = queue.Delete(accountKey(0x11)); err != nil {
		t.Fatal(err)
	}
	if !queue.IsEmpty() {
		t.Fatal("dispatch queue is not empty after deleting every account")
	}
	if lt := rootExtraLT(queue); lt != 0 {
		t.Fatalf("empty dispatch queue extra = %d, want 0", lt)
	}
}

func TestOutMsgQueueInfoRoundTrip(t *testing.T) {
	outQueue, err := NewOutMsgQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}
	dispatchQueue, err := NewDispatchQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}

	processed := cell.NewDict(96)
	processedKey := cell.BeginCell().MustStoreUInt(7, 64).MustStoreUInt(9, 32).EndCell()
	processedValue := cell.BeginCell().MustStoreUInt(123, 64).MustStoreSlice(bytes.Repeat([]byte{0xAA}, 32), 256).EndCell()
	if err = processed.Set(processedKey, processedValue); err != nil {
		t.Fatal(err)
	}

	legacy := OutMsgQueueInfo{OutQueue: outQueue, ProcInfo: processed}
	legacyCell, err := legacy.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var info OutMsgQueueInfo
	if err = info.LoadFromCell(legacyCell.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if info.Extra != nil {
		t.Fatal("legacy out message queue unexpectedly has extra")
	}

	size := uint64(42)
	info.Extra = &OutMsgQueueExtra{
		DispatchQueue: dispatchQueue,
		OutQueueSize:  &size,
	}
	encoded, err := info.ToCell()
	if err != nil {
		t.Fatal(err)
	}

	var parsed OutMsgQueueInfo
	if err = parsed.LoadFromCell(encoded.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if parsed.Extra == nil || parsed.Extra.OutQueueSize == nil || *parsed.Extra.OutQueueSize != size {
		t.Fatalf("out queue size = %v", parsed.Extra)
	}
	if parsed.OutQueue == nil || !parsed.OutQueue.IsEmpty() {
		t.Fatal("out queue did not round-trip")
	}
	if parsed.Extra.DispatchQueue == nil || !parsed.Extra.DispatchQueue.IsEmpty() {
		t.Fatal("dispatch queue did not round-trip")
	}
	value, err := parsed.ProcInfo.LoadValue(processedKey)
	if err != nil {
		t.Fatal(err)
	}
	valueCell, err := value.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(valueCell.Hash(), processedValue.Hash()) {
		t.Fatal("processed info did not round-trip")
	}

	back, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Hash(), encoded.Hash()) {
		t.Fatal("out message queue info round-trip changed the cell hash")
	}
}

func TestOutMsgQueueSerializersRejectInvalidRanges(t *testing.T) {
	dispatchQueue, err := NewDispatchQueueAugDict()
	if err != nil {
		t.Fatal(err)
	}
	overflow := uint64(1 << 48)
	if _, err := (OutMsgQueueExtra{DispatchQueue: dispatchQueue, OutQueueSize: &overflow}).ToCell(); err == nil {
		t.Fatal("out queue size above uint48 was accepted")
	}
	if _, err := (AccountDispatchQueue{Count: overflow}).ToCell(); err == nil {
		t.Fatal("account dispatch count above uint48 was accepted")
	}
}

func TestAccountDispatchQueueRoundTrip(t *testing.T) {
	envelope := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	message := EnqueuedMsg{EnqueuedLT: 777, Msg: envelope}
	messageCell, err := message.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	messages := cell.NewDict(64)
	if err = messages.SetIntKey(big.NewInt(123), messageCell); err != nil {
		t.Fatal(err)
	}

	queue := AccountDispatchQueue{Messages: messages, Count: 1}
	encoded, err := queue.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var parsed AccountDispatchQueue
	if err = parsed.LoadFromCell(encoded.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	if parsed.Count != 1 {
		t.Fatalf("count = %d, want 1", parsed.Count)
	}
	value, err := parsed.Messages.LoadValueByIntKey(big.NewInt(123))
	if err != nil {
		t.Fatal(err)
	}
	var parsedMessage EnqueuedMsg
	if err = parsedMessage.LoadFromCell(value); err != nil {
		t.Fatal(err)
	}
	if parsedMessage.EnqueuedLT != message.EnqueuedLT || !bytes.Equal(parsedMessage.Msg.Hash(), envelope.Hash()) {
		t.Fatal("account dispatch message did not round-trip")
	}

	back, err := parsed.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Hash(), encoded.Hash()) {
		t.Fatal("account dispatch queue round-trip changed the cell hash")
	}
}

func TestAccountDispatchQueueRejectsEmptyOrZeroCount(t *testing.T) {
	empty := cell.NewDict(64)
	message := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	nonempty := cell.NewDict(64)
	if err := nonempty.SetIntKey(big.NewInt(1), message); err != nil {
		t.Fatal(err)
	}

	for _, queue := range []AccountDispatchQueue{
		{Messages: empty, Count: 1},
		{Messages: nonempty, Count: 0},
	} {
		if _, err := queue.ToCell(); err == nil {
			t.Fatalf("serialized invalid queue: %+v", queue)
		}
	}

	for _, encoded := range []*cell.Cell{
		cell.BeginCell().MustStoreDict(empty).MustStoreUInt(1, 48).EndCell(),
		cell.BeginCell().MustStoreDict(nonempty).MustStoreUInt(0, 48).EndCell(),
	} {
		var queue AccountDispatchQueue
		if err := queue.LoadFromCell(encoded.MustBeginParse()); err == nil {
			t.Fatal("parsed invalid queue")
		}
	}
}
