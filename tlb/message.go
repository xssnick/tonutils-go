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

type AnyMessage interface {
	Payload() *cell.Cell
	SenderAddr() *address.Address
	DestAddr() *address.Address
}

type Message struct {
	MsgType MsgType    `tlb:"-"`
	Msg     AnyMessage `tlb:"."`
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

	StateInit *StateInit `tlb:"maybe either . ^"`
	Body      *cell.Cell `tlb:"either . ^"`
}

type ExternalMessage struct {
	_         Magic            `tlb:"$10"`
	SrcAddr   *address.Address `tlb:"addr"`
	DstAddr   *address.Address `tlb:"addr"`
	ImportFee Coins            `tlb:"."`

	StateInit *StateInit `tlb:"maybe either . ^"`
	Body      *cell.Cell `tlb:"either . ^"`
}

type ExternalMessageOut struct {
	_         Magic            `tlb:"$11"`
	SrcAddr   *address.Address `tlb:"addr"`
	DstAddr   *address.Address `tlb:"addr"`
	CreatedLT uint64           `tlb:"## 64"`
	CreatedAt uint32           `tlb:"## 32"`

	StateInit *StateInit `tlb:"maybe either . ^"`
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

func (m *Message) ToCell() (*cell.Cell, error) {
	switch m.MsgType {
	case MsgTypeInternal:
		return m.AsInternal().ToCell()
	case MsgTypeExternalIn:
		return m.AsExternalIn().ToCell()
	case MsgTypeExternalOut:
		return m.AsExternalOut().ToCell()
	default:
		return nil, errors.New("unknown message type")
	}
}

func appendInitStateAndBody(b *cell.Builder, stateInit *StateInit, body *cell.Cell) error {
	var err error
	if b.BitsLeft() < 3 {
		return fmt.Errorf("not enough storage to serialize state init and body")
	}
	b.MustStoreBoolBit(stateInit != nil)
	if stateInit != nil {
		stateCell, err := ToCell(stateInit)
		if err != nil {
			return fmt.Errorf("failed to serialize state init: %w", err)
		}

		if int(stateCell.BitsSize()) > int(b.BitsLeft())-2 || int(stateCell.RefsNum()) > int(b.RefsLeft())-1 {
			b.MustStoreBoolBit(true) // state as ref
			err = b.StoreRef(stateCell)
		} else {
			b.MustStoreBoolBit(false) // state as slice
			err = b.StoreBuilder(stateCell.ToBuilder())
		}
		if err != nil {
			return fmt.Errorf("failed to store message state init: %w", err)
		}
	}

	if body != nil {
		if int(body.BitsSize()) > int(b.BitsLeft())-1 || body.RefsNum() > b.RefsLeft() {
			b.MustStoreBoolBit(true) // body as ref
			err = b.StoreRef(body)
		} else {
			b.MustStoreBoolBit(false) // body as slice
			err = b.StoreBuilder(body.ToBuilder())
		}
		if err != nil {
			return fmt.Errorf("failed to store message body: %w", err)
		}
	} else {
		b.MustStoreBoolBit(false)
	}

	return nil
}

func (m *InternalMessage) ToCell() (*cell.Cell, error) {
	b := cell.BeginCell()
	b.MustStoreUInt(0, 1) // identification of int msg
	b.MustStoreBoolBit(m.IHRDisabled)
	b.MustStoreBoolBit(m.Bounce)
	b.MustStoreBoolBit(m.Bounced)
	b.MustStoreAddr(m.SrcAddr)
	b.MustStoreAddr(m.DstAddr)
	b.MustStoreBigCoins(m.Amount.Nano())

	b.MustStoreDict(m.ExtraCurrencies)

	b.MustStoreBigCoins(m.IHRFee.Nano())
	b.MustStoreBigCoins(m.FwdFee.Nano())

	b.MustStoreUInt(m.CreatedLT, 64)
	b.MustStoreUInt(uint64(m.CreatedAt), 32)

	err := appendInitStateAndBody(b, m.StateInit, m.Body)
	if err != nil {
		return nil, err
	}

	return b.EndCell(), nil
}

func (m *InternalMessage) Dump() string {
	return fmt.Sprintf("Amount %s TON, Created at: %d, Created lt %d\nBounce: %t, Bounced %t, IHRDisabled %t\nSrcAddr: %s\nDstAddr: %s\nPayload: %s",
		m.Amount.String(), m.CreatedAt, m.CreatedLT, m.Bounce, m.Bounced, m.IHRDisabled, m.SrcAddr, m.DstAddr, m.Body.Dump())
}

func (m *ExternalMessage) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell().MustStoreUInt(0b10, 2).
		MustStoreAddr(m.SrcAddr).
		MustStoreAddr(m.DstAddr).
		MustStoreBigCoins(m.ImportFee.Nano())

	err := appendInitStateAndBody(builder, m.StateInit, m.Body)
	if err != nil {
		return nil, err
	}

	return builder.EndCell(), nil
}

func (m *ExternalMessageOut) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell().MustStoreUInt(0b11, 2).
		MustStoreAddr(m.SrcAddr).
		MustStoreAddr(m.DstAddr).
		MustStoreUInt(m.CreatedLT, 64).
		MustStoreUInt(uint64(m.CreatedAt), 32)

	err := appendInitStateAndBody(builder, m.StateInit, m.Body)
	if err != nil {
		return nil, err
	}

	return builder.EndCell(), nil
}

func (m *MessagesList) ToSlice() ([]Message, error) {
	if m.List == nil {
		return nil, nil
	}

	var list []Message
	for i, kv := range m.List.All() {
		var msg Message
		s := kv.Value.BeginParse()
		ms, err := s.LoadRef()
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
