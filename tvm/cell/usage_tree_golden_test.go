package cell

import (
	"bytes"
	"testing"
)

type usageProofStatGolden struct {
	cells        uint64
	bits         uint64
	internalRefs uint64
	externalRefs uint64
}

type cppNewCellStorageStatGolden struct {
	seen      map[Hash]struct{}
	proofSeen map[Hash]struct{}
	stat      usageProofStatGolden
	proofStat usageProofStatGolden
	parent    *cppNewCellStorageStatGolden
	tree      *CellUsageTree
}

func newCppNewCellStorageStatGolden() *cppNewCellStorageStatGolden {
	return &cppNewCellStorageStatGolden{
		seen:      map[Hash]struct{}{},
		proofSeen: map[Hash]struct{}{},
	}
}

func collectUsageProofStatGolden(root *Cell, usageTree *CellUsageTree) usageProofStatGolden {
	var stat usageProofStatGolden
	seen := map[Hash]struct{}{}

	var dfs func(*Cell)
	dfs = func(c *Cell) {
		if c == nil {
			return
		}
		if _, ok := usageTree.NodeForCell(c); ok {
			stat.externalRefs++
			return
		}

		stat.internalRefs++
		if _, ok := seen[c.HashKey()]; ok {
			return
		}
		seen[c.HashKey()] = struct{}{}

		stat.cells++
		stat.bits += uint64(c.BitsSize())

		refView := newCellRefView(c)
		for i := 0; i < c.refsCount(); i++ {
			ref, err := refView.boundaryRef(i)
			if err != nil {
				panic(err)
			}
			dfs(ref)
		}
	}

	dfs(root)
	return stat
}

func (s *cppNewCellStorageStatGolden) addCell(c *Cell) error {
	return s.dfs(c, true, false)
}

func (s *cppNewCellStorageStatGolden) addProof(c *Cell, tree *CellUsageTree) error {
	s.tree = tree
	return s.dfs(c, false, true)
}

func (s *cppNewCellStorageStatGolden) addCellAndProof(c *Cell, tree *CellUsageTree) error {
	s.tree = tree
	return s.dfs(c, true, true)
}

func (s *cppNewCellStorageStatGolden) tentativeAddCell(c *Cell) (usageProofStatGolden, error) {
	tentative := newCppNewCellStorageStatGolden()
	tentative.parent = s
	if err := tentative.addCell(c); err != nil {
		return usageProofStatGolden{}, err
	}
	return tentative.stat, nil
}

func (s *cppNewCellStorageStatGolden) tentativeAddProof(c *Cell, tree *CellUsageTree) (usageProofStatGolden, error) {
	tentative := newCppNewCellStorageStatGolden()
	tentative.parent = s
	if err := tentative.addProof(c, tree); err != nil {
		return usageProofStatGolden{}, err
	}
	return tentative.proofStat, nil
}

func (s *cppNewCellStorageStatGolden) dfs(c *Cell, needStat, needProofStat bool) error {
	if c == nil {
		return nil
	}
	if needStat {
		s.stat.internalRefs++
		hash := c.HashKey()
		if s.parent != nil {
			if _, ok := s.parent.seen[hash]; ok {
				needStat = false
			}
		}
		if needStat {
			if _, ok := s.seen[hash]; ok {
				needStat = false
			} else {
				s.seen[hash] = struct{}{}
				s.stat.cells++
			}
		}
	}
	if needProofStat {
		if node, ok := s.tree.NodeForCell(c); ok && s.tree.validNode(node) {
			s.proofStat.externalRefs++
			needProofStat = false
		} else {
			s.proofStat.internalRefs++
			hash := c.HashKey()
			if s.parent != nil {
				if _, ok := s.parent.proofSeen[hash]; ok {
					needProofStat = false
				}
			}
			if needProofStat {
				if _, ok := s.proofSeen[hash]; ok {
					needProofStat = false
				} else {
					s.proofSeen[hash] = struct{}{}
					s.proofStat.cells++
				}
			}
		}
	}
	if !needStat && !needProofStat {
		return nil
	}

	loaded, err := c.load()
	if err != nil {
		return err
	}
	if needStat {
		s.stat.bits += uint64(loaded.BitsSize())
	}
	if needProofStat {
		s.proofStat.bits += uint64(loaded.BitsSize())
	}

	refView := newCellRefView(loaded)
	for i := 0; i < loaded.refsCount(); i++ {
		ref, err := refView.boundaryRef(i)
		if err != nil {
			return err
		}
		if err = s.dfs(ref, needStat, needProofStat); err != nil {
			return err
		}
	}
	return nil
}

