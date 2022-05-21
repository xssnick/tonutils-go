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

type Message struct {
	MsgType MsgType
	msg     interface{}
}

type InternalMessage struct {
	IHRDisabled     bool
	Bounce          bool
	Bounced         bool
	SrcAddr         *address.Address
	DstAddr         *address.Address
	Amount          Grams
	ExtraCurrencies *Hashmap
	IHRFee          Grams
	FwdFee          Grams
	CreatedLT       uint64
	CreatedAt       uint32

	StateInit *StateInit
	Body      *cell.Cell
}

type ExternalMessageIn struct {
	SrcAddr   *address.Address
	DestAddr  *address.Address
	ImportFee Grams

	StateInit *StateInit
	Body      *cell.Cell
}

type ExternalMessageOut struct {
	SrcAddr   *address.Address
	DestAddr  *address.Address
	CreatedLT uint64
	CreatedAt uint32

	StateInit *StateInit
	Body      *cell.Cell
}

func (m *Message) LoadFromCell(loader *cell.LoadCell) error {
	isExternal, err := loader.LoadBoolBit()
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

		m.msg = &intMsg
		m.MsgType = MsgTypeInternal
		return nil
	case true:
		isOut, err := loader.LoadBoolBit()
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

			m.msg = &extMsg
			m.MsgType = MsgTypeExternalOut
			return nil
		case false:
			var extMsg ExternalMessageIn
			err = extMsg.LoadFromCell(loader)
			if err != nil {
				return fmt.Errorf("failed to parse external in message: %w", err)
			}

			m.msg = &extMsg
			m.MsgType = MsgTypeExternalIn
			return nil
		}
	}

	return errors.New("unknown message type")
}

func (m *Message) SenderAddr() *address.Address {
	switch m.MsgType {
	case MsgTypeInternal:
		return m.AsInternal().SrcAddr
	case MsgTypeExternalIn:
		return m.AsExternalIn().SrcAddr
	case MsgTypeExternalOut:
		return m.AsExternalOut().SrcAddr
	}
	return nil
}

func (m *Message) Payload() *cell.Cell {
	switch m.MsgType {
	case MsgTypeInternal:
		return m.AsInternal().Body
	case MsgTypeExternalIn:
		return m.AsExternalIn().Body
	case MsgTypeExternalOut:
		return m.AsExternalOut().Body
	}
	return nil
}

func (m *Message) DestAddr() *address.Address {
	switch m.MsgType {
	case MsgTypeInternal:
		return m.AsInternal().DstAddr
	case MsgTypeExternalIn:
		return m.AsExternalIn().DestAddr
	case MsgTypeExternalOut:
		return m.AsExternalOut().DestAddr
	}
	return nil
}

func (m *Message) AsInternal() *InternalMessage {
	return m.msg.(*InternalMessage)
}

func (m *Message) AsExternalIn() *ExternalMessageIn {
	return m.msg.(*ExternalMessageIn)
}

func (m *Message) AsExternalOut() *ExternalMessageOut {
	return m.msg.(*ExternalMessageOut)
}

func (m *InternalMessage) LoadFromCell(loader *cell.LoadCell) error {
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

	var extra *Hashmap
	if hasExtraCurrencies {
		extra = &Hashmap{}

		root, err := loader.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load extra currencies ref: %w", err)
		}

		err = extra.LoadFromCell(32, root)
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
		Amount:          Grams{val: value},
		ExtraCurrencies: extra,
		IHRFee:          Grams{val: ihrFee},
		FwdFee:          Grams{val: fwdFee},
		CreatedLT:       createdLt,
		CreatedAt:       uint32(createdAt),
		StateInit:       init,
		Body:            body,
	}

	return nil
}

func (m *InternalMessage) Dump() string {
	return fmt.Sprintf("Amount %s TON, Created at: %d, Created lt %d\nBounce: %t, Bounced %t, IHRDisabled %t\nSrcAddr: %s\nDstAddr: %s\nPayload: %s",
		m.Amount.TON(), m.CreatedAt, m.CreatedLT, m.Bounce, m.Bounced, m.IHRDisabled, m.SrcAddr, m.DstAddr, m.Body.Dump())
}

func (m *ExternalMessageIn) LoadFromCell(loader *cell.LoadCell) error {
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
		DestAddr:  dstAddr,
		ImportFee: Grams{ihrFee},
		StateInit: init,
		Body:      body,
	}
	return nil
}

func (m *ExternalMessageOut) LoadFromCell(loader *cell.LoadCell) error {
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
		DestAddr:  dstAddr,
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
