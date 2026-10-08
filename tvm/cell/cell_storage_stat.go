package cell

import "bytes"

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

// storageSeenSet is the dedup memo of CellStorageStat as an open-addressed
// table instead of a map[Hash]struct{}: the key is already a cryptographic
// hash, so a slot carries four of its bytes as a fingerprint and the full
// 32-byte compare happens only on a candidate hit, against the cached hash of
// the stored cell. Entries hold the visited cell rather than a copy of its
// hash — the cell outlives the set anyway, and eight bytes per entry keep the
// growth churn of a full block a fraction of what the map reallocated.
//
// The set starts empty and grows as it fills, because instances differ by
// orders of magnitude in how far they grow (a shard collation seeds thousands
// of ordinary cells but only hundreds of proof cells) — presizing for the big
// one was measured to waste more than it saved on the others.
//
// Growth is where a full block actually spends its time here: filling the
// ~28k-cell set of a mainnet shard block costs about 2.5x what probing the
// finished table costs, because every doubling rehashes everything inserted so
// far. Past seenSetWideGrowFrom the table therefore grows fourfold instead,
// which cuts the number of rehashes for a block-sized set from ten to five for
// the same final capacity — the growth threshold is a fill ratio, so the last
// step lands on the same size either way. Small instances keep doubling, so
// the proof-side sets do not pay for the block-side ones in memory.
const seenSetWideGrowFrom = 4096

// storageSeenMaxPresizedCells caps the estimate presize honours, so a caller
// whose figure is wrong by orders of magnitude allocates a bounded table rather
// than an arbitrary one.
const storageSeenMaxPresizedCells = 1 << 20

type storageSeenSet struct {
	// slots hold usageCellFingerprint(hash)<<32 | entry index+1; zero is empty.
	slots []uint64
	cells []*Cell
	// growAt is the entry count at which slots must grow, kept here rather
	// than recomputed per insert: the comparison is on the hot path of every
	// cell the collation walks.
	growAt int
}

// presize allocates for expected cells up front. It is capacity and nothing
// else: the set is per-CellStorageStat, holds cells of the tree being walked and
// is dropped with it, and a wrong estimate costs only the growth it failed to
// avoid. The wide growth above exists because filling a block-sized set from
// nothing rehashes it ten times over; an estimate removes the question.
func (s *storageSeenSet) presize(expected int) {
	if expected <= 0 || s.slots != nil {
		return
	}
	if expected > storageSeenMaxPresizedCells {
		expected = storageSeenMaxPresizedCells
	}
	slots := 64
	for slots < 2*expected {
		slots *= 2
	}
	s.slots = make([]uint64, slots)
	s.cells = make([]*Cell, 0, slots/2)
	s.growAt = slots / 2
}

// contains reports whether hash is already recorded, without recording it.
func (s *storageSeenSet) contains(hash Hash) bool {
	if s.slots == nil {
		return false
	}
	fingerprint := usageCellFingerprint(hash)
	mask := len(s.slots) - 1
	pos := int(fingerprint) & mask
	for {
		slot := s.slots[pos]
		if slot == 0 {
			return false
		}
		if uint32(slot>>32) == fingerprint &&
			bytes.Equal(s.cells[uint32(slot)-1].getHash(_DataCellMaxLevel), hash[:]) {
			return true
		}
		pos = (pos + 1) & mask
	}
}

// addIfAbsent reports whether hash was new and records it if so. hash must be
// c.HashKey().
func (s *storageSeenSet) addIfAbsent(c *Cell, hash Hash) bool {
	if s.slots == nil {
		s.slots = make([]uint64, 64)
		s.cells = make([]*Cell, 0, 32)
		s.growAt = 32
	}
	fingerprint := usageCellFingerprint(hash)
	mask := len(s.slots) - 1
	pos := int(fingerprint) & mask
	for {
		slot := s.slots[pos]
		if slot == 0 {
			break
		}
		if uint32(slot>>32) == fingerprint &&
			bytes.Equal(s.cells[uint32(slot)-1].getHash(_DataCellMaxLevel), hash[:]) {
			return false
		}
		pos = (pos + 1) & mask
	}

	if len(s.cells) >= s.growAt {
		s.grow()
		mask = len(s.slots) - 1
		pos = int(fingerprint) & mask
		for s.slots[pos] != 0 {
			pos = (pos + 1) & mask
		}
	}
	if len(s.cells) == cap(s.cells) {
		// Explicit growth: append's factor for large slices would reallocate
		// more often and retire more garbage. Entries are kept at half the
		// slots they were sized against, which is the fill the growth
		// threshold above is chosen for.
		next := make([]*Cell, len(s.cells), len(s.slots)/2)
		copy(next, s.cells)
		s.cells = next
	}
	s.cells = append(s.cells, c)
	s.slots[pos] = uint64(fingerprint)<<32 | uint64(len(s.cells))
	return true
}

