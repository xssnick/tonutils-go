package cell

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// The differential gate for PreparedMerkleUpdate.
//
// It is a precondition of the fused path, not a follow-up to it: this is the
// one package where a divergence between two nodes is a fork rather than a
// slowdown, and a hand-written diff to these walks has already passed every
// pre-existing gate while carrying a real boundaryRef/virtualization bug.
//
// Per case it compares the separate path (ValidateMerkleUpdate, then
// ApplyMerkleUpdate per parent) against the fused path (PrepareMerkleUpdate /
// PrepareMerkleUpdatePlanned, then ApplyTo per parent) on INDEPENDENTLY
// constructed inputs, asserting:
//
//  1. accept/reject, per verdict, and the error text
//  2. the output root hash, per parent form
//  3. shared-subtree reuse: countSharedCells equality, plus a lockstep walk
//     requiring reuse and rebuild at exactly the same nodes
//  4. materialization counters (lazy / virtualized / pruned cells), including
//     the cross-form assertion that two hash-equal parents produce hash-equal
//     results with DIFFERENT materialization
//  5. the per-hash lazy-loader call multiset
//  6. determinism, by applying twice to freshly built parents
//  7. concurrency, by applying one capsule to two parents at once, and two
//     capsules to one SHARED LAZY parent — production's ChainState root, which
//     every competing child applies to at the same time
//  8. all of the above once more with the UPDATE tree lazy on both sides, which
//     is where the capsule's cached update cells could produce a right-looking
//     root standing on differently materialized ones

// ---------------------------------------------------------------- loaders

// preparedLoader resolves a whole tree lazily and counts the resolutions per
// cell, so a test can say WHICH subtree was fetched and how often.
//
// The mutex is not decoration: production hands one lazy ChainState root to
// every competing child at once, so a loader standing in for the node store is
// read from several goroutines, and an unguarded map here would report a
// harness race where the question is whether the WALK is safe.
type preparedLoader struct {
	mu      sync.Mutex
	cells   map[Hash]*Cell
	calls   map[Hash]int
	missing map[Hash]bool
}

func newPreparedLoader(root *Cell) *preparedLoader {
	l := &preparedLoader{cells: map[Hash]*Cell{}, calls: map[Hash]int{}, missing: map[Hash]bool{}}
	l.index(root)
	return l
}

func (l *preparedLoader) index(c *Cell) {
	if c == nil {
		return
	}
	key := c.HashKey()
	if _, ok := l.cells[key]; ok {
		return
	}
	l.cells[key] = c
	for _, ref := range c.rawRefs() {
		l.index(ref)
	}
}

func (l *preparedLoader) LoadCell(hash Hash) (*Cell, error) {
	l.mu.Lock()
	l.calls[hash]++
	missing, c := l.missing[hash], l.cells[hash]
	l.mu.Unlock()

	if missing || c == nil {
		return nil, ErrLazyRefNotFound
	}
	return cellWithLazyRefsFromCell(c, l.LoadCell), nil
}

func (l *preparedLoader) lazyRoot(root *Cell) *Cell {
	return cellWithLazyRefsFromCell(root, l.LoadCell)
}

func (l *preparedLoader) snapshot() map[Hash]int {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make(map[Hash]int, len(l.calls))
	for k, v := range l.calls {
		out[k] = v
	}
	return out
}

func (l *preparedLoader) loadCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.calls)
}

// ---------------------------------------------------------------- parents

type preparedParent struct {
	name   string
	root   *Cell
	loader *preparedLoader
}

// preparedParents builds every materialization of one parent the fused path has
// to behave identically on. The narrow proof form is production's parent A —
// unwrapCollatedProof feeds exactly this shape to the proof-backed apply.
func preparedParents(tb testing.TB, from, update *Cell) []preparedParent {
	tb.Helper()

	parents := []preparedParent{{name: "resident", root: from}}

	if eager, err := FromBOC(from.ToBOC()); err == nil {
		parents = append(parents, preparedParent{name: "eager_boc", root: eager})
	}

	loader := newPreparedLoader(from)
	parents = append(parents, preparedParent{name: "lazy", root: loader.lazyRoot(from), loader: loader})

	updateFrom, err := update.PeekRef(0)
	if err != nil {
		tb.Fatalf("peek update source body: %v", err)
	}
	if narrow := preparedProofParent(updateFrom, from); narrow != nil {
		parents = append(parents, preparedParent{name: "narrow_proof", root: narrow})
	}
	if wide := widenSourceBody(updateFrom, from, 0); wide != nil {
		if root := preparedProofParent(wide, from); root != nil {
			parents = append(parents, preparedParent{name: "wide_proof", root: root})
		}
	}
	return parents
}

func preparedProofParent(body, from *Cell) *Cell {
	proof, err := CreateMerkleProof(body)
	if err != nil {
		return nil
	}
	root, err := UnwrapProofVirtualized(proof, from.Hash())
	if err != nil {
		return nil
	}
	if root.Level() != 0 || root.HashKeyAt(0) != from.HashKeyAt(0) {
		return nil
	}
	return root
}

