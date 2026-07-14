package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type CellManager struct {
	state              *State
	loaded             map[cell.Hash]struct{}
	pendingErr         error
	trace              *cell.Trace
	loadTrace          *cell.Trace
	alreadyLoadedTrace *cell.Trace
}

func (m *CellManager) Init(state *State) {
	m.state = state
	if m.loaded == nil {
		m.loaded = map[cell.Hash]struct{}{}
	}
}

func (m *CellManager) PendingError() error {
	return m.pendingErr
}

// OnLoad implements cell.TraceListener for the gas trace: it charges cell
// load gas, latching the first gas error so later events keep the original
// failure.
func (m *CellManager) OnLoad(c *cell.Cell) {
	if m.pendingErr != nil {
		return
	}
	m.pendingErr = m.RegisterCellLoad(c)
}

// OnCreate implements cell.TraceListener for the gas trace, charging cell
// create gas with the same sticky-error semantics as OnLoad.
func (m *CellManager) OnCreate() {
	if m.pendingErr != nil {
		return
	}
	m.pendingErr = m.RegisterCellCreate()
}

// ChildTrace implements cell.TraceListener for the gas trace: children stay
// on the same gas trace.
func (m *CellManager) ChildTrace(int) *cell.Trace {
	return m.Trace()
}

func (m *CellManager) Trace() *cell.Trace {
	if m == nil {
		return nil
	}
	if m.trace == nil {
		m.trace = cell.NewTraceForListener(m)
	}
	return m.trace
}

// cellManagerAlreadyLoadedTrace adapts CellManager as the trace listener for
// cells whose load has already been charged: creates and children forward to
// the gas trace, loads are not re-registered.
type cellManagerAlreadyLoadedTrace CellManager

func (m *cellManagerAlreadyLoadedTrace) OnLoad(*cell.Cell) {}

func (m *cellManagerAlreadyLoadedTrace) OnCreate() {
	(*CellManager)(m).OnCreate()
}

func (m *cellManagerAlreadyLoadedTrace) ChildTrace(int) *cell.Trace {
	return (*CellManager)(m).Trace()
}

func (m *cellManagerAlreadyLoadedTrace) PendingError() error {
	return (*CellManager)(m).pendingErr
}

func (m *CellManager) TraceAlreadyLoaded() *cell.Trace {
	if m == nil {
		return nil
	}
	if m.alreadyLoadedTrace == nil {
		m.alreadyLoadedTrace = cell.NewTraceForListener((*cellManagerAlreadyLoadedTrace)(m))
	}
	return m.alreadyLoadedTrace
}

// cellManagerLoadTrace adapts CellManager as the load-only trace listener: it
// registers cell loads but never charges create gas.
type cellManagerLoadTrace CellManager

func (m *cellManagerLoadTrace) OnLoad(c *cell.Cell) {
	(*CellManager)(m).OnLoad(c)
}

func (m *cellManagerLoadTrace) OnCreate() {}

func (m *cellManagerLoadTrace) ChildTrace(int) *cell.Trace {
	return (*CellManager)(m).LoadTrace()
}

func (m *cellManagerLoadTrace) PendingError() error {
	return (*CellManager)(m).pendingErr
}

func (m *CellManager) LoadTrace() *cell.Trace {
	if m == nil {
		return nil
	}
	if m.loadTrace == nil {
		m.loadTrace = cell.NewTraceForListener((*cellManagerLoadTrace)(m))
	}
	return m.loadTrace
}

func (m *CellManager) RegisterCellLoad(cl *cell.Cell) error {
	if cl == nil {
		return nil
	}
	return m.RegisterCellLoadKey(cl.HashKey())
}

func (m *CellManager) RegisterCellLoadKey(key cell.Hash) error {
	if m.loaded == nil {
		m.loaded = map[cell.Hash]struct{}{}
	}
	if m.state == nil {
		return nil
	}

	_, ok := m.loaded[key]
	if !ok {
		m.loaded[key] = struct{}{}
		return m.state.ConsumeGas(CellLoadGasPrice)
	}
	return m.state.ConsumeGas(CellReloadGasPrice)
}

func (m *CellManager) IsCellLoaded(cl *cell.Cell) bool {
	if cl == nil {
		return false
	}
	return m.IsCellLoadedKey(cl.HashKey())
}

func (m *CellManager) IsCellLoadedKey(key cell.Hash) bool {
	if m == nil || m.loaded == nil {
		return false
	}
	_, ok := m.loaded[key]
	return ok
}

