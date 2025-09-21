package adnl

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"sync"
	"time"
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
	tl.Register(MessagePing{}, "adnl.ping value:long = adnl.Pong")
	tl.Register(MessagePong{}, "adnl.pong value:long = adnl.Pong")
}

type MessagePing struct {
	Value int64 `tl:"long"`
}

type MessagePong struct {
	Value int64 `tl:"long"`
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
	Data any    `tl:"bytes struct boxed"`
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
	startedAt    time.Time
	knownOffsets map[int32]bool
	buf          []byte
	gotLen       int32

	mx sync.Mutex
}

func newPartitionedMessage(size int32) *partitionedMessage {
	return &partitionedMessage{
		startedAt:    time.Now(),
		knownOffsets: map[int32]bool{},
		buf:          make([]byte, size),
	}
}

func (m *partitionedMessage) AddPart(offset int32, data []byte) (bool, error) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if m.gotLen == int32(len(m.buf)) {
		// already full, skip part processing and don't report as ready
		return false, nil
	}

	if len(data) == 0 || offset < 0 {
		return false, nil
	}

	if len(m.buf[offset:]) < len(data) {
		return false, fmt.Errorf("part is bigger than defined message")
	}
	if m.knownOffsets[offset] {
		return false, nil
	}

	if len(m.knownOffsets) > 32 {
		return false, fmt.Errorf("too many parts")
	}

	copy(m.buf[offset:], data)

	m.knownOffsets[offset] = true
	m.gotLen += int32(len(data))

	return m.gotLen == int32(len(m.buf)), nil
}

func (m *partitionedMessage) Build(msgHash []byte) ([]byte, error) {
	m.mx.Lock()
	defer m.mx.Unlock()

	if m.gotLen != int32(len(m.buf)) {
		return nil, fmt.Errorf("not full yet")
	}

	hash := sha256.Sum256(m.buf)
	if !bytes.Equal(hash[:], msgHash) {
		return nil, fmt.Errorf("invalid message, hash not matches")
	}

	return m.buf, nil
}

func splitMessage(data []byte, mtu int) []MessagePart {
	hash := sha256.Sum256(data)

	x := len(data) / mtu
	if len(data)%mtu != 0 {
		x++
	}

	res := make([]MessagePart, 0, x)
	for i := 0; i < x; i++ {
		buf := data[i*mtu:]
		if len(buf) > mtu {
			buf = buf[:mtu]
		}

		res = append(res, MessagePart{
			Hash:      hash[:],
			TotalSize: int32(len(data)),
			Offset:    int32(i * mtu),
			Data:      buf,
		})
	}
	return res
}
