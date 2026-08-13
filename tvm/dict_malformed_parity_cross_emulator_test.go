//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// Witnesses from the 2026-08 dictionary parity audit: malformed fork nodes
// (payload beyond the label or a reference count other than two) fail every
// walk with a dictionary error, prefix-dict set/delete treat a fork consuming
// the whole key as a no-op, and both engines must agree on exit code, gas and
// stack for each case.

func dictWitnessCellFromBOC(t *testing.T, hexBOC string) *cell.Cell {
	t.Helper()
	raw, err := hex.DecodeString(hexBOC)
	if err != nil {
		t.Fatalf("bad boc hex: %v", err)
	}
	c, err := cell.FromBOC(raw)
	if err != nil {
		t.Fatalf("bad boc: %v", err)
	}
	return c
}

func TestTVMCrossEmulatorMalformedDictForkParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Fork node with 4 junk bits after an empty hml_short label.
	junkFork := dictWitnessCellFromBOC(t, "b5ee9c7241010301000d0002013e010200032aa000032ee0f422347c")
	// Fork node with a single reference.
	oneRefFork := dictWitnessCellFromBOC(t, "b5ee9c72410102010008000101200100032aa09ae81046")

	zeroKey := rawCodeCellFromHex(t, "00").MustBeginParse()
	zeroKey2 := rawCodeCellFromHex(t, "00").MustBeginParse()
	oneKey := rawCodeCellFromHex(t, "80").MustBeginParse()

	cases := []struct {
		name  string
		code  string // hex, prefixed with DROP for the method-id slot
		stack []any
	}{
		{name: "dictget-junk-fork", code: "30F40A", stack: []any{sliceTakeBits(t, zeroKey, 1), junkFork, int64(1)}},
		{name: "dictget-one-ref-fork", code: "30F40A", stack: []any{sliceTakeBits(t, oneKey, 1), oneRefFork, int64(1)}},
		{name: "dictmin-junk-fork", code: "30F482", stack: []any{junkFork, int64(1)}},
		{name: "dictuset-junk-fork", code: "30F416", stack: []any{sliceTakeBits(t, zeroKey2, 8), int64(0), junkFork, int64(1)}},
		{name: "dictremmin-junk-fork", code: "30F492", stack: []any{junkFork, int64(1)}},
		{name: "dictugetnext-junk-fork", code: "30F47C", stack: []any{int64(0), junkFork, int64(1)}},
		{name: "subdictuget-junk-fork", code: "30F4B3", stack: []any{int64(0), int64(1), junkFork, int64(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFlowParityCase(t, tc.name, rawCodeCellFromHex(t, tc.code), tc.stack, 13, 1_000_000)
		})
	}
}

func TestTVMCrossEmulatorMalformedDictForkOOGPrecedence(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Loading the malformed fork exhausts gas before the dictionary walker can
	// report its structural error. The out-of-gas abort therefore wins without
	// charging the exception handler; 43/44 and 143/144 pin both boundaries.
	const junkForkBOC = "b5ee9c7241010301000d0002013e010200032aa000032ee0f422347c"
	for _, gasLimit := range []int64{43, 44, 143, 144} {
		t.Run(fmt.Sprintf("dictget-gas-%d", gasLimit), func(t *testing.T) {
			key := rawCodeCellFromHex(t, "00").MustBeginParse()
			stack := []any{sliceTakeBits(t, key, 1), dictWitnessCellFromBOC(t, junkForkBOC), int64(1)}
			assertFlowParityCase(t, "dictget-junk-fork-oog", rawCodeCellFromHex(t, "30F40A"), stack, 13, gasLimit)
		})

		t.Run(fmt.Sprintf("dictuset-gas-%d", gasLimit), func(t *testing.T) {
			value := rawCodeCellFromHex(t, "00").MustBeginParse()
			stack := []any{sliceTakeBits(t, value, 8), int64(0), dictWitnessCellFromBOC(t, junkForkBOC), int64(1)}
			assertFlowParityCase(t, "dictuset-junk-fork-oog", rawCodeCellFromHex(t, "30F416"), stack, 13, gasLimit)
		})
	}
}

func sliceTakeBits(t *testing.T, s *cell.Slice, bits uint) *cell.Slice {
	t.Helper()
	sub, err := s.LoadSlice(bits)
	if err != nil {
		t.Fatalf("take bits: %v", err)
	}
	return cell.BeginCell().MustStoreSlice(sub, bits).EndCell().MustBeginParse()
}

