package cell

import (
	"fmt"
	"sync"
)

// AugDictDiffFunc receives changed leaves in key order. A nil oldValueExtra or
// newValueExtra means that the key is absent from that side of the diff. The
// slices contain the raw extra followed by the value.
type AugDictDiffFunc func(key *Cell, oldValueExtra, newValueExtra *Slice) error

// ScanDiff compares two HashmapAug tries structurally and visits their changed
// leaves in key order. Equal subtrees are skipped by hash. When
// checkOtherAugmentation is set, every changed node in other has its stored
// augmentation checked in the same traversal.
func (d *AugmentedDictionary) ScanDiff(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffFunc) error {
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}

	walk := augDictDiffWalk{
		keySz:    d.keySz,
		newAug:   other.aug,
		checkNew: checkOtherAugmentation,
		fn:       fn,
	}
	oldRoot := d.root.withTraceCombined(d.trace)
	newRoot := other.root.withTraceCombined(other.trace)
	if err := walk.node(oldRoot, newRoot, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	return nil
}

// ScanDiffParallel is ScanDiff with the subtrees below a shared fork at key
// depth scanDiffFrontierBits scanned on workers goroutines. The scan's
// observable effect in a collation is the reads it records — the validator's
// augmentation checks replayed so their cells reach the proof — and a set of
// reads is the same in any order. fn is called from the workers and must be
// safe for that; the collator passes one that does nothing. Errors keep the
// sequential priority: the top of the walk returns the first it meets, and a
// task's error is reported in trie order.
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

// scanDiffParallelAt is ScanDiffParallel with the frontier depth chosen by the
// caller; tests use it on dictionaries with short keys.
func (d *AugmentedDictionary) scanDiffParallelAt(other *AugmentedDictionary, checkOtherAugmentation bool, fn AugDictDiffFunc, workers int, frontierBits uint) error {
	if workers < 2 {
		return d.ScanDiff(other, checkOtherAugmentation, fn)
	}
	if d.keySz != other.keySz {
		return fmt.Errorf("cannot compare augmented dictionaries with different key sizes")
	}
	var tasks []augDictDiffTask
	top := augDictDiffWalk{
		keySz:        d.keySz,
		newAug:       other.aug,
		checkNew:     checkOtherAugmentation,
		fn:           fn,
		frontierBits: frontierBits,
		tasks:        &tasks,
	}
	oldRoot := d.root.withTraceCombined(d.trace)
	newRoot := other.root.withTraceCombined(other.trace)
	if err := top.node(oldRoot, newRoot, d.keySz, 0, 0); err != nil {
		return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
	}
	if len(tasks) < 2 {
		for i := range tasks {
			walk := augDictDiffWalk{keySz: d.keySz, key: tasks[i].key, newAug: other.aug, checkNew: checkOtherAugmentation, fn: fn}
			if err := walk.node(tasks[i].old, tasks[i].new, tasks[i].remaining, 0, 0); err != nil {
				return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
			}
		}
		return nil
	}

	errs := make([]error, len(tasks))
	next := make(chan int, len(tasks))
	for i := range tasks {
		next <- i
	}
	close(next)
	var wait sync.WaitGroup
	for range min(workers, len(tasks)) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range next {
				walk := augDictDiffWalk{keySz: d.keySz, key: tasks[i].key, newAug: other.aug, checkNew: checkOtherAugmentation, fn: fn}
				errs[i] = walk.node(tasks[i].old, tasks[i].new, tasks[i].remaining, 0, 0)
			}
		}()
	}
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to scan augmented dictionary diff: %w", err)
		}
	}
	return nil
}

// scanDiffFrontierBits is the key depth at which ScanDiffParallel hands
// subtrees to workers. A queue key leads with 32 bits of workchain, so the
// frontier sits a few bits past that — deep enough for dozens of pairs,
// shallow enough that the sequential top stays small.
const scanDiffFrontierBits = 38

type augDictDiffWalk struct {
	keySz  uint
	key    Builder
	newAug Augmentation

	checkNew bool
	fn       AugDictDiffFunc

	checker augmentedNodeChecker

	// frontierBits, when non-zero, makes the walk stop at the first fork the
	// two dictionaries share at or below that key depth and hand the pair of
	// subtrees to tasks instead of recursing. See ScanDiffParallel.
	frontierBits uint
	tasks        *[]augDictDiffTask
}

