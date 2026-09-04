package adnl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"sync"
	"time"
)

// Constructor ids of the adnl.Message kinds, taken from the registry so the
// hand-written parser in parseMessageNoCopy can never disagree with the schema
// tl serializes from.
var (
	_MessagePartID           uint32
	_MessageCustomID         uint32
	_MessageNopID            uint32
	_MessageAnswerID         uint32
	_MessageQueryID          uint32
	_MessageReinitID         uint32
	_MessageCreateChannelID  uint32
	_MessageConfirmChannelID uint32
	_MessagePingID           uint32
	_MessagePongID           uint32
)

func init() {
	_MessagePartID = tl.Register(MessagePart{}, "adnl.message.part hash:int256 total_size:int offset:int data:bytes = adnl.Message")
	_MessageCustomID = tl.Register(MessageCustom{}, "adnl.message.custom data:bytes = adnl.Message")
	_MessageNopID = tl.Register(MessageNop{}, "adnl.message.nop = adnl.Message")
	_MessageAnswerID = tl.Register(MessageAnswer{}, "adnl.message.answer query_id:int256 answer:bytes = adnl.Message")
	_MessageQueryID = tl.Register(MessageQuery{}, "adnl.message.query query_id:int256 query:bytes = adnl.Message")
	_MessageReinitID = tl.Register(MessageReinit{}, "adnl.message.reinit date:int = adnl.Message")
	_MessageCreateChannelID = tl.Register(MessageCreateChannel{}, "adnl.message.createChannel key:int256 date:int = adnl.Message")
	_MessageConfirmChannelID = tl.Register(MessageConfirmChannel{}, "adnl.message.confirmChannel key:int256 peer_key:int256 date:int = adnl.Message")
	_MessagePingID = tl.Register(MessagePing{}, "adnl.ping value:long = adnl.Pong")
	_MessagePongID = tl.Register(MessagePong{}, "adnl.pong value:long = adnl.Pong")
}

// parseMessageNoCopy decodes one boxed adnl.Message the way
// tl.ParseNoCopy(&msg, data, true) does, without reflection: the kind is
// switched on its constructor id and the fields are read in schema order. The
// result carries the same value type and the same ownership as the reflective
// decode, so the type switches downstream and the buffer-reuse rules are
// unchanged: fixed-size and bytes fields alias data (they are consumed before
// the datagram buffer is released), while the custom, query and answer
// payloads are copied by their ParseNoCopy methods because handlers keep them.
// Kinds this switch does not know keep going through tl.
func parseMessageNoCopy(data []byte) (msg any, rest []byte, err error) {
	rest, err = parseMessageNoCopyInto(&msg, data)
	return msg, rest, err
}

// parseMessageNoCopyInto writes the decoded value directly into dst. Receive
// paths use it to avoid making the interface result of parseMessageNoCopy
// escape independently from the slot that ultimately owns it.
func parseMessageNoCopyInto(dst *any, data []byte) (rest []byte, err error) {
	if len(data) < 4 {
		return nil, ErrTooShortData
	}
	id := binary.LittleEndian.Uint32(data)
	body := data[4:]

	switch id {
	case _MessageCustomID:
		var m MessageCustom
		if body, err = m.ParseNoCopy(body); err != nil {
			return nil, err
		}
		*dst = m
		return body, nil
	case _MessagePartID:
		if len(body) < 32+4+4 {
			return nil, ErrTooShortData
		}
		m := MessagePart{
			Hash:      body[:32:32],
			TotalSize: int32(binary.LittleEndian.Uint32(body[32:])),
			Offset:    int32(binary.LittleEndian.Uint32(body[36:])),
		}
		if m.Data, body, err = tl.FromBytesNoCopy(body[40:]); err != nil {
			return nil, err
		}
		*dst = m
		return body, nil
	case _MessageNopID:
		*dst = MessageNop{}
		return body, nil
	case _MessageAnswerID:
		var m MessageAnswer
		if body, err = m.ParseNoCopy(body); err != nil {
			return nil, err
		}
		*dst = m
		return body, nil
	case _MessageQueryID:
		var m MessageQuery
		if body, err = m.ParseNoCopy(body); err != nil {
			return nil, err
		}
		*dst = m
		return body, nil
	case _MessageReinitID:
		if len(body) < 4 {
			return nil, ErrTooShortData
		}
		*dst = MessageReinit{Date: int32(binary.LittleEndian.Uint32(body))}
		return body[4:], nil
	case _MessageCreateChannelID:
		if len(body) < 32+4 {
			return nil, ErrTooShortData
		}
		m := MessageCreateChannel{
			Key:  body[:32:32],
			Date: int32(binary.LittleEndian.Uint32(body[32:])),
		}
		*dst = m
		return body[36:], nil
	case _MessageConfirmChannelID:
		if len(body) < 32+32+4 {
			return nil, ErrTooShortData
		}
		m := MessageConfirmChannel{
			Key:     body[:32:32],
			PeerKey: body[32:64:64],
			Date:    int32(binary.LittleEndian.Uint32(body[64:])),
		}
		*dst = m
		return body[68:], nil
	case _MessagePingID:
		if len(body) < 8 {
			return nil, ErrTooShortData
		}
		*dst = MessagePing{Value: int64(binary.LittleEndian.Uint64(body))}
		return body[8:], nil
	case _MessagePongID:
		if len(body) < 8 {
			return nil, ErrTooShortData
		}
		*dst = MessagePong{Value: int64(binary.LittleEndian.Uint64(body))}
		return body[8:], nil
	default:
		rest, err = tl.ParseNoCopy(dst, data, true)
		if err != nil {
			return nil, err
		}
		return rest, nil
	}
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
	m.Data, data, err = parseBoxedPayloadOwned(data)
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

	m.ID = append([]byte(nil), data[:32]...)

	var err error
	m.Data, data, err = parseBoxedPayloadOwned(data[32:])
	return data, err
}

func (m MessageQuery) Serialize(buf *bytes.Buffer) error {
	if len(m.ID) == 32 {
		buf.Write(m.ID)
	} else if len(m.ID) == 0 {
		var zero [32]byte
		buf.Write(zero[:])
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

	m.ID = append([]byte(nil), data[:32]...)

	var err error
	m.Data, data, err = parseBoxedPayloadOwned(data[32:])
	return data, err
}

func (m MessageAnswer) Serialize(buf *bytes.Buffer) error {
	if len(m.ID) == 32 {
		buf.Write(m.ID)
	} else if len(m.ID) == 0 {
		var zero [32]byte
		buf.Write(zero[:])
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

	return parseBoxedPayloadSource(source, rest, noCopy)
}

func parseBoxedPayloadOwned(data []byte) (tl.Serializable, []byte, error) {
	source, rest, err := tl.FromBytesNoCopy(data)
	if err != nil {
		return nil, nil, err
	}

	source = append([]byte(nil), source...)
	return parseBoxedPayloadSource(source, rest, true)
}

func parseBoxedPayloadSource(source []byte, rest []byte, noCopy bool) (tl.Serializable, []byte, error) {
	if len(source) == 0 {
		return nil, nil, fmt.Errorf("empty bytes slice cannot be parsed as boxed payload")
	}

	var first any
	var err error
	if noCopy {
		source, err = tl.ParseNoCopy(&first, source, true)
	} else {
		source, err = tl.Parse(&first, source, true)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(source) == 0 {
		return first, rest, nil
	}

	list := make([]tl.Serializable, 0, 2)
	list = append(list, first)
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

	return list, rest, nil
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
