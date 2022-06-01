package tlb

import (
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TickTock struct {
	Tick bool
	Tock bool
}

type StateInit struct {
	Depth    *uint64
	TickTock *TickTock
	Data     *cell.Cell
	Code     *cell.Cell
	Lib      *cell.Dictionary
}

func (m *StateInit) LoadFromCell(loader *cell.LoadCell) error {
	hasDepth, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has depth: %w", err)
	}

	var depth *uint64
	if hasDepth {
		d, err := loader.LoadUInt(5)
		if err != nil {
			return fmt.Errorf("failed to load depth: %w", err)
		}
		depth = &d
	}

	hasTickTock, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has tick tock: %w", err)
	}

	var tickTock *TickTock
	if hasTickTock {
		isTick, err := loader.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load is tick: %w", err)
		}

		isTock, err := loader.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load is tock: %w", err)
		}

		tickTock = &TickTock{
			Tick: isTick,
			Tock: isTock,
		}
	}

	hasCode, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has code: %w", err)
	}

	var codeCell *cell.LoadCell
	if hasCode {
		codeCell, err = loader.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load code ref: %w", err)
		}
	}

	hasData, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has data: %w", err)
	}

	var dataCell *cell.LoadCell
	if hasData {
		dataCell, err = loader.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load data ref: %w", err)
		}
	}

	hasLib, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has lib: %w", err)
	}

	var lib *cell.Dictionary
	if hasLib {
		root, err := loader.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load lib ref: %w", err)
		}

		lib, err = root.LoadDict(256)
		if err != nil {
			return fmt.Errorf("failed to load lib dict: %w", err)
		}
	}

	*m = StateInit{
		Depth:    depth,
		TickTock: tickTock,
		Lib:      lib,
	}

	if codeCell != nil {
		m.Code, err = codeCell.ToCell()
		if err != nil {
			return fmt.Errorf("failed to convert code to cell: %w", err)
		}
	}

	if dataCell != nil {
		m.Data, err = dataCell.ToCell()
		if err != nil {
			return fmt.Errorf("failed to convert data to cell: %w", err)
		}
	}
	return nil
}

func (m *StateInit) ToCell() (*cell.Cell, error) {
	var flags byte
	state := cell.BeginCell()

	if m.Lib != nil {
		return nil, errors.New("lib serialization is currently not supported")
	}

	if m.Depth != nil {
		return nil, errors.New("depth serialization is currently not supported")
	}

	if m.TickTock != nil {
		return nil, errors.New("ticktock serialization is currently not supported")
	}

	if m.Code != nil {
		flags |= 1 << 2
		state.MustStoreRef(m.Code)
	}

	if m.Data != nil {
		flags |= 1 << 1
		state.MustStoreRef(m.Data)
	}

	return state.MustStoreUInt(uint64(flags), 5).EndCell(), nil
}
