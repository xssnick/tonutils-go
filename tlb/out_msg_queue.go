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

// IntermediateAddress is the IntermediateAddress TLB type
// (crypto/block/block.tlb:161-165, parsing per IntermediateAddress::get_size,
// crypto/block/block-parse.cpp:797-808). The zero value is the regular variant
// with use_dest_bits = 0.
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
// per crypto/block/block.tlb:167-169 and MsgEnvelope::unpack
// (crypto/block/block-parse.cpp:832-839), plus the current upstream
// msg_envelope_v2#5 with emitted_lt:(Maybe uint64) and
// metadata:(Maybe MsgMetadata) appended after the msg ref (the v2 layout is
// not present in the local ton checkout's block.tlb; it matches the format
// produced by current mainnet nodes and the previous parser of this package).
type MsgEnvelope struct {
	CurAddr         IntermediateAddress
	NextAddr        IntermediateAddress
	FwdFeeRemaining Coins
	Msg             *cell.Cell

	// EmittedLT and Metadata exist only in msg_envelope_v2#5.
	EmittedLT *uint64
	Metadata  *MsgMetadata

	// V2 reports whether the envelope was parsed from (or should be serialized
	// as) msg_envelope_v2#5. ToCell auto-upgrades to v2 when EmittedLT or
	// Metadata is set.
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
	dict, err := loader.LoadAugDict(256, cell.ReadOnlyAugmentation{SkipExtraFn: skipUint64Boundary}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *DispatchQueueAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, cell.ReadOnlyAugmentation{SkipExtraFn: skipUint64Boundary}, true)
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

	q.Messages = messages
	q.Count = count
	return nil
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

// skipIntermediateAddress skips any IntermediateAddress variant, mirroring
// IntermediateAddress::get_size (crypto/block/block-parse.cpp:797-808):
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
