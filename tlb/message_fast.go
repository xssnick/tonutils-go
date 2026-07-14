package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

const maxInlineCellDepth = 1024

func (s *StateInit) LoadFromCell(loader *cell.Slice) error {
	hasDepth, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load depth presence: %w", err)
	}
	if hasDepth {
		depth, err := loader.LoadUInt(5)
		if err != nil {
			return fmt.Errorf("failed to load depth: %w", err)
		}
		s.Depth = &depth
	} else {
		s.Depth = nil
	}

	hasTickTock, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load tick-tock presence: %w", err)
	}
	if hasTickTock {
		tick, err := loader.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load tick flag: %w", err)
		}
		tock, err := loader.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load tock flag: %w", err)
		}
		s.TickTock = &TickTock{
			Tick: tick,
			Tock: tock,
		}
	} else {
		s.TickTock = nil
	}

	s.Code, err = loadMaybeRefCell(loader)
	if err != nil {
		return fmt.Errorf("failed to load code ref: %w", err)
	}
	s.Data, err = loadMaybeRefCell(loader)
	if err != nil {
		return fmt.Errorf("failed to load data ref: %w", err)
	}
	s.Lib, err = loader.LoadDict(256)
	if err != nil {
		return fmt.Errorf("failed to load libraries: %w", err)
	}
	return nil
}

func (s StateInit) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if s.Depth == nil {
		if err := builder.StoreBoolBit(false); err != nil {
			return nil, fmt.Errorf("failed to store depth presence: %w", err)
		}
	} else {
		if err := builder.StoreBoolBit(true); err != nil {
			return nil, fmt.Errorf("failed to store depth presence: %w", err)
		}
		if err := builder.StoreUInt(*s.Depth, 5); err != nil {
			return nil, fmt.Errorf("failed to store depth: %w", err)
		}
	}

	if s.TickTock == nil {
		if err := builder.StoreBoolBit(false); err != nil {
			return nil, fmt.Errorf("failed to store tick-tock presence: %w", err)
		}
	} else {
		if err := builder.StoreBoolBit(true); err != nil {
			return nil, fmt.Errorf("failed to store tick-tock presence: %w", err)
		}
		if err := builder.StoreBoolBit(s.TickTock.Tick); err != nil {
			return nil, fmt.Errorf("failed to store tick flag: %w", err)
		}
		if err := builder.StoreBoolBit(s.TickTock.Tock); err != nil {
			return nil, fmt.Errorf("failed to store tock flag: %w", err)
		}
	}

	if err := builder.StoreMaybeRef(s.Code); err != nil {
		return nil, fmt.Errorf("failed to store code ref: %w", err)
	}
	if err := builder.StoreMaybeRef(s.Data); err != nil {
		return nil, fmt.Errorf("failed to store data ref: %w", err)
	}
	if err := builder.StoreDict(s.Lib); err != nil {
		return nil, fmt.Errorf("failed to store libraries: %w", err)
	}
	return builder.EndCell(), nil
}

func loadMaybeRefCell(loader *cell.Slice) (*cell.Cell, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return loader.LoadRefCell()
}

func loadMaybeStateInitEither(loader *cell.Slice) (*StateInit, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	inRef, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if inRef {
		ref, err := loader.LoadRef()
		if err != nil {
			return nil, err
		}
		var state StateInit
		if err = state.LoadFromCell(ref); err != nil {
			return nil, err
		}
		return &state, nil
	}

	var state StateInit
	if err = state.LoadFromCell(loader); err != nil {
		return nil, err
	}
	return &state, nil
}

func storeMaybeStateInitEither(builder *cell.Builder, state *StateInit) error {
	if state == nil {
		return builder.StoreBoolBit(false)
	}

	stateCell, err := state.ToCell()
	if err != nil {
		return err
	}
	if err = builder.StoreBoolBit(true); err != nil {
		return err
	}

	if canStoreInlineCell(builder, stateCell, 1, 1) {
		if err = builder.StoreBoolBit(false); err != nil {
			return err
		}
		return builder.StoreBuilder(stateCell.ToBuilder())
	}

	if err = builder.StoreBoolBit(true); err != nil {
		return err
	}
	return builder.StoreRef(stateCell)
}