// widenSourceBody replaces the first pruned boundary of a source body with the
// real subtree, producing a proof of the same root that proves strictly more.
// Its result must apply and must be materialized differently from the narrow
// one — that pair is the whole point of the capsule.
func widenSourceBody(body, real *Cell, merkleDepth int) *Cell {
	if body == nil || real == nil {
		return nil
	}
	if body.GetType() == PrunedCellType && body.Level() == merkleDepth+1 {
		if real.refsCount() == 0 || real.HashKeyAt(merkleDepth) != body.HashKeyAt(merkleDepth) {
			return nil
		}
		return real
	}
	if body.refsCount() == 0 || body.refsCount() != real.refsCount() {
		return nil
	}
	childDepth := merkleChildDepth(body, merkleDepth)
	refs := append([]*Cell(nil), body.rawRefs()...)
	for i := range refs {
		widened := widenSourceBody(refs[i], real.ref(i), childDepth)
		if widened == nil {
			continue
		}
		refs[i] = widened
		rebuilt, err := copyCellWithRefs(body, refs)
		if err != nil {
			return nil
		}
		return rebuilt
	}
	return nil
}

// narrowSourceBody replaces the first descendable node of a source body with a
// boundary, producing a proof the source walk cannot complete.
func narrowSourceBody(body *Cell, merkleDepth int) *Cell {
	if body == nil || body.refsCount() == 0 || body.GetType() == PrunedCellType {
		return nil
	}
	childDepth := merkleChildDepth(body, merkleDepth)
	refs := append([]*Cell(nil), body.rawRefs()...)
	for i := range refs {
		if refs[i].refsCount() == 0 || refs[i].GetType() == PrunedCellType || body.IsSpecial() {
			continue
		}
		pruned, err := CreatePrunedBranch(refs[i], childDepth+1, childDepth)
		if err != nil {
			continue
		}
		refs[i] = pruned
		rebuilt, err := copyCellWithRefs(body, refs)
		if err != nil {
			return nil
		}
		return rebuilt
	}
	for i := range refs {
		narrowed := narrowSourceBody(refs[i], childDepth)
		if narrowed == nil {
			continue
		}
		refs[i] = narrowed
		rebuilt, err := copyCellWithRefs(body, refs)
		if err != nil {
			return nil
		}
		return rebuilt
	}
	return nil
}

// ---------------------------------------------------------------- the gate

func TestPreparedMerkleUpdateDifferential(t *testing.T) {
	specs := preparedDifferentialSpecs()
	before := preparedDistinctForms.value()
	t.Cleanup(func() {
		// Parallel subtests are joined before the parent's cleanup runs.
		gained := preparedDistinctForms.value() - before
		t.Logf("corpus: %d specs, %d accepted, %d rejected, %d skipped, %d apply comparisons, %d with distinct materialization; "+
			"lazy update tree: %d product comparisons, %d cases the lazifier could not serve",
			len(specs), preparedAccepted.value(), preparedRejected.value(), preparedSkipped.value(),
			preparedApplies.value(), gained, preparedLazyApplies.value(), preparedLazyRefused.value())
		t.Logf("shared lazy parent: %d cases raced two capsules over a store-backed root",
			preparedLazyParentRaces.value())
		if preparedLazyApplies.value() < preparedApplies.value()/4 {
			t.Errorf("the lazy-update leg compared %d products against %d eager ones; it is not covering the corpus",
				preparedLazyApplies.value(), preparedApplies.value())
		}
		if preparedLazyParentRaces.value() < len(specs)/10 {
			t.Errorf("only %d of %d cases raced over a parent that actually served cells from its loader",
				preparedLazyParentRaces.value(), len(specs))
		}
		if gained < len(specs)/10 {
			t.Errorf("only %d of %d cases produced hash-equal results with different materialization; "+
				"the parent-form multiplier is not exercising virtualization", gained, len(specs))
		}
		if preparedRejected.value() == 0 || preparedAccepted.value() == 0 {
			t.Error("the corpus must contain both accepted and rejected updates")
		}
	})
	for _, spec := range specs {
		spec := spec
		t.Run(spec.name(), func(t *testing.T) {
			t.Parallel()
			comparePreparedSpec(t, spec)
		})
	}
}

// preparedDifferentialSpecs is the deterministic cross product that runs on
// every PR. It is the seed corpus of FuzzPreparedMerkleUpdate as well, so the
// fuzz target's non-fuzzing run is this same gate.
func preparedDifferentialSpecs() []preparedSpec {
	var specs []preparedSpec
	sizes := map[preparedShape][]int{
		shapeBinary:      {1, 3, 6, 9},
		shapeFan4:        {4, 32, 200},
		shapeChain:       {2, 64, 512},
		shapeWide:        {8, 40},
		shapeSingle:      {1},
		shapeDAG:         {1},
		shapeDict:        {8, 256, 1024},
		shapeNestedProof: {2, 4, 6},
		shapeDupDepths:   {1},
	}
	changeSets := []int{0, 1, -1, -2, 3, 64}
	seed := int64(20260814)
	for shape := preparedShape(0); shape < preparedShapeCount; shape++ {
		for _, size := range sizes[shape] {
			for _, changes := range changeSets {
				for builder := preparedBuilder(0); builder < preparedBuilderCount; builder++ {
					seed++
					specs = append(specs, preparedSpec{
						shape:    shape,
						size:     size,
						changes:  changes,
						builder:  builder,
						mutation: -1,
						seed:     seed,
					})
				}
			}
		}
	}
	// Exotic-cell rejects: one per validateLoadedCell branch, placed once in
	// the source body and once in the destination body, on two shapes.
	for mutation := range preparedMutations {
		for _, shape := range []preparedShape{shapeBinary, shapeNestedProof} {
			for _, mutateTo := range []bool{false, true} {
				seed++
				specs = append(specs, preparedSpec{
					shape:    shape,
					size:     4,
					changes:  1,
					builder:  builderDiffPrune,
					mutation: mutation,
					mutateTo: mutateTo,
					seed:     seed,
				})
			}
		}
	}
	return specs
}

