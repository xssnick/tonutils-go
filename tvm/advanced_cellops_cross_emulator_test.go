//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const advancedCellOpsParityCaseCount = 81

type advancedCellOpsParityCase struct {
	name  string
	code  *cell.Cell
	stack []any
	exit  int32
	c7    tuple.Tuple
	libs  *cell.Cell
}

func TestTVMCrossEmulatorAdvancedCellOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := advancedCellOpsParityCases(t)
	if len(tests) != advancedCellOpsParityCaseCount {
		t.Fatalf("advanced cellops parity case count = %d, want %d", len(tests), advancedCellOpsParityCaseCount)
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runAdvancedCellOpsParityCase(t, tt)
		})
	}
}

func TestTVMCrossEmulatorAdvancedCellOpsAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := advancedCellOpsParityCases(t)
	if len(tests) != advancedCellOpsParityCaseCount {
		t.Fatalf("advanced cellops parity case count = %d, want %d", len(tests), advancedCellOpsParityCaseCount)
	}
	versions := crossEmulatorVersionAuditVersions(t, "TVM_ADVANCED_CELLOPS_CORE_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runAdvancedCellOpsVersionedParityCaseWithoutExpected(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorAdvancedCellOpsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%advancedCellOpsParityCaseCount))
	}
	for i := 0; i < advancedCellOpsParityCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := advancedCellOpsParityCases(t)
		if len(tests) != advancedCellOpsParityCaseCount {
			t.Fatalf("advanced cellops parity case count = %d, want %d", len(tests), advancedCellOpsParityCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runAdvancedCellOpsVersionedParityCaseWithoutExpected(t, tt, version)
	})
}

