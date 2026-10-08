package cell

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAugmentedDictionaryScanDiffRawParity(t *testing.T) {
	old, next := benchmarkQueueDiffDictionaries(t, 1024, 197)

	type result struct {
		key    string
		hasOld bool
		hasNew bool
	}
	collectBorrowed := func() []result {
		var got []result
		err := old.ScanDiffBorrowed(next, true, func(view AugDictDiffView) error {
			key := make([]byte, 44)
			loader := view.Key
			if err := loader.LoadSliceInto(key, 352); err != nil {
				return err
			}
			got = append(got, result{key: string(key), hasOld: view.HasOld, hasNew: view.HasNew})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	collectRaw := func(workers int) []result {
		var mu sync.Mutex
		var got []result
		err := old.ScanDiffParallelRaw(next, true, func(view AugDictDiffRawView) error {
			key := append([]byte(nil), view.Key...)
			mu.Lock()
			got = append(got, result{key: string(key), hasOld: view.HasOld, hasNew: view.HasNew})
			mu.Unlock()
			return nil
		}, workers)
		if err != nil {
			t.Fatal(err)
		}
		if workers > 1 {
			sort.Slice(got, func(i, j int) bool { return got[i].key < got[j].key })
		}
		return got
	}

	want := collectBorrowed()
	for _, workers := range []int{1, 2, 4, 8, 10} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			if got := collectRaw(workers); !reflect.DeepEqual(got, want) {
				t.Fatalf("raw diff mismatch: got %d leaves, want %d", len(got), len(want))
			}
		})
	}
}