func loadMessageBody(loader *cell.Slice) (*cell.Cell, error) {
	inRef, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if inRef {
		return loader.LoadRefCell()
	}

	body, err := loader.ToCell()
	if err != nil {
		return nil, err
	}
	if err = loader.SkipBitsAndRefs(loader.BitsLeft(), loader.RefsNum()); err != nil {
		return nil, err
	}
	return body, nil
}

func storeMessageBody(builder *cell.Builder, body *cell.Cell) error {
	if body == nil {
		body = cell.BeginCell().EndCell()
	}

	if canStoreInlineCell(builder, body, 0, 0) {
		if err := builder.StoreBoolBit(false); err != nil {
			return err
		}
		return builder.StoreBuilder(body.ToBuilder())
	}

	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return builder.StoreRef(body)
}

func canStoreInlineCell(builder *cell.Builder, c *cell.Cell, leaveBits uint, leaveRefs uint) bool {
	return c.Depth() < maxInlineCellDepth &&
		builder.BitsLeft() >= 1+c.BitsSize()+leaveBits &&
		builder.RefsLeft() >= c.RefsNum()+leaveRefs
}

func (m *InternalMessage) LoadFromCell(loader *cell.Slice) error {
	isInternal, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load internal message magic: %w", err)
	}
	if isInternal {
		return fmt.Errorf("invalid internal message magic")
	}
	return m.loadFromCellAfterMagic(loader)
}

func (m *InternalMessage) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	m.IHRDisabled, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load ihr_disabled flag: %w", err)
	}
	m.Bounce, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounce flag: %w", err)
	}
	m.Bounced, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounced flag: %w", err)
	}
	m.SrcAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load source address: %w", err)
	}
	m.DstAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load destination address: %w", err)
	}
	m.Amount, err = loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load amount: %w", err)
	}
	m.ExtraCurrencies, err = loader.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load extra currencies: %w", err)
	}
	m.IHRFee, err = loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load ihr fee: %w", err)
	}
	m.FwdFee, err = loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load fwd fee: %w", err)
	}
	m.CreatedLT, err = loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load created lt: %w", err)
	}
	createdAt, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load created at: %w", err)
	}
	m.CreatedAt = uint32(createdAt)

	m.StateInit, err = loadMaybeStateInitEither(loader)
	if err != nil {
		return fmt.Errorf("failed to load state init: %w", err)
	}
	m.Body, err = loadMessageBody(loader)
	if err != nil {
		return fmt.Errorf("failed to load body: %w", err)
	}
	return nil
}

func (m InternalMessage) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreBoolBit(false); err != nil {
		return nil, fmt.Errorf("failed to store internal message magic: %w", err)
	}
	if err := builder.StoreBoolBit(m.IHRDisabled); err != nil {
		return nil, fmt.Errorf("failed to store ihr_disabled flag: %w", err)
	}
	if err := builder.StoreBoolBit(m.Bounce); err != nil {
		return nil, fmt.Errorf("failed to store bounce flag: %w", err)
	}
	if err := builder.StoreBoolBit(m.Bounced); err != nil {
		return nil, fmt.Errorf("failed to store bounced flag: %w", err)
	}
	if err := builder.StoreAddr(m.SrcAddr); err != nil {
		return nil, fmt.Errorf("failed to store source address: %w", err)
	}
	if err := builder.StoreAddr(m.DstAddr); err != nil {
		return nil, fmt.Errorf("failed to store destination address: %w", err)
	}
	if err := storeCoins(builder, m.Amount); err != nil {
		return nil, fmt.Errorf("failed to store amount: %w", err)
	}
	if err := builder.StoreDict(m.ExtraCurrencies); err != nil {
		return nil, fmt.Errorf("failed to store extra currencies: %w", err)
	}
	if err := storeCoins(builder, m.IHRFee); err != nil {
		return nil, fmt.Errorf("failed to store ihr fee: %w", err)
	}
	if err := storeCoins(builder, m.FwdFee); err != nil {
		return nil, fmt.Errorf("failed to store fwd fee: %w", err)
	}
	if err := builder.StoreUInt(m.CreatedLT, 64); err != nil {
		return nil, fmt.Errorf("failed to store created lt: %w", err)
	}
	if err := builder.StoreUInt(uint64(m.CreatedAt), 32); err != nil {
		return nil, fmt.Errorf("failed to store created at: %w", err)
	}
	if err := storeMaybeStateInitEither(builder, m.StateInit); err != nil {
		return nil, fmt.Errorf("failed to store state init: %w", err)
	}
	if err := storeMessageBody(builder, m.Body); err != nil {
		return nil, fmt.Errorf("failed to store body: %w", err)
	}
	return builder.EndCell(), nil
}