// augDictDiffTask is one pair of subtrees under a shared fork, with the key
// prefix that leads to it, waiting to be scanned on a worker.
type augDictDiffTask struct {
	old, new  *Cell
	remaining uint
	key       Builder
}

func (w *augDictDiffWalk) node(old, new *Cell, remaining, skipOld, skipNew uint) error {
	if old == nil {
		if new == nil {
			return nil
		}
		if skipNew != 0 {
			return fmt.Errorf("invalid new dictionary alignment")
		}
		return w.oneSide(nil, new, remaining, false)
	}
	if new == nil {
		if skipOld != 0 {
			return fmt.Errorf("invalid old dictionary alignment")
		}
		return w.oneSide(old, nil, remaining, true)
	}
	if skipOld == skipNew && old.HashKey() == new.HashKey() {
		return nil
	}

	oldNode, err := parseAugDictDiffNode(old, remaining+skipOld)
	if err != nil {
		return fmt.Errorf("invalid old dictionary node: %w", err)
	}
	newNode, err := parseAugDictDiffNode(new, remaining+skipNew)
	if err != nil {
		return fmt.Errorf("invalid new dictionary node: %w", err)
	}
	if oldNode.labelLen < skipOld || newNode.labelLen < skipNew {
		return fmt.Errorf("invalid dictionary diff alignment")
	}

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
			if err = w.node(old, nil, remaining+skipOld, 0, 0); err != nil {
				return err
			}
			return w.node(nil, new, remaining+skipNew, 0, 0)
		}
		if err = w.node(nil, new, remaining+skipNew, 0, 0); err != nil {
			return err
		}
		return w.node(old, nil, remaining+skipOld, 0, 0)
	}

	if common == oldLen && common == newLen {
		if common == remaining {
			oldValue, newValue := oldNode.value(), newNode.value()
			equal := equalSliceContents(oldValue, newValue)
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
			return w.emit(oldValue, newValue)
		}

		if w.checkNew {
			if err = w.checker.fork(newNode, remaining-common, w.newAug); err != nil {
				return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
			}
		}

		nextRemaining := remaining - common - 1
		for child := 0; child < 2; child++ {
			w.setKeyBit(depth+common, byte(child))
			oldChild, err := oldNode.ref(child)
			if err != nil {
				return fmt.Errorf("failed to load old dictionary child: %w", err)
			}
			newChild, err := newNode.ref(child)
			if err != nil {
				return fmt.Errorf("failed to load new dictionary child: %w", err)
			}
			if w.tasks != nil && depth+common+1 >= w.frontierBits {
				// Both sides fork here and the pair of children below is an
				// independent scan: it reads nothing the other pairs read and
				// its key prefix is fully known. A task carries its own copy
				// of the prefix, so the worker's walk starts where this one
				// would have continued.
				*w.tasks = append(*w.tasks, augDictDiffTask{
					old: oldChild, new: newChild, remaining: nextRemaining, key: w.key,
				})
				continue
			}
			if err = w.node(oldChild, newChild, nextRemaining, 0, 0); err != nil {
				return err
			}
		}
		return nil
	}

	if common == oldLen {
		w.storeLabel(&newLabel, depth-skipNew)
		oldLeft, err := oldNode.ref(0)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary left child: %w", err)
		}
		oldRight, err := oldNode.ref(1)
		if err != nil {
			return fmt.Errorf("failed to load old dictionary right child: %w", err)
		}

		nextRemaining := remaining - common - 1
		branch := int(newEffective.bitAt(common))
		w.setKeyBit(depth+common, byte(branch))
		if branch == 0 {
			if err = w.node(oldLeft, new, nextRemaining, 0, skipNew+common+1); err != nil {
				return err
			}
			w.setKeyBit(depth+common, 1)
			return w.node(oldRight, nil, nextRemaining, 0, 0)
		}
		w.setKeyBit(depth+common, 0)
		if err = w.node(oldLeft, nil, nextRemaining, 0, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(oldRight, new, nextRemaining, 0, skipNew+common+1)
	}

	if w.checkNew {
		if err = w.checker.fork(newNode, remaining-common, w.newAug); err != nil {
			return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
		}
	}
	newLeft, err := newNode.ref(0)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary left child: %w", err)
	}
	newRight, err := newNode.ref(1)
	if err != nil {
		return fmt.Errorf("failed to load new dictionary right child: %w", err)
	}

	nextRemaining := remaining - common - 1
	branch := int(oldEffective.bitAt(common))
	if branch == 0 {
		w.setKeyBit(depth+common, 0)
		if err = w.node(old, newLeft, nextRemaining, skipOld+common+1, 0); err != nil {
			return err
		}
		w.setKeyBit(depth+common, 1)
		return w.node(nil, newRight, nextRemaining, 0, 0)
	}
	w.setKeyBit(depth+common, 0)
	if err = w.node(nil, newLeft, nextRemaining, 0, 0); err != nil {
		return err
	}
	w.setKeyBit(depth+common, 1)
	return w.node(old, newRight, nextRemaining, skipOld+common+1, 0)
}