func TestDictionaryScanDiffRawParity(t *testing.T) {
	old := mustPlainDiffDict(t, map[uint64]uint64{0x0000: 1, 0x2000: 2, 0x4000: 3, 0x8000: 4})
	next := mustPlainDiffDict(t, map[uint64]uint64{0x1000: 5, 0x2000: 2, 0x4000: 6, 0xc000: 7})

	var want, got []string
	if err := old.ScanDiffBorrowed(next, func(view DictDiffView) error {
		key := view.Key
		value, err := key.LoadUInt(16)
		if err != nil {
			return err
		}
		want = append(want, fmt.Sprintf("%04x:%t:%t", value, view.HasOld, view.HasNew))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := old.ScanDiffRaw(next, func(view DictDiffRawView) error {
		if view.KeyBits != 16 || len(view.Key) != 2 {
			return fmt.Errorf("raw key shape = %d bits in %d bytes", view.KeyBits, len(view.Key))
		}
		got = append(got, fmt.Sprintf("%04x:%t:%t", binary.BigEndian.Uint16(view.Key), view.HasOld, view.HasNew))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("raw diff = %v, borrowed = %v", got, want)
	}
}

func TestDiffRawNonByteAlignedKeysAndCallbackError(t *testing.T) {
	const keyBits = 13
	key := func(value uint64) *Cell {
		return BeginCell().MustStoreUInt(value, keyBits).EndCell()
	}
	old := NewDict(keyBits)
	next := NewDict(keyBits)
	for i := uint64(0); i < 64; i++ {
		if err := old.Set(key(i), BeginCell().MustStoreUInt(i, 16).EndCell()); err != nil {
			t.Fatal(err)
		}
		if err := next.Set(key(i), BeginCell().MustStoreUInt(i+1, 16).EndCell()); err != nil {
			t.Fatal(err)
		}
	}

	wantErr := errors.New("raw callback")
	callbacks := 0
	err := old.ScanDiffRaw(next, func(view DictDiffRawView) error {
		callbacks++
		if view.KeyBits != keyBits || len(view.Key) != 2 {
			return fmt.Errorf("raw key shape = %d bits in %d bytes", view.KeyBits, len(view.Key))
		}
		value := binary.BigEndian.Uint16(view.Key) >> (16 - keyBits)
		if value == 7 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("callback error = %v, want sentinel", err)
	}
	if callbacks != 8 {
		t.Fatalf("callbacks before error = %d, want 8", callbacks)
	}
}

func TestDiffRawValidationErrors(t *testing.T) {
	plain8 := NewDict(8)
	plain16 := NewDict(16)
	if err := plain8.ScanDiffRaw(plain8, nil); err == nil {
		t.Fatal("plain raw diff accepted a nil callback")
	}
	if err := plain8.ScanDiffRaw(plain16, func(DictDiffRawView) error { return nil }); err == nil {
		t.Fatal("plain raw diff accepted a key-size mismatch")
	}

	aug8, err := NewAugDict(8, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	aug16, err := NewAugDict(16, testMetricAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	if err = aug8.ScanDiffRaw(aug8, true, nil); err == nil {
		t.Fatal("augmented raw diff accepted a nil callback")
	}
	if err = aug8.ScanDiffRaw(aug16, true, func(AugDictDiffRawView) error { return nil }); err == nil {
		t.Fatal("augmented raw diff accepted a key-size mismatch")
	}
}

func TestAugmentedDictionaryScanDiffParallelRawKeepsErrorPriority(t *testing.T) {
	old, next := benchmarkQueueDiffDictionaries(t, 1024, 197)
	leftmost := errors.New("leftmost callback")
	other := errors.New("later callback")

	for _, workers := range []int{1, 2, 4, 8, 10} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			err := old.ScanDiffParallelRaw(next, true, func(view AugDictDiffRawView) error {
				if binary.BigEndian.Uint64(view.Key[36:]) == 0 {
					return leftmost
				}
				return other
			}, workers)
			if !errors.Is(err, leftmost) {
				t.Fatalf("error priority = %v, want leftmost", err)
			}
		})
	}
}

func TestAugmentedDictionaryScanDiffParallelRawReadSetParity(t *testing.T) {
	old, next := benchmarkQueueDiffDictionaries(t, 2048, 257)
	record := func(workers int) (map[Hash]struct{}, []byte) {
		read := NewReadSet(old.RootCell())
		traced := old.Copy().SetTrace(read.Trace())
		if err := traced.ScanDiffParallelRaw(next, true, func(AugDictDiffRawView) error { return nil }, workers); err != nil {
			t.Fatal(err)
		}
		got := make(map[Hash]struct{}, read.Size())
		for _, loaded := range read.Cells() {
			got[loaded.HashKey()] = struct{}{}
		}
		proof, err := read.Proof()
		if err != nil {
			t.Fatal(err)
		}
		return got, proof.ToBOCWithOptions(BOCSerializeOptions{})
	}

	want, wantProof := record(1)
	before := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(before)
	for _, workers := range []int{1, 2, 4, 8, 10} {
		t.Run(fmt.Sprintf("gomaxprocs_%d", workers), func(t *testing.T) {
			runtime.GOMAXPROCS(workers)
			got, gotProof := record(workers)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("parallel read set has %d cells, sequential has %d", len(got), len(want))
			}
			if !bytes.Equal(gotProof, wantProof) {
				t.Fatal("parallel proof bytes differ from sequential proof")
			}
		})
	}
}

func TestAugmentedDictionaryScanDiffParallelUsesAdaptiveWorkers(t *testing.T) {
	old, next := benchmarkQueueDiffDictionaries(t, 2048, 257)
	var active, maximum atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once

	err := old.ScanDiffParallelRaw(next, false, func(AugDictDiffRawView) error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		if current >= 4 {
			releaseOnce.Do(func() { close(release) })
		}

		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-release:
		case <-timer.C:
			releaseOnce.Do(func() { close(release) })
		}
		return nil
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	if got := maximum.Load(); got < 4 {
		t.Fatalf("maximum concurrent callbacks = %d, want at least 4", got)
	}
}

func TestAugmentedDictionaryDeleteManySidecarReadSetParity(t *testing.T) {
	base, _ := benchmarkQueueDiffDictionaries(t, 2048, 1)
	deleted := make([][]byte, 257)
	for i := range deleted {
		deleted[i] = benchmarkQueueKey(uint64(i * 2048 / len(deleted)))
	}

	record := func(workers int) (Hash, map[Hash]struct{}, []byte) {
		read := NewReadSet(base.RootCell())
		dict := base.Copy().SetTrace(read.Trace())
		if err := dict.DeleteManyByBytes(deleted, workers); err != nil {
			t.Fatal(err)
		}
		loaded := make(map[Hash]struct{}, read.Size())
		for _, item := range read.Cells() {
			loaded[item.HashKey()] = struct{}{}
		}
		proof, err := read.Proof()
		if err != nil {
			t.Fatal(err)
		}
		return dict.RootCell().HashKey(), loaded, proof.ToBOCWithOptions(BOCSerializeOptions{})
	}

	wantRoot, wantReads, wantProof := record(1)
	for _, workers := range []int{2, 4, 8, 10} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			gotRoot, gotReads, gotProof := record(workers)
			if gotRoot != wantRoot {
				t.Fatalf("parallel root = %x, sequential = %x", gotRoot[:4], wantRoot[:4])
			}
			if !reflect.DeepEqual(gotReads, wantReads) {
				t.Fatalf("parallel read set has %d cells, sequential has %d", len(gotReads), len(wantReads))
			}
			if !bytes.Equal(gotProof, wantProof) {
				t.Fatal("parallel proof bytes differ from sequential proof")
			}
		})
	}
}

