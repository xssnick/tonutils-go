package cell

import (
	"strings"
	"testing"
)

// refExtraAugmentation keeps its extra behind a reference and reads that
// reference back when combining, which is what makes the stored and the
// recomputed extra distinguishable even when their hashes agree.
type refExtraAugmentation struct{}

func (refExtraAugmentation) SkipExtra(loader *Slice) error {
	_, err := loader.LoadRefCell()
	return err
}

func (refExtraAugmentation) EmptyExtra(dst *Builder) error {
	return dst.StoreRef(BeginCell().MustStoreUInt(0, 16).EndCell())
}

func (refExtraAugmentation) LeafExtra(value *Slice, dst *Builder) error {
	return dst.StoreRef(BeginCell().MustStoreUInt(uint64(value.BitsLeft()), 16).EndCell())
}

func (refExtraAugmentation) CombineExtra(leftExtra, rightExtra *Slice, dst *Builder) error {
	left, err := loadRefUInt16(leftExtra)
	if err != nil {
		return err
	}
	right, err := loadRefUInt16(rightExtra)
	if err != nil {
		return err
	}
	return dst.StoreRef(BeginCell().MustStoreUInt(left+right, 16).EndCell())
}

func loadRefUInt16(extra *Slice) (uint64, error) {
	ref, err := extra.LoadRefCell()
	if err != nil {
		return 0, err
	}
	body, err := ref.BeginParse()
	if err != nil {
		return 0, err
	}
	return body.LoadUInt(16)
}

func prunedTwinForTest(t *testing.T, c *Cell) *Cell {
	t.Helper()

	hash := c.HashKey()
	depth := c.Depth()
	data := make([]byte, 0, 36)
	data = append(data, byte(PrunedCellType), 0x01)
	data = append(data, hash[:]...)
	data = append(data, byte(depth>>8), byte(depth))
	return makeManualCellForTest(true, LevelMask{Mask: 1}, 288, data, nil)
}

func testAugLabelBits(t *testing.T, node *Cell, keySz uint) uint {
	t.Helper()

	probe, err := node.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	before := probe.BitsLeft()
	if _, _, err = loadLabel(keySz, probe, BeginCell()); err != nil {
		t.Fatal(err)
	}
	return before - probe.BitsLeft()
}

// rebuildLeafWithPrunedExtraRef re-emits a leaf whose stored extra reference is
// replaced by a pruned cell standing for the very same content.
func rebuildLeafWithPrunedExtraRef(t *testing.T, leaf *Cell, keySz uint) *Cell {
	t.Helper()

	labelBits := testAugLabelBits(t, leaf, keySz)
	src, err := leaf.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	b := BeginCell()
	if err = b.storeSliceFromSlice(src, labelBits); err != nil {
		t.Fatal(err)
	}
	extraRef, err := src.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.StoreRef(prunedTwinForTest(t, extraRef)); err != nil {
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

func rebuildForkChildrenForTest(t *testing.T, node *Cell, keySz uint, left, right *Cell) *Cell {
	t.Helper()

	labelBits := testAugLabelBits(t, node, keySz)
	src, err := node.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	b := BeginCell()
	if err = b.storeSliceFromSlice(src, labelBits); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = src.LoadRefCell(); err != nil {
			t.Fatal(err)
		}
	}
	if err = b.StoreRef(left); err != nil {
		t.Fatal(err)
	}
	if err = b.StoreRef(right); err != nil {
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

// TestAugmentedDictionary_ValidateAllRejectsPrunedStoredExtraRef pins why the
// walk may carry a stored extra upwards instead of the one it recomputed. The
// worry is a stored reference that matches by hash but cannot be read the same
// way — a pruned stand-in for the recomputed cell. It cannot happen: a pruned
// cell hashes its own body, so it never matches the cell it stands for, and
// the node is rejected right there. The augmentation here keeps its extra
// behind a reference precisely so that this is testable at all.
func TestAugmentedDictionary_ValidateAllRejectsPrunedStoredExtraRef(t *testing.T) {
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

	err = validateAugmentedDictRoot(root, 8, aug)
	if err == nil || !strings.Contains(err.Error(), "leaf extra mismatch") {
		t.Fatalf("a pruned stored extra reference should be rejected at its own node, got %v", err)
	}
	withPruned := &AugmentedDictionary{keySz: 8, root: root, aug: aug}
	if withPruned.ValidateAll() {
		t.Fatal("ValidateAll accepted a tree whose stored extra reference is pruned")
	}
}
