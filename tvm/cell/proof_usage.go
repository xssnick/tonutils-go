package cell

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
)

// MerkleProofBuilder records reads of a tree and serializes them as a Merkle
// proof. It is a thin front for ReadSet, kept because it is the shape every
// proof-serving call site already has: init with a root, hand Root() to the
// reader, call CreateProof when the reader is done.
type MerkleProofBuilder struct {
	read      *ReadSet
	origRoot  *Cell
	usageRoot *Cell
}

func NewMerkleProofBuilder(root *Cell) *MerkleProofBuilder {
	b := &MerkleProofBuilder{}
	b.Init(root)
	return b
}

func (b *MerkleProofBuilder) Init(root *Cell) *Cell {
	b.origRoot = root
	if root == nil {
		b.read = nil
		b.usageRoot = nil
		return nil
	}
	b.read = NewReadSet(root)
	b.usageRoot = b.read.Root()
	return b.usageRoot
}

func (b *MerkleProofBuilder) Clear() {
	b.read = nil
	b.origRoot = nil
	b.usageRoot = nil
}

// Root returns the root the reader must read through: reads reached any other way
// are not recorded and their cells will be missing from the proof.
func (b *MerkleProofBuilder) Root() *Cell {
	if b == nil {
		return nil
	}
	return b.usageRoot
}

func (b *MerkleProofBuilder) OriginalRoot() *Cell {
	if b == nil {
		return nil
	}
	return b.origRoot
}

// ReadSet exposes the recorder, for callers that need to record a read that could
// not ride the trace or to ask what has been read so far.
func (b *MerkleProofBuilder) ReadSet() *ReadSet {
	if b == nil {
		return nil
	}
	return b.read
}

func (b *MerkleProofBuilder) CreateProof() (*Cell, error) {
	if b == nil || b.read == nil {
		return nil, fmt.Errorf("failed to build usage proof: proof builder is not initialized")
	}
	return b.read.Proof()
}

// CreateHashUsageProof creates a Merkle proof that retains exactly the cells
// whose level-zero hashes were observed by the caller. Unlike CellUsageTree,
// this selection is independent of the path through which a cell was loaded.
// That distinction is required when a dictionary split or merge rebuilds a
// logically identical subtree under a synthetic path while the proof is rooted
// in the original state tree.
func (c *Cell) CreateHashUsageProof(isLoaded func(Hash) bool) (*Cell, error) {
	return c.CreateHashUsageProofResolved(isLoaded, nil)
}

// CreateHashUsageProofResolved is CreateHashUsageProof for a caller that already
// holds the loaded form of the source cells — a ReadSet recorded over this very
// tree, through ReadSet.RecordedCell. Handing them back keeps the build from
// resolving a lazy source cell through its loader a second time, which for a
// disk-backed loader is a second read.
//
// The proof is byte-identical either way: a recorded cell and the cell its
// loader would return are the same cell, which is the same equality
// ReadSet.Proof already relies on. resolveLoaded may return nil for any hash,
// and the build then resolves it itself.
func (c *Cell) CreateHashUsageProofResolved(
	isLoaded func(Hash) bool,
	resolveLoaded func(Hash) *Cell,
) (*Cell, error) {
	return c.CreateHashUsageProofResolvedSized(isLoaded, resolveLoaded, 0)
}

// CreateHashUsageProofResolvedSized is CreateHashUsageProofResolved for a caller
// that can say roughly how many cells the walk will visit. The build memoises
// every cell it finishes, and the memo is the largest scratch structure a
// block-sized proof allocates: reached from nothing it doubles two dozen times
// and discards three quarters of everything it ever allocated. A caller holding
// a read set over the same tree has the number to hand.
//
// The estimate changes no byte of the proof — it is a capacity and nothing else.
// A wrong one costs only the growth it failed to avoid, and nothing is retained
// between proofs.
func (c *Cell) CreateHashUsageProofResolvedSized(
	isLoaded func(Hash) bool,
	resolveLoaded func(Hash) *Cell,
	expectedCells int,
) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("failed to generate Merkle proof: cell is nil")
	}
	if c.Level() != 0 {
		return nil, fmt.Errorf("failed to generate Merkle proof: level is not 0")
	}
	if isLoaded == nil {
		return nil, fmt.Errorf("failed to build hash usage proof: loaded-cell selector is nil")
	}

	root := true
	state := merkleProofPruneBuildState{
		shouldPrune: func(_ *Cell, _ int, hash Hash) (*Cell, bool, error) {
			// MerkleProof::generate always materializes the proof body root. The
			// loaded-cell predicate selects only descendants; pruning the root
			// would leave a virtual state with no shape to which a block Merkle
			// update can be applied.
			if root {
				root = false
				return nil, false, nil
			}
			return nil, !isLoaded(hash), nil
		},
		resolveLoaded:       resolveLoaded,
		arena:               &proofCellArena{},
		memoHint:            expectedCells,
		pruneUnloadedLeaves: true,
	}
	body, _, err := state.build(c, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build hash usage proof: %w", err)
	}
	return CreateMerkleProof(body)
}

