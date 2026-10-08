package cell

import (
	"errors"
	"testing"
)

func TestDictionaryDeleteRejectsMalformedSurvivingFork(t *testing.T) {
	leaf := BeginCell().MustStoreUInt(0b0100, 4).MustStoreUInt(42, 8).EndCell()
	child := BeginCell().MustStoreUInt(0, 2).EndCell()
	for _, tc := range []struct {
		name    string
		sibling *Cell
	}{
		{name: "junk_bit", sibling: BeginCell().MustStoreUInt(0b001, 3).EndCell()},
		{name: "one_ref", sibling: BeginCell().MustStoreUInt(0, 2).MustStoreRef(child).EndCell()},
		{name: "trailing_bit", sibling: BeginCell().MustStoreUInt(0b001, 3).MustStoreRef(child).MustStoreRef(child).EndCell()},
		{name: "three_refs", sibling: BeginCell().MustStoreUInt(0, 2).MustStoreRef(child).MustStoreRef(child).MustStoreRef(child).EndCell()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := BeginCell().MustStoreUInt(0, 2).MustStoreRef(leaf).MustStoreRef(tc.sibling).EndCell()
			key := BeginCell().MustStoreUInt(0, 2).EndCell()
			for _, op := range []struct {
				name string
				run  func(*Dictionary) error
			}{
				{name: "delete", run: func(d *Dictionary) error { return d.Delete(key) }},
				{name: "delete_get", run: func(d *Dictionary) error { _, err := d.LoadValueAndDelete(key); return err }},
				{name: "remove_min", run: func(d *Dictionary) error { _, _, err := d.LoadMinMaxAndDelete(false, false); return err }},
			} {
				t.Run(op.name, func(t *testing.T) {
					d := root.AsDict(2)
					if err := op.run(d); !errors.Is(err, ErrInvalidDictForkNode) {
						t.Fatalf("delete = %v, want ErrInvalidDictForkNode", err)
					}
					if d.AsCell().HashKey() != root.HashKey() {
						t.Fatal("failed delete changed dictionary root")
					}
					value, err := d.LoadValue(key)
					if err != nil || value.MustLoadUInt(8) != 42 {
						t.Fatalf("failed delete changed target value: %v", err)
					}
				})
			}
		})
	}
}
