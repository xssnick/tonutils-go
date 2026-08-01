package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// CellManager belongs to one State execution. Init only attaches that State;
// it intentionally preserves loaded cells and a pending gas error across
// repeated InitForExecution calls during the same execution. A pooled State
// must therefore replace or explicitly reset its CellManager before reuse.
type CellManager struct {
	state              *State
	loaded             cellLoadSet
	pendingErr         error
	trace              *cell.Trace
	loadTrace          *cell.Trace
	alreadyLoadedTrace *cell.Trace
}

const cellLoadInlineCapacity = 8

// cellLoadSet keeps the common short-execution case allocation-free and
// spills to a regular map without giving up exact hash equality. Once spilled,
// the map contains the inline prefix as well and becomes authoritative.
type cellLoadSet struct {
	inline [cellLoadInlineCapacity]cell.Hash
	spill  map[cell.Hash]struct{}
	count  uint8
}

func (s *cellLoadSet) add(key cell.Hash) bool {
	if s.spill != nil {
		if _, ok := s.spill[key]; ok {
			return false
		}
		s.spill[key] = struct{}{}
		return true
	}

	for i := uint8(0); i < s.count; i++ {
		if s.inline[i] == key {
			return false
		}
	}
	if s.count < cellLoadInlineCapacity {
		s.inline[s.count] = key
		s.count++
		return true
	}

	s.spill = make(map[cell.Hash]struct{}, cellLoadInlineCapacity*2)
	for i := range s.inline {
		s.spill[s.inline[i]] = struct{}{}
	}
	s.spill[key] = struct{}{}
	return true
}

func (s *cellLoadSet) contains(key cell.Hash) bool {
	if s.spill != nil {
		_, ok := s.spill[key]
		return ok
	}
	for i := uint8(0); i < s.count; i++ {
		if s.inline[i] == key {
			return true
		}
	}
	return false
}

