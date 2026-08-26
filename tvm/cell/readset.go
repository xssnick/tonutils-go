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

// RecordMany flushes callbacks in bounded windows accepted by callers such as
// the proof estimator, so a cached traversal never allocates a slice
// proportional to its size or holds first-read cells longer than one window.
const readSetRecordManyCallbackCells = 64

// referencedFrontierPercent is the frontier's size as a share of the read set.
// Measured across whole blocks of the fixture family it was 0.525-0.690, and it
// is far steadier than either count on its own: over that same family the hint
// itself was wrong by -47% to +88%. That is what makes the hint usable here —
// not as the frontier's size, but as the thing the frontier is proportional to.
const referencedFrontierPercent = 70

// referencedMaxPresizedCells caps what the frontier presize honours, so a hint
// wrong by orders of magnitude allocates a bounded table rather than an
// arbitrary one. The read half carries the same ceiling for the same reason.
//
// It is a separate constant from readSetMaxPresizedCells despite holding the
// same number, because it bounds a different set of inputs: not only the hint —
// which reaches this code having already been clamped once, and must not depend
// on that having happened — but also the read-count floor below, which is a live
// counter with no ceiling of its own. A frontier genuinely this large stands
// over a resident tree of at least as many cells, so a table this size is
// proportionate to what the caller already holds.
const referencedMaxPresizedCells = 1 << 20

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

	// sealed closes the recorder for good. Cells handed out while it was open
	// keep pointing at its trace, so a caller that retains one of them past the
	// proof it was recording for would otherwise retain the whole set — the
	// source tree and a six-figure table — and would keep recording into it from
	// every later descent through that cell. Sealing answers both: the write
	// paths become no-ops and ChildTrace stops propagating, so descending a
	// retained cell is a plain descent again, sharing rather than copying.
	sealed atomic.Bool

	// detached stops the recorder without dropping what it recorded. It is the
	// half of Seal that a producer needs at the instant its reading is over but
	// its record is still being consulted: the collated proof is selected from
	// the table after the last read, so the table has to outlive the fence, while
	// any descent through a cell the set handed out must stop being recorded from
	// that instant on. Sealing at the same point would force the selection to
	// re-resolve every cell from disk.
	//
	// Seal implies it. Nothing clears either one.
	detached atomic.Bool

	shards [readSetShards]readSetShard

	// onRecord observes the first read of every cell. The collated-size estimators
	// are fed from here; it may run on several goroutines.
	onRecord func(*Cell)

	// onRecordMany is the batched form of onRecord. RecordMany uses it after
	// releasing the read-set shard locks; when it is absent the ordinary callback
	// is invoked once per first read instead.
	onRecordMany func([]*Cell)

	// onIgnored observes the cells an IgnoreReads scope drops, so a caller that
	// deliberately reads without recording can still keep what it parsed and
	// decide later — after the point where recording would have been harmful —
	// whether to Record it. Unlike onRecord it is single-threaded: it fires from
	// inside a scope only one goroutine may open, for the reason IgnoreReads
	// itself is whole-set state.
	//
	// onIgnoredAt is the nesting depth the observer was installed at, and only
	// that depth fires. A scope opened underneath it — a dictionary validating
	// its root, say — is dropping reads for its own reason, and those cells are
	// not the installer's to keep.
	onIgnored   func(*Cell)
	onIgnoredAt int32

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

	// expected is the whole-set estimate NewReadSetSized was given, kept because
	// the frontier above is sized long after the read half is: its one and only
	// sizing decision is taken at the first Prunable question, which the
	// proof-size estimator asks during collation, when a few dozen cells have
	// been read out of the six figures that will be. Sized from what has been
	// read at that moment the frontier doubles seven times and discards every
	// generation but the last; sized from the estimate it is allocated once.
	//
	// It is a capacity hint and nothing else. Nothing is pooled or carried
	// across recorders — a table holds *Cell of the source tree, which is why
	// Seal exists — and a wrong estimate costs only the growth it failed to
	// avoid.
	expected int
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
//
// The entry arrays are sized independently of the slot array. They used to be
// slots/2, which tied the two dimensions that cost the most different amounts: a
// slot is 8 bytes and exists to keep the probe short, an entry is 40 (a 32-byte
// hash and a pointer) and exists to hold a cell. With them tied, buying probe
// headroom bought entry capacity nobody asked for — and because the slot count
// is rounded to a power of two, a table sized for the same population the shard
// really reaches landed one doubling above it and paid 56 bytes per slot for the
// privilege. Sized apart, slots stay generous and entries track the population.
type readSetTable struct {
	// slots hold fingerprint<<32 | readSetUnbilledBit? | (index+1); zero is
	// empty. The unbilled bit marks an entry RecordUnbilled put there, which
	// onRecord has not seen yet; the first billed read of that cell clears it
	// and fires the callback, so a cell is billed exactly once no matter which
	// route reached it first.
	slots  []atomic.Uint64
	hashes []Hash
	cells  []*Cell
}

