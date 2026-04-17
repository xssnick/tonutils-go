package cell

type proofTraceNode struct {
	parent    TraceNode
	children  [4]TraceNode
	marked    bool
	recursive bool
}

// ProofTrace accumulates a path-aware usage tree and can project it into a proof skeleton.
type ProofTrace struct {
	nodes []proofTraceNode
	root  TraceNode
}

type proofTraceSkeletonMode uint8

const (
	proofTraceSkeletonUsage proofTraceSkeletonMode = iota
	proofTraceSkeletonUsageRecursive
	proofTraceSkeletonMarked
)

// NewProofTrace creates an empty proof trace.
func NewProofTrace() *ProofTrace {
	return &ProofTrace{
		nodes: make([]proofTraceNode, 1),
	}
}

// OnCellLoad is a no-op observer hook kept for interface compatibility.
func (t *ProofTrace) OnCellLoad(_ Hash) {}

// OnCellCreate is a no-op observer hook kept for interface compatibility.
func (t *ProofTrace) OnCellCreate() {}

// OnRef returns a stable child node for the given parent/ref slot pair.
func (t *ProofTrace) OnRef(parent TraceNode, refIdx int) TraceNode {
	if refIdx < 0 || refIdx >= 4 {
		return 0
	}
	if parent == 0 {
		parent = t.ensureRoot()
	} else if !t.validNode(parent) {
		return 0
	}

	if childNode := t.nodes[parent].children[refIdx]; childNode != 0 {
		return childNode
	}

	childNode := t.newNode(parent)
	t.nodes[parent].children[refIdx] = childNode
	return childNode
}

// MarkPath marks the slice node and all its ancestors up to the root.
func (t *ProofTrace) MarkPath(sl *Slice) bool {
	node := t.nodeForSlice(sl)
	if node == 0 {
		return false
	}
	return t.markPathNode(node)
}

// MarkRecursive marks the path to the slice node and keeps the whole subtree
// under that node in ordinary form in MarkedSkeleton.
func (t *ProofTrace) MarkRecursive(sl *Slice) bool {
	node := t.nodeForSlice(sl)
	if node == 0 {
		return false
	}
	if !t.markPathNode(node) {
		return false
	}
	t.nodes[node].recursive = true
	return true
}

func (t *ProofTrace) markPathNode(node TraceNode) bool {
	if !t.validNode(node) {
		return false
	}

	for node != 0 {
		if t.nodes[node].marked {
			break
		}
		t.nodes[node].marked = true
		node = t.nodes[node].parent
	}
	return true
}

// Skeleton projects the full recorded usage into a proof skeleton and keeps
// recursively marked nodes in ordinary form.
func (t *ProofTrace) Skeleton() *ProofSkeleton {
	return t.newSkeleton(proofTraceSkeletonUsageRecursive)
}

// UsageSkeleton projects the recorded ref usage into a proof skeleton.
func (t *ProofTrace) UsageSkeleton() *ProofSkeleton {
	return t.newSkeleton(proofTraceSkeletonUsage)
}

// AppendUsageTo merges the recorded usage tree into dst and returns the subtree
// skeleton corresponding to leaf. If leaf is zero, dst itself is returned.
func (t *ProofTrace) AppendUsageTo(dst *ProofSkeleton, leaf TraceNode) *ProofSkeleton {
	return t.buildSkeleton(dst, leaf, proofTraceSkeletonUsage)
}

// MarkedSkeleton projects the currently marked part of the usage tree into a proof skeleton.
func (t *ProofTrace) MarkedSkeleton() *ProofSkeleton {
	return t.newSkeleton(proofTraceSkeletonMarked)
}

func (t *ProofTrace) newSkeleton(mode proofTraceSkeletonMode) *ProofSkeleton {
	sk := CreateProofSkeleton()
	t.buildSkeleton(sk, 0, mode)
	return sk
}

func (t *ProofTrace) buildSkeleton(dst *ProofSkeleton, leaf TraceNode, mode proofTraceSkeletonMode) *ProofSkeleton {
	if dst == nil {
		return nil
	}
	if leaf != 0 && !t.validNode(leaf) {
		return nil
	}
	if !t.validNode(t.root) {
		if leaf == 0 {
			return dst
		}
		return nil
	}

	leafSk := (*ProofSkeleton)(nil)
	if leaf == 0 {
		leafSk = dst
	}
	t.appendSkeleton(dst, t.root, leaf, &leafSk, mode)
	return leafSk
}

func (t *ProofTrace) appendSkeleton(sk *ProofSkeleton, node, leaf TraceNode, leafSk **ProofSkeleton, mode proofTraceSkeletonMode) bool {
	if node == leaf && *leafSk == nil {
		*leafSk = sk
	}

	cur := t.nodes[node]
	if mode == proofTraceSkeletonMarked {
		if cur.recursive {
			sk.SetRecursive()
			return true
		}

		has := cur.marked
		for i, child := range cur.children {
			if child == 0 {
				continue
			}

			childSk := sk.ProofRef(i)
			if !t.appendSkeleton(childSk, child, leaf, leafSk, mode) {
				sk.branches[i] = nil
				continue
			}
			has = true
		}
		return has
	}

	if mode == proofTraceSkeletonUsageRecursive && cur.recursive {
		sk.SetRecursive()
		return true
	}

	for i, child := range cur.children {
		if child == 0 {
			continue
		}

		t.appendSkeleton(sk.ProofRef(i), child, leaf, leafSk, mode)
	}
	return true
}

func (t *ProofTrace) newNode(parent TraceNode) TraceNode {
	id := TraceNode(len(t.nodes))
	t.nodes = append(t.nodes, proofTraceNode{parent: parent})
	return id
}

func (t *ProofTrace) ensureRoot() TraceNode {
	if t.root == 0 {
		t.root = t.newNode(0)
	}
	return t.root
}

func (t *ProofTrace) nodeForSlice(sl *Slice) TraceNode {
	if sl == nil || sl.observer != t {
		return 0
	}
	if sl.traceNode != 0 {
		return sl.traceNode
	}
	if sl.cell == nil {
		return 0
	}
	return t.ensureRoot()
}

func (t *ProofTrace) validNode(node TraceNode) bool {
	return node != 0 && int(node) < len(t.nodes)
}
