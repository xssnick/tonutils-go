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
	MsgType MsgType
	Msg     AnyMessage
}

type InternalMessage struct {
	_               Magic            `tlb:"$0"`
	IHRDisabled     bool             `tlb:"bool"`
	Bounce          bool             `tlb:"bool"`
	Bounced         bool             `tlb:"bool"`
	SrcAddr         *address.Address `tlb:"addr"`
	DstAddr         *address.Address `tlb:"addr"`
	Amount          *Grams           `tlb:"."`
	ExtraCurrencies *cell.Dictionary `tlb:"maybe ^dict 32"`
	IHRFee          *Grams           `tlb:"."`
	FwdFee          *Grams           `tlb:"."`
	CreatedLT       uint64           `tlb:"## 64"`
	CreatedAt       uint32           `tlb:"## 32"`

	StateInit *StateInit `tlb:"maybe either . ^"`
	Body      *cell.Cell `tlb:"either . ^"`
}

type ExternalMessageIn struct {
	_         Magic            `tlb:"$10"`
	SrcAddr   *address.Address `tlb:"addr"`
	DstAddr   *address.Address `tlb:"addr"`
	ImportFee *Grams           `tlb:"."`

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
			b, err := l.LoadSlice(l.BitsLeft())
			if err == nil {
				return string(b)
			}
		}
	}
	return ""
}

func (m *ExternalMessageIn) Payload() *cell.Cell {
	return m.Body
}

func (m *ExternalMessageIn) SenderAddr() *address.Address {
	return m.SrcAddr
}

func (m *ExternalMessageIn) DestAddr() *address.Address {
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

func (m *Message) LoadFromCell(loader *cell.LoadCell) error {
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
			var extMsg ExternalMessageIn
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

func (m *Message) AsExternalIn() *ExternalMessageIn {
	return m.Msg.(*ExternalMessageIn)
}

func (m *Message) AsExternalOut() *ExternalMessageOut {
	return m.Msg.(*ExternalMessageOut)
}

func (m *InternalMessage) ToCell() (*cell.Cell, error) {
	b := cell.BeginCell()
	b.MustStoreUInt(0, 1) // identification of int msg
	b.MustStoreBoolBit(m.IHRDisabled)
	b.MustStoreBoolBit(m.Bounce)
	b.MustStoreBoolBit(m.Bounced)
	b.MustStoreAddr(m.SrcAddr)
	b.MustStoreAddr(m.DstAddr)
	if m.Amount != nil {
		b.MustStoreBigCoins(m.Amount.NanoTON())
	} else {
		b.MustStoreCoins(0)
	}

	if m.ExtraCurrencies != nil {
		return nil, errors.New("extra currencies serialization is not supported yet")
	}

	b.MustStoreBoolBit(m.ExtraCurrencies != nil)

	if m.IHRFee != nil {
		b.MustStoreBigCoins(m.IHRFee.NanoTON())
	} else {
		b.MustStoreCoins(0)
	}

	if m.FwdFee != nil {
		b.MustStoreBigCoins(m.FwdFee.NanoTON())
	} else {
		b.MustStoreCoins(0)
	}

	b.MustStoreUInt(m.CreatedLT, 64)
	b.MustStoreUInt(uint64(m.CreatedAt), 32)
	b.MustStoreBoolBit(m.StateInit != nil)
	if m.StateInit != nil {
		stateCell, err := m.StateInit.ToCell()
		if err != nil {
			return nil, err
		}

		b.MustStoreBoolBit(true)
		b.MustStoreRef(stateCell)
	}

	if m.Body != nil {
		if b.BitsLeft() < m.Body.BitsSize() {
			b.MustStoreBoolBit(true)
			b.MustStoreRef(m.Body)
		} else {
			b.MustStoreBoolBit(false)
			b.MustStoreBuilder(m.Body.ToBuilder())
		}
	} else {
		// store 1 zero bit as body
		b.MustStoreBoolBit(false)
		b.MustStoreUInt(0, 1)
	}

	return b.EndCell(), nil
}

func (m *InternalMessage) Dump() string {
	return fmt.Sprintf("Amount %s TON, Created at: %d, Created lt %d\nBounce: %t, Bounced %t, IHRDisabled %t\nSrcAddr: %s\nDstAddr: %s\nPayload: %s",
		m.Amount.TON(), m.CreatedAt, m.CreatedLT, m.Bounce, m.Bounced, m.IHRDisabled, m.SrcAddr, m.DstAddr, m.Body.Dump())
}
