package adnl

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
)

type PacketContent struct {
	Rand1                       []byte
	From                        *PublicKeyED25519
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
}

var _PacketContentID uint32

func init() {
	_PacketContentID = tl.Register(PacketContent{}, "adnl.packetContents rand1:bytes flags:# "+
		"from:flags.0?PublicKey from_short:flags.1?adnl.id.short "+
		"message:flags.2?adnl.Message messages:flags.3?(vector adnl.Message) "+
		"address:flags.4?adnl.addressList priority_address:flags.5?adnl.addressList "+
		"seqno:flags.6?long confirm_seqno:flags.7?long recv_addr_list_version:flags.8?int "+
		"recv_priority_addr_list_version:flags.9?int reinit_date:flags.10?int "+
		"dst_reinit_date:flags.10?int signature:flags.11?bytes rand2:bytes = adnl.PacketContents")
}

var ErrTooShortData = errors.New("too short data")

func parsePacket(data []byte) (_ *PacketContent, err error) {
	var packet PacketContent

	if len(data) < 4 {
		return nil, ErrTooShortData
	}

	if _PacketContentID != binary.LittleEndian.Uint32(data[:4]) {
		return nil, errors.New("not an adnl.packetContents")
	}
	data = data[4:]

	// skip rand1
	_, data, err = tl.FromBytes(data)
	if err != nil {
		return nil, err
	}

	if len(data) < 4 {
		return nil, ErrTooShortData
	}
	flags := binary.LittleEndian.Uint32(data)
	data = data[4:]

	if flags&_FlagFrom != 0 {
		if len(data) < 4 {
			return nil, ErrTooShortData
		}

		var key PublicKeyED25519
		data, err = tl.Parse(&key, data, true)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'from' key, err: %w", err)
		}

		packet.From = &key
	}

	if flags&_FlagFromShort != 0 {
		packet.FromIDShort = data[:32]
		data = data[32:]
	}

	if flags&_FlagOneMessage != 0 {
		var msg any
		data, err = tl.Parse(&msg, data, true)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'message', err: %w", err)
		}

		packet.Messages = []any{msg}
	}

	if flags&_FlagMultipleMessages != 0 {
		num := binary.LittleEndian.Uint32(data)
		data = data[4:]

		for i := uint32(0); i < num; i++ {
			var msg any
			data, err = tl.Parse(&msg, data, true)
			if err != nil {
				return nil, fmt.Errorf("failed to parse 'messages'[%d], err: %w", i, err)
			}
			packet.Messages = append(packet.Messages, msg)
		}
	}

	if flags&_FlagAddress != 0 {
		var list address.List
		data, err = tl.Parse(&list, data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'address', err: %w", err)
		}
		packet.Address = &list

		// TODO: check
		// ppp, _ := json.Marshal(packet.Address)
		// println("GOT SIMPLE", string(ppp))
	}

	if flags&_FlagPriorityAddress != 0 {
		var list address.List
		data, err = tl.Parse(&list, data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse 'priority address', err: %w", err)
		}
		packet.PriorityAddress = &list
	}

	if flags&_FlagSeqno != 0 {
		seqno := int64(binary.LittleEndian.Uint64(data))
		data = data[8:]

		packet.Seqno = &seqno
	}

	if flags&_FlagConfirmSeqno != 0 {
		seqno := int64(binary.LittleEndian.Uint64(data))
		data = data[8:]

		packet.ConfirmSeqno = &seqno
	}

	if flags&_FlagRecvAddrListVer != 0 {
		ver := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]

		packet.RecvPriorityAddrListVersion = &ver
	}

	if flags&_FlagRecvPriorityAddrVer != 0 {
		ver := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]

		packet.RecvPriorityAddrListVersion = &ver
	}

	if flags&_FlagReinitDate != 0 {
		reinit := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]
		packet.ReinitDate = &reinit

		dstReinit := int32(binary.LittleEndian.Uint32(data))
		data = data[4:]
		packet.DstReinitDate = &dstReinit
	}

	if flags&_FlagSignature != 0 {
		packet.Signature, data, err = tl.FromBytes(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse signature: %w", err)
		}
	}

	return &packet, nil
}

