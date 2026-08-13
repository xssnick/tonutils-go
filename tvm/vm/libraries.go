package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// unmeteredLibraryDictResolver mirrors the dummy VmStateInterface installed
// during library lookup since global version 4: library nodes cannot resolve,
// while a virtualized pruned branch still aborts outside VmError handling.
type unmeteredLibraryDictResolver struct{}

// libraryLoadState groups the lazily-created state used only by executions
// that enforce a library-load limit or actually miss a library. Keeping its
// pointer in State avoids moving every execution into the next heap size class.
type libraryLoadState struct {
	loadedLibraries map[cell.Hash]struct{}
	missingLibrary  cell.Hash
	hasMissing      bool
	missingExposed  bool
}

func (unmeteredLibraryDictResolver) ResolveDictNodeCell(cl *cell.Cell) (*cell.Cell, error) {
	if cl.GetType() == cell.PrunedCellType && cl.IsVirtualized() && cl.EffectiveLevel() < cl.ActualLevel() {
		return nil, vmerr.Virtualization(1)
	}
	return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library dictionary node")
}

func (s *State) SetLibraries(libs ...*cell.Cell) {
	s.Libraries = append([]*cell.Cell(nil), libs...)
	s.libraryCache = nil
}

// checkLibraryLoadLimit enforces max_transaction_library_loads (see
// VmState::load_library in the reference C++ vm.cpp): unique hashes are
// counted, a repeat of an already-seen hash is always free, and the slot is
// consumed on the attempt itself, before the lookup below runs.
func (s *State) checkLibraryLoadLimit(hash cell.Hash) bool {
	if !s.hasMaxLibraryLoads {
		return true
	}

	loads := s.libraryLoads
	var loaded uint32
	if loads != nil {
		if _, seen := loads.loadedLibraries[hash]; seen {
			return true
		}
		loaded = uint32(len(loads.loadedLibraries))
	}

	// The reference compares for exact equality, so a limit lowered below the
	// number of already-seen hashes does not retroactively refuse new ones.
	if loaded == s.maxLibraryLoads {
		return false
	}

	if loads == nil {
		loads = new(libraryLoadState)
		s.libraryLoads = loads
	}
	if loads.loadedLibraries == nil {
		loads.loadedLibraries = make(map[cell.Hash]struct{})
	}
	loads.loadedLibraries[hash] = struct{}{}
	return true
}

func (s *State) shareLibraryLoadsWith(child *State) {
	if s.libraryLoads == nil {
		s.libraryLoads = new(libraryLoadState)
	}
	if s.libraryLoads.loadedLibraries == nil {
		s.libraryLoads.loadedLibraries = make(map[cell.Hash]struct{})
	}
	if child.libraryLoads == nil {
		child.libraryLoads = new(libraryLoadState)
	}
	child.libraryLoads.loadedLibraries = s.libraryLoads.loadedLibraries
}

// SuspendLibraryLoadAccounting temporarily gives a lookup the isolated state
// of the reference's startup DummyVmState. Besides lifting max-library-loads
// accounting, it keeps lookup cache and missing-library changes out of the
// execution state. The returned func restores the previous mode.
func (s *State) SuspendLibraryLoadAccounting() func() {
	prevHas := s.hasMaxLibraryLoads
	prevSandboxed := s.libraryLookupSandboxed
	s.hasMaxLibraryLoads = false
	s.libraryLookupSandboxed = true
	return func() {
		s.hasMaxLibraryLoads = prevHas
		s.libraryLookupSandboxed = prevSandboxed
	}
}

