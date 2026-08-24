package cell

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
)

const augmentedBulkParallelMinItems = 16

// AugmentedEntry is one update of a bulk dictionary write. The zero Mode means
// DictSetModeSet; Add and Replace are assertions here rather than silent
// no-ops, because a bulk write has no per-entry result to report.
type AugmentedEntry struct {
	Key   *Cell
	Value *Cell
	Mode  DictSetMode
}

// SetMany applies a batch of add-or-replace updates in a single descent.
//
// Repeated Set walks a full root-to-leaf path per key and recombines the
// augmentation of every node on the way back up, so a node shared by k keys is
// rebuilt k times and the root's extra is recomputed once per key. SetMany
// visits the union of the paths once instead: each node is rebuilt once and its
// augmentation combined once, no matter how many of the keys pass through it.
//
// The result is the dictionary repeated Set would have produced — same cells,
// same augmentations — so callers may pick either by cost alone. Keys must all
// be the dictionary's key size and must be distinct; the batch is sorted here,
// so the caller's order does not matter.
//
// parallelism is optional and defaults to 1. A larger value bounds the number
// of branch workers, including the caller. Independent left branches run on a
// goroutine only when they hold a meaningful share of a sufficiently large
// batch. Augmentation and trace callbacks may then run concurrently.
func (d *AugmentedDictionary) SetMany(entries []AugmentedEntry, parallelism ...int) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	workers, err := augmentedBulkParallelism(parallelism)
	if err != nil {
		return err
	}
	_, err = d.setManyEntries(entries, nil, workers, false)
	return err
}

// SetManyWithDiff is SetMany with an exact structural mutation receipt. The
// receipt replays candidate augmentation checks through their final paths
// without scanning the old and new dictionaries again.
func (d *AugmentedDictionary) SetManyWithDiff(entries []AugmentedEntry, parallelism ...int) (*AugmentedDictionaryDiff, error) {
	if d == nil {
		return nil, fmt.Errorf("dict is nil")
	}
	workers, err := augmentedBulkParallelism(parallelism)
	if err != nil {
		return nil, err
	}
	return d.setManyEntries(entries, nil, workers, true)
}

// SetManyWithLoadedPaths is SetMany for a caller that already looked up the
// affected keys in the same immutable tree. loadedPaths is the union of those
// resident Patricia spines; it need not correspond one-to-one with entries.
//
// Existing nodes on an update path must be present in loadedPaths. The method
// deliberately does not fall back to the lazy loader there: doing so would
// repeat storage reads the caller has already paid for and would hide an
// incomplete path handoff. Lazy loads remain allowed only for untouched sibling
// roots whose augmentation is required to rebuild their parent. Each distinct
// sibling root is loaded at most once during the batch. parallelism has the same
// optional bounded branch-worker semantics as SetMany.
func (d *AugmentedDictionary) SetManyWithLoadedPaths(
	entries []AugmentedEntry,
	loadedPaths [][]*Cell,
	parallelism ...int,
) error {
	if d == nil {
		return fmt.Errorf("dict is nil")
	}
	workers, err := augmentedBulkParallelism(parallelism)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	workers = min(workers, len(entries))
	resolver, err := newAugBulkPathResolver(
		loadedPaths,
		workers > 1 && len(entries) >= augmentedBulkParallelMinItems,
	)
	if err != nil {
		return err
	}
	_, err = d.setManyEntries(entries, resolver, workers, false)
	return err
}

// SetManyWithLoadedPathsAndDiff combines SetManyWithLoadedPaths with the exact
// mutation receipt returned by SetManyWithDiff.
func (d *AugmentedDictionary) SetManyWithLoadedPathsAndDiff(
	entries []AugmentedEntry,
	loadedPaths [][]*Cell,
	parallelism ...int,
) (*AugmentedDictionaryDiff, error) {
	if d == nil {
		return nil, fmt.Errorf("dict is nil")
	}
	workers, err := augmentedBulkParallelism(parallelism)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return &AugmentedDictionaryDiff{aug: d.aug}, nil
	}

	workers = min(workers, len(entries))
	resolver, err := newAugBulkPathResolver(
		loadedPaths,
		workers > 1 && len(entries) >= augmentedBulkParallelMinItems,
	)
	if err != nil {
		return nil, err
	}
	return d.setManyEntries(entries, resolver, workers, true)
}