// CreateHashUsageProofResolvedSizedParallel is CreateHashUsageProofResolvedSized
// with the subtree walk split across parallelism branch workers, the same
// machinery createMerkleUpdateRaw arms for the state update's two proofs. The
// prize is the same too: the walk is hashing and cloning over an already
// resident selection, so branches split real work.
//
// Two differences from the serial entry, both deliberate:
//   - The root exemption is keyed by the root's hash instead of a captured
//     boolean. Branch workers call the prune callback concurrently, and a
//     mutated capture would be a data race; the hash comparison is pure. The
//     two are equivalent because a DAG cell cannot be its own descendant, so
//     the root's hash occurs exactly once on the walk.
//   - parallelism below two, or a selection smaller than the parallel
//     threshold, falls back to the serial walk — same rule as the update.
func (c *Cell) CreateHashUsageProofResolvedSizedParallel(
	isLoaded func(Hash) bool,
	resolveLoaded func(Hash) *Cell,
	expectedCells int,
	parallelism int,
) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("failed to generate Merkle proof: cell is nil")
	}
	if c.Level() != 0 {
		return nil, fmt.Errorf("failed to generate Merkle proof: level is not 0")
	}
	if isLoaded == nil {
		return nil, fmt.Errorf("failed to build hash usage proof: loaded-cell selector is nil")
	}
	if parallelism < 2 || expectedCells < proofParallelMinCells {
		return c.CreateHashUsageProofResolvedSized(isLoaded, resolveLoaded, expectedCells)
	}

	rootHash := c.HashKey()
	state := merkleProofPruneBuildState{
		shouldPrune: func(_ *Cell, _ int, hash Hash) (*Cell, bool, error) {
			// MerkleProof::generate always materializes the proof body root;
			// see CreateHashUsageProofResolvedSized.
			if hash == rootHash {
				return nil, false, nil
			}
			return nil, !isLoaded(hash), nil
		},
		resolveLoaded:       resolveLoaded,
		arena:               &proofCellArena{},
		memoHint:            expectedCells,
		parallelism:         parallelism,
		parallel:            newProofParallelCache(expectedCells),
		pruneUnloadedLeaves: true,
	}
	// One entry slab for every build state of this walk, sized from the same
	// estimate that plans the walk: the branch split conserves the estimate but
	// distributes it by subtree depth, which misplaces most of it, while the
	// walk's total population tracks the estimate itself.
	state.built.slab = newProofBuildSlab(expectedCells)
	body, _, err := state.build(c, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to build hash usage proof: %w", err)
	}
	return CreateMerkleProof(body)
}

// merkleProofPruneFunc decides whether a destination cell may be replaced by
// a pruned boundary. It receives the merkle depth so a caller can key the cell
// by the same hash the boundary will carry, plus that hash itself, which the
// build state has already computed. When the caller asked for the applied root
// it also returns the source subtree standing behind the boundary.
type merkleProofPruneFunc func(c *Cell, merkleDepth int, hash Hash) (*Cell, bool, error)

// Small proofs keep their memo table on the stack; larger DAGs spill once to
// a map. The callback must still observe C++ first-visit semantics.
const merkleProofPruneInlineCacheSize = 8

func buildMerkleProofBodyByPruneFunc(c *Cell, shouldPrune merkleProofPruneFunc, merkleDepth int) (*Cell, error) {
	return buildMerkleProofBodyByPruneFuncResolved(c, shouldPrune, nil, merkleDepth, 0)
}

// buildMerkleProofBodyByPruneFuncResolved additionally lets the caller hand back
// the loaded form of a cell it already holds, so building the proof does not
// resolve a lazy source cell a second time, and take the memo capacity the walk
// should start from.
func buildMerkleProofBodyByPruneFuncResolved(
	c *Cell,
	shouldPrune merkleProofPruneFunc,
	resolveLoaded func(Hash) *Cell,
	merkleDepth int,
	expectedCells int,
) (*Cell, error) {
	state := merkleProofPruneBuildState{
		shouldPrune:   shouldPrune,
		resolveLoaded: resolveLoaded,
		arena:         &proofCellArena{},
		memoHint:      expectedCells,
	}
	proof, _, err := state.build(c, merkleDepth)
	return proof, err
}

type merkleProofPruneCacheEntry struct {
	key     proofBodyKey
	value   *Cell
	applied *Cell
}

