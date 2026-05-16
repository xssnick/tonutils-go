package adnl

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func TestParsePacketRejectsShortFields(t *testing.T) {
	buf := bytes.NewBuffer(nil)

	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, _PacketContentID)
	buf.Write(tmp)
	_ = tl.ToBytesToBuffer(buf, []byte{1, 2, 3, 4, 5, 6, 7})

	binary.LittleEndian.PutUint32(tmp, _FlagSeqno)
	buf.Write(tmp)

	if _, err := parsePacket(buf.Bytes()); !errors.Is(err, ErrTooShortData) {
		t.Fatalf("expected ErrTooShortData, got %v", err)
	}
}

func TestDecodePacketRejectsTooShortPayload(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = decodePacket(priv, []byte{1, 2, 3}); !errors.Is(err, ErrTooShortData) {
		t.Fatalf("expected ErrTooShortData, got %v", err)
	}
}

func TestChannelDecodePacketRejectsTooShortPayload(t *testing.T) {
	var ch Channel
	if _, err := ch.decodePacket([]byte{1, 2, 3}); !errors.Is(err, ErrTooShortData) {
		t.Fatalf("expected ErrTooShortData, got %v", err)
	}
}

func TestPacketVerifySignatureAcceptsRootPacketWithFullSource(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw := buildSignedPacketBytes(t, &PacketContent{
		Rand1:         []byte{1, 2, 3, 4, 5, 6, 7},
		From:          &keys.PublicKeyED25519{Key: pub},
		Messages:      []any{MessageNop{}},
		Seqno:         int64Ptr(1),
		ConfirmSeqno:  int64Ptr(1),
		ReinitDate:    int32Ptr(1),
		DstReinitDate: int32Ptr(1),
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, priv)

	packet, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}

	if err = packet.verifySignature(pub); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestCreatePacketUsesFullSourceAndRecvAddrVersion(t *testing.T) {
	_, ourPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	peerPub, peerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(ourPriv).initADNL()
	a.peerKey = peerPub
	a.peerID, err = tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		t.Fatal(err)
	}
	a.peerKeyX25519, err = keys.Ed25519PubToX25519(peerPub)
	if err != nil {
		t.Fatal(err)
	}
	a.recvAddrVer = 111
	a.recvPriorityAddrVer = 222

	packetBytes, err := a.createPacket(1, MessageNop{})
	if err != nil {
		t.Fatal(err)
	}

	data, err := decodePacket(peerPriv, packetBytes[32:])
	if err != nil {
		t.Fatal(err)
	}

	packet, err := parsePacket(data)
	if err != nil {
		t.Fatal(err)
	}

	if packet.From == nil || !bytes.Equal(packet.From.Key, ourPriv.Public().(ed25519.PublicKey)) {
		t.Fatalf("expected full source in root packet")
	}
	if packet.FromIDShort != nil {
		t.Fatalf("did not expect from_short in root packet")
	}
	if packet.RecvAddrListVersion == nil || *packet.RecvAddrListVersion != 111 {
		t.Fatalf("unexpected recv addr version: %+v", packet.RecvAddrListVersion)
	}
	if packet.RecvPriorityAddrListVersion == nil || *packet.RecvPriorityAddrListVersion != 222 {
		t.Fatalf("unexpected recv priority addr version: %+v", packet.RecvPriorityAddrListVersion)
	}
}

func TestPacketVerifySignatureRejectsSpoofedSource(t *testing.T) {
	_, victimPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	victimPub := victimPriv.Public().(ed25519.PublicKey)

	attackerPub, attackerPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	raw := buildSignedPacketBytes(t, &PacketContent{
		Rand1: []byte{1, 2, 3, 4, 5, 6, 7},
		From:  &keys.PublicKeyED25519{Key: victimPub},
		Messages: []any{
			MessageNop{},
		},
		Seqno:         int64Ptr(1),
		ConfirmSeqno:  int64Ptr(1),
		ReinitDate:    int32Ptr(1),
		DstReinitDate: int32Ptr(1),
		Rand2:         []byte{8, 9, 10, 11, 12, 13, 14},
	}, attackerPriv)

	packet, err := parsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}

	if err = packet.verifySignature(attackerPub); err == nil || !strings.Contains(err.Error(), "source mismatch") {
		t.Fatalf("expected source mismatch, got %v", err)
	}
}

