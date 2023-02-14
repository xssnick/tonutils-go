package tlb

import (
	"errors"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TickTock struct {
	Tick bool `tlb:"bool"`
	Tock bool `tlb:"bool"`
}

type StateInit struct {
	Depth    uint64           `tlb:"maybe ## 5"`
	TickTock *TickTock        `tlb:"maybe ."`
	Code     *cell.Cell       `tlb:"maybe ^"`
	Data     *cell.Cell       `tlb:"maybe ^"`
	Lib      *cell.Dictionary `tlb:"dict 256"`
}

func (m *StateInit) ToCell() (*cell.Cell, error) {
	var flags byte
	state := cell.BeginCell()

	if m.Lib != nil {
		return nil, errors.New("lib serialization is currently not supported")
	}

	if m.Depth != 0 {
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
