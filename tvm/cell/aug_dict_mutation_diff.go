package cell

import "fmt"

// AugmentedDictionaryDiff is the structural augmentation closure produced by
// one bulk augmented-dictionary mutation. Replay validates every changed
// candidate node through its final Patricia path without walking the old and
// new dictionaries again.
type AugmentedDictionaryDiff struct {
	aug    Augmentation
	replay *augDiffReplayNode
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
	var checker augmentedNodeChecker
	var trace *Trace
	if d.replay != nil {
		trace = d.replay.cell.Trace()
	}
	if err := replayAugDiffNode(d.replay, trace, d.aug, &checker); err != nil {
		return fmt.Errorf("failed to replay augmented dictionary diff: %w", err)
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
