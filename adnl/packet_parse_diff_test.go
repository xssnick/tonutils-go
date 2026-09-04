package adnl

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

// parsePacketReflect is the decoder parsePacket replaced, kept verbatim so the
// hand-written one can be pinned against it: every adnl.Message, the source
// key and the address lists went through tl's reflective boxed decode.
func parsePacketReflect(data []byte) (_ *PacketContent, err error) {
	orig := data

	if len(data) < 4 {
		return nil, ErrTooShortData
	}

	if _PacketContentID != binary.LittleEndian.Uint32(data[:4]) {
		return nil, fmt.Errorf("not an adnl.packetContents")
	}
	data = data[4:]

	var packet PacketContent

	packet.Rand1, data, err = tl.FromBytesNoCopy(data)
	if err != nil {
		return nil, err
	}

	if len(data) < 4 {
		return nil, ErrTooShortData
	}

	flagsOffset := len(orig) - len(data)
	flags := binary.LittleEndian.Uint32(data)
	data = data[4:]

	if flags&_FlagFrom != 0 {
		if len(data) < 4 {
			return nil, ErrTooShortData
		}

		var key keys.PublicKeyED25519
		data, err = tl.ParseNoCopy(&key, data, true)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'from' key, err: %w", err)
		}

		packet.From = &key
	}

	if flags&_FlagFromShort != 0 {
		if len(data) < 32 {
			return nil, ErrTooShortData
		}

		packet.FromIDShort = data[:32]
		data = data[32:]
	}

	if flags&_FlagOneMessage != 0 {
		var msg any
		data, err = tl.ParseNoCopy(&msg, data, true)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'message', err: %w", err)
		}

		packet.Messages = []any{msg}
	}

	if flags&_FlagMultipleMessages != 0 {
		if len(data) < 4 {
			return nil, ErrTooShortData
		}

		num := binary.LittleEndian.Uint32(data)
		data = data[4:]

		for i := uint32(0); i < num; i++ {
			var msg any
			data, err = tl.ParseNoCopy(&msg, data, true)
			if err != nil {
				return nil, fmt.Errorf("failed to parse 'messages'[%d], err: %w", i, err)
			}
			packet.Messages = append(packet.Messages, msg)
		}
	}

	if flags&_FlagAddress != 0 {
		var list address.List
		data, err = tl.ParseNoCopy(&list, data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'address', err: %w", err)
		}
		packet.Address = &list
	}

	if flags&_FlagPriorityAddress != 0 {
		var list address.List
		data, err = tl.ParseNoCopy(&list, data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'priority address', err: %w", err)
		}
		packet.PriorityAddress = &list
	}

	if flags&_FlagSeqno != 0 {
		if len(data) < 8 {
			return nil, ErrTooShortData
		}

		seqno := int64(binary.LittleEndian.Uint64(data))
		data = data[8:]

		packet.Seqno = &seqno
	}

	if flags&_FlagConfirmSeqno != 0 {
		if len(data) < 8 {
			return nil, ErrTooShortData
		}

		seqno := int64(binary.LittleEndian.Uint64(data))
		data = data[8:]

		packet.ConfirmSeqno = &seqno
	}

	if flags&_FlagRecvAddrListVer != 0 {
		if len(data) < 4 {
			return nil, ErrTooShortData
		}

		ver := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]

		packet.RecvAddrListVersion = &ver
	}

	if flags&_FlagRecvPriorityAddrVer != 0 {
		if len(data) < 4 {
			return nil, ErrTooShortData
		}

		ver := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]

		packet.RecvPriorityAddrListVersion = &ver
	}

	if flags&_FlagReinitDate != 0 {
		if len(data) < 8 {
			return nil, ErrTooShortData
		}

		reinit := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]
		packet.ReinitDate = &reinit

		dstReinit := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]
		packet.DstReinitDate = &dstReinit
	}

	signatureStart, signatureEnd := -1, -1
	if flags&_FlagSignature != 0 {
		signatureStart = len(orig) - len(data)
		packet.Signature, data, err = tl.FromBytesNoCopy(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse signature: %w", err)
		}
		signatureEnd = len(orig) - len(data)
	}

	packet.Rand2, data, err = tl.FromBytesNoCopy(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rand2: %w", err)
	}

	if len(data) > 0 {
		return nil, fmt.Errorf("too much data in packet")
	}

	if signatureStart >= 0 {
		packet.toSign = buildPacketToSign(orig, flagsOffset, flags, signatureStart, signatureEnd)
	}

	return &packet, nil
}

