package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TickTock struct {
	Tick bool
	Tock bool
}

type StateInit struct {
	Depth    uint64
	TickTock *TickTock
	Data     *cell.Cell
	Code     *cell.Cell
}

func (m *StateInit) LoadFromCell(loader *cell.LoadCell) error {
	depth, err := loader.LoadUInt(5)
	if err != nil {
		return err
	}

	hasTickTock, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	var tickTock *TickTock
	if hasTickTock {
		isTick, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}

		isTock, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}

		tickTock = &TickTock{
			Tick: isTick,
			Tock: isTock,
		}
	}

	hasCode, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	var codeCell *cell.LoadCell
	if hasCode {
		codeCell, err = loader.LoadRef()
		if err != nil {
			return err
		}
	}

	hasData, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	var dataCell *cell.LoadCell
	if hasData {
		dataCell, err = loader.LoadRef()
		if err != nil {
			return err
		}
	}

	hasLib, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	var lib *Hashmap
	if hasLib {
		lib = &Hashmap{}
		root, err := loader.LoadRef()
		if err != nil {
			return err
		}

		err = lib.LoadFromCell(256, root)
		if err != nil {
			return err
		}
	}

	*m = StateInit{
		Depth:    depth,
		TickTock: tickTock,
	}

	if codeCell != nil {
		m.Code, err = codeCell.ToCell()
		if err != nil {
			return err
		}
	}

	if dataCell != nil {
		m.Data, err = dataCell.ToCell()
		if err != nil {
			return err
		}
	}
	return nil
}