func (m *ExternalMessage) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load external inbound message magic: %w", err)
	}
	if magic != 0b10 {
		return fmt.Errorf("invalid external inbound message magic %b", magic)
	}
	return m.loadFromCellAfterMagic(loader)
}

func (m *ExternalMessage) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	m.SrcAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load source address: %w", err)
	}
	m.DstAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load destination address: %w", err)
	}
	m.ImportFee, err = loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load import fee: %w", err)
	}
	m.StateInit, err = loadMaybeStateInitEither(loader)
	if err != nil {
		return fmt.Errorf("failed to load state init: %w", err)
	}
	m.Body, err = loadMessageBody(loader)
	if err != nil {
		return fmt.Errorf("failed to load body: %w", err)
	}
	return nil
}

func (m ExternalMessage) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b10, 2); err != nil {
		return nil, fmt.Errorf("failed to store external inbound message magic: %w", err)
	}
	if err := builder.StoreAddr(m.SrcAddr); err != nil {
		return nil, fmt.Errorf("failed to store source address: %w", err)
	}
	if err := builder.StoreAddr(m.DstAddr); err != nil {
		return nil, fmt.Errorf("failed to store destination address: %w", err)
	}
	if err := storeCoins(builder, m.ImportFee); err != nil {
		return nil, fmt.Errorf("failed to store import fee: %w", err)
	}
	if err := storeMaybeStateInitEither(builder, m.StateInit); err != nil {
		return nil, fmt.Errorf("failed to store state init: %w", err)
	}
	if err := storeMessageBody(builder, m.Body); err != nil {
		return nil, fmt.Errorf("failed to store body: %w", err)
	}
	return builder.EndCell(), nil
}

func (m *ExternalMessageOut) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load external outbound message magic: %w", err)
	}
	if magic != 0b11 {
		return fmt.Errorf("invalid external outbound message magic %b", magic)
	}
	return m.loadFromCellAfterMagic(loader)
}

func (m *ExternalMessageOut) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	m.SrcAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load source address: %w", err)
	}
	m.DstAddr, err = loader.LoadAddr()
	if err != nil {
		return fmt.Errorf("failed to load destination address: %w", err)
	}
	m.CreatedLT, err = loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load created lt: %w", err)
	}
	createdAt, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load created at: %w", err)
	}
	m.CreatedAt = uint32(createdAt)

	m.StateInit, err = loadMaybeStateInitEither(loader)
	if err != nil {
		return fmt.Errorf("failed to load state init: %w", err)
	}
	m.Body, err = loadMessageBody(loader)
	if err != nil {
		return fmt.Errorf("failed to load body: %w", err)
	}
	return nil
}

func (m ExternalMessageOut) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b11, 2); err != nil {
		return nil, fmt.Errorf("failed to store external outbound message magic: %w", err)
	}
	if err := builder.StoreAddr(m.SrcAddr); err != nil {
		return nil, fmt.Errorf("failed to store source address: %w", err)
	}
	if err := builder.StoreAddr(m.DstAddr); err != nil {
		return nil, fmt.Errorf("failed to store destination address: %w", err)
	}
	if err := builder.StoreUInt(m.CreatedLT, 64); err != nil {
		return nil, fmt.Errorf("failed to store created lt: %w", err)
	}
	if err := builder.StoreUInt(uint64(m.CreatedAt), 32); err != nil {
		return nil, fmt.Errorf("failed to store created at: %w", err)
	}
	if err := storeMaybeStateInitEither(builder, m.StateInit); err != nil {
		return nil, fmt.Errorf("failed to store state init: %w", err)
	}
	if err := storeMessageBody(builder, m.Body); err != nil {
		return nil, fmt.Errorf("failed to store body: %w", err)
	}
	return builder.EndCell(), nil
}
