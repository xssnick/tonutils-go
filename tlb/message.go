package tlb

import (
	"errors"
	"fmt"
	"math/big"

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

	Register(ActionSendMsg{})
	Register(ActionSetCode{})
	Register(ActionReserveCurrency{})
	Register(LibRefHash{})
	Register(LibRefRef{})
	Register(ActionChangeLibrary{})
}

type AnyMessage interface {
	Payload() *cell.Cell
	SenderAddr() *address.Address
	DestAddr() *address.Address
}

type OutList struct {
	Prev *cell.Cell `tlb:"^"`
	Out  any        `tlb:"[ActionSendMsg,ActionSetCode,ActionReserveCurrency,ActionChangeLibrary]"`
}

type ActionSendMsg struct {
	_    Magic      `tlb:"#0ec3c86d"`
	Mode uint8      `tlb:"## 8"`
	Msg  *cell.Cell `tlb:"^"`
}

type ActionSetCode struct {
	_       Magic      `tlb:"#ad4de08e"`
	NewCode *cell.Cell `tlb:"^"`
}

type ActionReserveCurrency struct {
	_        Magic              `tlb:"#36e6b809"`
	Mode     uint8              `tlb:"## 8"`
	Currency CurrencyCollection `tlb:"."`
}

type LibRefHash struct {
	_       Magic  `tlb:"$0"`
	LibHash []byte `tlb:"bits 256"`
}

type LibRefRef struct {
	_       Magic      `tlb:"$1"`
	Library *cell.Cell `tlb:"^"`
}

type ActionChangeLibrary struct {
	_      Magic `tlb:"#26fa1dd4"`
	Mode   uint8 `tlb:"## 7"`
	LibRef any   `tlb:"[LibRefHash,LibRefRef]"`
}

type Message struct {
	MsgType MsgType    `tlb:"-"`
	Msg     AnyMessage `tlb:"[ExternalMessage,ExternalMessageOut,InternalMessage]"`
}

type MessageRelaxed struct {
	MsgType MsgType                 `tlb:"-"`
	Info    MessageRelaxedInfo      `tlb:"-"`
	Init    MessageRelaxedStateInit `tlb:"-"`
	Body    MessageRelaxedBody      `tlb:"-"`
}

type MessageRelaxedInfo struct {
	IHRDisabled     bool
	Bounce          bool
	Bounced         bool
	SrcAddr         *address.Address
	DstAddr         *address.Address
	Amount          *big.Int
	ExtraPresent    bool
	ExtraCurrencies *cell.Dictionary
	ExtraFlags      *big.Int
	FwdFee          *big.Int
	CreatedLT       uint64
	CreatedAt       uint32
}

type MessageRelaxedStateInit struct {
	Exists bool
	InRef  bool
	Ref    *cell.Cell
}

type MessageRelaxedBody struct {
	InRef bool
	Ref   *cell.Cell
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
		l, err := m.Body.BeginParse()
		if err != nil {
			return ""
		}
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

func (m *Message) ToCell() (*cell.Cell, error) {
	if m == nil || m.Msg == nil {
		return nil, errors.New("message is nil")
	}

	switch msg := m.Msg.(type) {
	case *InternalMessage:
		return ToCell(msg)
	case *ExternalMessage:
		return ToCell(msg)
	case *ExternalMessageOut:
		return ToCell(msg)
	default:
		return nil, fmt.Errorf("unsupported message type %T", m.Msg)
	}
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

func (m *MessageRelaxed) LoadFromCell(loader *cell.Slice) error {
	isExternal, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load external flag: %w", err)
	}

	if isExternal {
		isOut, err := loader.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load external direction flag: %w", err)
		}
		if !isOut {
			return errors.New("external inbound message is not relaxed")
		}
		m.MsgType = MsgTypeExternalOut
		if err = m.loadExternalOutRelaxedInfo(loader); err != nil {
			return err
		}
	} else {
		m.MsgType = MsgTypeInternal
		if err = m.loadInternalRelaxedInfo(loader); err != nil {
			return err
		}
	}

	if err = m.Init.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load relaxed message state init: %w", err)
	}
	if err = m.Body.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load relaxed message body: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return fmt.Errorf("relaxed message has trailing data: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}

	return nil
}

