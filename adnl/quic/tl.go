package quic

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/tl"
)

var (
	idQuicMessage = tl.Register(Message{}, "quic.message data:bytes = quic.Request")
	idQuicQuery   = tl.Register(Query{}, "quic.query data:bytes = quic.Request")
	idQuicAnswer  = tl.Register(Answer{}, "quic.answer data:bytes = quic.Response")
	idPubEd25519  = tl.CRC("pub.ed25519 key:int256 = PublicKey")
)

const maxTLBytesLen = 1 << 24

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
	dst := make([]byte, 4, len(payload)+12)
	binary.LittleEndian.PutUint32(dst, id)
	return tl.AppendBytes(dst, payload)
}

func boxedObjectHeader(id uint32, payloadLen int) (header [8]byte, headerLen, pad, total int, err error) {
	if payloadLen >= maxTLBytesLen {
		return header, 0, 0, 0, fmt.Errorf("quic: TL bytes too big (%d), limited to 1<<24", payloadLen)
	}

	binary.LittleEndian.PutUint32(header[:4], id)
	bytesHeaderLen := 1
	if payloadLen >= 0xFE {
		bytesHeaderLen = 4
		binary.LittleEndian.PutUint32(header[4:8], uint32(payloadLen<<8)|0xFE)
	} else {
		header[4] = byte(payloadLen)
	}

	bytesLen := bytesHeaderLen + payloadLen
	if rem := bytesLen % 4; rem != 0 {
		pad = 4 - rem
	}
	headerLen = 4 + bytesHeaderLen
	total = headerLen + payloadLen + pad
	return header, headerLen, pad, total, nil
}

// parseBoxed decodes a boxed `<name> data:bytes` object, returning its
// constructor id and the payload.
func parseBoxed(data []byte) (id uint32, payload []byte, err error) {
	if len(data) < 4 {
		return 0, nil, errors.New("quic: boxed object too short")
	}
	if len(data)%4 != 0 {
		return 0, nil, fmt.Errorf("quic: boxed object is not 4-byte aligned: %d bytes", len(data))
	}

	id = binary.LittleEndian.Uint32(data[:4])
	payload, rest, err := tl.FromBytesNoCopy(data[4:])
	if err != nil {
		return 0, nil, err
	}
	if len(rest) != 0 {
		return 0, nil, fmt.Errorf("quic: %d trailing bytes after boxed object", len(rest))
	}
	return id, payload, nil
}