// packetView is the observable content of a parsed packet: every exported
// field plus the bytes verifySignature checks. The pointer fields compare by
// value under reflect.DeepEqual, and the unexported backing storage the
// hand-written parser fills is deliberately left out.
type packetView struct {
	Rand1                       []byte
	From                        *keys.PublicKeyED25519
	FromIDShort                 []byte
	Messages                    []any
	Address                     *address.List
	PriorityAddress             *address.List
	Seqno                       *int64
	ConfirmSeqno                *int64
	RecvAddrListVersion         *int32
	RecvPriorityAddrListVersion *int32
	ReinitDate                  *int32
	DstReinitDate               *int32
	Signature                   []byte
	Rand2                       []byte
	ToSign                      []byte
}

func viewPacket(p *PacketContent) packetView {
	return packetView{
		Rand1:                       p.Rand1,
		From:                        p.From,
		FromIDShort:                 p.FromIDShort,
		Messages:                    p.Messages,
		Address:                     p.Address,
		PriorityAddress:             p.PriorityAddress,
		Seqno:                       p.Seqno,
		ConfirmSeqno:                p.ConfirmSeqno,
		RecvAddrListVersion:         p.RecvAddrListVersion,
		RecvPriorityAddrListVersion: p.RecvPriorityAddrListVersion,
		ReinitDate:                  p.ReinitDate,
		DstReinitDate:               p.DstReinitDate,
		Signature:                   p.Signature,
		Rand2:                       p.Rand2,
		ToSign:                      p.toSign,
	}
}

// comparePacketDecoders runs both decoders over data and reports the first
// disagreement: one failing where the other succeeds, or different content.
func comparePacketDecoders(data []byte) error {
	want, wantErr := parsePacketReflect(data)
	got, gotErr := parsePacket(data)
	if (wantErr == nil) != (gotErr == nil) {
		return fmt.Errorf("reflect err = %v, hand-written err = %v", wantErr, gotErr)
	}
	if wantErr != nil {
		return nil
	}
	if !reflect.DeepEqual(viewPacket(want), viewPacket(got)) {
		return fmt.Errorf("decoded packets differ:\nreflect:      %+v\nhand-written: %+v", viewPacket(want), viewPacket(got))
	}
	return nil
}

func packetCorpusBytes(n int, seed byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = seed + byte(i)
	}
	return out
}

func packetCorpusAddressList(version int32) *address.List {
	return &address.List{
		Addresses: []address.Address{
			address.UDP{IP: net.IPv4(10, 1, 2, 3).To4(), Port: 30301},
			address.UDP6{IP: net.ParseIP("2001:db8::1").To16(), Port: 30302},
			address.QUIC{IP: net.IPv4(192, 168, 7, 8).To4(), Port: 30303},
		},
		Version:    version,
		ReinitDate: version + 1,
		Priority:   version + 2,
		ExpireAt:   version + 3,
	}
}

