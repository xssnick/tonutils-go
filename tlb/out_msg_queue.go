package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type OutMsgQueueAugDict struct {
	*cell.AugmentedDictionary
}

type DispatchQueueAugDict struct {
	*cell.AugmentedDictionary
}

type OutMsgQueueInfo struct {
	OutQueue *OutMsgQueueAugDict
	ProcInfo *cell.Dictionary
	Extra    *OutMsgQueueExtra
}

type OutMsgQueueExtra struct {
	DispatchQueue *DispatchQueueAugDict
	OutQueueSize  *uint64
}

type AccountDispatchQueue struct {
	Messages *cell.Dictionary
	Count    uint64
}

type EnqueuedMsg struct {
	EnqueuedLT uint64
	Msg        *cell.Cell
}

type MsgMetadata struct {
	Depth       uint32
	Initiator   *address.Address
	InitiatorLT uint64
}

// NewDispatchQueueAugDict returns an empty writable DispatchQueue dictionary.
func NewDispatchQueueAugDict() (*DispatchQueueAugDict, error) {
	dict, err := cell.NewAugDict(256, AugDispatchQueue{})
	if err != nil {
		return nil, err
	}
	return &DispatchQueueAugDict{AugmentedDictionary: dict}, nil
}

// IntermediateAddressType selects the IntermediateAddress variant
// (crypto/block/block.tlb:161-165).
type IntermediateAddressType uint8

const (
	// IntermediateAddressRegular is interm_addr_regular$0 use_dest_bits:(#<= 96).
	IntermediateAddressRegular IntermediateAddressType = iota
	// IntermediateAddressSimple is interm_addr_simple$10 workchain_id:int8 addr_pfx:uint64.
	IntermediateAddressSimple
	// IntermediateAddressExt is interm_addr_ext$11 workchain_id:int32 addr_pfx:uint64.
	IntermediateAddressExt
)

// IntermediateAddress is the IntermediateAddress TLB type. The zero value is
// the regular variant with use_dest_bits = 0.
type IntermediateAddress struct {
	Type IntermediateAddressType

	// UseDestBits is set for the regular variant, must be <= 96.
	UseDestBits uint8

	// Workchain and AddrPfx are set for the simple and ext variants.
	// The simple variant limits Workchain to int8 range.
	Workchain int32
	AddrPfx   uint64
}

func (a *IntermediateAddress) LoadFromCell(loader *cell.Slice) error {
	isNotRegular, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load intermediate address tag: %w", err)
	}

	if !isNotRegular { // interm_addr_regular$0
		useDestBits, err := loader.LoadUInt(7)
		if err != nil {
			return fmt.Errorf("failed to load use dest bits: %w", err)
		}
		if useDestBits > 96 {
			return fmt.Errorf("use dest bits %d is above 96", useDestBits)
		}
		*a = IntermediateAddress{Type: IntermediateAddressRegular, UseDestBits: uint8(useDestBits)}
		return nil
	}

	isExt, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load intermediate address tag: %w", err)
	}

	workchainBits := uint(8) // interm_addr_simple$10 workchain_id:int8
	addrType := IntermediateAddressSimple
	if isExt { // interm_addr_ext$11 workchain_id:int32
		workchainBits = 32
		addrType = IntermediateAddressExt
	}

	workchain, err := loader.LoadInt(workchainBits)
	if err != nil {
		return fmt.Errorf("failed to load intermediate address workchain: %w", err)
	}
	if addrType == IntermediateAddressExt && workchain >= -128 && workchain < 128 {
		return fmt.Errorf("workchain %d must use interm_addr_simple", workchain)
	}
	addrPfx, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load intermediate address prefix: %w", err)
	}

	*a = IntermediateAddress{Type: addrType, Workchain: int32(workchain), AddrPfx: addrPfx}
	return nil
}