func TestCellUsageTreeCppGoldenLoadMarkAndIgnoreSemantics(t *testing.T) {
	tree := NewCellUsageTree()
	root := tree.RootNode()
	left := tree.CreateChild(root, 0)
	right := tree.CreateChild(root, 1)
	leaf := tree.CreateChild(left, 0)

	var loaded []*Cell
	rootCell := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	leftCell := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	rightCell := BeginCell().MustStoreUInt(0x33, 8).EndCell()

	tree.SetCellLoadCallback(func(c *Cell) {
		loaded = append(loaded, c)
	})
	tree.OnLoad(root, rootCell)
	tree.OnLoad(root, rootCell)
	tree.OnLoad(left, leftCell)

	if len(loaded) != 2 {
		t.Fatalf("load callback should fire once per node, got %d", len(loaded))
	}
	if !tree.IsLoaded(root) || !tree.IsLoaded(left) || tree.IsLoaded(right) || tree.IsLoaded(leaf) {
		t.Fatal("loaded flags do not match C++ CellUsageTree semantics")
	}

	tree.SetIgnoreLoads(true)
	tree.SetIgnoreLoads(true)
	tree.OnLoad(right, rightCell)
	if tree.IsLoaded(right) || len(loaded) != 2 {
		t.Fatal("ignore_loads should suppress both load flag and callback")
	}
	if _, ok := tree.loadedCell(right); ok {
		t.Fatal("ignore_loads should not cache loaded cell by node")
	}
	if _, ok := tree.loadedCellByHash(rightCell.HashKey()); ok {
		t.Fatal("ignore_loads should not cache loaded cell by hash")
	}

	tree.SetIgnoreLoads(false)
	tree.OnLoad(right, rightCell)
	if tree.IsLoaded(right) || len(loaded) != 2 {
		t.Fatal("nested ignore_loads should remain active until all scopes are reset")
	}

	tree.SetIgnoreLoads(false)
	tree.OnLoad(right, rightCell)
	if !tree.IsLoaded(right) || len(loaded) != 3 {
		t.Fatal("load after ignore reset was not tracked")
	}
	if cached, ok := tree.loadedCell(right); !ok || cached.HashKey() != rightCell.HashKey() {
		t.Fatal("loaded cell was not cached after ignore reset")
	}

	if !tree.MarkPath(leaf) {
		t.Fatal("mark_path should accept a valid node")
	}
	if !tree.HasMark(root) || !tree.HasMark(left) || tree.HasMark(leaf) {
		t.Fatal("mark_path should mark parents only, including root")
	}

	tree.SetUseMarkForIsLoaded(true)
	if !tree.IsLoaded(root) || !tree.IsLoaded(left) || tree.IsLoaded(right) || tree.IsLoaded(leaf) {
		t.Fatal("use_mark should switch is_loaded to marked nodes only")
	}
}

