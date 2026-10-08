package cell

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

type diffReferencedValueAugmentation struct {
	leafCalls map[uint64]int
}

func (a *diffReferencedValueAugmentation) SkipExtra(value *Slice) error {
	return value.SkipBits(8)
}

func (a *diffReferencedValueAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	var payload Slice
	if err := value.PreloadRefInto(&payload); err != nil {
		return err
	}
	v, err := payload.LoadUInt(8)
	if err != nil {
		return err
	}
	if a.leafCalls != nil {
		a.leafCalls[v]++
	}
	return dst.StoreUInt(v, 8)
}

func (*diffReferencedValueAugmentation) CombineExtra(left, right *Slice, dst *Builder) error {
	l, err := left.LoadUInt(8)
	if err != nil {
		return err
	}
	r, err := right.LoadUInt(8)
	if err != nil {
		return err
	}
	return dst.StoreUInt(min(l, r), 8)
}

func (*diffReferencedValueAugmentation) EmptyExtra(dst *Builder) error {
	return dst.StoreUInt(0, 8)
}

func TestAugmentedDictionaryScanDiffMatchesChangedLeaves(t *testing.T) {
	aug := testMetricAugmentation{}
	old := mustDiffDict(t, aug, map[uint64]uint64{
		0x00: 0xa0,
		0x40: 0xa1,
		0x80: 0xa2,
		0xc0: 0xa3,
	})
	newDict := mustDiffDict(t, aug, map[uint64]uint64{
		0x10: 0xb0,
		0x40: 0xa1,
		0x80: 0xb2,
		0xe0: 0xb3,
	})

	var got []string
	err := old.ScanDiff(newDict, true, func(key *Cell, oldValueExtra, newValueExtra *Slice) error {
		keyValue := key.MustBeginParse().MustLoadUInt(8)
		oldValue, err := diffValue(old, oldValueExtra)
		if err != nil {
			return err
		}
		newValue, err := diffValue(newDict, newValueExtra)
		if err != nil {
			return err
		}
		got = append(got, fmt.Sprintf("%02x:%s:%s", keyValue, oldValue, newValue))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"00:a0:-",
		"10:-:b0",
		"80:a2:b2",
		"c0:a3:-",
		"e0:-:b3",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("changed leaves = %v, want %v", got, want)
	}
}

func TestAugmentedDictionaryScanDiffTracesForkAugmentationClosure(t *testing.T) {
	aug := testMetricAugmentation{}
	old := mustDiffDict(t, aug, map[uint64]uint64{
		0x00: 0xa0,
		0x80: 0xb0,
		0xc0: 0xc0,
	})
	newDict := old.Copy()
	if err := newDict.Set(mustTestAugKey(t, 0x00), mustTestAugValue(t, 0xa1, 8)); err != nil {
		t.Fatal(err)
	}

	// A lookup of the only changed key opens its path, but not the unchanged
	// sibling roots whose extras scan_diff mode 2 recomputes at changed forks.
	pointOld := proveDiffDictAfterLookup(t, old, 0x00, aug)
	pointNew := proveDiffDictAfterLookup(t, newDict, 0x00, aug)
	err := pointOld.ScanDiff(pointNew, true, func(*Cell, *Slice, *Slice) error { return nil })
	if err == nil {
		t.Fatal("point-lookup proof unexpectedly contains the structural diff closure")
	}

	oldRead := NewReadSet(old.root)
	newRead := NewReadSet(newDict.root)
	tracedOld := old.Copy().SetTrace(oldRead.Trace())
	tracedNew := newDict.Copy().SetTrace(newRead.Trace())
	callbacks := 0
	if err = tracedOld.ScanDiff(tracedNew, true, func(*Cell, *Slice, *Slice) error {
		callbacks++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if callbacks != 1 {
		t.Fatalf("callbacks = %d, want 1", callbacks)
	}

	provenOld := proveDiffDict(t, old, oldRead, aug)
	provenNew := proveDiffDict(t, newDict, newRead, aug)
	callbacks = 0
	if err = provenOld.ScanDiff(provenNew, true, func(*Cell, *Slice, *Slice) error {
		callbacks++
		return nil
	}); err != nil {
		t.Fatalf("ScanDiff on structurally traced proof: %v", err)
	}
	if callbacks != 1 {
		t.Fatalf("proven callbacks = %d, want 1", callbacks)
	}
}

func TestAugmentedDictionaryScanDiffTracesRealignedEqualLeafThroughPredecessorPath(t *testing.T) {
	aug := &diffReferencedValueAugmentation{}
	old, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	for key, payload := range map[uint64]uint64{0x00: 0xa0, 0x40: 0xb0} {
		value := BeginCell().MustStoreRef(BeginCell().MustStoreUInt(payload, 8).EndCell()).EndCell()
		if err = old.Set(mustTestAugKey(t, key), value); err != nil {
			t.Fatal(err)
		}
	}

	read := NewReadSet(old.root)
	tracedOld := old.Copy().SetTrace(read.Trace())
	value, err := tracedOld.LoadValue(mustTestAugKey(t, 0x00))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := value.PeekRefCell()
	if err != nil {
		t.Fatal(err)
	}
	payloadHash := payload.HashKey()
	// The lookup reached the payload only as a reference of the leaf it read,
	// which is the state the node lookup reported: known to the recorder, not
	// read itself.
	if _, known := read.Prunable(payloadHash); !known {
		t.Fatal("predecessor payload was not reached through the recorded lookup")
	}
	if _, loaded := read.Contains(payloadHash); loaded {
		t.Fatal("predecessor payload was loaded before ScanDiff")
	}

	newDict := tracedOld.Copy()
	if err = newDict.Delete(mustTestAugKey(t, 0x40)); err != nil {
		t.Fatal(err)
	}
	if _, loaded := read.Contains(payloadHash); loaded {
		t.Fatal("dictionary mutation loaded the unchanged predecessor payload")
	}
	aug.leafCalls = make(map[uint64]int)
	callbacks := 0
	if err = tracedOld.ScanDiff(newDict, true, func(*Cell, *Slice, *Slice) error {
		callbacks++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if callbacks != 1 {
		t.Fatalf("callbacks = %d, want only the removed leaf", callbacks)
	}
	if aug.leafCalls[0xa0] == 0 {
		t.Fatal("mode-2 diff did not validate the structurally realigned equal leaf")
	}
	if _, loaded := read.Contains(payloadHash); !loaded {
		t.Fatal("realigned equal leaf payload was not recorded through its predecessor path")
	}
}

func TestAugmentedDictionaryScanDiffSkipsEqualRootByHash(t *testing.T) {
	aug := testMetricAugmentation{}
	dict := mustDiffDict(t, aug, map[uint64]uint64{
		0x00: 0xa0,
		0x40: 0xb0,
		0x80: 0xc0,
		0xc0: 0xd0,
	})

	loads := 0
	trace := NewTrace(TraceHooks{OnLoad: func(*Cell) { loads++ }})
	old := dict.Copy().SetTrace(trace)
	newDict := dict.Copy().SetTrace(trace)
	if err := old.ScanDiff(newDict, true, func(*Cell, *Slice, *Slice) error {
		t.Fatal("callback called for equal dictionaries")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if loads != 0 {
		t.Fatalf("equal-root cell loads = %d, want 0", loads)
	}
}

func TestAugmentedDictionaryScanDiffChecksOnlyRequestedSideAugmentation(t *testing.T) {
	aug := testMetricAugmentation{}
	empty, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	valid := mustDiffDict(t, aug, map[uint64]uint64{0x12: 0xa0})

	root := valid.root
	labelBits := testAugLabelBits(t, root, 8)
	src := root.MustBeginParse()
	bad := BeginCell()
	if err = bad.storeSliceFromSlice(src, labelBits); err != nil {
		t.Fatal(err)
	}
	extra, err := src.LoadUInt(16)
	if err != nil {
		t.Fatal(err)
	}
	bad.MustStoreUInt(extra^1, 16)
	remaining := src.BitsLeft()
	if err = bad.StoreSlice(src.MustLoadSlice(remaining), remaining); err != nil {
		t.Fatal(err)
	}
	corrupt := bad.EndCell().AsAugDict(8, aug)

	if err = empty.ScanDiff(corrupt, false, func(*Cell, *Slice, *Slice) error { return nil }); err != nil {
		t.Fatalf("ScanDiff without augmentation check: %v", err)
	}
	err = empty.ScanDiff(corrupt, true, func(*Cell, *Slice, *Slice) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "leaf extra mismatch") {
		t.Fatalf("ScanDiff checked augmentation error = %v", err)
	}
}

func TestAugmentedDictionaryScanDiffRandomizedShapes(t *testing.T) {
	aug := testMetricAugmentation{}
	rnd := rand.New(rand.NewSource(0x5ca1d1ff))
	for iteration := 0; iteration < 300; iteration++ {
		oldValues := make(map[uint64]uint64)
		newValues := make(map[uint64]uint64)
		for key := uint64(0); key < 256; key++ {
			if rnd.Intn(16) == 0 {
				oldValues[key] = uint64(rnd.Intn(256))
			}
			if rnd.Intn(16) == 0 {
				newValues[key] = uint64(rnd.Intn(256))
			}
		}

		old := mustDiffDict(t, aug, oldValues)
		newDict := mustDiffDict(t, aug, newValues)
		var got []uint64
		err := old.ScanDiff(newDict, true, func(key *Cell, oldValueExtra, newValueExtra *Slice) error {
			keyValue := key.MustBeginParse().MustLoadUInt(8)
			oldValue, oldPresent := oldValues[keyValue]
			newValue, newPresent := newValues[keyValue]
			if oldPresent == (oldValueExtra == nil) || newPresent == (newValueExtra == nil) {
				return fmt.Errorf("key %02x presence mismatch", keyValue)
			}
			if oldPresent {
				value, _, err := old.decomposeValueExtra(oldValueExtra)
				if err != nil {
					return err
				}
				if value.MustLoadUInt(8) != oldValue {
					return fmt.Errorf("key %02x old value mismatch", keyValue)
				}
			}
			if newPresent {
				value, _, err := newDict.decomposeValueExtra(newValueExtra)
				if err != nil {
					return err
				}
				if value.MustLoadUInt(8) != newValue {
					return fmt.Errorf("key %02x new value mismatch", keyValue)
				}
			}
			got = append(got, keyValue)
			return nil
		})
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}

		var want []uint64
		for key := uint64(0); key < 256; key++ {
			oldValue, oldPresent := oldValues[key]
			newValue, newPresent := newValues[key]
			if oldPresent != newPresent || oldPresent && oldValue != newValue {
				want = append(want, key)
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("iteration %d changed keys = %v, want %v", iteration, got, want)
		}
	}
}

func mustDiffDict(t *testing.T, aug Augmentation, values map[uint64]uint64) *AugmentedDictionary {
	t.Helper()
	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		if err = dict.Set(mustTestAugKey(t, key), mustTestAugValue(t, value, 8)); err != nil {
			t.Fatal(err)
		}
	}
	return dict
}

func diffValue(dict *AugmentedDictionary, valueExtra *Slice) (string, error) {
	if valueExtra == nil {
		return "-", nil
	}
	value, _, err := dict.decomposeValueExtra(valueExtra)
	if err != nil {
		return "", err
	}
	v, err := value.LoadUInt(8)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02x", v), nil
}

func proveDiffDictAfterLookup(t *testing.T, dict *AugmentedDictionary, key uint64, aug Augmentation) *AugmentedDictionary {
	t.Helper()
	read := NewReadSet(dict.root)
	traced := dict.Copy().SetTrace(read.Trace())
	if _, err := traced.LoadValueWithExtra(mustTestAugKey(t, key)); err != nil {
		t.Fatal(err)
	}
	return proveDiffDict(t, dict, read, aug)
}

func proveDiffDict(t *testing.T, dict *AugmentedDictionary, read *ReadSet, aug Augmentation) *AugmentedDictionary {
	t.Helper()
	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := UnwrapProofVirtualized(proof, dict.root.Hash())
	if err != nil {
		t.Fatal(err)
	}
	return proven.AsAugDict(dict.keySz, aug)
}