func BenchmarkAugmentedDictionaryQueueDiffParallel(b *testing.B) {
	old, next := benchmarkQueueDiffDictionaries(b, 8192, 753)
	trace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	old = old.Copy().SetTrace(trace)
	next = next.Copy().SetTrace(trace)
	noop := func(AugDictDiffView) error { return nil }

	for _, workers := range []int{1, 2, 4, 8, 10} {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := old.ScanDiffParallelBorrowed(next, true, noop, workers); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	for _, workers := range []int{1, 2, 4, 8, 10} {
		b.Run(fmt.Sprintf("raw_workers_%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := old.ScanDiffParallelRaw(next, true, func(AugDictDiffRawView) error { return nil }, workers); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAugmentedDictionaryDeleteManyTraced(b *testing.B) {
	base, _ := benchmarkQueueDiffDictionaries(b, 8192, 1)
	base = base.Copy().SetTrace(NewTrace(TraceHooks{OnLoad: func(*Cell) {}}))
	deleted := make([][]byte, 753)
	for i := range deleted {
		deleted[i] = benchmarkQueueKey(uint64(i * 8192 / len(deleted)))
	}

	for _, workers := range []int{1, 10} {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				dict := base.Copy()
				if err := dict.DeleteManyByBytes(deleted, workers); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type queueDiffBenchmarkAugmentation struct{}

func (queueDiffBenchmarkAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadUInt(32)
	return err
}

func (queueDiffBenchmarkAugmentation) EmptyExtra(dst *Builder) error {
	return dst.StoreUInt(0, 32)
}

func (queueDiffBenchmarkAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	return dst.StoreUInt(uint64(value.BitsLeft())+uint64(value.RefsNum())*1024, 32)
}

func (queueDiffBenchmarkAugmentation) CombineExtra(left, right *Slice, dst *Builder) error {
	l, err := left.LoadUInt(32)
	if err != nil {
		return err
	}
	r, err := right.LoadUInt(32)
	if err != nil {
		return err
	}
	return dst.StoreUInt(l+r, 32)
}

func benchmarkQueueDiffDictionaries(tb testing.TB, entries, changed int) (*AugmentedDictionary, *AugmentedDictionary) {
	tb.Helper()
	old, err := NewAugDict(352, queueDiffBenchmarkAugmentation{})
	if err != nil {
		tb.Fatal(err)
	}

	base := make([]AugmentedBytesEntry, entries)
	for i := range base {
		base[i] = AugmentedBytesEntry{
			Key:   benchmarkQueueKey(uint64(i)),
			Value: BeginCell().MustStoreUInt(uint64(i), 32).EndCell(),
		}
	}
	if err = old.SetManyByBytes(base, 10); err != nil {
		tb.Fatal(err)
	}

	next := old.Copy()
	updates := make([]AugmentedBytesEntry, changed)
	for i := range updates {
		index := i * entries / changed
		updates[i] = AugmentedBytesEntry{
			Key:   benchmarkQueueKey(uint64(index)),
			Value: BeginCell().MustStoreUInt(uint64(index+entries), 32).EndCell(),
		}
	}
	if err = next.SetManyByBytes(updates, 10); err != nil {
		tb.Fatal(err)
	}
	return old, next
}

func benchmarkQueueKey(sequence uint64) []byte {
	key := make([]byte, 44)
	binary.BigEndian.PutUint32(key, uint32(0))
	binary.BigEndian.PutUint64(key[4:], 0x8000000000000000)
	binary.BigEndian.PutUint64(key[36:], sequence)
	return key
}
