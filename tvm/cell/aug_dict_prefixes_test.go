package cell

import (
	"math/rand"
	"sort"
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

func TestKeyPrefixesMatchesKeySet(t *testing.T) {
	const keyBits = uint(48)
	rnd := rand.New(rand.NewSource(0x7a6b5c4d))

	for round := 0; round < 20; round++ {
		dict, err := NewAugDict(keyBits, testMetricAugmentation{})
		if err != nil {
			t.Fatal(err)
		}

		keys := make(map[uint64]struct{})
		for len(keys) < 32+rnd.Intn(256) {
			key := uint64(rnd.Int63()) & ((uint64(1) << keyBits) - 1)
			if _, ok := keys[key]; ok {
				continue
			}
			keys[key] = struct{}{}
			if err = dict.Set(
				BeginCell().MustStoreUInt(key, keyBits).EndCell(),
				mustTestAugValue(t, uint64(rnd.Intn(256)), 8),
			); err != nil {
				t.Fatal(err)
			}
		}

		for _, requestedBits := range []uint{1, 7, 8, 13, 16, 31, keyBits, keyBits + 10} {
			bits := min(requestedBits, keyBits)
			wantSet := make(map[uint64]struct{}, len(keys))
			for key := range keys {
				wantSet[key>>(keyBits-bits)] = struct{}{}
			}
			want := make([]uint64, 0, len(wantSet))
			for prefix := range wantSet {
				want = append(want, prefix)
			}
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })

			got, err := dict.KeyPrefixes(requestedBits, len(want))
			if err != nil {
				t.Fatalf("round %d bits %d: %v", round, requestedBits, err)
			}
			if len(got) != len(want) {
				t.Fatalf("round %d bits %d: got %d prefixes, want %d", round, requestedBits, len(got), len(want))
			}
			for i, prefix := range got {
				if prefix.BitsSize() != bits {
					t.Fatalf("round %d bits %d prefix %d has %d bits", round, requestedBits, i, prefix.BitsSize())
				}
				if value := prefix.MustBeginParse().MustLoadUInt(bits); value != want[i] {
					t.Fatalf("round %d bits %d prefix %d = %x, want %x", round, requestedBits, i, value, want[i])
				}
			}

			if len(want) > 0 {
				if limited, err := dict.KeyPrefixes(requestedBits, len(want)-1); err == nil || limited != nil {
					t.Fatalf("round %d bits %d: limit did not reject the last prefix", round, requestedBits)
				}
			}
		}
	}
}

func BenchmarkAugmentedDictionaryKeyPrefixes16(b *testing.B) {
	const entries = 1024
	dict, err := NewAugDict(16, testMetricAugmentation{})
	if err != nil {
		b.Fatal(err)
	}
	for i := range entries {
		// Multiplication by an odd number permutes the 16-bit key space and
		// keeps the benchmark from measuring one unusually dense branch.
		key := uint64(uint16(i * 40503))
		if _, err = dict.SetBuilderByUintKeyWithMode(key, BeginCell().MustStoreUInt(key, 16), DictSetModeSet); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		prefixes, err := dict.KeyPrefixes(16, entries)
		if err != nil {
			b.Fatal(err)
		}
		if len(prefixes) != entries {
			b.Fatalf("got %d prefixes, want %d", len(prefixes), entries)
		}
	}
}
