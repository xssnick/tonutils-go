package cell

// proofBuildTable is the memo the proof builders keep, as an open-addressed
// table instead of a Go map. The key is a cell hash plus a merkle depth: 40
// bytes that the runtime map hashes in full on every probe, for a table whose
// entries are visited two or three times each. Here a probe compares four bytes
// of fingerprint first and touches the full hash only on a candidate hit.
type proofBuildTable struct {
	slots   []uint32
	entries []proofBuildEntry
}

type proofBuildEntry struct {
	hash        Hash
	merkleDepth int32
	value       *Cell
	applied     *Cell
}

func (t *proofBuildTable) init(hint int) {
	slots := 16
	for slots < 2*hint {
		slots *= 2
	}
	t.slots = make([]uint32, slots)
	t.entries = make([]proofBuildEntry, 0, hint)
}

func proofBuildFingerprint(hash Hash, merkleDepth int) uint32 {
	return (usageCellFingerprint(hash) ^ uint32(merkleDepth)*0x9e3779b1) | 1
}

func (t *proofBuildTable) lookup(hash Hash, merkleDepth int) (*Cell, *Cell, bool) {
	if len(t.slots) == 0 {
		return nil, nil, false
	}
	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(hash, merkleDepth)) & mask
	for {
		slot := t.slots[pos]
		if slot == 0 {
			return nil, nil, false
		}
		entry := &t.entries[slot-1]
		if entry.merkleDepth == int32(merkleDepth) && entry.hash == hash {
			return entry.value, entry.applied, true
		}
		pos = (pos + 1) & mask
	}
}

func (t *proofBuildTable) store(hash Hash, merkleDepth int, value, applied *Cell) {
	if len(t.slots) == 0 {
		t.init(16)
	}
	if (len(t.entries)+1)*2 > len(t.slots) {
		t.grow()
	}
	t.entries = append(t.entries, proofBuildEntry{
		hash:        hash,
		merkleDepth: int32(merkleDepth),
		value:       value,
		applied:     applied,
	})
	t.place(uint32(len(t.entries)))
}

func (t *proofBuildTable) place(slot uint32) {
	entry := &t.entries[slot-1]
	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(entry.hash, int(entry.merkleDepth))) & mask
	for t.slots[pos] != 0 {
		pos = (pos + 1) & mask
	}
	t.slots[pos] = slot
}

func (t *proofBuildTable) grow() {
	t.slots = make([]uint32, len(t.slots)*2)
	for i := range t.entries {
		t.place(uint32(i + 1))
	}
}

// usageCellFingerprint takes the four leading bytes of a hash as an open-address
// table fingerprint. The storage stat and the proof build table both key by cell
// hash, and a fingerprint keeps the full 32-byte compare off every probe.
func usageCellFingerprint(hash Hash) uint32 {
	return uint32(hash[0]) |
		uint32(hash[1])<<8 |
		uint32(hash[2])<<16 |
		uint32(hash[3])<<24
}
