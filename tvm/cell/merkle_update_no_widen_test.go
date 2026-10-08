package cell

import (
	"math/rand"
	"testing"
)

// Building a merkle update must not widen the record. createMerkleUpdateRaw
// used to enforce that by muting the recorder (IgnoreReads) for the duration of
// the call, but the mute belongs to the set, not to the building goroutine: a
// recorder legitimately running concurrently — the collator's validation-closure
// replay under a deferred-recording window — had its reads swallowed by it, and
// the collated proof silently lost the cells those reads select. The mute is
// gone, so the guarantee is structural now: the source-graph walk and the
// destination prune walk descend through raw cell fields and the recorded
// table, never through a traced parse. This test is the pin — it builds updates
// over a recorded dictionary rewrite, plain and applied, serial and parallel,
// and fails if the build records anything or fires the first-read callback.
func TestCreateMerkleUpdateDoesNotWidenTheRecord(t *testing.T) {
	for _, tc := range []struct {
		name        string
		applied     bool
		parallelism int
	}{
		{name: "plain", applied: false, parallelism: 1},
		{name: "applied-serial", applied: true, parallelism: 1},
		{name: "applied-parallel", applied: true, parallelism: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, keys := merkleUpdateSourceDict(t, 4096, 2026082701)
			rs := NewReadSet(from)

			recorded := 0
			rs.SetRecordCallback(func(*Cell) { recorded++ })

			dict, err := rs.Root().BeginParse()
			if err != nil {
				t.Fatal(err)
			}
			rebuilt := dict.MustToCell().AsDict(64)
			rnd := rand.New(rand.NewSource(2026082702))
			for i := 0; i < 512; i++ {
				if _, err = rebuilt.LoadValue(merkleUpdateDictKey(keys[rnd.Intn(len(keys))])); err != nil {
					t.Fatalf("read %d: %v", i, err)
				}
			}
			for i := 0; i < 256; i++ {
				key := keys[rnd.Intn(len(keys))]
				if err = rebuilt.Set(merkleUpdateDictKey(key), merkleUpdateDictValue(key^uint64(i+1))); err != nil {
					t.Fatalf("write %d: %v", i, err)
				}
			}
			to := BeginCell().MustStoreUInt(0xD4, 8).MustStoreRef(rebuilt.AsCell()).EndCell()

			sizeBefore := rs.Size()
			recordedBefore := recorded

			var update *Cell
			if tc.applied {
				update, _, _, err = rs.CreateMerkleUpdateAppliedSized(to, 0, tc.parallelism)
			} else {
				update, err = rs.CreateMerkleUpdate(to)
			}
			if err != nil {
				t.Fatalf("create merkle update: %v", err)
			}

			if rs.Size() != sizeBefore {
				t.Fatalf("building the update widened the record: %d -> %d cells", sizeBefore, rs.Size())
			}
			if recorded != recordedBefore {
				t.Fatalf("building the update fired the first-read callback %d times", recorded-recordedBefore)
			}

			applied, err := ApplyMerkleUpdate(from.WithoutTrace(), update)
			if err != nil {
				t.Fatalf("apply merkle update: %v", err)
			}
			if applied.HashKey() != to.HashKey() {
				t.Fatalf("applied root = %x, want %x", applied.Hash()[:8], to.Hash()[:8])
			}
		})
	}
}
