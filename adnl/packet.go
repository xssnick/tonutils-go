package adnl

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

type PacketContent struct {
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

	toSign []byte

	// Backing storage for the pointer fields and for the single-message case.
	// parsePacket points the fields above into the packet itself, so a decoded
	// packet is one allocation instead of one per optional field.
	seqnoStore, confirmSeqnoStore              int64
	recvAddrVerStore, recvPriorityAddrVerStore int32
	reinitDateStore, dstReinitDateStore        int32
	fromStore                                  keys.PublicKeyED25519
	messageStore                               [1]any
}

func (p *PacketContent) SeqnoValue() int64 {
	if p == nil || p.Seqno == nil {
		return 0
	}
	return *p.Seqno
}

func (p *PacketContent) ConfirmSeqnoValue() int64 {
	if p == nil || p.ConfirmSeqno == nil {
		return 0
	}
	return *p.ConfirmSeqno
}

func (p *PacketContent) ReinitDateValue() int32 {
	if p == nil || p.ReinitDate == nil {
		return 0
	}
	return *p.ReinitDate
}

func (p *PacketContent) DstReinitDateValue() int32 {
	if p == nil || p.DstReinitDate == nil {
		return 0
	}
	return *p.DstReinitDate
}

var _PacketContentID uint32

// _PublicKeyED25519ID is the boxed constructor of pub.ed25519, the only key
// type a packet source may carry. It is derived from the registry rather than
// spelled out so it cannot drift from what keys serializes.
var _PublicKeyED25519ID = func() uint32 {
	wire, err := tl.Serialize(keys.PublicKeyED25519{Key: make([]byte, ed25519.PublicKeySize)}, true)
	if err != nil {
		panic(err)
	}
	return binary.LittleEndian.Uint32(wire)
}()

func init() {
	_PacketContentID = tl.CRC("adnl.packetContents rand1:bytes flags:# " +
		"from:flags.0?PublicKey from_short:flags.1?adnl.id.short " +
		"message:flags.2?adnl.Message messages:flags.3?(vector adnl.Message) " +
		"address:flags.4?adnl.addressList priority_address:flags.5?adnl.addressList " +
		"seqno:flags.6?long confirm_seqno:flags.7?long recv_addr_list_version:flags.8?int " +
		"recv_priority_addr_list_version:flags.9?int reinit_date:flags.10?int " +
		"dst_reinit_date:flags.10?int signature:flags.11?bytes rand2:bytes = adnl.PacketContents")
}

var ErrTooShortData = errors.New("too short data")

// parsePacket decodes an adnl.packetContents by hand, field by field in schema
// order. Every adnl.Message kind is decoded by parseMessageNoCopy; only the
// address lists, which appear on root (handshake and reinit) packets and never
// on channel traffic, are still handed to tl. The result aliases data the same
// way the reflective decode did: rand, short id, signature and the fixed fields
// of channel messages point into the datagram, which the caller consumes
// before releasing it, while the payloads that handlers retain are copied by
// the message parsers.
func parsePacket(data []byte) (*PacketContent, error) {
	packet := new(PacketContent)
	if err := parsePacketInto(packet, data); err != nil {
		return nil, err
	}

	return packet, nil
}

