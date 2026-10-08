package adnl

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

// TestPrepareCustomMessagePartsMatchesSerialize pins the hand-built framing
// to tl across the bytes-header forms: the one-byte length below 0xFE, the
// four-byte 0xFE form above it, and every alignment padding.
func TestPrepareCustomMessagePartsMatchesSerialize(t *testing.T) {
	for _, size := range []int{0, 1, 2, 3, 4, 31, 32, 33, 249, 250, 251, 252, 253, 254, 255, 256, 1000, 1452, 66000} {
		head := packetCorpusBytes(36, 0x11)
		body := packetCorpusBytes(size, 0x22)

		want, err := tl.Serialize(&MessageCustom{Data: []tl.Serializable{tl.Raw(head), tl.Raw(body)}}, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := PrepareCustomMessageParts(head, body).Wire(); !bytes.Equal(got, want) {
			t.Fatalf("size %d: parts framing differs from tl\n got %x\nwant %x", size, got, want)
		}

		want, err = tl.Serialize(&MessageCustom{Data: tl.Raw(body)}, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := PrepareCustomMessageParts(body).Wire(); !bytes.Equal(got, want) {
			t.Fatalf("size %d: single part framing differs from tl", size)
		}
	}
}

func TestPrepareCustomMessageMatchesSerialize(t *testing.T) {
	msg := TestMsg{Data: packetCorpusBytes(700, 0x33)}
	want, err := tl.Serialize(&MessageCustom{Data: msg}, true)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareCustomMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prepared.Wire(), want) {
		t.Fatal("prepared message differs from tl serialization")
	}
}

// TestSerializeChannelPacketMatchesPacketContent pins the field-by-field
// channel packet writer to PacketContent.Serialize, which the receiving side
// and every other packet builder still use.
func TestSerializeChannelPacketMatchesPacketContent(t *testing.T) {
	rand1 := packetCorpusBytes(7, 0x01)
	rand2 := packetCorpusBytes(15, 0x02)
	for _, size := range []int{0, 4, 100, 1000, 1400} {
		for _, seqno := range []int64{0, 1, 1 << 40, -1} {
			message := PrepareCustomMessageParts(packetCorpusBytes(size, 0x44)).Wire()
			confirm := seqno + 7

			want := bytes.NewBuffer(nil)
			wantPayload, err := (&PacketContent{
				Rand1:        rand1,
				Messages:     []any{tl.Raw(message)},
				Seqno:        int64Ptr(seqno),
				ConfirmSeqno: int64Ptr(confirm),
				Rand2:        rand2,
			}).Serialize(want)
			if err != nil {
				t.Fatal(err)
			}

			got := bytes.NewBuffer(nil)
			gotPayload := serializeChannelPacket(got, rand1, rand2, seqno, confirm, message)
			if !bytes.Equal(got.Bytes(), want.Bytes()) {
				t.Fatalf("size %d seqno %d: channel packet differs from PacketContent.Serialize", size, seqno)
			}
			if gotPayload != wantPayload {
				t.Fatalf("size %d seqno %d: payload size %d, want %d", size, seqno, gotPayload, wantPayload)
			}
		}
	}
}

// preparedSendPeer is a sender with a capturing writer and, when withChannel
// is set, an established channel whose peer half can decrypt what it sends.
type preparedSendPeer struct {
	adnl     *ADNL
	writer   *capturePacketWriter
	peerCh   *Channel
	peerPriv ed25519.PrivateKey
}

func newPreparedSendPeer(t *testing.T, withChannel bool) *preparedSendPeer {
	t.Helper()

	ourPub, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	if a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub}); err != nil {
		t.Fatal(err)
	}
	if a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub); err != nil {
		t.Fatal(err)
	}
	writer := &capturePacketWriter{}
	a.writer = writer
	p := &preparedSendPeer{adnl: a, writer: writer, peerPriv: peerPriv}
	if !withChannel {
		return p
	}

	ourCh := &Channel{adnl: a, key: ourPriv}
	if err = ourCh.setup(peerPub); err != nil {
		t.Fatal(err)
	}
	peerSide := NewGateway(peerPriv).initADNL()
	peerSide.peerKey = ourPub
	p.peerCh = &Channel{adnl: peerSide, key: peerPriv}
	if err = p.peerCh.setup(ourPub); err != nil {
		t.Fatal(err)
	}
	atomic.StorePointer(&a.channelPtr, unsafe.Pointer(ourCh))
	return p
}

// decodeSent decrypts every captured packet and returns the parsed messages
// with the packet-specific fields (random padding, seqno) dropped.
func (p *preparedSendPeer) decodeSent(t *testing.T) ([][]byte, [][]any) {
	t.Helper()

	var plains [][]byte
	var messages [][]any
	for i, packet := range p.writer.packets {
		var plain []byte
		var err error
		if p.peerCh != nil {
			if !bytes.Equal(packet[:32], p.peerCh.id) {
				t.Fatalf("packet %d is not a channel packet", i)
			}
			plain, err = p.peerCh.decodePacket(packet[32:])
		} else {
			plain, err = decodePacket(p.peerPriv, packet[32:])
		}
		if err != nil {
			t.Fatalf("decode packet %d: %v", i, err)
		}
		parsed, err := parsePacket(plain)
		if err != nil {
			t.Fatalf("parse packet %d: %v", i, err)
		}
		plains = append(plains, plain)
		messages = append(messages, parsed.Messages)
	}
	p.writer.packets = nil
	return plains, messages
}