// packetCorpusMessages covers every adnl.Message kind, alone and combined,
// with custom payloads that take both the single-object and the object-list
// branches of the boxed payload decoder.
func packetCorpusMessages() [][]any {
	return [][]any{
		{MessageNop{}},
		{MessagePing{Value: 0x1122334455667788}},
		{MessagePong{Value: -5}},
		{MessageReinit{Date: 1700000000}},
		{MessageCreateChannel{Key: packetCorpusBytes(32, 0x10), Date: 1700000001}},
		{MessageConfirmChannel{Key: packetCorpusBytes(32, 0x20), PeerKey: packetCorpusBytes(32, 0x30), Date: 1700000002}},
		{MessagePart{Hash: packetCorpusBytes(32, 0x40), TotalSize: 5000, Offset: 1024, Data: packetCorpusBytes(1024, 0x50)}},
		{MessageCustom{Data: TestMsg{Data: packetCorpusBytes(700, 0x60)}}},
		{MessageCustom{Data: []tl.Serializable{TestMsg{Data: packetCorpusBytes(32, 0x61)}, TestMsg{Data: packetCorpusBytes(900, 0x62)}}}},
		{MessageQuery{ID: packetCorpusBytes(32, 0x70), Data: TestMsg{Data: packetCorpusBytes(300, 0x71)}}},
		{MessageAnswer{ID: packetCorpusBytes(32, 0x80), Data: TestMsg{Data: packetCorpusBytes(300, 0x81)}}},
		{MessageCreateChannel{Key: packetCorpusBytes(32, 0x90), Date: 3}, MessageCustom{Data: TestMsg{Data: packetCorpusBytes(64, 0x91)}}},
		{MessageConfirmChannel{Key: packetCorpusBytes(32, 0xa0), PeerKey: packetCorpusBytes(32, 0xa1), Date: 4}, MessageNop{}},
		{MessageNop{}, MessagePing{Value: 1}, MessagePong{Value: 2}, MessageReinit{Date: 5}},
		{
			MessageCustom{Data: TestMsg{Data: packetCorpusBytes(100, 0xb0)}},
			MessageCustom{Data: TestMsg{Data: packetCorpusBytes(200, 0xb1)}},
			MessagePart{Hash: packetCorpusBytes(32, 0xb2), TotalSize: 300, Offset: 0, Data: packetCorpusBytes(300, 0xb3)},
		},
	}
}

type packetCorpusProfile struct {
	name      string
	from      bool
	fromShort bool
	address   bool
	priority  bool
	seqno     bool
	confirm   bool
	recvVer   bool
	recvPrio  bool
	reinit    bool
	signed    bool
}

func packetCorpusProfiles() []packetCorpusProfile {
	return []packetCorpusProfile{
		{name: "bare"},
		{name: "channel", seqno: true, confirm: true},
		{name: "channel-seqno-only", seqno: true},
		{name: "root-full", from: true, address: true, priority: true, seqno: true, confirm: true, recvVer: true, recvPrio: true, reinit: true, signed: true},
		{name: "root-short", fromShort: true, seqno: true, confirm: true, reinit: true, signed: true},
		{name: "root-both-sources", from: true, fromShort: true, signed: true},
		{name: "root-address-only", from: true, address: true, signed: true},
		{name: "root-priority-only", from: true, priority: true, recvPrio: true, signed: true},
		{name: "recv-versions", seqno: true, recvVer: true, recvPrio: true},
		{name: "reinit-unsigned", from: true, reinit: true},
	}
}

func buildPacketCorpusEntry(tb testing.TB, profile packetCorpusProfile, messages []any, signer ed25519.PrivateKey) []byte {
	tb.Helper()

	packet := &PacketContent{
		Rand1:    packetCorpusBytes(7, 0x01),
		Messages: messages,
		Rand2:    packetCorpusBytes(15, 0x02),
	}
	if profile.from {
		packet.From = &keys.PublicKeyED25519{Key: signer.Public().(ed25519.PublicKey)}
	}
	if profile.fromShort {
		id, err := tl.Hash(keys.PublicKeyED25519{Key: signer.Public().(ed25519.PublicKey)})
		if err != nil {
			tb.Fatal(err)
		}
		packet.FromIDShort = id
	}
	if profile.address {
		packet.Address = packetCorpusAddressList(100)
	}
	if profile.priority {
		packet.PriorityAddress = packetCorpusAddressList(200)
	}
	if profile.seqno {
		packet.Seqno = int64Ptr(0x0102030405060708)
	}
	if profile.confirm {
		packet.ConfirmSeqno = int64Ptr(0x1112131415161718)
	}
	if profile.recvVer {
		packet.RecvAddrListVersion = int32Ptr(21)
	}
	if profile.recvPrio {
		packet.RecvPriorityAddrListVersion = int32Ptr(22)
	}
	if profile.reinit {
		packet.ReinitDate = int32Ptr(1700000100)
		packet.DstReinitDate = int32Ptr(1700000200)
	}

	buf := bytes.NewBuffer(nil)
	if _, err := packet.Serialize(buf); err != nil {
		tb.Fatalf("serialize %s: %v", profile.name, err)
	}
	if !profile.signed {
		return buf.Bytes()
	}

	packet.Signature = ed25519.Sign(signer, buf.Bytes())
	buf.Reset()
	if _, err := packet.Serialize(buf); err != nil {
		tb.Fatalf("serialize signed %s: %v", profile.name, err)
	}
	return buf.Bytes()
}

