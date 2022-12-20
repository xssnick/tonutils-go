package adnl

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(MessagePart{}, "adnl.message.part hash:int256 total_size:int offset:int data:bytes = adnl.Message")
	tl.Register(MessageCustom{}, "adnl.message.custom data:bytes = adnl.Message")
	tl.Register(MessageNop{}, "adnl.message.nop = adnl.Message")
	tl.Register(MessageAnswer{}, "adnl.message.answer query_id:int256 answer:bytes = adnl.Message")
	tl.Register(MessageQuery{}, "adnl.message.query query_id:int256 query:bytes = adnl.Message")
	tl.Register(MessageReinit{}, "adnl.message.reinit date:int = adnl.Message")
	tl.Register(MessageCreateChannel{}, "adnl.message.createChannel key:int256 date:int = adnl.Message")
	tl.Register(MessageConfirmChannel{}, "adnl.message.confirmChannel key:int256 peer_key:int256 date:int = adnl.Message")
}

type MessageCreateChannel struct {
	Key  []byte `tl:"int256"`
	Date int32  `tl:"int"`
}

type MessageConfirmChannel struct {
	Key     []byte `tl:"int256"`
	PeerKey []byte `tl:"int256"`
	Date    int32  `tl:"int"`
}

type MessageCustom struct {
	Data any `tl:"bytes struct boxed"`
}

type MessageReinit struct {
	Date int32 `tl:"int"`
}

type MessageNop struct{}

type MessageQuery struct {
	ID   []byte `tl:"int256"`
	Data []byte `tl:"bytes"`
}

type MessageAnswer struct {
	ID   []byte `tl:"int256"`
	Data any    `tl:"bytes struct boxed"`
}

type MessagePart struct {
	Hash      []byte `tl:"int256"`
	TotalSize int32  `tl:"int"`
	Offset    int32  `tl:"int"`
	Data      []byte `tl:"bytes"`
}

type partitionedMessage struct {
	knownOffsets map[int32]bool
	buf          []byte
	gotLen       int32
}

func newPartitionedMessage(size int32) *partitionedMessage {
	return &partitionedMessage{
		knownOffsets: map[int32]bool{},
		buf:          make([]byte, size),
	}
}

func (m *partitionedMessage) AddPart(offset int32, data []byte) (bool, error) {
	if len(m.buf[offset:]) < len(data) {
		return false, fmt.Errorf("part is bigger than defined message")
	}
	if m.knownOffsets[offset] {
		return m.gotLen == int32(len(m.buf)), nil
	}

	copy(m.buf[offset:], data)

	m.knownOffsets[offset] = true
	m.gotLen += int32(len(data))

	return m.gotLen == int32(len(m.buf)), nil
}

func (m *partitionedMessage) Build() []byte {
	if m.gotLen != int32(len(m.buf)) {
		return nil
	}
	return m.buf
}