func (s *State) LoadLibraryByHash(hash []byte) (*cell.Cell, error) {
	if len(hash) != 32 {
		return nil, nil
	}

	var cacheKey cell.Hash
	copy(cacheKey[:], hash)

	if !s.checkLibraryLoadLimit(cacheKey) {
		return nil, nil
	}

	// Since global version 4 the reference VmState::load_library (see the C++
	// vm.cpp) installs an empty VmStateInterface for the duration of the
	// dictionary lookup, so nothing loaded while searching is charged; the
	// resolved cell itself is charged (or not) by the caller. Before v4 the
	// regular interface stays active and every dictionary node actually loaded
	// by the lookup consumes cell load/reload gas through the usual
	// per-unique-cell accounting, so a repeated lookup re-walks the tree at
	// reload prices and even a failed lookup pays for the nodes it visited.
	metered := s.GlobalVersion < 4

	if !metered && !s.libraryLookupSandboxed && s.libraryCache != nil {
		if cached := s.libraryCache[cacheKey]; cached != nil {
			return cached, nil
		}
	}

	key := cell.BeginCell().MustStoreSlice(hash, 256).EndCell()
	for _, root := range s.Libraries {
		if root == nil {
			continue
		}

		dict := root.AsDict(256)
		if metered {
			// Before v4 the active VM interface remains installed during lookup,
			// so a library cell used as a dictionary node resolves recursively.
			dict.SetTrace(s.Cells.Trace())
		}
		var value *cell.Slice
		var err error
		if metered {
			value, err = dict.LoadValue(key)
		} else {
			value, err = dict.LoadValueWithResolver(key, unmeteredLibraryDictResolver{})
		}
		if metered {
			if gasErr := s.Cells.PendingError(); gasErr != nil {
				return nil, gasErr
			}
		}
		if _, ok := vmerr.AsVirtualization(err); ok {
			return nil, err
		}
		if err != nil || value == nil || value.RefsNum() == 0 {
			continue
		}

		ref, err := value.LoadRefCell()
		if err != nil || ref == nil {
			continue
		}

		if ref.HashKey() == cacheKey {
			if !metered && !s.libraryLookupSandboxed {
				if s.libraryCache == nil {
					s.libraryCache = make(map[cell.Hash]*cell.Cell, len(s.Libraries))
				}
				s.libraryCache[cacheKey] = ref
			}
			return ref, nil
		}
	}

	if !s.libraryLookupSandboxed {
		loads := s.libraryLoads
		if loads == nil || loads.missingExposed {
			var loadedLibraries map[cell.Hash]struct{}
			if loads != nil {
				loadedLibraries = loads.loadedLibraries
			}
			loads = &libraryLoadState{loadedLibraries: loadedLibraries}
			s.libraryLoads = loads
		}
		loads.missingLibrary = cacheKey
		loads.hasMissing = true
		loads.missingExposed = false
	}
	return nil, nil
}

// ResolveDictNodeCell resolves a library cell met inside a dictionary walk:
// the library cell itself has already been charged by the walk's own load
// notification; the resolved cell is charged only below global version 5,
// where the first-resolution marker does not exist yet and library-to-library
// chains keep resolving.
func (s *State) ResolveDictNodeCell(cl *cell.Cell) (*cell.Cell, error) {
	libraryLoaded := false
	for {
		// A dictionary node load is a regular cell load: a pruned branch above
		// the effective level aborts the whole VM and cannot be caught, which
		// happens before the special cell kind is inspected at all.
		if cl.GetType() == cell.PrunedCellType && cl.IsVirtualized() && cl.EffectiveLevel() < cl.ActualLevel() {
			return nil, vmerr.Virtualization(1)
		}
		if cl.GetType() != cell.LibraryCellType {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
		}
		if libraryLoaded && s.GlobalVersion >= 5 {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell: recursive library cells are not allowed")
		}

		sl, err := s.Cells.BeginParseAlreadyLoadedRaw(cl)
		if err != nil {
			return nil, err
		}
		if err = sl.SkipBits(8); err != nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}
		hash, err := sl.LoadSlice(256)
		if err != nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}

		resolved, err := s.LoadLibraryByHash(hash)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}
		if s.GlobalVersion < 5 {
			if err = s.Cells.RegisterCellLoad(resolved); err != nil {
				return nil, err
			}
		}
		libraryLoaded = true
		if !resolved.IsSpecial() {
			return resolved, nil
		}
		cl = resolved
	}
}

func (s *State) ResolveLibraryCell(cl *cell.Cell) (*cell.Cell, error) {
	if cl == nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load cell")
	}

	if err := s.Cells.RegisterCellLoad(cl); err != nil {
		return nil, err
	}

	current := cl
	var loadedSlice *cell.Slice
	if cl.IsLazy() {
		// the special-cell load is raw here: no virtualization check happens,
		// so a virtualized pruned branch is just a special non-library cell
		// and falls through to the cell underflow below
		loadedSlice = new(cell.Slice)
		if err := s.Cells.beginParseWithGasTraceInto(cl, cl.Trace(), loadedSlice, true); err != nil {
			return nil, err
		}
		current = loadedSlice.BaseCell()
		if !current.IsSpecial() {
			return current, nil
		}
	} else if !cl.IsSpecial() {
		return cl, nil
	}

	switch current.GetType() {
	case cell.LibraryCellType:
		libSlice := loadedSlice
		if libSlice == nil {
			var err error
			libSlice, err = s.Cells.BeginParseAlreadyLoadedRaw(current)
			if err != nil {
				return nil, err
			}
		}
		if err := libSlice.SkipBits(8); err != nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}

		hash, err := libSlice.LoadSlice(256)
		if err != nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}

		lib, err := s.LoadLibraryByHash(hash)
		if err != nil {
			return nil, err
		}
		if lib == nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}
		return lib, nil
	case cell.PrunedCellType:
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load cell")
	default:
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
	}
}
