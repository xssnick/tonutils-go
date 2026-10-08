package cell

import (
	"bytes"
	"testing"
)

func TestDictionaryBulkDoesNotRetainKeyBuffers(t *testing.T) {
	for _, mode := range []string{"build", "multiset"} {
		t.Run(mode, func(t *testing.T) {
			items := make([]DictBulkKV, 512)
			for i := range items {
				items[i] = DictBulkKV{
					Key:   []byte{byte(i >> 5), byte(i<<3) | 7},
					Value: BeginCell().MustStoreUInt(uint64(i), 16),
				}
			}
			d := NewDict(13)
			var err error
			if mode == "build" {
				d, err = NewDictFromItems(13, items)
			} else {
				err = d.Multiset(items)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := d.AsCell().ToBOC()
			for i := range items {
				clear(items[i].Key)
			}
			if !bytes.Equal(before, d.AsCell().ToBOC()) {
				t.Fatal("dictionary retained input key buffers")
			}
			for i := range 512 {
				key := BeginCell().MustStoreUInt(uint64(i), 13).EndCell()
				v, err := d.LoadValue(key)
				if err != nil {
					t.Fatal(err)
				}
				if got := v.MustLoadUInt(16); got != uint64(i) {
					t.Fatalf("key %d: value = %d", i, got)
				}
			}
		})
	}
}
