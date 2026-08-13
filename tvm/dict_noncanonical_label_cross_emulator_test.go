//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The reference rebuilds every changed ancestor with append_dict_label. This
// canonicalizes a valid but non-minimal HmLabel on the mutation path; an
// unchanged operation must still preserve the original cell.
func TestTVMCrossEmulatorDictMutationCanonicalizesForkLabel(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Run("fixed-set", func(t *testing.T) {
		root := crossNonCanonicalFixedForkRoot(t, 1, map[int64]uint64{0: 0x11, 1: 0x22})
		key := cell.BeginCell().MustStoreUInt(1, 1).EndCell().MustBeginParse()
		value := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell().MustBeginParse()
		assertFlowParityCase(t, "dict-set-noncanonical-fork", rawCodeCellFromHex(t, "30F412"),
			[]any{value, key, root, int64(1)}, 13, 1_000_000)
	})

	t.Run("fixed-delete", func(t *testing.T) {
		root := crossNonCanonicalFixedForkRoot(t, 2, map[int64]uint64{0: 0x11, 2: 0x22, 3: 0x33})
		key := cell.BeginCell().MustStoreUInt(2, 2).EndCell().MustBeginParse()
		assertFlowParityCase(t, "dict-delete-noncanonical-fork", rawCodeCellFromHex(t, "30F459"),
			[]any{key, root, int64(2)}, 13, 1_000_000)
	})

	t.Run("prefix-set", func(t *testing.T) {
		root := crossNonCanonicalPrefixForkRoot(t, 1, map[int64]uint64{0: 0x11, 1: 0x22})
		key := cell.BeginCell().MustStoreUInt(1, 1).EndCell().MustBeginParse()
		value := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell().MustBeginParse()
		assertFlowParityCase(t, "prefix-set-noncanonical-fork", rawCodeCellFromHex(t, "30F470"),
			[]any{value, key, root, int64(1)}, 13, 1_000_000)
	})

	t.Run("prefix-delete", func(t *testing.T) {
		root := crossNonCanonicalPrefixForkRoot(t, 2, map[int64]uint64{0: 0x11, 2: 0x22, 3: 0x33})
		key := cell.BeginCell().MustStoreUInt(2, 2).EndCell().MustBeginParse()
		assertFlowParityCase(t, "prefix-delete-noncanonical-fork", rawCodeCellFromHex(t, "30F473"),
			[]any{key, root, int64(2)}, 13, 1_000_000)
	})
}

func crossNonCanonicalFixedForkRoot(t *testing.T, keyBits uint, values map[int64]uint64) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(keyBits)
	for key, value := range values {
		if err := dict.SetIntKey(big.NewInt(key), cell.BeginCell().MustStoreUInt(value, 8).EndCell()); err != nil {
			t.Fatalf("build fixed dictionary: %v", err)
		}
	}
	canonical := dict.AsCell()
	root := canonical.MustBeginParse()
	if err := root.SkipBits(canonical.BitsSize()); err != nil {
		t.Fatalf("skip canonical root label: %v", err)
	}
	left, err := root.LoadRefCell()
	if err != nil {
		t.Fatalf("load canonical left branch: %v", err)
	}
	right, err := root.LoadRefCell()
	if err != nil {
		t.Fatalf("load canonical right branch: %v", err)
	}
	return cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreUInt(0, uint64BitLen(keyBits)).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
}

func crossNonCanonicalPrefixForkRoot(t *testing.T, keyBits uint, values map[int64]uint64) *cell.Cell {
	t.Helper()
	dict := cell.NewPrefixDict(keyBits)
	for key, value := range values {
		keyCell := cell.BeginCell().MustStoreUInt(uint64(key), keyBits).EndCell()
		if err := dict.Set(keyCell, cell.BeginCell().MustStoreUInt(value, 8).EndCell()); err != nil {
			t.Fatalf("build prefix dictionary: %v", err)
		}
	}
	canonical := dict.AsCell()
	root := canonical.MustBeginParse()
	if err := root.SkipBits(canonical.BitsSize()); err != nil {
		t.Fatalf("skip canonical root label: %v", err)
	}
	left, err := root.LoadRefCell()
	if err != nil {
		t.Fatalf("load canonical left branch: %v", err)
	}
	right, err := root.LoadRefCell()
	if err != nil {
		t.Fatalf("load canonical right branch: %v", err)
	}
	return cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreUInt(0, uint64BitLen(keyBits)).
		MustStoreBoolBit(true).
		MustStoreRef(left).
		MustStoreRef(right).
		EndCell()
}

func uint64BitLen(value uint) uint {
	bits := uint(0)
	for value > 0 {
		bits++
		value >>= 1
	}
	return bits
}