func comparePreparedSpec(t *testing.T, spec preparedSpec) {
	t.Helper()

	// Two independent constructions of the same case. Sharing a tree between
	// the two paths would hide every materialization difference.
	sep := buildPreparedCase(t, spec)
	fused := buildPreparedCase(t, spec)
	if sep.skip != "" || fused.skip != "" {
		preparedSkipped.add(1)
		t.Skip(sep.skip + fused.skip)
	}
	if sep.update.HashKey() != fused.update.HashKey() {
		t.Fatalf("the corpus is not deterministic: two builds produced different updates")
	}

	sepErr := ValidateMerkleUpdate(sep.update)
	plain, plainErr := PrepareMerkleUpdate(fused.update)
	planned, plannedErr := PrepareMerkleUpdatePlanned(fused.update)

	assertSameError(t, "verdict/plain", sepErr, plainErr)
	assertSameError(t, "verdict/planned", sepErr, plannedErr)
	if sepErr != nil {
		preparedRejected.add(1)
		if plain != nil || planned != nil {
			t.Fatal("a rejected update produced a capsule")
		}
		// Every rejection this corpus produces must come from a walk, not from
		// a nil dereference or a panic; reaching here is that assertion.
		return
	}
	preparedAccepted.add(1)
	if !planned.Planned() {
		t.Fatal("PrepareMerkleUpdatePlanned produced no plans for an untraced update")
	}
	if planned.Cell() != fused.update || plain.Cell() != fused.update {
		t.Fatal("the capsule does not carry the update it was prepared from")
	}

	sepParents := preparedParents(t, sep.from, sep.update)
	fusedParents := preparedParents(t, fused.from, fused.update)
	if len(sepParents) != len(fusedParents) {
		t.Fatalf("parent forms diverged: %d vs %d", len(sepParents), len(fusedParents))
	}

	results := map[string]*Cell{}
	for i := range sepParents {
		sepParent, fusedParent := sepParents[i], fusedParents[i]
		if sepParent.name != fusedParent.name {
			t.Fatalf("parent form order diverged: %s vs %s", sepParent.name, fusedParent.name)
		}
		t.Run(sepParent.name, func(t *testing.T) {
			results[sepParent.name] = comparePreparedApply(t, sep, fused, plain, planned, sepParent, fusedParent)
		})
	}
	assertCrossFormMaterialization(t, results)
	comparePreparedRejectingParents(t, sep, fused, planned)
	comparePreparedConcurrency(t, spec, planned)
	comparePreparedLazyUpdateTree(t, spec, sepErr)
}

