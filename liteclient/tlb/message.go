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
	IHRDisabled     bool
	Bounce          bool
	Bounced         bool
	SrcAddr         *address.Address
	DstAddr         *address.Address
	Amount          *Grams
	ExtraCurrencies *cell.Dictionary
	IHRFee          *Grams
	FwdFee          *Grams
	CreatedLT       uint64
	CreatedAt       uint32

	StateInit *StateInit
	Body      *cell.Cell
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

type ExternalMessageIn struct {
	SrcAddr   *address.Address
	DstAddr   *address.Address
	ImportFee *Grams

	StateInit *StateInit
	Body      *cell.Cell
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

type ExternalMessageOut struct {
	SrcAddr   *address.Address
	DstAddr   *address.Address
	CreatedLT uint64
	CreatedAt uint32

	StateInit *StateInit
	Body      *cell.Cell
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
		err = intMsg.LoadFromCell(loader)
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
			err = extMsg.LoadFromCell(loader)
			if err != nil {
				return fmt.Errorf("failed to parse external out message: %w", err)
			}

			m.Msg = &extMsg
			m.MsgType = MsgTypeExternalOut
			return nil
		case false:
			var extMsg ExternalMessageIn
			err = extMsg.LoadFromCell(loader)
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

func (m *InternalMessage) LoadFromCell(loader *cell.LoadCell) error {
	ident, err := loader.LoadUInt(1)
	if err != nil {
		return fmt.Errorf("failed to load identificator bit: %w", err)
	}

	if ident != 0 {
		return fmt.Errorf("its not internal message")
	}

	ihrDisabled, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load ihr disabled bit: %w", err)
	}

	bounce, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounce bit: %w", err)
	}

	bounced, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounced bit: %w", err)
	}

	srcAddr, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load src addr: %w", err)
	}

	dstAddr, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load dst addr: %w", err)
	}

	value, err := loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load amount: %w", err)
	}

	hasExtraCurrencies, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has extra currencies bit: %w", err)
	}

	var extra *cell.Dictionary
	if hasExtraCurrencies {
		root, err := loader.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load extra currencies ref: %w", err)
		}

		extra, err = root.LoadDict(32)
		if err != nil {
			return fmt.Errorf("failed to parse extra currencies hashmap: %w", err)
		}
	}

	ihrFee, err := loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load ihr fee: %w", err)
	}

	fwdFee, err := loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load fwd fee: %w", err)
	}

	createdLt, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load created lt: %w", err)
	}

	createdAt, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load created at: %w", err)
	}

	init, body, err := loadMsgTail(loader)
	if err != nil {
		return fmt.Errorf("failed to parse msg tail: %w", err)
	}

	*m = InternalMessage{
		IHRDisabled:     ihrDisabled,
		Bounce:          bounce,
		Bounced:         bounced,
		SrcAddr:         srcAddr,
		DstAddr:         dstAddr,
		Amount:          new(Grams).FromNanoTON(value),
		ExtraCurrencies: extra,
		IHRFee:          new(Grams).FromNanoTON(ihrFee),
		FwdFee:          new(Grams).FromNanoTON(fwdFee),
		CreatedLT:       createdLt,
		CreatedAt:       uint32(createdAt),
		StateInit:       init,
		Body:            body,
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
	if m.Amount != nil {
		b.MustStoreBigCoins(m.Amount.NanoTON())
	} else {
		b.MustStoreCoins(0)
	}

	if m.ExtraCurrencies != nil {
		return nil, errors.New("extra currencies serialization is not supported yet")
	}

	if m.StateInit != nil {
		return nil, errors.New("state init serialization is not supported yet")
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

func (m *ExternalMessageIn) LoadFromCell(loader *cell.LoadCell) error {
	ident, err := loader.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load identificator bit: %w", err)
	}

	if ident != 2 {
		return fmt.Errorf("its not external in message")
	}

	srcAddr, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load src addr: %w", err)
	}

	dstAddr, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load dst addr: %w", err)
	}

	ihrFee, err := loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load ihr fee: %w", err)
	}

	init, body, err := loadMsgTail(loader)
	if err != nil {
		return fmt.Errorf("failed to parse msg tail: %w", err)
	}

	*m = ExternalMessageIn{
		SrcAddr:   srcAddr,
		DstAddr:   dstAddr,
		ImportFee: new(Grams).FromNanoTON(ihrFee),
		StateInit: init,
		Body:      body,
	}
	return nil
}

func (m *ExternalMessageOut) LoadFromCell(loader *cell.LoadCell) error {
	ident, err := loader.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load identificator bit: %w", err)
	}

	if ident != 3 {
		return fmt.Errorf("its not external out message")
	}

	srcAddr, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load src addr: %w", err)
	}

	dstAddr, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load dst addr: %w", err)
	}

	createdLt, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load created lt: %w", err)
	}

	createdAt, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load created at: %w", err)
	}

	init, body, err := loadMsgTail(loader)
	if err != nil {
		return fmt.Errorf("failed to parse msg tail: %w", err)
	}

	*m = ExternalMessageOut{
		SrcAddr:   srcAddr,
		DstAddr:   dstAddr,
		CreatedLT: createdLt,
		CreatedAt: uint32(createdAt),
		StateInit: init,
		Body:      body,
	}
	return nil
}

func loadMsgTail(loader *cell.LoadCell) (*StateInit, *cell.Cell, error) {
	hasInit, err := loader.LoadBoolBit()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load has init bit: %w", err)
	}

	var init *StateInit
	if hasInit {
		isInitCell, err := loader.LoadBoolBit()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load is init cell bit: %w", err)
		}

		init = &StateInit{}

		from := loader
		if isInitCell {
			from, err = loader.LoadRef()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load init cell ref: %w", err)
			}
		}

		err = init.LoadFromCell(from)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse state init: %w", err)
		}
	}

	isBodyCell, err := loader.LoadBoolBit()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load is body cell bit: %w", err)
	}

	body := loader
	if isBodyCell {
		body, err = loader.LoadRef()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load body cell ref: %w", err)
		}
	}

	bodyCell, err := body.ToCell()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert body frrom slice to cell: %w", err)
	}

	return init, bodyCell, nil
}