func (p *PacketContent) Serialize() ([]byte, error) {
	// adnl.packetContents id
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, _PacketContentID)

	data = append(data, tl.ToBytes(p.Rand1)...)

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

	flagsBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(flagsBytes, flags)
	data = append(data, flagsBytes...)

	if p.From != nil {
		payload, err := tl.Serialize(p.From, true)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize from key, err: %w", err)
		}
		data = append(data, payload...)
	}

	if p.FromIDShort != nil {
		data = append(data, p.FromIDShort...)
	}

	if len(p.Messages) > 1 {
		msgsNumBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(msgsNumBytes, uint32(len(p.Messages)))
		data = append(data, msgsNumBytes...)

		fullLen := 0
		for i, msg := range p.Messages {
			payload, err := tl.Serialize(msg, true)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize %d message, err: %w", i, err)
			}

			fullLen += len(payload)
			data = append(data, payload...)
		}

		if fullLen > _MTU+128 {
			return nil, fmt.Errorf("payload bigger than MTU, sz %d", fullLen)
		}
	} else if len(p.Messages) == 1 {
		payload, err := tl.Serialize(p.Messages[0], true)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize single message, err: %w", err)
		}

		if len(payload) > _MTU+128 {
			return nil, fmt.Errorf("payload bigger than MTU, sz %d", len(payload))
		}

		data = append(data, payload...)
	} else {
		return nil, fmt.Errorf("no messages in packet")
	}

	if p.Address != nil {
		payload, err := tl.Serialize(p.Address, false)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize address, err: %w", err)
		}

		data = append(data, payload...)
	}

	if p.PriorityAddress != nil {
		payload, err := tl.Serialize(p.PriorityAddress, false)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize priority address, err: %w", err)
		}

		data = append(data, payload...)
	}

	if p.Seqno != nil {
		seqnoBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(seqnoBytes, uint64(*p.Seqno))
		data = append(data, seqnoBytes...)
	}

	if p.ConfirmSeqno != nil {
		confirmSeqnoBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(confirmSeqnoBytes, uint64(*p.ConfirmSeqno))
		data = append(data, confirmSeqnoBytes...)
	}

	if p.RecvAddrListVersion != nil {
		recvAddrListBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(recvAddrListBytes, uint32(*p.RecvAddrListVersion))
		data = append(data, recvAddrListBytes...)
	}

	if p.RecvPriorityAddrListVersion != nil {
		recvAddrListBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(recvAddrListBytes, uint32(*p.RecvPriorityAddrListVersion))
		data = append(data, recvAddrListBytes...)
	}

	if p.ReinitDate != nil {
		reinitDateBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(reinitDateBytes, uint32(*p.ReinitDate))
		data = append(data, reinitDateBytes...)

		if p.DstReinitDate == nil {
			return nil, fmt.Errorf("dst reinit could not be nil when reinit is specified")
		}

		dstReinitDateBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(dstReinitDateBytes, uint32(*p.DstReinitDate))
		data = append(data, dstReinitDateBytes...)
	}

	if p.Signature != nil {
		data = append(data, tl.ToBytes(p.Signature)...)
	}

	data = append(data, tl.ToBytes(p.Rand2)...)

	return data, nil
}

var _FlagsDBG = map[uint32]string{
	0x1:    "FROM",
	0x2:    "FROM_SHORT",
	0x4:    "ONE_MESSAGE",
	0x8:    "MULT_MESSAGE",
	0x10:   "ADDRESS",
	0x20:   "PRIORITY_ADDRESS",
	0x40:   "SEQNO",
	0x80:   "CONFIRM_SEQNO",
	0x100:  "RECV_ADDR_LIST_VER",
	0x200:  "RECV_PRIORITY_ADDR_VER",
	0x400:  "REINIT_DATE",
	0x800:  "SIGNATURE",
	0x1000: "PRIORITY",
	0x1fff: "ALL",
}

func randForPacket() ([]byte, error) {
	data := make([]byte, 16)
	_, err := rand.Read(data)
	if err != nil {
		return nil, err
	}

	if data[0]&1 > 0 {
		return data[1:], nil
	}
	return data[1:8], nil
}