// comparePreparedLazyUpdateTree runs the differential once more with the UPDATE
// tree itself lazy on both sides.
//
// That is the one deliberate I/O divergence of the capsule: a planned capsule
// keeps the update cells it loaded, so the second and later applies perform no
// update-side loads where the classic path repeats them. The only test of that
// shape counted loader calls and never looked at the tree it produced, which
// leaves the interesting half unchecked — cells materialized out of a loader
// and then held in a plan could yield a correct-looking root standing on
// differently materialized cells, or share the parent's subtrees at different
// nodes, and load counts would say nothing about either.
//
// Both sides are lazy so that what is compared is fused against classic and not
// lazy against eager; the eager root of the same case is compared by hash on
// top, which is the assertion that laziness does not move the product at all.
// Where this loader cannot reconstruct a level-1 pruned branch — it rebuilds
// cells from their significant hashes, which is not the layout such a branch
// keeps — both paths fail on it identically, and that agreement is what gets
// asserted instead.
func comparePreparedLazyUpdateTree(t *testing.T, spec preparedSpec, eagerVerdict error) {
	t.Helper()

	sep := buildPreparedCase(t, spec)
	fused := buildPreparedCase(t, spec)
	eager := buildPreparedCase(t, spec)
	if sep.skip != "" || fused.skip != "" || eager.skip != "" {
		return
	}
	sepLoader := newPreparedLoader(sep.update)
	fusedLoader := newPreparedLoader(fused.update)
	lazySepUpdate := sepLoader.lazyRoot(sep.update)
	lazyFusedUpdate := fusedLoader.lazyRoot(fused.update)

	sepErr := ValidateMerkleUpdate(lazySepUpdate)
	planned, plannedErr := PrepareMerkleUpdatePlanned(lazyFusedUpdate)
	assertSameError(t, "verdict/lazy_update", sepErr, plannedErr)
	if sepErr != nil {
		// A lazy update the loader cannot serve is refused by both paths. It is
		// still a verdict comparison; there is simply no product to compare.
		preparedLazyRefused.add(1)
		return
	}
	if eagerVerdict != nil {
		t.Fatal("a lazy update was accepted where its eager twin was rejected")
	}
	if !planned.Planned() {
		t.Fatal("a lazy but untraced update was not planned")
	}
	if fused.update.refsCount() > 0 && fusedLoader.loadCount() == 0 {
		t.Fatal("preparing the lazy update loaded nothing, so the fixture is not lazy")
	}

	sepParents := preparedParents(t, sep.from, sep.update)
	fusedParents := preparedParents(t, fused.from, fused.update)
	eagerParents := preparedParents(t, eager.from, eager.update)
	for i := range sepParents {
		sepParent, fusedParent := sepParents[i], fusedParents[i]
		if sepParent.name != fusedParent.name {
			t.Fatalf("parent form order diverged: %s vs %s", sepParent.name, fusedParent.name)
		}
		sepRoot, sepApplyErr := ApplyMerkleUpdate(sepParent.root, lazySepUpdate)
		fusedRoot, fusedApplyErr := planned.ApplyTo(fusedParent.root)
		assertSameError(t, "apply/lazy_update/"+sepParent.name, sepApplyErr, fusedApplyErr)
		if sepApplyErr != nil {
			continue
		}
		preparedLazyApplies.add(1)
		if sepRoot.HashKeyAt(0) != fusedRoot.HashKeyAt(0) {
			t.Fatalf("%s: the fused path over a lazy update produced root %x, classic produced %x",
				sepParent.name, fusedRoot.Hash(), sepRoot.Hash())
		}
		// Bytes, not just the hash: a root standing on differently materialized
		// cells of the same hash shows here and nowhere else. Roots carrying
		// pruned cells are compared by hash alone, exactly as the determinism
		// leg does, because a proof's BOC depends on which boundaries the
		// parent form carried.
		pruned := hasPrunedCells(sepRoot, map[Hash]struct{}{})
		if !pruned && !bytes.Equal(sepRoot.ToBOC(), fusedRoot.ToBOC()) {
			t.Fatalf("%s: same root hash from a lazy update, different bytes", sepParent.name)
		}
		if shared, want := countSharedCells(fusedRoot, fusedParent.root),
			countSharedCells(sepRoot, sepParent.root); shared != want {
			t.Fatalf("%s: shared cells with the parent: fused=%d classic=%d", sepParent.name, shared, want)
		}
		assertSameReuseShape(t, fusedRoot, fusedParent.root, sepRoot, sepParent.root)

		// And against the same case with an eager update: laziness of the
		// update must not move the product at all.
		eagerRoot, eagerApplyErr := ApplyMerkleUpdate(eagerParents[i].root, eager.update)
		if eagerApplyErr != nil {
			t.Fatalf("%s: the eager twin failed to apply: %v", sepParent.name, eagerApplyErr)
		}
		if eagerRoot.HashKeyAt(0) != fusedRoot.HashKeyAt(0) {
			t.Fatalf("%s: a lazy update tree produced root %x where an eager one produced %x",
				sepParent.name, fusedRoot.Hash(), eagerRoot.Hash())
		}
		if !pruned && !bytes.Equal(eagerRoot.ToBOC(), fusedRoot.ToBOC()) {
			t.Fatalf("%s: a lazy update tree produced the eager root hash with different bytes", sepParent.name)
		}
	}
}

func comparePreparedApply(
	t *testing.T,
	sep, fused preparedCase,
	plain, planned *PreparedMerkleUpdate,
	sepParent, fusedParent preparedParent,
) *Cell {
	t.Helper()

	preparedApplies.add(1)
	sepRoot, sepErr := ApplyMerkleUpdate(sepParent.root, sep.update)
	fusedRoot, fusedErr := planned.ApplyTo(fusedParent.root)
	assertSameError(t, "apply", sepErr, fusedErr)

	// I/O parity: the loader multisets, snapshotted here — before the control
	// apply below and before anything walks the results, either of which would
	// force further loads and make the comparison meaningless.
	var sepCalls, fusedCalls map[Hash]int
	if sepParent.loader != nil && fusedParent.loader != nil {
		sepCalls, fusedCalls = sepParent.loader.snapshot(), fusedParent.loader.snapshot()
	}

	// The unplanned capsule takes the classic apply, so it is the control that
	// says the plan replay and not the capsule wrapper is what is being tested.
	plainRoot, plainErr := plain.ApplyTo(fusedParent.root)
	assertSameError(t, "apply/unplanned", sepErr, plainErr)

	if sepCalls != nil {
		assertSameCallMultiset(t, sepCalls, fusedCalls)
	}

	if sepErr != nil {
		if sepRoot != nil || fusedRoot != nil || plainRoot != nil {
			t.Fatal("a failed apply returned a root")
		}
		return nil
	}
	// The destination the update itself names, rather than a copy of it the
	// capsule cached: what has to hold is that the replay lands on the update's
	// own target.
	target := fused.update.MustPeekRef(1).HashKeyAt(0)
	if sepRoot.HashKeyAt(0) != fusedRoot.HashKeyAt(0) || fusedRoot.HashKeyAt(0) != target {
		t.Fatalf("root hash mismatch: separate=%x fused=%x target=%x",
			sepRoot.Hash(), fusedRoot.Hash(), target)
	}
	if plainRoot.HashKeyAt(0) != fusedRoot.HashKeyAt(0) {
		t.Fatal("planned and unplanned applies produced different roots")
	}
	if sep.to != nil && sepRoot.HashKeyAt(0) != sep.to.HashKeyAt(0) {
		t.Fatal("the applied root is not the destination the case was built for")
	}

	if shared, want := countSharedCells(fusedRoot, fusedParent.root), countSharedCells(sepRoot, sepParent.root); shared != want {
		t.Fatalf("shared cells with the parent: fused=%d separate=%d", shared, want)
	}
	assertSameReuseShape(t, fusedRoot, fusedParent.root, sepRoot, sepParent.root)

	fusedCounts := materializationCounts(fusedRoot)
	sepCounts := materializationCounts(sepRoot)
	if fusedCounts != sepCounts {
		t.Fatalf("materialization differs: fused=%+v separate=%+v", fusedCounts, sepCounts)
	}

	// Determinism: the same capsule applied to the same parent again. A fresh
	// parent would test the corpus rather than the capsule.
	repeat, err := planned.ApplyTo(fusedParent.root)
	if err != nil {
		t.Fatalf("second apply to the same parent failed: %v", err)
	}
	if repeat.HashKeyAt(0) != fusedRoot.HashKeyAt(0) {
		t.Fatal("two applies of one capsule to one parent produced different roots")
	}
	if !hasPrunedCells(fusedRoot, map[Hash]struct{}{}) && !bytes.Equal(repeat.ToBOC(), fusedRoot.ToBOC()) {
		t.Fatal("two applies of one capsule to one parent produced different bytes")
	}
	return fusedRoot
}

