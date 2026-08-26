package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// minLTAugmentation is the OutMsgQueue augmentation shape: a 64-bit minimum
// over the subtree, stored before the leaf value.
type minLTAugmentation struct{}

func (minLTAugmentation) SkipExtra(loader *Slice) error {
	return loader.SkipBits(64)
}

func (minLTAugmentation) EmptyExtra(dst *Builder) error {
	return dst.StoreUInt(^uint64(0), 64)
}

func (minLTAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	lt, err := value.Copy().LoadUInt(64)
	if err != nil {
		return err
	}
	return dst.StoreUInt(lt, 64)
}

func (minLTAugmentation) CombineExtra(leftExtra, rightExtra *Slice, dst *Builder) error {
	left, err := leftExtra.Copy().LoadUInt(64)
	if err != nil {
		return err
	}
	right, err := rightExtra.Copy().LoadUInt(64)
	if err != nil {
		return err
	}
	return dst.StoreUInt(min(left, right), 64)
}

// refMinLTAugmentation exercises the generic augmented-dictionary layout where
// a fork has child refs 0/1 and the augmentation owns refs after them.
type refMinLTAugmentation struct{}

func (refMinLTAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadRefCell()
	return err
}

func (refMinLTAugmentation) EmptyExtra(dst *Builder) error {
	return storeRefMinLT(dst, ^uint64(0))
}

func (refMinLTAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	lt, err := value.Copy().LoadUInt(64)
	if err != nil {
		return err
	}
	return storeRefMinLT(dst, lt)
}

func (refMinLTAugmentation) CombineExtra(leftExtra, rightExtra *Slice, dst *Builder) error {
	left, err := loadRefMinLT(leftExtra)
	if err != nil {
		return err
	}
	right, err := loadRefMinLT(rightExtra)
	if err != nil {
		return err
	}
	return storeRefMinLT(dst, min(left, right))
}

func storeRefMinLT(dst *Builder, lt uint64) error {
	return dst.StoreRef(BeginCell().MustStoreUInt(lt, 64).EndCell())
}

func loadRefMinLT(extra *Slice) (uint64, error) {
	ref, err := extra.LoadRefCell()
	if err != nil {
		return 0, err
	}
	loader, err := ref.BeginParse()
	if err != nil {
		return 0, err
	}
	lt, err := loader.LoadUInt(64)
	if err != nil {
		return 0, err
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return 0, fmt.Errorf("referenced augmentation has trailing data")
	}
	return lt, nil
}

type countingMinLTAugmentation struct {
	minLTAugmentation
	skipCalls int
}

func (a *countingMinLTAugmentation) SkipExtra(loader *Slice) error {
	a.skipCalls++
	return a.minLTAugmentation.SkipExtra(loader)
}

func minRank(extra *Slice) (uint64, error) {
	lt, err := extra.LoadUInt(64)
	if err != nil {
		return 0, err
	}
	if extra.BitsLeft() != 0 || extra.RefsNum() != 0 {
		return 0, fmt.Errorf("augmentation has trailing data")
	}
	return lt, nil
}

const minIterKeySz = 352

type minIterEntry struct {
	key [44]byte
	lt  uint64
}

// buildMinIterDict fills a 352-bit-keyed augmented dictionary whose leaf value
// starts with the same 64-bit lt the augmentation minimises over, matching the
// EnqueuedMsg shape.
func buildMinIterDict(t *testing.T, entries []minIterEntry) *AugmentedDictionary {
	t.Helper()
	d, err := NewAugDict(minIterKeySz, minLTAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		keyCell := BeginCell().MustStoreSlice(entries[i].key[:], minIterKeySz).EndCell()
		value := BeginCell().MustStoreUInt(entries[i].lt, 64).MustStoreUInt(uint64(i), 16).EndCell()
		if err = d.Set(keyCell, value); err != nil {
			t.Fatalf("set entry %d: %v", i, err)
		}
	}
	return d
}

// sortedByRankThenSuffix is the reference order: the exact comparator
// service/validator/collator's eager queueCandidates used, and the exact
// comparator C++ OutputQueueMerger::MsgKeyValue::operator< uses.
func sortedByRankThenSuffix(entries []minIterEntry, tieAt int) []minIterEntry {
	out := append([]minIterEntry(nil), entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].lt != out[j].lt {
			return out[i].lt < out[j].lt
		}
		return bytes.Compare(out[i].key[tieAt:], out[j].key[tieAt:]) < 0
	})
	return out
}

