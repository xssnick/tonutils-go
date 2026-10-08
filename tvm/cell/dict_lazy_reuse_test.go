package cell

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"
)

func dictReuseLazyRoot(t *testing.T, root *Cell) (*Cell, *preparedLoader) {
	t.Helper()
	loader := newPreparedLoader(root)
	return mustCreateLazyPrunedRef(t, lazyRefFromCell(root), loader.LoadCell), loader
}

func requireDictSingleLazyLoads(t *testing.T, loader *preparedLoader, count int) {
	t.Helper()
	calls := loader.snapshot()
	if len(calls) != count {
		t.Fatalf("loaded %d distinct cells, want %d", len(calls), count)
	}
	for hash, n := range calls {
		if n != 1 {
			t.Fatalf("cell %x loaded %d times, want 1", hash, n)
		}
	}
}

func requireDictLoadTraceParity(t *testing.T, eager, lazy *bulkLoadedPathRecorder) {
	t.Helper()
	if len(eager.path) != len(lazy.path) {
		t.Fatalf("logical loads: eager=%d lazy=%d", len(eager.path), len(lazy.path))
	}
	for i := range eager.path {
		if eager.path[i].HashKey() != lazy.path[i].HashKey() {
			t.Fatalf("logical load %d differs: eager=%x lazy=%x", i, eager.path[i].HashKey(), lazy.path[i].HashKey())
		}
	}
}

func dictReuseChainKeys() []uint64 {
	keys := []uint64{0}
	for bit := range 16 {
		keys = append(keys, uint64(1)<<bit)
	}
	return keys
}