func (m *CellManager) Init(state *State) {
	m.state = state
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
	if m.state == nil {
		return nil
	}

	if m.loaded.add(key) {
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
	if m == nil {
		return false
	}
	return m.loaded.contains(key)
}

func (m *CellManager) RegisterCellCreate() error {
	if m == nil || m.state == nil {
		return nil
	}
	return m.state.ConsumeGas(CellCreateGasPrice)
}

func (m *CellManager) beginParseWithGasTraceInto(cl *cell.Cell, sourceTrace *cell.Trace, dst *cell.Slice, alreadyLoaded bool) error {
	gasTrace := m.Trace()
	cellTrace := sourceTrace.WithoutTrace(gasTrace)
	withGas := cell.CombineTraces(cellTrace, gasTrace)

	if alreadyLoaded {
		if err := cl.BeginParseIntoWithTrace(dst, cellTrace); err != nil {
			return err
		}
		dst.SetTrace(withGas)
	} else {
		if err := cl.BeginParseIntoWithTrace(dst, withGas); err != nil {
			return err
		}
	}

	if err := withGas.PendingError(); err != nil {
		return err
	}
	return nil
}

func (m *CellManager) beginParseWithGasTrace(cl *cell.Cell, alreadyLoaded bool) (*cell.Slice, error) {
	sl := new(cell.Slice)
	if err := m.beginParseWithGasTraceInto(cl, cl.Trace(), sl, alreadyLoaded); err != nil {
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
	sl := new(cell.Slice)
	if err := m.BeginParseAlreadyLoadedNoCreateIntoWithTrace(cl, cl.Trace(), sl); err != nil {
		return nil, err
	}
	return sl, nil
}

// BeginParseAlreadyLoadedNoCreateIntoWithTrace parses a cell whose load has
// already been charged into caller-owned storage. sourceTrace is carried
// separately so traversal code does not need to clone the immutable Cell.
func (m *CellManager) BeginParseAlreadyLoadedNoCreateIntoWithTrace(cl *cell.Cell, sourceTrace *cell.Trace, dst *cell.Slice) error {
	gasTrace := m.Trace()
	loadTrace := m.LoadTrace()
	cellTrace := sourceTrace.WithoutTrace(gasTrace).WithoutTrace(loadTrace)
	withLoad := cell.CombineTraces(cellTrace, loadTrace)

	if err := cl.BeginParseIntoWithTrace(dst, cellTrace); err != nil {
		return err
	}
	dst.SetTrace(withLoad)
	if err := withLoad.PendingError(); err != nil {
		return err
	}
	return nil
}

func (m *CellManager) beginParseLoadedCell(cl *cell.Cell, allowSpecial bool, currentAlreadyLoaded bool) (*cell.Slice, bool, error) {
	sl := new(cell.Slice)
	special, err := m.beginParseLoadedCellInto(cl, cl.Trace(), sl, allowSpecial, currentAlreadyLoaded)
	if err != nil {
		return nil, false, err
	}
	return sl, special, nil
}

func (m *CellManager) beginParseLoadedCellInto(cl *cell.Cell, sourceTrace *cell.Trace, dst *cell.Slice, allowSpecial bool, currentAlreadyLoaded bool) (bool, error) {
	current := cl
	currentTrace := sourceTrace
	libraryLoaded := false
	// Only since global version 5 the reference load_cell_slice_impl (see the
	// C++ CellSlice.cpp) marks the first library resolution: the flag both
	// forbids library->library chains and skips charging the resolved cell's
	// load. Below v5 the flag stays unset, so chains keep resolving and every
	// iteration charges the loaded cell as before.
	restrictNestedLibraries := true
	if state := m.state; state != nil && state.GlobalVersion < 5 {
		restrictNestedLibraries = false
	}

	for {
		if current == nil {
			return false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load cell")
		}
		alreadyLoaded := currentAlreadyLoaded || libraryLoaded
		currentAlreadyLoaded = false
		loaded := false

		if current.GetType() == cell.PrunedCellType && current.IsVirtualized() && current.EffectiveLevel() < current.ActualLevel() {
			return false, vmerr.Virtualization(1)
		}
		if current.IsLazy() {
			if err := m.beginParseWithGasTraceInto(current, currentTrace, dst, alreadyLoaded); err != nil {
				return false, err
			}
			loaded = true
			current = dst.RawCell()
			currentTrace = dst.Trace()
			alreadyLoaded = true
		}
		if allowSpecial {
			if loaded {
				return current.IsSpecial(), nil
			}
			err := m.beginParseWithGasTraceInto(current, currentTrace, dst, alreadyLoaded)
			return current.IsSpecial(), err
		}
		if !current.IsSpecial() {
			if loaded {
				return false, nil
			}
			return false, m.beginParseWithGasTraceInto(current, currentTrace, dst, alreadyLoaded)
		}

		switch current.GetType() {
		case cell.LibraryCellType:
			if libraryLoaded {
				return false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell: recursive library cells are not allowed")
			}

			if !loaded {
				if err := m.beginParseWithGasTraceInto(current, currentTrace, dst, alreadyLoaded); err != nil {
					return false, err
				}
			}
			if err := dst.SkipBits(8); err != nil {
				return false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
			}

			hash, err := dst.LoadSlice(256)
			if err != nil {
				return false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
			}

			resolved, err := m.state.LoadLibraryByHash(hash)
			if err != nil {
				return false, err
			}
			if resolved == nil {
				return false, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
			}

			if restrictNestedLibraries {
				libraryLoaded = true
			}
			current = resolved
			currentTrace = resolved.Trace()
		case cell.PrunedCellType:
			if !loaded {
				if err := m.beginParseWithGasTraceInto(current, currentTrace, dst, alreadyLoaded); err != nil {
					return false, err
				}
			}
			return false, vmerr.Error(vmerr.CodeCellUnderflow, "trying to load pruned cell")
		default:
			if !loaded {
				if err := m.beginParseWithGasTraceInto(current, currentTrace, dst, alreadyLoaded); err != nil {
					return false, err
				}
			}
			return false, vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
		}
	}
}

// BeginParseInto is the allocation-free CellManager parsing gateway using the
// trace attached to cl.
func (m *CellManager) BeginParseInto(cl *cell.Cell, dst *cell.Slice) error {
	return m.BeginParseIntoWithTrace(cl, cl.Trace(), dst)
}

// BeginParseIntoWithTrace parses an immutable Cell while carrying its
// effective traversal trace out-of-line.
func (m *CellManager) BeginParseIntoWithTrace(cl *cell.Cell, sourceTrace *cell.Trace, dst *cell.Slice) error {
	_, err := m.beginParseLoadedCellInto(cl, sourceTrace, dst, false, false)
	return err
}

func (m *CellManager) BeginParse(cl *cell.Cell) (*cell.Slice, error) {
	sl, _, err := m.beginParseLoadedCell(cl, false, false)
	return sl, err
}

func (m *CellManager) BeginParseAlreadyLoaded(cl *cell.Cell) (*cell.Slice, error) {
	sl, _, err := m.beginParseLoadedCell(cl, false, true)
	return sl, err
}

// BeginParseAlreadyLoadedIntoWithTrace is the allocation-free form used when
// the immutable cell and its effective traversal trace are carried separately.
func (m *CellManager) BeginParseAlreadyLoadedIntoWithTrace(cl *cell.Cell, sourceTrace *cell.Trace, dst *cell.Slice) error {
	_, err := m.beginParseLoadedCellInto(cl, sourceTrace, dst, false, true)
	return err
}

func (m *CellManager) BeginParseSpecial(cl *cell.Cell) (*cell.Slice, bool, error) {
	return m.beginParseLoadedCell(cl, true, false)
}

func (m *CellManager) LoadRef(sl *cell.Slice) (*cell.Slice, error) {
	parsed := new(cell.Slice)
	if err := m.LoadRefInto(sl, parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// LoadRefInto advances sl and parses its next reference into caller-owned dst
// while keeping the child trace separate from the immutable referenced Cell.
func (m *CellManager) LoadRefInto(sl, dst *cell.Slice) error {
	ref, refTrace, err := sl.PeekRefCellAtWithTrace(0)
	if err != nil {
		return err
	}
	if err = sl.SkipBitsAndRefs(0, 1); err != nil {
		return err
	}
	if err = m.state.CheckGas(); err != nil {
		return err
	}
	_, err = m.beginParseLoadedCellInto(ref, refTrace, dst, false, false)
	return err
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