type merkleProofPruneBuildState struct {
	shouldPrune merkleProofPruneFunc
	// wantApplied makes the same walk also assemble the destination root as
	// applying the update would rebuild it: every pruned boundary becomes the
	// source subtree the callback handed back, and every cell above one is
	// rebuilt around it.
	wantApplied bool
	// resolveLoaded hands back the already-loaded form of a cell the recorder
	// kept. Without it a lazy source cell is resolved through its loader a second
	// time while the proof is being built, which is both a wasted load and, for a
	// disk-backed loader, a wasted read.
	resolveLoaded func(Hash) *Cell
	built         proofBuildTable
	spilled       bool
	inline        [merkleProofPruneInlineCacheSize]merkleProofPruneCacheEntry
	inlineLen     uint8
	// prunedShared lets the source proof reuse a boundary this walk already
	// built. Both sides prune the same subtree roots, and a pruned branch is a
	// function of the subtree hash and the level it is cut at.
	prunedShared map[proofBodyKey]*Cell
	// memoHint is the caller's estimate of how many cells the walk will finish,
	// used to size built when the inline cache spills. Capacity only: it never
	// reaches the produced bytes, and an estimate that is wrong costs no more
	// than the growth it failed to avoid.
	memoHint int
	// arena supplies the proof body's cells. A nil arena allocates each cell on
	// its own, which is what a caller keeping built cells beyond the proof must
	// use.
	arena *proofCellArena

	parallelism int
	parallel    *proofParallelCache

	// pruneUnloadedLeaves makes a cell the callback declined into a pruned
	// branch even when it is a resident leaf. createPrunedBranchInto otherwise
	// keeps such a leaf whole, which is CellBuilder::create_pruned_branch's rule
	// (CellBuilder.cpp:100-108) — for a cell that is_loaded(). The hash usage
	// proof is the one build whose callback has already answered that question:
	// a hash it declines is a cell the recorder never saw, and in the reference
	// collator that cell is an ExtCell nobody loaded, so create_pruned_branch
	// prunes it there. Over a resident predecessor every cell is loaded, and the
	// port kept every unread leaf next to a read cell whole — each queued message
	// body and StateInit leaf under the split filter's reads, for one.
	//
	// Measured on the stand, 2026-09-03, the first post-split blocks of two
	// splits: 2,498,713 and 2,461,902 bytes of collated data against 2,168,633
	// and 2,160,356 for the reference's sibling blocks over the same parents,
	// with the blocks themselves within 0.3%. On the deep-queue fixture (8,192
	// entries, half with referenced bodies) the previous-state proof goes from
	// 1,585,867 to 1,553,125 bytes and its unread leaves from 4,096 whole cells
	// to 4,099 pruned branches; the block is unchanged.
	//
	// ReadSet.Proof and the state update keep the loaded-leaf rule: the C++
	// goldens in readset_golden_test.go pin the reference over an in-memory
	// tree, where every leaf is loaded, and the update's own callback never
	// declines a resident leaf.
	pruneUnloadedLeaves bool
}

// prunedBranch builds the boundary for a cell the callback declined.
func (s *merkleProofPruneBuildState) prunedBranch(c *Cell, newLevel int) (*Cell, error) {
	if s.pruneUnloadedLeaves {
		return buildPrunedBranchFromCellAtDepth(c, newLevel, _DataCellMaxLevel, s.arena)
	}
	return createPrunedBranchFromCellInto(c, newLevel, s.arena)
}

func (s *merkleProofPruneBuildState) build(c *Cell, merkleDepth int) (*Cell, *Cell, error) {
	if s.parallelism > 1 {
		return s.buildParallel(c, merkleDepth)
	}
	if c == nil {
		return nil, nil, fmt.Errorf("cell is nil")
	}

	hash := c.HashKey()
	key := proofBodyKey{hash: hash, merkleDepth: merkleDepth}
	if built, applied, ok := s.cached(key); ok {
		return built, applied, nil
	}

	if s.shouldPrune != nil {
		source, pruned, err := s.shouldPrune(c, merkleDepth, hash)
		if err != nil {
			return nil, nil, err
		}
		if pruned {
			built, err := s.prunedBranch(c, merkleDepth+1)
			if err != nil {
				return nil, nil, err
			}
			s.shareBoundary(c, key, built)
			s.cacheBuilt(key, built, source)
			return built, source, nil
		}
	}

	loaded := c
	if s.resolveLoaded != nil {
		if recorded := s.resolveLoaded(hash); recorded != nil {
			loaded = recorded
		}
	}
	if loaded.IsLazy() || loaded == c {
		var err error
		if loaded, err = c.load(); err != nil {
			return nil, nil, err
		}
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		built := loaded.WithoutTrace()
		s.cacheBuilt(key, built, built)
		return built, built, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	var appliedBuf [4]*Cell
	appliedRefs := appliedBuf[:refCnt]
	refView := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	substituted := false
	for i := 0; i < refCnt; i++ {
		ref, err := merkleProofRef(c, refView, i)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}
		next, applied, err := s.build(ref, childDepth)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
		}
		refs[i] = next
		appliedRefs[i] = applied
		substituted = substituted || applied != next
	}

	arena := s.arena
	if s.wantApplied && !substituted {
		arena = nil
	}
	built, err := cloneProofCellWithRefs(loaded, refView, refs, arena)
	if err != nil {
		return nil, nil, err
	}
	applied := built
	if s.wantApplied && substituted {
		applied, err = cloneAppliedCellWithRefs(loaded, refView, appliedRefs)
		if err != nil {
			return nil, nil, err
		}
	}
	s.cacheBuilt(key, built, applied)
	return built, applied, nil
}

