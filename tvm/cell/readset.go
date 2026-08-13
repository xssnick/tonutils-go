package cell

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
)

// readSetShards keeps concurrent recorders off each other's cache lines. A
// collation replays account lanes on GOMAXPROCS goroutines through one read set,
// and the shard is picked from bytes of the hash the caller already has.
const readSetShards = 8

// readSetInitialSlots is deliberately tiny. Most read sets are small — an account
// storage proof is a handful of cells — and sizing every shard for the largest
// case up front would cost far more than it saves. A set that records a whole
// block instead grows into its size, which is what NewReadSetSized is for.
const readSetInitialSlots = 16

// readSetMaxPresizedCells caps the hint NewReadSetSized honours, so a caller
// whose estimate is wrong by orders of magnitude allocates a bounded table
// rather than an arbitrary one. A read set genuinely this large would allocate
// as much on its own.
const readSetMaxPresizedCells = 1 << 20

// ReadSet records which cells of a tree were read, and is the sole record a Merkle
// proof or a state update is built from.
//
// It replaces CellUsageTree. The difference is not the bookkeeping but what is
// recorded: the usage tree recorded read *paths* into a side arena keyed by
// (parent node, reference index), and every consumer then had to reconcile that
// arena against the tree, because cells rebuilt during a transition inherit the
// trace of the path they came from and so claim a node whose source held something
// else. A read set records read *cells*, keyed by their hash, and a hash cannot be
// inherited — which removes the whole class of mistakes the reconciliation code (a
// source index, a hash index, a mark mode) existed to paper over.
//
// It is wired in as a TraceListener, so it rides the plumbing that was already
// there and combines with the VM's gas trace the same way. Unlike the usage tree it
// returns itself as its own child trace, so descending a reference allocates
// nothing at all.
type ReadSet struct {
	source *Cell
	trace  *Trace

	shards [readSetShards]readSetShard

	// onRecord observes the first read of every cell. The collated-size estimators
	// are fed from here; it may run on several goroutines.
	onRecord func(*Cell)

	// ignore counts nested IgnoreReads scopes. It is atomic because it is consulted
	// on every parse, on the same path the lock-free probe exists to keep cheap.
	ignore atomic.Int32

	// recorded counts first reads. It exists so a Prunable caller can tell whether
	// the referenced frontier below is still current without touching a lock; it is
	// bumped only on a genuine insert, never on the repeat probe.
	recorded atomic.Int64

	// referenced is the set of cells that were never read but are referenced by one
	// that was: the boundaries a state update may prune onto, and the unchanged
	// siblings of every modified dictionary path. It is derived from the read cells
	// rather than maintained during reading, because computing it per read would put
	// four more inserts on the hot path to answer a question only the update and the
	// proof-size accounting ask.
	//
	// Deriving it is not a one-shot snapshot, because the proof-size accounting asks
	// during collation, interleaved with further reading: a set frozen at the first
	// question answers "unknown" for every boundary read after it, and the estimator
	// then descends into subtrees that the block will carry as pruned branches. It is
	// extended instead, from a per-shard cursor into the read table, so the work done
	// across a whole collation is one pass over the read cells however often it is
	// asked.
	//
	// It is a readSetShard rather than a map for the same reason the read half is
	// one: the key is a cryptographic hash the caller already holds, and it is
	// probed far more often than it is written — once per cell of every proof the
	// estimator adds. referencedMu covers extending it; reading it does not need a
	// lock, because the table publishes its own entries.
	referencedMu   sync.Mutex
	referenced     readSetShard
	referencedFrom [readSetShards]int
	referencedAt   atomic.Int64
}

// readSetShard is an open-addressed table rather than a map[Hash]*Cell: the key is
// already a cryptographic hash, so a slot carries four of its bytes as a
// fingerprint and the full 32-byte compare runs only on a candidate hit. Recording
// is attempted on every cell parse of a whole collation — hundreds of thousands of
// times per block, almost always for a cell already recorded — and a map spends
// more on hashing a 32-byte key than the whole probe is worth.
//
// Lookups are lock-free and inserts are not. That asymmetry is the point: a cell is
// inserted once and then re-probed on every later descent through it, so the repeat
// path must not take a mutex. The published table is immutable in shape; growing it
// builds a new one and swaps the pointer.
//
// Modelled on usageTreeCellIndex, which the deleted arena used for the same reason.
type readSetShard struct {
	mu    sync.Mutex
	table atomic.Pointer[readSetTable]
	used  int
}