// comparePreparedRejectingParents runs the parents that must fail, and requires
// both paths to fail at the same place with the same words.
func comparePreparedRejectingParents(
	t *testing.T,
	sep, fused preparedCase,
	planned *PreparedMerkleUpdate,
) {
	t.Helper()

	sepFrom := sep.update.MustPeekRef(0)
	fusedFrom := fused.update.MustPeekRef(0)

	// (a) a parent that is not this update's source at all
	other := BeginCell().MustStoreUInt(0xDEADBEEF, 32).EndCell()
	assertSameApplyError(t, "other_parent", sep.update, planned, other, other)

	// (b) a proof too narrow for the source walk
	if sepNarrow, fusedNarrow := narrowSourceBody(sepFrom, 0), narrowSourceBody(fusedFrom, 0); sepNarrow != nil && fusedNarrow != nil {
		sepRoot := preparedProofParent(sepNarrow, sep.from)
		fusedRoot := preparedProofParent(fusedNarrow, fused.from)
		if sepRoot != nil && fusedRoot != nil {
			assertSameApplyError(t, "too_narrow_proof", sep.update, planned, sepRoot, fusedRoot)
		}
	}

	// (c) a lazy parent whose storage has lost one subtree. Both paths must
	// fail at the same node, which is what makes this an ordering assertion as
	// well as a rejection one.
	if sep.from.refsCount() > 0 {
		sepLoader := newPreparedLoader(sep.from)
		sepLoader.missing[sep.from.ref(0).HashKey()] = true
		fusedLoader := newPreparedLoader(fused.from)
		fusedLoader.missing[fused.from.ref(0).HashKey()] = true
		assertSameApplyError(t, "lazy_missing", sep.update, planned,
			sepLoader.lazyRoot(sep.from), fusedLoader.lazyRoot(fused.from))
	}
}

func assertSameApplyError(
	t *testing.T,
	name string,
	update *Cell,
	planned *PreparedMerkleUpdate,
	sepParent, fusedParent *Cell,
) {
	t.Helper()

	sepRoot, sepErr := ApplyMerkleUpdate(sepParent, update)
	fusedRoot, fusedErr := planned.ApplyTo(fusedParent)
	assertSameError(t, name, sepErr, fusedErr)
	if sepErr == nil {
		if sepRoot.HashKeyAt(0) != fusedRoot.HashKeyAt(0) {
			t.Fatalf("%s: accepted by both paths with different roots", name)
		}
		return
	}
	if fusedRoot != nil {
		t.Fatalf("%s: failed fused apply returned a root", name)
	}
}

func comparePreparedConcurrency(t *testing.T, spec preparedSpec, planned *PreparedMerkleUpdate) {
	t.Helper()

	left := buildPreparedCase(t, spec)
	right := buildPreparedCase(t, spec)
	if left.skip != "" || right.skip != "" {
		return
	}
	leftParent := left.from
	rightParent := preparedProofParent(right.update.MustPeekRef(0), right.from)
	if rightParent == nil {
		rightParent = right.from
	}

	serialLeft, err := planned.ApplyTo(leftParent)
	if err != nil {
		return
	}
	serialRight, err := planned.ApplyTo(rightParent)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	var gotLeft, gotRight *Cell
	var errLeft, errRight error
	wg.Add(2)
	go func() { defer wg.Done(); gotLeft, errLeft = planned.ApplyTo(leftParent) }()
	go func() { defer wg.Done(); gotRight, errRight = planned.ApplyTo(rightParent) }()
	wg.Wait()

	if errLeft != nil || errRight != nil {
		t.Fatalf("concurrent applies failed: %v / %v", errLeft, errRight)
	}
	if gotLeft.HashKeyAt(0) != serialLeft.HashKeyAt(0) || gotRight.HashKeyAt(0) != serialRight.HashKeyAt(0) {
		t.Fatal("concurrent applies differ from the serial ones")
	}

	comparePreparedConcurrentLazyParent(t, spec, planned)
}