func (s *merkleProofPruneBuildState) buildParallel(c *Cell, merkleDepth int) (*Cell, *Cell, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("cell is nil")
	}

	hash := c.HashKey()
	key := proofBodyKey{hash: hash, merkleDepth: merkleDepth}
	if built, applied, ok := s.cached(key); ok {
		return built, applied, nil
	}

	if s.shouldPrune != nil {
		source, pruned, err := s.shouldPrune(c, merkleDepth, hash)
		if err != nil {
			return nil, nil, err
		}
		if pruned {
			built, err := s.prunedBranch(c, merkleDepth+1)
			if err != nil {
				return nil, nil, err
			}
			s.shareBoundary(c, key, built)
			s.cacheBuilt(key, built, source)
			return built, source, nil
		}
	}

	loaded := c
	if s.resolveLoaded != nil {
		if recorded := s.resolveLoaded(hash); recorded != nil {
			loaded = recorded
		}
	}
	if loaded.IsLazy() || loaded == c {
		var err error
		if loaded, err = c.load(); err != nil {
			return nil, nil, err
		}
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		built := loaded.WithoutTrace()
		s.cacheBuilt(key, built, built)
		return built, built, nil
	}

	var sourceRefsBuf [4]*Cell
	sourceRefs := sourceRefsBuf[:refCnt]
	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	var appliedBuf [4]*Cell
	appliedRefs := appliedBuf[:refCnt]
	refView := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref, err := merkleProofRef(c, refView, i)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}
		sourceRefs[i] = ref
	}

	if plan, ok := planProofParallelBranch(sourceRefs, s.parallelism, s.memoHint); ok {
		branch := s.parallelTask(plan.branchWorkers, plan.branchHint)
		branch.done.Add(1)
		go buildMerkleProofParallelBranch(
			branch,
			plan.branch,
			sourceRefs[plan.branch],
			childDepth,
		)

		remainingErrIndex := len(sourceRefs)
		var remainingErr error
		previousParallelism, previousHint := s.parallelism, s.memoHint
		s.parallelism, s.memoHint = plan.remainingWorkers, plan.remainingHint
		for i, ref := range sourceRefs {
			if i == plan.branch || remainingErr != nil {
				continue
			}
			refs[i], appliedRefs[i], remainingErr = s.build(ref, childDepth)
			if remainingErr != nil {
				remainingErrIndex = i
				remainingErr = fmt.Errorf("failed to proof %d ref: %w", i, remainingErr)
			}
		}
		s.parallelism, s.memoHint = previousParallelism, previousHint
		branch.done.Wait()
		result := branch.result
		refs[result.index] = result.built
		appliedRefs[result.index] = result.applied
		if result.err != nil {
			result.err = fmt.Errorf("failed to proof %d ref: %w", result.index, result.err)
		}
		if result.err != nil && result.index < remainingErrIndex {
			return nil, nil, result.err
		}
		if remainingErr != nil {
			return nil, nil, remainingErr
		}
		if result.err != nil {
			return nil, nil, result.err
		}
	} else {
		for i, ref := range sourceRefs {
			next, applied, err := s.build(ref, childDepth)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
			refs[i] = next
			appliedRefs[i] = applied
		}
	}

	substituted := false
	for i := range refs {
		substituted = substituted || appliedRefs[i] != refs[i]
	}

	// With nothing substituted below it this same cell is also the applied
	// cell, which the caller keeps as part of the new state root; that outlives
	// the proof, so it must not hold a slab of proof cells with it.
	arena := s.arena
	if s.wantApplied && !substituted {
		arena = nil
	}
	built, err := cloneProofCellWithRefs(loaded, refView, refs, arena)
	if err != nil {
		return nil, nil, err
	}
	applied := built
	if s.wantApplied && substituted {
		applied, err = cloneAppliedCellWithRefs(loaded, refView, appliedRefs)
		if err != nil {
			return nil, nil, err
		}
	}
	s.cacheBuilt(key, built, applied)
	return built, applied, nil
}

type merkleProofParallelTask struct {
	state merkleProofPruneBuildState
	arena proofCellArena
	done  sync.WaitGroup

	result merkleProofParallelResult
}

type merkleProofParallelResult struct {
	index   int
	built   *Cell
	applied *Cell
	err     error
}

func buildMerkleProofParallelBranch(
	task *merkleProofParallelTask,
	index int,
	ref *Cell,
	merkleDepth int,
) {
	defer task.done.Done()

	built, applied, err := task.state.build(ref, merkleDepth)
	task.result = merkleProofParallelResult{index: index, built: built, applied: applied, err: err}
}

// shareBoundary keeps a boundary the destination walk built so the source proof
// can reuse it instead of building an equal one. Only cells with references are
// offered: a childless one goes through the materializing fast path, whose
// result depends on the cell it was asked about and not only on its hash.
func (s *merkleProofPruneBuildState) shareBoundary(c *Cell, key proofBodyKey, built *Cell) {
	if c.refsCount() == 0 || built.GetType() != PrunedCellType {
		return
	}
	if s.parallel != nil {
		s.parallel.storeBoundary(key, built)
		return
	}
	if s.prunedShared == nil {
		return
	}
	if _, ok := s.prunedShared[key]; !ok {
		s.prunedShared[key] = built
	}
}

func (s *merkleProofPruneBuildState) parallelTask(workers, hint int) *merkleProofParallelTask {
	task := &merkleProofParallelTask{
		arena: newParallelProofArena(hint),
	}
	task.state = merkleProofPruneBuildState{
		shouldPrune:         s.shouldPrune,
		wantApplied:         s.wantApplied,
		resolveLoaded:       s.resolveLoaded,
		memoHint:            hint,
		arena:               &task.arena,
		parallelism:         workers,
		parallel:            s.parallel,
		pruneUnloadedLeaves: s.pruneUnloadedLeaves,
	}
	// The branch memoises into the walk's shared slab: the depth-planned hint
	// splits the worker budget well but the storage badly, and the slab is what
	// makes that misplacement free. Slots stay branch-local.
	task.state.built.slab = s.built.slab

	return task
}

