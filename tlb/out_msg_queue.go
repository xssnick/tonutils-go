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

type MsgEnvelope struct {
	Msg      *cell.Cell
	Metadata *MsgMetadata
}

func (d *OutMsgQueueAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(352, cell.ReadOnlyAugmentation{SkipExtraFn: skipUint64Boundary}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OutMsgQueueAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(352, cell.ReadOnlyAugmentation{SkipExtraFn: skipUint64Boundary}, true)
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

func (m *MsgEnvelope) LoadFromCell(loader *cell.Slice) error {
	tag, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load message envelope tag: %w", err)
	}
	if tag != 4 && tag != 5 {
		return fmt.Errorf("unsupported message envelope tag %d", tag)
	}

	if err = skipRegularIntermediateAddress(loader); err != nil {
		return fmt.Errorf("failed to load current intermediate address: %w", err)
	}
	if err = skipRegularIntermediateAddress(loader); err != nil {
		return fmt.Errorf("failed to load next intermediate address: %w", err)
	}
	if _, err = loader.LoadBigCoins(); err != nil {
		return fmt.Errorf("failed to load remaining forward fee: %w", err)
	}

	msg, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load message ref: %w", err)
	}

	m.Msg = msg
	m.Metadata = nil
	if tag == 4 {
		return nil
	}

	hasEmittedLT, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load emitted lt flag: %w", err)
	}
	if hasEmittedLT {
		if _, err = loader.LoadUInt(64); err != nil {
			return fmt.Errorf("failed to load emitted lt: %w", err)
		}
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

func skipRegularIntermediateAddress(loader *cell.Slice) error {
	useDestBits, err := loader.LoadUInt(8)
	if err != nil {
		return err
	}
	if useDestBits > 96 {
		return fmt.Errorf("intermediate address is not regular")
	}
	return nil
}