func (s *storageSeenSet) grow() {
	old := s.slots
	factor := 2
	if len(old) >= seenSetWideGrowFrom {
		factor = 4
	}
	s.slots = make([]uint64, factor*len(old))
	s.growAt = len(s.slots) / 2
	mask := len(s.slots) - 1
	for _, slot := range old {
		if slot == 0 {
			continue
		}
		pos := int(uint32(slot>>32)) & mask
		for s.slots[pos] != 0 {
			pos = (pos + 1) & mask
		}
		s.slots[pos] = slot
	}
}

// CellStorageStat incrementally counts ordinary cell storage and the part of a
// Merkle proof that lies outside a ReadSet. Cells shared by multiple
// roots are counted once while references to them are counted for every edge.
// A CellStorageStat is mutable and must have a single writer.
type CellStorageStat struct {
	seen      storageSeenSet
	proofSeen storageSeenSet
	stat      StorageStat
	proofStat StorageStat
}

func NewCellStorageStat() *CellStorageStat {
	return &CellStorageStat{}
}

// NewCellStorageStatSized allocates both dedup sets for the populations the
// caller expects: expectedCells distinct ordinary cells and expectedProofCells
// distinct cells outside the read set. The two differ by an order of magnitude
// on the same walk, which is why they are estimated separately and why sizing
// one for the other was measured to cost more than it saved.
//
// The counts to hand back are CellCounts() of the previous walk of the same
// shape. Capacity only: nothing is carried across instances, and a wrong
// estimate costs only the growth it failed to avoid.
func NewCellStorageStatSized(expectedCells, expectedProofCells int) *CellStorageStat {
	s := &CellStorageStat{}
	s.seen.presize(expectedCells)
	s.proofSeen.presize(expectedProofCells)
	return s
}

// CellCounts returns how many distinct cells each dedup set ended up holding —
// the ordinary walk and the proof walk. It is what NewCellStorageStatSized wants
// for the next walk of the same shape.
func (s *CellStorageStat) CellCounts() (cells, proofCells uint64) {
	return s.stat.Cells, s.proofStat.Cells
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

func (s *CellStorageStat) AddProof(root *Cell, read *ReadSet) error {
	// An empty dictionary has no root cell and contributes no proof storage.
	if root == nil {
		return nil
	}
	return s.walk(root, false, true, read)
}

func (s *CellStorageStat) walk(c *Cell, countCell, countProof bool, read *ReadSet) error {
	if countCell {
		s.stat.InternalRefs++
		if s.seen.addIfAbsent(c, c.HashKey()) {
			s.stat.Cells++
		} else {
			countCell = false
		}
	}

	if countProof {
		hash := c.HashKey()
		switch {
		case s.proofSeen.contains(hash):
			// This proof already carries the body, so a further edge onto it is an
			// ordinary reference — whatever the read set has learned in between.
			//
			// The dedup memo is consulted BEFORE the read set, and that order is the
			// whole correctness of this walk. Prunable answers "was this content
			// parsed through the recording trace", which is not the same question as
			// "does the source tree hold this subtree": a cell the transition rebuilt
			// and then read back is recorded even though the predecessor never held
			// it (see readset_source_graph.go, which exists to separate exactly these
			// two and is what the update builder prunes by). Prunable also grows
			// during a collation, so asking it first let such a cell be serialized in
			// full early on and then charged as a 40-byte boundary on every later
			// edge — a boundary the produced update never emits, because the source
			// graph does not hold that hash either.
			//
			// Measured on the mainnet-heavy fixture at the message the block stopped
			// at: 113 hashes, 138 edges, every one of them absent from the source
			// graph and every one of them previously serialized in full by this same
			// walk. They inflated the admission estimate by 4,732 B on a 1,048,576 B
			// budget and cost the block six inbound messages the reference
			// implementation admitted. The reference cannot express the mistake at
			// all: its predicate is per-object usage-tree provenance
			// (NewCellStorageStat::dfs, is_from_tree), which is fixed when the cell
			// is created and cannot change under it.
			//
			// A genuine boundary never reaches this branch. To carry a predecessor
			// subtree the destination had to descend to it, which means its parent
			// was read, which puts it in the frontier before any proof walk can see
			// it — so it is classified external on first sight and never enters the
			// memo.
			s.proofStat.InternalRefs++
			countProof = false
		default:
			if _, known := read.Prunable(hash); known {
				// A cell the read set already knows about — read or merely
				// referenced — stands outside this proof, exactly as owning a usage
				// node used to mean.
				s.proofStat.ExternalRefs++
				countProof = false
			} else {
				s.proofStat.InternalRefs++
				s.proofSeen.addIfAbsent(c, hash)
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
		if err = s.walk(refs.viewRef(loaded.refs[i]), countCell, countProof, read); err != nil {
			return err
		}
	}
	return nil
}