// cloneAppliedCellWithRefs rebuilds an applied cell around substituted children.
// Its bits are the ones the caller assembled and every child carries the hash
// the destination child had, so level 0 is the only significant level and its
// hash and depth are the source cell's.
func cloneAppliedCellWithRefs(src *Cell, view cellRefView, refs []*Cell) (*Cell, error) {
	if src.IsSpecial() || view.virtual {
		rebuilt, _, err := view.cloneWithRefs(refs, nil)
		return rebuilt, err
	}

	cloned := new(Cell)
	*cloned = *src
	cloned.meta = nil
	for i, ref := range refs {
		cloned.setRef(i, ref)
	}
	if err := cloned.refreshLevelMaskForRefs(); err != nil {
		return nil, err
	}
	if cloned.getLevelMask().getHashIndex() != 0 {
		// Not reachable while the applied tree stays at level 0; recompute
		// rather than trust the invariant.
		if err := cloned.calculateHashes(); err != nil {
			return nil, err
		}
		return cloned, nil
	}
	cloned.clearExtraHashes()
	cloned.setHashAt(0, src.getHash(0))
	cloned.setDepthAt(0, src.getDepth(0))
	return cloned, nil
}

func (s *merkleProofPruneBuildState) cacheBuilt(key proofBodyKey, c, applied *Cell) {
	if s.parallel != nil {
		s.parallel.memo.Add(1)
	}
	if s.spilled {
		s.built.store(key.hash, key.merkleDepth, c, applied)
		return
	}
	if int(s.inlineLen) < len(s.inline) {
		s.inline[s.inlineLen] = merkleProofPruneCacheEntry{key: key, value: c, applied: applied}
		s.inlineLen++
		return
	}

	s.spilled = true
	if len(s.built.slots) == 0 {
		s.built.init(max(merkleProofPruneInlineCacheSize*2, s.memoHint))
	}
	for i := range s.inlineLen {
		entry := s.inline[i]
		s.built.store(entry.key.hash, entry.key.merkleDepth, entry.value, entry.applied)
	}
	s.built.store(key.hash, key.merkleDepth, c, applied)
}

// memoSize is how many cells the walk memoised. It is what a caller sizing the
// next walk of the same shape should hand back as memoHint; the walk itself
// never reads it.
func (s *merkleProofPruneBuildState) memoSize() int {
	if s.parallel != nil {
		return s.parallel.size()
	}
	if s.spilled {
		return s.built.count
	}

	return int(s.inlineLen)
}

func (s *merkleProofPruneBuildState) cached(key proofBodyKey) (*Cell, *Cell, bool) {
	if s.spilled {
		return s.built.lookup(key.hash, key.merkleDepth)
	}
	for i := range s.inlineLen {
		if entry := s.inline[i]; entry.key == key {
			return entry.value, entry.applied, true
		}
	}
	return nil, nil, false
}

// proofCellWithHashes packs a rebuilt proof cell with the metadata and extra
// hash storage it almost always needs into a single heap object.
type proofCellWithHashes struct {
	c Cell
	m cellMeta
	h [3]Hash
}

// Slabs start small because most proofs are small — an account storage proof is
// a handful of cells — and double towards the block-sized ones, so a whole
// state merkle update pays a few dozen allocations instead of thousands while a
// tiny proof still wastes only a few unused slots.
//
// The ceiling keeps the largest slab inside the runtime's small-object size
// classes: the widest element here is 240 bytes, so 64 of them is under the 32
// KiB above which an allocation is served as a large object off the page
// allocator. Raising it past that trades the allocations this saves for a
// slower allocation path, and the count barely improves — a block-sized proof
// already amortises to a few dozen slabs at this size.
const (
	proofArenaFirstSlab = 8
	proofArenaMaxSlab   = 64
)

// proofCellArena hands out the cells a proof body is made of. Those cells are
// built together, serialized together and dropped together, so taking them from
// slabs costs one allocation per slab rather than one per cell without making
// anything outlive what it already did. A cell that the caller knows will be
// kept by something longer-lived than the proof — the destination root the
// applied update returns — must be allocated on its own instead, or its whole
// slab is held with it.
//
// The zero value is unusable on purpose: a nil arena is the "allocate
// individually" path, which is what every caller outside the proof builders
// gets.
//
// CAPACITY ONLY — an arena is per-proof and must stay that way. Slab sizes may
// be tuned freely; reusing a slab across proofs must not be attempted, by a
// sync.Pool or by hanging one off a longer-lived struct. body() and
// prunedBranch() hand out interior pointers into a slab, and those pointers
// become cells of the produced proof: a state update's cells reach the published
// state and the serialized block, and the previous-state proof's cells reach the
// candidate's collated data, which a background goroutine is still serializing
// after the collation that built it has returned. A recycled slab therefore
// aliases one block's proof cells into the next block's proof and writes into
// memory the previous block's broadcast is still reading. This project has a
// name for that failure class: it is how a block once came out 41x too large.
type proofCellArena struct {
	bodies     []proofCellWithHashes
	bodySlab   int
	pruned     []prunedBranchCell
	prunedSlab int
}

func nextProofSlabSize(current int) int {
	switch {
	case current == 0:
		return proofArenaFirstSlab
	case current >= proofArenaMaxSlab:
		return proofArenaMaxSlab
	default:
		return current * 2
	}
}

func (a *proofCellArena) body() *proofCellWithHashes {
	if a == nil {
		return new(proofCellWithHashes)
	}
	if len(a.bodies) == 0 {
		a.bodySlab = nextProofSlabSize(a.bodySlab)
		a.bodies = make([]proofCellWithHashes, a.bodySlab)
	}
	out := &a.bodies[0]
	a.bodies = a.bodies[1:]
	return out
}