func assertDictLibraryParityCase(t *testing.T, name string, code *cell.Cell, stackVals []any, libs *cell.Cell, globalVersion int) *crossRunResult {
	t.Helper()

	goStack, err := buildCrossStack(stackVals...)
	if err != nil {
		t.Fatalf("%s: go stack: %v", name, err)
	}
	refStack, err := buildCrossStack(stackVals...)
	if err != nil {
		t.Fatalf("%s: ref stack: %v", name, err)
	}
	data := cell.BeginCell().EndCell()

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, data, tuple.Tuple{}, []*cell.Cell{libs}, goStack, globalVersion, 1_000_000)
	if err != nil {
		t.Fatalf("%s: go run: %v", name, err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = 1_000_000
	refCfg.Libs = libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, data, refStack, *refCfg)
	if err != nil {
		t.Fatalf("%s: reference run: %v", name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("%s v%d: exit mismatch: go=%d reference=%d", name, globalVersion, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("%s v%d: gas mismatch: go=%d reference=%d", name, globalVersion, goRes.gasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
		t.Fatalf("%s v%d: stack mismatch:\ngo:  %s\nref: %s", name, globalVersion, goRes.stack.Dump(), refRes.stack.Dump())
	}
	return goRes
}

// A library cell met as a dictionary node resolves through the VM library
// collection like any other cell load: charged per version rules, with the
// walk continuing over the resolved content.
func TestTVMCrossEmulatorDictLibraryNodeParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Leaf-only dict (n=0) stored behind a library cell.
	node := dictWitnessCellFromBOC(t, "b5ee9c724101010100040000032ae0f44b5d97")
	libCell := mustLibraryCellForHash(t, node.Hash())
	collection := mustCrossLibraryCollection(t, node)

	emptyKey := cell.BeginCell().EndCell().MustBeginParse()

	for _, version := range []int{0, 4, 5, 9, 13} {
		assertDictLibraryParityCase(t, "dictget-library-root", rawCodeCellFromHex(t, "30F40A"),
			[]any{emptyKey.Copy(), libCell, int64(0)}, collection, version)
		assertDictLibraryParityCase(t, "dictmin-library-root", rawCodeCellFromHex(t, "30F482"),
			[]any{libCell, int64(0)}, collection, version)
	}

	// Missing library: both engines fail identically.
	otherCollection := mustCrossLibraryCollection(t, cell.BeginCell().MustStoreUInt(0x55, 8).EndCell())
	assertDictLibraryParityCase(t, "dictget-library-missing", rawCodeCellFromHex(t, "30F40A"),
		[]any{emptyKey.Copy(), libCell, int64(0)}, otherCollection, 13)

	// The set walk resolves a library root the same way.
	setKey := cell.BeginCell().EndCell().MustBeginParse()
	assertDictLibraryParityCase(t, "dictuset-library-root", rawCodeCellFromHex(t, "30F412"),
		[]any{setKey, int64(0), libCell, int64(0)}, collection, 13)

	// A library cell as a fork child resolves mid-descent too: n=1 fork with
	// both children being the library cell (leaf dict n=0 inside).
	forkRoot := cell.BeginCell().
		MustStoreUInt(0b00, 2). // hml_short len 0
		MustStoreRef(libCell).
		MustStoreRef(libCell).
		EndCell()
	oneBitKey := cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse()
	assertDictLibraryParityCase(t, "dictget-library-child", rawCodeCellFromHex(t, "30F40A"),
		[]any{oneBitKey, forkRoot, int64(1)}, collection, 13)

	// The prefix-dict walk also resolves the library, then rejects the
	// resolved leaf shape with a dictionary error, like the reference.
	pfxKey := cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse()
	assertDictLibraryParityCase(t, "pfxdictgetq-library-root", rawCodeCellFromHex(t, "30F4A8"),
		[]any{pfxKey, libCell, int64(1)}, collection, 13)
}

func TestTVMCrossEmulatorNestedLibraryLookupParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	target, collection := nestedLibraryLookupFixture(t)
	targetKey := cell.BeginCell().MustStoreSlice(target.Hash(), 256).EndCell()
	if _, err := collection.AsDict(256).LoadValue(targetKey); !errors.Is(err, cell.ErrDictHasSpecialCells) {
		t.Fatalf("fixture target path must hit its library node, got %v", err)
	}
	targetLibrary := mustLibraryCellForHash(t, target.Hash())
	code := rawCodeCellFromHex(t, "30D0") // DROP; CTOS

	// Before v4 the active VM interface resolves a library cell encountered
	// inside the library dictionary. Since v4 lookup runs under a dummy
	// interface, so the same nested node is deliberately not resolved.
	for _, version := range []int{0, 3, 4, 13} {
		res := assertDictLibraryParityCase(t, "nested-library-lookup", code, []any{targetLibrary}, collection, version)
		wantExit := int32(0)
		if version >= 4 {
			wantExit = int32(vmerr.CodeCellUnderflow)
		}
		if res.exitCode != wantExit {
			t.Fatalf("nested library lookup v%d exit = %d, want %d; gas=%d stack=%s", version, res.exitCode, wantExit, res.gasUsed, res.stack.Dump())
		}
	}
}

func nestedLibraryLookupFixture(t *testing.T) (*cell.Cell, *cell.Cell) {
	t.Helper()

	var target, targetSubtree *cell.Cell
	for nonce := uint64(0); nonce < 10_000; nonce++ {
		candidate := cell.BeginCell().MustStoreUInt(nonce, 16).EndCell()
		if candidate.Hash()[0]&0x80 == 0 {
			continue
		}

		subtree := cell.NewDict(255)
		value := cell.BeginCell().MustStoreRef(candidate).EndCell()
		if err := subtree.Set(hashSuffixKey(t, candidate.Hash()), value); err != nil {
			t.Fatalf("set target subtree: %v", err)
		}
		if subtree.AsCell().Hash()[0]&0x80 != 0 {
			continue
		}

		target = candidate
		targetSubtree = subtree.AsCell()
		break
	}
	if target == nil {
		t.Fatal("failed to construct nested library lookup fixture")
	}

	resolver := cell.NewDict(255)
	resolverValue := cell.BeginCell().MustStoreRef(targetSubtree).EndCell()
	if err := resolver.Set(hashSuffixKey(t, targetSubtree.Hash()), resolverValue); err != nil {
		t.Fatalf("set resolver subtree: %v", err)
	}

	collection := cell.BeginCell().
		MustStoreUInt(0, 2). // empty hml_short label, then a fork
		MustStoreRef(resolver.AsCell()).
		MustStoreRef(mustLibraryCellForHash(t, targetSubtree.Hash())).
		EndCell()
	return target, collection
}

func hashSuffixKey(t *testing.T, hash []byte) *cell.Cell {
	t.Helper()
	sl := cell.BeginCell().MustStoreSlice(hash, 256).EndCell().MustBeginParse()
	if err := sl.SkipBits(1); err != nil {
		t.Fatalf("skip hash prefix: %v", err)
	}
	return cell.BeginCell().MustStoreSlice(sl.MustLoadSlice(255), 255).EndCell()
}

// REMMIN/REMMAX are a lookup walk followed by a delete walk in the reference:
// the mid-operation out-of-gas point (and so the reported gas_used) depends on
// that exact load/reload/create order. Sweep gas limits across the whole
// operation and require identical exit and gas at every step.
func TestTVMCrossEmulatorDictRemMinMaxGasOrderParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// 4-key uint dict (keys 0,37,74,111; 8-bit values).
	root := dictWitnessCellFromBOC(t, "b5ee9c72410107010022000201480102020120030402012005060003d4020005a940600005aa80a00005abc0e097b46848")

	for _, op := range []struct {
		name string
		code string
	}{
		{name: "dicturemmin", code: "30F496"},
		{name: "dicturemmax", code: "30F49E"},
		{name: "dictremminref", code: "30F493"},
	} {
		t.Run(op.name, func(t *testing.T) {
			for limit := int64(50); limit <= 1600; limit += 25 {
				assertFlowParityCase(t, op.name, rawCodeCellFromHex(t, op.code), []any{root, int64(8)}, 13, limit)
			}
		})
	}
}