func (w *augDictDiffWalk) oneSide(old, new *Cell, remaining uint, oldOnly bool) error {
	branch := new
	if oldOnly {
		branch = old
	}
	node, err := parseAugDictDiffNode(branch, remaining)
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
			return w.emit(node.value(), nil)
		}
		return w.emit(nil, node.value())
	}

	if !oldOnly && w.checkNew {
		if err = w.checker.fork(node, remaining-node.labelLen, w.newAug); err != nil {
			return fmt.Errorf("invalid new dictionary fork augmentation: %w", err)
		}
	}

	nextRemaining := remaining - node.labelLen - 1
	for child := 0; child < 2; child++ {
		w.setKeyBit(depth+node.labelLen, byte(child))
		ref, err := node.ref(child)
		if err != nil {
			return fmt.Errorf("failed to load dictionary child: %w", err)
		}
		if oldOnly {
			err = w.oneSide(ref, nil, nextRemaining, true)
		} else {
			err = w.oneSide(nil, ref, nextRemaining, false)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func parseAugDictDiffNode(branch *Cell, remaining uint) (fixedDictNode, error) {
	node, err := parseFixedDictNodeWithTrace(branch, remaining, branch.Trace())
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
	value := node.loader
	if err := aug.SkipExtra(&value); err != nil {
		return err
	}
	stored.bitEnd, stored.refEnd = value.bitStart, value.refStart

	c.computed = Builder{}
	if err := aug.LeafExtra(&value, &c.computed); err != nil {
		return err
	}
	if !c.computed.equalsSlice(&stored, &c.buf) {
		return fmt.Errorf("augmented dictionary leaf extra mismatch")
	}
	return nil
}

func (c *augmentedNodeChecker) fork(node fixedDictNode, remaining uint, aug Augmentation) error {
	left, err := c.child(node, 0)
	if err != nil {
		return err
	}
	right, err := c.child(node, 1)
	if err != nil {
		return err
	}
	childRemaining := remaining - 1
	c.left, err = extractAugmentedNodeExtraViewScratch(left, childRemaining, aug.SkipExtra, &c.scratch)
	if err != nil {
		return err
	}
	c.right, err = extractAugmentedNodeExtraViewScratch(right, childRemaining, aug.SkipExtra, &c.scratch)
	if err != nil {
		return err
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
// virtualizes the placeholder exactly as the loader result is validated, the
// same trace rides on the result, and the parse then notifies that trace with
// the same cell. The recorded read set, and the proof selected from it, are
// therefore unchanged — only the storage read disappears.
func (c *augmentedNodeChecker) child(node fixedDictNode, i int) (*Cell, error) {
	ref, err := node.ref(i)
	if err != nil || c.warm == nil || ref == nil || !ref.IsLazy() {
		return ref, err
	}
	loaded := c.warm[ref.rawCell().HashKey()]
	if loaded == nil {
		return ref, nil
	}
	return resolveLoadedLazyRefWithTrace(ref, loaded, ref.Trace())
}

func (w *augDictDiffWalk) emit(oldValueExtra, newValueExtra *Slice) error {
	w.key.bitsSz = w.keySz
	return w.fn(w.key.EndCell(), oldValueExtra, newValueExtra)
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
