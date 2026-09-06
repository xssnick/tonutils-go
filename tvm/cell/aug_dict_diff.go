package cell

import (
	"fmt"
	"sync"
)

// AugDictDiffFunc receives changed leaves in key order. A nil oldValueExtra or
// newValueExtra means that the key is absent from that side of the diff. The
// slices contain the raw extra followed by the value.
type AugDictDiffFunc func(key *Cell, oldValueExtra, newValueExtra *Slice) error

// AugDictDiffView is one changed augmented-dictionary leaf. The value slices
// contain the raw extra followed by the value. All three slices are borrowed
// and valid only until the callback returns; call ToCell or copy their contents
// to retain them.
type AugDictDiffView struct {
	Key           Slice
	OldValueExtra Slice
	NewValueExtra Slice
	HasOld        bool
	HasNew        bool
}

// AugDictDiffViewFunc receives borrowed changed-leaf views.
type AugDictDiffViewFunc func(AugDictDiffView) error

// AugDictDiffRawView is one changed augmented-dictionary leaf with the key
// exposed as borrowed packed bits. Key, OldValueExtra and NewValueExtra are
// read-only, valid only until the callback returns, and must not be retained;
// the first KeyBits bits of Key are significant.
//
// Unlike AugDictDiffView, this form does not materialize or hash a temporary
// Cell for the key. It is intended for validation paths that either do not use
// the key or only decode it on an error path.
type AugDictDiffRawView struct {
	Key           []byte
	KeyBits       uint
	OldValueExtra Slice
	NewValueExtra Slice
	HasOld        bool
	HasNew        bool
}

// AugDictDiffRawViewFunc receives a borrowed raw-key diff view.
type AugDictDiffRawViewFunc func(AugDictDiffRawView) error

// ScanDiff compares two HashmapAug tries structurally and visits their changed
// leaves in key order. Equal subtrees are skipped by hash. When
// checkOtherAugmentation is set, every changed node in other has its stored
// augmentation checked in the same traversal.
func (d *AugmentedDictionary) ScanDiff(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffFunc) error {
	if fn == nil {
		return fmt.Errorf("augmented dictionary diff callback is required")
	}
	return d.ScanDiffBorrowed(other, checkOtherAugmentation, func(view AugDictDiffView) error {
		key, err := view.Key.ToCell()
		if err != nil {
			return err
		}

		var oldValueExtra, newValueExtra *Slice
		if view.HasOld {
			oldValueExtra = &view.OldValueExtra
		}
		if view.HasNew {
			newValueExtra = &view.NewValueExtra
		}
		return fn(key, oldValueExtra, newValueExtra)
	})
}

// ScanDiffBorrowed is ScanDiff without materializing a key Cell for every
// changed leaf. Every Slice in the view is borrowed and must not be retained
// after the callback returns.
func (d *AugmentedDictionary) ScanDiffBorrowed(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffViewFunc) error {
	if fn == nil {
		return fmt.Errorf("augmented dictionary diff callback is required")
	}
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}

	walk := augDictDiffWalk{
		keySz:    d.keySz,
		newAug:   other.aug,
		checkNew: checkOtherAugmentation,
		viewFn:   fn,
	}
	oldTrace := combinedCellTrace(d.root, d.trace)
	newTrace := combinedCellTrace(other.root, other.trace)
	if err := walk.node(d.root, oldTrace, other.root, newTrace, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	return nil
}

// ScanDiffRaw is ScanDiffBorrowed without materializing or hashing a key Cell.
// The callback receives the key as borrowed packed bits.
func (d *AugmentedDictionary) ScanDiffRaw(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffRawViewFunc) error {
	if fn == nil {
		return fmt.Errorf("augmented dictionary diff callback is required")
	}
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}

	walk := augDictDiffWalk{
		keySz:    d.keySz,
		newAug:   other.aug,
		checkNew: checkOtherAugmentation,
		rawFn:    fn,
	}
	oldTrace := combinedCellTrace(d.root, d.trace)
	newTrace := combinedCellTrace(other.root, other.trace)
	if err := walk.node(d.root, oldTrace, other.root, newTrace, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	return nil
}