// A slice-key REMMIN materializes the returned key for 500 gas. If the root
// load has already exhausted gas, the reference aborts before that charge.
// Keep a one-leaf witness because integer-key variants do not exercise this
// post-lookup key-materialization boundary.
func TestTVMCrossEmulatorDictRemMinSliceKeyStopsAtRootLoadOOG(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	dict := cell.NewDict(8)
	if err := dict.SetIntKey(big.NewInt(0x12), cell.BeginCell().MustStoreUInt(0x34, 8).EndCell()); err != nil {
		t.Fatalf("build witness dictionary: %v", err)
	}

	assertFlowParityCase(t, "dictremmin-slice-key-root-load-oog", rawCodeCellFromHex(t, "30F492"),
		[]any{dict.AsCell(), int64(8)}, 13, 118)
}

func TestTVMCrossEmulatorPrefixDictWholeKeyForkParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// Root: hml_long label 1010 (4 bits) marked as a fork with two children —
	// the label consumes the whole 4-bit key space.
	root := dictWitnessCellFromBOC(t, "b5ee9c7241010201000a000203a560010100032aa07c79bca4")

	key := func() *cell.Slice {
		return rawCodeCellFromHex(t, "A0").MustBeginParse()
	}

	cases := []struct {
		name  string
		code  string
		stack []any
	}{
		{name: "pfxdictdel-whole-key-fork", code: "30F473", stack: []any{sliceTakeBits(t, key(), 4), root, int64(4)}},
		{name: "pfxdictset-whole-key-fork", code: "30F470", stack: []any{rawCodeCellFromHex(t, "AB").MustBeginParse(), sliceTakeBits(t, key(), 4), root, int64(4)}},
		{name: "pfxdictreplace-whole-key-fork", code: "30F471", stack: []any{rawCodeCellFromHex(t, "AB").MustBeginParse(), sliceTakeBits(t, key(), 4), root, int64(4)}},
		{name: "pfxdictadd-whole-key-fork", code: "30F472", stack: []any{rawCodeCellFromHex(t, "AB").MustBeginParse(), sliceTakeBits(t, key(), 4), root, int64(4)}},
		{name: "pfxdictgetq-whole-key-fork", code: "30F4A8", stack: []any{sliceTakeBits(t, key(), 4), root, int64(4)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFlowParityCase(t, tc.name, rawCodeCellFromHex(t, tc.code), tc.stack, 13, 1_000_000)
		})
	}
}
