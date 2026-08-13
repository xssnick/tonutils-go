package cell

import (
	"testing"
)

// TestAdvVirtualizedPrunedStoredExtraRef is the case the shipped prototype's
// own test does not cover: the same pruned stand-in, but reached through a
// VIRTUALIZED view. Under virtualization a pruned branch reports the hash of
// the cell it stands for, so the hash comparison that rejects it in the plain
// case now succeeds - and the walk decides whether to hand the parent the
// stored (pruned) reference or the recomputed (full) one.
func TestValidateAllAcceptsVirtualizedPrunedStoredExtraRef(t *testing.T) {
	aug := refExtraAugmentation{}

	dict, err := NewAugDict(8, aug)
	if err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x00), mustTestAugValue(t, 0xa1, 8)); err != nil {
		t.Fatal(err)
	}
	if err = dict.Set(mustTestAugKey(t, 0x80), mustTestAugValue(t, 0xb2, 16)); err != nil {
		t.Fatal(err)
	}
	if !dict.ValidateAll() {
		t.Fatal("freshly built ref-extra dict should validate")
	}

	leaf, err := dict.root.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	right, err := dict.root.PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}

	pruned := rebuildLeafWithPrunedExtraRef(t, leaf, 7)
	root := rebuildForkChildrenForTest(t, dict.root, 8, pruned, right)

	// plain view: pruned cell hashes its own body, so it is rejected
	plainErr := validateAugmentedDictRoot(root, 8, aug)
	t.Logf("plain view          -> %v", plainErr)

	// virtualized view: the pruned branch now reports the represented hash
	virt := root.Virtualize(0)
	if virt == root {
		t.Log("NOTE: Virtualize(0) returned the same cell (root level is 0); " +
			"wrapping the level-1 pruned child means the root itself carries level 1")
	}
	virtErr := validateAugmentedDictRoot(virt, 8, aug)
	t.Logf("virtualized view    -> %v", virtErr)

	// report what a caller would see
	plainDict := &AugmentedDictionary{keySz: 8, root: root, aug: aug}
	virtDict := &AugmentedDictionary{keySz: 8, root: virt, aug: aug}
	t.Logf("ValidateAll plain=%v virtualized=%v", plainDict.ValidateAll(), virtDict.ValidateAll())
	t.Logf("root level=%d pruned-leaf level=%d", root.Level(), pruned.Level())
}
