package quic

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/xssnick/tonutils-go/tl"
)

var (
	idQuicMessage = tl.Register(Message{}, "quic.message data:bytes = quic.Request")
	idQuicQuery   = tl.Register(Query{}, "quic.query data:bytes = quic.Request")
	idQuicAnswer  = tl.Register(Answer{}, "quic.answer data:bytes = quic.Response")
	idPubEd25519  = tl.CRC("pub.ed25519 key:int256 = PublicKey")
)

const (
	maxPlumtreeBroadcastSize   = 16 << 20
	plumtreePayloadMTUOverhead = 4096

	// MaxPlumtreePayloadSize follows the reference node's Plumtree sender MTU:
	// the 16 MiB broadcast limit plus its exact 4096-byte envelope allowance.
	MaxPlumtreePayloadSize = maxPlumtreeBroadcastSize + plumtreePayloadMTUOverhead

	// A default-size quic.message or quic.query consists of a four-byte
	// constructor, an eight-byte extended TL bytes header, and the payload.
	defaultMaxBoxedObjectSize = 4 + 8 + MaxPlumtreePayloadSize

	maxTLBytesPayloadSize  = (uint64(1) << 32) - 1
	maxBoxedObjectWireSize = (int64(1) << 32) + 12
)

// Message is a TL quic.message object.
type Message struct {
	Data []byte `tl:"bytes"`
}

// Query is a TL quic.query object.
type Query struct {
	Data []byte `tl:"bytes"`
}

// Answer is a TL quic.answer object.
type Answer struct {
	Data []byte `tl:"bytes"`
}

// adnlIDFromKey derives the 32-byte ADNL short id of an Ed25519 public key:
// sha256 of the boxed TL object `pub.ed25519 key:int256 = PublicKey`.
func adnlIDFromKey(pub ed25519.PublicKey) adnlID {
	var buf [4 + ed25519.PublicKeySize]byte
	binary.LittleEndian.PutUint32(buf[:4], idPubEd25519)
	copy(buf[4:], pub)
	return adnlID(sha256.Sum256(buf[:]))
}

func serializeBoxed(id uint32, payload []byte) ([]byte, error) {
	header, headerLen, _, total, err := boxedObjectHeader(id, len(payload))
	if err != nil {
		return nil, err
	}

	dst := make([]byte, total)
	copy(dst, header[:headerLen])
	copy(dst[headerLen:], payload)
	return dst, nil
}

func boxedObjectHeader(id uint32, payloadLen int) (header [12]byte, headerLen, pad, total int, err error) {
	if payloadLen < 0 {
		return header, 0, 0, 0, fmt.Errorf("quic: negative payload size %d", payloadLen)
	}
	if uint64(payloadLen) > maxTLBytesPayloadSize {
		return header, 0, 0, 0, fmt.Errorf("quic: payload size %d exceeds TL uint32 limit", payloadLen)
	}

	binary.LittleEndian.PutUint32(header[:4], id)
	bytesHeaderLen := 1
	switch {
	case payloadLen < 0xFE:
		header[4] = byte(payloadLen)
	case payloadLen < 1<<24:
		bytesHeaderLen = 4
		binary.LittleEndian.PutUint32(header[4:8], uint32(payloadLen)<<8|0xFE)
	default:
		bytesHeaderLen = 8
		header[4] = 0xFF
		binary.LittleEndian.PutUint32(header[5:9], uint32(payloadLen))
	}

	bytesLen := uint64(bytesHeaderLen) + uint64(payloadLen)
	if rem := bytesLen % 4; rem != 0 {
		pad = int(4 - rem)
	}
	headerLen = 4 + bytesHeaderLen

	totalSize := uint64(headerLen) + uint64(payloadLen) + uint64(pad)
	maxInt := uint64(^uint(0) >> 1)
	if totalSize > maxInt {
		return header, 0, 0, 0, fmt.Errorf("quic: boxed object size %d overflows int", totalSize)
	}

	total = int(totalSize)
	return header, headerLen, pad, total, nil
}