// comparePreparedConcurrentLazyParent is the production concurrency shape the
// two parents above do not model: one LAZY parent shared by competing children.
//
// A ChainState root is immutable and shared between the concurrent validations
// of every child proposed on it, and on a node that restarted or fell behind
// that root is store-backed — its subtrees arrive from the node store on
// demand. So two candidates apply two transitions to the same lazy cells at the
// same time, through the same loader. The claim the runtime rests on is that
// this only reads: a lazy reference materializes into a fresh cell rather than
// into the shared tree, so neither walk can see the other's half-built node.
//
// Two distinct capsules, because that is what competing children hold; they
// carry the same transition here only because one spec describes one.
func comparePreparedConcurrentLazyParent(t *testing.T, spec preparedSpec, planned *PreparedMerkleUpdate) {
	t.Helper()

	shared := buildPreparedCase(t, spec)
	if shared.skip != "" {
		return
	}
	second, err := PrepareMerkleUpdatePlanned(shared.update)
	if err != nil {
		return
	}
	loader := newPreparedLoader(shared.from)
	parent := loader.lazyRoot(shared.from)

	serial, err := planned.ApplyTo(parent)
	if err != nil {
		return
	}
	// Not every case reads the parent at all: an update that rebuilds the whole
	// tree prunes onto nothing and its apply loads zero cells however lazy the
	// parent is. So coverage is counted over the corpus instead of asserted per
	// case.
	if loader.loadCount() > 0 {
		preparedLazyParentRaces.add(1)
	}
	pruned := hasPrunedCells(serial, map[Hash]struct{}{})

	const racers = 4
	capsules := [racers]*PreparedMerkleUpdate{planned, second, planned, second}
	var (
		wg    sync.WaitGroup
		roots [racers]*Cell
		errs  [racers]error
	)
	wg.Add(racers)
	for i := range racers {
		go func(i int) {
			defer wg.Done()
			roots[i], errs[i] = capsules[i].ApplyTo(parent)
		}(i)
	}
	wg.Wait()

	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d over a shared lazy parent failed: %v", i, errs[i])
		}
		if roots[i].HashKeyAt(0) != serial.HashKeyAt(0) {
			t.Fatalf("racer %d produced %x, the serial apply produced %x",
				i, roots[i].Hash(), serial.Hash())
		}
		if !pruned && !bytes.Equal(roots[i].ToBOC(), serial.ToBOC()) {
			t.Fatalf("racer %d produced the serial root hash with different bytes", i)
		}
	}
}

// ---------------------------------------------------------------- asserts

func assertSameError(t *testing.T, what string, want, got error) {
	t.Helper()

	if (want == nil) != (got == nil) {
		t.Fatalf("%s: accept/reject diverged: separate=%v fused=%v", what, want, got)
	}
	if want == nil {
		return
	}
	if want.Error() != got.Error() {
		t.Fatalf("%s: rejection differs:\n separate: %v\n fused:    %v", what, want, got)
	}
}

func assertSameCallMultiset(t *testing.T, want, got map[Hash]int) {
	t.Helper()

	for hash, n := range want {
		if got[hash] != n {
			t.Fatalf("loader calls for %x: separate=%d fused=%d", hash[:8], n, got[hash])
		}
	}
	for hash, n := range got {
		if want[hash] != n {
			t.Fatalf("loader calls for %x: separate=%d fused=%d", hash[:8], want[hash], n)
		}
	}
}

type preparedMaterialization struct {
	cells       int
	lazy        int
	virtualized int
	pruned      int
}

func materializationCounts(root *Cell) preparedMaterialization {
	var counts preparedMaterialization
	seen := map[*Cell]struct{}{}
	var walk func(*Cell)
	walk = func(c *Cell) {
		if c == nil {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		counts.cells++
		if c.IsLazy() {
			counts.lazy++
			return
		}
		if c.IsVirtualized() {
			counts.virtualized++
		}
		if c.GetType() == PrunedCellType {
			counts.pruned++
			return
		}
		for _, ref := range c.rawRefs() {
			walk(ref)
		}
	}
	walk(root)
	return counts
}

// assertSameReuseShape walks both results in lockstep and requires, at every
// node, that the fused result reuses a parent cell exactly where the separate
// one does, and rebuilds exactly where it does.
func assertSameReuseShape(t *testing.T, fusedRoot, fusedParent, sepRoot, sepParent *Cell) {
	t.Helper()

	fusedCells := pointerSet(fusedParent)
	sepCells := pointerSet(sepParent)

	var walk func(a, b *Cell, path string)
	seen := map[[2]*Cell]struct{}{}
	walk = func(a, b *Cell, path string) {
		if a == nil || b == nil {
			if a != b {
				t.Fatalf("%s: one result has a reference the other does not", path)
			}
			return
		}
		key := [2]*Cell{a, b}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}

		_, aReused := fusedCells[a]
		_, bReused := sepCells[b]
		if aReused != bReused {
			t.Fatalf("%s: reuse of the parent's own cell diverged (fused=%v separate=%v)", path, aReused, bReused)
		}
		if a.HashKeyAt(0) != b.HashKeyAt(0) {
			t.Fatalf("%s: node hashes diverged", path)
		}
		if a.getLevelMask() != b.getLevelMask() || a.bitsSz != b.bitsSz || a.refsCount() != b.refsCount() {
			t.Fatalf("%s: node shape diverged", path)
		}
		if a.IsLazy() || b.IsLazy() {
			return
		}
		for i := 0; i < a.refsCount(); i++ {
			walk(a.ref(i), b.ref(i), fmt.Sprintf("%s/%d", path, i))
		}
	}
	walk(fusedRoot, sepRoot, "root")
}