func (m *CellManager) RegisterCellCreate() error {
	if m == nil || m.state == nil {
		return nil
	}
	return m.state.ConsumeGas(CellCreateGasPrice)
}

func (m *CellManager) beginParseWithGasTrace(cl *cell.Cell, alreadyLoaded bool) (*cell.Slice, error) {
	gasTrace := m.Trace()
	cellTrace := cl.Trace().WithoutTrace(gasTrace)
	withGas := cell.CombineTraces(cellTrace, gasTrace)

	var sl *cell.Slice
	var err error
	if alreadyLoaded {
		sl, err = cl.BeginParseWithTrace(cellTrace)
		if err != nil {
			return nil, err
		}
		sl.SetTrace(withGas)
	} else {
		sl, err = cl.BeginParseWithTrace(withGas)
		if err != nil {
			return nil, err
		}
	}

	if err := withGas.PendingError(); err != nil {
		return nil, err
	}
	return sl, nil
}

func (m *CellManager) BeginParseAlreadyLoadedRaw(cl *cell.Cell) (*cell.Slice, error) {
	sl, err := m.beginParseWithGasTrace(cl, true)
	if err != nil {
		return nil, err
	}
	return sl, nil
}

func (m *CellManager) BeginParseAlreadyLoadedNoCreate(cl *cell.Cell) (*cell.Slice, error) {
	gasTrace := m.Trace()
	loadTrace := m.LoadTrace()
	cellTrace := cl.Trace().WithoutTrace(gasTrace).WithoutTrace(loadTrace)
	withLoad := cell.CombineTraces(cellTrace, loadTrace)

	sl, err := cl.BeginParseWithTrace(cellTrace)
	if err != nil {
		return nil, err
	}
	sl.SetTrace(withLoad)
	if err := withLoad.PendingError(); err != nil {
		return nil, err
	}
	return sl, nil
}

func (m *CellManager) beginParseLoadedCell(cl *cell.Cell, allowSpecial bool, currentAlreadyLoaded bool) (*cell.Slice, bool, error) {
	current := cl
	libraryLoaded := false

	for {
		if current == nil {
			return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load cell")
		}
		alreadyLoaded := currentAlreadyLoaded || libraryLoaded
		currentAlreadyLoaded = false
		var loadedSlice *cell.Slice

		if current.GetType() == cell.PrunedCellType && current.IsVirtualized() && current.EffectiveLevel() < current.ActualLevel() {
			return nil, false, vmerr.Virtualization(1)
		}
		if current.IsLazy() {
			sl, err := m.beginParseWithGasTrace(current, alreadyLoaded)
			if err != nil {
				return nil, false, err
			}
			loadedSlice = sl
			current = sl.BaseCell()
			alreadyLoaded = true
		}
		if allowSpecial {
			if loadedSlice != nil {
				return loadedSlice, current.IsSpecial(), nil
			}
			sl, err := m.beginParseWithGasTrace(current, alreadyLoaded)
			return sl, current.IsSpecial(), err
		}
		if !current.IsSpecial() {
			if loadedSlice != nil {
				return loadedSlice, false, nil
			}
			sl, err := m.beginParseWithGasTrace(current, alreadyLoaded)
			return sl, false, err
		}

		switch current.GetType() {
		case cell.LibraryCellType:
			if libraryLoaded {
				return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell: recursive library cells are not allowed")
			}

			libSlice := loadedSlice
			if libSlice == nil {
				var err error
				libSlice, err = m.beginParseWithGasTrace(current, alreadyLoaded)
				if err != nil {
					return nil, false, err
				}
			}
			if err := libSlice.SkipBits(8); err != nil {
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
			if loadedSlice == nil {
				if _, err := m.beginParseWithGasTrace(current, alreadyLoaded); err != nil {
					return nil, false, err
				}
			}
			return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "trying to load pruned cell")
		default:
			if loadedSlice == nil {
				if _, err := m.beginParseWithGasTrace(current, alreadyLoaded); err != nil {
					return nil, false, err
				}
			}
			return nil, false, vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
		}
	}
}

func (m *CellManager) BeginParse(cl *cell.Cell) (*cell.Slice, error) {
	sl, _, err := m.beginParseLoadedCell(cl, false, false)
	return sl, err
}

func (m *CellManager) BeginParseAlreadyLoaded(cl *cell.Cell) (*cell.Slice, error) {
	sl, _, err := m.beginParseLoadedCell(cl, false, true)
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
	parsed, _, err := m.beginParseLoadedCell(ref, false, false)
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