func TestADNLProcessPacketAllowsPacketWithoutSeqno(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(priv).initADNL()
	if err = a.processPacket(&PacketContent{
		Messages: []any{MessageNop{}},
	}, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGatewayAcceptsPacketSignedBySourceWithEphemeralOuterKey(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	_ = conn.Close()

	gateway := NewGateway(serverPriv)
	defer gateway.Close()

	accepted := make(chan Peer, 1)
	gateway.SetConnectionHandler(func(client Peer) error {
		select {
		case accepted <- client:
		default:
		}
		return nil
	})

	if err = gateway.StartServer(addr); err != nil {
		t.Fatal(err)
	}

	senderPub, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, ephPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	packet := buildEncryptedRootPacket(t, serverPriv, senderPriv, ephPriv, &PacketContent{
		Rand1: []byte{1, 2, 3, 4, 5, 6, 7},
		From:  &keys.PublicKeyED25519{Key: senderPub},
		Messages: []any{
			MessageNop{},
		},
		Seqno:        int64Ptr(1),
		ConfirmSeqno: int64Ptr(1),
		Rand2:        []byte{8, 9, 10, 11, 12, 13, 14},
	})

	udpConn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()

	if _, err = udpConn.Write(packet); err != nil {
		t.Fatal(err)
	}

	select {
	case peer := <-accepted:
		if got := peer.GetPubKey(); !bytes.Equal(got, senderPub) {
			t.Fatalf("unexpected peer key: %x != %x", got, senderPub)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected gateway to accept packet")
	}
}

func buildSignedPacketBytes(t *testing.T, packet *PacketContent, signer ed25519.PrivateKey) []byte {
	t.Helper()

	buf := bytes.NewBuffer(nil)
	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize packet to sign: %v", err)
	}

	packet.Signature = ed25519.Sign(signer, buf.Bytes())
	buf.Reset()

	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize signed packet: %v", err)
	}

	return buf.Bytes()
}

func int32Ptr(v int32) *int32 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func buildEncryptedRootPacket(t *testing.T, dstPriv, signerPriv, outerPriv ed25519.PrivateKey, packet *PacketContent) []byte {
	t.Helper()

	buf := bytes.NewBuffer(make([]byte, 96))
	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize packet to sign: %v", err)
	}

	packet.Signature = ed25519.Sign(signerPriv, buf.Bytes()[96:])
	buf.Truncate(96)

	if _, err := packet.Serialize(buf); err != nil {
		t.Fatalf("failed to serialize signed packet: %v", err)
	}

	data := buf.Bytes()
	payload := data[96:]

	sharedKey, err := keys.SharedKey(outerPriv, dstPriv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("failed to compute shared key: %v", err)
	}

	checksum := sha256.Sum256(payload)
	stream, err := buildSharedCipherForTest(sharedKey, checksum[:])
	if err != nil {
		t.Fatalf("failed to build stream cipher: %v", err)
	}
	stream.XORKeyStream(payload, payload)

	dstID, err := tl.Hash(keys.PublicKeyED25519{Key: dstPriv.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatalf("failed to compute dst id: %v", err)
	}

	copy(data, dstID)
	copy(data[32:], outerPriv.Public().(ed25519.PublicKey))
	copy(data[64:], checksum[:])

	return append([]byte(nil), data...)
}

func buildSharedCipherForTest(key, checksum []byte) (cipher.Stream, error) {
	kiv := make([]byte, 48)
	copy(kiv, key[:16])
	copy(kiv[16:], checksum[16:])
	copy(kiv[32:], checksum[:4])
	copy(kiv[36:], key[20:])

	block, err := aes.NewCipher(kiv[:32])
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, kiv[32:]), nil
}
