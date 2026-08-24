package cell

import (
	"fmt"
	"sync"
)

// AugmentedDictionaryDiff is the structural augmentation closure produced by
// one bulk augmented-dictionary mutation. Replay validates every changed
// candidate node through its final Patricia path without walking the old and
// new dictionaries again.
type AugmentedDictionaryDiff struct {
	aug    Augmentation
	replay *augDiffReplayNode
	// warm is the mutation's resident cells, keyed by lazy placeholder hash.
	// See augBulkPathResolver.warmCells and augmentedNodeChecker.child.
	warm map[Hash]*Cell
}

// augDiffReplayNode is the sparse set of changed nodes in the final trie. Its
// parent shape retains the final path needed to derive the same trace as
// ScanDiff when augmentation code follows candidate references.
type augDiffReplayNode struct {
	cell             *Cell
	remaining        uint
	predecessorTrace *Trace
	left             *augDiffReplayNode
	right            *augDiffReplayNode
}

// Replay checks the mutation's structural augmentation closure through the
// final candidate paths without comparing the old and new dictionaries again.
func (d *AugmentedDictionaryDiff) Replay() error {
	checker := augmentedNodeChecker{warm: d.warm}
	var trace *Trace
	if d.replay != nil {
		trace = d.replay.cell.Trace()
	}
	if err := replayAugDiffNode(d.replay, trace, d.aug, &checker); err != nil {
		return fmt.Errorf("failed to replay augmented dictionary diff: %w", err)
	}
	return nil
}

// ReplayParallel is Replay with the work split across workers goroutines.
//
// The closure is a sparse trie: a path per changed key, sharing the top. The
// top is replayed sequentially down to replayFrontierDepth, and every changed
// child hanging off that depth is a task — an independent subtree whose
// replay reads nothing another task writes. The checker's scratch is per
// task, and what the replay does to shared state is record reads into
// listeners that are built for concurrent recording. The recorded set is the
// same in any order; errors keep the sequential priority by being collected
// per task and reported in trie order.
//
// A changed-account closure on a state of real depth costs tens of
// milliseconds on one goroutine, about half of it parsing siblings the
// collation never loaded; splitting it overlaps both the parsing and the
// loads. workers below two, or a closure too small to have a frontier, fall
// back to the sequential walk.
func (d *AugmentedDictionaryDiff) ReplayParallel(workers int) error {
	if workers < 2 || d.replay == nil {
		return d.Replay()
	}
	var trace *Trace
	if d.replay != nil {
		trace = d.replay.cell.Trace()
	}
	top := augmentedNodeChecker{warm: d.warm}
	var tasks []augDiffReplayTask
	if err := replayAugDiffNodeTo(d.replay, trace, d.aug, &top, replayFrontierDepth, &tasks); err != nil {
		return fmt.Errorf("failed to replay augmented dictionary diff: %w", err)
	}
	if len(tasks) < 2 {
		for _, task := range tasks {
			if err := replayAugDiffNode(task.node, task.trace, d.aug, &top); err != nil {
				return fmt.Errorf("failed to replay augmented dictionary diff: %w", err)
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
			checker := augmentedNodeChecker{warm: d.warm}
			for i := range next {
				errs[i] = replayAugDiffNode(tasks[i].node, tasks[i].trace, d.aug, &checker)
			}
		}()
	}
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			return fmt.Errorf("failed to replay augmented dictionary diff: %w", err)
		}
	}
	return nil
}

// replayFrontierDepth is where ReplayParallel stops descending on its own and
// hands subtrees to workers. Six levels give at most 64 tasks, enough to keep
// sixteen workers busy on a closure of a few hundred keys while keeping the
// sequential top to a few dozen nodes.
const replayFrontierDepth = 6

type augDiffReplayTask struct {
	node  *augDiffReplayNode
	trace *Trace
}