func (a IntermediateAddress) ToCell() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := a.storeTo(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (a IntermediateAddress) storeTo(b *cell.Builder) error {
	switch a.Type {
	case IntermediateAddressRegular:
		if a.UseDestBits > 96 {
			return fmt.Errorf("use dest bits %d is above 96", a.UseDestBits)
		}
		if err := b.StoreBoolBit(false); err != nil {
			return err
		}
		return b.StoreUInt(uint64(a.UseDestBits), 7)
	case IntermediateAddressSimple:
		if a.Workchain < -128 || a.Workchain > 127 {
			return fmt.Errorf("workchain %d does not fit int8 of interm_addr_simple", a.Workchain)
		}
		if err := b.StoreUInt(0b10, 2); err != nil {
			return err
		}
		if err := b.StoreInt(int64(a.Workchain), 8); err != nil {
			return err
		}
		return b.StoreUInt(a.AddrPfx, 64)
	case IntermediateAddressExt:
		if a.Workchain >= -128 && a.Workchain < 128 {
			return fmt.Errorf("workchain %d must use interm_addr_simple", a.Workchain)
		}
		if err := b.StoreUInt(0b11, 2); err != nil {
			return err
		}
		if err := b.StoreInt(int64(a.Workchain), 32); err != nil {
			return err
		}
		return b.StoreUInt(a.AddrPfx, 64)
	default:
		return fmt.Errorf("unknown intermediate address type %d", a.Type)
	}
}

// MsgEnvelope is the full MsgEnvelope TLB type:
//
//	msg_envelope#4 cur_addr:IntermediateAddress next_addr:IntermediateAddress
//	  fwd_fee_remaining:Grams msg:^(Message Any) = MsgEnvelope;
//
// The msg_envelope_v2#5 variant appends emitted_lt:(Maybe uint64) and
// metadata:(Maybe MsgMetadata) after the message reference.
type MsgEnvelope struct {
	CurAddr         IntermediateAddress
	NextAddr        IntermediateAddress
	FwdFeeRemaining Coins
	Msg             *cell.Cell

	// EmittedLT and Metadata exist only in msg_envelope_v2#5.
	EmittedLT *uint64
	Metadata  *MsgMetadata

	// V2 preserves an explicitly selected or decoded v2 wire variant when its
	// optional fields are absent.
	V2 bool
}

func (d *OutMsgQueueAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(352, AugOutMsgQueue{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OutMsgQueueAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(352, AugOutMsgQueue{}, true)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *DispatchQueueAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugDispatchQueue{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *DispatchQueueAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugDispatchQueue{}, true)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OutMsgQueueAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *DispatchQueueAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *OutMsgQueueAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (d *DispatchQueueAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (q *OutMsgQueueInfo) LoadFromCell(loader *cell.Slice) error {
	return q.load(loader, false)
}

func (q *OutMsgQueueInfo) LoadFromCellAsProof(loader *cell.Slice) error {
	return q.load(loader, true)
}

func (q OutMsgQueueInfo) ToCell() (*cell.Cell, error) {
	if q.OutQueue == nil || q.OutQueue.AugmentedDictionary == nil {
		return nil, fmt.Errorf("out message queue is nil")
	}
	if q.OutQueue.GetKeySize() != 352 {
		return nil, fmt.Errorf("out message queue has key size %d", q.OutQueue.GetKeySize())
	}
	if q.ProcInfo != nil && q.ProcInfo.GetKeySize() != 96 {
		return nil, fmt.Errorf("processed info has key size %d", q.ProcInfo.GetKeySize())
	}

	outQueue, err := q.OutQueue.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to store out message queue: %w", err)
	}

	builder := cell.BeginCell()
	if err = builder.StoreBuilder(outQueue.ToBuilder()); err != nil {
		return nil, fmt.Errorf("failed to store out message queue: %w", err)
	}
	if err = builder.StoreDict(q.ProcInfo); err != nil {
		return nil, fmt.Errorf("failed to store processed info: %w", err)
	}
	if err = builder.StoreBoolBit(q.Extra != nil); err != nil {
		return nil, fmt.Errorf("failed to store out message queue extra flag: %w", err)
	}
	if q.Extra != nil {
		extra, err := q.Extra.ToCell()
		if err != nil {
			return nil, fmt.Errorf("failed to store out message queue extra: %w", err)
		}
		if err = builder.StoreBuilder(extra.ToBuilder()); err != nil {
			return nil, fmt.Errorf("failed to store out message queue extra: %w", err)
		}
	}
	return builder.EndCell(), nil
}

func (q *OutMsgQueueInfo) load(loader *cell.Slice, asProof bool) error {
	var outQueue OutMsgQueueAugDict
	if err := loadOutMsgQueueAugDict(&outQueue, loader, asProof); err != nil {
		return fmt.Errorf("failed to load out message queue: %w", err)
	}

	procInfo, err := loader.LoadDict(96)
	if err != nil {
		return fmt.Errorf("failed to load processed info: %w", err)
	}

	hasExtra, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load out message queue extra flag: %w", err)
	}

	q.OutQueue = &outQueue
	q.ProcInfo = procInfo
	q.Extra = nil
	if hasExtra {
		var extra OutMsgQueueExtra
		if err = extra.load(loader, asProof); err != nil {
			return fmt.Errorf("failed to load out message queue extra: %w", err)
		}
		q.Extra = &extra
	}

	return nil
}

func (q *OutMsgQueueExtra) LoadFromCell(loader *cell.Slice) error {
	return q.load(loader, false)
}

func (q *OutMsgQueueExtra) LoadFromCellAsProof(loader *cell.Slice) error {
	return q.load(loader, true)
}

func (q OutMsgQueueExtra) ToCell() (*cell.Cell, error) {
	if q.DispatchQueue == nil || q.DispatchQueue.AugmentedDictionary == nil {
		return nil, fmt.Errorf("dispatch queue is nil")
	}
	if q.DispatchQueue.GetKeySize() != 256 {
		return nil, fmt.Errorf("dispatch queue has key size %d", q.DispatchQueue.GetKeySize())
	}
	if q.OutQueueSize != nil && *q.OutQueueSize >= 1<<48 {
		return nil, fmt.Errorf("out queue size %d does not fit uint48", *q.OutQueueSize)
	}

	dispatchQueue, err := q.DispatchQueue.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to store dispatch queue: %w", err)
	}

	builder := cell.BeginCell().MustStoreUInt(0, 4)
	if err = builder.StoreBuilder(dispatchQueue.ToBuilder()); err != nil {
		return nil, fmt.Errorf("failed to store dispatch queue: %w", err)
	}
	if err = builder.StoreBoolBit(q.OutQueueSize != nil); err != nil {
		return nil, fmt.Errorf("failed to store out queue size flag: %w", err)
	}
	if q.OutQueueSize != nil {
		if err = builder.StoreUInt(*q.OutQueueSize, 48); err != nil {
			return nil, fmt.Errorf("failed to store out queue size: %w", err)
		}
	}
	return builder.EndCell(), nil
}

func (q *OutMsgQueueExtra) load(loader *cell.Slice, asProof bool) error {
	if !checkMagic("#0", loader) {
		return fmt.Errorf("out message queue extra magic is not correct")
	}

	var dispatchQueue DispatchQueueAugDict
	if err := loadDispatchQueueAugDict(&dispatchQueue, loader, asProof); err != nil {
		return fmt.Errorf("failed to load dispatch queue: %w", err)
	}

	hasOutQueueSize, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load out queue size flag: %w", err)
	}

	q.DispatchQueue = &dispatchQueue
	q.OutQueueSize = nil
	if hasOutQueueSize {
		size, err := loader.LoadUInt(48)
		if err != nil {
			return fmt.Errorf("failed to load out queue size: %w", err)
		}
		q.OutQueueSize = &size
	}

	return nil
}

func (q *AccountDispatchQueue) LoadFromCell(loader *cell.Slice) error {
	messages, err := loader.LoadDict(64)
	if err != nil {
		return fmt.Errorf("failed to load account dispatch messages: %w", err)
	}

	count, err := loader.LoadUInt(48)
	if err != nil {
		return fmt.Errorf("failed to load account dispatch messages count: %w", err)
	}
	if count == 0 || messages.IsEmpty() {
		return fmt.Errorf("account dispatch queue must contain messages and a non-zero count")
	}

	q.Messages = messages
	q.Count = count
	return nil
}

func (q AccountDispatchQueue) ToCell() (*cell.Cell, error) {
	if q.Messages.IsEmpty() || q.Count == 0 {
		return nil, fmt.Errorf("account dispatch queue must contain messages and a non-zero count")
	}
	if q.Messages.GetKeySize() != 64 {
		return nil, fmt.Errorf("account dispatch messages have key size %d", q.Messages.GetKeySize())
	}
	if q.Count >= 1<<48 {
		return nil, fmt.Errorf("account dispatch message count %d does not fit uint48", q.Count)
	}

	builder := cell.BeginCell()
	if err := builder.StoreDict(q.Messages); err != nil {
		return nil, fmt.Errorf("failed to store account dispatch messages: %w", err)
	}
	// The count fits by the guard above and 49 bits always fit the builder.
	builder.MustStoreUInt(q.Count, 48)
	return builder.EndCell(), nil
}

func (m *EnqueuedMsg) LoadFromCell(loader *cell.Slice) error {
	lt, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load enqueued lt: %w", err)
	}

	msg, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load enqueued message envelope: %w", err)
	}

	m.EnqueuedLT = lt
	m.Msg = msg
	return nil
}

