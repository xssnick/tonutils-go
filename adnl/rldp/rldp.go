package rldp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(Query{}, "rldp.query query_id:int256 max_answer_size:long timeout:int data:bytes = rldp.Message")
	tl.Register(Answer{}, "rldp.answer query_id:int256 data:bytes = rldp.Message")
	tl.Register(Message{}, "rldp.message id:int256 data:bytes = rldp.Message")
	tl.Register(Confirm{}, "rldp.confirm transfer_id:int256 part:int seqno:int = rldp.MessagePart")
	tl.Register(ConfirmV2{}, "rldp2.confirm transfer_id:int256 part:int max_seqno:int received_mask:int received_count:int = rldp2.MessagePart")
	tl.Register(Complete{}, "rldp.complete transfer_id:int256 part:int = rldp.MessagePart")
	tl.Register(CompleteV2{}, "rldp2.complete transfer_id:int256 part:int = rldp2.MessagePart")
	tl.Register(MessagePart{}, "rldp.messagePart transfer_id:int256 fec_type:fec.Type part:int total_size:long seqno:int data:bytes = rldp.MessagePart")
	tl.Register(MessagePartV2{}, "rldp2.messagePart transfer_id:int256 fec_type:fec.Type part:int total_size:long seqno:int data:bytes = rldp2.MessagePart")
}

type Query struct {
	ID            []byte `tl:"int256"`
	MaxAnswerSize uint64 `tl:"long"`
	Timeout       uint32 `tl:"int"`
	Data          any    `tl:"bytes struct boxed"`
}

type Answer struct {
	ID   []byte `tl:"int256"`
	Data any    `tl:"bytes struct boxed"`
}

type Message struct {
	ID   []byte `tl:"int256"`
	Data []byte `tl:"bytes"`
}

type Confirm struct {
	TransferID []byte `tl:"int256"`
	Part       uint32 `tl:"int"`
	Seqno      uint32 `tl:"int"`
}

type ConfirmV2 struct {
	TransferID    []byte `tl:"int256"`
	Part          uint32 `tl:"int"`
	MaxSeqno      uint32 `tl:"int"`
	ReceivedMask  uint32 `tl:"int"`
	ReceivedCount uint32 `tl:"int"`
}

type Complete struct {
	TransferID []byte `tl:"int256"`
	Part       uint32 `tl:"int"`
}

type CompleteV2 struct {
	TransferID []byte `tl:"int256"`
	Part       uint32 `tl:"int"`
}

type MessagePart struct {
	TransferID []byte // `tl:"int256"`
	FecType    any    // `tl:"struct boxed [fec.roundRobin,fec.raptorQ,fec.online]"`
	Part       uint32 // `tl:"int"`
	TotalSize  uint64 // `tl:"long"`
	Seqno      uint32 // `tl:"int"`
	Data       []byte // `tl:"bytes"`
}

func (m *MessagePart) Parse(data []byte) ([]byte, error) {
	if len(data) < 56 {
		return nil, errors.New("message part is too short")
	}

	transfer := make([]byte, 32)
	copy(transfer, data)

	var fec FECRaptorQ
	data, err := tl.Parse(&fec, data[32:], true)
	if err != nil {
		return nil, err
	}

	if len(data) < 20 {
		return nil, errors.New("message part is too short")
	}

	part := binary.LittleEndian.Uint32(data)
	size := binary.LittleEndian.Uint64(data[4:])
	seq := binary.LittleEndian.Uint32(data[12:])

	slc, data, err := tl.FromBytes(data[16:])
	if err != nil {
		return nil, fmt.Errorf("tl.FromBytes: %v", err)
	}

	m.TransferID = transfer
	m.FecType = fec
	m.Part = part
	m.TotalSize = size
	m.Seqno = seq
	m.Data = slc

	return data, nil
}

func (m *MessagePart) Serialize(buf *bytes.Buffer) error {
	switch m.FecType.(type) {
	case FECRaptorQ:
		if len(m.TransferID) == 0 {
			buf.Write(make([]byte, 32))
		} else if len(m.TransferID) != 32 {
			return errors.New("invalid transfer id")
		} else {
			buf.Write(m.TransferID)
		}

		_, err := tl.Serialize(m.FecType, true, buf)
		if err != nil {
			return err
		}
	default:
		return errors.New("invalid fec type")
	}

	tmp := make([]byte, 16)
	binary.LittleEndian.PutUint32(tmp, m.Part)
	binary.LittleEndian.PutUint64(tmp[4:], m.TotalSize)
	binary.LittleEndian.PutUint32(tmp[12:], m.Seqno)
	buf.Write(tmp)
	tl.ToBytesToBuffer(buf, m.Data)

	return nil
}

type MessagePartV2 struct {
	TransferID []byte `tl:"int256"`
	FecType    any    `tl:"struct boxed [fec.roundRobin,fec.raptorQ,fec.online]"`
	Part       uint32 `tl:"int"`
	TotalSize  uint64 `tl:"long"`
	Seqno      uint32 `tl:"int"`
	Data       []byte `tl:"bytes"`
}