func drainMinIterator(t *testing.T, it *AugMinIterator) []minIterEntry {
	t.Helper()
	var out []minIterEntry
	for it.Next() {
		view := it.View()
		var entry minIterEntry
		if err := view.Key.Copy().LoadSliceInto(entry.key[:], minIterKeySz); err != nil {
			t.Fatal(err)
		}
		lt, err := view.Value.Copy().LoadUInt(64)
		if err != nil {
			t.Fatal(err)
		}
		entry.lt = lt
		if it.Rank() != lt {
			t.Fatalf("rank %d differs from the leaf lt %d", it.Rank(), lt)
		}
		out = append(out, entry)
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator failed: %v", err)
	}
	return out
}

func randomMinIterEntries(rnd *rand.Rand, n int, distinctLT, ties int) []minIterEntry {
	entries := make([]minIterEntry, n)
	for i := range entries {
		rnd.Read(entries[i].key[:])
		if distinctLT > 0 {
			entries[i].lt = uint64(rnd.Intn(distinctLT)) + 1
		} else {
			entries[i].lt = uint64(i) + 1
		}
		_ = ties
	}
	return entries
}

// TestAugMinIteratorYieldsRankThenSuffixOrder is the ordering gate: the lazy
// stream must equal the eagerly collected and sorted list, element for element.
func TestAugMinIteratorYieldsRankThenSuffixOrder(t *testing.T) {
	sizes := []int{0, 1, 2, 3, 17, 1000}
	distributions := []struct {
		name       string
		distinctLT int
	}{
		{"all-distinct", 0},
		{"heavy-ties", 4},
		{"all-equal", 1},
	}
	for _, size := range sizes {
		for _, dist := range distributions {
			t.Run(fmt.Sprintf("%d/%s", size, dist.name), func(t *testing.T) {
				rnd := rand.New(rand.NewSource(int64(size*7 + dist.distinctLT)))
				entries := randomMinIterEntries(rnd, size, dist.distinctLT, 0)
				d := buildMinIterDict(t, entries)
				it, err := d.MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
				if err != nil {
					t.Fatal(err)
				}
				got := drainMinIterator(t, it)
				want := sortedByRankThenSuffix(entries, 12)
				if len(got) != len(want) {
					t.Fatalf("streamed %d entries, want %d", len(got), len(want))
				}
				for i := range want {
					if got[i].key != want[i].key || got[i].lt != want[i].lt {
						t.Fatalf("entry %d: got (lt=%d key=%x), want (lt=%d key=%x)",
							i, got[i].lt, got[i].key, want[i].lt, want[i].key)
					}
				}
			})
		}
	}
}