func pointerSet(root *Cell) map[*Cell]struct{} {
	out := map[*Cell]struct{}{}
	var walk func(*Cell)
	walk = func(c *Cell) {
		if c == nil {
			return
		}
		if _, ok := out[c]; ok {
			return
		}
		out[c] = struct{}{}
		if c.IsLazy() {
			return
		}
		for _, ref := range c.rawRefs() {
			walk(ref)
		}
	}
	walk(root)
	return out
}

// preparedDistinctForms counts the cases where a proof-virtualized parent and a
// resident parent produced hash-equal results with DIFFERENT materialization —
// the property the capsule exists to preserve. Per case it is not an assertion
// (an update that rebuilds everything legitimately materializes the same tree
// from either parent); over the corpus it is, and the parent-form multiplier is
// worthless without it.
var (
	preparedDistinctForms atomicCounter
	preparedAccepted      atomicCounter
	preparedRejected      atomicCounter
	preparedSkipped       atomicCounter
	preparedApplies       atomicCounter
	// preparedLazyApplies and preparedLazyRefused split the lazy-update leg:
	// how many product comparisons it made, and how many cases the test
	// lazifier could not reconstruct and both paths therefore refused. The
	// second is a harness limit, not a property, so it is reported rather than
	// asserted — but the first must be non-trivial or the leg proves nothing.
	preparedLazyApplies atomicCounter
	preparedLazyRefused atomicCounter
	// preparedLazyParentRaces counts the concurrency cases whose shared parent
	// actually served the walks from its loader — the store-backed ChainState
	// root two competing children apply to at once.
	preparedLazyParentRaces atomicCounter
)

func assertCrossFormMaterialization(t *testing.T, results map[string]*Cell) {
	t.Helper()

	resident, narrow := results["resident"], results["narrow_proof"]
	if resident == nil || narrow == nil {
		return
	}
	if resident.HashKeyAt(0) != narrow.HashKeyAt(0) {
		t.Fatal("the resident and proof-backed parents produced different roots")
	}
	if materializationCounts(resident) != materializationCounts(narrow) {
		preparedDistinctForms.add(1)
	}
	if wide := results["wide_proof"]; wide != nil {
		if wide.HashKeyAt(0) != narrow.HashKeyAt(0) {
			t.Fatal("the narrow and wide proof parents produced different roots")
		}
		if materializationCounts(wide) != materializationCounts(narrow) {
			preparedDistinctForms.add(1)
		}
	}
}

type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) add(n int) {
	c.mu.Lock()
	c.n += n
	c.mu.Unlock()
}

func (c *atomicCounter) value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// ---------------------------------------------------------------- the fuzz

// FuzzPreparedMerkleUpdate drives the same generator from the fuzz bytes. Every
// deterministic case is seeded, so the corpus alone is the gate and -fuzz is a
// nightly-only extension of it.
func FuzzPreparedMerkleUpdate(f *testing.F) {
	for _, spec := range preparedDifferentialSpecs() {
		f.Add(uint8(spec.shape), uint16(spec.size), int16(spec.changes), uint8(spec.builder),
			int8(spec.mutation), spec.mutateTo, spec.seed)
	}
	f.Fuzz(func(t *testing.T, shape uint8, size uint16, changes int16, builder uint8, mutation int8, mutateTo bool, seed int64) {
		spec := preparedSpec{
			shape:    preparedShape(int(shape) % int(preparedShapeCount)),
			size:     clampInt(int(size), 1, 512),
			changes:  clampInt(int(changes), -2, 512),
			builder:  preparedBuilder(int(builder) % int(preparedBuilderCount)),
			mutation: -1,
			mutateTo: mutateTo,
			seed:     seed,
		}
		if mutation >= 0 {
			spec.mutation = int(mutation) % len(preparedMutations)
		}
		comparePreparedSpec(t, spec)
	})
}

// ---------------------------------------------------------------- pinned units