func (m EnqueuedMsg) ToCell() (*cell.Cell, error) {
	if m.Msg == nil {
		return nil, fmt.Errorf("enqueued message envelope is nil")
	}
	b := cell.BeginCell()
	if err := b.StoreUInt(m.EnqueuedLT, 64); err != nil {
		return nil, fmt.Errorf("failed to store enqueued lt: %w", err)
	}
	if err := b.StoreRef(m.Msg); err != nil {
		return nil, fmt.Errorf("failed to store enqueued message envelope: %w", err)
	}
	return b.EndCell(), nil
}

func (m *MsgEnvelope) LoadFromCell(loader *cell.Slice) error {
	tag, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load message envelope tag: %w", err)
	}
	if tag != 4 && tag != 5 {
		return fmt.Errorf("unsupported message envelope tag %d", tag)
	}

	var curAddr, nextAddr IntermediateAddress
	if err = curAddr.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load current intermediate address: %w", err)
	}
	if err = nextAddr.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load next intermediate address: %w", err)
	}

	fwdFeeRemaining, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load remaining forward fee: %w", err)
	}

	msg, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load message ref: %w", err)
	}

	m.CurAddr = curAddr
	m.NextAddr = nextAddr
	m.FwdFeeRemaining = fwdFeeRemaining
	m.Msg = msg
	m.EmittedLT = nil
	m.Metadata = nil
	m.V2 = tag == 5
	if tag == 4 {
		return nil
	}

	hasEmittedLT, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load emitted lt flag: %w", err)
	}
	if hasEmittedLT {
		emittedLT, err := loader.LoadUInt(64)
		if err != nil {
			return fmt.Errorf("failed to load emitted lt: %w", err)
		}
		m.EmittedLT = &emittedLT
	}

	hasMetadata, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load metadata flag: %w", err)
	}
	if hasMetadata {
		var metadata MsgMetadata
		if err = metadata.LoadFromCell(loader); err != nil {
			return fmt.Errorf("failed to load metadata: %w", err)
		}
		m.Metadata = &metadata
	}

	return nil
}