// replayAugDiffNodeTo is replayAugDiffNode that stops at depth and collects
// the children below it instead of recursing into them. It does for its own
// nodes exactly what the sequential walk does, so the top of the closure is
// checked identically whichever walk runs underneath.
func replayAugDiffNodeTo(node *augDiffReplayNode, trace *Trace, aug Augmentation, checker *augmentedNodeChecker, depth int, tasks *[]augDiffReplayTask) error {
	if node == nil {
		return nil
	}
	parsed, err := parseFixedDictNodeWithTrace(node.cell, node.remaining, trace)
	if err != nil {
		return fmt.Errorf("failed to load replay node: %w", err)
	}
	if err = parsed.rejectSpecial("augmented dictionary"); err != nil {
		return err
	}
	if err = parsed.validateForkShape(node.remaining, true); err != nil {
		return err
	}
	checked := parsed
	checked.loader.trace = CombineTraces(parsed.loader.trace, node.predecessorTrace)
	if checked.isLeaf(node.remaining) {
		if err = checker.leaf(checked, aug); err != nil {
			return fmt.Errorf("invalid changed leaf augmentation: %w", err)
		}
	} else if err = checker.fork(checked, node.remaining-checked.labelLen, aug); err != nil {
		return fmt.Errorf("invalid changed fork augmentation: %w", err)
	}

	children := [2]*augDiffReplayNode{node.left, node.right}
	for bit, child := range children {
		if child == nil {
			continue
		}
		ref, childTrace, err := parsed.refAndTrace(bit)
		if err != nil {
			return fmt.Errorf("failed to load replay child %d: %w", bit, err)
		}
		if ref.HashKey() != child.cell.HashKey() {
			return fmt.Errorf("replay child %d does not belong to the final dictionary", bit)
		}
		if depth <= 1 {
			*tasks = append(*tasks, augDiffReplayTask{node: child, trace: childTrace})
			continue
		}
		if err = replayAugDiffNodeTo(child, childTrace, aug, checker, depth-1, tasks); err != nil {
			return err
		}
	}
	return nil
}

func replayAugDiffNode(node *augDiffReplayNode, trace *Trace, aug Augmentation, checker *augmentedNodeChecker) error {
	if node == nil {
		return nil
	}

	parsed, err := parseFixedDictNodeWithTrace(node.cell, node.remaining, trace)
	if err != nil {
		return fmt.Errorf("failed to load replay node: %w", err)
	}
	if err = parsed.rejectSpecial("augmented dictionary"); err != nil {
		return err
	}
	if err = parsed.validateForkShape(node.remaining, true); err != nil {
		return err
	}

	checked := parsed
	checked.loader.trace = CombineTraces(parsed.loader.trace, node.predecessorTrace)
	if checked.isLeaf(node.remaining) {
		if err = checker.leaf(checked, aug); err != nil {
			return fmt.Errorf("invalid changed leaf augmentation: %w", err)
		}
	} else if err = checker.fork(checked, node.remaining-checked.labelLen, aug); err != nil {
		return fmt.Errorf("invalid changed fork augmentation: %w", err)
	}

	children := [2]*augDiffReplayNode{node.left, node.right}
	for bit, child := range children {
		if child == nil {
			continue
		}
		ref, childTrace, err := parsed.refAndTrace(bit)
		if err != nil {
			return fmt.Errorf("failed to load replay child %d: %w", bit, err)
		}
		if ref.HashKey() != child.cell.HashKey() {
			return fmt.Errorf("replay child %d does not belong to the final dictionary", bit)
		}
		if err = replayAugDiffNode(child, childTrace, aug, checker); err != nil {
			return err
		}
	}
	return nil
}

func computedAugDiffReplay(cell *Cell, remaining uint, left, right *augDiffReplayNode) *augDiffReplayNode {
	return &augDiffReplayNode{
		cell:      cell,
		remaining: remaining,
		left:      left,
		right:     right,
	}
}

func relabeledAugDiffReplay(cell *Cell, remaining uint, predecessorTrace *Trace, prior *augDiffReplayNode) *augDiffReplayNode {
	replay := &augDiffReplayNode{
		cell:             cell,
		remaining:        remaining,
		predecessorTrace: predecessorTrace,
	}
	if prior != nil {
		replay.predecessorTrace = CombineTraces(predecessorTrace, prior.predecessorTrace)
		replay.left = prior.left
		replay.right = prior.right
	}
	return replay
}