func dictReusePlain(t *testing.T, keys []uint64, valueRef *Cell) *Dictionary {
	t.Helper()
	d := NewDict(16)
	for _, key := range keys {
		value := BeginCell().MustStoreUInt(key+1, 16)
		if valueRef != nil {
			value.MustStoreRef(valueRef)
		}
		if err := d.SetBuilderByUintKey(key, value); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func dictReuseAugmented(t *testing.T, keys []uint64) *AugmentedDictionary {
	t.Helper()
	d, err := NewAugDict(16, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, err := d.SetBuilderByUintKeyWithMode(key, BeginCell().MustStoreUInt(key+1, 16), DictSetModeSet); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestDictionaryLookupNearestReusesLoadedDivergentRoot(t *testing.T) {
	d := dictReusePlain(t, []uint64{100, 101, 102, 103}, nil)
	for _, next := range []bool{false, true} {
		t.Run(fmt.Sprintf("next_%t", next), func(t *testing.T) {
			query, want := uint64(104), uint64(103)
			if next {
				query, want = 99, 100
			}
			key := BeginCell().MustStoreUInt(query, 16).EndCell()
			eagerTrace, lazyTrace := newBulkLoadedPathRecorder(), newBulkLoadedPathRecorder()
			lazyRoot, loader := dictReuseLazyRoot(t, d.root)
			for _, view := range []*Dictionary{d.root.AsDictWithTrace(16, eagerTrace.trace), lazyRoot.AsDictWithTrace(16, lazyTrace.trace)} {
				got, _, err := view.LookupNearestKey(key, next, true, false)
				if err != nil {
					t.Fatal(err)
				}
				if got.MustBeginParse().MustLoadUInt(16) != want {
					t.Fatalf("nearest key = %s, want %d", got.Dump(), want)
				}
			}
			requireDictSingleLazyLoads(t, loader, 3)
			requireDictLoadTraceParity(t, eagerTrace, lazyTrace)
			if len(lazyTrace.path) != 4 {
				t.Fatalf("logical loads = %d, want 4", len(lazyTrace.path))
			}
		})
	}
}

func TestDictionaryScanDiffReusesLoadedAlignedRoots(t *testing.T) {
	for _, virtual := range []bool{false, true} {
		t.Run(fmt.Sprintf("virtual_%t", virtual), func(t *testing.T) {
			var valueRef *Cell
			if virtual {
				var err error
				valueRef, err = createPrunedBranchFromCell(BeginCell().MustStoreUInt(1, 8).EndCell(), 1)
				if err != nil {
					t.Fatal(err)
				}
			}
			many := dictReusePlain(t, dictReuseChainKeys(), valueRef)
			one := dictReusePlain(t, []uint64{0}, valueRef)
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("reverse_%t", reverse), func(t *testing.T) {
					eagerTrace, lazyTrace := newBulkLoadedPathRecorder(), newBulkLoadedPathRecorder()
					lazyMany, manyLoader := dictReuseLazyRoot(t, many.root)
					lazyOne, oneLoader := dictReuseLazyRoot(t, one.root)
					eagerMany, eagerOne := many.root, one.root
					if virtual {
						eagerMany, eagerOne = eagerMany.Virtualize(0), eagerOne.Virtualize(0)
						lazyMany, lazyOne = lazyMany.Virtualize(0), lazyOne.Virtualize(0)
					}
					var outputs [2][]uint16
					for i, roots := range [][2]*Cell{{eagerMany, eagerOne}, {lazyMany, lazyOne}} {
						trace := eagerTrace.trace
						if i == 1 {
							trace = lazyTrace.trace
						}
						if reverse {
							roots[0], roots[1] = roots[1], roots[0]
						}
						err := roots[0].AsDictWithTrace(16, trace).ScanDiffRaw(roots[1].AsDictWithTrace(16, trace), func(v DictDiffRawView) error {
							outputs[i] = append(outputs[i], binary.BigEndian.Uint16(v.Key))
							return nil
						})
						if err != nil {
							t.Fatal(err)
						}
					}
					if len(outputs[0]) != 16 || !slices.Equal(outputs[0], outputs[1]) {
						t.Fatalf("diff keys: eager=%v lazy=%v", outputs[0], outputs[1])
					}
					requireDictSingleLazyLoads(t, manyLoader, 33)
					requireDictSingleLazyLoads(t, oneLoader, 1)
					requireDictLoadTraceParity(t, eagerTrace, lazyTrace)
				})
			}
		})
	}
}

func TestDictionaryCombineReusesLoadedAlignedRoot(t *testing.T) {
	many, one := dictReusePlain(t, dictReuseChainKeys(), nil), dictReusePlain(t, []uint64{0}, nil)
	eagerTrace, lazyTrace := newBulkLoadedPathRecorder(), newBulkLoadedPathRecorder()
	lazyMany, manyLoader := dictReuseLazyRoot(t, many.root)
	lazyOne, oneLoader := dictReuseLazyRoot(t, one.root)
	eager := many.root.AsDictWithTrace(16, eagerTrace.trace)
	lazy := lazyMany.AsDictWithTrace(16, lazyTrace.trace)
	if err := eager.CombineWith(one, combineUint16Values); err != nil {
		t.Fatal(err)
	}
	if err := lazy.CombineWith(lazyOne.AsDict(16), combineUint16Values); err != nil {
		t.Fatal(err)
	}
	if eager.root.HashKey() != lazy.root.HashKey() {
		t.Fatal("lazy combine differs from eager combine")
	}
	requireDictSingleLazyLoads(t, manyLoader, 17)
	requireDictSingleLazyLoads(t, oneLoader, 1)
	requireDictLoadTraceParity(t, eagerTrace, lazyTrace)
}

func TestAugmentedDictionaryScanDiffReusesCheckedLazyChildren(t *testing.T) {
	keys := make([]uint64, 256)
	for i := range keys {
		keys[i] = uint64(i)
	}
	next := dictReuseAugmented(t, keys)
	for _, shape := range []string{"empty", "aligned", "longer_label"} {
		t.Run(shape, func(t *testing.T) {
			old := dictReuseAugmented(t, nil)
			switch shape {
			case "aligned":
				old = dictReuseAugmented(t, keys)
				for _, key := range keys {
					if _, err := old.SetBuilderByUintKeyWithMode(key, BeginCell().MustStoreUInt(key+257, 16), DictSetModeReplace); err != nil {
						t.Fatal(err)
					}
				}
			case "longer_label":
				old = dictReuseAugmented(t, []uint64{0})
			}
			eagerTrace, lazyTrace := newBulkLoadedPathRecorder(), newBulkLoadedPathRecorder()
			lazyRoot, loader := dictReuseLazyRoot(t, next.root)
			var outputs [2][]string
			for i, view := range []*AugmentedDictionary{next.Copy().SetTrace(eagerTrace.trace), lazyRoot.AsAugDict(16, next.aug).SetTrace(lazyTrace.trace)} {
				err := old.ScanDiffRaw(view, true, func(v AugDictDiffRawView) error {
					outputs[i] = append(outputs[i], fmt.Sprintf("%x:%t:%t", v.Key, v.HasOld, v.HasNew))
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(outputs[0]) == 0 || !slices.Equal(outputs[0], outputs[1]) {
				t.Fatalf("diff keys differ: eager=%d lazy=%d", len(outputs[0]), len(outputs[1]))
			}
			requireDictSingleLazyLoads(t, loader, 511)
			requireDictLoadTraceParity(t, eagerTrace, lazyTrace)
		})
	}
}

func TestAugmentedDictionaryCombineReusesLoadedAlignedRoot(t *testing.T) {
	many, one := dictReuseAugmented(t, dictReuseChainKeys()), dictReuseAugmented(t, []uint64{3})
	eagerTrace, lazyTrace := newBulkLoadedPathRecorder(), newBulkLoadedPathRecorder()
	lazyMany, manyLoader := dictReuseLazyRoot(t, many.root)
	lazyOne, oneLoader := dictReuseLazyRoot(t, one.root)
	eager := many.Copy().SetTrace(eagerTrace.trace)
	lazy := lazyMany.AsAugDict(16, many.aug).SetTrace(lazyTrace.trace)
	if ok, err := eager.CombineWith(one); err != nil || !ok {
		t.Fatalf("eager combine: ok=%t err=%v", ok, err)
	}
	if ok, err := lazy.CombineWith(lazyOne.AsAugDict(16, one.aug)); err != nil || !ok {
		t.Fatalf("lazy combine: ok=%t err=%v", ok, err)
	}
	if eager.root.HashKey() != lazy.root.HashKey() {
		t.Fatal("lazy augmented combine differs from eager combine")
	}
	// Keys 0 and 1 remain in an untouched subtree below its loaded fork.
	requireDictSingleLazyLoads(t, manyLoader, 31)
	requireDictSingleLazyLoads(t, oneLoader, 1)
	requireDictLoadTraceParity(t, eagerTrace, lazyTrace)
}