func (m MsgEnvelope) ToCell() (*cell.Cell, error) {
	if m.Msg == nil {
		return nil, fmt.Errorf("message envelope message is nil")
	}

	isV2 := m.V2 || m.EmittedLT != nil || m.Metadata != nil

	b := cell.BeginCell()
	tag := uint64(4)
	if isV2 {
		tag = 5
	}
	if err := b.StoreUInt(tag, 4); err != nil {
		return nil, fmt.Errorf("failed to store message envelope tag: %w", err)
	}
	if err := m.CurAddr.storeTo(b); err != nil {
		return nil, fmt.Errorf("failed to store current intermediate address: %w", err)
	}
	if err := m.NextAddr.storeTo(b); err != nil {
		return nil, fmt.Errorf("failed to store next intermediate address: %w", err)
	}
	if err := storeCoins(b, m.FwdFeeRemaining); err != nil {
		return nil, fmt.Errorf("failed to store remaining forward fee: %w", err)
	}
	if err := b.StoreRef(m.Msg); err != nil {
		return nil, fmt.Errorf("failed to store message ref: %w", err)
	}

	if !isV2 {
		return b.EndCell(), nil
	}

	if err := b.StoreBoolBit(m.EmittedLT != nil); err != nil {
		return nil, fmt.Errorf("failed to store emitted lt flag: %w", err)
	}
	if m.EmittedLT != nil {
		if err := b.StoreUInt(*m.EmittedLT, 64); err != nil {
			return nil, fmt.Errorf("failed to store emitted lt: %w", err)
		}
	}

	if err := b.StoreBoolBit(m.Metadata != nil); err != nil {
		return nil, fmt.Errorf("failed to store metadata flag: %w", err)
	}
	if m.Metadata != nil {
		if err := m.Metadata.storeTo(b); err != nil {
			return nil, fmt.Errorf("failed to store metadata: %w", err)
		}
	}

	return b.EndCell(), nil
}