func TestCellUsageTreeCppGoldenTraceNodeIdentityThroughCellFlows(t *testing.T) {
	tree := NewCellUsageTree()
	leaf := BeginCell().MustStoreUInt(0x44, 8).EndCell()
	root := BeginCell().MustStoreUInt(0x55, 8).MustStoreRef(leaf).EndCell().WithTrace(tree.RootTrace())

	rootNode, ok := tree.NodeForCell(root)
	if !ok || rootNode != tree.RootNode() {
		t.Fatalf("root usage node mismatch: got=%d ok=%v", rootNode, ok)
	}

	rootSlice := root.MustBeginParse()
	peeked, err := rootSlice.PeekRefCell()
	if err != nil {
		t.Fatal(err)
	}

	leafNode := tree.GetChild(rootNode, 0)
	if leafNode == 0 {
		t.Fatal("peek ref should create child usage node")
	}
	if node, ok := tree.NodeForCell(peeked); !ok || node != leafNode {
		t.Fatalf("peeked ref usage node mismatch: got=%d want=%d ok=%v", node, leafNode, ok)
	}
	if tree.IsLoaded(leafNode) {
		t.Fatal("peek ref cell should carry node identity without marking child as loaded")
	}

	loadedSlice, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if loadedSlice.MustLoadUInt(8) != 0x44 {
		t.Fatal("unexpected loaded leaf value")
	}
	if !tree.IsLoaded(leafNode) {
		t.Fatal("LoadRef should mark child node as loaded through BeginParse")
	}

	rebuilt, err := root.MustBeginParse().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tree.NodeForCell(rebuilt); ok {
		t.Fatal("newly built cell should not pretend to be a usage-tree source node")
	}
	if node, ok := tree.NodeForCell(rebuilt.MustPeekRef(0)); !ok || node != leafNode {
		t.Fatalf("slice copy should preserve child usage node: got=%d want=%d ok=%v", node, leafNode, ok)
	}

	builder := root.MustBeginParse().ToBuilder()
	if node, ok := tree.NodeForCell(builder.refs[0]); !ok || node != leafNode {
		t.Fatalf("builder copy should preserve child usage node: got=%d want=%d ok=%v", node, leafNode, ok)
	}
}

func TestMerkleProofBuilderCppGoldenPrunesUnloadedLazyBranch(t *testing.T) {
	left := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	fullRoot := BeginCell().MustStoreUInt(0x33, 8).MustStoreRef(left).MustStoreRef(right).EndCell()
	cells := map[Hash]*Cell{
		left.HashKey(0):  left,
		right.HashKey(0): right,
	}
	loads := map[Hash]int{}
	root := cellWithLazyRefsFromCell(fullRoot, func(hash Hash) (*Cell, error) {
		loads[hash]++
		return cells[hash], nil
	})

	builder := NewMerkleProofBuilder(root)
	rootSlice := builder.Root().MustBeginParse()
	leftSlice, err := rootSlice.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if leftSlice.MustLoadUInt(8) != 0x11 {
		t.Fatal("unexpected loaded left value")
	}
	if loads[left.HashKey(0)] == 0 {
		t.Fatal("left branch should be loaded before proof")
	}
	if loads[right.HashKey(0)] != 0 {
		t.Fatal("right branch should not be loaded before proof")
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	if loads[right.HashKey(0)] != 0 {
		t.Fatalf("unloaded lazy proof branch should stay at boundary, right loads=%d", loads[right.HashKey(0)])
	}

	body, err := UnwrapProof(proof, fullRoot.Hash())
	if err != nil {
		t.Fatal(err)
	}

	kept, err := body.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if kept.IsSpecial() {
		t.Fatal("loaded branch should stay ordinary in usage proof")
	}

	pruned, err := body.PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}
	if !pruned.IsSpecial() || pruned.GetType() != PrunedCellType {
		t.Fatalf("unloaded branch should be pruned, got special=%v type=%v", pruned.IsSpecial(), pruned.GetType())
	}
	if pruned.Level() != 1 {
		t.Fatalf("unloaded lazy branch should keep original pruned boundary level: got=%d want=1", pruned.Level())
	}
	if !bytes.Equal(pruned.Hash(0), right.Hash()) {
		t.Fatal("pruned lazy branch should preserve original branch hash")
	}
	if loads[right.HashKey(0)] != 0 {
		t.Fatalf("proof inspection should not load pruned lazy branch, right loads=%d", loads[right.HashKey(0)])
	}

	boc := proof.ToBOCWithFlags(false)
	if boc == nil {
		t.Fatal("expected proof boc")
	}
	if loads[right.HashKey(0)] != 0 {
		t.Fatalf("proof serialization should not load pruned lazy branch, right loads=%d", loads[right.HashKey(0)])
	}
}