// parsePacketInto avoids allocating PacketContent itself on synchronous
// receive paths. Nested messages may still allocate; packet and all slices
// that alias data must not outlive data.
func parsePacketInto(packet *PacketContent, data []byte) (err error) {
	*packet = PacketContent{}
	orig := data

	if len(data) < 4 {
		return ErrTooShortData
	}

	if _PacketContentID != binary.LittleEndian.Uint32(data[:4]) {
		return errors.New("not an adnl.packetContents")
	}
	data = data[4:]

	packet.Rand1, data, err = tl.FromBytesNoCopy(data)
	if err != nil {
		return err
	}

	if len(data) < 4 {
		return ErrTooShortData
	}

	flagsOffset := len(orig) - len(data)
	flags := binary.LittleEndian.Uint32(data)
	data = data[4:]

	if flags&_FlagFrom != 0 {
		data, err = parsePublicKeyED25519(&packet.fromStore, data)
		if err != nil {
			return fmt.Errorf("failed to parse 'from' key, err: %w", err)
		}

		packet.From = &packet.fromStore
	}

	if flags&_FlagFromShort != 0 {
		if len(data) < 32 {
			return ErrTooShortData
		}

		packet.FromIDShort = data[:32]
		data = data[32:]
	}

	if flags&_FlagOneMessage != 0 {
		data, err = parseMessageNoCopyInto(&packet.messageStore[0], data)
		if err != nil {
			return fmt.Errorf("failed to parse 'message', err: %w", err)
		}

		packet.Messages = packet.messageStore[:1:1]
	}

	if flags&_FlagMultipleMessages != 0 {
		if len(data) < 4 {
			return ErrTooShortData
		}

		num := binary.LittleEndian.Uint32(data)
		data = data[4:]

		if packet.Messages == nil && num > 0 {
			// A message is at least its 4-byte constructor, so a count above
			// a quarter of what is left cannot be satisfied and must not size
			// the slice; the parse of the first missing message rejects it.
			size := int(num)
			if limit := len(data) / 4; size > limit {
				size = limit
			}
			if size <= 1 {
				packet.Messages = packet.messageStore[:0:1]
			} else {
				packet.Messages = make([]any, 0, size)
			}
		}

		for i := uint32(0); i < num; i++ {
			packet.Messages = append(packet.Messages, nil)
			data, err = parseMessageNoCopyInto(&packet.Messages[len(packet.Messages)-1], data)
			if err != nil {
				return fmt.Errorf("failed to parse 'messages'[%d], err: %w", i, err)
			}
		}
	}

	if flags&_FlagAddress != 0 {
		var list address.List
		data, err = tl.ParseNoCopy(&list, data, false)
		if err != nil {
			return fmt.Errorf("failed to parse 'address', err: %w", err)
		}
		packet.Address = &list
	}

	if flags&_FlagPriorityAddress != 0 {
		var list address.List
		data, err = tl.ParseNoCopy(&list, data, false)
		if err != nil {
			return fmt.Errorf("failed to parse 'priority address', err: %w", err)
		}
		packet.PriorityAddress = &list
	}

	if flags&_FlagSeqno != 0 {
		if len(data) < 8 {
			return ErrTooShortData
		}

		packet.seqnoStore = int64(binary.LittleEndian.Uint64(data))
		packet.Seqno = &packet.seqnoStore
		data = data[8:]
	}

	if flags&_FlagConfirmSeqno != 0 {
		if len(data) < 8 {
			return ErrTooShortData
		}

		packet.confirmSeqnoStore = int64(binary.LittleEndian.Uint64(data))
		packet.ConfirmSeqno = &packet.confirmSeqnoStore
		data = data[8:]
	}

	if flags&_FlagRecvAddrListVer != 0 {
		if len(data) < 4 {
			return ErrTooShortData
		}

		packet.recvAddrVerStore = int32(binary.LittleEndian.Uint32(data))
		packet.RecvAddrListVersion = &packet.recvAddrVerStore
		data = data[4:]
	}

	if flags&_FlagRecvPriorityAddrVer != 0 {
		if len(data) < 4 {
			return ErrTooShortData
		}

		packet.recvPriorityAddrVerStore = int32(binary.LittleEndian.Uint32(data))
		packet.RecvPriorityAddrListVersion = &packet.recvPriorityAddrVerStore
		data = data[4:]
	}

	if flags&_FlagReinitDate != 0 {
		if len(data) < 8 {
			return ErrTooShortData
		}

		packet.reinitDateStore = int32(binary.LittleEndian.Uint32(data))
		packet.ReinitDate = &packet.reinitDateStore
		data = data[4:]

		packet.dstReinitDateStore = int32(binary.LittleEndian.Uint32(data))
		packet.DstReinitDate = &packet.dstReinitDateStore
		data = data[4:]
	}

	signatureStart, signatureEnd := -1, -1
	if flags&_FlagSignature != 0 {
		signatureStart = len(orig) - len(data)
		packet.Signature, data, err = tl.FromBytesNoCopy(data)
		if err != nil {
			return fmt.Errorf("failed to parse signature: %w", err)
		}
		signatureEnd = len(orig) - len(data)
	}

	packet.Rand2, data, err = tl.FromBytesNoCopy(data)
	if err != nil {
		return fmt.Errorf("failed to parse rand2: %w", err)
	}

	if len(data) > 0 {
		return fmt.Errorf("too much data in packet")
	}

	if signatureStart >= 0 {
		packet.toSign = buildPacketToSign(orig, flagsOffset, flags, signatureStart, signatureEnd)
	}

	return nil
}