func (a *proofCellArena) prunedBranch() *prunedBranchCell {
	if a == nil {
		return new(prunedBranchCell)
	}
	if len(a.pruned) == 0 {
		a.prunedSlab = nextProofSlabSize(a.prunedSlab)
		a.pruned = make([]prunedBranchCell, a.prunedSlab)
	}
	out := &a.pruned[0]
	a.pruned = a.pruned[1:]
	return out
}

type proofBodyKey struct {
	hash        Hash
	merkleDepth int
}

// proofBodyBuildState tracks the cells a proof body must include. Presence of a
// hash in cells marks it kept; a non-nil value also caches its loaded form for
// the build path.
type proofBodyBuildState struct {
	cells map[Hash]*Cell
	built map[proofBodyKey]*Cell
	// prunedShared holds boundaries a paired destination walk already built.
	prunedShared   map[proofBodyKey]*Cell
	prunedParallel *proofParallelCache
	arena          *proofCellArena

	parallelism int
	memoHint    int
}

func (s *proofBodyBuildState) cacheLoaded(hash Hash, c *Cell) {
	if s.cells == nil {
		s.cells = map[Hash]*Cell{}
	}
	s.cells[hash] = c
}

func (s *proofBodyBuildState) cacheBuilt(key proofBodyKey, c *Cell) {
	if s.built == nil {
		s.built = map[proofBodyKey]*Cell{}
	}
	s.built[key] = c
}

func (s *proofBodyBuildState) prepareBuiltCache() {
	if s.built == nil && len(s.cells) > 0 {
		// Hint from cells actually reached during tracking, never from arena
		// NodeCount: sparse usage trees stay small while dense proofs avoid the
		// repeated growth of their second build map.
		s.built = make(map[proofBodyKey]*Cell, len(s.cells))
	}
}

func (s *proofBodyBuildState) cachedBuilt(key proofBodyKey) *Cell {
	return s.built[key]
}

func (s *proofBodyBuildState) sharedBoundary(key proofBodyKey) (*Cell, bool) {
	if s.prunedParallel != nil {
		return s.prunedParallel.loadBoundary(key)
	}
	boundary, ok := s.prunedShared[key]

	return boundary, ok
}

func (s *proofBodyBuildState) parallelTask(workers, hint int) *proofBodyParallelTask {
	task := &proofBodyParallelTask{
		arena: newParallelProofArena(hint),
	}
	task.state = proofBodyBuildState{
		cells:          s.cells,
		prunedShared:   s.prunedShared,
		prunedParallel: s.prunedParallel,
		arena:          &task.arena,
		parallelism:    workers,
		memoHint:       hint,
		built:          make(map[proofBodyKey]*Cell, hint),
	}

	return task
}

// newParallelProofArena skips the tiny growth slabs when the branch's share of
// the previous walk says it will immediately outgrow them. Bodies and pruned
// boundaries share the hint conservatively; either side can still grow through
// the ordinary bounded slab sequence when the shape differs from the estimate.
func newParallelProofArena(hint int) proofCellArena {
	target := proofArenaFirstSlab
	for target < proofArenaMaxSlab && target < hint/2 {
		target *= 2
	}
	if target == proofArenaFirstSlab {
		return proofCellArena{}
	}

	previous := target / 2
	return proofCellArena{bodySlab: previous, prunedSlab: previous}
}

func loadedForBoundary(boundary, loaded *Cell) *Cell {
	if boundary == nil || loaded == nil || boundary.HashKey() == loaded.HashKey() {
		return loaded
	}

	raw := loaded.rawCell()
	if trace := loaded.Trace(); trace != nil && raw.Trace() != trace {
		raw = raw.WithTrace(trace)
	}
	return raw.Virtualize(uint8(boundary.currentEffectiveLevel()))
}

func buildRecordedProofBody(c *Cell, state *proofBodyBuildState, merkleDepth int) (*Cell, error) {
	if c == nil {
		return nil, fmt.Errorf("cell is nil")
	}
	hash := c.HashKey()
	cached, visited := state.cells[hash]
	return buildProofBodyCell(c, hash, cached, visited, state, merkleDepth)
}

// buildProofBodyCell takes the cell's hash and its entry in the visited set
// from the caller: the reference loop looked both up to decide how to descend,
// and looking them up again at the top of the call is the same two probes over
// a 32-byte key.
func buildProofBodyCell(c *Cell, hash Hash, cached *Cell, visited bool, state *proofBodyBuildState, merkleDepth int) (*Cell, error) {
	if state.parallelism > 1 {
		return buildProofBodyCellParallel(c, hash, cached, visited, state, merkleDepth)
	}
	key := proofBodyKey{hash: hash, merkleDepth: merkleDepth}
	if built := state.cachedBuilt(key); built != nil {
		return built, nil
	}

	if !visited {
		pruned, ok := state.sharedBoundary(key)
		if !ok || c.refsCount() == 0 {
			var err error
			pruned, err = createPrunedBranchFromCellInto(c, merkleDepth+1, state.arena)
			if err != nil {
				return nil, err
			}
		}
		state.cacheBuilt(key, pruned)
		return pruned, nil
	}

	loaded := c
	if cached != nil {
		loaded = cached
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		built := loaded.WithoutTrace()
		state.cacheBuilt(key, built)
		return built, nil
	}

	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	refView := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref, err := merkleProofRef(loaded, refView, i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}

		refHash := ref.HashKey()
		loadedRef, refVisited := state.cells[refHash]
		if refVisited && loadedRef == nil {
			loadedRef, err = ref.load()
			if err != nil {
				return nil, fmt.Errorf("failed to load %d ref: %w", i, err)
			}
			state.cacheLoaded(refHash, loadedRef)
		}
		next, err := buildProofBodyCell(ref, refHash, loadedRef, refVisited, state, childDepth)
		if err != nil {
			return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
		}
		refs[i] = next
	}

	rebuilt, err := cloneProofCellWithRefs(loaded, refView, refs, state.arena)
	if err != nil {
		return nil, err
	}
	state.cacheBuilt(key, rebuilt)
	return rebuilt, nil
}