func advancedCellOpsParityCases(t *testing.T) []advancedCellOpsParityCase {
	t.Helper()

	libraryCell := mustLibraryCell(t)
	libraryBuilder := mustLibraryBuilder(t)
	refCell := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	shortSlice := cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().MustBeginParse()
	refSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(refCell).EndCell().MustBeginParse()
	longSlice := cell.BeginCell().MustStoreSlice(make([]byte, 32), 250).EndCell().MustBeginParse()
	ldrefrtosParent := cell.BeginCell().MustStoreUInt(0xCD, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).EndCell()
	sdsubstrSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).EndCell().MustBeginParse()
	subsliceRefA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	subsliceRefB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	subsliceSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(subsliceRefA).MustStoreRef(subsliceRefB).EndCell().MustBeginParse()
	cutSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(subsliceRefA).MustStoreRef(subsliceRefB).EndCell().MustBeginParse()
	splitSlice := cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(cell.BeginCell().EndCell()).EndCell().MustBeginParse()
	refsSlice := cell.BeginCell().MustStoreUInt(0b111000, 6).MustStoreRef(subsliceRefA).MustStoreRef(subsliceRefB).EndCell().MustBeginParse()
	leadingOnes := cell.BeginCell().MustStoreUInt(0b111000, 6).EndCell().MustBeginParse()
	depthCell := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).EndCell()
	compareLeft := cell.BeginCell().MustStoreUInt(0b1010, 4).MustStoreRef(refCell).EndCell().MustBeginParse()
	compareRight := cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().MustBeginParse()
	compareGreater := cell.BeginCell().MustStoreUInt(0b1011, 4).EndCell().MustBeginParse()
	prefixSlice := cell.BeginCell().MustStoreUInt(0b10, 2).EndCell().MustBeginParse()
	suffixSlice := cell.BeginCell().MustStoreUInt(0b011, 3).EndCell().MustBeginParse()
	beginsHay := cell.BeginCell().MustStoreUInt(0b101101, 6).EndCell().MustBeginParse()
	beginsNeedle := cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().MustBeginParse()
	beginsMiss := cell.BeginCell().MustStoreUInt(0b111, 3).EndCell().MustBeginParse()
	leNegTwo := cell.BeginCell().MustStoreSlice([]byte{0xFE, 0xFF, 0xFF, 0xFF}, 32).EndCell().MustBeginParse()
	leShort := cell.BeginCell().MustStoreSlice([]byte{0xAA, 0xBB, 0xCC, 0xDD}, 32).EndCell().MustBeginParse()
	hashDepthCell := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).EndCell()
	proofRoot := cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreRef(cell.BeginCell().
			MustStoreUInt(0xBEEF, 16).
			MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
			EndCell()).
		EndCell()
	proofCell := mustUsageProofWithLoadedRoot(t, proofRoot)
	proofBody, err := cell.UnwrapProof(proofCell, proofRoot.Hash())
	if err != nil {
		t.Fatalf("unwrap proof cell: %v", err)
	}
	prunedRef, err := proofBody.PeekRef(0)
	if err != nil {
		t.Fatalf("peek pruned proof ref: %v", err)
	}
	if prunedRef.GetType() != cell.PrunedCellType {
		t.Fatalf("expected pruned proof ref, got %v", prunedRef.GetType())
	}
	lazyParent := mustCrossLazyParentWithLoadedRef(t)
	libraryTarget := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	libraryRef := mustCrossLibraryCellForHash(t, libraryTarget.Hash())
	libraryCollection := mustCrossLibraryCollection(t, libraryTarget)
	missingLibraryRef := mustCrossLibraryCellForHash(t, refCell.Hash())
	fullRefBuilder := cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreRef(cell.BeginCell().EndCell())
	fullBitsBuilder := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1023)
	bremBuilder := cell.BeginCell().MustStoreUInt(0xA, 4).MustStoreRef(cell.BeginCell().EndCell())
	constSlice := cell.BeginCell().MustStoreUInt(0b10101, 5).MustStoreRef(refCell).EndCell().MustBeginParse()
	storeBuilderSrc := cell.BeginCell().MustStoreUInt(0xA, 4)
	storeBuilderDst := cell.BeginCell().MustStoreUInt(0xB, 4)
	storeSlice := cell.BeginCell().MustStoreUInt(0xC, 4).MustStoreRef(refCell).EndCell().MustBeginParse()

	return []advancedCellOpsParityCase{
		{
			name: "pushref",
			code: codeFromBuilders(t, stackop.PUSHREF(refCell).Serialize()),
			exit: 0,
		},
		{
			name: "code_ref_xctos_merkle_proof_special",
			code: codeFromBuilders(t,
				stackop.PUSHREF(proofCell).Serialize(),
				cellsliceop.XCTOS().Serialize(),
			),
			exit: 0,
		},
		{
			name: "code_ref_ctos_merkle_proof_underflow",
			code: codeFromBuilders(t,
				stackop.PUSHREF(proofCell).Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "code_ref_xctos_pruned_special",
			code: codeFromBuilders(t, append(crossProofPrunedCellCode(proofCell), cellsliceop.XCTOS().Serialize())...),
			exit: 0,
		},
		{
			name: "code_ref_ctos_pruned_underflow",
			code: codeFromBuilders(t, append(crossProofPrunedCellCode(proofCell), cellsliceop.CTOS().Serialize())...),
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "code_ref_xload_pruned_underflow",
			code: codeFromBuilders(t, append(crossProofPrunedCellCode(proofCell), cellsliceop.XLOAD().Serialize())...),
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "code_ref_xloadq_pruned_false",
			code: codeFromBuilders(t, append(crossProofPrunedCellCode(proofCell), cellsliceop.XLOADQ().Serialize())...),
			exit: 0,
		},
		{
			name: "code_ref_ldref_rtos_pruned_child_underflow",
			code: codeFromBuilders(t, crossProofPrunedChildLoadCode(proofCell)...),
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "code_ref_ldref_rtos_lazy_child",
			code: codeFromBuilders(t,
				stackop.PUSHREF(lazyParent).Serialize(),
				cellsliceop.CTOS().Serialize(),
				cellsliceop.LDREFRTOS().Serialize(),
			),
			exit: 0,
		},
		{
			name: "code_ref_pldrefidx_lazy_child",
			code: codeFromBuilders(t,
				stackop.PUSHREF(lazyParent).Serialize(),
				cellsliceop.CTOS().Serialize(),
				cellsliceop.PLDREFIDX(0).Serialize(),
			),
			exit: 0,
		},
		{
			name: "code_ref_xload_library_resolution",
			code: codeFromBuilders(t,
				stackop.PUSHREF(libraryRef).Serialize(),
				cellsliceop.XLOAD().Serialize(),
			),
			exit: 0,
			libs: libraryCollection,
		},
		{
			name: "code_ref_xload_library_missing_underflow",
			code: codeFromBuilders(t,
				stackop.PUSHREF(missingLibraryRef).Serialize(),
				cellsliceop.XLOAD().Serialize(),
			),
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "code_ref_xloadq_library_missing_false",
			code: codeFromBuilders(t,
				stackop.PUSHREF(missingLibraryRef).Serialize(),
				cellsliceop.XLOADQ().Serialize(),
			),
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
			stack: []any{ldrefrtosParent.MustBeginParse()},
			exit:  0,
		},
		{
			name:  "sdsubstr",
			code:  codeFromOpcodes(t, 0xD724),
			stack: []any{sdsubstrSlice, int64(1), int64(3)},
			exit:  0,
		},
		{
			name:  "sdcutfirst",
			code:  codeFromBuilders(t, cellsliceop.SDCUTFIRST().Serialize()),
			stack: []any{cutSlice, int64(3)},
			exit:  0,
		},
		{
			name:  "sdskipfirst",
			code:  codeFromBuilders(t, cellsliceop.SDSKIPFIRST().Serialize()),
			stack: []any{cutSlice, int64(2)},
			exit:  0,
		},
		{
			name:  "sdcutlast",
			code:  codeFromBuilders(t, cellsliceop.SDCUTLAST().Serialize()),
			stack: []any{cutSlice, int64(4)},
			exit:  0,
		},
		{
			name:  "sdskiplast",
			code:  codeFromBuilders(t, cellsliceop.SDSKIPLAST().Serialize()),
			stack: []any{cutSlice, int64(2)},
			exit:  0,
		},
		{
			name:  "sdcutfirst_underflow",
			code:  codeFromBuilders(t, cellsliceop.SDCUTFIRST().Serialize()),
			stack: []any{cutSlice, int64(7)},
			exit:  vmerr.CodeCellUnderflow,
		},
		{
			name:  "scutfirst_bits_refs",
			code:  codeFromBuilders(t, cellsliceop.SCUTFIRST().Serialize()),
			stack: []any{cutSlice, int64(4), int64(1)},
			exit:  0,
		},
		{
			name:  "sskipfirst_bits_refs",
			code:  codeFromBuilders(t, cellsliceop.SSKIPFIRST().Serialize()),
			stack: []any{cutSlice, int64(2), int64(1)},
			exit:  0,
		},
		{
			name:  "scutlast_bits_refs",
			code:  codeFromBuilders(t, cellsliceop.SCUTLAST().Serialize()),
			stack: []any{cutSlice, int64(4), int64(1)},
			exit:  0,
		},
		{
			name:  "sskiplast_bits_refs",
			code:  codeFromBuilders(t, cellsliceop.SSKIPLAST().Serialize()),
			stack: []any{cutSlice, int64(2), int64(1)},
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
			stack: []any{cell.BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse(), int64(2)},
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
			stack: []any{depthCell.MustBeginParse()},
			exit:  0,
		},
		{
			name:  "cdepth_nil",
			code:  codeFromOpcodes(t, 0xD765),
			stack: []any{nil},
			exit:  0,
		},
		{
			name:  "clevel_proof",
			code:  codeFromBuilders(t, cellsliceop.CLEVEL().Serialize()),
			stack: []any{proofCell},
			exit:  0,
		},
		{
			name:  "clevelmask_proof",
			code:  codeFromBuilders(t, cellsliceop.CLEVELMASK().Serialize()),
			stack: []any{proofCell},
			exit:  0,
		},
		{
			name:  "cdepth_proof",
			code:  codeFromBuilders(t, cellsliceop.CDEPTH().Serialize()),
			stack: []any{proofCell},
			exit:  0,
		},
		{
			name:  "chashi_proof_lvl0",
			code:  codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
			stack: []any{proofCell},
			exit:  0,
		},
		{
			name:  "cdepthi_proof_lvl0",
			code:  codeFromBuilders(t, cellsliceop.CDEPTHI(0).Serialize()),
			stack: []any{proofCell},
			exit:  0,
		},
		{
			name:  "strefq_fail",
			code:  codeFromOpcodes(t, 0xCF18),
			stack: []any{cell.BeginCell().EndCell(), fullRefBuilder},
			exit:  0,
		},
		{
			name:  "stb_success",
			code:  codeFromBuilders(t, cellsliceop.STB().Serialize()),
			stack: []any{storeBuilderSrc, storeBuilderDst},
			exit:  0,
		},
		{
			name:  "stslice_success",
			code:  codeFromBuilders(t, cellsliceop.STSLICE().Serialize()),
			stack: []any{storeSlice, cell.BeginCell().MustStoreUInt(0xD, 4)},
			exit:  0,
		},
		{
			name:  "stbref_success",
			code:  codeFromBuilders(t, cellsliceop.STBREF().Serialize()),
			stack: []any{storeBuilderSrc, storeBuilderDst},
			exit:  0,
		},
		{
			name:  "stbrefq_fail",
			code:  codeFromBuilders(t, cellsliceop.STBREFQ().Serialize()),
			stack: []any{storeBuilderSrc, fullRefBuilder},
			exit:  0,
		},
		{
			name:  "stbref_r_success",
			code:  codeFromBuilders(t, cellsliceop.STBREFR().Serialize()),
			stack: []any{storeBuilderDst, storeBuilderSrc},
			exit:  0,
		},
		{
			name:  "stref_r_success",
			code:  codeFromBuilders(t, cellsliceop.STREFR().Serialize()),
			stack: []any{storeBuilderDst, refCell},
			exit:  0,
		},
		{
			name:  "stslicer_success",
			code:  codeFromBuilders(t, cellsliceop.STSLICER().Serialize()),
			stack: []any{storeBuilderDst, storeSlice},
			exit:  0,
		},
		{
			name:  "stbr_success",
			code:  codeFromBuilders(t, cellsliceop.STBR().Serialize()),
			stack: []any{storeBuilderDst, storeBuilderSrc},
			exit:  0,
		},
		{
			name:  "stsliceq_fail",
			code:  codeFromOpcodes(t, 0xCF1A),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 1).EndCell().MustBeginParse(), fullBitsBuilder},
			exit:  0,
		},
		{
			name:  "stbq_success",
			code:  codeFromOpcodes(t, 0xCF1B),
			stack: []any{cell.BeginCell().MustStoreUInt(0xA, 4), cell.BeginCell()},
			exit:  0,
		},
		{
			name:  "strefrq_fail",
			code:  codeFromBuilders(t, cellsliceop.STREFRQ().Serialize()),
			stack: []any{fullRefBuilder, refCell},
			exit:  0,
		},
		{
			name:  "stbrefrq_fail",
			code:  codeFromBuilders(t, cellsliceop.STBREFRQ().Serialize()),
			stack: []any{fullRefBuilder, storeBuilderSrc},
			exit:  0,
		},
		{
			name:  "stslicerq_fail",
			code:  codeFromBuilders(t, cellsliceop.STSLICERQ().Serialize()),
			stack: []any{fullBitsBuilder, storeSlice},
			exit:  0,
		},
		{
			name:  "stbrq_fail",
			code:  codeFromBuilders(t, cellsliceop.STBRQ().Serialize()),
			stack: []any{fullBitsBuilder, storeBuilderSrc},
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

}

