package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type CellManager struct {
	state      *State
	loaded     map[[32]byte]struct{}
	pendingErr error
}

func (m *CellManager) Init(state *State) {
	m.state = state
	if m.loaded == nil {
		m.loaded = map[[32]byte]struct{}{}
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

func (m *CellManager) OnCellLoadKey(hash [32]byte) {
	if m.pendingErr != nil {
		return
	}
	m.pendingErr = m.RegisterCellLoadKey(hash)
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
	return m.RegisterCellLoadKey(cl.HashKey())
}

func (m *CellManager) RegisterCellLoadHash(hash []byte) error {
	var key [32]byte
	copy(key[:], hash)
	return m.RegisterCellLoadKey(key)
}

func (m *CellManager) RegisterCellLoadKey(key [32]byte) error {
	_, ok := m.loaded[key]
	if !ok {
		m.loaded[key] = struct{}{}
		return m.state.ConsumeGas(CellLoadGasPrice)
	}
	return m.state.ConsumeGas(CellReloadGasPrice)
}

func (m *CellManager) RegisterCellCreate() error {
	return m.state.ConsumeGas(CellCreateGasPrice)
}

func (m *CellManager) beginParseLoadedCell(cl *cell.Cell, allowSpecial bool, currentAlreadyLoaded bool) (*cell.Slice, bool, error) {
	current := cl
	libraryLoaded := false

	for {
		if current == nil {
			return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load cell")
		}
		if !currentAlreadyLoaded {
			if err := m.RegisterCellLoad(current); err != nil {
				return nil, false, err
			}
		}
		currentAlreadyLoaded = false

		if current.GetType() == cell.PrunedCellType && current.IsVirtualized() && current.EffectiveLevel() < current.ActualLevel() {
			return nil, false, vmerr.Virtualization(1)
		}
		if allowSpecial {
			return current.BeginParse().SetObserver(m), current.IsSpecial(), nil
		}
		if !current.IsSpecial() {
			return current.BeginParse().SetObserver(m), false, nil
		}

		switch current.GetType() {
		case cell.LibraryCellType:
			if libraryLoaded && m.state.GlobalVersion >= 5 {
				return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell: recursive library cells are not allowed")
			}

			libSlice := current.BeginParse()
			if _, err := libSlice.LoadUInt(8); err != nil {
				return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
			}

			hash, err := libSlice.LoadSlice(256)
			if err != nil {
				return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
			}

			resolved, err := m.state.LoadLibraryByHash(hash)
			if err != nil || resolved == nil {
				return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
			}

			libraryLoaded = true
			current = resolved
		case cell.PrunedCellType:
			return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "trying to load pruned cell")
		default:
			return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
		}
	}
}

func (m *CellManager) BeginParse(cl *cell.Cell) (*cell.Slice, error) {
	sl, _, err := m.beginParseLoadedCell(cl, false, false)
	return sl, err
}

func (m *CellManager) BeginParseSpecial(cl *cell.Cell) (*cell.Slice, bool, error) {
	return m.beginParseLoadedCell(cl, true, false)
}

func (m *CellManager) LoadRef(sl *cell.Slice) (*cell.Slice, error) {
	ref, err := sl.LoadRefCell()
	if err != nil {
		return nil, err
	}
	if err = m.state.CheckGas(); err != nil {
		return nil, err
	}
	parsed, _, err := m.beginParseLoadedCell(ref, false, true)
	return parsed, err
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
