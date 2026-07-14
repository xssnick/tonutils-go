package cell

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

type usageTreeConcurrentCase struct {
	from *Cell
	keys []uint64
}

func newUsageTreeConcurrentCase(tb testing.TB, entries int, seed int64) usageTreeConcurrentCase {
	tb.Helper()

	rnd := rand.New(rand.NewSource(seed))
	dict := NewDict(64)
	keys := make([]uint64, 0, entries)
	used := make(map[uint64]struct{}, entries)
	for len(keys) < entries {
		key := rnd.Uint64()
		if _, ok := used[key]; ok {
			continue
		}
		used[key] = struct{}{}
		keys = append(keys, key)
		if err := dict.Set(merkleUpdateBenchKey(key), merkleUpdateBenchValue(key^0x55aa55aa55aa55aa)); err != nil {
			tb.Fatalf("failed to set source dict item: %v", err)
		}
	}

	// Round-trip through BOC so reads go through freshly deserialized cells.
	from, err := FromBOC(dict.AsCell().ToBOC())
	if err != nil {
		tb.Fatalf("failed to reload source dict: %v", err)
	}
	return usageTreeConcurrentCase{from: from, keys: keys}
}

// usageTreeWalkLane reads a deterministic set of keys through the traced root,
// mixing lane-private keys with keys shared by every lane.
func usageTreeWalkLane(tb testing.TB, tracedRoot *Cell, keys []uint64, lane, lanes int) {
	dict := tracedRoot.AsDict(64)
	for i := lane; i < len(keys); i += lanes * 2 {
		if _, err := dict.LoadValue(merkleUpdateBenchKey(keys[i])); err != nil {
			tb.Errorf("lane %d failed to load key %d: %v", lane, i, err)
			return
		}
	}
	// Overlapping part: all lanes read the same tail keys.
	for i := len(keys) - len(keys)/10; i < len(keys); i++ {
		if _, err := dict.LoadValue(merkleUpdateBenchKey(keys[i])); err != nil {
			tb.Errorf("lane %d failed to load shared key %d: %v", lane, i, err)
			return
		}
	}
}

func usageTreeApplyChanges(tb testing.TB, tracedRoot *Cell, keys []uint64) *Cell {
	dict := tracedRoot.AsDict(64)
	for i := 0; i < len(keys); i += 7 {
		next := keys[i] ^ uint64(i+1)*0x9e3779b97f4a7c15
		if err := dict.Set(merkleUpdateBenchKey(keys[i]), merkleUpdateBenchValue(next)); err != nil {
			tb.Fatalf("failed to update dict item %d: %v", i, err)
		}
	}
	return dict.AsCell().WithoutTrace()
}

func usageTreeRunScenario(tb testing.TB, tc usageTreeConcurrentCase, lanes int, concurrent bool) (proofHash, updateHash Hash) {
	tb.Helper()

	builder := NewMerkleProofBuilder(tc.from)
	if concurrent {
		var wg sync.WaitGroup
		for lane := 0; lane < lanes; lane++ {
			wg.Add(1)
			go func(lane int) {
				defer wg.Done()
				usageTreeWalkLane(tb, builder.Root(), tc.keys, lane, lanes)
			}(lane)
		}
		wg.Wait()
	} else {
		for lane := 0; lane < lanes; lane++ {
			usageTreeWalkLane(tb, builder.Root(), tc.keys, lane, lanes)
		}
	}
	if tb.Failed() {
		tb.FailNow()
	}

	to := usageTreeApplyChanges(tb, builder.Root(), tc.keys)

	proof, err := builder.CreateProof()
	if err != nil {
		tb.Fatalf("failed to build usage proof: %v", err)
	}
	update, err := builder.UsageTree().CreateMerkleUpdate(tc.from, to)
	if err != nil {
		tb.Fatalf("failed to build merkle update: %v", err)
	}

	applied, _, err := ApplyMerkleUpdate(tc.from, update)
	if err != nil {
		tb.Fatalf("failed to apply merkle update: %v", err)
	}
	if applied.HashKey() != to.HashKey() {
		tb.Fatalf("applied update mismatch: got=%x want=%x", applied.Hash(), to.Hash())
	}
	return proof.HashKey(), update.HashKey()
}