func runAdvancedCellOpsParityCase(t *testing.T, tt advancedCellOpsParityCase) {
	t.Helper()

	runAdvancedCellOpsParityCaseWithExpected(t, tt, 0, false, &tt.exit)
}

func runAdvancedCellOpsVersionedParityCaseWithoutExpected(t *testing.T, tt advancedCellOpsParityCase, version int) {
	t.Helper()

	runAdvancedCellOpsParityCaseWithExpected(t, tt, version, true, nil)
}

func runAdvancedCellOpsParityCaseWithExpected(t *testing.T, tt advancedCellOpsParityCase, version int, versioned bool, wantExit *int32) {
	t.Helper()

	code := prependRawMethodDrop(tt.code)
	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	var goLibs []*cell.Cell
	if tt.libs != nil {
		goLibs = []*cell.Cell{tt.libs}
	}

	var goRes *crossRunResult
	var refRes *crossRunResult
	if versioned {
		goRes, err = runGoCrossCodeWithVersionAndLibs(code, cell.BeginCell().EndCell(), tt.c7, goLibs, goStack, version)
	} else {
		goRes, err = runGoCrossCodeWithLibs(code, cell.BeginCell().EndCell(), tt.c7, goLibs, goStack)
	}
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	if versioned {
		refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
		refCfg.Libs = tt.libs
		refRes, err = runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	} else {
		refRes, err = runReferenceCrossCodeWithLibs(code, cell.BeginCell().EndCell(), tt.c7, tt.libs, refStack)
	}
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if wantExit != nil && (goRes.exitCode != *wantExit || refRes.exitCode != *wantExit) {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, *wantExit)
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
}

