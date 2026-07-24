package cell

// StorageStat describes the cells reachable from one or more roots.
// InternalRefs counts every traversed edge, including roots. Cells and Bits
// count unique cells by hash. ExternalRefs is used for usage-tree proof
// boundaries.
type StorageStat struct {
	Cells        uint64
	Bits         uint64
	InternalRefs uint64
	ExternalRefs uint64
}

// CellStorageStat incrementally counts ordinary cell storage and the part of a
// Merkle proof that lies outside a CellUsageTree. Cells shared by multiple
// roots are counted once while references to them are counted for every edge.
// A CellStorageStat is mutable and must have a single writer.
type CellStorageStat struct {
	seen      map[Hash]struct{}
	proofSeen map[Hash]struct{}
	stat      StorageStat
	proofStat StorageStat
}

func NewCellStorageStat() *CellStorageStat {
	return &CellStorageStat{
		seen:      make(map[Hash]struct{}),
		proofSeen: make(map[Hash]struct{}),
	}
}

func (s *CellStorageStat) TotalStat() StorageStat {
	return StorageStat{
		Cells:        s.stat.Cells + s.proofStat.Cells,
		Bits:         s.stat.Bits + s.proofStat.Bits,
		InternalRefs: s.stat.InternalRefs + s.proofStat.InternalRefs,
		ExternalRefs: s.stat.ExternalRefs + s.proofStat.ExternalRefs,
	}
}

func (s *CellStorageStat) AddCell(root *Cell) error {
	// An absent root (e.g. an empty dictionary) contributes no storage.
	if root == nil {
		return nil
	}
	return s.walk(root, true, false, nil)
}

func (s *CellStorageStat) AddProof(root *Cell, usageTree *CellUsageTree) error {
	// An empty dictionary has no root cell and contributes no proof storage.
	if root == nil {
		return nil
	}
	return s.walk(root, false, true, usageTree)
}

func (s *CellStorageStat) walk(c *Cell, countCell, countProof bool, usageTree *CellUsageTree) error {
	if countCell {
		s.stat.InternalRefs++
		hash := c.HashKey()
		if _, exists := s.seen[hash]; exists {
			countCell = false
		} else {
			s.seen[hash] = struct{}{}
			s.stat.Cells++
		}
	}

	if countProof {
		if _, exists := usageTree.NodeForCell(c); exists {
			s.proofStat.ExternalRefs++
			countProof = false
		} else {
			s.proofStat.InternalRefs++
			hash := c.HashKey()
			if _, exists := s.proofSeen[hash]; exists {
				countProof = false
			} else {
				s.proofSeen[hash] = struct{}{}
				s.proofStat.Cells++
			}
		}
	}

	if !countCell && !countProof {
		return nil
	}

	loaded, err := c.load()
	if err != nil {
		return err
	}
	if countCell {
		s.stat.Bits += uint64(loaded.BitsSize())
	}
	if countProof {
		s.proofStat.Bits += uint64(loaded.BitsSize())
	}

	refs := newCellRefView(loaded)
	for i := 0; i < int(refs.refCnt); i++ {
		if err = s.walk(refs.viewRef(loaded.refs[i]), countCell, countProof, usageTree); err != nil {
			return err
		}
	}
	return nil
}