func (d *AugmentedDictionary) setManyEntries(
	entries []AugmentedEntry,
	resolver *augBulkPathResolver,
	parallelism int,
	captureDiff bool,
) (*AugmentedDictionaryDiff, error) {
	if len(entries) == 0 {
		if captureDiff {
			return &AugmentedDictionaryDiff{aug: d.aug}, nil
		}
		return nil, nil
	}
	if err := d.ensureWritable(); err != nil {
		return nil, err
	}

	items := make([]augBulkItem, len(entries))
	for i := range entries {
		entry := &entries[i]
		if entry.Key == nil || entry.Key.BitsSize() != d.keySz {
			return nil, fmt.Errorf("invalid key size at entry %d", i)
		}
		if entry.Value == nil {
			return nil, fmt.Errorf("value is nil at entry %d", i)
		}
		if err := entry.Key.BeginParseInto(&items[i].key); err != nil {
			return nil, fmt.Errorf("failed to load key at entry %d: %w", i, err)
		}
		items[i].value = entry.Value.ToBuilder()
		items[i].mode = entry.Mode
		if items[i].mode == 0 {
			items[i].mode = DictSetModeSet
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return compareKeySlices(&items[i].key, &items[j].key) < 0
	})
	for i := 1; i < len(items); i++ {
		if compareKeySlices(&items[i-1].key, &items[i].key) == 0 {
			return nil, fmt.Errorf("duplicate key in bulk update")
		}
	}

	parallelism = min(parallelism, len(items))
	state := augmentedMutationState{
		pathResolver: resolver,
		parallelism:  parallelism,
		captureDiff:  captureDiff,
	}
	root, rootExtra, replay, err := d.setMany(d.root, d.root.Trace(), items, d.keySz, &state, nil)
	if err != nil {
		return nil, err
	}
	rootExtraCell, err := rootExtra.ToCell()
	if err != nil {
		return nil, err
	}
	if err = d.setRootWithExtra(root, rootExtraCell); err != nil {
		return nil, err
	}
	if !captureDiff {
		return nil, nil
	}
	if replay != nil {
		// setRootWithExtra attaches the dictionary trace to a copy of the built
		// root. Replay must start from that final wrapper so child traces follow
		// the candidate path rather than the untraced construction cells.
		replay.cell = d.root
	}

	return &AugmentedDictionaryDiff{
		aug:    d.aug,
		replay: replay,
		warm:   resolver.warmCells(),
	}, nil
}

// warmCells is every resident cell the mutation already owned or loaded, keyed
// by the hash of the lazy placeholder it stands in for. The augmentation
// closure replayed afterwards forks on the same nodes and reads the same
// untouched siblings, so handing it this map turns each of those reads into the
// cache-hit half of a lazy load instead of a second trip to storage.
//
// The map is built once the mutation has finished and is only read from there
// on, which is what makes it safe to share with the concurrent replay workers.
func (r *augBulkPathResolver) warmCells() map[Hash]*Cell {
	if r == nil {
		return nil
	}
	if r.parallel {
		r.indexOnce.Do(r.indexPaths)
	} else if r.pathCells == nil {
		r.indexPaths()
	}
	// The resolver is done with pathCells by now, so the loaded siblings fold
	// into it rather than into a second map the lookup would have to consult.
	warm := r.pathCells
	put := func(hash Hash, c *Cell) {
		if c == nil {
			return
		}
		if warm == nil {
			warm = make(map[Hash]*Cell, len(r.loadedSequential)+1)
		}
		warm[hash] = c
	}
	for hash, c := range r.loadedSequential {
		put(hash, c)
	}
	r.loadedParallel.Range(func(key, value any) bool {
		put(key.(Hash), value.(*augBulkLoadedCell).cell)
		return true
	})
	return warm
}

type augBulkPathResolver struct {
	paths            [][]*Cell
	count            int
	parallel         bool
	indexOnce        sync.Once
	pathCells        map[Hash]*Cell
	loadedSequential map[Hash]*Cell
	loadedParallel   sync.Map
}

type augBulkLoadedCell struct {
	once sync.Once
	cell *Cell
	err  error
}

func newAugBulkPathResolver(paths [][]*Cell, parallel bool) (*augBulkPathResolver, error) {
	count := 0
	for _, path := range paths {
		count += len(path)
	}
	resolver := &augBulkPathResolver{paths: paths, count: count, parallel: parallel}
	for pathIdx, path := range paths {
		for cellIdx, c := range path {
			if c == nil || c.IsLazy() {
				return nil, fmt.Errorf("loaded path %d cell %d is not resident", pathIdx, cellIdx)
			}
		}
	}
	return resolver, nil
}