// readSetTable is one generation of a shard's storage. hashes and cells are written
// at a reserved index BEFORE the slot naming that index is published, and the slot
// store is a release, so a reader that observes a slot observes a complete entry.
// Both are allocated at full length, never appended to, so a reader indexing into
// them cannot race a reallocation.
type readSetTable struct {
	slots  []atomic.Uint64 // fingerprint<<32 | (index+1); zero is empty
	hashes []Hash
	cells  []*Cell
}

func newReadSetTable(slots int) *readSetTable {
	return &readSetTable{
		slots:  make([]atomic.Uint64, slots),
		hashes: make([]Hash, slots/2),
		cells:  make([]*Cell, slots/2),
	}
}

// NewReadSet opens a recorder over root. Reads are recorded when they happen
// through Trace(): attach it with root.WithTrace(rs.Trace()), or hand rs.Root() to
// the reader.
func NewReadSet(root *Cell) *ReadSet {
	rs := &ReadSet{source: root}
	rs.trace = NewTraceForListener(rs)
	return rs
}

// NewReadSetSized opens a recorder over root whose shards are allocated for
// expectedCells distinct cells up front.
//
// A whole-block recorder ends up holding six figures of cells, and reaching that
// from the default sixteen slots per shard means a dozen doublings, each of
// which allocates the next generation and rehashes everything inserted so far.
// The final table is the same size either way, so a caller that can estimate the
// end — the same chain's previous block is the closest estimate available before
// the first read — pays for it once instead.
//
// The estimate only has to be the right order of magnitude: a shard that
// outgrows it still grows, and one that does not fill it wastes the difference.
// A non-positive or over-large hint falls back to the ordinary lazy growth, so
// this is never worse than NewReadSet by more than the hint is wrong.
func NewReadSetSized(root *Cell, expectedCells int) *ReadSet {
	rs := NewReadSet(root)
	slots := readSetSlotsFor(expectedCells)
	if slots <= readSetInitialSlots {
		return rs
	}
	for i := range rs.shards {
		rs.shards[i].table.Store(newReadSetTable(slots))
	}
	return rs
}

// readSetSlotsFor is the per-shard table size for a whole-set estimate. An
// insert grows at half occupancy, so a shard holding n entries needs 2n slots to
// avoid growing at all; the extra quarter absorbs the uneven split of cells
// across shards, which is a hash of the data rather than an exact division.
func readSetSlotsFor(expectedCells int) int {
	if expectedCells <= 0 {
		return readSetInitialSlots
	}
	if expectedCells > readSetMaxPresizedCells {
		expectedCells = readSetMaxPresizedCells
	}
	perShard := (expectedCells + readSetShards - 1) / readSetShards
	want := 2*perShard + perShard/2

	slots := readSetInitialSlots
	for slots < want {
		slots *= 2
	}
	return slots
}

// Trace returns the trace that records into this set. It is safe to combine with
// other traces; CombineTraces keeps both.
func (rs *ReadSet) Trace() *Trace {
	if rs == nil {
		return nil
	}
	return rs.trace
}

// Root returns the source root with the recording trace attached, which is what a
// reader must be handed: reads reached any other way are not recorded.
func (rs *ReadSet) Root() *Cell {
	if rs == nil || rs.source == nil {
		return nil
	}
	return rs.source.WithTrace(rs.trace)
}

// Source returns the untouched tree the set was opened over.
func (rs *ReadSet) Source() *Cell {
	if rs == nil {
		return nil
	}
	return rs.source
}

// SetRecordCallback registers fn for the first read of every cell. It must be set
// before reads start; fn may run concurrently.
func (rs *ReadSet) SetRecordCallback(fn func(*Cell)) {
	rs.onRecord = fn
}

// OnLoad implements TraceListener and is the single write path of the recorder.
func (rs *ReadSet) OnLoad(c *Cell) {
	if rs == nil || c == nil {
		return
	}
	if rs.ignoring() {
		return
	}
	rs.record(c)
}

// OnCreate implements TraceListener. A read set does not care about cells the
// reader builds: only a cell read out of the source tree can appear in its proof.
func (rs *ReadSet) OnCreate() {}

// ChildTrace implements TraceListener. Returning the same trace for every
// reference is what makes descent free: there is no per-position node to create, so
// a dictionary walk allocates nothing to stay recorded.
func (rs *ReadSet) ChildTrace(int) *Trace {
	return rs.trace
}

// PendingError implements TraceListener. Recording cannot fail.
func (rs *ReadSet) PendingError() error { return nil }