func (m *MsgMetadata) LoadFromCell(loader *cell.Slice) error {
	if !checkMagic("#0", loader) {
		return fmt.Errorf("message metadata magic is not correct")
	}

	depth, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load metadata depth: %w", err)
	}

	initiator, err := loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load metadata initiator: %w", err)
	}
	if initiator.Type() != address.StdAddress || initiator.Anycast() != nil {
		return fmt.Errorf("metadata initiator is not a standard address without anycast")
	}

	initiatorLT, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load metadata initiator lt: %w", err)
	}

	m.Depth = uint32(depth)
	m.Initiator = initiator
	m.InitiatorLT = initiatorLT
	return nil
}

func (m MsgMetadata) ToCell() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := m.storeTo(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (m MsgMetadata) storeTo(b *cell.Builder) error {
	// msg_metadata#0 depth:uint32 initiator_addr:MsgAddressInt initiator_lt:uint64
	if m.Initiator == nil {
		return fmt.Errorf("metadata initiator is nil")
	}
	if m.Initiator.Type() != address.StdAddress || m.Initiator.Anycast() != nil {
		return fmt.Errorf("metadata initiator is not a standard address without anycast")
	}
	if err := b.StoreUInt(0, 4); err != nil {
		return fmt.Errorf("failed to store metadata magic: %w", err)
	}
	if err := b.StoreUInt(uint64(m.Depth), 32); err != nil {
		return fmt.Errorf("failed to store metadata depth: %w", err)
	}
	if err := b.StoreAddr(m.Initiator); err != nil {
		return fmt.Errorf("failed to store metadata initiator: %w", err)
	}
	if err := b.StoreUInt(m.InitiatorLT, 64); err != nil {
		return fmt.Errorf("failed to store metadata initiator lt: %w", err)
	}
	return nil
}

func loadOutMsgQueueAugDict(d *OutMsgQueueAugDict, loader *cell.Slice, asProof bool) error {
	if asProof {
		return d.LoadFromCellAsProof(loader)
	}
	return d.LoadFromCell(loader)
}

func loadDispatchQueueAugDict(d *DispatchQueueAugDict, loader *cell.Slice, asProof bool) error {
	if asProof {
		return d.LoadFromCellAsProof(loader)
	}
	return d.LoadFromCell(loader)
}

func skipUint64Boundary(loader *cell.Slice) error {
	_, err := loader.LoadUInt(64)
	return err
}

// skipIntermediateAddress skips any IntermediateAddress variant:
// regular$0 is 1+7 bits, simple$10 is 2+8+64 bits, ext$11 is 2+32+64 bits.
func skipIntermediateAddress(loader *cell.Slice) error {
	isNotRegular, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if !isNotRegular {
		useDestBits, err := loader.LoadUInt(7)
		if err != nil {
			return err
		}
		if useDestBits > 96 {
			return fmt.Errorf("use dest bits %d is above 96", useDestBits)
		}
		return nil
	}

	isExt, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	sz := uint(8 + 64) // interm_addr_simple$10
	if isExt {
		sz = 32 + 64 // interm_addr_ext$11
	}
	_, err = loader.LoadSlice(sz)
	return err
}