func (r *augBulkPathResolver) resolve(branch *Cell, allowLoad bool) (*Cell, error) {
	if branch == nil || !branch.IsLazy() {
		return branch, nil
	}

	if r.parallel {
		r.indexOnce.Do(r.indexPaths)
	} else if r.pathCells == nil {
		r.indexPaths()
	}
	hash := branch.rawCell().HashKey()
	if loaded := r.pathCells[hash]; loaded != nil {
		return resolveLoadedLazyRefWithTrace(branch, loaded, nil)
	}
	if !allowLoad {
		return nil, fmt.Errorf("loaded paths do not contain changed branch %x", hash)
	}

	if !r.parallel {
		if loaded := r.loadedSequential[hash]; loaded != nil {
			return resolveLoadedLazyRefWithTrace(branch, loaded, nil)
		}
		resolved, err := loadLazyPrunedRefWithTrace(branch, nil)
		if err != nil {
			return nil, err
		}
		if r.loadedSequential == nil {
			r.loadedSequential = make(map[Hash]*Cell)
		}
		r.loadedSequential[hash] = resolved.rawCell()
		return resolved, nil
	}

	entry, _ := r.loadedParallel.LoadOrStore(hash, &augBulkLoadedCell{})
	loaded := entry.(*augBulkLoadedCell)
	loaded.once.Do(func() {
		resolved, err := loadLazyPrunedRefWithTrace(branch, nil)
		if err != nil {
			loaded.err = err
			return
		}
		loaded.cell = resolved.rawCell()
	})
	if loaded.err != nil {
		return nil, loaded.err
	}
	return resolveLoadedLazyRefWithTrace(branch, loaded.cell, nil)
}

// indexPaths delays hashing and indexing until the walk actually meets a lazy
// branch. A fully resident dictionary never consults the resolver and therefore
// avoids building an index it cannot use.
func (r *augBulkPathResolver) indexPaths() {
	r.pathCells = make(map[Hash]*Cell, r.count)
	for _, path := range r.paths {
		for _, c := range path {
			raw := c.rawCell()
			r.pathCells[raw.HashKey()] = raw
		}
	}
	r.paths = nil
}

func augmentedBulkParallelism(values []int) (int, error) {
	if len(values) == 0 {
		return 1, nil
	}
	if len(values) != 1 || values[0] < 1 {
		return 0, fmt.Errorf("parallelism must be one positive value")
	}
	return values[0], nil
}

type augmentedBranchResult struct {
	cell   *Cell
	extra  Slice
	replay *augDiffReplayNode
	err    error
}

type augmentedBranchFunc func(*augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error)

func (s *augmentedMutationState) shouldFork(leftKeys, totalKeys int) bool {
	if s.parallelism <= 1 || totalKeys < augmentedBulkParallelMinItems {
		return false
	}
	minimumLeftKeys := totalKeys / s.parallelism
	if totalKeys%s.parallelism != 0 {
		minimumLeftKeys++
	}
	return leftKeys >= minimumLeftKeys
}

func (s *augmentedMutationState) branch(parallelism int) augmentedMutationState {
	return augmentedMutationState{
		pathResolver: s.pathResolver,
		parallelism:  parallelism,
		captureDiff:  s.captureDiff,
	}
}

// runAugmentedBranches starts only the left branch. The caller remains the
// right worker and joins the left before rebuilding their parent. Splitting the
// worker budget recursively bounds the whole operation without a semaphore: no
// subtree can wait while holding capacity needed by one of its descendants.
func runAugmentedBranches(
	state *augmentedMutationState,
	leftKeys int,
	totalKeys int,
	left augmentedBranchFunc,
	right augmentedBranchFunc,
) (augmentedBranchResult, augmentedBranchResult) {
	leftParallelism := state.parallelism * leftKeys / totalKeys
	leftParallelism = max(1, min(leftParallelism, state.parallelism-1))
	rightParallelism := state.parallelism - leftParallelism

	leftState := state.branch(leftParallelism)
	rightState := state.branch(rightParallelism)
	var leftResult augmentedBranchResult
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()

		leftResult.cell, leftResult.extra, leftResult.replay, leftResult.err = left(&leftState)
	}()

	var rightResult augmentedBranchResult
	rightResult.cell, rightResult.extra, rightResult.replay, rightResult.err = right(&rightState)
	wait.Wait()
	return leftResult, rightResult
}