func TestUsageTreeConcurrentWalkMatchesSequential(t *testing.T) {
	tc := newUsageTreeConcurrentCase(t, 4096, 2026070701)

	const lanes = 8
	seqProof, seqUpdate := usageTreeRunScenario(t, tc, lanes, false)
	for round := 0; round < 3; round++ {
		conProof, conUpdate := usageTreeRunScenario(t, tc, lanes, true)
		if conProof != seqProof {
			t.Fatalf("round %d: concurrent usage proof differs from sequential: got=%x want=%x", round, conProof, seqProof)
		}
		if conUpdate != seqUpdate {
			t.Fatalf("round %d: concurrent merkle update differs from sequential: got=%x want=%x", round, conUpdate, seqUpdate)
		}
	}
}

func TestUsageTreeConcurrentDeepWalks(t *testing.T) {
	tc := newUsageTreeConcurrentCase(t, 2048, 2026070702)

	builder := NewMerkleProofBuilder(tc.from)

	// All lanes walk the full tree: every CreateChild races on the same edges.
	var walk func(c *Cell) error
	walk = func(c *Cell) error {
		sl, err := c.BeginParse()
		if err != nil {
			return err
		}
		for sl.RefsNum() > 0 {
			ref, err := sl.LoadRefCell()
			if err != nil {
				return err
			}
			if err = walk(ref); err != nil {
				return err
			}
		}
		return nil
	}

	const lanes = 8
	var wg sync.WaitGroup
	errs := make([]error, lanes)
	for lane := 0; lane < lanes; lane++ {
		wg.Add(1)
		go func(lane int) {
			defer wg.Done()
			errs[lane] = walk(builder.Root())
		}(lane)
	}
	wg.Wait()
	for lane, err := range errs {
		if err != nil {
			t.Fatalf("lane %d walk failed: %v", lane, err)
		}
	}

	proof, err := builder.CreateProof()
	if err != nil {
		t.Fatalf("failed to build proof: %v", err)
	}

	// A full walk keeps everything, so the proof must carry the same root hash
	// and no pruned branches.
	ref := proof.MustPeekRef(0)
	if ref.HashKey(0) != tc.from.HashKey() {
		t.Fatalf("full-walk proof root hash mismatch")
	}

	seq := NewMerkleProofBuilder(tc.from)
	if err = walk(seq.Root()); err != nil {
		t.Fatalf("sequential walk failed: %v", err)
	}
	seqProof, err := seq.CreateProof()
	if err != nil {
		t.Fatalf("failed to build sequential proof: %v", err)
	}
	if proof.HashKey() != seqProof.HashKey() {
		t.Fatalf("concurrent full-walk proof differs from sequential")
	}
}

func BenchmarkUsageTreeTracedWalk(b *testing.B) {
	tc := newUsageTreeConcurrentCase(b, 4096, 2026070703)

	for _, lanes := range []int{1, 8} {
		b.Run(fmt.Sprintf("lanes_%d", lanes), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				builder := NewMerkleProofBuilder(tc.from)
				if lanes == 1 {
					for lane := 0; lane < 8; lane++ {
						usageTreeWalkLane(b, builder.Root(), tc.keys, lane, 8)
					}
					continue
				}
				var wg sync.WaitGroup
				for lane := 0; lane < lanes; lane++ {
					wg.Add(1)
					go func(lane int) {
						defer wg.Done()
						usageTreeWalkLane(b, builder.Root(), tc.keys, lane, 8)
					}(lane)
				}
				wg.Wait()
			}
		})
	}
}