func TestTVMCrossEmulatorAdvancedCellOpsLibraryResolution(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	refCell := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()

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

	t.Run("ctos_library_resolution_gas", func(t *testing.T) {
		target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
		libraries := mustCrossLibraryCollection(t, target)

		code := prependRawMethodDrop(codeFromBuilders(t, cellsliceop.CTOS().Serialize()))
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

func TestTVMCrossEmulatorAdvancedCellOpsVersionedEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
	libraries := mustCrossLibraryCollection(t, target)
	missingLibraryCell := mustCrossLibraryCellForHash(t, cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell().Hash())
	shortSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	withRefSlice := cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()).
		EndCell().
		MustBeginParse()
	builderToSlice := cell.BeginCell().
		MustStoreUInt(0xABCD, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell())
	hashDepthCell := cell.BeginCell().
		MustStoreUInt(1, 1).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).
		EndCell()

	tests := []advancedCellVersionedCase{
		{
			name:    "v4_xload_library_keeps_special_cell",
			version: 4,
			code:    codeFromBuilders(t, cellsliceop.XLOAD().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v5_xload_library_resolves",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.XLOAD().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v0_ctos_library_resolves",
			version: 0,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v1_ctos_library_resolves",
			version: 1,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v2_ctos_library_resolves",
			version: 2,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v3_ctos_library_resolves",
			version: 3,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v4_ctos_library_resolves",
			version: 4,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v0_ctos_code_ref_library_resolves",
			version: 0,
			code: codeFromBuilders(t,
				stackop.PUSHREF(libraryCell).Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			libs: libraries,
			exit: 0,
		},
		{
			name:    "v4_ctos_code_ref_library_resolves",
			version: 4,
			code: codeFromBuilders(t,
				stackop.PUSHREF(libraryCell).Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			libs: libraries,
			exit: 0,
		},
		{
			name:    "v5_ctos_code_ref_library_resolves",
			version: 5,
			code: codeFromBuilders(t,
				stackop.PUSHREF(libraryCell).Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			libs: libraries,
			exit: 0,
		},
		{
			name:    "v0_ctos_library_resolves_twice",
			version: 0,
			code: codeFromBuilders(t,
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			stack: []any{libraryCell, libraryCell},
			libs:  libraries,
			exit:  0,
		},
		{
			name:    "v4_ctos_library_resolves_twice",
			version: 4,
			code: codeFromBuilders(t,
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			stack: []any{libraryCell, libraryCell},
			libs:  libraries,
			exit:  0,
		},
		{
			name:    "v5_ctos_library_resolves_twice",
			version: 5,
			code: codeFromBuilders(t,
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			stack: []any{libraryCell, libraryCell},
			libs:  libraries,
			exit:  0,
		},
		{
			name:    "v5_ctos_library_resolves",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v13_ctos_library_resolves",
			version: 13,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    "v4_xload_missing_library_still_succeeds",
			version: 4,
			code:    codeFromBuilders(t, cellsliceop.XLOAD().Serialize()),
			stack:   []any{missingLibraryCell},
			exit:    0,
		},
		{
			name:    "v5_xload_missing_library_underflow",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.XLOAD().Serialize()),
			stack:   []any{missingLibraryCell},
			exit:    vmerr.CodeCellUnderflow,
		},
		{
			name:    "v5_xloadq_missing_library_false",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.XLOADQ().Serialize()),
			stack:   []any{missingLibraryCell},
			exit:    0,
		},
		{
			name:    "v11_btos_rejected",
			version: 11,
			code:    codeFromBuilders(t, cellsliceop.BTOS().Serialize()),
			stack:   []any{builderToSlice},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v12_btos_allowed",
			version: 12,
			code:    codeFromBuilders(t, cellsliceop.BTOS().Serialize()),
			stack:   []any{builderToSlice},
			exit:    0,
		},
		{
			name:    "v5_clevel_rejected",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CLEVEL().Serialize()),
			stack:   []any{hashDepthCell},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v6_clevel_allowed",
			version: 6,
			code:    codeFromBuilders(t, cellsliceop.CLEVEL().Serialize()),
			stack:   []any{hashDepthCell},
			exit:    0,
		},
		{
			name:    "v5_clevelmask_rejected",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CLEVELMASK().Serialize()),
			stack:   []any{hashDepthCell},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v6_clevelmask_allowed",
			version: 6,
			code:    codeFromBuilders(t, cellsliceop.CLEVELMASK().Serialize()),
			stack:   []any{hashDepthCell},
			exit:    0,
		},
		{
			name:    "v5_chashi_rejected",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
			stack:   []any{hashDepthCell},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v6_chashi_allowed",
			version: 6,
			code:    codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
			stack:   []any{hashDepthCell},
			exit:    0,
		},
		{
			name:    "v5_cdepthi_rejected",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CDEPTHI(0).Serialize()),
			stack:   []any{hashDepthCell},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v6_cdepthi_allowed",
			version: 6,
			code:    codeFromBuilders(t, cellsliceop.CDEPTHI(0).Serialize()),
			stack:   []any{hashDepthCell},
			exit:    0,
		},
		{
			name:    "v5_chashix_rejected",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()),
			stack:   []any{hashDepthCell, int64(0)},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v6_chashix_allowed",
			version: 6,
			code:    codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()),
			stack:   []any{hashDepthCell, int64(0)},
			exit:    0,
		},
		{
			name:    "v5_cdepthix_rejected",
			version: 5,
			code:    codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()),
			stack:   []any{hashDepthCell, int64(0)},
			exit:    vmerr.CodeInvalidOpcode,
		},
		{
			name:    "v6_cdepthix_allowed",
			version: 6,
			code:    codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()),
			stack:   []any{hashDepthCell, int64(0)},
			exit:    0,
		},
		{
			name:    "v0_schkbitsq_false",
			version: 0,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITSQ().Serialize()),
			stack:   []any{shortSlice, int64(16)},
			exit:    0,
		},
		{
			name:    "v0_schkbits_nonquiet_underflow",
			version: 0,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITS().Serialize()),
			stack:   []any{shortSlice, int64(16)},
			exit:    vmerr.CodeCellUnderflow,
		},
		{
			name:    "v0_schkbitrefsq_refs_range",
			version: 0,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()),
			stack:   []any{withRefSlice, int64(8), int64(5)},
			exit:    vmerr.CodeRangeCheck,
		},
		{
			name:    "v13_schkbitrefsq_success",
			version: 13,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()),
			stack:   []any{withRefSlice, int64(8), int64(1)},
			exit:    0,
		},
	}
	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_ADVANCED_CELLOPS_VERSION_AUDIT") {
		tests = append(tests, advancedCellOpsGeneratedVersionCases(t, version)...)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAdvancedCellVersionedParityCase(t, tt)
		})
	}
}

