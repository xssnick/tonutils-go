package adnl

import (
	"bytes"
	"sync"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

// decodePing pulls the ping value back out of a datagram the channel produced.
func decodePing(t *testing.T, peer *Channel, datagram []byte) int64 {
	t.Helper()

	body, err := peer.decodePacket(append([]byte(nil), datagram[32:]...))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	packet, err := parsePacket(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(packet.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(packet.Messages))
	}
	ping, ok := packet.Messages[0].(MessagePing)
	if !ok {
		t.Fatalf("unexpected message type %T", packet.Messages[0])
	}
	return ping.Value
}

// TestPacketBuildBufferReuseKeepsPacketsIndependent is the safety property the
// staging pool depends on: the datagram handed to the net manager must be a
// private copy, because the manager can hold it long after the next packet has
// already reused the staging buffer.
func TestPacketBuildBufferReuseKeepsPacketsIndependent(t *testing.T) {
	_, ch, peerCh := packetBuildPeer(t)

	const count = 32
	built := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		pkt, err := ch.createPacket(int64(i+1), MessagePing{Value: int64(i + 1)})
		if err != nil {
			t.Fatal(err)
		}
		built = append(built, pkt)
	}

	// decode only after every packet was built, so any reuse would show up
	for i, pkt := range built {
		if got := decodePing(t, peerCh, pkt); got != int64(i+1) {
			t.Fatalf("packet %d carries value %d", i, got)
		}
	}
}

func TestPacketBuildBufferConcurrentReuse(t *testing.T) {
	_, ch, peerCh := packetBuildPeer(t)

	const workers = 8
	const perWorker = 64

	var mx sync.Mutex
	seen := map[int64]struct{}{}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()

			packets := make([][]byte, 0, perWorker)
			for i := 0; i < perWorker; i++ {
				value := int64(w*perWorker + i + 1)
				pkt, err := ch.createPacket(value, MessagePing{Value: value})
				if err != nil {
					t.Error(err)
					return
				}
				packets = append(packets, pkt)
			}

			mx.Lock()
			defer mx.Unlock()
			for i, pkt := range packets {
				value := int64(w*perWorker + i + 1)
				if got := decodePing(t, peerCh, pkt); got != value {
					t.Errorf("worker %d packet %d carries value %d", w, i, got)
					return
				}
				seen[value] = struct{}{}
			}
		}(w)
	}
	wg.Wait()

	if len(seen) != workers*perWorker {
		t.Fatalf("decoded %d distinct packets, want %d", len(seen), workers*perWorker)
	}
}

// TestPacketBuildBufferOversizeDropped keeps a one-off huge packet from pinning
// its staging array in the pool forever.
func TestPacketBuildBufferOversizeDropped(t *testing.T) {
	oversize := bytes.NewBuffer(make([]byte, 0, maxPooledPacketBufCap+1))
	putPacketBuild(oversize)

	for i := 0; i < 64; i++ {
		if buf := getPacketBuild(64); buf.Cap() > maxPooledPacketBufCap {
			t.Fatalf("oversized staging buffer was pooled (cap %d)", buf.Cap())
		}
	}

	huge := bytes.NewBuffer(make([]byte, 0, maxPooledMessageBufCap+1))
	putMessageBuild(huge)
	for i := 0; i < 64; i++ {
		if buf := getMessageBuild(); buf.Cap() > maxPooledMessageBufCap {
			t.Fatalf("oversized message buffer was pooled (cap %d)", buf.Cap())
		}
	}
}

// TestPacketBuildBufferHeaderReserved pins the invariant the header write after
// encryption relies on: the staging buffer starts with exactly headerSize zero
// bytes, whatever the previous user left in it.
func TestPacketBuildBufferHeaderReserved(t *testing.T) {
	dirty := getPacketBuild(64)
	dirty.Write(bytes.Repeat([]byte{0xAA}, 512))
	putPacketBuild(dirty)

	for _, size := range []int{64, 96} {
		buf := getPacketBuild(size)
		if buf.Len() != size {
			t.Fatalf("header size %d: buffer starts at %d bytes", size, buf.Len())
		}
		if !bytes.Equal(buf.Bytes(), make([]byte, size)) {
			t.Fatalf("header size %d: reserved prefix is not zeroed: %x", size, buf.Bytes())
		}
		putPacketBuild(buf)
	}
}

// TestMessageBuildBufferMatchesUnbufferedSerialization guards the one wire-
// visible risk of staging the body in a pooled buffer: tl has two independent
// serializers, an append-based one for the bufferless call and a bytes.Buffer
// one for the call with a buffer, and buildRequestMaySplit now takes the
// second. They must agree byte for byte for everything ADNL puts on the wire.
func TestMessageBuildBufferMatchesUnbufferedSerialization(t *testing.T) {
	payload := make([]byte, 300)
	for i := range payload {
		payload[i] = byte(i)
	}

	cases := []struct {
		name string
		msg  tl.Serializable
	}{
		{"nop", MessageNop{}},
		{"ping", MessagePing{Value: 0x0102030405060708}},
		{"custom struct", &MessageCustom{Data: MessagePing{Value: 42}}},
		{"custom raw", &MessageCustom{Data: tl.Raw(payload)}},
		{"custom list", &MessageCustom{Data: []tl.Serializable{MessagePing{Value: 1}, MessagePong{Value: 2}}}},
		{"query", &MessageQuery{ID: payload[:32], Data: MessagePing{Value: 7}}},
		{"answer", &MessageAnswer{ID: payload[:32], Data: MessagePong{Value: 7}}},
		{"part", MessagePart{Hash: payload[:32], TotalSize: 900, Offset: 300, Data: payload}},
		{"create channel", &MessageCreateChannel{Key: payload[:32], Date: 12345}},
		{"confirm channel", &MessageConfirmChannel{Key: payload[:32], PeerKey: payload[32:64], Date: 12345}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := tl.Serialize(tc.msg, true)
			if err != nil {
				t.Fatalf("unbuffered: %v", err)
			}

			body := getMessageBuild()
			defer putMessageBuild(body)

			got, err := tl.Serialize(tc.msg, true, body)
			if err != nil {
				t.Fatalf("buffered: %v", err)
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("serialization differs\n got %x\nwant %x", got, want)
			}
		})
	}
}