// buildPacketCorpusBothMessageFlags hand-assembles the layout Serialize never
// produces: message (flag 2) and messages (flag 3) set together.
func buildPacketCorpusBothMessageFlags(tb testing.TB) []byte {
	tb.Helper()

	buf := bytes.NewBuffer(nil)
	var tmp [8]byte
	binary.LittleEndian.PutUint32(tmp[:4], _PacketContentID)
	buf.Write(tmp[:4])
	_ = tl.ToBytesToBuffer(buf, packetCorpusBytes(7, 0x03))
	binary.LittleEndian.PutUint32(tmp[:4], _FlagOneMessage|_FlagMultipleMessages|_FlagSeqno)
	buf.Write(tmp[:4])

	if _, err := tl.Serialize(MessageNop{}, true, buf); err != nil {
		tb.Fatal(err)
	}
	binary.LittleEndian.PutUint32(tmp[:4], 2)
	buf.Write(tmp[:4])
	if _, err := tl.Serialize(MessagePing{Value: 9}, true, buf); err != nil {
		tb.Fatal(err)
	}
	if _, err := tl.Serialize(MessageCustom{Data: TestMsg{Data: packetCorpusBytes(50, 0x04)}}, true, buf); err != nil {
		tb.Fatal(err)
	}
	binary.LittleEndian.PutUint64(tmp[:8], 77)
	buf.Write(tmp[:8])
	_ = tl.ToBytesToBuffer(buf, packetCorpusBytes(15, 0x05))
	return buf.Bytes()
}

type packetCorpusEntry struct {
	name string
	data []byte
}

func buildPacketCorpus(tb testing.TB) []packetCorpusEntry {
	tb.Helper()

	signer := ed25519.NewKeyFromSeed(packetCorpusBytes(ed25519.SeedSize, 0x99))
	var corpus []packetCorpusEntry
	for _, profile := range packetCorpusProfiles() {
		for i, messages := range packetCorpusMessages() {
			corpus = append(corpus, packetCorpusEntry{
				name: fmt.Sprintf("%s/messages-%d", profile.name, i),
				data: buildPacketCorpusEntry(tb, profile, messages, signer),
			})
		}
	}
	corpus = append(corpus, packetCorpusEntry{name: "both-message-flags", data: buildPacketCorpusBothMessageFlags(tb)})
	return corpus
}