func buildProofBodyCellParallel(
	c *Cell,
	hash Hash,
	cached *Cell,
	visited bool,
	state *proofBodyBuildState,
	merkleDepth int,
) (*Cell, error) {
	key := proofBodyKey{hash: hash, merkleDepth: merkleDepth}
	if built := state.cachedBuilt(key); built != nil {
		return built, nil
	}

	if !visited {
		pruned, ok := state.sharedBoundary(key)
		if !ok || c.refsCount() == 0 {
			var err error
			pruned, err = createPrunedBranchFromCellInto(c, merkleDepth+1, state.arena)
			if err != nil {
				return nil, err
			}
		}
		state.cacheBuilt(key, pruned)
		return pruned, nil
	}

	loaded := c
	if cached != nil {
		loaded = cached
	}

	refCnt := loaded.refsCount()
	if refCnt == 0 {
		built := loaded.WithoutTrace()
		state.cacheBuilt(key, built)
		return built, nil
	}

	var sourceRefsBuf [4]*Cell
	sourceRefs := sourceRefsBuf[:refCnt]
	var sourceHashesBuf [4]Hash
	sourceHashes := sourceHashesBuf[:refCnt]
	var loadedRefsBuf [4]*Cell
	loadedRefs := loadedRefsBuf[:refCnt]
	var visitedRefsBuf [4]bool
	visitedRefs := visitedRefsBuf[:refCnt]
	var refsBuf [4]*Cell
	refs := refsBuf[:refCnt]
	refView := newCellRefView(loaded)
	childDepth := merkleChildDepth(loaded, merkleDepth)
	for i := 0; i < refCnt; i++ {
		ref, err := merkleProofRef(loaded, refView, i)
		if err != nil {
			return nil, fmt.Errorf("failed to peek %d ref: %w", i, err)
		}

		refHash := ref.HashKey()
		loadedRef, refVisited := state.cells[refHash]
		if refVisited && loadedRef == nil {
			loadedRef, err = ref.load()
			if err != nil {
				return nil, fmt.Errorf("failed to load %d ref: %w", i, err)
			}
			state.cacheLoaded(refHash, loadedRef)
		}
		sourceRefs[i] = ref
		sourceHashes[i] = refHash
		loadedRefs[i] = loadedRef
		visitedRefs[i] = refVisited
	}

	if plan, ok := planProofParallelBranch(sourceRefs, state.parallelism, state.memoHint); ok {
		branch := state.parallelTask(plan.branchWorkers, plan.branchHint)
		branch.done.Add(1)
		go buildProofBodyParallelBranch(
			branch,
			plan.branch,
			sourceRefs[plan.branch],
			sourceHashes[plan.branch],
			loadedRefs[plan.branch],
			visitedRefs[plan.branch],
			childDepth,
		)

		remainingErrIndex := len(sourceRefs)
		var remainingErr error
		previousParallelism, previousHint := state.parallelism, state.memoHint
		state.parallelism, state.memoHint = plan.remainingWorkers, plan.remainingHint
		for i, ref := range sourceRefs {
			if i == plan.branch || remainingErr != nil {
				continue
			}
			refs[i], remainingErr = buildProofBodyCell(
				ref, sourceHashes[i], loadedRefs[i], visitedRefs[i], state, childDepth,
			)
			if remainingErr != nil {
				remainingErrIndex = i
				remainingErr = fmt.Errorf("failed to proof %d ref: %w", i, remainingErr)
			}
		}
		state.parallelism, state.memoHint = previousParallelism, previousHint
		branch.done.Wait()
		result := branch.result
		refs[result.index] = result.built
		if result.err != nil {
			result.err = fmt.Errorf("failed to proof %d ref: %w", result.index, result.err)
		}
		if result.err != nil && result.index < remainingErrIndex {
			return nil, result.err
		}
		if remainingErr != nil {
			return nil, remainingErr
		}
		if result.err != nil {
			return nil, result.err
		}
	} else {
		for i, ref := range sourceRefs {
			next, err := buildProofBodyCell(
				ref, sourceHashes[i], loadedRefs[i], visitedRefs[i], state, childDepth,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to proof %d ref: %w", i, err)
			}
			refs[i] = next
		}
	}

	rebuilt, err := cloneProofCellWithRefs(loaded, refView, refs, state.arena)
	if err != nil {
		return nil, err
	}
	state.cacheBuilt(key, rebuilt)
	return rebuilt, nil
}

type proofBodyParallelTask struct {
	state proofBodyBuildState
	arena proofCellArena
	done  sync.WaitGroup

	result proofBodyParallelResult
}

