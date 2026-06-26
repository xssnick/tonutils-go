package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func (s *State) SetLibraries(libs ...*cell.Cell) {
	s.Libraries = append([]*cell.Cell(nil), libs...)
	s.libraryCache = nil
}

func (s *State) LoadLibraryByHash(hash []byte) (*cell.Cell, error) {
	if len(hash) != 32 {
		return nil, nil
	}

	var cacheKey cell.Hash
	copy(cacheKey[:], hash)
	if s.libraryCache != nil {
		if cached := s.libraryCache[cacheKey]; cached != nil {
			if err := s.consumeLegacyLibraryLookupGas(cached, true); err != nil {
				return nil, err
			}
			return cached, nil
		}
	}

	key := cell.BeginCell().MustStoreSlice(hash, 256).EndCell()
	for _, root := range s.Libraries {
		if root == nil {
			continue
		}

		dict := root.AsDict(256)
		value, err := dict.LoadValue(key)
		if err != nil || value == nil || value.RefsNum() == 0 {
			continue
		}

		ref, err := value.LoadRefCell()
		if err != nil || ref == nil {
			continue
		}

		if ref.HashKey() == cacheKey {
			if err := s.consumeLegacyLibraryLookupGas(ref, false); err != nil {
				return nil, err
			}
			if s.libraryCache == nil {
				s.libraryCache = make(map[cell.Hash]*cell.Cell, len(s.Libraries))
			}
			s.libraryCache[cacheKey] = ref
			return ref, nil
		}
	}

	return nil, nil
}

func (s *State) consumeLegacyLibraryLookupGas(ref *cell.Cell, cached bool) error {
	if s.GlobalVersion >= 5 {
		return nil
	}

	if err := s.Cells.RegisterCellLoad(ref); err != nil {
		return err
	}

	if s.GlobalVersion < 4 {
		if cached {
			return s.ConsumeGas(CellReloadGasPrice)
		}
		return s.ConsumeGas(CellLoadGasPrice)
	}

	return nil
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
		if err != nil || lib == nil {
			return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load library cell")
		}
		return lib, nil
	case cell.PrunedCellType:
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "failed to load cell")
	default:
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "unexpected special cell")
	}
}
