package cell

import (
	"fmt"
	"math/big"
	"reflect"
	"sync"
	"testing"
)

func TestDictionaryScanDiffBorrowedMatchesCompatibilityAPI(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1, 0x4000: 2, 0x8000: 3})
	next := mustPlainDiffDict(t, map[uint64]uint64{0x1000: 4, 0x4000: 2, 0x8000: 5})
	want := collectPlainDiff(t, old, next)

	var got []string
	var retained []*Cell
	err := old.ScanDiffBorrowed(next, func(view DictDiffView) error {
		keyView := view.Key
		key, err := keyView.LoadUInt(16)
		if err != nil {
			return err
		}
		owned, err := view.Key.ToCell()
		if err != nil {
			return err
		}
		retained = append(retained, owned)

		var oldValue, newValue *Slice
		if view.HasOld {
			oldValue = &view.OldValue
		}
		if view.HasNew {
			newValue = &view.NewValue
		}
		got = append(got, fmt.Sprintf("%04x:%s:%s", key, plainDiffValue(oldValue), plainDiffValue(newValue)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("borrowed diff = %v, want %v", got, want)
	}
	wantRetained := []uint64{0x0000, 0x1000, 0x8000}
	for i, key := range retained {
		if gotKey := key.MustBeginParse().MustLoadUInt(16); gotKey != wantRetained[i] {
			t.Fatalf("retained key %d = %04x, want %04x", i, gotKey, wantRetained[i])
		}
	}
}

func TestAugmentedDictionaryScanDiffBorrowedMatchesParallel(t *testing.T) {
	aug := testMetricAugmentation{}
	oldValues := map[uint64]uint64{}
	newValues := map[uint64]uint64{}
	for i := uint64(0); i < 128; i++ {
		oldValues[i] = i
		newValues[i] = i
		if i%3 == 0 {
			newValues[i] += 100
		}
	}
	old := mustDiffDict(t, aug, oldValues)
	next := mustDiffDict(t, aug, newValues)

	collect := func(parallel bool) map[uint64][2]uint64 {
		t.Helper()
		got := map[uint64][2]uint64{}
		var mu sync.Mutex
		fn := func(view AugDictDiffView) error {
			keyView := view.Key
			key, err := keyView.LoadUInt(8)
			if err != nil {
				return err
			}
			oldValue, newValue := ^uint64(0), ^uint64(0)
			if view.HasOld {
				value := view.OldValueExtra
				if err = old.aug.SkipExtra(&value); err != nil {
					return err
				}
				oldValue, err = value.LoadUInt(8)
				if err != nil {
					return err
				}
			}
			if view.HasNew {
				value := view.NewValueExtra
				if err = next.aug.SkipExtra(&value); err != nil {
					return err
				}
				newValue, err = value.LoadUInt(8)
				if err != nil {
					return err
				}
			}
			mu.Lock()
			got[key] = [2]uint64{oldValue, newValue}
			mu.Unlock()
			return nil
		}
		var err error
		if parallel {
			err = old.ScanDiffParallelBorrowed(next, true, fn, 4)
		} else {
			err = old.ScanDiffBorrowed(next, true, fn)
		}
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	sequential := collect(false)
	parallel := collect(true)
	if !reflect.DeepEqual(sequential, parallel) {
		t.Fatalf("parallel borrowed diff = %v, sequential = %v", parallel, sequential)
	}
}

var borrowedDiffBenchmarkSink int

func BenchmarkDictionaryScanDiffBorrowed(b *testing.B) {
	old := NewDict(16)
	next := NewDict(16)
	for i := 0; i < 1024; i++ {
		key := new(big.Int).SetUint64(uint64(i))
		if err := old.SetIntKey(key, BeginCell().MustStoreUInt(uint64(i), 32).EndCell()); err != nil {
			b.Fatal(err)
		}
		if err := next.SetIntKey(key, BeginCell().MustStoreUInt(uint64(i+1), 32).EndCell()); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("compatibility", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			count := 0
			if err := old.ScanDiff(next, func(*Cell, *Slice, *Slice) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
	b.Run("borrowed", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			count := 0
			if err := old.ScanDiffBorrowed(next, func(DictDiffView) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
	b.Run("raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			count := 0
			if err := old.ScanDiffRaw(next, func(DictDiffRawView) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
}

func BenchmarkAugmentedDictionaryScanDiffBorrowed(b *testing.B) {
	old, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	next, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		key := BeginCell().MustStoreUInt(uint64(i), 8).EndCell()
		if err = old.Set(key, BeginCell().MustStoreUInt(uint64(i), 16).EndCell()); err != nil {
			b.Fatal(err)
		}
		if err = next.Set(key, BeginCell().MustStoreUInt(uint64(i+1), 16).EndCell()); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("compatibility", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			count := 0
			if err := old.ScanDiff(next, false, func(*Cell, *Slice, *Slice) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
	b.Run("borrowed", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			count := 0
			if err := old.ScanDiffBorrowed(next, false, func(AugDictDiffView) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
	b.Run("borrowed_check_augmentation", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			count := 0
			if err := old.ScanDiffBorrowed(next, true, func(AugDictDiffView) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
	b.Run("raw_check_augmentation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			count := 0
			if err := old.ScanDiffRaw(next, true, func(AugDictDiffRawView) error {
				count++
				return nil
			}); err != nil {
				b.Fatal(err)
			}
			borrowedDiffBenchmarkSink = count
		}
	})
}