// ScanDiffParallel is ScanDiff with shared forks at and below key depth
// scanDiffFrontierBits scanned on up to workers goroutines. The scan's
// observable effect in a collation is the reads it records — the validator's
// augmentation checks replayed so their cells reach the proof — and a set of
// reads is the same in any order. fn is called from the workers and must be
// safe for that; the collator passes one that does nothing. Errors keep the
// sequential priority: every fork reports its left error before its right one,
// regardless of completion order.
//
// The walk is one of the five tasks of the collation's validation closure and,
// on a queue thousands of entries deep, the slowest: two tries of that size
// scanned in lockstep with an augmentation check at every changed fork, on
// one goroutine. Like the account closure it splits by subtree, and for the
// same reason pays off: the work is parsing and loading siblings the
// collation never touched.
func (d *AugmentedDictionary) ScanDiffParallel(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffFunc, workers int) error {
	return d.scanDiffParallelAt(other, checkOtherAugmentation, fn, workers, scanDiffFrontierBits)
}

// ScanDiffParallelBorrowed is the borrowed-view form of ScanDiffParallel. fn
// can run concurrently and must not retain a view after that invocation
// returns.
func (d *AugmentedDictionary) ScanDiffParallelBorrowed(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffViewFunc, workers int) error {
	return d.scanDiffParallelBorrowedAt(other, checkOtherAugmentation, fn, workers, scanDiffFrontierBits)
}

// ScanDiffParallelRaw is the raw-key form of ScanDiffParallelBorrowed.
func (d *AugmentedDictionary) ScanDiffParallelRaw(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffRawViewFunc, workers int) error {
	return d.scanDiffParallelRawAt(other, checkOtherAugmentation, fn, workers, scanDiffFrontierBits)
}

// scanDiffParallelAt is ScanDiffParallel with the frontier depth chosen by the
// caller; tests use it on dictionaries with short keys.
func (d *AugmentedDictionary) scanDiffParallelAt(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffFunc, workers int, frontierBits uint) error {
	if fn == nil {
		return fmt.Errorf("augmented dictionary diff callback is required")
	}
	return d.scanDiffParallelBorrowedAt(other, checkOtherAugmentation, func(view AugDictDiffView) error {
		key, err := view.Key.ToCell()
		if err != nil {
			return err
		}

		var oldValueExtra, newValueExtra *Slice
		if view.HasOld {
			oldValueExtra = &view.OldValueExtra
		}
		if view.HasNew {
			newValueExtra = &view.NewValueExtra
		}
		return fn(key, oldValueExtra, newValueExtra)
	}, workers, frontierBits)
}

func (d *AugmentedDictionary) scanDiffParallelBorrowedAt(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffViewFunc, workers int, frontierBits uint) error {
	if fn == nil {
		return fmt.Errorf("augmented dictionary diff callback is required")
	}
	if workers < 2 {
		return d.ScanDiffBorrowed(other, checkOtherAugmentation, fn)
	}
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}
	walk := augDictDiffWalk{
		keySz:        d.keySz,
		newAug:       other.aug,
		checkNew:     checkOtherAugmentation,
		viewFn:       fn,
		frontierBits: frontierBits,
		parallelism:  workers,
	}
	oldTrace := combinedCellTrace(d.root, d.trace)
	newTrace := combinedCellTrace(other.root, other.trace)
	if err := walk.node(d.root, oldTrace, other.root, newTrace, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	return nil
}

func (d *AugmentedDictionary) scanDiffParallelRawAt(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffRawViewFunc, workers int, frontierBits uint) error {
	if fn == nil {
		return fmt.Errorf("augmented dictionary diff callback is required")
	}
	if workers < 2 {
		return d.ScanDiffRaw(other, checkOtherAugmentation, fn)
	}
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}
	walk := augDictDiffWalk{
		keySz:        d.keySz,
		newAug:       other.aug,
		checkNew:     checkOtherAugmentation,
		rawFn:        fn,
		frontierBits: frontierBits,
		parallelism:  workers,
	}
	oldTrace := combinedCellTrace(d.root, d.trace)
	newTrace := combinedCellTrace(other.root, other.trace)
	if err := walk.node(d.root, oldTrace, other.root, newTrace, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	return nil
}

// scanDiffFrontierBits is the minimum key depth at which ScanDiffParallel may
// split a shared fork. After that point the worker budget keeps splitting
// recursively, instead of stopping at the first fork and leaving only two
// coarse tasks for a structured key prefix.
const scanDiffFrontierBits = 38