func (m *MessageRelaxed) loadInternalRelaxedInfo(loader *cell.Slice) error {
	var err error
	info := &m.Info
	info.IHRDisabled, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load ihr_disabled: %w", err)
	}
	info.Bounce, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounce: %w", err)
	}
	info.Bounced, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounced: %w", err)
	}
	info.SrcAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load source address: %w", err)
	}
	info.DstAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load destination address: %w", err)
	}
	info.Amount, err = loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load amount: %w", err)
	}
	info.ExtraPresent, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load extra currencies presence: %w", err)
	}
	if info.ExtraPresent {
		extraRoot, err := loader.LoadRefCell()
		if err != nil {
			return fmt.Errorf("failed to load extra currencies: %w", err)
		}
		info.ExtraCurrencies = extraRoot.AsDict(32)
	} else {
		info.ExtraCurrencies = cell.NewDict(32)
	}
	info.ExtraFlags, err = loader.LoadVarUInt(16)
	if err != nil {
		return fmt.Errorf("failed to load extra flags: %w", err)
	}
	info.FwdFee, err = loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load forward fee: %w", err)
	}
	info.CreatedLT, err = loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load created_lt: %w", err)
	}
	createdAt, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load created_at: %w", err)
	}
	info.CreatedAt = uint32(createdAt)
	return nil
}

func (m *MessageRelaxed) loadExternalOutRelaxedInfo(loader *cell.Slice) error {
	var err error
	info := &m.Info
	info.SrcAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load source address: %w", err)
	}
	info.DstAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load destination address: %w", err)
	}
	info.CreatedLT, err = loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load created_lt: %w", err)
	}
	createdAt, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load created_at: %w", err)
	}
	info.CreatedAt = uint32(createdAt)
	info.Amount = big.NewInt(0)
	info.ExtraFlags = big.NewInt(0)
	info.FwdFee = big.NewInt(0)
	return nil
}

func (m MessageRelaxedInfo) HasExtraCurrencies() bool {
	return m.ExtraPresent
}

func (m *MessageRelaxedStateInit) LoadFromCell(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if !has {
		return nil
	}

	m.Exists = true
	m.InRef, err = loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if m.InRef {
		m.Ref, err = loader.LoadRefCell()
		return err
	}

	return skipRelaxedInlineStateInit(loader)
}

func (m *MessageRelaxedBody) LoadFromCell(loader *cell.Slice) error {
	inRef, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	m.InRef = inRef
	if inRef {
		m.Ref, err = loader.LoadRefCell()
		return err
	}

	return loader.SkipBitsAndRefs(loader.BitsLeft(), loader.RefsNum())
}

func skipRelaxedInlineStateInit(loader *cell.Slice) error {
	hasFixedPrefix, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasFixedPrefix {
		if _, err = loader.LoadUInt(5); err != nil {
			return err
		}
	}

	hasSpecial, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasSpecial {
		if _, err = loader.LoadUInt(2); err != nil {
			return err
		}
	}

	if err = skipRelaxedMaybeRef(loader); err != nil {
		return err
	}
	if err = skipRelaxedMaybeRef(loader); err != nil {
		return err
	}
	return skipRelaxedMaybeRef(loader)
}

func skipRelaxedMaybeRef(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if !has {
		return nil
	}
	_, err = loader.LoadRefCell()
	return err
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

func (o *OutList) ToSlice() ([]any, error) {
	if o == nil {
		return nil, nil
	}
	prev, err := LoadOutList(o.Prev)
	if err != nil {
		return nil, err
	}
	return append(prev, o.Out), nil
}

func LoadOutList(root *cell.Cell) ([]any, error) {
	if root == nil {
		return nil, nil
	}
	if root.BitsSize() == 0 && root.RefsNum() == 0 {
		return nil, nil
	}

	var list OutList
	if err := Parse(&list, root); err != nil {
		return nil, err
	}
	return list.ToSlice()
}