// parsePublicKeyED25519 reads a boxed pub.ed25519 into key. The key bytes are
// copied, as keys.PublicKeyED25519.Parse does, because the source key outlives
// the datagram: the gateway and the peer learn it from root packets.
func parsePublicKeyED25519(key *keys.PublicKeyED25519, data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, ErrTooShortData
	}
	if id := binary.LittleEndian.Uint32(data); id != _PublicKeyED25519ID {
		return nil, fmt.Errorf("invalid TL type id %08x, want pub.ed25519 for packet source", id)
	}
	data = data[4:]

	if len(data) < ed25519.PublicKeySize {
		return nil, ErrTooShortData
	}

	key.Key = make([]byte, ed25519.PublicKeySize)
	copy(key.Key, data)
	return data[ed25519.PublicKeySize:], nil
}

func buildPacketToSign(data []byte, flagsOffset int, flags uint32, signatureStart, signatureEnd int) []byte {
	toSignLen := len(data)
	if signatureStart >= 0 {
		toSignLen -= signatureEnd - signatureStart
	}

	toSign := make([]byte, toSignLen)
	if signatureStart >= 0 {
		copy(toSign, data[:signatureStart])
		copy(toSign[signatureStart:], data[signatureEnd:])
	} else {
		copy(toSign, data)
	}

	binary.LittleEndian.PutUint32(toSign[flagsOffset:], flags&^_FlagSignature)
	return toSign
}

func (p *PacketContent) verifySignature(pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid outer public key length")
	}
	if len(p.Signature) == 0 {
		return fmt.Errorf("packet signature is missing")
	}

	if p.From != nil && !bytes.Equal(p.From.Key, pub) {
		return fmt.Errorf("packet source mismatch")
	}

	if p.FromIDShort != nil {
		shortID, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
		if err != nil {
			return fmt.Errorf("failed to compute packet source id: %w", err)
		}
		if !bytes.Equal(p.FromIDShort, shortID) {
			return fmt.Errorf("packet source short mismatch")
		}
	}
	if !ed25519.Verify(pub, p.toSign, p.Signature) {
		return fmt.Errorf("bad packet signature")
	}
	return nil
}

