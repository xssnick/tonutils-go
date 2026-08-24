package cell

import (
	"math/rand"
	"testing"
)

// The prefixes must partition the key set exactly: every key lands in exactly
// one prefix's subtree, no prefix is empty, and iterating the subtrees yields
// the full dictionary.
func TestKeyPrefixesPartitionTheDictionary(t *testing.T) {
	rnd := rand.New(rand.NewSource(0x9e1f))
	for round := 0; round < 20; round++ {
		dict, err := NewAugDict(48, testMetricAugmentation{})
		if err != nil {
			t.Fatal(err)
		}
		// Two "workchains" in the top 8 bits, like a queue keyed by workchain.
		n := 200 + rnd.Intn(2000)
		keys := make(map[uint64]struct{}, n)
		for len(keys) < n {
			wc := uint64(0)
			if rnd.Intn(4) == 0 {
				wc = 0xff
			}
			key := wc<<40 | uint64(rnd.Int63n(1<<40))
			if _, dup := keys[key]; dup {
				continue
			}
			keys[key] = struct{}{}
			if err = dict.Set(BeginCell().MustStoreUInt(key, 48).EndCell(), mustTestAugValue(t, uint64(rnd.Intn(256)), 8)); err != nil {
				t.Fatal(err)
			}
		}
		for _, bits := range []uint{8, 8 + 5, 8 + 8} {
			prefixes, err := dict.KeyPrefixes(bits, 4096)
			if err != nil {
				t.Fatalf("round %d bits %d: %v", round, bits, err)
			}
			if len(prefixes) < 2 {
				t.Fatalf("round %d bits %d: %d prefixes; the split is vacuous", round, bits, len(prefixes))
			}
			seen := make(map[uint64]int, n)
			for i, prefix := range prefixes {
				if prefix.BitsSize() != bits {
					t.Fatalf("prefix %d has %d bits, want %d", i, prefix.BitsSize(), bits)
				}
				sub := dict.Copy()
				ok, err := sub.CutPrefixSubdict(prefix, false)
				if err != nil || !ok {
					t.Fatalf("cut prefix %d: ok=%t err=%v", i, ok, err)
				}
				count := 0
				_, err = sub.CheckForEachExtra(func(_, _ *Slice, key *Cell) (bool, error) {
					k, err := key.BeginParse()
					if err != nil {
						return false, err
					}
					v, err := k.LoadUInt(48)
					if err != nil {
						return false, err
					}
					seen[v]++
					count++
					return true, nil
				}, false)
				if err != nil {
					t.Fatal(err)
				}
				if count == 0 {
					t.Fatalf("round %d bits %d: prefix %d names an empty subtree", round, bits, i)
				}
			}
			if len(seen) != n {
				t.Fatalf("round %d bits %d: subtrees cover %d keys of %d", round, bits, len(seen), n)
			}
			for k, c := range seen {
				if c != 1 {
					t.Fatalf("key %x appears in %d subtrees", k, c)
				}
			}
		}
	}
}