type proofBodyParallelResult struct {
	index int
	built *Cell
	err   error
}

func buildProofBodyParallelBranch(
	task *proofBodyParallelTask,
	index int,
	ref *Cell,
	hash Hash,
	loaded *Cell,
	visited bool,
	merkleDepth int,
) {
	defer task.done.Done()

	built, err := buildProofBodyCell(ref, hash, loaded, visited, &task.state, merkleDepth)
	task.result = proofBodyParallelResult{index: index, built: built, err: err}
}

// cloneProofCellWithRefs rebuilds a proof-body cell in one copy with the same
// bits, replaced references and no traversal trace. It returns the source when
// neither its references nor its view changed.
func cloneProofCellWithRefs(src *Cell, view cellRefView, refs []*Cell, arena *proofCellArena) (*Cell, error) {
	if src.Trace() == nil && !view.virtual {
		unchanged := true
		for i, ref := range refs {
			if ref != src.refs[i] {
				unchanged = false
				break
			}
		}
		if unchanged {
			return src, nil
		}
	}

	if !src.IsSpecial() {
		// an ordinary rebuilt cell needs nothing from the source meta: the
		// trace and lazy loader must not carry over, virtualization is
		// resolved by the explicit refs, and hashes are reseeded below.
		// Almost every rebuilt proof cell gains a pruned descendant and so
		// needs metadata and an extra-hash array; taking all three from one
		// object makes it one allocation instead of three.
		fused := arena.body()
		cloned := &fused.c
		*cloned = *src
		cloned.meta = nil
		for i, ref := range refs {
			cloned.setRef(i, ref)
		}
		if err := cloned.refreshLevelMaskForRefs(); err != nil {
			return nil, err
		}
		if err := calculateSeededProofHashes(cloned, src, fused); err != nil {
			return nil, err
		}
		return cloned, nil
	}

	cloned := src.copy()
	cloned.clearVirtualization()
	if cloned.meta != nil {
		cloned.meta.trace = nil
		cloned.clearMetaIfEmpty()
	}
	for i, ref := range refs {
		cloned.setRef(i, ref)
	}
	if err := cloned.refreshLevelMaskForRefs(); err != nil {
		return nil, err
	}
	if err := cloned.calculateHashes(); err != nil {
		return nil, err
	}
	return cloned, nil
}

// calculateSeededProofHashes finalizes a rebuilt ordinary proof-body cell
// reusing the Merkle invariant that its level-0 hash and depth equal the
// source cell's: the body bits are identical and every child is the source
// subtree either included (same level-0 hash by induction) or replaced by a
// pruned branch that preserves it. Only the higher significant levels
// introduced by pruned children are hashed, mirroring the corresponding
// levels of calculateHashes.
func calculateSeededProofHashes(cloned, src *Cell, storage *proofCellWithHashes) error {
	levelMask := cloned.getLevelMask()
	if levelMask.getHashIndex() == 0 {
		cloned.clearExtraHashes()
		cloned.setHashAt(0, src.getHash(0))
		cloned.setDepthAt(0, src.getDepth(0))
		return nil
	}

	var meta *cellMeta
	if storage != nil && cloned == &storage.c {
		storage.m = cellMeta{extraHashes: &storage.h}
		cloned.meta = &storage.m
		meta = &storage.m
	} else {
		meta = cloned.ensureMeta()
		if meta.extraHashes == nil {
			meta.extraHashes = new([3]Hash)
		}
	}
	meta.extraDepths = [3]uint16{}

	cloned.setHashAt(0, src.getHash(0))
	cloned.setDepthAt(0, src.getDepth(0))

	refCnt := cloned.refsCount()
	level := levelMask.GetLevel()
	var hashBuf [2 + maxCellDataBytes + (4 * depthSize) + (4 * hashSize)]byte
	hashIndex := 1
	for levelIndex := 1; levelIndex <= level; levelIndex++ {
		if !levelMask.IsSignificant(levelIndex) {
			continue
		}

		dsc1, dsc2 := cloned.descriptors(levelMask.Apply(levelIndex))
		hashBuf[0], hashBuf[1] = dsc1, dsc2
		bufPos := 2
		bufPos += copy(hashBuf[bufPos:], cloned.hashAt(hashIndex-1))

		var depth uint16
		for i := 0; i < refCnt; i++ {
			childDepth := cloned.refs[i].getDepth(levelIndex)
			binary.BigEndian.PutUint16(hashBuf[bufPos:bufPos+depthSize], childDepth)
			bufPos += depthSize

			if childDepth > depth {
				depth = childDepth
			}
		}
		if refCnt > 0 {
			depth++
			if depth > maxDepth {
				return ErrCellDepthLimit
			}
		}

		for i := 0; i < refCnt; i++ {
			bufPos += copy(hashBuf[bufPos:], cloned.refs[i].getHash(levelIndex))
		}

		cloned.setDepthAt(hashIndex, depth)
		sum := sha256.Sum256(hashBuf[:bufPos])
		cloned.setHashAt(hashIndex, sum[:])
		hashIndex++
	}
	return nil
}

func merkleProofRef(parent *Cell, refView cellRefView, i int) (*Cell, error) {
	ref, err := refView.boundaryRef(i)
	if err != nil {
		return nil, err
	}
	return ref.Virtualize(childEffectiveLevelFor(parent, uint8(parent.currentEffectiveLevel()))), nil
}