type augDictDiffWalk struct {
	keySz   uint
	key     Builder
	keyCell Cell
	newAug  Augmentation

	checkNew bool
	viewFn   AugDictDiffViewFunc
	rawFn    AugDictDiffRawViewFunc

	checker augmentedNodeChecker

	// frontierBits is the minimum shared-fork depth for parallel descent.
	// parallelism is an exact subtree budget: every split partitions it between
	// children, bounding live workers without a semaphore or task queue.
	frontierBits uint
	parallelism  int
}

func (w *augDictDiffWalk) node(old *Cell, oldTrace *Trace, new *Cell, newTrace *Trace, remaining, skipOld, skipNew uint) error {
	if old == nil {
		if new == nil {
			return nil
		}
		if skipNew != 0 {
			return fmt.Errorf("invalid new dictionary alignment")
		}
		return w.oneSide(new, newTrace, remaining, false)
	}
	if new == nil {
		if skipOld != 0 {
			return fmt.Errorf("invalid old dictionary alignment")
		}
		return w.oneSide(old, oldTrace, remaining, true)
	}
	if skipOld == skipNew && old.HashKey() == new.HashKey() {
		return nil
	}

	oldNode, err := parseAugDictDiffNode(old, oldTrace, remaining+skipOld)
	if err != nil {
		return fmt.Errorf("invalid old dictionary node: %w", err)
	}
	newNode, err := parseAugDictDiffNode(new, newTrace, remaining+skipNew)
	if err != nil {
		return fmt.Errorf("invalid new dictionary node: %w", err)
	}
	if oldNode.labelLen < skipOld || newNode.labelLen < skipNew {
		return fmt.Errorf("invalid dictionary diff alignment")
	}

	// Realigning labels may revisit a root. Retain its validated resident
	// cell, with the original path trace still supplied to each logical parse.
	old, new = oldNode.cell, newNode.cell

	oldLabel := oldNode.labelSlice()
	newLabel := newNode.labelSlice()
	oldEffective := oldLabel
	newEffective := newLabel
	if err = oldEffective.SkipBits(skipOld); err != nil {
		return err
	}
	if err = newEffective.SkipBits(skipNew); err != nil {
		return err
	}

	depth := w.keySz - remaining
	if !w.keyMatchesSkippedLabel(&oldLabel, depth, skipOld) || !w.keyMatchesSkippedLabel(&newLabel, depth, skipNew) {
		return fmt.Errorf("invalid dictionary diff prefix")
	}
	w.storeLabel(&oldLabel, depth-skipOld)

	oldLen := oldNode.labelLen - skipOld
	newLen := newNode.labelLen - skipNew
	common, err := commonSlicePrefix(&oldEffective, &newEffective, min(oldLen, newLen))
	if err != nil {
		return err
	}

	if common < oldLen && common < newLen {
		w.storeLabel(&newLabel, depth-skipNew)
		if oldEffective.bitAt(common) == 0 {
			if err = w.node(old, oldTrace, nil, nil, remaining+skipOld, 0, 0); err != nil {
				return err
			}
			return w.node(nil, nil, new, newTrace, remaining+skipNew, 0, 0)
		}
		if err = w.node(nil, nil, new, newTrace, remaining+skipNew, 0, 0); err != nil {
			return err
		}
		return w.node(old, oldTrace, nil, nil, remaining+skipOld, 0, 0)
	}

	if common == oldLen && common == newLen {
		if common == remaining {
			oldValue, newValue := oldNode.loader, newNode.loader
			equal := equalSliceContents(&oldValue, &newValue)
			if w.checkNew {
				if equal {
					// A nearby insertion or deletion can rebuild the same logical
					// leaf under a different Patricia path. C++ then validates the
					// new leaf through references shared with the predecessor. Carry
					// both path identities so its LeafExtra reads retain that exact
					// predecessor closure in a path-based usage tree.
					newNode.loader.trace = CombineTraces(newNode.loader.trace, oldNode.loader.trace)
				}
				if err = w.checker.leaf(newNode, w.newAug); err != nil {
					return fmt.Errorf("invalid new dictionary leaf augmentation: %w", err)
				}
			}
			if equal {
				return nil
			}
			return w.emit(oldValue, true, newValue, true)
		}

		var loadedChildren [2]*Cell
		if w.checkNew {
			if err = w.checker.fork(newNode, remaining-common, w.newAug, &loadedChildren); err != nil {
				return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
			}
		}

		nextRemaining := remaining - common - 1
		var oldChildren, newChildren [2]*Cell
		var oldChildTraces, newChildTraces [2]*Trace
		for child := range 2 {
			oldChildren[child], oldChildTraces[child], err = oldNode.refAndTrace(child)
			if err != nil {
				return fmt.Errorf("failed to load old dictionary child: %w", err)
			}
			newChildren[child], newChildTraces[child], err = newNode.refAndTrace(child)
			if err != nil {
				return fmt.Errorf("failed to load new dictionary child: %w", err)
			}
			if loadedChildren[child] != nil {
				newChildren[child] = loadedChildren[child]
			}
		}
		if w.parallelism > 1 && depth+common+1 >= w.frontierBits {
			return w.parallelSharedFork(oldChildren, oldChildTraces, newChildren, newChildTraces, nextRemaining, depth+common)
		}
		for child := range 2 {
			w.setKeyBit(depth+common, byte(child))
			if err = w.node(oldChildren[child], oldChildTraces[child], newChildren[child], newChildTraces[child], nextRemaining, 0, 0); err != nil {
				return err
			}
		}
		return nil
	}

	if common == oldLen {
		w.storeLabel(&newLabel, depth-skipNew)
		oldLeft, oldLeftTrace, err := oldNode.refAndTrace(0)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary left child: %w", err)
		}
		oldRight, oldRightTrace, err := oldNode.refAndTrace(1)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary right child: %w", err)
		}

		nextRemaining := remaining - common - 1
		branch := int(newEffective.bitAt(common))
		w.setKeyBit(depth+common, byte(branch))
		if branch == 0 {
			if err = w.node(oldLeft, oldLeftTrace, new, newTrace, nextRemaining, 0, skipNew+common+1); err != nil {
				return err
			}
			w.setKeyBit(depth+common, 1)
			return w.node(oldRight, oldRightTrace, nil, nil, nextRemaining, 0, 0)
		}
		w.setKeyBit(depth+common, 0)
		if err = w.node(oldLeft, oldLeftTrace, nil, nil, nextRemaining, 0, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(oldRight, oldRightTrace, new, newTrace, nextRemaining, 0, skipNew+common+1)
	}

	var loadedChildren [2]*Cell
	if w.checkNew {
		if err = w.checker.fork(newNode, remaining-common, w.newAug, &loadedChildren); err != nil {
			return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
		}
	}
	newLeft, newLeftTrace, err := newNode.refAndTrace(0)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary left child: %w", err)
	}
	newRight, newRightTrace, err := newNode.refAndTrace(1)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary right child: %w", err)
	}
	if loadedChildren[0] != nil {
		newLeft, newRight = loadedChildren[0], loadedChildren[1]
	}

	nextRemaining := remaining - common - 1
	branch := int(oldEffective.bitAt(common))
	if branch == 0 {
		w.setKeyBit(depth+common, 0)
		if err = w.node(old, oldTrace, newLeft, newLeftTrace, nextRemaining, skipOld+common+1, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(nil, nil, newRight, newRightTrace, nextRemaining, 0, 0)
	}
	w.setKeyBit(depth+common, 0)
	if err = w.node(nil, nil, newLeft, newLeftTrace, nextRemaining, 0, 0); err != nil {
		return err
	}
	w.setKeyBit(depth+common, 1)
	return w.node(old, oldTrace, newRight, newRightTrace, nextRemaining, skipOld+common+1, 0)
}