func TestMerkleProofBuilderCppGoldenKeepsUnloadedOrdinaryLeafRef(t *testing.T) {
	child := BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	root := BeginCell().MustStoreUInt(0, 1).MustStoreRef(child).EndCell()

	builder := NewMerkleProofBuilder(root)
	if _, err := builder.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}

	kept, err := body.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if kept.IsSpecial() {
		t.Fatalf("unloaded ordinary leaf ref should stay ordinary like C++, got type=%v", kept.GetType())
	}
	if !bytes.Equal(kept.Hash(0), child.Hash()) {
		t.Fatal("kept leaf should preserve child hash")
	}
}

func TestMerkleProofBuilderCppGoldenPrunesUnloadedOrdinaryBranchRef(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(1, 1).EndCell()
	child := BeginCell().MustStoreUInt(0xBEEF, 16).MustStoreRef(leaf).EndCell()
	root := BeginCell().MustStoreUInt(0, 1).MustStoreRef(child).EndCell()

	builder := NewMerkleProofBuilder(root)
	if _, err := builder.Root().BeginParse(); err != nil {
		t.Fatal(err)
	}
	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	body, err := UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatal(err)
	}

	pruned, err := body.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !pruned.IsSpecial() || pruned.GetType() != PrunedCellType {
		t.Fatalf("unloaded ordinary branch ref should be pruned, got special=%v type=%v", pruned.IsSpecial(), pruned.GetType())
	}
	if !bytes.Equal(pruned.Hash(0), child.Hash()) {
		t.Fatal("pruned branch should preserve child hash")
	}
}

