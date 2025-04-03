package tlb

import (
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type MsgType string

const (
	MsgTypeInternal    MsgType = "INTERNAL"
	MsgTypeExternalIn  MsgType = "EXTERNAL_IN"
	MsgTypeExternalOut MsgType = "EXTERNAL_OUT"
)

func init() {
	Register(ExternalMessage{})
	Register(ExternalMessageOut{})
	Register(InternalMessage{})
}

type AnyMessage interface {
	Payload() *cell.Cell
	SenderAddr() *address.Address
	DestAddr() *address.Address
}

type Message struct {
	MsgType MsgType    `tlb:"-"`
	Msg     AnyMessage `tlb:"[ExternalMessage,ExternalMessageOut,InternalMessage]"`
}

type MessagesList struct {
	List *cell.Dictionary `tlb:"dict inline 15"`
}

type InternalMessage struct {
	_               Magic            `tlb:"$0"`
	IHRDisabled     bool             `tlb:"bool"`
	Bounce          bool             `tlb:"bool"`
	Bounced         bool             `tlb:"bool"`
	SrcAddr         *address.Address `tlb:"addr"`
	DstAddr         *address.Address `tlb:"addr"`
	Amount          Coins            `tlb:"."`
	ExtraCurrencies *cell.Dictionary `tlb:"dict 32"`
	IHRFee          Coins            `tlb:"."`
	FwdFee          Coins            `tlb:"."`
	CreatedLT       uint64           `tlb:"## 64"`
	CreatedAt       uint32           `tlb:"## 32"`

	StateInit *StateInit `tlb:"maybe either leave 1,1 . ^"`
	Body      *cell.Cell `tlb:"either . ^"`
}

type ExternalMessageIn = ExternalMessage
type ExternalMessage struct {
	_         Magic            `tlb:"$10"`
	SrcAddr   *address.Address `tlb:"addr"`
	DstAddr   *address.Address `tlb:"addr"`
	ImportFee Coins            `tlb:"."`

	StateInit *StateInit `tlb:"maybe either leave 1,1 . ^"`
	Body      *cell.Cell `tlb:"either . ^"`
}

type ExternalMessageOut struct {
	_         Magic            `tlb:"$11"`
	SrcAddr   *address.Address `tlb:"addr"`
	DstAddr   *address.Address `tlb:"addr"`
	CreatedLT uint64           `tlb:"## 64"`
	CreatedAt uint32           `tlb:"## 32"`

	StateInit *StateInit `tlb:"maybe either leave 1,1 . ^"`
	Body      *cell.Cell `tlb:"either . ^"`
}

func (m *InternalMessage) Payload() *cell.Cell {
	return m.Body
}

func (m *InternalMessage) SenderAddr() *address.Address {
	return m.SrcAddr
}

func (m *InternalMessage) DestAddr() *address.Address {
	return m.DstAddr
}

func (m *InternalMessage) Comment() string {
	if m.Body != nil {
		l := m.Body.BeginParse()
		if val, err := l.LoadUInt(32); err == nil && val == 0 {
			str, _ := l.LoadStringSnake()
			return str
		}
	}
	return ""
}

func (m *ExternalMessage) NormalizedHash() []byte {
	body := m.Body
	if body == nil {
		// to not panic when body is nil
		body = cell.BeginCell().EndCell()
	}

	return cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreAddr(nil). // no src addr
		MustStoreAddr(m.DstAddr).
		MustStoreCoins(0).       // no import fee
		MustStoreBoolBit(false). // no state init
		MustStoreBoolBit(true).  // body always in ref
		MustStoreRef(body).
		EndCell().Hash()
}

func (m *ExternalMessage) Payload() *cell.Cell {
	return m.Body
}

func (m *ExternalMessage) SenderAddr() *address.Address {
	return m.SrcAddr
}

func (m *ExternalMessage) DestAddr() *address.Address {
	return m.DstAddr
}

func (m *ExternalMessageOut) Payload() *cell.Cell {
	return m.Body
}

func (m *ExternalMessageOut) SenderAddr() *address.Address {
	return m.SrcAddr
}

func (m *ExternalMessageOut) DestAddr() *address.Address {
	return m.DstAddr
}

func (m *Message) LoadFromCell(loader *cell.Slice) error {
	dup := loader.Copy()

	isExternal, err := dup.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load external flag: %w", err)
	}

	switch isExternal {
	case false:
		var intMsg InternalMessage
		err = LoadFromCell(&intMsg, loader)
		if err != nil {
			return fmt.Errorf("failed to parse internal message: %w", err)
		}

		m.Msg = &intMsg
		m.MsgType = MsgTypeInternal
		return nil
	case true:
		isOut, err := dup.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load external in/out flag: %w", err)
		}

		switch isOut {
		case true:
			var extMsg ExternalMessageOut
			err = LoadFromCell(&extMsg, loader)
			if err != nil {
				return fmt.Errorf("failed to parse external out message: %w", err)
			}

			m.Msg = &extMsg
			m.MsgType = MsgTypeExternalOut
			return nil
		case false:
			var extMsg ExternalMessage
			err = LoadFromCell(&extMsg, loader)
			if err != nil {
				return fmt.Errorf("failed to parse external in message: %w", err)
			}

			m.Msg = &extMsg
			m.MsgType = MsgTypeExternalIn
			return nil
		}
	}

	return errors.New("unknown message type")
}

func (m *Message) AsInternal() *InternalMessage {
	return m.Msg.(*InternalMessage)
}

func (m *Message) AsExternalIn() *ExternalMessage {
	return m.Msg.(*ExternalMessage)
}

func (m *Message) AsExternalOut() *ExternalMessageOut {
	return m.Msg.(*ExternalMessageOut)
}

func (m *InternalMessage) Dump() string {
	return fmt.Sprintf("Amount %s TON, Created at: %d, Created lt %d\nBounce: %t, Bounced %t, IHRDisabled %t\nSrcAddr: %s\nDstAddr: %s\nPayload: %s",
		m.Amount.String(), m.CreatedAt, m.CreatedLT, m.Bounce, m.Bounced, m.IHRDisabled, m.SrcAddr, m.DstAddr, m.Body.Dump())
}

func (m *MessagesList) ToSlice() ([]Message, error) {
	if m.List == nil {
		return nil, nil
	}

	kvs, err := m.List.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load messages dict: %w", err)
	}

	var list []Message
	for i, kv := range kvs {
		var msg Message
		ms, err := kv.Value.LoadRef()
		if err != nil {
			return nil, fmt.Errorf("failed to load ref of message %d: %w", i, err)
		}

		err = msg.LoadFromCell(ms)
		if err != nil {
			return nil, fmt.Errorf("failed to parse message %d: %w", i, err)
		}
		list = append(list, msg)
	}
	return list, nil
}
