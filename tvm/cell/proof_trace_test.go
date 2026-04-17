package cell

import "testing"

type recordingTraceObserver struct {
	creates int
	next    TraceNode
	loads   []Hash
	refs    []recordedTraceRef
}

type recordedTraceRef struct {
	parent TraceNode
	refIdx int
	node   TraceNode
}

func (o *recordingTraceObserver) OnCellLoad(hash Hash) {
	o.loads = append(o.loads, hash)
}

func (o *recordingTraceObserver) OnCellCreate() {
	o.creates++
}

func (o *recordingTraceObserver) OnRef(parent TraceNode, refIdx int) TraceNode {
	o.next++
	node := o.next
	o.refs = append(o.refs, recordedTraceRef{
		parent: parent,
		refIdx: refIdx,
		node:   node,
	})
	return node
}

func TestProofTraceSkeletonTracksUsedRefs(t *testing.T) {
	trace := NewProofTrace()
	leftNode := trace.OnRef(0, 0)
	if leftNode == 0 {
		t.Fatal("expected left child node")
	}
	rootNode := trace.root
	if rootNode == 0 {
		t.Fatal("expected lazy root node")
	}
	if again := trace.OnRef(0, 0); again != leftNode {
		t.Fatalf("expected stable child node, got %d and %d", leftNode, again)
	}

	rightLeafNode := trace.OnRef(leftNode, 1)
	if rightLeafNode == 0 {
		t.Fatal("expected right leaf node")
	}

	sk := trace.Skeleton()
	if sk.branches[0] == nil {
		t.Fatal("expected left branch in skeleton")
	}
	if sk.branches[1] != nil {
		t.Fatal("did not expect right branch in skeleton")
	}
	if sk.branches[0].branches[0] != nil {
		t.Fatal("did not expect unused left[0] branch in skeleton")
	}
	if sk.branches[0].branches[1] == nil {
		t.Fatal("expected used left[1] branch in skeleton")
	}
}

func TestProofTraceMarkedSkeletonUsesMarksWhenPresent(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xaa, 8).EndCell()
	left := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(leaf).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).MustStoreRef(BeginCell().MustStoreUInt(0xbb, 8).EndCell()).EndCell()
	root := BeginCell().MustStoreUInt(0x33, 8).MustStoreRef(left).MustStoreRef(right).EndCell()

	trace := NewProofTrace()
	rootSlice := root.BeginParse().SetObserver(trace)
	leftSlice, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatalf("load left ref: %v", err)
	}
	leafSlice, err := leftSlice.LoadRef()
	if err != nil {
		t.Fatalf("load leaf ref: %v", err)
	}

	if !trace.MarkPath(leafSlice) {
		t.Fatal("expected path mark to succeed")
	}

	sk := trace.MarkedSkeleton()
	if sk.branches[0] == nil {
		t.Fatal("expected path to marked leaf in skeleton")
	}
	if sk.branches[0].branches[0] == nil {
		t.Fatal("expected marked leaf branch in skeleton")
	}
	if sk.branches[1] != nil {
		t.Fatal("did not expect unmarked right branch in skeleton")
	}
}

func TestProofTraceMarkedSkeletonIsEmptyWithoutMarks(t *testing.T) {
	trace := NewProofTrace()
	leftNode := trace.OnRef(0, 0)
	if leafNode := trace.OnRef(leftNode, 0); leafNode == 0 {
		t.Fatal("expected leaf node")
	}

	sk := trace.MarkedSkeleton()
	for i, br := range sk.branches {
		if br != nil {
			t.Fatalf("expected empty marked skeleton after reset, got branch %d", i)
		}
	}
}

func TestSliceObserverTraceHooks(t *testing.T) {
	child := BeginCell().MustStoreUInt(2, 8).EndCell()
	root := BeginCell().MustStoreUInt(1, 8).MustStoreRef(child).EndCell()

	obs := &recordingTraceObserver{next: 100}
	sl := root.BeginParse().SetObserver(obs)

	if peeked, err := sl.PeekRefCell(); err != nil || peeked != child {
		t.Fatalf("peek ref cell = (%v, %v), want (%v, nil)", peeked, err, child)
	}
	if len(obs.refs) != 0 || len(obs.loads) != 0 {
		t.Fatal("peek ref should not notify observer")
	}

	childSlice, err := sl.LoadRef()
	if err != nil {
		t.Fatalf("load ref: %v", err)
	}
	if childSlice.observer != obs {
		t.Fatal("expected observer to propagate to child slice")
	}
	if childSlice.traceNode == 0 {
		t.Fatal("expected non-zero child trace node")
	}
	if len(obs.refs) != 1 {
		t.Fatalf("expected one ref notification, got %d", len(obs.refs))
	}
	if obs.refs[0].parent != 0 || obs.refs[0].refIdx != 0 {
		t.Fatalf("unexpected ref notification: %+v", obs.refs[0])
	}
	if childSlice.traceNode != obs.refs[0].node {
		t.Fatalf("expected child slice node %d, got %d", obs.refs[0].node, childSlice.traceNode)
	}
	if len(obs.loads) != 1 || obs.loads[0] != child.HashKey() {
		t.Fatal("expected one load notification for child hash")
	}
}

func TestSliceObserverTraceNodeIsStableAcrossPreloadAndLoad(t *testing.T) {
	child := BeginCell().MustStoreUInt(2, 8).EndCell()
	root := BeginCell().MustStoreUInt(1, 8).MustStoreRef(child).EndCell()

	trace := NewProofTrace()
	sl := root.BeginParse().SetObserver(trace)

	first, err := sl.PreloadRef()
	if err != nil {
		t.Fatalf("first preload ref: %v", err)
	}
	second, err := sl.PreloadRef()
	if err != nil {
		t.Fatalf("second preload ref: %v", err)
	}
	loaded, err := sl.LoadRef()
	if err != nil {
		t.Fatalf("load ref: %v", err)
	}

	if first.traceNode != second.traceNode || first.traceNode != loaded.traceNode {
		t.Fatalf("expected stable child node across preload/load, got %d, %d, %d", first.traceNode, second.traceNode, loaded.traceNode)
	}
}
