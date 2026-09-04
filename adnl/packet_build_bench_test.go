package adnl

import (
	"crypto/ed25519"
	"strconv"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

// packetBuildPeer wires the two halves of a channel the way a live pair ends
// up after the handshake, so the build and decode paths run over real keys.
func packetBuildPeer(b testing.TB) (*ADNL, *Channel, *Channel) {
	b.Helper()

	ourPub, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	newSide := func(ours ed25519.PrivateKey, theirs ed25519.PublicKey) (*ADNL, *Channel) {
		a := NewGateway(ours).initADNL()
		a.peerKey = theirs
		if a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: theirs}); err != nil {
			b.Fatal(err)
		}
		if a.peerKeyX25519, err = keys.Ed25519PubToX25519(theirs); err != nil {
			b.Fatal(err)
		}
		return a, &Channel{adnl: a, key: ours}
	}

	our, ourCh := newSide(ourPriv, peerPub)
	_, peerCh := newSide(peerPriv, ourPub)

	if err = ourCh.setup(peerPub); err != nil {
		b.Fatal(err)
	}
	if err = peerCh.setup(ourPub); err != nil {
		b.Fatal(err)
	}

	// Our encrypt key must be the peer's decrypt key; setup already picks the
	// direction from the node ids, so the peer side decodes what we produce.
	return our, ourCh, peerCh
}

var packetBuildSizes = []int{8, 240, 1024}

// BenchmarkChannelCreatePacket covers everything an outbound channel datagram
// costs except the syscall: TL serialization, the staging buffer, sha256 and
// the stream cipher.
func BenchmarkChannelCreatePacket(b *testing.B) {
	_, ch, _ := packetBuildPeer(b)

	for _, size := range packetBuildSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			payload := tl.Raw(make([]byte, size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if _, err := ch.createPacket(int64(i+1), payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkChannelPacketRoundTrip adds the receive half: the peer decrypts and
// checksums what we built.
func BenchmarkChannelPacketRoundTrip(b *testing.B) {
	_, ch, peerCh := packetBuildPeer(b)

	for _, size := range packetBuildSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			payload := tl.Raw(make([]byte, size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				pkt, err := ch.createPacket(int64(i+1), payload)
				if err != nil {
					b.Fatal(err)
				}
				if _, err = peerCh.decodePacket(pkt[32:]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSendCustomMessageBuild is the full SendCustomMessage path with the
// write removed, i.e. what every relay send pays per peer.
func BenchmarkSendCustomMessageBuild(b *testing.B) {
	a, ch, _ := packetBuildPeer(b)
	atomic.StorePointer(&a.channelPtr, unsafe.Pointer(ch))

	for _, size := range packetBuildSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			msg := &MessageCustom{Data: tl.Raw(make([]byte, size))}
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				packet, packets, err := a.buildRequestMaySplit(msg, false)
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