type augBulkItem struct {
	// key is the not yet consumed suffix. Every item in a batch carries the
	// same number of remaining bits, which is the recursion's keyOffset.
	key   Slice
	value *Builder
	mode  DictSetMode
}

// checkMode enforces Add/Replace against what the descent found. The two call
// sites are the only places an entry can come to rest: replacing the leaf of an
// existing key, or landing in a subtree built from scratch, where by
// construction no existing key shares the path.
func (i *augBulkItem) checkMode(existed bool) error {
	if existed {
		if i.mode&DictSetModeReplace == 0 {
			return fmt.Errorf("add-mode entry already exists in the dictionary")
		}
		return nil
	}
	if i.mode&DictSetModeAdd == 0 {
		return fmt.Errorf("replace-mode entry is absent from the dictionary")
	}
	return nil
}

// compareKeySlices orders two unconsumed whole-key views by their backing
// bytes, which is only meaningful because both are views of a complete key
// cell. SetMany and DeleteMany are the only callers, and both reject an entry
// whose Key.BitsSize differs from the dictionary key size before handing it to
// Cell.BeginParseInto, which opens the Slice at bit 0 of that cell: hence
// bitStart is 0 on both sides — so the byte range starts on a byte boundary and
// covers the whole key — and bitEnd is the same key size on both sides.
//
// A trailing partial byte is compared masked to its key bits, because the
// padding under them is not canonical: a cell built here pads with zeros while
// one parsed from a BoC keeps its completion tag. Comparing that byte whole
// would order the same key differently depending on where its cell came from,
// and the callers' distinctness check would not recognize it as a duplicate.
func compareKeySlices(a, b *Slice) int {
	aKey := a.cell.data[a.bitStart/8 : (a.bitEnd+7)/8]
	bKey := b.cell.data[b.bitStart/8 : (b.bitEnd+7)/8]

	rem := a.bitEnd % 8
	if rem == 0 {
		return bytes.Compare(aKey, bKey)
	}

	last := len(aKey) - 1
	if cmp := bytes.Compare(aKey[:last], bKey[:last]); cmp != 0 {
		return cmp
	}

	mask := byte(0xFF << (8 - rem))
	switch {
	case aKey[last]&mask < bKey[last]&mask:
		return -1
	case aKey[last]&mask > bKey[last]&mask:
		return 1
	default:
		return 0
	}
}