func (p *PacketContent) Serialize(buf *bytes.Buffer) (int, error) {
	// adnl.packetContents id
	var tmp [8]byte
	binary.LittleEndian.PutUint32(tmp[:4], _PacketContentID)
	buf.Write(tmp[:4])

	_ = tl.ToBytesToBuffer(buf, p.Rand1)

	var flags uint32
	if p.Seqno != nil {
		flags |= _FlagSeqno
	}
	if p.ConfirmSeqno != nil {
		flags |= _FlagConfirmSeqno
	}
	if p.RecvAddrListVersion != nil {
		flags |= _FlagRecvAddrListVer
	}
	if p.RecvPriorityAddrListVersion != nil {
		flags |= _FlagRecvPriorityAddrVer
	}
	if p.Signature != nil {
		flags |= _FlagSignature
	}
	if p.From != nil {
		flags |= _FlagFrom
	}
	if p.FromIDShort != nil {
		flags |= _FlagFromShort
	}
	if p.Address != nil {
		flags |= _FlagAddress
	}
	if p.PriorityAddress != nil {
		flags |= _FlagPriorityAddress
	}
	if p.ReinitDate != nil {
		flags |= _FlagReinitDate
	}

	if len(p.Messages) > 1 {
		flags |= _FlagMultipleMessages
	} else {
		flags |= _FlagOneMessage
	}

	binary.LittleEndian.PutUint32(tmp[:4], flags)
	buf.Write(tmp[:4])

	if p.From != nil {
		_, err := tl.Serialize(p.From, true, buf)
		if err != nil {
			return 0, fmt.Errorf("failed to serialize from key, err: %w", err)
		}
	}

	if p.FromIDShort != nil {
		buf.Write(p.FromIDShort)
	}

	var payloadLen = buf.Len()
	if len(p.Messages) > 1 {
		binary.LittleEndian.PutUint32(tmp[:4], uint32(len(p.Messages)))
		buf.Write(tmp[:4])

		for i, msg := range p.Messages {
			_, err := tl.Serialize(msg, true, buf)
			if err != nil {
				return 0, fmt.Errorf("failed to serialize %d message, err: %w", i, err)
			}
		}
	} else if len(p.Messages) == 1 {
		_, err := tl.Serialize(p.Messages[0], true, buf)
		if err != nil {
			return 0, fmt.Errorf("failed to serialize single message, err: %w", err)
		}
	} else {
		return 0, fmt.Errorf("no messages in packet")
	}
	payloadLen = buf.Len() - payloadLen

	if p.Address != nil {
		_, err := tl.Serialize(p.Address, false, buf)
		if err != nil {
			return 0, fmt.Errorf("failed to serialize address, err: %w", err)
		}
	}

	if p.PriorityAddress != nil {
		_, err := tl.Serialize(p.PriorityAddress, false, buf)
		if err != nil {
			return 0, fmt.Errorf("failed to serialize priority address, err: %w", err)
		}
	}

	if p.Seqno != nil {
		binary.LittleEndian.PutUint64(tmp[:8], uint64(*p.Seqno))
		buf.Write(tmp[:8])
	}

	if p.ConfirmSeqno != nil {
		binary.LittleEndian.PutUint64(tmp[:8], uint64(*p.ConfirmSeqno))
		buf.Write(tmp[:8])
	}

	if p.RecvAddrListVersion != nil {
		binary.LittleEndian.PutUint32(tmp[:4], uint32(*p.RecvAddrListVersion))
		buf.Write(tmp[:4])
	}

	if p.RecvPriorityAddrListVersion != nil {
		binary.LittleEndian.PutUint32(tmp[:4], uint32(*p.RecvPriorityAddrListVersion))
		buf.Write(tmp[:4])
	}

	if p.ReinitDate != nil {
		binary.LittleEndian.PutUint32(tmp[:4], uint32(*p.ReinitDate))
		buf.Write(tmp[:4])

		if p.DstReinitDate == nil {
			return 0, fmt.Errorf("dst reinit could not be nil when reinit is specified")
		}

		binary.LittleEndian.PutUint32(tmp[:4], uint32(*p.DstReinitDate))
		buf.Write(tmp[:4])
	}

	if p.Signature != nil {
		_ = tl.ToBytesToBuffer(buf, p.Signature)
	}

	_ = tl.ToBytesToBuffer(buf, p.Rand2)

	return payloadLen, nil
}

func resizeRandForPacket(data []byte) ([]byte, error) {
	if data[0]&1 > 0 {
		return data[1:], nil
	}
	return data[1:8], nil
}