func (rs *ReadSet) record(c *Cell) {
	// A lazy placeholder carries hashes but no body, so recording it would put a
	// cell in the proof that cannot be serialized. The materialized cell arrives
	// through the same trace as soon as it is resolved.
	if c.IsLazy() {
		return
	}
	// The repeat path must not copy the hash. Recording is attempted on every
	// parse and almost always finds the cell already there, so the probe reads the
	// cell's hash in place and only a genuine first read pays for materializing a
	// Hash value.
	raw := c.getHash(_DataCellMaxLevel)
	shard := &rs.shards[raw[0]&(readSetShards-1)]
	if shard.probe(raw) {
		return
	}

	var hash Hash
	copy(hash[:], raw)
	if shard.insert(hash, c) {
		rs.recorded.Add(1)
		if rs.onRecord != nil {
			rs.onRecord(c)
		}
	}
}

// Record adds a cell explicitly. It is for the reads that cannot ride the trace: a
// cache handing out a cell it took from the source tree earlier, or the VM, whose
// own loaded-cell set is merged in wholesale.
func (rs *ReadSet) Record(c *Cell) {
	if rs == nil || c == nil {
		return
	}
	rs.record(c)
}

// RecordUnbilled adds a cell to the record without telling the record callback.
//
// It exists for the machine's own loaded-cell reports. Those cover everything
// execution touched, including the inbound message and cells the transaction
// built, which are not part of the predecessor tree and so can never appear in
// its proof: the proof walks the source and simply never looks their hashes up.
// Charging them to the collated-size estimate would shrink the block for bytes
// that are never emitted, and the estimate is a floor with a hard check on the
// real serialized size behind it, so leaving them uncharged is the safe side.
func (rs *ReadSet) RecordUnbilled(c *Cell) {
	if rs == nil || c == nil || c.IsLazy() {
		return
	}
	raw := c.getHash(_DataCellMaxLevel)
	shard := &rs.shards[raw[0]&(readSetShards-1)]
	if shard.probe(raw) {
		return
	}
	var hash Hash
	copy(hash[:], raw)
	if shard.insert(hash, c) {
		rs.recorded.Add(1)
	}
}

// RecordRecursive adds a cell and everything reachable from it. The execution
// proof needs it: whatever is reachable from the VM's final stack must appear in
// the proof even if the VM only referenced it.
//
// The walk stops on what it has already visited, not on what is already
// recorded. Those are different questions, and answering the wrong one silently
// shrinks the proof: a cell the execution read is in the record, but the
// references it never opened are exactly the ones this walk exists to add, so
// stopping there would leave them pruned.
func (rs *ReadSet) RecordRecursive(c *Cell) error {
	if rs == nil || c == nil {
		return nil
	}
	return rs.recordRecursive(c, make(map[Hash]struct{}), 0)
}