// parallelSharedFork recursively partitions the available worker budget. A
// structured queue key may not fork until hundreds of bits after the minimum
// frontier; splitting again inside each child keeps all requested workers busy
// instead of freezing the parallelism at the first two children.
func (w *augDictDiffWalk) parallelSharedFork(
	oldChildren [2]*Cell,
	oldTraces [2]*Trace,
	newChildren [2]*Cell,
	newTraces [2]*Trace,
	remaining uint,
	keyBit uint,
) error {
	leftWorkers := (w.parallelism + 1) / 2
	rightWorkers := w.parallelism - leftWorkers

	left := w.childWalk(keyBit, 0, leftWorkers)
	right := w.childWalk(keyBit, 1, rightWorkers)

	var leftErr error
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		leftErr = left.node(oldChildren[0], oldTraces[0], newChildren[0], newTraces[0], remaining, 0, 0)
	}()
	rightErr := right.node(oldChildren[1], oldTraces[1], newChildren[1], newTraces[1], remaining, 0, 0)
	wait.Wait()

	// Trie order defines error priority regardless of which worker finished
	// first. This matches the sequential walk and the previous task collector.
	if leftErr != nil {
		return leftErr
	}
	return rightErr
}

func (w *augDictDiffWalk) childWalk(keyBit uint, value byte, parallelism int) augDictDiffWalk {
	child := *w
	child.key = w.key
	child.keyCell = Cell{}
	child.checker = augmentedNodeChecker{warm: w.checker.warm}
	child.parallelism = parallelism
	child.setKeyBit(keyBit, value)
	return child
}

