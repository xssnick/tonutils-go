package cell

import "encoding/binary"

const bocHashIndexInitialCapacity = 64

type bocHashIndex struct {
	// Each entry packs a 32-bit hash fingerprint and index+1. A zero low
	// half marks an empty slot; matching fingerprints still verify full hashes.
	entries []uint64
	used    int
	growAt  int
}

func newBOCHashIndex(capacityHint int) *bocHashIndex {
	m := &bocHashIndex{}
	if capacityHint <= 0 {
		return m
	}
	if capacityHint > 1<<30 {
		capacityHint = 1 << 30
	}

	capacity := bocHashIndexInitialCapacity
	for capacityHint*4 > capacity*3 {
		capacity *= 2
	}

	m.entries = make([]uint64, capacity)
	m.growAt = capacity * 3 / 4
	return m
}

func (m *bocHashIndex) get(fp uint32, hash []byte, cell *Cell, items []bocSerializeItem) (uint32, bool) {
	if len(m.entries) == 0 {
		return 0, false
	}

	mask := uint32(len(m.entries) - 1)
	pos := fp & mask
	for {
		entry := m.entries[pos]
		idxPlusOne := uint32(entry)
		if idxPlusOne == 0 {
			return 0, false
		}

		idx := idxPlusOne - 1
		if uint32(entry>>32) == fp {
			itemCell := items[idx].cell
			if itemCell == cell || bocCellHashEqual(itemCell, hash) {
				return idx, true
			}
		}

		pos = (pos + 1) & mask
	}
}

func (m *bocHashIndex) reserve() {
	if m.used+1 > m.growAt {
		m.grow()
	}
}

func (m *bocHashIndex) set(fp uint32, idx uint32) {
	m.reserve()
	m.insert(fp, idx)
}

func (m *bocHashIndex) grow() {
	capacity := len(m.entries) * 2
	if capacity == 0 {
		capacity = bocHashIndexInitialCapacity
	}
	for (m.used+1)*4 > capacity*3 {
		capacity *= 2
	}

	oldEntries := m.entries
	m.entries = make([]uint64, capacity)
	m.growAt = capacity * 3 / 4

	if len(oldEntries) == 0 {
		return
	}

	m.used = 0
	for _, entry := range oldEntries {
		idxPlusOne := uint32(entry)
		if idxPlusOne == 0 {
			continue
		}
		m.insert(uint32(entry>>32), idxPlusOne-1)
	}
}

func (m *bocHashIndex) insert(fp uint32, idx uint32) {
	mask := uint32(len(m.entries) - 1)
	pos := fp & mask
	for uint32(m.entries[pos]) != 0 {
		pos = (pos + 1) & mask
	}

	m.entries[pos] = uint64(fp)<<32 | uint64(idx+1)
	m.used++
}

func bocHashFingerprint(hash []byte) uint32 {
	return binary.LittleEndian.Uint32(hash[:4])
}

func bocCellHashEqual(cell *Cell, hash []byte) bool {
	got := cell.getHash(_DataCellMaxLevel)
	for i := 0; i < hashSize; i++ {
		if got[i] != hash[i] {
			return false
		}
	}
	return true
}