// setMany writes a non-empty, sorted, distinct batch into branch. Every item
// holds exactly keyOffset key bits. It returns the new subtree and a view of
// its augmentation, which lives inside the returned cell and so survives the
// sibling recursion that follows.
// The trace travels beside branch instead of on it: the descent reaches a
// child through its parent's Slice, which already holds the child trace, and
// attaching that trace to the child cell would allocate a copy of the cell per
// visited node for nothing else.
func (d *AugmentedDictionary) setMany(
	branch *Cell,
	trace *Trace,
	items []augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
	prior *augDiffReplayNode,
) (*Cell, Slice, *augDiffReplayNode, error) {
	if branch == nil {
		return d.buildMany(items, keyOffset, state)
	}

	parseBranch := branch
	var err error
	if state.pathResolver != nil {
		parseBranch, err = state.pathResolver.resolve(branch, false)
		if err != nil {
			return nil, Slice{}, nil, err
		}
	}
	node, err := parseFixedDictNodeWithTrace(parseBranch, keyOffset, trace)
	if err != nil {
		return nil, Slice{}, nil, fmt.Errorf("failed to load branch: %w", err)
	}
	if err = node.rejectSpecial("augmented dict"); err != nil {
		return nil, Slice{}, nil, err
	}
	if err = node.validateForkShape(keyOffset, true); err != nil {
		return nil, Slice{}, nil, err
	}
	sz, label := node.labelLen, node.label

	// The batch follows the label only as far as its least-matching member:
	// past that bit the node has to fork, and the rest of the label moves down
	// into the old child.
	matched := sz
	for i := range items {
		labelView, keyView := label, items[i].key
		shared, err := commonSlicePrefix(&labelView, &keyView, sz)
		if err != nil {
			return nil, Slice{}, nil, fmt.Errorf("failed to match key prefix: %w", err)
		}
		if shared < matched {
			matched = shared
			if matched == 0 {
				break
			}
		}
	}

	if matched == sz {
		for i := range items {
			if err = items[i].key.SkipBits(sz); err != nil {
				return nil, Slice{}, nil, err
			}
		}
		if keyOffset == sz {
			// The whole key is spent, so the batch is the single key living here.
			if err = items[0].checkMode(true); err != nil {
				return nil, Slice{}, nil, err
			}
			labelView := label
			leaf, extra, replay, err := d.storeManyLeaf(&labelView, &items[0], keyOffset, state)
			if err != nil {
				return nil, Slice{}, nil, err
			}
			if state.captureDiff && leaf.HashKey() == branch.HashKey() {
				replay = prior
			}
			return leaf, extra, replay, nil
		}

		left, right, err := splitBulkOnNextBit(items, augBulkItemKey)
		if err != nil {
			return nil, Slice{}, nil, err
		}
		childOffset := keyOffset - sz - 1
		var leftCell, rightCell *Cell
		var leftExtra, rightExtra Slice
		var leftReplay, rightReplay *augDiffReplayNode
		if prior != nil {
			leftReplay, rightReplay = prior.left, prior.right
		}
		// An empty right half is not worth a goroutine: setManyChild returns the
		// old child untouched for it, so forking would hand one of the workers the
		// left side needs to a branch with nothing to do. shouldFork already rules
		// out an empty LEFT half — its minimum-keys bound cannot be met by zero —
		// so guarding the right one closes the pair. Same shape as deleteMany
		// (aug_dict_bulk_delete.go:129).
		if len(right) != 0 && state.shouldFork(len(left), len(items)) {
			leftResult, rightResult := runAugmentedBranches(
				state,
				len(left),
				len(items),
				func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
					return d.setManyChild(&node, 0, left, childOffset, branchState, leftReplay)
				},
				func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
					return d.setManyChild(&node, 1, right, childOffset, branchState, rightReplay)
				},
			)
			if leftResult.err != nil {
				return nil, Slice{}, nil, leftResult.err
			}
			if rightResult.err != nil {
				return nil, Slice{}, nil, rightResult.err
			}
			leftCell, leftExtra = leftResult.cell, leftResult.extra
			rightCell, rightExtra = rightResult.cell, rightResult.extra
			leftReplay, rightReplay = leftResult.replay, rightResult.replay
		} else {
			leftCell, leftExtra, leftReplay, err = d.setManyChild(&node, 0, left, childOffset, state, leftReplay)
			if err != nil {
				return nil, Slice{}, nil, err
			}
			rightCell, rightExtra, rightReplay, err = d.setManyChild(&node, 1, right, childOffset, state, rightReplay)
			if err != nil {
				return nil, Slice{}, nil, err
			}
		}
		labelView := label
		fork, forkExtra, err := d.storeForkWithExtraSlices(
			&labelView, leftCell, &leftExtra, rightCell, &rightExtra, keyOffset, state)
		if err != nil {
			return nil, Slice{}, nil, err
		}
		if state.captureDiff {
			if fork.HashKey() == branch.HashKey() {
				return fork, forkExtra, prior, nil
			}
			return fork, forkExtra, computedAugDiffReplay(fork, keyOffset, leftReplay, rightReplay), nil
		}
		return fork, forkExtra, nil, nil
	}

	// Divergence: the node keeps its first matched bits as the fork label and
	// moves down one level, carrying the rest of its own label. At least one
	// item took the other edge, so that side is always newly built.
	prefixLabel, labelRemainder, err := node.splitLabel(matched)
	if err != nil {
		return nil, Slice{}, nil, fmt.Errorf("failed to split old child label: %w", err)
	}
	labelBit, err := label.BitAt(matched)
	if err != nil {
		return nil, Slice{}, nil, err
	}
	childOffset := keyOffset - matched - 1

	oldChild := BeginCell().SetTrace(d.trace)
	if err = storeDictLabel(oldChild, labelRemainder, childOffset); err != nil {
		return nil, Slice{}, nil, fmt.Errorf("failed to store old child label: %w", err)
	}
	node.loader.ToBuilderInto(&state.extra)
	if err = oldChild.StoreBuilderUncheckedDepth(&state.extra); err != nil {
		return nil, Slice{}, nil, fmt.Errorf("failed to store old child payload: %w", err)
	}
	oldExtra, err := augmentedNodeExtraViewScratch(node, keyOffset, d.aug.SkipExtra, &state.skipScratch)
	if err != nil {
		return nil, Slice{}, nil, fmt.Errorf("failed to extract old child extra: %w", err)
	}
	oldChildCell := oldChild.EndCell()
	var oldReplay *augDiffReplayNode
	if state.captureDiff {
		oldReplay = relabeledAugDiffReplay(oldChildCell, childOffset, node.loader.trace, prior)
	}

	for i := range items {
		if err = items[i].key.SkipBits(matched); err != nil {
			return nil, Slice{}, nil, err
		}
	}
	left, right, err := splitBulkOnNextBit(items, augBulkItemKey)
	if err != nil {
		return nil, Slice{}, nil, err
	}
	sides := [2][]augBulkItem{left, right}

	var children [2]*Cell
	var extras [2]Slice
	var replays [2]*augDiffReplayNode
	buildSide := func(bit int, batch []augBulkItem, branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
		if uint8(bit) == labelBit {
			if len(batch) == 0 {
				return oldChildCell, oldExtra, oldReplay, nil
			}
			return d.setMany(oldChildCell, oldChildCell.Trace(), batch, childOffset, branchState, oldReplay)
		}
		return d.buildMany(batch, childOffset, branchState)
	}
	// An empty right half is not worth a goroutine: setManyChild returns the
	// old child untouched for it, so forking would hand one of the workers the
	// left side needs to a branch with nothing to do. shouldFork already rules
	// out an empty LEFT half — its minimum-keys bound cannot be met by zero —
	// so guarding the right one closes the pair. Same shape as deleteMany
	// (aug_dict_bulk_delete.go:129).
	if len(right) != 0 && state.shouldFork(len(left), len(items)) {
		leftResult, rightResult := runAugmentedBranches(
			state,
			len(left),
			len(items),
			func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
				return buildSide(0, left, branchState)
			},
			func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
				return buildSide(1, right, branchState)
			},
		)
		if leftResult.err != nil {
			return nil, Slice{}, nil, leftResult.err
		}
		if rightResult.err != nil {
			return nil, Slice{}, nil, rightResult.err
		}
		children[0], extras[0] = leftResult.cell, leftResult.extra
		children[1], extras[1] = rightResult.cell, rightResult.extra
		replays[0], replays[1] = leftResult.replay, rightResult.replay
	} else {
		for bit, batch := range sides {
			children[bit], extras[bit], replays[bit], err = buildSide(bit, batch, state)
			if err != nil {
				return nil, Slice{}, nil, err
			}
		}
	}

	fork, forkExtra, err := d.storeForkWithExtraSlices(
		prefixLabel, children[0], &extras[0], children[1], &extras[1], keyOffset, state)
	if err != nil {
		return nil, Slice{}, nil, err
	}
	if state.captureDiff {
		return fork, forkExtra, computedAugDiffReplay(fork, keyOffset, replays[0], replays[1]), nil
	}
	return fork, forkExtra, nil, nil
}