// TestParsePacketMatchesReflectiveDecode pins the hand-written packet decoder
// to the reflective one over every flag combination and message kind, over
// every truncation of those packets, and over random single-byte corruptions
// of them (which reach the branches a well-formed packet never takes: unknown
// constructors, oversized counts, wrong key types).
func TestParsePacketMatchesReflectiveDecode(t *testing.T) {
	corpus := buildPacketCorpus(t)
	if len(corpus) < 100 {
		t.Fatalf("corpus has only %d packets", len(corpus))
	}

	for _, entry := range corpus {
		if err := comparePacketDecoders(entry.data); err != nil {
			t.Fatalf("%s: %v", entry.name, err)
		}
		if _, err := parsePacket(entry.data); err != nil {
			t.Fatalf("%s: well-formed packet rejected: %v", entry.name, err)
		}

		for cut := 0; cut < len(entry.data); cut++ {
			if err := comparePacketDecoders(entry.data[:cut]); err != nil {
				t.Fatalf("%s truncated to %d: %v", entry.name, cut, err)
			}
		}
	}

	rng := rand.New(rand.NewSource(0x5eed))
	mutated := make([]byte, 0, 2048)
	for round := 0; round < 200; round++ {
		for _, entry := range corpus {
			mutated = append(mutated[:0], entry.data...)
			pos := rng.Intn(len(mutated))
			mutated[pos] ^= byte(1 + rng.Intn(255))
			if err := comparePacketDecoders(mutated); err != nil {
				t.Fatalf("%s with byte %d corrupted: %v", entry.name, pos, err)
			}
		}
	}
}

func TestParsePacketIntoResetsDestination(t *testing.T) {
	signer := ed25519.NewKeyFromSeed(packetCorpusBytes(ed25519.SeedSize, 0x96))
	rich := buildPacketCorpusEntry(t, packetCorpusProfile{
		name:     "rich",
		from:     true,
		address:  true,
		priority: true,
		seqno:    true,
		confirm:  true,
		recvVer:  true,
		recvPrio: true,
		reinit:   true,
		signed:   true,
	}, []any{MessageNop{}}, signer)
	bare := buildPacketCorpusEntry(t, packetCorpusProfile{name: "bare"}, []any{MessageNop{}}, signer)

	var packet PacketContent
	if err := parsePacketInto(&packet, rich); err != nil {
		t.Fatal(err)
	}
	if packet.From == nil || packet.Address == nil || packet.Seqno == nil || len(packet.Signature) == 0 {
		t.Fatal("rich packet did not populate optional fields")
	}

	if err := parsePacketInto(&packet, bare); err != nil {
		t.Fatal(err)
	}
	if packet.From != nil || packet.FromIDShort != nil || packet.Address != nil || packet.PriorityAddress != nil ||
		packet.Seqno != nil || packet.ConfirmSeqno != nil || packet.RecvAddrListVersion != nil ||
		packet.RecvPriorityAddrListVersion != nil || packet.ReinitDate != nil || packet.DstReinitDate != nil ||
		packet.Signature != nil || packet.toSign != nil {
		t.Fatal("parsePacketInto retained fields from the previous packet")
	}
	if len(packet.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(packet.Messages))
	}
}

// TestParsePacketMessagesOwnership pins the aliasing contract the transport
// relies on: payloads that handlers keep (custom, query, answer) survive the
// receive path returning PacketContent to its pool, while everything else may
// point into the datagram.
func TestParsePacketMessagesOwnership(t *testing.T) {
	payload := packetCorpusBytes(64, 0x77)
	data := buildPacketCorpusEntry(t, packetCorpusProfile{name: "channel", seqno: true, confirm: true}, []any{
		MessageCustom{Data: TestMsg{Data: payload}},
		MessageQuery{ID: packetCorpusBytes(32, 0x11), Data: TestMsg{Data: payload}},
		MessageAnswer{ID: packetCorpusBytes(32, 0x22), Data: TestMsg{Data: payload}},
		MessagePart{Hash: packetCorpusBytes(32, 0x33), TotalSize: 64, Offset: 0, Data: payload},
	}, ed25519.NewKeyFromSeed(packetCorpusBytes(ed25519.SeedSize, 0x98)))

	packet := getPacketContent()
	if err := parsePacketInto(packet, data); err != nil {
		t.Fatal(err)
	}
	custom := packet.Messages[0].(MessageCustom)
	query := packet.Messages[1].(MessageQuery)
	answer := packet.Messages[2].(MessageAnswer)
	part := packet.Messages[3].(MessagePart)
	putPacketContent(packet)

	// Exercise the same get/parse/put cycle again before inspecting values a
	// handler retained from the previous packet.
	reused := getPacketContent()
	bare := buildPacketCorpusEntry(t, packetCorpusProfile{name: "bare"}, []any{MessageNop{}}, ed25519.NewKeyFromSeed(packetCorpusBytes(ed25519.SeedSize, 0x99)))
	if err := parsePacketInto(reused, bare); err != nil {
		t.Fatal(err)
	}
	putPacketContent(reused)

	for i := range data {
		data[i] ^= 0xFF
	}

	if got := custom.Data.(TestMsg).Data; !bytes.Equal(got, payload) {
		t.Fatal("custom payload aliases the datagram")
	}
	if !bytes.Equal(query.ID, packetCorpusBytes(32, 0x11)) || !bytes.Equal(query.Data.(TestMsg).Data, payload) {
		t.Fatal("query aliases the datagram")
	}
	if !bytes.Equal(answer.ID, packetCorpusBytes(32, 0x22)) || !bytes.Equal(answer.Data.(TestMsg).Data, payload) {
		t.Fatal("answer aliases the datagram")
	}
	if bytes.Equal(part.Data, payload) {
		t.Fatal("part data was copied; expected it to alias the datagram like the reflective decode")
	}
	if cap(part.Hash) != 32 || cap(part.Data) != len(part.Data) {
		t.Fatal("aliased fields must be capacity-capped")
	}
}

