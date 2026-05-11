package cell

import "encoding/binary"

const bocHashIndexInitialCapacity = 64

type bocHashIndex struct {
	fingerprints []uint64
	indexes      []uint32
	used         int
	growAt       int
}

func (m *bocHashIndex) get(fp uint64, hash []byte, cell *Cell, items []bocSerializeItem) (uint32, bool) {
	if len(m.indexes) == 0 {
		return 0, false
	}

	mask := uint64(len(m.indexes) - 1)
	pos := fp & mask
	for {
		idxPlusOne := m.indexes[pos]
		if idxPlusOne == 0 {
			return 0, false
		}

		idx := idxPlusOne - 1
		if m.fingerprints[pos] == fp {
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

func (m *bocHashIndex) set(fp uint64, idx uint32) {
	m.reserve()
	m.insert(fp, idx)
}

func (m *bocHashIndex) grow() {
	capacity := len(m.indexes) * 2
	if capacity == 0 {
		capacity = bocHashIndexInitialCapacity
	}
	for (m.used+1)*4 > capacity*3 {
		capacity *= 2
	}

	oldFingerprints := m.fingerprints
	oldIndexes := m.indexes
	m.fingerprints = make([]uint64, capacity)
	m.indexes = make([]uint32, capacity)
	m.growAt = capacity * 3 / 4

	if len(oldIndexes) == 0 {
		return
	}

	m.used = 0
	for i, idxPlusOne := range oldIndexes {
		if idxPlusOne == 0 {
			continue
		}
		m.insert(oldFingerprints[i], idxPlusOne-1)
	}
}

func (m *bocHashIndex) insert(fp uint64, idx uint32) {
	mask := uint64(len(m.indexes) - 1)
	pos := fp & mask
	for m.indexes[pos] != 0 {
		pos = (pos + 1) & mask
	}

	m.fingerprints[pos] = fp
	m.indexes[pos] = idx + 1
	m.used++
}

func bocHashFingerprint(hash []byte) uint64 {
	return binary.LittleEndian.Uint64(hash[:8])
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
