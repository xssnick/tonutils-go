package cell

import "testing"

func TestAugmentedDictionaryCount(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var dict *AugmentedDictionary
		count, err := dict.Count()
		if err != nil {
			t.Fatalf("count nil dictionary: %v", err)
		}
		if count != 0 {
			t.Fatalf("nil dictionary count = %d, want 0", count)
		}
	})

	t.Run("empty", func(t *testing.T) {
		dict, err := NewAugDict(8, testMetricAugmentation{})
		if err != nil {
			t.Fatal(err)
		}
		count, err := dict.Count()
		if err != nil {
			t.Fatalf("count empty dictionary: %v", err)
		}
		if count != 0 {
			t.Fatalf("empty dictionary count = %d, want 0", count)
		}
	})

	t.Run("populated", func(t *testing.T) {
		dict, err := NewAugDict(8, testMetricAugmentation{})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []uint64{0x01, 0x10, 0x80, 0xff} {
			if err = dict.Set(mustTestAugKey(t, key), mustTestAugValue(t, key, 8)); err != nil {
				t.Fatal(err)
			}
		}

		count, err := dict.Count()
		if err != nil {
			t.Fatalf("count populated dictionary: %v", err)
		}
		if count != 4 {
			t.Fatalf("populated dictionary count = %d, want 4", count)
		}

		if err = dict.Delete(mustTestAugKey(t, 0x10)); err != nil {
			t.Fatal(err)
		}
		count, err = dict.Count()
		if err != nil {
			t.Fatalf("count dictionary after delete: %v", err)
		}
		if count != 3 {
			t.Fatalf("dictionary count after delete = %d, want 3", count)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		dict := &AugmentedDictionary{
			root:  BeginCell().EndCell(),
			keySz: 8,
		}
		if _, err := dict.Count(); err == nil {
			t.Fatal("malformed dictionary count returned no error")
		}
	})
}
