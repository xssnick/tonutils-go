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

func (m *MessageCustom) Parse(data []byte) ([]byte, error) {
	var err error
	m.Data, data, err = parseBoxedPayload(data, false)
	return data, err
}

func (m *MessageCustom) ParseNoCopy(data []byte) ([]byte, error) {
	var err error
	m.Data, data, err = parseBoxedPayload(data, true)
	return data, err
}

func (m MessageCustom) Serialize(buf *bytes.Buffer) error {
	return serializeBoxedPayload(buf, m.Data)
}

type MessageReinit struct {
	Date int32 `tl:"int"`
}

type MessageNop struct{}

type MessageQuery struct {
	ID   []byte `tl:"int256"`
	Data any    `tl:"bytes struct boxed"`
}

func (m *MessageQuery) Parse(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("message query is too short")
	}

	m.ID = make([]byte, 32)
	copy(m.ID, data[:32])

	var err error
	m.Data, data, err = parseBoxedPayload(data[32:], false)
	return data, err
}

func (m *MessageQuery) ParseNoCopy(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("message query is too short")
	}

	m.ID = data[:32:32]

	var err error
	m.Data, data, err = parseBoxedPayload(data[32:], true)
	return data, err
}

func (m MessageQuery) Serialize(buf *bytes.Buffer) error {
	if len(m.ID) == 32 {
		buf.Write(m.ID)
	} else if len(m.ID) == 0 {
		buf.Write(make([]byte, 32))
	} else {
		return fmt.Errorf("invalid query id size %d", len(m.ID))
	}

	return serializeBoxedPayload(buf, m.Data)
}

type MessageAnswer struct {
	ID   []byte `tl:"int256"`
	Data any    `tl:"bytes struct boxed"`
}

func (m *MessageAnswer) Parse(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("message answer is too short")
	}

	m.ID = make([]byte, 32)
	copy(m.ID, data[:32])

	var err error
	m.Data, data, err = parseBoxedPayload(data[32:], false)
	return data, err
}

func (m *MessageAnswer) ParseNoCopy(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("message answer is too short")
	}

	m.ID = data[:32:32]

	var err error
	m.Data, data, err = parseBoxedPayload(data[32:], true)
	return data, err
}

func (m MessageAnswer) Serialize(buf *bytes.Buffer) error {
	if len(m.ID) == 32 {
		buf.Write(m.ID)
	} else if len(m.ID) == 0 {
		buf.Write(make([]byte, 32))
	} else {
		return fmt.Errorf("invalid answer id size %d", len(m.ID))
	}

	return serializeBoxedPayload(buf, m.Data)
}

type MessagePart struct {
	Hash      []byte `tl:"int256"`
	TotalSize int32  `tl:"int"`
	Offset    int32  `tl:"int"`
	Data      []byte `tl:"bytes"`
}

func serializeBoxedPayload(buf *bytes.Buffer, v tl.Serializable) error {
	from := buf.Len()
	var zero [4]byte
	buf.Write(zero[:])

	if _, err := tl.Serialize(v, true, buf); err != nil {
		return err
	}

	tl.RemapBufferAsSlice(buf, from)
	return nil
}

func parseBoxedPayload(data []byte, noCopy bool) (tl.Serializable, []byte, error) {
	source, rest, err := tl.FromBytesNoCopy(data)
	if err != nil {
		return nil, nil, err
	}

	list := make([]tl.Serializable, 0, 2)
	for len(source) > 0 {
		var obj any
		if noCopy {
			source, err = tl.ParseNoCopy(&obj, source, true)
		} else {
			source, err = tl.Parse(&obj, source, true)
		}
		if err != nil {
			return nil, nil, err
		}
		list = append(list, obj)
	}

	switch len(list) {
	case 1:
		return list[0], rest, nil
	case 0:
		return nil, nil, fmt.Errorf("empty bytes slice cannot be parsed as boxed payload")
	default:
		return list, rest, nil
	}
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

	if int64(offset) > int64(len(m.buf)) || int64(offset)+int64(len(data)) > int64(len(m.buf)) {
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
