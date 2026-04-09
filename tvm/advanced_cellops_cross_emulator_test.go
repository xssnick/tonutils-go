//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorAdvancedCellOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	libraryCell := mustLibraryCell(t)
	libraryBuilder := mustLibraryBuilder(t)
	refCell := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	shortSlice := cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().BeginParse()
	refSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(refCell).EndCell().BeginParse()
	longSlice := cell.BeginCell().MustStoreSlice(make([]byte, 32), 250).EndCell().BeginParse()
	ldrefrtosParent := cell.BeginCell().MustStoreUInt(0xCD, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).EndCell()
	sdsubstrSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).EndCell().BeginParse()
	subsliceRefA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	subsliceRefB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	subsliceSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(subsliceRefA).MustStoreRef(subsliceRefB).EndCell().BeginParse()
	splitSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()
	refsSlice := cell.BeginCell().MustStoreUInt(0b111000, 6).MustStoreRef(subsliceRefA).MustStoreRef(subsliceRefB).EndCell().BeginParse()
	leadingOnes := cell.BeginCell().MustStoreUInt(0b111000, 6).EndCell().BeginParse()
	depthCell := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 1).EndCell()).EndCell()
	compareLeft := cell.BeginCell().MustStoreUInt(0b1010, 4).MustStoreRef(refCell).EndCell().BeginParse()
	compareRight := cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse()
	compareGreater := cell.BeginCell().MustStoreUInt(0b1011, 4).EndCell().BeginParse()
	prefixSlice := cell.BeginCell().MustStoreUInt(0b10, 2).EndCell().BeginParse()
	suffixSlice := cell.BeginCell().MustStoreUInt(0b011, 3).EndCell().BeginParse()
	beginsHay := cell.BeginCell().MustStoreUInt(0b101101, 6).EndCell().BeginParse()
	beginsNeedle := cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().BeginParse()
	beginsMiss := cell.BeginCell().MustStoreUInt(0b111, 3).EndCell().BeginParse()
	leNegTwo := cell.BeginCell().MustStoreSlice([]byte{0xFE, 0xFF, 0xFF, 0xFF}, 32).EndCell().BeginParse()
	leShort := cell.BeginCell().MustStoreSlice([]byte{0xAA, 0xBB, 0xCC, 0xDD}, 32).EndCell().BeginParse()
	hashDepthCell := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 1).EndCell()).EndCell()
	fullRefBuilder := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell())
	fullBitsBuilder := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023)
	bremBuilder := cell.BeginCell().MustStoreUInt(0xA, 4).MustStoreRef(cell.BeginCell().EndCell())
	constSlice := cell.BeginCell().MustStoreUInt(0b10101, 5).MustStoreRef(refCell).EndCell().BeginParse()

	type testCase struct {
		name  string
		code  *cell.Cell
		stack []any
		exit  int32
		c7    tuple.Tuple
	}

	tests := []testCase{
		{
			name: "pushref",
			code: codeFromBuilders(t, stackop.PUSHREF(refCell).Serialize()),
			exit: 0,
		},
		{
			name: "pushslice_inline_short",
			code: codeFromBuilders(t, stackop.PUSHSLICEINLINE(shortSlice).Serialize()),
			exit: 0,
		},
		{
			name: "pushslice_inline_ref",
			code: codeFromBuilders(t, stackop.PUSHSLICEINLINE(refSlice).Serialize()),
			exit: 0,
		},
		{
			name: "pushslice_inline_long",
			code: codeFromBuilders(t, stackop.PUSHSLICEINLINE(longSlice).Serialize()),
			exit: 0,
		},
		{
			name:  "ldrefrtos",
			code:  codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xD5, 8)),
			stack: []any{ldrefrtosParent.BeginParse()},
			exit:  0,
		},
		{
			name:  "sdsubstr",
			code:  codeFromOpcodes(t, 0xD724),
			stack: []any{sdsubstrSlice, int64(1), int64(3)},
			exit:  0,
		},
		{
			name:  "subslice",
			code:  codeFromOpcodes(t, 0xD734),
			stack: []any{subsliceSlice, int64(1), int64(0), int64(3), int64(1)},
			exit:  0,
		},
		{
			name:  "splitq_fail",
			code:  codeFromOpcodes(t, 0xD737),
			stack: []any{splitSlice, int64(8), int64(0)},
			exit:  0,
		},
		{
			name:  "xctos_special",
			code:  codeFromOpcodes(t, 0xD739),
			stack: []any{libraryCell},
			exit:  0,
		},
		{
			name:  "pldrefvar",
			code:  codeFromOpcodes(t, 0xD748),
			stack: []any{refsSlice, int64(1)},
			exit:  0,
		},
		{
			name:  "pldrefidx",
			code:  codeFromBuilders(t, cellsliceop.PLDREFIDX(0).Serialize()),
			stack: []any{refsSlice},
			exit:  0,
		},
		{
			name:  "schkbitrefsq_fail",
			code:  codeFromOpcodes(t, 0xD747),
			stack: []any{refsSlice, int64(7), int64(3)},
			exit:  0,
		},
		{
			name:  "schkbits_underflow",
			code:  codeFromOpcodes(t, 0xD741),
			stack: []any{cell.BeginCell().MustStoreUInt(0, 1).EndCell().BeginParse(), int64(2)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "sbitrefs",
			code:  codeFromOpcodes(t, 0xD74B),
			stack: []any{refsSlice},
			exit:  0,
		},
		{
			name:  "ldsame",
			code:  codeFromOpcodes(t, 0xD762),
			stack: []any{leadingOnes, int64(1)},
			exit:  0,
		},
		{
			name:  "sdepth",
			code:  codeFromOpcodes(t, 0xD764),
			stack: []any{depthCell.BeginParse()},
			exit:  0,
		},
		{
			name:  "cdepth_nil",
			code:  codeFromOpcodes(t, 0xD765),
			stack: []any{nil},
			exit:  0,
		},
		{
			name:  "strefq_fail",
			code:  codeFromOpcodes(t, 0xCF18),
			stack: []any{cell.BeginCell().EndCell(), fullRefBuilder},
			exit:  0,
		},
		{
			name:  "stsliceq_fail",
			code:  codeFromOpcodes(t, 0xCF1A),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 1).EndCell().BeginParse(), fullBitsBuilder},
			exit:  0,
		},
		{
			name:  "stbq_success",
			code:  codeFromOpcodes(t, 0xCF1B),
			stack: []any{cell.BeginCell().MustStoreUInt(0xA, 4), cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "endxc_library",
			code:  codeFromOpcodes(t, 0xCF23),
			stack: []any{libraryBuilder, int64(-1)},
			exit:  0,
		},
		{
			name:  "bchkbitsimmq_success",
			code:  codeFromBuilders(t, cellsliceop.BCHKBITSIMM(8, true).Serialize()),
			stack: []any{cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "bchkbitrefsq_fail",
			code:  codeFromOpcodes(t, 0xCF3F),
			stack: []any{cell.BeginCell(), int64(1023), int64(5)},
			exit:  0,
		},
		{
			name:  "stsame",
			code:  codeFromOpcodes(t, 0xCF42),
			stack: []any{cell.BeginCell(), int64(4), int64(1)},
			exit:  0,
		},
		{
			name:  "sdeq_ignores_refs",
			code:  codeFromBuilders(t, cellsliceop.SDEQ().Serialize()),
			stack: []any{compareLeft, compareRight},
			exit:  0,
		},
		{
			name:  "sdlexcmp",
			code:  codeFromBuilders(t, cellsliceop.SDLEXCMP().Serialize()),
			stack: []any{compareRight, compareGreater},
			exit:  0,
		},
		{
			name:  "sdpfx",
			code:  codeFromBuilders(t, cellsliceop.SDPFX().Serialize()),
			stack: []any{prefixSlice, compareGreater},
			exit:  0,
		},
		{
			name:  "sdsfx",
			code:  codeFromBuilders(t, cellsliceop.SDSFX().Serialize()),
			stack: []any{suffixSlice, compareGreater},
			exit:  0,
		},
		{
			name:  "sdbeginsx",
			code:  codeFromBuilders(t, cellsliceop.SDBEGINSX().Serialize()),
			stack: []any{beginsHay, beginsNeedle},
			exit:  0,
		},
		{
			name:  "sdbeginsxq_fail",
			code:  codeFromBuilders(t, cellsliceop.SDBEGINSXQ().Serialize()),
			stack: []any{beginsHay, beginsMiss},
			exit:  0,
		},
		{
			name:  "ldile4",
			code:  codeFromBuilders(t, cellsliceop.LDILE4().Serialize()),
			stack: []any{leNegTwo},
			exit:  0,
		},
		{
			name:  "pldule8q_fail",
			code:  codeFromBuilders(t, cellsliceop.PLDULE8Q().Serialize()),
			stack: []any{leShort},
			exit:  0,
		},
		{
			name:  "stule4",
			code:  codeFromBuilders(t, cellsliceop.STULE4().Serialize()),
			stack: []any{int64(0x11223344), cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "stile4_rangecheck",
			code:  codeFromBuilders(t, cellsliceop.STILE4().Serialize()),
			stack: []any{int64(1 << 40), cell.BeginCell()},
			exit:  vmerr.CodeRangeCheck,
		},
		{
			name:  "brembitrefs",
			code:  codeFromBuilders(t, cellsliceop.BREMBITREFS().Serialize()),
			stack: []any{bremBuilder},
			exit:  0,
		},
		{
			name:  "strefconst",
			code:  codeFromBuilders(t, cellsliceop.STREFCONST(refCell).Serialize()),
			stack: []any{cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "stref2const",
			code:  codeFromBuilders(t, cellsliceop.STREF2CONST(refCell, subsliceRefA).Serialize()),
			stack: []any{cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "stsliceconst",
			code:  codeFromBuilders(t, cellsliceop.STSLICECONST(constSlice).Serialize()),
			stack: []any{cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "endcst",
			code:  codeFromBuilders(t, cellsliceop.ENDCST().Serialize()),
			stack: []any{cell.BeginCell(), cell.BeginCell().MustStoreUInt(0xAB, 8)},
			exit:  0,
		},
		{
			name:  "chashi",
			code:  codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
			stack: []any{hashDepthCell},
			exit:  0,
		},
		{
			name:  "cdepthi",
			code:  codeFromBuilders(t, cellsliceop.CDEPTHI(0).Serialize()),
			stack: []any{hashDepthCell},
			exit:  0,
		},
		{
			name:  "chashix",
			code:  codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()),
			stack: []any{hashDepthCell, int64(0)},
			exit:  0,
		},
		{
			name:  "cdepthix",
			code:  codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()),
			stack: []any{hashDepthCell, int64(0)},
			exit:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)
			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), tt.c7, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}

			refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), tt.c7, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}

	t.Run("xload_library_resolution", func(t *testing.T) {
		target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
		libraries := mustCrossLibraryCollection(t, target)

		code := prependRawMethodDrop(codeFromOpcodes(t, 0xD73A))
		goStack, err := buildCrossStack(libraryCell)
		if err != nil {
			t.Fatalf("failed to build go stack: %v", err)
		}
		refStack, err := buildCrossStack(libraryCell)
		if err != nil {
			t.Fatalf("failed to build reference stack: %v", err)
		}

		goRes, err := runGoCrossCodeWithLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, []*cell.Cell{libraries}, goStack)
		if err != nil {
			t.Fatalf("go tvm execution failed: %v", err)
		}

		refRes, err := runReferenceCrossCodeWithLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, libraries, refStack)
		if err != nil {
			t.Fatalf("reference tvm execution failed: %v", err)
		}

		if goRes.exitCode != 0 || refRes.exitCode != 0 {
			t.Fatalf("unexpected exit code: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
		}
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
		}

		goStackCell, err := normalizeStackCell(goRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize go stack: %v", err)
		}
		refStackCell, err := normalizeStackCell(refRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize reference stack: %v", err)
		}
		if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
			t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
		}
	})

	t.Run("xloadq_library_resolution", func(t *testing.T) {
		target := cell.BeginCell().MustStoreUInt(0xABCD, 16).EndCell()
		libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
		libraries := mustCrossLibraryCollection(t, target)

		code := prependRawMethodDrop(codeFromOpcodes(t, 0xD73B))
		goStack, err := buildCrossStack(libraryCell)
		if err != nil {
			t.Fatalf("failed to build go stack: %v", err)
		}
		refStack, err := buildCrossStack(libraryCell)
		if err != nil {
			t.Fatalf("failed to build reference stack: %v", err)
		}

		goRes, err := runGoCrossCodeWithLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, []*cell.Cell{libraries}, goStack)
		if err != nil {
			t.Fatalf("go tvm execution failed: %v", err)
		}

		refRes, err := runReferenceCrossCodeWithLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, libraries, refStack)
		if err != nil {
			t.Fatalf("reference tvm execution failed: %v", err)
		}

		if goRes.exitCode != 0 || refRes.exitCode != 0 {
			t.Fatalf("unexpected exit code: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
		}
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
		}

		goStackCell, err := normalizeStackCell(goRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize go stack: %v", err)
		}
		refStackCell, err := normalizeStackCell(refRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize reference stack: %v", err)
		}
		if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
			t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
		}
	})

	t.Run("xloadq_library_missing", func(t *testing.T) {
		missingLib := mustCrossLibraryCellForHash(t, refCell.Hash())
		code := prependRawMethodDrop(codeFromOpcodes(t, 0xD73B))
		goStack, err := buildCrossStack(missingLib)
		if err != nil {
			t.Fatalf("failed to build go stack: %v", err)
		}
		refStack, err := buildCrossStack(missingLib)
		if err != nil {
			t.Fatalf("failed to build reference stack: %v", err)
		}

		goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack)
		if err != nil {
			t.Fatalf("go tvm execution failed: %v", err)
		}
		refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), tuple.Tuple{}, refStack)
		if err != nil {
			t.Fatalf("reference tvm execution failed: %v", err)
		}

		if goRes.exitCode != 0 || refRes.exitCode != 0 {
			t.Fatalf("unexpected exit code: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
		}
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
		}

		goStackCell, err := normalizeStackCell(goRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize go stack: %v", err)
		}
		refStackCell, err := normalizeStackCell(refRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize reference stack: %v", err)
		}
		if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
			t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
		}
	})
}

func mustLibraryBuilder(t *testing.T) *cell.Builder {
	t.Helper()
	return cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256)
}

func mustLibraryCell(t *testing.T) *cell.Cell {
	t.Helper()
	cl, err := mustLibraryBuilder(t).EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func mustCrossLibraryCellForHash(t *testing.T, hash []byte) *cell.Cell {
	t.Helper()
	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(hash, 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func mustCrossLibraryCollection(t *testing.T, entries ...*cell.Cell) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(256)
	for _, entry := range entries {
		key := cell.BeginCell().MustStoreSlice(entry.Hash(), 256).EndCell()
		if _, err := dict.SetRefWithMode(key, entry, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to add library entry: %v", err)
		}
	}
	return dict.AsCell()
}
