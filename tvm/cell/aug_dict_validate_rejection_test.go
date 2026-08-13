package cell

import (
	"strings"
	"testing"
)

// rebuildTestAugNode re-emits an augmented node with its stored 16-bit extra
// replaced. The label is copied bit for bit, so the node keeps the exact shape
// the dictionary built and only the augmentation it claims changes.
func rebuildTestAugNode(t *testing.T, node *Cell, keySz uint, extra uint64) *Cell {
	t.Helper()

	probe, err := node.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	beforeLabel := probe.BitsLeft()
	labelLen, _, err := loadLabel(keySz, probe, BeginCell())
	if err != nil {
		t.Fatal(err)
	}
	labelBits := beforeLabel - probe.BitsLeft()

	src, err := node.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	b := BeginCell()
	if err = b.storeSliceFromSlice(src, labelBits); err != nil {
		t.Fatal(err)
	}
	if labelLen != keySz {
		for i := 0; i < 2; i++ {
			ref, err := src.LoadRefCell()
			if err != nil {
				t.Fatal(err)
			}
			if err = b.StoreRef(ref); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = src.LoadUInt(16); err != nil {
		t.Fatal(err)
	}
	if err = b.StoreUInt(extra, 16); err != nil {
		t.Fatal(err)
	}
	if rest := src.BitsLeft(); rest > 0 {
		if err = b.storeSliceFromSlice(src, rest); err != nil {
			t.Fatal(err)
		}
	}
	for src.RefsNum() > 0 {
		ref, err := src.LoadRefCell()
		if err != nil {
			t.Fatal(err)
		}
		if err = b.StoreRef(ref); err != nil {
			t.Fatal(err)
		}
	}
	return b.EndCell()
}

func mustTestAugNodeExtra(t *testing.T, node *Cell, keySz uint) uint64 {
	t.Helper()

	extra, err := extractAugmentedNodeExtra(node, keySz, testMetricAugmentation{}.SkipExtra)
	if err != nil {
		t.Fatal(err)
	}
	return extra.MustBeginParse().MustLoadUInt(16)
}

// twoLeafTestAugDict builds the smallest tree that has both node kinds: a root
// fork over two leaves, so a corruption can be planted at either.
func twoLeafTestAugDict(t *testing.T) *AugmentedDictionary {
	t.Helper()

	dict, err := NewAugDict(8, testMetricAugmentation{})
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
		t.Fatal("freshly built augmented dict should validate")
	}
	if dict.root == nil || dict.root.RefsNum() != 2 {
		t.Fatalf("expected a root fork over two leaves, got %v refs", dict.root.RefsNum())
	}
	return dict
}

// TestAugmentedDictionary_ValidateAllRejectsCorruptedExtra pins the whole point
// of the semantic walk: a stored augmentation that does not follow from the
// data below it is rejected, whether it sits on a leaf or on a fork, and
// whether or not the rest of the tree was doctored to agree with it.
func TestAugmentedDictionary_ValidateAllRejectsCorruptedExtra(t *testing.T) {
	aug := testMetricAugmentation{}

	t.Run("fork", func(t *testing.T) {
		dict := twoLeafTestAugDict(t)
		stored := mustTestAugNodeExtra(t, dict.root, 8)

		corrupted := rebuildTestAugNode(t, dict.root, 8, stored+1)
		err := validateAugmentedDictRoot(corrupted, 8, aug)
		if err == nil || !strings.Contains(err.Error(), "fork extra mismatch") {
			t.Fatalf("corrupted fork extra should be rejected by the fork check, got %v", err)
		}

		bad := &AugmentedDictionary{keySz: 8, root: corrupted, aug: aug}
		if bad.ValidateAll() {
			t.Fatal("ValidateAll accepted a corrupted fork extra")
		}
	})

	t.Run("leaf", func(t *testing.T) {
		dict := twoLeafTestAugDict(t)
		leaf, err := dict.root.PeekRef(0)
		if err != nil {
			t.Fatal(err)
		}
		leafStored := mustTestAugNodeExtra(t, leaf, 7)

		corruptedLeaf := rebuildTestAugNode(t, leaf, 7, leafStored+1)
		right, err := dict.root.PeekRef(1)
		if err != nil {
			t.Fatal(err)
		}

		// The fork keeps the extra it always had: only the leaf lies.
		root := rebuildTestAugNodeWithRefs(t, dict.root, 8, corruptedLeaf, right,
			mustTestAugNodeExtra(t, dict.root, 8))
		err = validateAugmentedDictRoot(root, 8, aug)
		if err == nil || !strings.Contains(err.Error(), "leaf extra mismatch") {
			t.Fatalf("corrupted leaf extra should be rejected by the leaf check, got %v", err)
		}

		bad := &AugmentedDictionary{keySz: 8, root: root, aug: aug}
		if bad.ValidateAll() {
			t.Fatal("ValidateAll accepted a corrupted leaf extra")
		}
	})

	// The adversarial shape: the fork extra is recomputed from the corrupted
	// leaf, so the tree agrees with itself everywhere. Only recomputing the
	// leaf from its value catches it, which is exactly the check that must not
	// be short-circuited by carrying stored extras upwards.
	t.Run("leaf with a consistent fork above it", func(t *testing.T) {
		dict := twoLeafTestAugDict(t)
		leaf, err := dict.root.PeekRef(0)
		if err != nil {
			t.Fatal(err)
		}
		right, err := dict.root.PeekRef(1)
		if err != nil {
			t.Fatal(err)
		}
		leafStored := mustTestAugNodeExtra(t, leaf, 7)
		rightStored := mustTestAugNodeExtra(t, right, 7)

		corruptedLeaf := rebuildTestAugNode(t, leaf, 7, leafStored+1)
		root := rebuildTestAugNodeWithRefs(t, dict.root, 8, corruptedLeaf, right,
			leafStored+1+rightStored)

		err = validateAugmentedDictRoot(root, 8, aug)
		if err == nil || !strings.Contains(err.Error(), "leaf extra mismatch") {
			t.Fatalf("a self-consistent but wrong augmentation should still be rejected, got %v", err)
		}

		bad := &AugmentedDictionary{keySz: 8, root: root, aug: aug}
		if bad.ValidateAll() {
			t.Fatal("ValidateAll accepted a self-consistent but wrong augmentation")
		}
	})
}

// rebuildTestAugNodeWithRefs re-emits a fork with new children and a chosen
// stored extra.
func rebuildTestAugNodeWithRefs(t *testing.T, node *Cell, keySz uint, left, right *Cell, extra uint64) *Cell {
	t.Helper()

	probe, err := node.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	beforeLabel := probe.BitsLeft()
	if _, _, err = loadLabel(keySz, probe, BeginCell()); err != nil {
		t.Fatal(err)
	}
	labelBits := beforeLabel - probe.BitsLeft()

	src, err := node.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	b := BeginCell()
	if err = b.storeSliceFromSlice(src, labelBits); err != nil {
		t.Fatal(err)
	}
	if err = b.StoreRef(left); err != nil {
		t.Fatal(err)
	}
	if err = b.StoreRef(right); err != nil {
		t.Fatal(err)
	}
	if err = b.StoreUInt(extra, 16); err != nil {
		t.Fatal(err)
	}
	return b.EndCell()
}
