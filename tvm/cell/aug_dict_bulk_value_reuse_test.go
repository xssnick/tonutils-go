package cell

import "testing"

func TestAugmentedBulkCellValuesMaterializeOwnedPayloads(t *testing.T) {
	pruned, err := CreatePrunedBranch(BeginCell().MustStoreUInt(9, 8).
		MustStoreRef(BeginCell().MustStoreUInt(1, 8).EndCell()).EndCell(), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := FromBOC(BeginCell().MustStoreUInt(0x1234, 13).EndCell().ToBOC())
	if err != nil {
		t.Fatal(err)
	}
	withRef := BeginCell().MustStoreUInt(7, 13).MustStoreRef(pruned).EndCell()
	virtual := withRef.Virtualize(0)
	if withRef.Level() != 1 || !virtual.IsVirtualized() {
		t.Fatal("fixture must contain a level-one value and its virtual view")
	}
	values := []*Cell{partial, withRef, virtual, libraryDictNode(t)}
	inputTrace := NewTrace(TraceHooks{
		OnLoad:   func(*Cell) { t.Error("bulk preparation loaded the input value") },
		OnCreate: func() { t.Error("bulk preparation propagated the input value trace") },
	})
	want := make([]*Cell, len(values))
	for i := range values {
		values[i] = values[i].WithTrace(inputTrace)
		// Cell-valued writes copy the raw payload as an ordinary value, including
		// when the input happens to be a special or virtual cell.
		want[i] = values[i].ToBuilder().EndCell()
	}
	for _, api := range []string{"cell", "bytes", "uint", "diff"} {
		t.Run(api, func(t *testing.T) {
			aug := &materializingAugmentation{}
			d, err := NewAugDict(8, aug)
			if err != nil {
				t.Fatal(err)
			}
			entries := make([]AugmentedEntry, len(values))
			bytesEntries := make([]AugmentedBytesEntry, len(values))
			uintEntries := make([]AugmentedUintEntry, len(values))
			for i, value := range values {
				entries[i] = AugmentedEntry{Key: BeginCell().MustStoreUInt(uint64(i), 8).EndCell(), Value: value}
				bytesEntries[i] = AugmentedBytesEntry{Key: []byte{byte(i)}, Value: value}
				uintEntries[i] = AugmentedUintEntry{Key: uint64(i), Value: value}
			}
			switch api {
			case "cell":
				err = d.SetMany(entries)
			case "bytes":
				err = d.SetManyByBytes(bytesEntries)
			case "uint":
				err = d.SetManyByUint(uintEntries)
			case "diff":
				_, err = d.SetManyWithDiff(entries)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(aug.owned) != len(values) || len(aug.base) != len(values) {
				t.Fatalf("materialized %d owned values and %d bases", len(aug.owned), len(aug.base))
			}
			for i := range values {
				for _, actual := range []*Cell{aug.owned[i], aug.base[i]} {
					if actual.HashKey() != want[i].HashKey() || actual.IsSpecial() || actual.IsVirtualized() {
						t.Fatalf("materialized value %d has incorrect payload or metadata", i)
					}
					for ref := range want[i].refsCount() {
						if actual.refs[ref] != want[i].refs[ref] {
							t.Fatalf("materialized value %d changed reference %d", i, ref)
						}
					}
					proof, err := CreateMerkleProof(actual)
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := FromBOC(proof.ToBOC())
					if err != nil || decoded.refs[0].HashKey() != want[i].HashKey() {
						t.Fatalf("materialized value %d is not independently serializable: %v", i, err)
					}
				}
			}
		})
	}
}
