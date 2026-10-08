//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorDictDeleteValidatesSurvivingSibling(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Deleting 00 must validate the unvisited sibling before merging its
	// label into the parent. Read-only MIN does not need that sibling.
	left := cell.BeginCell().MustStoreUInt(0b0100, 4).MustStoreUInt(42, 8).EndCell()
	child := cell.BeginCell().MustStoreUInt(0, 2).EndCell()
	for _, shape := range []struct {
		name    string
		sibling *cell.Cell
	}{
		{name: "junk_bit", sibling: cell.BeginCell().MustStoreUInt(0b001, 3).EndCell()},
		{name: "one_ref", sibling: cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(child).EndCell()},
		{name: "trailing_bit", sibling: cell.BeginCell().MustStoreUInt(0b001, 3).MustStoreRef(child).MustStoreRef(child).EndCell()},
		{name: "three_refs", sibling: cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(child).MustStoreRef(child).MustStoreRef(child).EndCell()},
	} {
		root := cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(left).MustStoreRef(shape.sibling).EndCell()
		for _, tc := range []struct {
			name     string
			code     string
			stack    []any
			wantExit int32
		}{
			{name: "delete", code: "30F45B", stack: []any{int64(0), root, int64(2)}, wantExit: vmerr.CodeDict},
			{name: "delete_get", code: "30F466", stack: []any{int64(0), root, int64(2)}, wantExit: vmerr.CodeDict},
			{name: "remove_min", code: "30F496", stack: []any{root, int64(2)}, wantExit: vmerr.CodeDict},
			{name: "min_does_not_visit_sibling", code: "30F486", stack: []any{root, int64(2)}},
		} {
			for _, version := range []int{0, 13} {
				t.Run(fmt.Sprintf("%s/%s/v%d", shape.name, tc.name, version), func(t *testing.T) {
					res := assertFlowParityCase(t, tc.name, rawCodeCellFromHex(t, tc.code), tc.stack, version, 1_000_000)
					if res.exitCode != tc.wantExit {
						t.Fatalf("exit = %d, want %d", res.exitCode, tc.wantExit)
					}
				})
			}
		}
	}

	t.Run("library_sibling", func(t *testing.T) {
		malformed := cell.BeginCell().MustStoreUInt(0b001, 3).EndCell()
		root := cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(left).MustStoreRef(mustLibraryCellForHash(t, malformed.Hash())).EndCell()
		libs := mustCrossLibraryCollection(t, malformed)
		for _, version := range []int{0, 4, 13} {
			t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
				res := assertDictLibraryParityCase(t, "delete-library-sibling", rawCodeCellFromHex(t, "30F45B"),
					[]any{int64(0), root, int64(2)}, libs, version)
				if res.exitCode != vmerr.CodeDict {
					t.Fatalf("resolved sibling exit = %d, want %d", res.exitCode, vmerr.CodeDict)
				}
			})
		}
	})
}

func TestTVMCrossEmulatorDictSurvivingSiblingLoadGasBoundary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	leaf := cell.BeginCell().MustStoreUInt(0b0100, 4).MustStoreUInt(42, 8).EndCell()
	sibling := cell.BeginCell().MustStoreUInt(0b001, 3).EndCell()
	root := cell.BeginCell().MustStoreUInt(0, 2).MustStoreRef(leaf).MustStoreRef(sibling).EndCell()

	// DEL loads root, leaf, sibling; REMMIN first reads root/leaf, then
	// reloads them for deletion. At the last load OOG wins over invalid fork.
	for _, tc := range []struct {
		name   string
		code   string
		stack  []any
		loaded int64
	}{
		{name: "delete", code: "30F45B", stack: []any{int64(0), root, int64(2)}, loaded: 344},
		{name: "remove_min", code: "30F496", stack: []any{root, int64(2)}, loaded: 394},
	} {
		for _, boundary := range []struct {
			limit    int64
			gas      int64
			wantExit int32
		}{
			{limit: tc.loaded - 101, gas: tc.loaded - 100, wantExit: ^vmerr.CodeOutOfGas},
			{limit: tc.loaded - 100, gas: tc.loaded, wantExit: ^vmerr.CodeOutOfGas},
			{limit: tc.loaded - 1, gas: tc.loaded, wantExit: ^vmerr.CodeOutOfGas},
			{limit: tc.loaded, gas: tc.loaded + 50, wantExit: ^vmerr.CodeOutOfGas},
			{limit: tc.loaded + 49, gas: tc.loaded + 50, wantExit: ^vmerr.CodeOutOfGas},
			{limit: tc.loaded + 50, gas: tc.loaded + 50, wantExit: vmerr.CodeDict},
		} {
			t.Run(fmt.Sprintf("%s/gas%d", tc.name, boundary.limit), func(t *testing.T) {
				res := assertFlowParityCase(t, tc.name, rawCodeCellFromHex(t, tc.code), tc.stack, 13, boundary.limit)
				if res.exitCode != boundary.wantExit || res.gasUsed != boundary.gas {
					t.Fatalf("exit/gas = %d/%d, want %d/%d", res.exitCode, res.gasUsed, boundary.wantExit, boundary.gas)
				}
			})
		}
	}
}

func TestTVMCrossEmulatorSplitQuietFailurePreservesSliceWindow(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	discarded := cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()
	kept := cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()
	source := cell.BeginCell().MustStoreUInt(0xABC123, 24).MustStoreRef(discarded).MustStoreRef(kept).EndCell().MustBeginParse()
	if err := source.SkipBitsAndRefs(3, 1); err != nil {
		t.Fatal(err)
	}
	if !source.OnlyFirst(13, 1) {
		t.Fatal("cannot construct slice window")
	}

	// Eight data bits fit but two references do not. The failed SPLITQ
	// must preserve both bounds; the following strict split reads 0x5E,
	// reference B2, and the five-bit suffix 1 from the same window.
	code := prependRawMethodDrop(codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(8)).Serialize(),
		stackop.PUSHINT(big.NewInt(2)).Serialize(),
		cellsliceop.SPLITQ().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(8)).Serialize(),
		stackop.PUSHINT(big.NewInt(1)).Serialize(),
		cellsliceop.SPLIT().Serialize(),
		cellsliceop.LDU(5).Serialize(),
		cellsliceop.ENDS().Serialize(),
		stackop.SWAP().Serialize(),
		cellsliceop.LDU(8).Serialize(),
		cellsliceop.LDREF().Serialize(),
		cellsliceop.ENDS().Serialize(),
	))
	res := assertFlowParityCase(t, "quiet-split-window", code, []any{source}, 13, 1_000_000)
	if res.exitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.exitCode)
	}
	assertCrossSkippedGoStack(t, res.stack, []any{int64(1), int64(0x5E), kept})
}
