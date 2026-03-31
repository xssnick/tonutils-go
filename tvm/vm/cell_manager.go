package vm

import "github.com/xssnick/tonutils-go/tvm/cell"

type CellManager struct {
	state      *State
	loaded     map[string]struct{}
	pendingErr error
}

func (m *CellManager) Init(state *State) {
	m.state = state
	if m.loaded == nil {
		m.loaded = map[string]struct{}{}
	}
}

func (m *CellManager) PendingError() error {
	return m.pendingErr
}

func (m *CellManager) OnCellLoad(hash []byte) {
	if m.pendingErr != nil {
		return
	}
	m.pendingErr = m.RegisterCellLoadHash(hash)
}

func (m *CellManager) OnCellCreate() {
	if m.pendingErr != nil {
		return
	}
	m.pendingErr = m.RegisterCellCreate()
}

func (m *CellManager) RegisterCellLoad(cl *cell.Cell) error {
	if cl == nil {
		return nil
	}
	return m.RegisterCellLoadHash(cl.Hash())
}

func (m *CellManager) RegisterCellLoadHash(hash []byte) error {
	_, ok := m.loaded[string(hash)]
	if !ok {
		m.loaded[string(hash)] = struct{}{}
		return m.state.ConsumeGas(CellLoadGasPrice)
	}
	return m.state.ConsumeGas(CellReloadGasPrice)
}

func (m *CellManager) RegisterCellCreate() error {
	return m.state.ConsumeGas(CellCreateGasPrice)
}

func (m *CellManager) BeginParse(cl *cell.Cell) (*cell.Slice, error) {
	if err := m.RegisterCellLoad(cl); err != nil {
		return nil, err
	}
	return cl.BeginParse().SetObserver(m), nil
}

func (m *CellManager) LoadRef(sl *cell.Slice) (*cell.Slice, error) {
	ref, err := sl.LoadRef()
	if err != nil {
		return nil, err
	}
	if err = m.state.CheckGas(); err != nil {
		return nil, err
	}
	return ref, nil
}

func (m *CellManager) LoadRefCell(sl *cell.Slice) (*cell.Cell, error) {
	ref, err := sl.LoadRefCell()
	if err != nil {
		return nil, err
	}
	if err = m.state.CheckGas(); err != nil {
		return nil, err
	}
	return ref, nil
}
