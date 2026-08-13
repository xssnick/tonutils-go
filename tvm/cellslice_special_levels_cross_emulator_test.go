//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"math/big"
	"math/bits"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func wrapCellLevelToZero(t *testing.T, cl *cell.Cell) (*cell.Cell, int) {
	t.Helper()

	root := cl
	levels := 0
	for root.Level() > 0 {
		level := root.Level()
		wrapped, err := cell.CreateMerkleProof(root)
		if err != nil {
			t.Fatalf("wrap level-%d cell: %v", level, err)
		}
		root = wrapped
		levels++
	}
	return root, levels
}

func revealWrappedCellCode(root *cell.Cell, levels int) []*cell.Builder {
	code := []*cell.Builder{stackop.PUSHREF(root).Serialize()}
	for range levels {
		code = append(code,
			cellsliceop.XCTOS().Serialize(),
			stackop.DROP().Serialize(),
			cellsliceop.PLDREFIDX(0).Serialize(),
		)
	}
	return code
}

func TestTVMCrossEmulatorCellSliceSpecialLevels(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	branch := cell.BeginCell().
		MustStoreUInt(0xA5, 8).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()).
		EndCell()
	pruned := make([]*cell.Cell, 8)
	prunedSources := make([]*cell.Cell, 8)
	prunedSources[0] = branch
	for mask := 1; mask <= 7; mask++ {
		level := bits.Len8(uint8(mask))
		lowerMask := mask &^ (1 << (level - 1))
		var err error
		pruned[mask], err = cell.CreatePrunedBranch(prunedSources[lowerMask], level, 3)
		if err != nil {
			t.Fatalf("create mask-%03b pruned branch: %v", mask, err)
		}
		if got := int(pruned[mask].LevelMask().Mask); got != mask {
			t.Fatalf("pruned level mask = %03b, want %03b", got, mask)
		}
		prunedSources[mask] = cell.BeginCell().MustStoreRef(pruned[mask]).EndCell()
	}

	proof, err := cell.CreateMerkleProof(pruned[7])
	if err != nil {
		t.Fatalf("create Merkle proof: %v", err)
	}
	update, err := cell.CreateMerkleUpdate(pruned[7], pruned[6])
	if err != nil {
		t.Fatalf("create Merkle update: %v", err)
	}

	var tests []advancedCellOpsParityCase
	for mask := 1; mask <= 7; mask++ {
		level := pruned[mask].Level()
		wrapped, wrappers := wrapCellLevelToZero(t, pruned[mask])
		reveal := revealWrappedCellCode(wrapped, wrappers)
		for hashLevel := 0; hashLevel <= 3; hashLevel++ {
			tests = append(tests,
				advancedCellOpsParityCase{
					name: fmt.Sprintf("pruned_mask%03b_level%d_chashi_%d", mask, level, hashLevel),
					code: codeFromBuilders(t, append(reveal, cellsliceop.CHASHI(hashLevel).Serialize())...),
					exit: 0,
				},
				advancedCellOpsParityCase{
					name: fmt.Sprintf("pruned_mask%03b_level%d_cdepthi_%d", mask, level, hashLevel),
					code: codeFromBuilders(t, append(reveal, cellsliceop.CDEPTHI(hashLevel).Serialize())...),
					exit: 0,
				},
				advancedCellOpsParityCase{
					name: fmt.Sprintf("pruned_mask%03b_level%d_chashix_%d", mask, level, hashLevel),
					code: codeFromBuilders(t, append(reveal,
						stackop.PUSHINT(big.NewInt(int64(hashLevel))).Serialize(),
						cellsliceop.CHASHIX().Serialize(),
					)...),
					exit: 0,
				},
				advancedCellOpsParityCase{
					name: fmt.Sprintf("pruned_mask%03b_level%d_cdepthix_%d", mask, level, hashLevel),
					code: codeFromBuilders(t, append(reveal,
						stackop.PUSHINT(big.NewInt(int64(hashLevel))).Serialize(),
						cellsliceop.CDEPTHIX().Serialize(),
					)...),
					exit: 0,
				},
			)
		}
		tests = append(tests,
			advancedCellOpsParityCase{
				name: fmt.Sprintf("pruned_mask%03b_level%d_clevel", mask, level),
				code: codeFromBuilders(t, append(reveal, cellsliceop.CLEVEL().Serialize())...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: fmt.Sprintf("pruned_mask%03b_level%d_clevelmask", mask, level),
				code: codeFromBuilders(t, append(reveal, cellsliceop.CLEVELMASK().Serialize())...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: fmt.Sprintf("pruned_mask%03b_level%d_cdepth", mask, level),
				code: codeFromBuilders(t, append(reveal, cellsliceop.CDEPTH().Serialize())...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: fmt.Sprintf("pruned_mask%03b_level%d_sdepth", mask, level),
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.SDEPTH().Serialize(),
				)...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: fmt.Sprintf("pruned_mask%03b_level%d_rebuild_special", mask, level),
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STSLICE().Serialize(),
					stackop.PUSHINT(big.NewInt(-1)).Serialize(),
					cellsliceop.ENDXC().Serialize(),
				)...),
				exit: 0,
			},
		)
	}

	for _, special := range []struct {
		name string
		cl   *cell.Cell
	}{
		{name: "proof", cl: proof},
		{name: "update", cl: update},
	} {
		wrapped, wrappers := wrapCellLevelToZero(t, special.cl)
		reveal := revealWrappedCellCode(wrapped, wrappers)
		tests = append(tests,
			advancedCellOpsParityCase{
				name: special.name + "_sdepth",
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.SDEPTH().Serialize(),
				)...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: special.name + "_raw_ref_hash",
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.PLDREFIDX(0).Serialize(),
					cellsliceop.CHASHI(3).Serialize(),
				)...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: special.name + "_store_slice",
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STSLICE().Serialize(),
					cellsliceop.ENDC().Serialize(),
				)...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: special.name + "_hash_slice",
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.HASHSU().Serialize(),
				)...),
				exit: 0,
			},
			advancedCellOpsParityCase{
				name: special.name + "_rebuild_special",
				code: codeFromBuilders(t, append(reveal,
					cellsliceop.XCTOS().Serialize(),
					stackop.DROP().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STSLICE().Serialize(),
					stackop.PUSHINT(big.NewInt(-1)).Serialize(),
					cellsliceop.ENDXC().Serialize(),
				)...),
				exit: 0,
			},
		)
	}

	parent := cell.BeginCell().MustStoreRef(pruned[7]).EndCell()
	wrappedParent, parentWrappers := wrapCellLevelToZero(t, parent)
	revealParent := revealWrappedCellCode(wrappedParent, parentWrappers)
	tests = append(tests,
		advancedCellOpsParityCase{
			name: "ordinary_parent_sdepth_pruned_ref",
			code: codeFromBuilders(t, append(revealParent,
				cellsliceop.CTOS().Serialize(),
				cellsliceop.SDEPTH().Serialize(),
			)...),
			exit: 0,
		},
		advancedCellOpsParityCase{
			name: "ordinary_parent_ldref_pruned_raw",
			code: codeFromBuilders(t, append(revealParent,
				cellsliceop.CTOS().Serialize(),
				cellsliceop.LDREF().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.CHASHI(3).Serialize(),
			)...),
			exit: 0,
		},
	)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runAdvancedCellOpsParityCaseWithExpected(t, tt, referenceRawRunGlobalVersion, true, &tt.exit)
		})
	}
}
