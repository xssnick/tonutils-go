package cell

import "sync/atomic"

// proofBuildTable is the memo the proof builders keep, as an open-addressed
// table instead of a Go map. The key is a cell hash plus a merkle depth: 40
// bytes that the runtime map hashes in full on every probe, for a table whose
// entries are visited two or three times each. Here a probe compares four bytes
// of fingerprint first and touches the full hash only on a candidate hit.
//
// Entries live in one of two places. A table on its own appends them to its
// private array, presized from the caller's hint. The tables of one parallel
// proof walk instead share a slab sized once from the walk's hint: the walk
// plans its branch split by subtree depth, which conserves the hint but places
// it badly — measured on the heavy mainnet block, per-branch hints ran from a
// fifth of the branch's population to seven times it, so every walk paid both
// the misplaced presize and the growth it failed to prevent, while the summed
// population landed within 3% of the walk hint the slab is sized from. The
// slots stay per-table; only the entry storage is shared, reserved with one
// atomic add per store and written by the reserving table alone.
type proofBuildTable struct {
	slots []uint32
	// count is how many entries this table holds; it drives the load factor,
	// which len(entries) no longer reflects when a slab holds the storage.
	count   int
	entries []proofBuildEntry
	slab    *proofBuildSlab
}

type proofBuildEntry struct {
	hash        Hash
	merkleDepth int32
	value       *Cell
	applied     *Cell
}

// proofBuildSlab is one walk's shared entry storage. It lives exactly as long
// as the walk's build states do and is dropped with them; nothing is pooled or
// carried between walks. cursor only grows: a reservation past the end stays
// counted and the store falls back to the reserving table's private array, so
// an under-sized hint degrades to the pre-slab behaviour for the overflow only.
type proofBuildSlab struct {
	entries []proofBuildEntry
	cursor  atomic.Int64
}

func newProofBuildSlab(hint int) *proofBuildSlab {
	return &proofBuildSlab{entries: make([]proofBuildEntry, hint)}
}

// reserve returns the position of one slab entry, or -1 when the slab is full.
func (s *proofBuildSlab) reserve() int {
	pos := s.cursor.Add(1) - 1
	if pos >= int64(len(s.entries)) {
		return -1
	}
	return int(pos)
}

// proofBuildPrivateBit marks a slot whose entry lives in the table's private
// overflow array rather than the shared slab. Slot values are index+1 in
// either space; entry counts stay far below the bit.
const proofBuildPrivateBit = uint32(1) << 31

func (t *proofBuildTable) init(hint int) {
	slots := 16
	for slots < 2*hint {
		slots *= 2
	}
	t.slots = make([]uint32, slots)
	if t.slab == nil {
		t.entries = make([]proofBuildEntry, 0, hint)
	}
}

func proofBuildFingerprint(hash Hash, merkleDepth int) uint32 {
	return (usageCellFingerprint(hash) ^ uint32(merkleDepth)*0x9e3779b1) | 1
}

// entryAt resolves a slot value to its entry.
func (t *proofBuildTable) entryAt(slot uint32) *proofBuildEntry {
	if slot&proofBuildPrivateBit != 0 {
		return &t.entries[(slot&^proofBuildPrivateBit)-1]
	}
	if t.slab != nil {
		return &t.slab.entries[slot-1]
	}
	return &t.entries[slot-1]
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
		entry := t.entryAt(slot)
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
	if (t.count+1)*2 > len(t.slots) {
		t.grow()
	}
	entry := proofBuildEntry{
		hash:        hash,
		merkleDepth: int32(merkleDepth),
		value:       value,
		applied:     applied,
	}
	var slot uint32
	if t.slab != nil {
		if pos := t.slab.reserve(); pos >= 0 {
			t.slab.entries[pos] = entry
			slot = uint32(pos) + 1
		} else {
			t.entries = append(t.entries, entry)
			slot = uint32(len(t.entries)) | proofBuildPrivateBit
		}
	} else {
		t.entries = append(t.entries, entry)
		slot = uint32(len(t.entries))
	}
	t.count++
	t.place(slot)
}

func (t *proofBuildTable) place(slot uint32) {
	entry := t.entryAt(slot)
	mask := len(t.slots) - 1
	pos := int(proofBuildFingerprint(entry.hash, int(entry.merkleDepth))) & mask
	for t.slots[pos] != 0 {
		pos = (pos + 1) & mask
	}
	t.slots[pos] = slot
}

func (t *proofBuildTable) grow() {
	old := t.slots
	t.slots = make([]uint32, len(t.slots)*2)
	for _, slot := range old {
		if slot != 0 {
			t.place(slot)
		}
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