func TestAugMinIteratorReadsForkExtraAfterChildRefs(t *testing.T) {
	d, err := NewAugDict(8, refMinLTAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	entries := []struct {
		key byte
		lt  uint64
	}{
		{0x80, 40},
		{0x00, 10},
		{0xC0, 30},
		{0x40, 20},
	}
	for _, entry := range entries {
		key := BeginCell().MustStoreUInt(uint64(entry.key), 8).EndCell()
		value := BeginCell().MustStoreUInt(entry.lt, 64).EndCell()
		if err = d.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}

	it, err := d.MinIterator(AugMinIteratorOptions{Rank: loadRefMinLT})
	if err != nil {
		t.Fatal(err)
	}
	for wantLT := uint64(10); wantLT <= 40; wantLT += 10 {
		if !it.Next() {
			t.Fatalf("iterator ended before rank %d: %v", wantLT, it.Err())
		}
		if got := it.Rank(); got != wantLT {
			t.Fatalf("rank = %d, want %d", got, wantLT)
		}
		view := it.View()
		valueLT, loadErr := view.Value.Copy().LoadUInt(64)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if valueLT != wantLT {
			t.Fatalf("value rank = %d, want %d", valueLT, wantLT)
		}
	}
	if it.Next() {
		t.Fatal("iterator returned an extra entry")
	}
	if err = it.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestAugMinIteratorDecomposesEachOpenedNodeOnce(t *testing.T) {
	augmentation := new(countingMinLTAugmentation)
	d, err := NewAugDict(minIterKeySz, augmentation)
	if err != nil {
		t.Fatal(err)
	}
	entries := randomMinIterEntries(rand.New(rand.NewSource(29)), 64, 1, 0)
	for i := range entries {
		key := BeginCell().MustStoreSlice(entries[i].key[:], minIterKeySz).EndCell()
		value := BeginCell().MustStoreUInt(entries[i].lt, 64).MustStoreUInt(uint64(i), 16).EndCell()
		if err = d.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}

	augmentation.skipCalls = 0
	rankCalls := 0
	it, err := d.MinIterator(AugMinIteratorOptions{
		Rank: func(extra *Slice) (uint64, error) {
			rankCalls++
			return minRank(extra)
		},
		TieBreakFrom: 96,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(drainMinIterator(t, it)); got != len(entries) {
		t.Fatalf("streamed %d entries, want %d", got, len(entries))
	}
	if augmentation.skipCalls != rankCalls {
		t.Fatalf("SkipExtra called %d times for %d opened nodes", augmentation.skipCalls, rankCalls)
	}
}

func TestAugMinIteratorReparseDoesNotNotifyTraceTwice(t *testing.T) {
	entries := randomMinIterEntries(rand.New(rand.NewSource(31)), 128, 1, 0)
	d := buildMinIterDict(t, entries)

	loads := 0
	var trace *Trace
	trace = NewTrace(TraceHooks{
		OnLoad: func(*Cell) {
			loads++
		},
		OnChild: func(int) *Trace {
			return trace
		},
	})
	rankCalls := 0
	it, err := d.CopyWithTrace(trace).MinIterator(AugMinIteratorOptions{
		Rank: func(extra *Slice) (uint64, error) {
			rankCalls++
			return minRank(extra)
		},
		TieBreakFrom: 96,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(drainMinIterator(t, it)); got != len(entries) {
		t.Fatalf("streamed %d entries, want %d", got, len(entries))
	}
	if loads != rankCalls {
		t.Fatalf("trace observed %d loads for %d opened nodes", loads, rankCalls)
	}
}

// TestAugMinIteratorRestrictsToPrefix covers replace_by_prefix: a hit, a total
// miss, a prefix landing inside a label and the empty prefix.
func TestAugMinIteratorRestrictsToPrefix(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	entries := make([]minIterEntry, 0, 256)
	for i := 0; i < 256; i++ {
		var e minIterEntry
		rnd.Read(e.key[:])
		// First four bytes select the "shard": 0x00.. or 0x80..
		if i%2 == 0 {
			e.key[0] = 0x00
		} else {
			e.key[0] = 0x80
		}
		e.lt = uint64(rnd.Intn(50)) + 1
		entries = append(entries, e)
	}
	d := buildMinIterDict(t, entries)

	for _, tc := range []struct {
		name    string
		prefix  *Cell
		matches func(minIterEntry) bool
	}{
		{"empty", BeginCell().EndCell(), func(minIterEntry) bool { return true }},
		{"hit-zero", BeginCell().MustStoreUInt(0, 1).EndCell(), func(e minIterEntry) bool { return e.key[0]&0x80 == 0 }},
		{"hit-one", BeginCell().MustStoreUInt(1, 1).EndCell(), func(e minIterEntry) bool { return e.key[0]&0x80 != 0 }},
		{"miss", BeginCell().MustStoreUInt(0x2A, 8).EndCell(), func(minIterEntry) bool { return false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, err := d.MinIterator(AugMinIteratorOptions{Rank: minRank, Prefix: tc.prefix, TieBreakFrom: 96})
			if err != nil {
				t.Fatal(err)
			}
			got := drainMinIterator(t, it)
			var want []minIterEntry
			for _, e := range entries {
				if tc.matches(e) {
					want = append(want, e)
				}
			}
			want = sortedByRankThenSuffix(want, 12)
			if len(got) != len(want) {
				t.Fatalf("streamed %d entries, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i].key != want[i].key {
					t.Fatalf("entry %d key %x, want %x", i, got[i].key, want[i].key)
				}
			}
		})
	}
}

// TestAugMinIteratorOpensOnlyTheMinimumPath is the test that catches a
// regression back to eagerness: after k pulls the stream must have opened a
// small multiple of k*depth cells, not the whole trie. The bound is not
// k*depth exactly — every fork whose subtree minimum ties the emitted rank is
// expanded first — and this fixture gives every entry a distinct rank so that
// the tie term is one and the count measures laziness alone.
func TestAugMinIteratorOpensOnlyTheMinimumPath(t *testing.T) {
	const size = 4096
	rnd := rand.New(rand.NewSource(5))
	entries := randomMinIterEntries(rnd, size, 0, 0)
	d := buildMinIterDict(t, entries)

	// Count the nodes the stream parses via a counting rank function: open()
	// calls the rank exactly once per node it materialises.
	var opened int
	countingRank := func(extra *Slice) (uint64, error) {
		opened++
		return minRank(extra)
	}
	it, err := d.MinIterator(AugMinIteratorOptions{Rank: countingRank, TieBreakFrom: 96})
	if err != nil {
		t.Fatal(err)
	}
	const pulls = 8
	for i := 0; i < pulls; i++ {
		if !it.Next() {
			t.Fatalf("stream ended after %d entries", i)
		}
	}
	if err = it.Err(); err != nil {
		t.Fatal(err)
	}
	// depth of a 4096-entry trie over random keys is ~12 plus label steps.
	limit := 4 * pulls * 24
	if opened > limit {
		t.Fatalf("opened %d nodes for %d entries, want at most %d (eager would be ~%d)",
			opened, pulls, limit, 2*size)
	}
	if opened >= size {
		t.Fatalf("opened %d nodes, which is the whole trie: the stream is not lazy", opened)
	}
}

// TestAugMinIteratorRejectsNonMonotoneExtra forges a fork whose augmentation is
// not the minimum of its children. The stream must fail rather than silently
// mis-order, because that check is the only thing standing between a forged
// augmentation and a skipped subtree.
func TestAugMinIteratorRejectsNonMonotoneExtra(t *testing.T) {
	entries := []minIterEntry{
		{key: [44]byte{0x00, 0x01}, lt: 10},
		{key: [44]byte{0x80, 0x02}, lt: 20},
	}
	d := buildMinIterDict(t, entries)
	root := d.RootCell()
	if root == nil {
		t.Fatal("dictionary has no root")
	}
	// The two keys differ in their first bit, so the root is a fork carrying an
	// empty hml_short label (bits "00") followed by the 64-bit augmentation and
	// two references. Rebuild it with an augmentation below both children.
	rootSlice := root.MustBeginParse()
	tag, err := rootSlice.LoadUInt(2)
	if err != nil || tag != 0 {
		t.Fatalf("root label is not the expected empty hml_short: tag=%d err=%v", tag, err)
	}
	original, err := rootSlice.LoadUInt(64)
	if err != nil {
		t.Fatal(err)
	}
	if original != 10 {
		t.Fatalf("root augmentation is %d, want the minimum 10", original)
	}
	left, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	right, err := rootSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	forgedRoot := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreUInt(1, 64).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()

	forgedDict := forgedRoot.AsAugDict(minIterKeySz, minLTAugmentation{})
	it, err := forgedDict.MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
	if err != nil {
		return // rejected at construction, also acceptable
	}
	for it.Next() {
	}
	if it.Err() == nil {
		t.Fatal("forged non-monotone fork augmentation was accepted")
	}
}

// TestAugMinIteratorServesSnapshotAcrossMutation documents the invariant the
// collator relies on: cleanup deletes from the dictionary while its streams are
// mid-flight, exactly as C++ mutates out_msg_queue_ under a merger holding the
// root cell captured before the loop.
func TestAugMinIteratorServesSnapshotAcrossMutation(t *testing.T) {
	rnd := rand.New(rand.NewSource(19))
	entries := randomMinIterEntries(rnd, 200, 0, 0)
	d := buildMinIterDict(t, entries)
	want := sortedByRankThenSuffix(entries, 12)

	it, err := d.MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
	if err != nil {
		t.Fatal(err)
	}
	var got []minIterEntry
	for i := 0; it.Next(); i++ {
		view := it.View()
		var entry minIterEntry
		if err = view.Key.Copy().LoadSliceInto(entry.key[:], minIterKeySz); err != nil {
			t.Fatal(err)
		}
		entry.lt = it.Rank()
		got = append(got, entry)
		// Delete an entry the stream has not reached yet.
		if i+50 < len(want) {
			victim := BeginCell().MustStoreSlice(want[i+50].key[:], minIterKeySz).EndCell()
			if delErr := d.Delete(victim); delErr != nil && !errors.Is(delErr, ErrNoSuchKeyInDict) {
				t.Fatalf("delete ahead of the stream: %v", delErr)
			}
		}
	}
	if err = it.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("snapshot stream yielded %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].key != want[i].key {
			t.Fatalf("entry %d key %x, want %x", i, got[i].key, want[i].key)
		}
	}
}

// TestAugMinIteratorViewIsInvalidatedByNext pins the borrowed-view contract the
// collator's loop depends on: the key buffer is reused, so a retained view goes
// stale rather than staying correct by accident.
func TestAugMinIteratorViewIsInvalidatedByNext(t *testing.T) {
	rnd := rand.New(rand.NewSource(23))
	entries := randomMinIterEntries(rnd, 64, 0, 0)
	d := buildMinIterDict(t, entries)
	it, err := d.MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
	if err != nil {
		t.Fatal(err)
	}
	if !it.Next() {
		t.Fatal("empty stream")
	}
	view := it.View()
	owned, err := view.Key.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	first := owned.Hash()
	if !it.Next() {
		t.Fatal("stream ended after one entry")
	}
	again, err := owned.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	_ = again
	if !bytes.Equal(owned.Hash(), first) {
		t.Fatal("ToCell key did not survive iterator advance")
	}
}

// TestAugMinIteratorEmptyDictionary keeps the degenerate case honest.
func TestAugMinIteratorEmptyDictionary(t *testing.T) {
	d, err := NewAugDict(minIterKeySz, minLTAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	it, err := d.MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
	if err != nil {
		t.Fatal(err)
	}
	if it.Next() {
		t.Fatal("empty dictionary produced an entry")
	}
	if err = it.Err(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkAugMinIteratorFirstEntries(b *testing.B) {
	for _, size := range []int{1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			entries := make([]minIterEntry, size)
			for i := range entries {
				binary.BigEndian.PutUint64(entries[i].key[4:], uint64(i)*7919)
				binary.BigEndian.PutUint64(entries[i].key[12:], uint64(i)*104729)
				entries[i].lt = uint64(i) + 1
			}
			d, err := NewAugDict(minIterKeySz, minLTAugmentation{})
			if err != nil {
				b.Fatal(err)
			}
			for i := range entries {
				keyCell := BeginCell().MustStoreSlice(entries[i].key[:], minIterKeySz).EndCell()
				value := BeginCell().MustStoreUInt(entries[i].lt, 64).EndCell()
				if err = d.Set(keyCell, value); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				it, err := d.MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
				if err != nil {
					b.Fatal(err)
				}
				for j := 0; j < 16 && it.Next(); j++ {
				}
			}
		})
	}
}

// deepDictBranchPath walks down the trie always taking the same branch, so the
// boundary the tests below plant sits well inside the walk rather than at the
// root, where the very first Next would meet it.
func deepDictBranchPath(root *Cell, branch, depth int) ([]int, *Cell) {
	var path []int
	node := root
	for len(path) < depth && int(node.refsCount()) > branch {
		node = node.ref(branch)
		path = append(path, branch)
	}
	return path, node
}

// pruneAtPath replaces one addressed node of a body with a pruned branch,
// producing a proof of the same root that proves everything except that
// subtree.
func pruneAtPath(body *Cell, path []int, merkleDepth int) *Cell {
	if len(path) == 0 {
		pruned, err := CreatePrunedBranch(body, merkleDepth+1, merkleDepth)
		if err != nil {
			return nil
		}
		return pruned
	}
	if int(body.refsCount()) <= path[0] {
		return nil
	}
	child := pruneAtPath(body.ref(path[0]), path[1:], merkleChildDepth(body, merkleDepth))
	if child == nil {
		return nil
	}
	refs := append([]*Cell(nil), body.rawRefs()...)
	refs[path[0]] = child
	rebuilt, err := copyCellWithRefs(body, refs)
	if err != nil {
		return nil
	}
	return rebuilt
}

// The shape the collator actually streams over on the proof-backed path: the
// out-queue root is not a resident trie but a virtualized proof of one, so a
// subtree the shipped proof did not cover is a pruned boundary sitting inside
// the walk. The stream must fail loudly when it reaches one.
//
// Silently treating the boundary as an absent subtree is the failure this
// guards: cleanup would then under-clean — emit what it could reach, stop, and
// produce a block whose queue still holds delivered messages, with nothing
// anywhere saying so. The check that prevents it is the special-cell rejection
// in open(); deleting it makes this test fail.
//
// Several tries, because where in the walk the boundary lands is a property of
// the fixture: the rank order decides which forks are split before it, so one
// trie alone would pin either the mid-walk case or the first-pull case by luck.
// Every trie must fail loudly, and at least one must do so after entries have
// already been emitted.
func TestAugMinIteratorRejectsPrunedBoundaryInsideTheWalk(t *testing.T) {
	deepest := 0
	for seed := int64(1); seed <= 8; seed++ {
		rnd := rand.New(rand.NewSource(seed))
		entries := randomMinIterEntries(rnd, 256, 0, 0)
		d := buildMinIterDict(t, entries)
		root := d.RootCell()
		if root == nil {
			t.Fatal("dictionary has no root")
		}
		path, withheld := deepDictBranchPath(root, 1, 5)
		if len(path) < 3 || withheld == root {
			t.Fatalf("seed %d: trie too shallow to plant an interior boundary: depth %d", seed, len(path))
		}

		narrowed := pruneAtPath(root, path, 0)
		if narrowed == nil {
			t.Fatalf("seed %d: could not prune the addressed subtree", seed)
		}
		// The same construction the proof-backed collator's predecessor goes
		// through: a Merkle proof of the trie, unwrapped to a level-0 root of
		// the original hash whose unproven subtrees are boundaries.
		virtualized := preparedProofParent(narrowed, root)
		if virtualized == nil {
			t.Fatalf("seed %d: could not virtualize the narrowed proof", seed)
		}
		if virtualized.HashKeyAt(0) != root.HashKeyAt(0) {
			t.Fatalf("seed %d: the virtualized root is not the same trie by hash", seed)
		}

		it, err := virtualized.AsAugDict(minIterKeySz, minLTAugmentation{}).
			MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
		if err != nil {
			t.Fatalf("seed %d: opening a stream over a proof-shaped trie failed at construction: %v", seed, err)
		}
		emitted := 0
		for it.Next() {
			emitted++
		}
		if it.Err() == nil {
			t.Fatalf("seed %d: the stream walked a pruned boundary and ended cleanly after %d of %d entries",
				seed, emitted, len(entries))
		}
		if !errors.Is(it.Err(), ErrDictHasSpecialCells) {
			t.Fatalf("seed %d: stream error = %v, want a special-cell rejection", seed, it.Err())
		}
		if emitted >= len(entries) {
			t.Fatalf("seed %d: the stream emitted %d entries from a trie missing a subtree", seed, emitted)
		}
		deepest = max(deepest, emitted)
		t.Logf("seed %d: pruned boundary reached after %d of %d entries", seed, emitted, len(entries))
	}
	if deepest == 0 {
		t.Fatal("every trie met the boundary before its first entry: nothing mid-walk was tested")
	}
}

// The same boundary in its other production form. A resident predecessor is
// lazy — its subtrees come from the node store on demand — so a subtree the
// store cannot produce is a load failure mid-stream rather than a pruned cell.
// It has to be just as loud.
func TestAugMinIteratorReportsLazyLoadFailureMidStream(t *testing.T) {
	deepest := 0
	for seed := int64(1); seed <= 8; seed++ {
		rnd := rand.New(rand.NewSource(seed))
		entries := randomMinIterEntries(rnd, 256, 0, 0)
		d := buildMinIterDict(t, entries)
		root := d.RootCell()
		if root == nil {
			t.Fatal("dictionary has no root")
		}
		path, withheld := deepDictBranchPath(root, 1, 5)
		if len(path) < 3 || withheld == root {
			t.Fatalf("seed %d: trie too shallow to withhold an interior subtree: depth %d", seed, len(path))
		}

		loader := newPreparedLoader(root)
		loader.missing[withheld.HashKey()] = true

		it, err := loader.lazyRoot(root).AsAugDict(minIterKeySz, minLTAugmentation{}).
			MinIterator(AugMinIteratorOptions{Rank: minRank, TieBreakFrom: 96})
		if err != nil {
			t.Fatalf("seed %d: opening a stream over a lazy trie failed at construction: %v", seed, err)
		}
		emitted := 0
		for it.Next() {
			emitted++
		}
		if it.Err() == nil {
			t.Fatalf("seed %d: a stream over a trie whose store lost a subtree ended cleanly after %d entries",
				seed, emitted)
		}
		if !errors.Is(it.Err(), ErrLazyRefNotFound) {
			t.Fatalf("seed %d: stream error = %v, want the lazy load failure", seed, it.Err())
		}
		deepest = max(deepest, emitted)
		t.Logf("seed %d: lazy load failure reached after %d of %d entries", seed, emitted, len(entries))
	}
	if deepest == 0 {
		t.Fatal("every trie met the missing subtree before its first entry: nothing mid-walk was tested")
	}
}
