package cell

import (
	"errors"
	"testing"
)

type fixedDictCellResolver struct {
	root  *Cell
	trace *Trace
}

func newFixedDictCellResolver(root *Cell) *fixedDictCellResolver {
	r := &fixedDictCellResolver{root: root}
	r.trace = NewTraceForListener(r)
	return r
}

func (r *fixedDictCellResolver) ResolveDictNodeCell(*Cell) (*Cell, error) {
	return r.root, nil
}

func (*fixedDictCellResolver) OnLoad(*Cell) {}
func (*fixedDictCellResolver) OnCreate()    {}

func (r *fixedDictCellResolver) ChildTrace(int) *Trace {
	return r.trace
}

func (*fixedDictCellResolver) PendingError() error { return nil }

func libraryDictNode(t *testing.T) *Cell {
	t.Helper()
	cell, err := BeginCell().
		MustStoreUInt(uint64(LibraryCellType), 8).
		MustStoreSlice(make([]byte, hashSize), hashSize*8).
		EndCellSpecial(true)
	if err != nil {
		t.Fatal(err)
	}
	return cell
}

func TestDictionaryFilterRejectsMalformedForkBeforeCallback(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0, 2).EndCell()
	root := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreUInt(1, 1).
		MustStoreRef(leaf).
		MustStoreRef(leaf).
		EndCell()
	dict := root.AsDict(1)

	called := 0
	_, err := dict.Filter(func(*Slice, *Cell) (DictFilterAction, error) {
		called++
		return DictFilterKeep, nil
	})
	if !errors.Is(err, ErrInvalidDictForkNode) {
		t.Fatalf("filter error = %v, want ErrInvalidDictForkNode", err)
	}
	if called != 0 {
		t.Fatalf("filter callback called %d times before malformed fork rejection", called)
	}
	if dict.root != root {
		t.Fatal("filter changed root after malformed fork rejection")
	}
}

func TestDictionaryLoadAllAllowsAugmentedForkPayload(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0, 2).EndCell()
	root := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreUInt(1, 1).
		MustStoreRef(leaf).
		MustStoreRef(leaf).
		EndCell()

	items, err := root.AsDict(1).LoadAll()
	if err != nil {
		t.Fatalf("LoadAll augmented fork: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("LoadAll augmented fork returned %d items, want 2", len(items))
	}
}

func TestDictionaryDirectAPIsResolveLibraryRoot(t *testing.T) {
	resolved := NewDict(4)
	for _, key := range []uint64{0, 8} {
		if err := resolved.Set(
			BeginCell().MustStoreUInt(key, 4).EndCell(),
			BeginCell().MustStoreUInt(key+1, 4).EndCell(),
		); err != nil {
			t.Fatal(err)
		}
	}

	resolver := newFixedDictCellResolver(resolved.root)
	dict := libraryDictNode(t).AsDictWithTrace(4, resolver.trace)

	items, err := dict.Range(false, false)
	if err != nil || len(items) != 2 {
		t.Fatalf("Range resolved %d items, err=%v", len(items), err)
	}

	it, err := dict.Iterator(false, false)
	if err != nil {
		t.Fatal(err)
	}
	iterated := 0
	for it.Next() {
		iterated++
	}
	if err = it.Err(); err != nil || iterated != 2 {
		t.Fatalf("Iterator resolved %d items, err=%v", iterated, err)
	}

	key := BeginCell().MustStoreUInt(0, 4).EndCell()
	it, err = dict.IteratorAt(key, false, false, true)
	next := err == nil && it.Next()
	if !next {
		t.Fatalf("IteratorAt failed to resolve root: next=%v err=%v", next, err)
	}
	if err = it.Err(); err != nil {
		t.Fatalf("IteratorAt: %v", err)
	}
	if _, _, err = dict.LookupNearestKey(key, true, true, false); err != nil {
		t.Fatalf("LookupNearestKey: %v", err)
	}
	if loaded, loadErr := dict.LoadAll(); loadErr != nil || len(loaded) != 2 {
		t.Fatalf("LoadAll resolved %d items, err=%v", len(loaded), loadErr)
	}

	prefix := BeginCell().MustStoreUInt(0, 1).EndCell()
	if _, err = dict.HasCommonPrefix(prefix); err != nil {
		t.Fatalf("HasCommonPrefix: %v", err)
	}
	if _, err = dict.GetCommonPrefix(); err != nil {
		t.Fatalf("GetCommonPrefix: %v", err)
	}
	if _, err = dict.ExtractPrefixSubdictRoot(prefix, false); err != nil {
		t.Fatalf("ExtractPrefixSubdictRoot: %v", err)
	}
	cut := dict.Copy()
	if ok, cutErr := cut.CutPrefixSubdict(prefix, false); cutErr != nil || !ok {
		t.Fatalf("CutPrefixSubdict: ok=%v err=%v", ok, cutErr)
	}

	visited := 0
	ok, err := dict.CheckForEach(func(*Slice, *Cell) (bool, error) {
		visited++
		return true, nil
	}, false, true)
	if err != nil || !ok || visited != 2 {
		t.Fatalf("CheckForEach visited %d items, ok=%v err=%v", visited, ok, err)
	}

	filtered := 0
	changes, err := dict.Filter(func(*Slice, *Cell) (DictFilterAction, error) {
		filtered++
		return DictFilterKeep, nil
	})
	if err != nil || changes != 0 || filtered != 2 {
		t.Fatalf("Filter visited %d items, changes=%d err=%v", filtered, changes, err)
	}
}