func TestCellUsageTreeCppGoldenLoadDictValidationDoesNotMarkDictRoot(t *testing.T) {
	dict := NewDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x00, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x80, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	container := BeginCell().MustStoreMaybeRef(dict.AsCell()).EndCell()
	builder := NewMerkleProofBuilder(container)
	loadedDict, err := builder.Root().MustBeginParse().LoadDict(8)
	if err != nil {
		t.Fatal(err)
	}

	dictNode, ok := builder.UsageTree().NodeForCell(loadedDict.root)
	if !ok {
		t.Fatal("loaded dict root should carry usage node identity")
	}
	if builder.UsageTree().IsLoaded(dictNode) {
		t.Fatal("dict validation should not mark the dict root as loaded")
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	body, err := UnwrapProof(proof, container.Hash())
	if err != nil {
		t.Fatal(err)
	}
	prunedDict, err := body.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if !prunedDict.IsSpecial() || prunedDict.GetType() != PrunedCellType {
		t.Fatalf("unloaded dict root should stay pruned in usage proof, got special=%v type=%v", prunedDict.IsSpecial(), prunedDict.GetType())
	}
}

func TestCellUsageTreeCppGoldenLoadPrefixDictDoesNotMarkDictRoot(t *testing.T) {
	dict := NewPrefixDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x10, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x20, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	container := BeginCell().MustStoreMaybeRef(dict.AsCell()).EndCell()
	builder := NewMerkleProofBuilder(container)
	loadedDict, err := builder.Root().MustBeginParse().LoadPrefixDict(8)
	if err != nil {
		t.Fatal(err)
	}

	dictNode, ok := builder.UsageTree().NodeForCell(loadedDict.root)
	if !ok {
		t.Fatal("loaded prefix dict root should carry usage node identity")
	}
	if builder.UsageTree().IsLoaded(dictNode) {
		t.Fatal("prefix dict loading should not recursively mark the dict root")
	}
}

func TestCellUsageTreeCppGoldenLoadAugDictChecksOnlyRootExtra(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(BeginCell().MustStoreUInt(0x10, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(BeginCell().MustStoreUInt(0x20, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	container := BeginCell().
		MustStoreBoolBit(true).
		MustStoreRef(dict.root).
		MustStoreBuilder(dict.GetRootExtra().MustBeginParse().ToBuilder()).
		EndCell()
	builder := NewMerkleProofBuilder(container)
	loadedDict, err := builder.Root().MustBeginParse().LoadAugDict(8, aug, false)
	if err != nil {
		t.Fatal(err)
	}

	dictNode, ok := builder.UsageTree().NodeForCell(loadedDict.root)
	if !ok {
		t.Fatal("loaded augmented dict root should carry usage node identity")
	}
	if builder.UsageTree().IsLoaded(dictNode) {
		t.Fatal("augmented dict loading should not recursively mark the dict root")
	}
}

func TestCellUsageTreeCppGoldenDictMutationDoesNotReuseSourceRootNode(t *testing.T) {
	dict := NewDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x00, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x80, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	tree := NewCellUsageTree()
	traced := dict.AsCell().AsDict(8).SetTrace(tree.RootTrace())
	if node, ok := tree.NodeForCell(traced.root); !ok || node != tree.RootNode() {
		t.Fatalf("source dict root usage node mismatch: got=%d ok=%v", node, ok)
	}

	tree.SetIgnoreLoads(true)
	if err := traced.Set(BeginCell().MustStoreUInt(0x40, 8).EndCell(), BeginCell().MustStoreUInt(0xCC, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	tree.SetIgnoreLoads(false)
	if node, ok := tree.NodeForCell(traced.root); ok {
		t.Fatalf("mutated dict root should not keep source usage node, got node=%d", node)
	}

	if _, err := traced.root.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if tree.IsLoaded(tree.RootNode()) {
		t.Fatal("parsing a mutated dict root should not mark the original source node")
	}
}

func TestMerkleProofBuilderCppGoldenProofBOCShape(t *testing.T) {
	left := BeginCell().MustStoreUInt(0x11, 8).EndCell()
	right := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	root := BeginCell().MustStoreUInt(0x33, 8).MustStoreRef(left).MustStoreRef(right).EndCell()

	builder := NewMerkleProofBuilder(root)
	leftSlice, err := builder.Root().MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if leftSlice.MustLoadUInt(8) != 0x11 {
		t.Fatal("unexpected left value")
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	prunedRight, err := createPrunedBranchFromCell(right, 1)
	if err != nil {
		t.Fatal(err)
	}
	expectedBody := BeginCell().
		MustStoreUInt(0x33, 8).
		MustStoreRef(left).
		MustStoreRef(prunedRight).
		EndCell()
	expectedProof, err := CreateMerkleProof(expectedBody)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(proof.ToBOCWithOptions(BOCSerializeOptions{}), expectedProof.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("usage proof BOC mismatch:\n got: %x\nwant: %x", proof.ToBOCWithOptions(BOCSerializeOptions{}), expectedProof.ToBOCWithOptions(BOCSerializeOptions{}))
	}
}

func TestMerkleProofBuilderCppGoldenDuplicateHashIncludedByVisitedHash(t *testing.T) {
	shared := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	root := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(shared).MustStoreRef(shared).EndCell()

	builder := NewMerkleProofBuilder(root)
	leftSlice, err := builder.Root().MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if leftSlice.MustLoadUInt(8) != 0xAA {
		t.Fatal("unexpected shared value")
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	expectedBody := BeginCell().
		MustStoreUInt(0xBB, 8).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()
	expectedProof, err := CreateMerkleProof(expectedBody)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(proof.ToBOCWithOptions(BOCSerializeOptions{}), expectedProof.ToBOCWithOptions(BOCSerializeOptions{})) {
		t.Fatalf("duplicate-hash proof BOC mismatch:\n got: %x\nwant: %x", proof.ToBOCWithOptions(BOCSerializeOptions{}), expectedProof.ToBOCWithOptions(BOCSerializeOptions{}))
	}
}

func TestCellUsageTreeCppGoldenStorageProofBoundaryStat(t *testing.T) {
	tree := NewCellUsageTree()
	root := tree.RootNode()
	child := tree.CreateChild(root, 0)

	leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell().WithTrace(tree.Trace(child))
	ordinaryRoot := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()

	stat := collectUsageProofStatGolden(ordinaryRoot, tree)
	want := usageProofStatGolden{
		cells:        1,
		bits:         8,
		internalRefs: 1,
		externalRefs: 1,
	}
	if stat != want {
		t.Fatalf("storage proof boundary stat mismatch: got=%+v want=%+v", stat, want)
	}

	directExternal := collectUsageProofStatGolden(leaf, tree)
	want = usageProofStatGolden{externalRefs: 1}
	if directExternal != want {
		t.Fatalf("direct usage cell should be counted as external only: got=%+v want=%+v", directExternal, want)
	}

	shared := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	withDuplicate := BeginCell().MustStoreRef(shared).MustStoreRef(shared).EndCell()
	stat = collectUsageProofStatGolden(withDuplicate, NewCellUsageTree())
	want = usageProofStatGolden{
		cells:        2,
		bits:         8,
		internalRefs: 3,
	}
	if stat != want {
		t.Fatalf("duplicate ordinary refs should count internal edges but deduplicate cells: got=%+v want=%+v", stat, want)
	}
}

func TestNewCellStorageStatCppGoldenCellAndProofSemantics(t *testing.T) {
	tree := NewCellUsageTree()
	externalNode := tree.CreateChild(tree.RootNode(), 0)

	external := BeginCell().MustStoreUInt(0xA, 4).EndCell().WithTrace(tree.Trace(externalNode))
	shared := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := BeginCell().
		MustStoreUInt(0xDD, 8).
		MustStoreRef(external).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	stat := newCppNewCellStorageStatGolden()
	if err := stat.addCell(nil); err != nil {
		t.Fatal(err)
	}
	if err := stat.addCell(root); err != nil {
		t.Fatal(err)
	}
	wantCell := usageProofStatGolden{
		cells:        3,
		bits:         20,
		internalRefs: 4,
	}
	if stat.stat != wantCell {
		t.Fatalf("cell stat mismatch: got=%+v want=%+v", stat.stat, wantCell)
	}

	proofStat := newCppNewCellStorageStatGolden()
	if err := proofStat.addProof(root, tree); err != nil {
		t.Fatal(err)
	}
	wantProof := usageProofStatGolden{
		cells:        2,
		bits:         16,
		internalRefs: 3,
		externalRefs: 1,
	}
	if proofStat.proofStat != wantProof {
		t.Fatalf("proof stat mismatch: got=%+v want=%+v", proofStat.proofStat, wantProof)
	}

	allStat := newCppNewCellStorageStatGolden()
	if err := allStat.addCellAndProof(root, tree); err != nil {
		t.Fatal(err)
	}
	if allStat.stat != wantCell {
		t.Fatalf("combined cell stat mismatch: got=%+v want=%+v", allStat.stat, wantCell)
	}
	if allStat.proofStat != wantProof {
		t.Fatalf("combined proof stat mismatch: got=%+v want=%+v", allStat.proofStat, wantProof)
	}

	tentativeCell, err := stat.tentativeAddCell(root)
	if err != nil {
		t.Fatal(err)
	}
	if tentativeCell != (usageProofStatGolden{internalRefs: 1}) {
		t.Fatalf("tentative duplicate cell stat mismatch: got=%+v", tentativeCell)
	}
	tentativeProof, err := proofStat.tentativeAddProof(root, tree)
	if err != nil {
		t.Fatal(err)
	}
	if tentativeProof != (usageProofStatGolden{internalRefs: 1}) {
		t.Fatalf("tentative duplicate proof stat mismatch: got=%+v", tentativeProof)
	}
}
