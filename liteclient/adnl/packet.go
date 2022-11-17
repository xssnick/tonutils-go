package adnl

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/liteclient/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
)

type PacketContent struct {
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

func (a *ADNL) parsePacket(data []byte) (_ *PacketContent, err error) {
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

		reinit = int32(binary.LittleEndian.Uint32(data))
		data = data[4:]
		packet.DstReinitDate = &reinit
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

	rand1, err := randForPacket()
	if err != nil {
		return nil, err
	}
	data = append(data, tl.ToBytes(rand1)...)

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

	if len(p.Messages) > 1 {
		msgsNumBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(msgsNumBytes, uint32(len(p.Messages)))
		data = append(data, msgsNumBytes...)

		for i, msg := range p.Messages {
			payload, err := tl.Serialize(msg, true)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize %d message, err: %w", i, err)
			}
			data = append(data, payload...)
		}
	} else if len(p.Messages) == 1 {
		payload, err := tl.Serialize(p.Messages[0], true)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize single message, err: %w", err)
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

	rand2, err := randForPacket()
	if err != nil {
		return nil, err
	}
	data = append(data, tl.ToBytes(rand2)...)

	return data, nil

	// 89cd42d10f6211afd3d39dc6193e4001afc98be3d9050000c6b41348ae6fa22dbf8b9e76280693ffcec84f01b5578336b2fc5f6b0d077c618538021902000000bbc373e677ed47b4c30ebb67e8d09cc1c7f3e31b0eb6b7e5391a1e1382d26e345ebbd9343d7446637af98bb4adb15d4f77f80e9b533f68242085a6cc72bfb289659d13751d9afbd87d89e861946907537dc6b41348ae6fa22dbf8b9e76280693ffcec84f01b5578336b2fc5f6b0d077c618538021901000000e7a60d679f44ac1f070d00003c7446633c74466300000000000000003d74466340a1d479f7fa9e600ee6d660896ddeaf10206927e9ad74787b1d541c580b031496c7c2dd41d27e3b36f6b55919ae4e945d1901640fe858df8f50a9f782d5db7f03000000ed4879a900000001000000e7a60d679f44ac1f070d00003c7446633c744663000000000000000001000000000000000000000000000000000000003c7446630000000007ea13fd82ae008c
	// 89cd42d10f6211afd3d39dc6193e4001afc98be3d90d0000c6b41348ae6fa22dbf8b9e76280693ffcec84f01b5578336b2fc5f6b0d077c618538021902000000bbc373e677ed47b4c30ebb67e8d09cc1c7f3e31b0eb6b7e5391a1e1382d26e345ebbd9343d7446637af98bb4adb15d4f77f80e9b533f68242085a6cc72bfb289659d13751d9afbd87d89e861946907537dc6b41348ae6fa22dbf8b9e76280693ffcec84f01b5578336b2fc5f6b0d077c618538021901000000e7a60d679f44ac1f070d00003c7446633c74466300000000000000003d74466340a1d479f7fa9e600ee6d660896ddeaf10206927e9ad74787b1d541c580b031496c7c2dd41d27e3b36f6b55919ae4e945d1901640fe858df8f50a9f782d5db7f03000000ed4879a900000001000000e7a60d679f44ac1f070d00003c7446633c744663000000000000000001000000000000000000000000000000000000003c7446630000000040ae1ca3b4491f67eb54b793adbb8460b284f59c7d2dbf1fd864514a312a26590840c373271db1905151235c49da0feea0b4f85dfd0cd270ecb5eeeaca293bb00b00000007ea13fd82ae008c

	// FLAGS CONFIRM_SEQNO SIGNATURE ALL MULT_MESSAGE ADDRESS SEQNO REINIT_DATE
	// FROM SEQNO MULT_MESSAGE SIGNATURE ADDRESS ALL CONFIRM_SEQNO RECV_ADDR_VER REINIT_DATE
	// 89cd42d1 0f 88db7070e7c5062f0579def8b16d38 d90d0000 c6b41348 344dd65d663f68b46c0ee8481c43c7a5870c50043ebb7e6a9ef085c897848981 02000000 bbc373e6d586e1b1d263541a2185905ffdf00a0faeb8299f2f27814e02c61f6b37fc746aab6845637af98bb42192a0bb9a3cf0583b16e3d5700a1b93fc383c233a596ad413e6b6e3de58c660946907537dc6b41348344dd65d663f68b46c0ee8481c43c7a5870c50043ebb7e6a9ef085c89784898101000000e7a60d6701177a4d050d0000aa684563aa6845630000000000000000ab684563400e641642aaf24a458071bd29b9332061865a6453a3481d057520460e2df660300f4ce83d923667eb8b9c5d886a6135fc9e314f2fd497a7b0715620bf5bfe510f000000ed4879a900000001000000e7a60d6701177a4d050d0000aa684563aa68456300000000000000000100000000000000000000000000000000000000aa6845630000000040db4ead2e6b4912af32f19f4f659d0190d65f87f9b871c3629a575afb59fbb6d91dc1691f701cbd76769a387c247e64f45946bc8dde8c84a3ba1a65761eb67d0a000000076dcfe8b631d527
	// 89cd42d1 07 9063d9d97616bb                 d90d0000 c6b41348 28be4c2aa715043c002cabb944c444628d09e2fee2951c1915e4552d1a05286b 02000000 bbc373e6eff948b047e970e4e4be0a65a6a3e57a727e048a0f0bb089b77e56954eaa1a13de6845637af98bb4e0e48516d6b87478d9225ca9a6600f6b613aa85c39957188f093dc8e355fc551946907537dc6b4134828be4c2aa715043c002cabb944c444628d09e2fee2951c1915e4552d1a05286b01000000e7a60d6701177a4d050d0000dd684563dd6845630000000000000000de68456340e184361fa19d44d89756ae9547d89b3ea9d7871b52346fd79894e43cd71b3e7eb06d236c8b8449a22158ad65ffbeb3809548c50ac005d7f136936d4fd34b830d000000ed4879a900000001000000e7a60d6701177a4d050d0000dd684563dd68456300000000000000000100000000000000000000000000000000000000dd684563000000004008d405da94ea4afc53f4b4e43bcc8c730d7d682311b8c9051cea0b45236dfad1a2e6899282a7de53a18febf2f4d1fc9687d9610a375d0e1a8622682e609fad0b00000007dd2443e1e4d52e
	// 89cd42d1 07 3fe627a3ab7d2a d80c000002000000bbc373e620d8aa4d259d3ad8f33125f3970f7414503abae0a19b545432f3f2837eeaee50b3000000d6bf64637af98bb4f8ccec37744c9bea39aeb2f8e551c25aae77845044827d8232c51b543661d97e04ed4879a900000000000000d6bf6463d6bf6463000000000000000001000000000000000000000000000000d6bf646300000000406405cf5f0e62a73b1948de66bf453bf81baa358f648a1eec4e11fba87e78db095693801570507ec7eef4f3a22b9c7e60fe61402da31d83fb6a59562b3945500e0000000744c16b6f7fabe9
	// 89cd42d1 = id
	// 07 9063d9d97616bb = rand1
	// d90d0000 = flags
	// c6b41348 28be4c2aa715043c002cabb944c444628d09e2fee2951c1915e4552d1a05286b = pubkey ed25519
	// 02000000 = size vec
	//			#1
	//			bbc373e6 = adnl.message.createChannel
	//			eff948b047e970e4e4be0a65a6a3e57a727e048a0f0bb089b77e56954eaa1a13 = key
	//			de684563 = date
	//			#2
	//			7af98bb4 = adnl.message.query
	//			e0e48516d6b87478d9225ca9a6600f6b613aa85c39957188f093dc8e355fc551 = query id
	// 				bytes (query)
	//				94
	//					6907537d = dht.query
	//					 c6b41348 = pub.ed25519
	//					 28be4c2aa715043c002cabb944c444628d09e2fee2951c1915e4552d1a05286b = key (our)
	//					 01000000 e7a60d67 01177a4d 050d0000= vector adnl.Address (our ip+port)
	//					 dd684563 = version
	//					 dd684563 = reinit_date
	//					 00000000 = priority
	//					 00000000 = expire_at
	//					de684563 = version
	//					40 e184361fa19d44d89756ae9547d89b3ea9d7871b52346fd79894e43cd71b3e7eb06d236c8b8449a22158ad65ffbeb3809548c50ac005d7f136936d4fd34b830d 000000 = signature
	//					ed4879a9 = operation??? (dht.getSignedAddressList)
	//					000000 -- padding
	//			01000000    e7a60d67 01177a4d 050d0000 = vec (adnl.address.udp)
	//			dd684563 = version
	//			dd684563 = reinit_date
	//			00000000 = priority
	//			00000000 = expire_at
	//
	//			0100000000000000 = seqno
	//			0000000000000000 = confirm seqno
	//			00000000 = recv_addr_list_version
	//			dd684563 = reinit_date
	//			00000000 = dst_reinit_date
	//			40 08d405da94ea4afc53f4b4e43bcc8c730d7d682311b8c9051cea0b45236dfad1a2e6899282a7de53a18febf2f4d1fc9687d9610a375d0e1a8622682e609fad0b 000000 = signature
	//			07 dd2443e1e4d52e = rand2

	// c6b41348 = pub.ed25519 key:int256 = PublicKey
}

func Flags(flags uint32) string {
	m := map[uint32]string{
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

	txt := ""
	for i, s := range m {
		if flags&i != 0 {
			txt += s + " "
		}
	}
	return txt
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
