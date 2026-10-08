package cell

import (
	"bytes"
	"testing"
)

type readProofStatGolden struct {
	cells        uint64
	bits         uint64
	internalRefs uint64
	externalRefs uint64
}

// cppNewCellStorageStatGolden is a transcription of the reference
// NewCellStorageStat: a plain recursive model with none of the packing
// CellStorageStat does, kept so the optimized counter is compared against the
// semantics rather than against itself.
type cppNewCellStorageStatGolden struct {
	seen      map[Hash]struct{}
	proofSeen map[Hash]struct{}
	stat      readProofStatGolden
	proofStat readProofStatGolden
	parent    *cppNewCellStorageStatGolden
	read      *ReadSet
}

func newCppNewCellStorageStatGolden() *cppNewCellStorageStatGolden {
	return &cppNewCellStorageStatGolden{
		seen:      map[Hash]struct{}{},
		proofSeen: map[Hash]struct{}{},
	}
}

func collectReadProofStatGolden(root *Cell, read *ReadSet) readProofStatGolden {
	var stat readProofStatGolden
	seen := map[Hash]struct{}{}

	var dfs func(*Cell)
	dfs = func(c *Cell) {
		if c == nil {
			return
		}
		if _, known := read.Prunable(c.HashKeyAt(0)); known {
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

func (s *cppNewCellStorageStatGolden) addProof(c *Cell, read *ReadSet) error {
	s.read = read
	return s.dfs(c, false, true)
}

func (s *cppNewCellStorageStatGolden) addCellAndProof(c *Cell, read *ReadSet) error {
	s.read = read
	return s.dfs(c, true, true)
}

func (s *cppNewCellStorageStatGolden) tentativeAddCell(c *Cell) (readProofStatGolden, error) {
	tentative := newCppNewCellStorageStatGolden()
	tentative.parent = s
	if err := tentative.addCell(c); err != nil {
		return readProofStatGolden{}, err
	}
	return tentative.stat, nil
}

func (s *cppNewCellStorageStatGolden) tentativeAddProof(c *Cell, read *ReadSet) (readProofStatGolden, error) {
	tentative := newCppNewCellStorageStatGolden()
	tentative.parent = s
	if err := tentative.addProof(c, read); err != nil {
		return readProofStatGolden{}, err
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
		if _, known := s.read.Prunable(c.HashKeyAt(0)); known {
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

func TestReadSetCppGoldenRecordAndIgnoreSemantics(t *testing.T) {
	left := BeginCell().MustStoreUInt(0x22, 8).EndCell()
	right := BeginCell().MustStoreUInt(0x33, 8).EndCell()
	root := BeginCell().MustStoreUInt(0x11, 8).MustStoreRef(left).MustStoreRef(right).EndCell()

	rs := NewReadSet(root)
	var recorded []*Cell
	rs.SetRecordCallback(func(c *Cell) {
		recorded = append(recorded, c)
	})

	slice, err := rs.Root().BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	leftRef, err := slice.PeekRefCellAt(0)
	if err != nil {
		t.Fatal(err)
	}
	rightRef, err := slice.PeekRefCellAt(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = leftRef.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, err = leftRef.BeginParse(); err != nil {
		t.Fatal(err)
	}

	if len(recorded) != 2 {
		t.Fatalf("record callback should fire once per cell, got %d", len(recorded))
	}
	if _, ok := rs.Contains(root.HashKey()); !ok {
		t.Fatal("the parsed root was not recorded")
	}
	if _, ok := rs.Contains(left.HashKey()); !ok {
		t.Fatal("the parsed left branch was not recorded")
	}
	if _, ok := rs.Contains(right.HashKey()); ok {
		t.Fatal("a branch that was only referenced was recorded")
	}

	rs.IgnoreReads(true)
	rs.IgnoreReads(true)
	if _, err = rightRef.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, ok := rs.Contains(right.HashKey()); ok {
		t.Fatal("ignore scope did not suppress recording")
	}
	if len(recorded) != 2 {
		t.Fatal("ignore scope did not suppress the record callback")
	}

	rs.IgnoreReads(false)
	if _, err = rightRef.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, ok := rs.Contains(right.HashKey()); ok {
		t.Fatal("nested ignore scopes must stay active until all of them are closed")
	}

	rs.IgnoreReads(false)
	if _, err = rightRef.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, ok := rs.Contains(right.HashKey()); !ok {
		t.Fatal("a read after the last ignore scope closed was not recorded")
	}
	if len(recorded) != 3 {
		t.Fatalf("record callback count = %d, want 3", len(recorded))
	}
}

// One trace records by hash, so two unrelated cells sharing it are two entries
// and one callback each — the recorder has no per-position identity to confuse.
func TestReadSetRecordCallbackReportsDistinctCellsAliasedToOneTrace(t *testing.T) {
	rs := NewReadSet(BeginCell().EndCell())
	records := make(map[Hash]int)
	rs.SetRecordCallback(func(c *Cell) {
		records[c.HashKey()]++
	})

	first := BeginCell().MustStoreUInt(1, 8).EndCell().WithTrace(rs.Trace())
	second := BeginCell().MustStoreUInt(2, 8).EndCell().WithTrace(rs.Trace())
	if _, err := first.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.BeginParse(); err != nil {
		t.Fatal(err)
	}

	if records[first.HashKey()] != 1 || records[second.HashKey()] != 1 {
		t.Fatalf("record callbacks = %#v, want each distinct hash once", records)
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

func TestReadSetCppGoldenLoadDictValidationDoesNotRecordDictRoot(t *testing.T) {
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

	if _, read := builder.ReadSet().Contains(loadedDict.root.HashKey()); read {
		t.Fatal("dict validation should not record the dict root")
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

func TestReadSetCppGoldenLoadPrefixDictDoesNotRecordDictRoot(t *testing.T) {
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

	if _, read := builder.ReadSet().Contains(loadedDict.root.HashKey()); read {
		t.Fatal("prefix dict loading should not recursively record the dict root")
	}
}

func TestReadSetCppGoldenLoadAugDictChecksOnlyRootExtra(t *testing.T) {
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

	if _, read := builder.ReadSet().Contains(loadedDict.root.HashKey()); read {
		t.Fatal("augmented dict loading should not recursively record the dict root")
	}
}

func TestReadSetCppGoldenAugDictValidationKeepsOnlyRoot(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []uint64{0x10, 0x20, 0x80} {
		if _, err = dict.SetWithMode(
			BeginCell().MustStoreUInt(key, 8).EndCell(),
			BeginCell().MustStoreUInt(key+1, 8).EndCell(),
			DictSetModeSet,
		); err != nil {
			t.Fatal(err)
		}
	}

	container := BeginCell().
		MustStoreBoolBit(true).
		MustStoreRef(dict.root).
		MustStoreBuilder(dict.GetRootExtra().MustBeginParse().ToBuilder()).
		EndCell()
	builder := NewMerkleProofBuilder(container)
	loaded, err := builder.Root().MustBeginParse().LoadAugDict(8, aug, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = loaded.Validate(); err != nil {
		t.Fatalf("validate recorded augmented dictionary: %v", err)
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := UnwrapProofVirtualized(proof, container.Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenDict, err := proven.MustBeginParse().LoadAugDict(8, aug, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = provenDict.Validate(); err != nil {
		t.Fatalf("validate proven augmented dictionary root: %v", err)
	}
	provenRoot := provenDict.RootCell()
	if provenRoot == nil || provenRoot.GetType() != OrdinaryCellType {
		t.Fatal("recorded validation proof does not carry the dictionary root")
	}
	pruned := 0
	for i := 0; i < int(provenRoot.RefsNum()); i++ {
		child, childErr := provenRoot.PeekRef(i)
		if childErr != nil {
			t.Fatal(childErr)
		}
		if child.GetType() == PrunedCellType {
			pruned++
		}
	}
	if pruned == 0 {
		t.Fatal("root-only validation retained every dictionary child")
	}
}

func TestReadSetCppGoldenAugDictValidationResolvesLazyRoot(t *testing.T) {
	aug := testMetricAugmentation{}
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []uint64{0x10, 0x80} {
		if _, err = dict.SetWithMode(
			BeginCell().MustStoreUInt(key, 8).EndCell(),
			BeginCell().MustStoreUInt(key+1, 8).EndCell(),
			DictSetModeSet,
		); err != nil {
			t.Fatal(err)
		}
	}

	loader := &testLazyLoader{cells: map[Hash]*Cell{dict.root.HashKey(): dict.root}}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(dict.root), loader.LoadCell)
	rs := NewReadSet(lazy)
	loaded := rs.Root().AsAugDict(8, aug)
	if err = loaded.Validate(); err != nil {
		t.Fatalf("validate lazy augmented dictionary: %v", err)
	}
	if loader.calls == 0 {
		t.Fatal("validation did not resolve the lazy augmented dictionary root")
	}
}

// A mutated dictionary root is a cell the source never held, and the recorder
// keys on hashes, so it can never stand in for the root it was derived from.
func TestReadSetCppGoldenMutatedDictRootIsNotPrunable(t *testing.T) {
	dict := NewDict(8)
	if err := dict.Set(BeginCell().MustStoreUInt(0x00, 8).EndCell(), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	if err := dict.Set(BeginCell().MustStoreUInt(0x80, 8).EndCell(), BeginCell().MustStoreUInt(0xBB, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	source := dict.AsCell()

	rs := NewReadSet(source)
	recorded := rs.Root().AsDict(8)

	rs.IgnoreReads(true)
	if err := recorded.Set(BeginCell().MustStoreUInt(0x40, 8).EndCell(), BeginCell().MustStoreUInt(0xCC, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	rs.IgnoreReads(false)

	mutated := recorded.AsCell()
	if mutated.HashKey() == source.HashKey() {
		t.Fatal("the mutation did not change the dictionary root")
	}
	if _, known := rs.Prunable(mutated.HashKeyAt(0)); known {
		t.Fatal("a mutated dict root must not be prunable against the source")
	}

	if _, err := mutated.BeginParse(); err != nil {
		t.Fatal(err)
	}
	if _, known := rs.Prunable(source.HashKeyAt(0)); known {
		t.Fatal("parsing a mutated dict root recorded the source root it was derived from")
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

func TestReadSetCppGoldenStorageProofBoundaryStat(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	ordinaryRoot := BeginCell().MustStoreUInt(0xBB, 8).MustStoreRef(leaf).EndCell()

	rs := NewReadSet(leaf)
	rs.Record(leaf)

	stat := collectReadProofStatGolden(ordinaryRoot, rs)
	want := readProofStatGolden{
		cells:        1,
		bits:         8,
		internalRefs: 1,
		externalRefs: 1,
	}
	if stat != want {
		t.Fatalf("storage proof boundary stat mismatch: got=%+v want=%+v", stat, want)
	}

	directExternal := collectReadProofStatGolden(leaf, rs)
	want = readProofStatGolden{externalRefs: 1}
	if directExternal != want {
		t.Fatalf("recorded cell should be counted as external only: got=%+v want=%+v", directExternal, want)
	}

	shared := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	withDuplicate := BeginCell().MustStoreRef(shared).MustStoreRef(shared).EndCell()
	stat = collectReadProofStatGolden(withDuplicate, NewReadSet(withDuplicate))
	want = readProofStatGolden{
		cells:        2,
		bits:         8,
		internalRefs: 3,
	}
	if stat != want {
		t.Fatalf("duplicate ordinary refs should count internal edges but deduplicate cells: got=%+v want=%+v", stat, want)
	}
}

func TestNewCellStorageStatCppGoldenCellAndProofSemantics(t *testing.T) {
	external := BeginCell().MustStoreUInt(0xA, 4).EndCell()
	shared := BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	root := BeginCell().
		MustStoreUInt(0xDD, 8).
		MustStoreRef(external).
		MustStoreRef(shared).
		MustStoreRef(shared).
		EndCell()

	rs := NewReadSet(external)
	rs.Record(external)

	stat := newCppNewCellStorageStatGolden()
	if err := stat.addCell(nil); err != nil {
		t.Fatal(err)
	}
	if err := stat.addCell(root); err != nil {
		t.Fatal(err)
	}
	wantCell := readProofStatGolden{
		cells:        3,
		bits:         20,
		internalRefs: 4,
	}
	if stat.stat != wantCell {
		t.Fatalf("cell stat mismatch: got=%+v want=%+v", stat.stat, wantCell)
	}

	proofStat := newCppNewCellStorageStatGolden()
	if err := proofStat.addProof(root, rs); err != nil {
		t.Fatal(err)
	}
	wantProof := readProofStatGolden{
		cells:        2,
		bits:         16,
		internalRefs: 3,
		externalRefs: 1,
	}
	if proofStat.proofStat != wantProof {
		t.Fatalf("proof stat mismatch: got=%+v want=%+v", proofStat.proofStat, wantProof)
	}

	allStat := newCppNewCellStorageStatGolden()
	if err := allStat.addCellAndProof(root, rs); err != nil {
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
	if tentativeCell != (readProofStatGolden{internalRefs: 1}) {
		t.Fatalf("tentative duplicate cell stat mismatch: got=%+v", tentativeCell)
	}
	tentativeProof, err := proofStat.tentativeAddProof(root, rs)
	if err != nil {
		t.Fatal(err)
	}
	if tentativeProof != (readProofStatGolden{internalRefs: 1}) {
		t.Fatalf("tentative duplicate proof stat mismatch: got=%+v", tentativeProof)
	}
}