// TestSendPreparedCustomMessageMatchesSendCustomMessage sends the same
// payload both ways and checks the peer decodes identical messages: on an
// established channel and on the root path (where the packet also carries the
// createChannel message), for a message that fits one datagram and for one
// that is split into parts. The single-datagram channel packet must carry the
// prepared bytes verbatim.
func TestSendPreparedCustomMessageMatchesSendCustomMessage(t *testing.T) {
	for _, withChannel := range []bool{true, false} {
		for _, size := range []int{100, 3000} {
			t.Run("channel="+strconv.FormatBool(withChannel)+"/size="+strconv.Itoa(size), func(t *testing.T) {
				p := newPreparedSendPeer(t, withChannel)
				payload := TestMsg{Data: packetCorpusBytes(size, 0x55)}

				// The split MTU is derived from the header size of the last
				// packet built, which varies with the random padding; pin
				// both sends to the base MTU so they split the same way.
				atomic.StoreUint32(&p.adnl.prevPacketHeaderSz, 0)
				if err := p.adnl.SendCustomMessage(context.Background(), payload); err != nil {
					t.Fatal(err)
				}
				_, wantMessages := p.decodeSent(t)

				prepared, err := PrepareCustomMessage(payload)
				if err != nil {
					t.Fatal(err)
				}
				atomic.StoreUint32(&p.adnl.prevPacketHeaderSz, 0)
				if err = p.adnl.SendPreparedCustomMessage(context.Background(), prepared); err != nil {
					t.Fatal(err)
				}
				plains, gotMessages := p.decodeSent(t)

				if len(gotMessages) == 0 || len(gotMessages) != len(wantMessages) {
					t.Fatalf("prepared send produced %d packets, plain send %d", len(gotMessages), len(wantMessages))
				}
				if !reflect.DeepEqual(gotMessages, wantMessages) {
					t.Fatalf("prepared send decoded to\n%+v\nplain send decoded to\n%+v", gotMessages, wantMessages)
				}
				if size > BasePayloadMTU && len(gotMessages) < 2 {
					t.Fatalf("expected a multipart send, got %d packets", len(gotMessages))
				}
				if withChannel && size < BasePayloadMTU && !bytes.Contains(plains[0], prepared.Wire()) {
					t.Fatal("channel packet does not carry the prepared message verbatim")
				}

				// The custom message payload is the one the handler retains.
				custom, ok := gotMessages[len(gotMessages)-1][len(gotMessages[len(gotMessages)-1])-1].(MessageCustom)
				if size < BasePayloadMTU {
					if !ok {
						t.Fatalf("last message is %T, want MessageCustom", gotMessages[0][0])
					}
					if !bytes.Equal(custom.Data.(TestMsg).Data, payload.Data) {
						t.Fatal("decoded payload differs")
					}
				}
			})
		}
	}
}

func TestSendPreparedCustomMessageRejectsEmpty(t *testing.T) {
	p := newPreparedSendPeer(t, true)
	if err := p.adnl.SendPreparedCustomMessage(context.Background(), nil); err == nil {
		t.Fatal("nil prepared message was accepted")
	}
	if err := p.adnl.SendPreparedCustomMessage(context.Background(), &PreparedCustomMessage{}); err == nil {
		t.Fatal("empty prepared message was accepted")
	}
	if len(p.writer.packets) != 0 {
		t.Fatalf("%d packets were written", len(p.writer.packets))
	}
}

type stubPeerConnClient struct {
	Peer
}

func (stubPeerConnClient) processPacket(*PacketContent, bool) error { return nil }
func (stubPeerConnClient) noteInboundPacket(int)                    {}
func (stubPeerConnClient) noteInboundError(time.Time)               {}

func TestPeerConnForwardsPreparedCustomMessage(t *testing.T) {
	p := newPreparedSendPeer(t, true)
	conn := &peerConn{client: p.adnl}
	prepared := PrepareCustomMessageParts(packetCorpusBytes(40, 0x66))
	if err := conn.SendPreparedCustomMessage(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	if len(p.writer.packets) != 1 {
		t.Fatalf("%d packets were written, want 1", len(p.writer.packets))
	}

	unsupported := &peerConn{client: stubPeerConnClient{}}
	if err := unsupported.SendPreparedCustomMessage(context.Background(), prepared); !errors.Is(err, ErrPreparedCustomMessageUnsupported) {
		t.Fatalf("unsupported client: err = %v", err)
	}
}

// BenchmarkSendPreparedCustomMessageBuild is BenchmarkSendCustomMessageBuild
// for a message prepared once: what a relay pays per peer after the first.
func BenchmarkSendPreparedCustomMessageBuild(b *testing.B) {
	a, ch, _ := packetBuildPeer(b)
	atomic.StorePointer(&a.channelPtr, unsafe.Pointer(ch))

	for _, size := range packetBuildSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			prepared := PrepareCustomMessageParts(make([]byte, size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				packet, packets, err := a.buildRequestMaySplitWire(prepared.Wire(), false)
				if err != nil {
					b.Fatal(err)
				}
				if packet == nil && len(packets) == 0 {
					b.Fatal("no packet built")
				}
			}
		})
	}
}
