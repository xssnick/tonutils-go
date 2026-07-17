package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

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
	if _, seen := s.loadedLibraries[hash]; seen {
		return true
	}
	if uint32(len(s.loadedLibraries)) >= s.maxLibraryLoads {
		return false
	}

	if s.loadedLibraries == nil {
		s.loadedLibraries = make(map[cell.Hash]struct{})
	}
	s.loadedLibraries[hash] = struct{}{}
	return true
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

	if !metered && s.libraryCache != nil {
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
			dict.SetTrace(s.Cells.Trace())
		}
		value, err := dict.LoadValue(key)
		if metered {
			if gasErr := s.Cells.PendingError(); gasErr != nil {
				return nil, gasErr
			}
		}
		if err != nil || value == nil || value.RefsNum() == 0 {
			continue
		}

		ref, err := value.LoadRefCell()
		if err != nil || ref == nil {
			continue
		}

		if ref.HashKey() == cacheKey {
			if !metered {
				if s.libraryCache == nil {
					s.libraryCache = make(map[cell.Hash]*cell.Cell, len(s.Libraries))
				}
				s.libraryCache[cacheKey] = ref
			}
			return ref, nil
		}
	}

	return nil, nil
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
		var special bool
		var err error
		loadedSlice, special, err = s.Cells.beginParseLoadedCell(cl, true, true)
		if err != nil {
			return nil, err
		}
		current = loadedSlice.BaseCell()
		if !special {
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