// setManyChild descends into one side of an existing fork, leaving it untouched
// when no key of the batch goes that way.
func (d *AugmentedDictionary) setManyChild(
	node *fixedDictNode,
	refIdx int,
	items []augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
	prior *augDiffReplayNode,
) (*Cell, Slice, *augDiffReplayNode, error) {
	ref, trace, err := node.refAndTrace(refIdx)
	if err != nil {
		return nil, Slice{}, nil, fmt.Errorf("failed to peek %d ref: %w", refIdx, err)
	}
	if len(items) == 0 {
		// This child is kept as it stands and becomes a ref of the rebuilt
		// fork, so here the trace has to ride on the cell: the merkle update
		// recognizes an untouched subtree by the usage node its trace resolves
		// to, and a child stored without one is written out in full instead of
		// being pruned against the source.
		kept := ref.WithTrace(trace)
		parseRef := ref
		if state.pathResolver != nil {
			parseRef, err = state.pathResolver.resolve(ref, true)
			if err != nil {
				return nil, Slice{}, nil, fmt.Errorf("failed to resolve %d ref extra: %w", refIdx, err)
			}
		}
		extra, err := extractAugmentedNodeExtraViewWithTraceScratch(parseRef, trace, keyOffset, d.aug.SkipExtra, &state.skipScratch)
		if err != nil {
			return nil, Slice{}, nil, fmt.Errorf("failed to extract %d ref extra: %w", refIdx, err)
		}
		return kept, extra, prior, nil
	}
	// The descent rebuilds this child, so nothing that survives the call refers
	// to the cell and the trace can travel beside it.
	return d.setMany(ref, trace, items, keyOffset, state, prior)
}