func (w *augDictDiffWalk) oneSide(branch *Cell, trace *Trace, remaining uint, oldOnly bool) error {
	node, err := parseAugDictDiffNode(branch, trace, remaining)
	if err != nil {
		return err
	}

	depth := w.keySz - remaining
	label := node.labelSlice()
	w.storeLabel(&label, depth)
	if node.isLeaf(remaining) {
		if !oldOnly && w.checkNew {
			if err = w.checker.leaf(node, w.newAug); err != nil {
				return fmt.Errorf("invalid new dictionary leaf augmentation: %w", err)
			}
		}
		if oldOnly {
			value := node.loader
			return w.emit(value, true, Slice{}, false)
		}
		value := node.loader
		return w.emit(Slice{}, false, value, true)
	}

	var loadedChildren [2]*Cell
	if !oldOnly && w.checkNew {
		if err = w.checker.fork(node, remaining-node.labelLen, w.newAug, &loadedChildren); err != nil {
			return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
		}
	}

	nextRemaining := remaining - node.labelLen - 1
	for child := 0; child < 2; child++ {
		w.setKeyBit(depth+node.labelLen, byte(child))
		ref, childTrace, err := node.refAndTrace(child)
		if err != nil {
			return fmt.Errorf("failed to load dictionary child: %w", err)
		}
		if loadedChildren[child] != nil {
			ref = loadedChildren[child]
		}
		err = w.oneSide(ref, childTrace, nextRemaining, oldOnly)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseAugDictDiffNode(branch *Cell, trace *Trace, remaining uint) (fixedDictNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, trace)
	if err != nil {
		return fixedDictNode{}, err
	}
	if err = node.rejectSpecial("augmented dictionary"); err != nil {
		return fixedDictNode{}, err
	}
	if err = node.validateForkShape(remaining, true); err != nil {
		return fixedDictNode{}, err
	}
	return node, nil
}

type augmentedNodeChecker struct {
	computed Builder
	value    Slice
	left     Slice
	right    Slice
	// warm is the producing mutation's resident cells, empty for a checker
	// that runs outside a replay. See child.
	warm map[Hash]*Cell
	// scratch backs the fork-extra probes; see augmentedNodeExtraViewScratch.
	scratch Slice
	buf     [maxCellDataBytes]byte
}

func (c *augmentedNodeChecker) leaf(node fixedDictNode, aug Augmentation) error {
	stored := node.loader
	c.value = node.loader
	if err := aug.SkipExtra(&c.value); err != nil {
		return err
	}
	stored.bitEnd, stored.refEnd = c.value.bitStart, c.value.refStart

	c.computed = Builder{}
	if err := aug.LeafExtra(&c.value, &c.computed); err != nil {
		return err
	}
	if !c.computed.equalsSlice(&stored, &c.buf) {
		return fmt.Errorf("augmented dictionary leaf extra mismatch")
	}
	return nil
}

func (c *augmentedNodeChecker) fork(node fixedDictNode, remaining uint, aug Augmentation, loadedChildren *[2]*Cell) error {
	left, leftTrace, err := c.child(node, 0)
	if err != nil {
		return err
	}
	right, rightTrace, err := c.child(node, 1)
	if err != nil {
		return err
	}
	childRemaining := remaining - 1
	c.left, err = extractAugmentedNodeExtraViewWithTraceScratch(left, leftTrace, childRemaining, aug.SkipExtra, &c.scratch)
	if err != nil {
		return err
	}
	c.right, err = extractAugmentedNodeExtraViewWithTraceScratch(right, rightTrace, childRemaining, aug.SkipExtra, &c.scratch)
	if err != nil {
		return err
	}
	if loadedChildren != nil && (left.IsLazy() || right.IsLazy()) {
		// Only lazy children need a handoff to avoid another storage lookup.
		// Capture before the augmentation callback consumes its borrowed views.
		*loadedChildren = [2]*Cell{c.left.cell, c.right.cell}
	}

	stored := node.loader
	if err = stored.SkipBitsAndRefs(0, 2); err != nil {
		return err
	}
	c.computed = Builder{}
	if err = aug.CombineExtra(&c.left, &c.right, &c.computed); err != nil {
		return err
	}
	if !c.computed.equalsSlice(&stored, &c.buf) {
		return fmt.Errorf("augmented dictionary fork extra mismatch")
	}
	return nil
}

// child is node.ref with the producing mutation's warm cells consulted first.
// A fork reads both children to recombine their augmentation, and the untouched
// one is exactly the sibling that mutation already resolved; asking storage for
// it again is the closure's single largest cost.
//
// Substituting the resident cell is the cache-hit half of the lazy load the
// parse would perform anyway: resolveLoadedLazyRefWithTrace validates and
// virtualizes the placeholder exactly as the loader result is validated. The
// trace is returned beside the immutable cell, and the next parse notifies it
// with the same cell. The recorded read set, and the proof selected from it,
// are therefore unchanged — only the storage read and traced wrapper disappear.
func (c *augmentedNodeChecker) child(node fixedDictNode, i int) (*Cell, *Trace, error) {
	ref, trace, err := node.refAndTrace(i)
	if err != nil || c.warm == nil || ref == nil || !ref.IsLazy() {
		return ref, trace, err
	}
	loaded := c.warm[ref.rawCell().HashKey()]
	if loaded == nil {
		return ref, trace, nil
	}
	resolved, err := resolveLoadedLazyRefWithTrace(ref, loaded, nil)
	if err == nil && trace == nil {
		// With no parent path, the legacy ref-based walk inherited a trace
		// already attached to the resident warm cell. Preserve it as the
		// sidecar; a non-nil parent trace still replaces it, as before.
		trace = resolved.Trace()
	}
	return resolved, trace, err
}

func (w *augDictDiffWalk) emit(oldValueExtra Slice, hasOld bool, newValueExtra Slice, hasNew bool) error {
	w.key.bitsSz = w.keySz
	if w.rawFn != nil {
		return w.rawFn(AugDictDiffRawView{
			Key:           w.key.data[:w.key.usedBytes()],
			KeyBits:       w.keySz,
			OldValueExtra: oldValueExtra,
			NewValueExtra: newValueExtra,
			HasOld:        hasOld,
			HasNew:        hasNew,
		})
	}
	w.keyCell = Cell{
		data:   w.key.data[:w.key.usedBytes()],
		bitsSz: uint16(w.keySz),
	}
	if err := w.keyCell.calculateHashesOrdinary(); err != nil {
		return err
	}
	view := AugDictDiffView{
		Key: Slice{
			cell:              &w.keyCell,
			bitEnd:            uint16(w.keySz),
			forceCopyOnToCell: true,
		},
		OldValueExtra: oldValueExtra,
		NewValueExtra: newValueExtra,
		HasOld:        hasOld,
		HasNew:        hasNew,
	}
	return w.viewFn(view)
}

func (w *augDictDiffWalk) keyMatchesSkippedLabel(label *Slice, depth, skip uint) bool {
	if skip == 0 {
		return true
	}
	start := depth - skip
	for bit := uint(0); bit < skip; bit++ {
		if w.keyBit(start+bit) != label.bitAt(bit) {
			return false
		}
	}
	return true
}

func (w *augDictDiffWalk) storeLabel(label *Slice, start uint) {
	for bit := uint(0); bit < label.BitsLeft(); bit++ {
		w.setKeyBit(start+bit, label.bitAt(bit))
	}
}

func (w *augDictDiffWalk) keyBit(bit uint) byte {
	return (w.key.data[bit/8] >> (7 - bit%8)) & 1
}

func (w *augDictDiffWalk) setKeyBit(bit uint, value byte) {
	mask := byte(1 << (7 - bit%8))
	if value != 0 {
		w.key.data[bit/8] |= mask
		return
	}
	w.key.data[bit/8] &^= mask
}

func equalSliceContents(a, b *Slice) bool {
	if a.BitsLeft() != b.BitsLeft() || a.RefsNum() != b.RefsNum() || !a.BitsEqual(b) {
		return false
	}
	for i := 0; i < a.RefsNum(); i++ {
		if a.boundaryRefCellAt(i).HashKey() != b.boundaryRefCellAt(i).HashKey() {
			return false
		}
	}
	return true
}