// TestPreparedMerkleUpdateKeepsValidateLoadedCellCoverage is trap (a) stated as
// a test: an update whose source or destination carries a cell the eager BOC
// parser would have refused must still be rejected, with the wrapper naming
// which side it was on. ApplyMerkleUpdate alone does NOT reject these — C++ can
// rely on DataCell::create having enforced them at deserialization, Go enforces
// them eagerly only on the eager parse path — so a fused pass that dropped the
// source walk's validateSource knob would silently accept them.
func TestPreparedMerkleUpdateKeepsValidateLoadedCellCoverage(t *testing.T) {
	covered := map[string]bool{}
	for mutation := range preparedMutations {
		for _, mutateTo := range []bool{false, true} {
			spec := preparedSpec{
				shape: shapeBinary, size: 4, changes: 1, builder: builderDiffPrune,
				mutation: mutation, mutateTo: mutateTo, seed: int64(1000 + mutation),
			}
			built := buildPreparedCase(t, spec)
			if built.skip != "" {
				continue
			}
			prepared, err := PrepareMerkleUpdatePlanned(built.update)
			if err == nil {
				// Not every forged shape reaches a validate branch on every
				// tree; the ones that do not must still agree with validate.
				if validateErr := ValidateMerkleUpdate(built.update); validateErr != nil {
					t.Fatalf("%s: prepare accepted what validate rejected: %v", spec.name(), validateErr)
				}
				_ = prepared
				continue
			}
			if validateErr := ValidateMerkleUpdate(built.update); validateErr == nil ||
				validateErr.Error() != err.Error() {
				t.Fatalf("%s: prepare and validate disagree:\n prepare:  %v\n validate: %v", spec.name(), err, validateErr)
			}
			side := "source"
			if mutateTo {
				side = "destination"
			}
			if strings.Contains(err.Error(), "invalid merkle update "+side+" subtree") {
				covered[preparedMutations[mutation].name+"/"+side] = true
			}
		}
	}
	if len(covered) < 20 {
		t.Fatalf("only %d validateLoadedCell branches were reached through the update walks: %v", len(covered), covered)
	}
}

// TestPreparedMerkleUpdateTracedUpdateKeepsClassicPath pins trap (e) from the
// capsule's own side: a traced update must not have its loaded cells cached,
// because a later apply is supposed to record those reads again.
func TestPreparedMerkleUpdateTracedUpdateKeepsClassicPath(t *testing.T) {
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{9, 2, 3, 4})
	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to, true)
	if err != nil {
		t.Fatalf("build update bodies: %v", err)
	}
	update, err := CreateMerkleUpdate(updateFrom, updateTo)
	if err != nil {
		t.Fatalf("create update: %v", err)
	}

	rs := NewReadSet(update)
	prepared, err := PrepareMerkleUpdatePlanned(rs.Root())
	if err != nil {
		t.Fatalf("prepare a traced update: %v", err)
	}
	if prepared.Planned() {
		t.Fatal("a traced update was planned, so its loaded cells are cached and a later apply records nothing")
	}
	got, err := prepared.ApplyTo(from)
	if err != nil {
		t.Fatalf("apply a traced-update capsule: %v", err)
	}
	if got.HashKeyAt(0) != to.HashKeyAt(0) {
		t.Fatal("a traced-update capsule applied to a different destination")
	}
}

// TestPreparedMerkleUpdateSecondApplySkipsUpdateSideLoads pins that the capsule
// caches the loaded UPDATE-side cells, so a second ApplyTo performs zero
// update-side loads. Parent loads are unchanged, which is the half the hot
// path's residency depends on.
//
// This used to be the one deliberate I/O divergence from the classic path, and
// it is not one any more: a lazy placeholder memoizes what its loader returned,
// so the classic path's second walk over an already-resolved update reaches the
// store for nothing either. What the capsule still saves over it is the work,
// not the loads. The contrast below therefore asserts the agreement rather than
// the divergence, and it is still worth running — a classic path that started
// repeating loads again would mean the memo had stopped holding.
func TestPreparedMerkleUpdateSecondApplySkipsUpdateSideLoads(t *testing.T) {
	// Every leaf differs, so the bodies carry no pruned branch: the test
	// lazifier reconstructs cells from their significant hashes, which is not
	// the physical layout a level-1 pruned branch keeps.
	from := buildBinaryTree([]uint16{1, 2, 3, 4})
	to := buildBinaryTree([]uint16{9, 8, 7, 6})
	updateFrom, updateTo, err := buildOrdinaryMerkleUpdateBodiesFromDiffAt(from, to, false)
	if err != nil {
		t.Fatalf("build update bodies: %v", err)
	}
	update, err := CreateMerkleUpdate(updateFrom, updateTo)
	if err != nil {
		t.Fatalf("create update: %v", err)
	}

	updateLoader := newPreparedLoader(update)
	lazyUpdate := updateLoader.lazyRoot(update)

	prepared, err := PrepareMerkleUpdatePlanned(lazyUpdate)
	if err != nil {
		t.Fatalf("prepare a lazy update: %v", err)
	}
	afterPrepare := updateLoader.loadCount()
	if afterPrepare == 0 {
		t.Fatal("preparing a lazy update loaded nothing, so the fixture is not lazy")
	}

	before := updateLoader.snapshot()
	if _, err = prepared.ApplyTo(from); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if _, err = prepared.ApplyTo(from); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	assertSameCallMultiset(t, before, updateLoader.snapshot())

	// The classic path — validate, then apply — walks the update tree twice and
	// therefore asks the loader twice. This test used to assert the opposite,
	// which held only while a lazy placeholder memoised its own resolution: that
	// memo answered the second walk. The memo was reverted because it has no
	// bound — it pins every cell ever reached through a retained root, and so
	// defeats the decoded-cell cache's own limit — and the same redundancy is
	// removed one layer down instead, by an operation-scoped cell cache in the
	// node's storage. Nothing at this layer restores the property, so asserting
	// it here would pin an implementation the cell package no longer has.
	//
	// What this test does still pin is the capsule's own claim, above: the plans
	// PrepareMerkleUpdatePlanned records make the SECOND ApplyTo free of
	// update-side loads, whatever the loader underneath does.
}