// readSetUnbilledBit rides in the low word of a slot beside the entry index. The
// index is bounded by readSetMaxPresizedCells and the doubling above it, far
// below this bit.
const readSetUnbilledBit = uint64(1) << 31

// readSetSlotIndex is the entry index a slot names.
func readSetSlotIndex(entry uint64) int {
	return int(uint32(entry)&^uint32(readSetUnbilledBit)) - 1
}

func newReadSetTable(slots, entries int) *readSetTable {
	return &readSetTable{
		slots:  make([]atomic.Uint64, slots),
		hashes: make([]Hash, entries),
		cells:  make([]*Cell, entries),
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
	if expectedCells > readSetMaxPresizedCells {
		expectedCells = readSetMaxPresizedCells
	}
	if expectedCells > 0 {
		rs.expected = expectedCells
	}
	slots := readSetSlotsFor(expectedCells)
	if slots <= readSetInitialSlots {
		return rs
	}
	entries := readSetEntriesFor(expectedCells)
	for i := range rs.shards {
		rs.shards[i].table.Store(newReadSetTable(slots, entries))
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

// readSetShardSkewPercent is how much more than an even split the busiest shard
// is allowed before its entry array has to grow. Cells are spread by a byte of a
// cryptographic hash, and across whole-block recorders the busiest shard measured
// 1.9-4.5% above the average; the allowance is that spread with room to spare.
//
// It replaces the 25% the slot sizing carries. That figure never protected
// anything — rounding the slot count up to a power of two is what absorbs the
// skew, and it absorbs far more than a quarter — while on the entry side it is
// the difference between one allocation and two.
const readSetShardSkewPercent = 15

// readSetEntriesFor is the per-shard entry capacity for a whole-set estimate.
// Entries are the expensive dimension, so they are sized to the population the
// shard is expected to hold rather than to the slot count that keeps its probes
// short. A shard that outgrows the allowance still grows, and it then doubles 40
// bytes per entry instead of 56 per slot.
func readSetEntriesFor(expectedCells int) int {
	if expectedCells <= 0 {
		return readSetInitialSlots / 2
	}
	if expectedCells > readSetMaxPresizedCells {
		expectedCells = readSetMaxPresizedCells
	}
	perShard := (expectedCells + readSetShards - 1) / readSetShards
	return perShard + perShard*readSetShardSkewPercent/100
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

// SetRecordManyCallback registers the batched equivalent of the record
// callback. RecordMany invokes fn with the cells it billed for the first time;
// when fn is nil it falls back to invoking SetRecordCallback's function once per
// cell. Both callbacks must describe the same observation and must be installed
// before reads start. fn may run concurrently and is never called while a read
// set shard is locked. fn must not retain or mutate the slice; the cells it
// points to may be retained. Install the ordinary callback as well, because
// traced reads and Record continue to report through it.
func (rs *ReadSet) SetRecordManyCallback(fn func([]*Cell)) {
	rs.onRecordMany = fn
}

// SetIgnoredObserver registers fn for every cell parsed inside the IgnoreReads
// scope that is open right now, and only that one. Install it after opening the
// scope and clear it before closing, on the goroutine that owns the scope.
//
// It changes nothing about what is recorded: fn is told about a read the set is
// dropping, and dropping it remains the whole point of the scope. What it buys
// is that the cells stay reachable, so a caller whose reads are premature rather
// than unwanted can hand them back through Record once they are not.
func (rs *ReadSet) SetIgnoredObserver(fn func(*Cell)) {
	if rs == nil {
		return
	}
	rs.onIgnored = fn
	rs.onIgnoredAt = rs.ignore.Load()
}

// OnLoad implements TraceListener and is the single write path of the recorder.
func (rs *ReadSet) OnLoad(c *Cell) {
	if rs == nil || c == nil {
		return
	}
	if depth := rs.ignore.Load(); depth > 0 {
		if rs.onIgnored != nil && depth == rs.onIgnoredAt {
			rs.onIgnored(c)
		}
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
//
// A sealed set returns nil, which is what stops a retained cell from dragging a
// dead recorder down its whole subtree.
func (rs *ReadSet) ChildTrace(int) *Trace {
	if rs.Inert() {
		return nil
	}
	return rs.trace
}

// Detach closes the recorder and keeps the record. Every write path becomes a
// no-op and ChildTrace stops propagating, exactly as after Seal, but the table,
// the referenced set and the source tree stay readable.
//
// It exists for the fence a producer needs partway through finishing a block:
// reading is over, so nothing more may be recorded — a successor collation may
// already be descending through cells this set handed out — while the selection
// that turns the record into a proof has not run yet and must not be made to
// resolve those cells a second time.
func (rs *ReadSet) Detach() {
	if rs == nil {
		return
	}
	rs.detached.Store(true)
}

// Inert reports whether the recorder is closed, by Detach or by Seal. Every
// write path tests this rather than sealed: a detached set records nothing, and
// the difference between the two states is only whether the record survives.
func (rs *ReadSet) Inert() bool {
	return rs != nil && (rs.detached.Load() || rs.sealed.Load())
}

// Seal closes the recorder. Every write path becomes a no-op, ChildTrace stops
// propagating, and the recorded table and the source tree are dropped.
//
// It is for the caller that keeps cells the recorder handed out after the proof
// or update built from those reads is finished — a produced block, for instance,
// embeds cells read out of the predecessor. Those cells carry this set's trace,
// and without sealing they retain its table for as long as the block is retained
// and record every later descent through them into a set nobody will read again.
//
// Reading must be over before this is called: it is the caller's statement that
// nothing will consult the record again, so it does not synchronize with readers
// beyond making the write paths safe to hit.
func (rs *ReadSet) Seal() {
	if rs == nil {
		return
	}
	// Detached first, so a concurrent writer that observes neither flag and then
	// one of them cannot observe the table being dropped while still believing it
	// may record.
	rs.Detach()
	if rs.sealed.Swap(true) {
		return
	}
	for i := range rs.shards {
		shard := &rs.shards[i]
		shard.mu.Lock()
		shard.table.Store(nil)
		shard.used = 0
		shard.mu.Unlock()
	}
	rs.referencedMu.Lock()
	rs.referenced.table.Store(nil)
	rs.referenced.used = 0
	rs.referencedFrom = [readSetShards]int{}
	rs.referencedAt.Store(0)
	rs.referencedMu.Unlock()
	rs.source = nil
	rs.onRecord = nil
	rs.onRecordMany = nil
	rs.onIgnored = nil
}

// Sealed reports whether the recorder is closed.
func (rs *ReadSet) Sealed() bool {
	return rs != nil && rs.sealed.Load()
}

// PendingError implements TraceListener. Recording cannot fail.
func (rs *ReadSet) PendingError() error { return nil }

func (rs *ReadSet) record(c *Cell) {
	if rs.recordCell(c) && rs.onRecord != nil {
		rs.onRecord(c)
	}
}

// recordCell publishes one billed read and reports whether its first-read
// callback is due. The callback stays in the caller so RecordMany can coalesce
// several notifications without ever running one under the shard lock.
func (rs *ReadSet) recordCell(c *Cell) bool {
	if rs.Inert() {
		return false
	}
	// A lazy placeholder carries hashes but no body, so recording it would put a
	// cell in the proof that cannot be serialized. The materialized cell arrives
	// through the same trace as soon as it is resolved.
	if c.IsLazy() {
		return false
	}
	// The repeat path must not copy the hash. Recording is attempted on every
	// parse and almost always finds the cell already there, so the probe reads the
	// cell's hash in place and only a genuine first read pays for materializing a
	// Hash value.
	raw := c.getHash(_DataCellMaxLevel)
	shard := &rs.shards[raw[0]&(readSetShards-1)]
	if shard.probe(raw) == readSetBilled {
		return false
	}

	var hash Hash
	copy(hash[:], raw)
	shard.mu.Lock()
	bill, added := shard.insertLocked(hash, c, true)
	shard.mu.Unlock()
	if added {
		rs.recorded.Add(1)
	}
	return bill
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

// RecordMany adds cells explicitly in one pass. It has the same billed/unbilled
// and duplicate semantics as calling Record for every cell, while coalescing the
// first-read callback that accounts for a cached traversal. The callback runs
// after every shard insert has been unlocked; batches from concurrent callers
// may arrive concurrently.
func (rs *ReadSet) RecordMany(cells []*Cell) {
	if rs == nil || len(cells) == 0 || rs.Inert() {
		return
	}
	if rs.onRecordMany == nil {
		onRecord := rs.onRecord
		for _, c := range cells {
			if c != nil && rs.recordCell(c) && onRecord != nil {
				onRecord(c)
			}
		}
		return
	}

	onRecordMany := rs.onRecordMany
	var notify [readSetRecordManyCallbackCells]*Cell
	notifyCount := 0
	for _, c := range cells {
		if c != nil && rs.recordCell(c) {
			notify[notifyCount] = c
			notifyCount++
			if notifyCount == len(notify) {
				onRecordMany(notify[:])
				notifyCount = 0
			}
		}
	}
	if notifyCount != 0 {
		onRecordMany(notify[:notifyCount])
	}
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
	if rs == nil || c == nil || c.IsLazy() || rs.Inert() {
		return
	}
	raw := c.getHash(_DataCellMaxLevel)
	shard := &rs.shards[raw[0]&(readSetShards-1)]
	if shard.probe(raw) != readSetAbsent {
		return
	}
	var hash Hash
	copy(hash[:], raw)
	if _, added := shard.insert(hash, c, false); added {
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
		rs.referenced.table.Store(newReadSetTable(referencedTableFor(upTo, rs.expected)))
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
				if rs.shards[raw[0]&(readSetShards-1)].probe(raw) != readSetAbsent {
					continue
				}
				if rs.referenced.probe(raw) != readSetAbsent {
					continue
				}
				var hash Hash
				copy(hash[:], raw)
				rs.referenced.insert(hash, ref, true)
			}
		}
	}
	rs.referencedAt.Store(upTo)
}

// referencedTableFor is the frontier's one and only sizing decision: how many
// slots and how many entries to allocate, given the cells read so far and the
// whole-set hint the recorder was opened with.
//
// The frontier ends up the same order of magnitude as the read half, and growing
// into it from the default sixteen slots rehashes everything inserted so far ten
// times over. What has been read at the moment of the decision is a poor thing
// to size it from, because of when that moment is: the proof-size estimator asks
// its first Prunable question during prepare, with a few dozen cells recorded out
// of the six figures a block reads, so a table sized from it doubles seven more
// times and throws away every generation but the last.
//
// So the hint sizes it — but as a proportion and inside a ceiling, never as the
// figure itself. The hint is the previous block's read count, and one builder
// serves collations whose read sets differ by an order of magnitude, so a stale
// one arriving unbounded is what turns this single decision into tens of
// megabytes the block being collated will never fill. Three things hold it:
//
//   - the measured share of the hint, not the hint;
//   - the read count as a floor, which is what ties the table to the block
//     actually being collated and is the whole sizing for a recorder opened
//     without a hint (the behaviour this replaced, unchanged);
//   - the ceiling, which bounds both of the above.
//
// Which of those decided the population also decides how the entry arrays are
// sized, and that is not a detail: a slot is 8 bytes and buys probe headroom, an
// entry is 40 and holds a cell.
//
//   - The hint is an estimate of the finished frontier, so the entries are sized
//     to it. Half the slots would be the same number rounded up to a power of
//     two — 8192 entries where 6556 are wanted, 65 kB of hashes and pointers for
//     a table that was never going to grow.
//   - The floor is not an estimate of anything finished: it is what has been read
//     by a moment somebody else chose, and the table will therefore grow. Entries
//     stay at half the slots there, because that is the one ratio at which both
//     dimensions run out together. Sized any tighter they alternate, and since a
//     generation rebuilds the slot array either way, alternating doubles the
//     number of rehashes.
func referencedTableFor(upTo int64, expected int) (slots, entries int) {
	if expected > referencedMaxPresizedCells {
		expected = referencedMaxPresizedCells
	}
	if upTo > referencedMaxPresizedCells {
		upTo = referencedMaxPresizedCells
	}
	population := expected * referencedFrontierPercent / 100
	// The floor is two entries per cell read, which is the population the four
	// times upTo slots this used to ask for would have held.
	fromHint := true
	if floor := 2 * int(upTo); floor > population {
		population, fromHint = floor, false
	}
	if population > referencedMaxPresizedCells {
		population = referencedMaxPresizedCells
	}

	slots = readSetInitialSlots
	for slots < 2*population {
		slots *= 2
	}
	entries = slots / 2
	// Never below the lazy default: a recorder with no hint and nothing read is
	// the ordinary small read set, and it starts where it always did.
	if fromHint && population < entries && population > readSetInitialSlots/2 {
		entries = population
	}
	return slots, entries
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

// insert records hash as billed or unbilled. It reports whether the caller's
// onRecord must fire: true for a first billed read, whether the entry is new or
// was until now unbilled; false for a repeat, and for every unbilled record.
// s.used moves only for a new entry, which is what recorded counts. Equality is
// never truncated: the fingerprint only decides whether the full hash is worth
// comparing.
func (s *readSetShard) insert(hash Hash, c *Cell, billed bool) (bill bool, added bool) {
	if status := s.status(hash); status == readSetBilled || (status == readSetUnbilled && !billed) {
		return false, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.insertLocked(hash, c, billed)
}

// insertLocked is the authoritative insert after the caller has acquired this
// shard. Both insert and recordCell probe before locking, so the locked lookup
// below resolves a concurrent insert and an unbilled-to-billed promotion.
func (s *readSetShard) insertLocked(hash Hash, c *Cell, billed bool) (bill bool, added bool) {
	fingerprint := readSetFingerprint(hash)
	table := s.table.Load()
	if table == nil {
		table = newReadSetTable(readSetInitialSlots, readSetInitialSlots/2)
		s.table.Store(table)
	} else if s.used >= len(table.hashes) || s.used*2 >= len(table.slots) {
		table = s.growLocked(table)
	}

	// The lock-free probe above ran before the mutex was taken, so another
	// goroutine may have inserted this hash in between; the probe is repeated here
	// under the lock, where it is authoritative. An unbilled entry met by a
	// billed read is promoted in place: the slot word loses its bit, and the
	// goroutine that clears it is the one that bills.
	mask := len(table.slots) - 1
	pos := int(fingerprint) & mask
	for {
		entry := table.slots[pos].Load()
		if entry == 0 {
			break
		}
		if uint32(entry>>32) == fingerprint {
			if idx := readSetSlotIndex(entry); table.hashes[idx] == hash {
				if billed && entry&readSetUnbilledBit != 0 {
					table.slots[pos].Store(entry &^ readSetUnbilledBit)
					return true, false
				}
				return false, false
			}
		}
		pos = (pos + 1) & mask
	}

	idx := s.used
	table.hashes[idx] = hash
	table.cells[idx] = c
	slot := uint64(fingerprint)<<32 | uint64(idx+1)
	if !billed {
		slot |= readSetUnbilledBit
	}
	table.slots[pos].Store(slot)
	s.used++
	return billed, true
}

// readSetEntryStatus is what a probe reports about a hash.
type readSetEntryStatus uint8

const (
	readSetAbsent readSetEntryStatus = iota
	readSetUnbilled
	readSetBilled
)

func (s *readSetShard) probe(raw []byte) readSetEntryStatus {
	table := s.table.Load()
	if table == nil {
		return readSetAbsent
	}

	fingerprint := uint32(raw[1]) | uint32(raw[2])<<8 | uint32(raw[3])<<16 | uint32(raw[4])<<24
	mask := len(table.slots) - 1
	pos := int(fingerprint) & mask
	for {
		entry := table.slots[pos].Load()
		if entry == 0 {
			return readSetAbsent
		}
		if uint32(entry>>32) == fingerprint {
			if idx := readSetSlotIndex(entry); bytes.Equal(table.hashes[idx][:], raw) {
				if entry&readSetUnbilledBit != 0 {
					return readSetUnbilled
				}
				return readSetBilled
			}
		}
		pos = (pos + 1) & mask
	}
}

// status is probe for a Hash value.
func (s *readSetShard) status(hash Hash) readSetEntryStatus {
	return s.probe(hash[:])
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
			if idx := readSetSlotIndex(entry); table.hashes[idx] == hash {
				return table.cells[idx], true
			}
		}
		pos = (pos + 1) & mask
	}
}

// lookupPos is lookup returning the entry's stable position in the current
// table generation beside the cell. The position numbers the entry arrays, so
// it is dense, stable while no insert grows the table, and free to obtain —
// the probe had it in hand. A caller keying its own flat array by it gets a
// map from recorded hashes to its values without hashing the 32-byte key a
// second time; sourceGraph is that caller.
func (s *readSetShard) lookupPos(hash Hash) (*Cell, int32, bool) {
	table := s.table.Load()
	if table == nil {
		return nil, -1, false
	}

	fingerprint := readSetFingerprint(hash)
	mask := len(table.slots) - 1
	pos := int(fingerprint) & mask
	for {
		entry := table.slots[pos].Load()
		if entry == 0 {
			return nil, -1, false
		}
		if uint32(entry>>32) == fingerprint {
			if idx := readSetSlotIndex(entry); table.hashes[idx] == hash {
				return table.cells[idx], int32(idx), true
			}
		}
		pos = (pos + 1) & mask
	}
}

// entryCapacity reports the entry-array length of the current generation, the
// exclusive upper bound of every position lookupPos can return.
func (s *readSetShard) entryCapacity() int {
	table := s.table.Load()
	if table == nil {
		return 0
	}
	return len(table.hashes)
}

// growLocked publishes a larger generation. Readers on the old table keep finding
// everything it held, so the swap needs no coordination with them.
//
// Whichever dimension ran out is the one that doubles: a shard that filled its
// entries while its slots are still half empty gets entries, and pays for probe
// headroom only when the probes actually need it. The slot array is rebuilt
// either way — the two generations cannot share it, since a reader left on the
// old table would index its shorter entry arrays with a slot the new table
// published.
func (s *readSetShard) growLocked(old *readSetTable) *readSetTable {
	slots, entries := len(old.slots), len(old.hashes)
	if s.used*2 >= slots {
		slots *= 2
	}
	if s.used >= entries {
		entries *= 2
	}
	next := newReadSetTable(slots, entries)
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
