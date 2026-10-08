package cell

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
)

type multisetReuseTrace struct {
	trace        *Trace
	events       []string
	resolved     *Cell
	resolves     int
	creates      int
	failCreateAt int
	err          error
}

func newMultisetReuseTrace() *multisetReuseTrace {
	r := &multisetReuseTrace{}
	r.trace = NewTraceForListener(r)
	return r
}

func (r *multisetReuseTrace) OnLoad(c *Cell) {
	r.events = append(r.events, fmt.Sprintf("load:%x", c.HashKey()))
}

func (r *multisetReuseTrace) OnCreate() {
	r.creates++
	r.events = append(r.events, "create")
}

func (r *multisetReuseTrace) ChildTrace(i int) *Trace {
	r.events = append(r.events, fmt.Sprintf("child:%d", i))
	return r.trace
}

func (r *multisetReuseTrace) PendingError() error {
	if r.failCreateAt > 0 && r.creates >= r.failCreateAt {
		return r.err
	}
	return nil
}

func (r *multisetReuseTrace) ResolveDictNodeCell(c *Cell) (*Cell, error) {
	r.resolves++
	r.events = append(r.events, fmt.Sprintf("resolve:%x", c.HashKey()))
	return r.resolved, nil
}

func TestMultisetReusesLoadedLabelSplit(t *testing.T) {
	for _, kind := range []string{"ordinary", "virtual", "library"} {
		t.Run(kind, func(t *testing.T) {
			var valueRef *Cell
			if kind == "virtual" {
				var err error
				valueRef, err = CreatePrunedBranch(BeginCell().MustStoreUInt(1, 8).
					MustStoreRef(BeginCell().MustStoreUInt(2, 8).EndCell()).EndCell(), 1, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			ordinary := dictReusePlain(t, []uint64{0}, valueRef).root
			root := ordinary
			if kind == "library" {
				root = libraryDictNode(t)
			}
			lazyRoot, loader := dictReuseLazyRoot(t, root)
			if kind == "virtual" {
				root, lazyRoot = root.Virtualize(0), lazyRoot.Virtualize(0)
				if ordinary.Level() != 1 || !root.IsVirtualized() || !lazyRoot.IsVirtualized() {
					t.Fatal("fixture must contain level-one roots viewed at level zero")
				}
			}
			batch := make([]DictBulkKV, 17)
			keys := make([][2]byte, len(batch))
			for i, key := range dictReuseChainKeys() {
				binary.BigEndian.PutUint16(keys[i][:], uint16(key))
				value := BeginCell().MustStoreUInt(key+2, 16)
				if valueRef != nil {
					value.MustStoreRef(valueRef)
				}
				batch[i] = DictBulkKV{Key: keys[i][:], Value: value}
			}
			eagerTrace, lazyTrace := newMultisetReuseTrace(), newMultisetReuseTrace()
			eagerTrace.resolved, lazyTrace.resolved = ordinary, ordinary
			eager := root.AsDictWithTrace(16, eagerTrace.trace)
			lazy := lazyRoot.AsDictWithTrace(16, lazyTrace.trace)
			for _, d := range []*Dictionary{eager, lazy} {
				if err := d.Multiset(append([]DictBulkKV(nil), batch...)); err != nil {
					t.Fatal(err)
				}
			}
			if eager.root.HashKey() != lazy.root.HashKey() {
				t.Fatal("lazy result differs from resident result")
			}
			requireDictSingleLazyLoads(t, loader, 1)
			if !slices.Equal(eagerTrace.events, lazyTrace.events) {
				t.Fatalf("event order differs\neager: %v\nlazy: %v", eagerTrace.events, lazyTrace.events)
			}
			if kind == "library" && lazyTrace.resolves != 17 {
				t.Fatalf("resolver calls = %d, want 17", lazyTrace.resolves)
			}
		})
	}
}

func TestMultisetReusesUnchangedCanonicalTree(t *testing.T) {
	for _, bits := range []uint{0, 1, 7, 8, 13, 256, 1023} {
		t.Run(fmt.Sprintf("key_bits_%d", bits), func(t *testing.T) {
			count := 8
			if bits < 3 {
				count = 1 << bits
			}
			items := make([]DictBulkKV, count)
			for i := range items {
				var key Builder
				if err := initUintKeyBuilder(uint64(i), bits, &key); err != nil {
					t.Fatal(err)
				}
				items[i] = DictBulkKV{
					Key:   append([]byte(nil), key.dataSlice()...),
					Value: BeginCell().MustStoreUInt(uint64(i), 13),
				}
			}
			d, err := NewDictFromItems(bits, items)
			if err != nil {
				t.Fatal(err)
			}
			original := d.root
			for _, batch := range [][]DictBulkKV{items, items[:1]} {
				if err = d.Multiset(batch); err != nil {
					t.Fatal(err)
				}
				if d.root != original {
					t.Fatal("identical update rebuilt the canonical tree")
				}
			}
		})
	}
}

func TestMultisetIdenticalUpdateCanonicalizesLabels(t *testing.T) {
	t.Run("leaf", func(t *testing.T) {
		value := BeginCell().MustStoreUInt(3, 13)
		root := rawShortDictLabel(0, 8).MustStoreBuilder(value).EndCell()
		d := root.AsDict(8)
		batch := []DictBulkKV{{Key: []byte{0}, Value: value}}
		if err := d.Multiset(batch); err != nil {
			t.Fatal(err)
		}
		want, err := NewDictFromItems(8, batch)
		if err != nil {
			t.Fatal(err)
		}
		if d.root == root || d.root.HashKey() != want.root.HashKey() {
			t.Fatal("identical update did not canonicalize the leaf label")
		}
	})
	t.Run("fork", func(t *testing.T) {
		root := nonCanonicalFixedForkRoot(t, 1, map[int64]uint64{0: 0x11, 1: 0x22})
		d := root.AsDict(1)
		if err := d.Multiset([]DictBulkKV{{Key: []byte{0}, Value: BeginCell().MustStoreUInt(0x11, 8)}}); err != nil {
			t.Fatal(err)
		}
		if d.root == root || d.root.BitsSize() != 2 {
			t.Fatal("identical update did not canonicalize the ancestor label")
		}
	})
}

func TestMultisetReuseRequiresReferenceIdentity(t *testing.T) {
	body := BeginCell().MustStoreUInt(3, 13).EndCell()
	oldRef, oldLoader := dictReuseLazyRoot(t, body)
	newRef, newLoader := dictReuseLazyRoot(t, body)
	trace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	newRef = newRef.WithTrace(trace)
	d := NewDict(8)
	if err := d.SetBuilderByUintKey(0, BeginCell().MustStoreUInt(1, 7).MustStoreRef(oldRef)); err != nil {
		t.Fatal(err)
	}
	before := d.root
	if err := d.Multiset([]DictBulkKV{{Key: []byte{0}, Value: BeginCell().MustStoreUInt(1, 7).MustStoreRef(oldRef)}}); err != nil {
		t.Fatal(err)
	}
	if d.root != before {
		t.Fatal("identical reference was not reused")
	}
	if err := d.Multiset([]DictBulkKV{{Key: []byte{0}, Value: BeginCell().MustStoreUInt(1, 7).MustStoreRef(newRef)}}); err != nil {
		t.Fatal(err)
	}
	if d.root == before || d.root.refs[0] != newRef || d.root.refs[0].Trace() != trace {
		t.Fatal("equal-hash replacement lost its reference identity or trace")
	}
	if len(oldLoader.snapshot()) != 0 || len(newLoader.snapshot()) != 0 {
		t.Fatal("reference equality check loaded a value subtree")
	}
	if _, err := d.root.refs[0].BeginParse(); err != nil {
		t.Fatal(err)
	}
	requireDictSingleLazyLoads(t, newLoader, 1)
	if len(oldLoader.snapshot()) != 0 {
		t.Fatal("replacement used the old reference's loader")
	}
}

func TestMultisetIdenticalUpdatePreservesCreateEventsAndErrors(t *testing.T) {
	base := dictReusePlain(t, []uint64{0, 1, 2, 3}, nil)
	for _, failAt := range []int{0, 1, 4, 7} {
		t.Run(fmt.Sprintf("fail_at_%d", failAt), func(t *testing.T) {
			var events [2][]string
			for run, change := range []bool{false, true} {
				trace := newMultisetReuseTrace()
				trace.failCreateAt, trace.err = failAt, errors.New("creation rejected")
				d := base.root.AsDictWithTrace(16, trace.trace)
				before := d.root
				batch := make([]DictBulkKV, 4)
				for i := range batch {
					value := uint64(i + 1)
					if change {
						value++
					}
					batch[i] = DictBulkKV{Key: []byte{0, byte(i)}, Value: BeginCell().MustStoreUInt(value, 16)}
				}
				err := d.Multiset(batch)
				if failAt == 0 {
					if err != nil || trace.creates != 7 {
						t.Fatalf("creates=%d, err=%v", trace.creates, err)
					}
				} else if !errors.Is(err, trace.err) || d.root != before || trace.creates != failAt {
					t.Fatalf("failed update: creates=%d, err=%v, root unchanged=%t", trace.creates, err, d.root == before)
				}
				events[run] = trace.events
			}
			if !slices.Equal(events[0], events[1]) {
				t.Fatalf("same-value event order differs from changed-value event order\nsame: %v\nchanged: %v", events[0], events[1])
			}
		})
	}
}