func FuzzTVMCrossEmulatorAdvancedCellOpsGeneratedGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%advancedCellOpsGeneratedVersionCaseCount))
	}
	for i := 0; i < advancedCellOpsGeneratedVersionCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := advancedCellOpsGeneratedVersionCases(t, version)
		if len(tests) != advancedCellOpsGeneratedVersionCaseCount {
			t.Fatalf("advanced cellops generated case count = %d, want %d", len(tests), advancedCellOpsGeneratedVersionCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runAdvancedCellVersionedParityCase(t, tt)
	})
}

const advancedCellOpsGeneratedVersionCaseCount = 17

type advancedCellVersionedCase struct {
	name    string
	version int
	code    *cell.Cell
	stack   []any
	libs    *cell.Cell
	exit    int32
}

func advancedCellOpsGeneratedVersionCases(t *testing.T, version int) []advancedCellVersionedCase {
	t.Helper()

	target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	libraryCell := mustCrossLibraryCellForHash(t, target.Hash())
	libraries := mustCrossLibraryCollection(t, target)
	missingLibraryCell := mustCrossLibraryCellForHash(t, cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell().Hash())
	shortSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	withRefSlice := cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()).
		EndCell().
		MustBeginParse()
	builderToSlice := cell.BeginCell().
		MustStoreUInt(0xABCD, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell())
	hashDepthCell := cell.BeginCell().
		MustStoreUInt(1, 1).
		MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).
		EndCell()

	versionExit := func(minVersion int) int32 {
		if version < minVersion {
			return int32(vmerr.CodeInvalidOpcode)
		}
		return 0
	}
	xloadMissingExit := int32(0)
	if version >= 5 {
		xloadMissingExit = int32(vmerr.CodeCellUnderflow)
	}

	return []advancedCellVersionedCase{
		{
			name:    fmt.Sprintf("xload_library_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.XLOAD().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    fmt.Sprintf("ctos_library_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CTOS().Serialize()),
			stack:   []any{libraryCell},
			libs:    libraries,
			exit:    0,
		},
		{
			name:    fmt.Sprintf("ctos_code_ref_library_v%d", version),
			version: version,
			code: codeFromBuilders(t,
				stackop.PUSHREF(libraryCell).Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			libs: libraries,
			exit: 0,
		},
		{
			name:    fmt.Sprintf("ctos_library_twice_v%d", version),
			version: version,
			code: codeFromBuilders(t,
				cellsliceop.CTOS().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.CTOS().Serialize(),
			),
			stack: []any{libraryCell, libraryCell},
			libs:  libraries,
			exit:  0,
		},
		{
			name:    fmt.Sprintf("xload_missing_library_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.XLOAD().Serialize()),
			stack:   []any{missingLibraryCell},
			exit:    xloadMissingExit,
		},
		{
			name:    fmt.Sprintf("xloadq_missing_library_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.XLOADQ().Serialize()),
			stack:   []any{missingLibraryCell},
			exit:    0,
		},
		{
			name:    fmt.Sprintf("btos_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.BTOS().Serialize()),
			stack:   []any{builderToSlice},
			exit:    versionExit(12),
		},
		{
			name:    fmt.Sprintf("clevel_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CLEVEL().Serialize()),
			stack:   []any{hashDepthCell},
			exit:    versionExit(6),
		},
		{
			name:    fmt.Sprintf("clevelmask_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CLEVELMASK().Serialize()),
			stack:   []any{hashDepthCell},
			exit:    versionExit(6),
		},
		{
			name:    fmt.Sprintf("chashi_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()),
			stack:   []any{hashDepthCell},
			exit:    versionExit(6),
		},
		{
			name:    fmt.Sprintf("cdepthi_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CDEPTHI(0).Serialize()),
			stack:   []any{hashDepthCell},
			exit:    versionExit(6),
		},
		{
			name:    fmt.Sprintf("chashix_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CHASHIX().Serialize()),
			stack:   []any{hashDepthCell, int64(0)},
			exit:    versionExit(6),
		},
		{
			name:    fmt.Sprintf("cdepthix_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()),
			stack:   []any{hashDepthCell, int64(0)},
			exit:    versionExit(6),
		},
		{
			name:    fmt.Sprintf("schkbitsq_false_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITSQ().Serialize()),
			stack:   []any{shortSlice, int64(16)},
			exit:    0,
		},
		{
			name:    fmt.Sprintf("schkbits_nonquiet_underflow_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITS().Serialize()),
			stack:   []any{shortSlice, int64(16)},
			exit:    int32(vmerr.CodeCellUnderflow),
		},
		{
			name:    fmt.Sprintf("schkbitrefsq_refs_range_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()),
			stack:   []any{withRefSlice, int64(8), int64(5)},
			exit:    int32(vmerr.CodeRangeCheck),
		},
		{
			name:    fmt.Sprintf("schkbitrefsq_success_v%d", version),
			version: version,
			code:    codeFromBuilders(t, cellsliceop.SCHKBITREFSQ().Serialize()),
			stack:   []any{withRefSlice, int64(8), int64(1)},
			exit:    0,
		},
	}
}

func runAdvancedCellVersionedParityCase(t *testing.T, tt advancedCellVersionedCase) {
	t.Helper()

	code := prependRawMethodDrop(tt.code)
	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	var goLibs []*cell.Cell
	if tt.libs != nil {
		goLibs = []*cell.Cell{tt.libs}
	}
	goRes, err := runGoCrossCodeWithVersionAndLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goLibs, goStack, tt.version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(tt.version)))
	refCfg.Libs = tt.libs
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
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
		if _, err := dict.SetBuilderWithMode(key, cell.BeginCell().MustStoreRef(entry), cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to add library entry: %v", err)
		}
	}
	return dict.AsCell()
}

func crossProofBodyCellCode(proof *cell.Cell) []*cell.Builder {
	return []*cell.Builder{
		stackop.PUSHREF(proof).Serialize(),
		cellsliceop.XCTOS().Serialize(),
		stackop.DROP().Serialize(),
		cellsliceop.LDREF().Serialize(),
		stackop.DROP().Serialize(),
	}
}

func crossProofPrunedCellCode(proof *cell.Cell) []*cell.Builder {
	return append(crossProofBodyCellCode(proof),
		cellsliceop.CTOS().Serialize(),
		cellsliceop.LDREF().Serialize(),
		stackop.DROP().Serialize(),
	)
}

func crossProofPrunedChildLoadCode(proof *cell.Cell) []*cell.Builder {
	return append(crossProofBodyCellCode(proof),
		cellsliceop.CTOS().Serialize(),
		cellsliceop.LDREFRTOS().Serialize(),
	)
}

func mustCrossLazyParentWithLoadedRef(t *testing.T) *cell.Cell {
	t.Helper()

	child := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xA5, 8).MustStoreRef(child).EndCell()
	boc, err := root.ToBOCWithOptionsErr(cell.BOCSerializeOptions{
		WithCRC32C:    true,
		WithIndex:     true,
		WithCacheBits: true,
		WithTopHash:   true,
		WithIntHashes: true,
	})
	if err != nil {
		t.Fatalf("failed to serialize lazy fixture: %v", err)
	}

	roots, _, err := cell.FromBOCMultiRootReader(bytes.NewReader(boc), cell.BOCParseOptions{
		TrustedHashes: true,
		Lazy:          true,
	})
	if err != nil {
		t.Fatalf("failed to lazy parse fixture: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("unexpected lazy fixture root count: %d", len(roots))
	}

	meta := roots[0].GetMetadata()
	if len(meta.Refs) != 1 || !meta.Refs[0].Lazy {
		t.Fatal("expected lazy fixture to keep its child behind a lazy pruned ref")
	}
	return roots[0]
}