// buildMany creates a fresh subtree holding exactly the batch.
func (d *AugmentedDictionary) buildMany(
	items []augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
) (*Cell, Slice, *augDiffReplayNode, error) {
	if len(items) == 1 {
		if err := items[0].checkMode(false); err != nil {
			return nil, Slice{}, nil, err
		}
		return d.storeManyLeaf(&items[0].key, &items[0], keyOffset, state)
	}

	// Distinct keys share less than the whole remaining space, so the common
	// prefix always leaves a bit to fork on and both sides come out non-empty.
	shared := keyOffset
	for i := 1; i < len(items); i++ {
		first, other := items[0].key, items[i].key
		common, err := commonSlicePrefix(&first, &other, keyOffset)
		if err != nil {
			return nil, Slice{}, nil, fmt.Errorf("failed to match batch prefix: %w", err)
		}
		if common < shared {
			shared = common
			if shared == 0 {
				break
			}
		}
	}

	label := items[0].key
	label.bitEnd = label.bitStart + uint16(shared)
	for i := range items {
		if err := items[i].key.SkipBits(shared); err != nil {
			return nil, Slice{}, nil, err
		}
	}
	left, right, err := splitBulkOnNextBit(items, augBulkItemKey)
	if err != nil {
		return nil, Slice{}, nil, err
	}

	childOffset := keyOffset - shared - 1
	var leftCell, rightCell *Cell
	var leftExtra, rightExtra Slice
	var leftReplay, rightReplay *augDiffReplayNode
	if state.shouldFork(len(left), len(items)) {
		leftResult, rightResult := runAugmentedBranches(
			state,
			len(left),
			len(items),
			func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
				return d.buildMany(left, childOffset, branchState)
			},
			func(branchState *augmentedMutationState) (*Cell, Slice, *augDiffReplayNode, error) {
				return d.buildMany(right, childOffset, branchState)
			},
		)
		if leftResult.err != nil {
			return nil, Slice{}, nil, leftResult.err
		}
		if rightResult.err != nil {
			return nil, Slice{}, nil, rightResult.err
		}
		leftCell, leftExtra = leftResult.cell, leftResult.extra
		rightCell, rightExtra = rightResult.cell, rightResult.extra
		leftReplay, rightReplay = leftResult.replay, rightResult.replay
	} else {
		leftCell, leftExtra, leftReplay, err = d.buildMany(left, childOffset, state)
		if err != nil {
			return nil, Slice{}, nil, err
		}
		rightCell, rightExtra, rightReplay, err = d.buildMany(right, childOffset, state)
		if err != nil {
			return nil, Slice{}, nil, err
		}
	}
	fork, forkExtra, err := d.storeForkWithExtraSlices(
		&label, leftCell, &leftExtra, rightCell, &rightExtra, keyOffset, state)
	if err != nil {
		return nil, Slice{}, nil, err
	}
	if state.captureDiff {
		return fork, forkExtra, computedAugDiffReplay(fork, keyOffset, leftReplay, rightReplay), nil
	}
	return fork, forkExtra, nil, nil
}

func (d *AugmentedDictionary) storeManyLeaf(
	label *Slice,
	item *augBulkItem,
	keyOffset uint,
	state *augmentedMutationState,
) (*Cell, Slice, *augDiffReplayNode, error) {
	leaf, extra, err := d.storeLeafWithExtra(label, item.value, keyOffset, state)
	if err != nil || !state.captureDiff {
		return leaf, extra, nil, err
	}
	return leaf, extra, computedAugDiffReplay(leaf, keyOffset, nil, nil), nil
}

// splitBulkOnNextBit consumes the edge bit of every item and partitions the
// batch. Sorted input keeps the zero side a prefix of the slice, so the split
// is a single index and neither side is copied.
func splitBulkOnNextBit[T any](items []T, key func(*T) *Slice) ([]T, []T, error) {
	split := len(items)
	for i := range items {
		bit, err := key(&items[i]).LoadUInt(1)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to consume edge bit: %w", err)
		}
		if bit != 0 && i < split {
			split = i
		}
	}
	return items[:split], items[split:], nil
}

func augBulkItemKey(item *augBulkItem) *Slice {
	return &item.key
}