func TestDictionaryForEachRefValueResolvesLibraryRoot(t *testing.T) {
	resolved := NewDict(1)
	key := BeginCell().MustStoreUInt(0, 1).EndCell()
	payload := BeginCell().MustStoreUInt(0xab, 8).EndCell()
	if err := resolved.Set(key, BeginCell().MustStoreRef(payload).EndCell()); err != nil {
		t.Fatal(err)
	}

	resolver := newFixedDictCellResolver(resolved.root)
	dict := libraryDictNode(t).AsDictWithTrace(1, resolver.trace)
	visited := 0
	count, err := dict.ForEachRefValue(func(value *Cell) error {
		visited++
		if value.HashKey() != payload.HashKey() {
			t.Fatal("ForEachRefValue returned the wrong reference")
		}
		return nil
	})
	if err != nil || count != 1 || visited != 1 {
		t.Fatalf("ForEachRefValue count=%d visited=%d err=%v", count, visited, err)
	}
}

func TestDictionaryCopyPreservesTraversalConfiguration(t *testing.T) {
	resolved := NewDict(1)
	key := BeginCell().MustStoreUInt(0, 1).EndCell()
	if err := resolved.Set(key, BeginCell().MustStoreUInt(1, 1).EndCell()); err != nil {
		t.Fatal(err)
	}

	resolver := newFixedDictCellResolver(resolved.root)
	dict := libraryDictNode(t).AsDictWithTrace(1, resolver.trace)
	copy := dict.Copy()
	if _, err := copy.LoadValue(key); err != nil {
		t.Fatalf("copied dictionary lost resolver: %v", err)
	}

	leaf := BeginCell().MustStoreUInt(0, 2).EndCell()
	augmentedFork := BeginCell().
		MustStoreUInt(0, 2).
		MustStoreUInt(1, 1).
		MustStoreRef(leaf).
		MustStoreRef(leaf).
		EndCell()
	augmented := (&AugmentedDictionary{keySz: 1, root: augmentedFork}).Copy()
	items, err := augmented.Range(false, false)
	if err != nil || len(items) != 2 {
		t.Fatalf("copied augmented dictionary lost lenient fork mode: items=%d err=%v", len(items), err)
	}
}

func TestPrefixDictionaryCopyPreservesResolver(t *testing.T) {
	resolved := NewPrefixDict(4)
	key := BeginCell().MustStoreUInt(0b10, 2).EndCell()
	if err := resolved.Set(key, BeginCell().MustStoreUInt(0xab, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	resolver := newFixedDictCellResolver(resolved.root)
	dict := (&PrefixDictionary{keySz: 4, root: libraryDictNode(t)}).
		SetTrace(resolver.trace).
		Copy()
	value, err := dict.LoadValue(key)
	if err != nil {
		t.Fatalf("copied prefix dictionary lost resolver: %v", err)
	}
	if got := value.MustLoadUInt(8); got != 0xab {
		t.Fatalf("resolved value = %x, want ab", got)
	}
}