// FuzzParsePacket keeps the two decoders in agreement on arbitrary input; the
// corpus above is its seed.
func FuzzParsePacket(f *testing.F) {
	for _, entry := range buildPacketCorpus(f) {
		f.Add(entry.data)
	}
	f.Add([]byte{})
	f.Add(packetCorpusBytes(3, 0))

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := comparePacketDecoders(data); err != nil {
			t.Fatal(err)
		}
	})
}

// BenchmarkParsePacket measures the decode of the plaintext packet bodies a
// node sees most: a channel packet carrying one custom message (the shape of
// every overlay broadcast part, with a two-object payload like the
// overlay.message + broadcast pair), a bare nop (acks), a multipart chunk,
// and a signed root packet with a full address list. The reflect variant is
// the decoder this one replaced.
func BenchmarkParsePacket(b *testing.B) {
	signer := ed25519.NewKeyFromSeed(packetCorpusBytes(ed25519.SeedSize, 0x97))
	channel := packetCorpusProfile{name: "channel", seqno: true, confirm: true}
	root := packetCorpusProfile{name: "root", from: true, address: true, seqno: true, confirm: true, recvVer: true, reinit: true, signed: true}

	cases := []struct {
		name string
		data []byte
	}{
		{"custom-1k", buildPacketCorpusEntry(b, channel, []any{
			MessageCustom{Data: []tl.Serializable{TestMsg{Data: packetCorpusBytes(32, 0x61)}, TestMsg{Data: packetCorpusBytes(900, 0x62)}}},
		}, signer)},
		{"custom-64", buildPacketCorpusEntry(b, channel, []any{
			MessageCustom{Data: TestMsg{Data: packetCorpusBytes(64, 0x63)}},
		}, signer)},
		{"nop", buildPacketCorpusEntry(b, channel, []any{MessageNop{}}, signer)},
		{"part-1k", buildPacketCorpusEntry(b, channel, []any{
			MessagePart{Hash: packetCorpusBytes(32, 0x40), TotalSize: 5000, Offset: 1024, Data: packetCorpusBytes(1024, 0x50)},
		}, signer)},
		{"root-signed", buildPacketCorpusEntry(b, root, []any{MessageCreateChannel{Key: packetCorpusBytes(32, 0x10), Date: 1}, MessageNop{}}, signer)},
	}

	for _, tc := range cases {
		b.Run("reflect/"+tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := parsePacketReflect(tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("hand/"+tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := parsePacket(tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("into/"+tc.name, func(b *testing.B) {
			var packet PacketContent
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := parsePacketInto(&packet, tc.data); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("pooled/"+tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				packet := getPacketContent()
				if err := parsePacketInto(packet, tc.data); err != nil {
					b.Fatal(err)
				}
				putPacketContent(packet)
			}
		})
	}
}
