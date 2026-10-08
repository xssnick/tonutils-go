//go:build cgo && tvm_cross_emulator

package tvm

import (
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// PFXDICTDEL collapses a fork after removing its last node on one side. The
// surviving sibling is loaded as a dictionary node too, so a library cell in
// that position must resolve before its edge label is merged into the parent.
func TestTVMCrossEmulatorPfxDictDeleteResolvesSurvivorLibrary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	left := cell.BeginCell().
		MustStoreUInt(0, 2). // empty hml_short label
		MustStoreBoolBit(false).
		MustStoreUInt(0xa1, 8).
		EndCell()
	survivor := cell.BeginCell().
		MustStoreUInt(0, 2). // empty hml_short label
		MustStoreBoolBit(false).
		MustStoreUInt(0xb2, 8).
		EndCell()
	library := mustCrossLibraryCellForHash(t, survivor.Hash())
	root := cell.BeginCell().
		MustStoreUInt(0, 2). // empty hml_short label
		MustStoreBoolBit(true).
		MustStoreRef(left).
		MustStoreRef(library).
		EndCell()
	libraries := mustCrossLibraryCollection(t, survivor)
	key := cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse()

	assertDictLibraryParityCase(t, "pfxdictdel-library-survivor", rawCodeCellFromHex(t, "30F473"),
		[]any{key, root, int64(4)}, libraries, 13)
}

func TestTVMCrossEmulatorPrefixDictResolvesValidLibraryNodes(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	targetDict := cell.NewPrefixDict(4)
	if err := targetDict.Set(
		cell.BeginCell().MustStoreUInt(0, 1).EndCell(),
		cell.BeginCell().MustStoreUInt(0xa1, 8).EndCell(),
	); err != nil {
		t.Fatal(err)
	}
	target := targetDict.AsCell()
	libraryRoot := mustCrossLibraryCellForHash(t, target.Hash())
	rootLibraries := mustCrossLibraryCollection(t, target)

	assertDictLibraryParityCase(t, "pfxdictgetq-library-root", rawCodeCellFromHex(t, "30F4A8"),
		[]any{cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse(), libraryRoot, int64(4)}, rootLibraries, 13)
	assertDictLibraryParityCase(t, "pfxdictset-library-root", rawCodeCellFromHex(t, "30F470"),
		[]any{
			cell.BeginCell().MustStoreUInt(0xb2, 8).EndCell().MustBeginParse(),
			cell.BeginCell().MustStoreUInt(1, 1).EndCell().MustBeginParse(),
			libraryRoot,
			int64(4),
		}, rootLibraries, 13)
	assertDictLibraryParityCase(t, "pfxdictreplace-library-root", rawCodeCellFromHex(t, "30F471"),
		[]any{
			cell.BeginCell().MustStoreUInt(0xc3, 8).EndCell().MustBeginParse(),
			cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse(),
			libraryRoot,
			int64(4),
		}, rootLibraries, 13)

	left := cell.BeginCell().
		MustStoreUInt(0, 2). // empty hml_short label after the parent edge
		MustStoreBoolBit(false).
		MustStoreUInt(0xd4, 8).
		EndCell()
	right := cell.BeginCell().
		MustStoreUInt(0, 2).
		MustStoreBoolBit(false).
		MustStoreUInt(0xe5, 8).
		EndCell()
	libraryChild := mustCrossLibraryCellForHash(t, left.Hash())
	fork := cell.BeginCell().
		MustStoreUInt(0, 2).
		MustStoreBoolBit(true).
		MustStoreRef(libraryChild).
		MustStoreRef(right).
		EndCell()
	childLibraries := mustCrossLibraryCollection(t, left)

	assertDictLibraryParityCase(t, "pfxdictgetq-library-child", rawCodeCellFromHex(t, "30F4A8"),
		[]any{cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse(), fork, int64(4)}, childLibraries, 13)
}