func (rs *ReadSet) recordRecursive(c *Cell, visited map[Hash]struct{}, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("read set recursion exceeded the cell depth limit")
	}
	loaded, err := c.load()
	if err != nil {
		return err
	}
	hash := loaded.HashKey()
	if _, seen := visited[hash]; seen {
		return nil
	}
	visited[hash] = struct{}{}
	rs.record(loaded)

	refView := newCellRefView(loaded)
	for i := 0; i < loaded.refsCount(); i++ {
		ref, refErr := refView.boundaryRef(i)
		if refErr != nil {
			return refErr
		}
		if err = rs.recordRecursive(ref, visited, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// IgnoreReads suspends and resumes recording. Dictionary, prefix-dictionary and
// augmented-dictionary readers parse their root to validate its shape, and a proof
// taken over such a dictionary must still be allowed to keep that root pruned;
// without the scope every validated root would silently widen the proof.
//
// Calls nest. Every IgnoreReads(true) must be paired with an IgnoreReads(false).
func (rs *ReadSet) IgnoreReads(ignore bool) {
	if rs == nil {
		return
	}
	if ignore {
		rs.ignore.Add(1)
		return
	}
	for {
		current := rs.ignore.Load()
		if current <= 0 {
			return
		}
		if rs.ignore.CompareAndSwap(current, current-1) {
			return
		}
	}
}

func (rs *ReadSet) ignoring() bool {
	return rs.ignore.Load() > 0
}

// Contains reports whether a cell with this hash was read — its body parsed — and
// returns the cell recorded under it. This is proof membership: only a cell that
// was read has to appear in a proof in full.
func (rs *ReadSet) Contains(hash Hash) (*Cell, bool) {
	if rs == nil {
		return nil, false
	}
	shard := &rs.shards[shardOf(hash)]
	return shard.lookup(hash)
}

// Prunable reports whether a cell with this hash is known at all — read, or merely
// referenced by a cell that was read — and returns it. This is update membership: a
// destination subtree may be replaced by a boundary onto any known predecessor
// subtree, including the unchanged siblings of a modified path, which are
// referenced but never opened.
//
// The answer is current as of the call: the referenced half is extended from the
// cells read since the last question, so interleaving questions with further
// reading is allowed and is what the proof-size estimator does.
//
// The returned cell is a boundary, not a body: it may be an unresolved lazy
// placeholder standing for a subtree nobody has opened. Callers that need the
// content must load() it; callers that only need to know the subtree is the
// predecessor's — which is what a boundary is for — can ignore it.
func (rs *ReadSet) Prunable(hash Hash) (*Cell, bool) {
	if rs == nil {
		return nil, false
	}
	if c, read := rs.Contains(hash); read {
		return c, true
	}

	if rs.referencedAt.Load() != rs.recorded.Load() {
		rs.referencedMu.Lock()
		if rs.referencedAt.Load() != rs.recorded.Load() {
			rs.extendReferencedLocked()
		}
		rs.referencedMu.Unlock()
	}
	return rs.referenced.lookup(hash)
}

// extendReferencedLocked walks the read cells recorded since the previous call and
// adds their unread children to the frontier.
//
// Extending is equivalent to rebuilding from scratch, for the only thing Prunable
// reports. A rebuild would drop a frontier entry that has since been read, whereas
// extending leaves it; but Prunable consults Contains first and a cell never stops
// being read, so both answer true for it either way. Nothing else reads the
// frontier.
func (rs *ReadSet) extendReferencedLocked() {
	// Sampled before the scan, so an insert racing the scan leaves the frontier
	// merely stale rather than wrongly declared current: the cursors below are
	// what makes the next call pick that cell up.
	upTo := rs.recorded.Load()
	if rs.referenced.table.Load() == nil {
		// The frontier ends up the same order of magnitude as the read half, and
		// growing into it from the default sixteen slots rehashes everything
		// inserted so far ten times over. The first question already knows how
		// much has been read, so start there instead.
		slots := readSetInitialSlots
		for slots < 4*int(upTo) {
			slots *= 2
		}
		rs.referenced.table.Store(newReadSetTable(slots))
	}

	for i := range rs.shards {
		shard := &rs.shards[i]
		shard.mu.Lock()
		var cells []*Cell
		if table := shard.table.Load(); table != nil && shard.used > rs.referencedFrom[i] {
			cells = table.cells[rs.referencedFrom[i]:shard.used:shard.used]
			rs.referencedFrom[i] = shard.used
		}
		shard.mu.Unlock()

		for _, c := range cells {
			refView := newCellRefView(c)
			for r := 0; r < c.refsCount(); r++ {
				ref, err := refView.boundaryRef(r)
				if err != nil || ref == nil {
					continue
				}
				// A lazy placeholder is kept, and that is the point: it carries
				// the hash of the predecessor subtree it stands for, so the
				// frontier answers the same for a paged-in state as for a
				// resident one. Skipping it would make membership — and every
				// size estimate derived from it — depend on which cells happen
				// to be in memory.
				//
				// Both probes take the hash in place; only a genuine first sight
				// of a boundary pays for materializing a Hash value.
				raw := ref.getHash(_DataCellMaxLevel)
				if rs.shards[raw[0]&(readSetShards-1)].probe(raw) {
					continue
				}
				if rs.referenced.probe(raw) {
					continue
				}
				var hash Hash
				copy(hash[:], raw)
				rs.referenced.insert(hash, ref)
			}
		}
	}
	rs.referencedAt.Store(upTo)
}

// Size returns how many distinct cells were read. It is the proof's cell count, and
// the proof builders use it to size their tables.
func (rs *ReadSet) Size() int {
	if rs == nil {
		return 0
	}
	total := 0
	for i := range rs.shards {
		shard := &rs.shards[i]
		shard.mu.Lock()
		total += shard.used
		shard.mu.Unlock()
	}
	return total
}

// Hashes returns the hashes of the cells that were read. It must be called after
// reading is done.
func (rs *ReadSet) Hashes() []Hash {
	if rs == nil {
		return nil
	}
	out := make([]Hash, 0, rs.Size())
	for i := range rs.shards {
		shard := &rs.shards[i]
		shard.mu.Lock()
		if table := shard.table.Load(); table != nil {
			out = append(out, table.hashes[:shard.used]...)
		}
		shard.mu.Unlock()
	}
	return out
}

// Cells returns the cells that were read, for callers that walk the record itself.
func (rs *ReadSet) Cells() []*Cell {
	if rs == nil {
		return nil
	}
	out := make([]*Cell, 0, rs.Size())
	for i := range rs.shards {
		shard := &rs.shards[i]
		shard.mu.Lock()
		if table := shard.table.Load(); table != nil {
			out = append(out, table.cells[:shard.used]...)
		}
		shard.mu.Unlock()
	}
	return out
}

func shardOf(hash Hash) int {
	return int(hash[0]) & (readSetShards - 1)
}

func readSetFingerprint(hash Hash) uint32 {
	// Bytes 1..4, because byte 0 already picked the shard and reusing it would
	// leave every fingerprint in a shard sharing its low bits.
	return uint32(hash[1]) |
		uint32(hash[2])<<8 |
		uint32(hash[3])<<16 |
		uint32(hash[4])<<24
}

// insert records a read cell and reports whether it is the first time. Never a
// truncated compare: the fingerprint only decides whether the full hash is worth
// comparing.
func (s *readSetShard) insert(hash Hash, c *Cell) bool {
	if _, found := s.lookup(hash); found {
		return false
	}

	fingerprint := readSetFingerprint(hash)

	s.mu.Lock()
	defer s.mu.Unlock()

	table := s.table.Load()
	if table == nil {
		table = newReadSetTable(readSetInitialSlots)
		s.table.Store(table)
	} else if s.used*2 >= len(table.slots) {
		table = s.growLocked(table)
	}

	// The lock-free probe above ran before the mutex was taken, so another
	// goroutine may have inserted this hash in between; the probe is repeated here
	// under the lock, where it is authoritative.
	mask := len(table.slots) - 1
	pos := int(fingerprint) & mask
	for {
		entry := table.slots[pos].Load()
		if entry == 0 {
			break
		}
		if uint32(entry>>32) == fingerprint {
			if idx := int(uint32(entry)) - 1; table.hashes[idx] == hash {
				return false
			}
		}
		pos = (pos + 1) & mask
	}

	idx := s.used
	table.hashes[idx] = hash
	table.cells[idx] = c
	table.slots[pos].Store(uint64(fingerprint)<<32 | uint64(idx+1))
	s.used++
	return true
}

// probe is lookup without materializing a Hash key, for the path that runs on
// every parse.
func (s *readSetShard) probe(raw []byte) bool {
	table := s.table.Load()
	if table == nil {
		return false
	}

	fingerprint := uint32(raw[1]) | uint32(raw[2])<<8 | uint32(raw[3])<<16 | uint32(raw[4])<<24
	mask := len(table.slots) - 1
	pos := int(fingerprint) & mask
	for {
		entry := table.slots[pos].Load()
		if entry == 0 {
			return false
		}
		if uint32(entry>>32) == fingerprint {
			if idx := int(uint32(entry)) - 1; bytes.Equal(table.hashes[idx][:], raw) {
				return true
			}
		}
		pos = (pos + 1) & mask
	}
}

// lookup probes without locking. It may miss an entry another goroutine is
// publishing right now, which only ever costs a redundant insert attempt.
func (s *readSetShard) lookup(hash Hash) (*Cell, bool) {
	table := s.table.Load()
	if table == nil {
		return nil, false
	}

	fingerprint := readSetFingerprint(hash)
	mask := len(table.slots) - 1
	pos := int(fingerprint) & mask
	for {
		entry := table.slots[pos].Load()
		if entry == 0 {
			return nil, false
		}
		if uint32(entry>>32) == fingerprint {
			if idx := int(uint32(entry)) - 1; table.hashes[idx] == hash {
				return table.cells[idx], true
			}
		}
		pos = (pos + 1) & mask
	}
}

// growLocked publishes a larger generation. Readers on the old table keep finding
// everything it held, so the swap needs no coordination with them.
func (s *readSetShard) growLocked(old *readSetTable) *readSetTable {
	next := newReadSetTable(len(old.slots) * 2)
	copy(next.hashes, old.hashes[:s.used])
	copy(next.cells, old.cells[:s.used])

	mask := len(next.slots) - 1
	for i := range old.slots {
		entry := old.slots[i].Load()
		if entry == 0 {
			continue
		}
		pos := int(uint32(entry>>32)) & mask
		for next.slots[pos].Load() != 0 {
			pos = (pos + 1) & mask
		}
		next.slots[pos].Store(entry)
	}
	s.table.Store(next)
	return next
}
