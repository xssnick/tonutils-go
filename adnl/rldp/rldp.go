package rldp

import (
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
	MaxAnswerSize int64  `tl:"long"`
	Timeout       int32  `tl:"int"`
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
	Part       int32  `tl:"int"`
	Seqno      int32  `tl:"int"`
}

type ConfirmV2 struct {
	TransferID    []byte `tl:"int256"`
	Part          int32  `tl:"int"`
	MaxSeqno      int32  `tl:"int"`
	ReceivedMask  int32  `tl:"int"`
	ReceivedCount int32  `tl:"int"`
}

type Complete struct {
	TransferID []byte `tl:"int256"`
	Part       int32  `tl:"int"`
}

type CompleteV2 struct {
	TransferID []byte `tl:"int256"`
	Part       int32  `tl:"int"`
}

type MessagePart struct {
	TransferID []byte `tl:"int256"`
	FecType    any    `tl:"struct boxed [fec.roundRobin,fec.raptorQ,fec.online]"`
	Part       int32  `tl:"int"`
	TotalSize  int64  `tl:"long"`
	Seqno      int32  `tl:"int"`
	Data       []byte `tl:"bytes"`
}

type MessagePartV2 struct {
	TransferID []byte `tl:"int256"`
	FecType    any    `tl:"struct boxed [fec.roundRobin,fec.raptorQ,fec.online]"`
	Part       int32  `tl:"int"`
	TotalSize  int64  `tl:"long"`
	Seqno      int32  `tl:"int"`
	Data       []byte `tl:"bytes"`
}
