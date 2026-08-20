package cell

import (
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestAugmentedBulkForkThreshold(t *testing.T) {
	for _, tc := range []struct {
		name        string
		parallelism int
		left        int
		total       int
		want        bool
	}{
		{name: "sequential", parallelism: 1, left: 64, total: 64},
		{name: "small_batch", parallelism: 8, left: 15, total: 15},
		{name: "below_one_eighth", parallelism: 8, left: 1, total: 16},
		{name: "exactly_one_eighth", parallelism: 8, left: 2, total: 16, want: true},
		{name: "rounded_up_below", parallelism: 8, left: 2, total: 17},
		{name: "rounded_up_exact", parallelism: 8, left: 3, total: 17, want: true},
		{name: "quarter_below", parallelism: 4, left: 15, total: 64},
		{name: "quarter_exact", parallelism: 4, left: 16, total: 64, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := augmentedMutationState{parallelism: tc.parallelism}
			if got := state.shouldFork(tc.left, tc.total); got != tc.want {
				t.Fatalf("shouldFork(%d of %d, parallelism %d) = %v, want %v",
					tc.left, tc.total, tc.parallelism, got, tc.want)
			}
		})
	}
}

func TestAugmentedBulkParallelismValidation(t *testing.T) {
	for _, parallelism := range [][]int{{0}, {-1}, {2, 3}} {
		if _, err := augmentedBulkParallelism(parallelism); err == nil {
			t.Fatalf("parallelism %v accepted", parallelism)
		}
	}
	for _, parallelism := range [][]int{nil, {1}, {8}} {
		if _, err := augmentedBulkParallelism(parallelism); err != nil {
			t.Fatalf("parallelism %v: %v", parallelism, err)
		}
	}
}

func TestSetManyParallelMatchesSequentialAndBoundsWorkers(t *testing.T) {
	const (
		keyBits  = 32
		keyCount = 512
		workers  = 8
	)
	rnd := rand.New(rand.NewSource(2026081901))
	keys := randomBulkKeys(t, rnd, keyBits, keyCount, nil)
	entries := make([]AugmentedEntry, len(keys))
	for i, key := range keys {
		entries[i] = AugmentedEntry{Key: key, Value: bulkValue(key, uint64(i+1))}
	}

	sequentialProbe := &parallelProbeAugmentation{}
	sequential, err := NewAugDict(keyBits, sequentialProbe)
	if err != nil {
		t.Fatal(err)
	}
	sequentialProbe.reset()
	if err = sequential.SetMany(entries); err != nil {
		t.Fatalf("sequential SetMany: %v", err)
	}
	if got := sequentialProbe.max.Load(); got != 1 {
		t.Fatalf("default SetMany used %d concurrent callbacks, want 1", got)
	}

	parallelProbe := &parallelProbeAugmentation{}
	parallel, err := NewAugDict(keyBits, parallelProbe)
	if err != nil {
		t.Fatal(err)
	}
	parallelProbe.reset()
	if err = parallel.SetMany(entries, workers); err != nil {
		t.Fatalf("parallel SetMany: %v", err)
	}
	if got := parallelProbe.max.Load(); got <= 1 || got > workers {
		t.Fatalf("parallel SetMany used %d concurrent callbacks, want 2..%d", got, workers)
	}
	if got, want := parallel.AsCell().HashKey(), sequential.AsCell().HashKey(); got != want {
		t.Fatalf("parallel root = %x, sequential = %x", got, want)
	}
}

func TestDeleteManyParallelMatchesSequential(t *testing.T) {
	const keyBits = 32
	rnd := rand.New(rand.NewSource(2026081902))
	keys := randomBulkKeys(t, rnd, keyBits, 512, nil)
	base, err := NewAugDict(keyBits, bulkSumAugmentation{})
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]AugmentedEntry, len(keys))
	for i, key := range keys {
		entries[i] = AugmentedEntry{Key: key, Value: bulkValue(key, uint64(i+1))}
	}
	if err = base.SetMany(entries); err != nil {
		t.Fatal(err)
	}

	rnd.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	deleted := keys[:256]
	sequential := base.Copy()
	if err = sequential.DeleteMany(deleted); err != nil {
		t.Fatalf("sequential DeleteMany: %v", err)
	}
	parallel := base.Copy()
	if err = parallel.DeleteMany(deleted, 8); err != nil {
		t.Fatalf("parallel DeleteMany: %v", err)
	}
	if got, want := parallel.AsCell().HashKey(), sequential.AsCell().HashKey(); got != want {
		t.Fatalf("parallel root = %x, sequential = %x", got, want)
	}
}

type parallelProbeAugmentation struct {
	active atomic.Int32
	max    atomic.Int32
}

func (p *parallelProbeAugmentation) reset() {
	p.active.Store(0)
	p.max.Store(0)
}

func (p *parallelProbeAugmentation) enter() func() {
	active := p.active.Add(1)
	for {
		maximum := p.max.Load()
		if active <= maximum || p.max.CompareAndSwap(maximum, active) {
			break
		}
	}
	for range 64 {
		runtime.Gosched()
	}
	return func() {
		p.active.Add(-1)
	}
}

func (p *parallelProbeAugmentation) SkipExtra(loader *Slice) error {
	done := p.enter()
	defer done()
	return bulkSumAugmentation{}.SkipExtra(loader)
}

func (p *parallelProbeAugmentation) EmptyExtra(dst *Builder) error {
	done := p.enter()
	defer done()
	return bulkSumAugmentation{}.EmptyExtra(dst)
}

func (p *parallelProbeAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	done := p.enter()
	defer done()
	return bulkSumAugmentation{}.LeafExtra(value, dst)
}

func (p *parallelProbeAugmentation) CombineExtra(leftExtra, rightExtra *Slice, dst *Builder) error {
	done := p.enter()
	defer done()
	return bulkSumAugmentation{}.CombineExtra(leftExtra, rightExtra, dst)
}
