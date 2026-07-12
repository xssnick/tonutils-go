//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

// TestTVMCrossEmulatorBLSOffSubgroupParity pins down, against the real reference
// emulator (exit code + gas + full result stack), the asymmetric subgroup rules
// that TON's low-level BLS opcodes inherit from blst:
//
//   - NEG / MUL / MULTIEXP / PAIRING decode every operand on-curve only.
//   - ADD(a,b) loads a (bottom) on-curve but aggregates b (top) → b must be in
//     the subgroup; SUB(a,b) is mirrored → a (bottom) must be in the subgroup.
//   - AGGREGATE exempts only its first (bottom) element; every other element
//     must be in the subgroup.
//   - FASTAGGREGATEVERIFY has the same internal aggregation rule, but any
//     off-subgroup key leaves the final aggregate off-subgroup, so its
//     TVM-observable result is false regardless of which key supplied it.
//   - INGROUP still enforces the full subgroup check.
//
// Before the fix the Go VM ran the full subgroup check on every operand and threw
// CodeUnknown where the reference computes a result (or vice versa).
func TestTVMCrossEmulatorBLSOffSubgroupParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	// On-curve, off-subgroup operands.
	offG1 := testBLSG1OffSubgroupBytes(t)
	offG2 := testBLSG2OffSubgroupBytes(t)
	// In-subgroup operands.
	inG1 := testBLSG1BytesForScalar(2)
	inG1b := testBLSG1BytesForScalar(3)
	inG2 := testBLSG2BytesForScalar(2)
	inG2b := testBLSG2BytesForScalar(3)

	// FASTAGGREGATEVERIFY inputs exercise both positions. Its API converts all
	// decode/group failures to false, and the final aggregate also remains outside
	// the subgroup, so only that observable false result can be pinned here.
	favMsg := []byte("ton-bls-off-subgroup")
	favSig := testBLSSigBytes(7, favMsg)

	type testCase struct {
		name      string
		code      *cell.Cell
		stack     []any
		exit      int32
		wantStack []any
	}

	unknown := int32(vmerr.CodeUnknown)

	tests := []testCase{
		// ---- fully on-curve-only ops: off-subgroup operands succeed ----
		{
			name:  "bls_g1_neg_off_subgroup",
			code:  codeFromBuilders(t, funcsop.BLS_G1_NEG().Serialize()),
			stack: []any{testSliceFromBytes(offG1)},
			exit:  0,
		},
		{
			name:  "bls_g1_mul_off_subgroup",
			code:  codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			stack: []any{testSliceFromBytes(offG1), int64(5)},
			exit:  0,
		},
		{
			name:  "bls_g1_multiexp_off_subgroup_single",
			code:  codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{testSliceFromBytes(offG1), int64(9), int64(1)},
			exit:  0,
		},
		{
			name:  "bls_g1_multiexp_off_subgroup_two",
			code:  codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{testSliceFromBytes(offG1), int64(3), testSliceFromBytes(offG1), int64(11), int64(2)},
			exit:  0,
		},
		{
			name:  "bls_g2_neg_off_subgroup",
			code:  codeFromBuilders(t, funcsop.BLS_G2_NEG().Serialize()),
			stack: []any{testSliceFromBytes(offG2)},
			exit:  0,
		},
		{
			name:  "bls_g2_mul_off_subgroup",
			code:  codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: []any{testSliceFromBytes(offG2), int64(6)},
			exit:  0,
		},
		{
			name:  "bls_g2_multiexp_off_subgroup_single",
			code:  codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{testSliceFromBytes(offG2), int64(4), int64(1)},
			exit:  0,
		},
		{
			name:  "bls_g2_multiexp_off_subgroup_two",
			code:  codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{testSliceFromBytes(offG2), int64(3), testSliceFromBytes(offG2), int64(11), int64(2)},
			exit:  0,
		},
		{
			// e(Q,R) * e(-Q,R) = identity for any on-curve Q,R (even off-subgroup):
			// deterministic TRUE, exercises PAIRING on off-subgroup inputs.
			name: "bls_pairing_off_subgroup_identity_true",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: []any{
				testSliceFromBytes(offG1),
				testSliceFromBytes(offG2),
				testSliceFromBytes(testBLSG1NegBytes(t, offG1)),
				testSliceFromBytes(offG2),
				int64(2),
			},
			exit:      0,
			wantStack: []any{int64(-1)},
		},

		// ---- ADD: bottom a exempt, top b must be in subgroup ----
		{
			name:  "bls_g1_add_bottom_off_top_in_ok",
			code:  codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{testSliceFromBytes(offG1), testSliceFromBytes(inG1)},
			exit:  0,
		},
		{
			name:  "bls_g1_add_bottom_in_top_off_throws",
			code:  codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{testSliceFromBytes(inG1), testSliceFromBytes(offG1)},
			exit:  unknown,
		},
		{
			name:  "bls_g2_add_bottom_off_top_in_ok",
			code:  codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			stack: []any{testSliceFromBytes(offG2), testSliceFromBytes(inG2)},
			exit:  0,
		},
		{
			name:  "bls_g2_add_bottom_in_top_off_throws",
			code:  codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			stack: []any{testSliceFromBytes(inG2), testSliceFromBytes(offG2)},
			exit:  unknown,
		},

		// ---- SUB: top b exempt, bottom a must be in subgroup ----
		{
			name:  "bls_g1_sub_bottom_in_top_off_ok",
			code:  codeFromBuilders(t, funcsop.BLS_G1_SUB().Serialize()),
			stack: []any{testSliceFromBytes(inG1), testSliceFromBytes(offG1)},
			exit:  0,
		},
		{
			name:  "bls_g1_sub_bottom_off_top_in_throws",
			code:  codeFromBuilders(t, funcsop.BLS_G1_SUB().Serialize()),
			stack: []any{testSliceFromBytes(offG1), testSliceFromBytes(inG1)},
			exit:  unknown,
		},
		{
			name:  "bls_g2_sub_bottom_in_top_off_ok",
			code:  codeFromBuilders(t, funcsop.BLS_G2_SUB().Serialize()),
			stack: []any{testSliceFromBytes(inG2), testSliceFromBytes(offG2)},
			exit:  0,
		},
		{
			name:  "bls_g2_sub_bottom_off_top_in_throws",
			code:  codeFromBuilders(t, funcsop.BLS_G2_SUB().Serialize()),
			stack: []any{testSliceFromBytes(offG2), testSliceFromBytes(inG2)},
			exit:  unknown,
		},

		// ---- AGGREGATE: bottom sig exempt, the rest must be in subgroup ----
		{
			name:      "bls_aggregate_single_off_subgroup_ok",
			code:      codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack:     []any{testSliceFromBytes(offG2), int64(1)},
			exit:      0,
			wantStack: []any{testSliceFromBytes(offG2)},
		},
		{
			name:  "bls_aggregate_bottom_off_top_in_ok",
			code:  codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{testSliceFromBytes(offG2), testSliceFromBytes(inG2), int64(2)},
			exit:  0,
		},
		{
			name:  "bls_aggregate_bottom_in_top_off_throws",
			code:  codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{testSliceFromBytes(inG2), testSliceFromBytes(offG2), int64(2)},
			exit:  unknown,
		},

		// ---- INGROUP still enforces the subgroup check ----
		{
			name:      "bls_g1_ingroup_off_subgroup_false",
			code:      codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			stack:     []any{testSliceFromBytes(offG1)},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name:      "bls_g2_ingroup_off_subgroup_false",
			code:      codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			stack:     []any{testSliceFromBytes(offG2)},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name:      "bls_g1_iszero_off_subgroup_false",
			code:      codeFromBuilders(t, funcsop.BLS_G1_ISZERO().Serialize()),
			stack:     []any{testSliceFromBytes(offG1)},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name:      "bls_g2_iszero_off_subgroup_false",
			code:      codeFromBuilders(t, funcsop.BLS_G2_ISZERO().Serialize()),
			stack:     []any{testSliceFromBytes(offG2)},
			exit:      0,
			wantStack: []any{int64(0)},
		},

		// ---- full verification paths reject off-subgroup inputs as false ----
		{
			name:      "bls_verify_off_subgroup_pub_false",
			code:      codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack:     []any{testSliceFromBytes(offG1), testSliceFromBytes(favMsg), testSliceFromBytes(favSig)},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name:      "bls_verify_off_subgroup_sig_false",
			code:      codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack:     []any{testSliceFromBytes(inG1), testSliceFromBytes(favMsg), testSliceFromBytes(offG2)},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name:      "bls_aggregateverify_off_subgroup_pub_false",
			code:      codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack:     []any{testSliceFromBytes(offG1), testSliceFromBytes(favMsg), int64(1), testSliceFromBytes(favSig)},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name:      "bls_aggregateverify_off_subgroup_sig_false",
			code:      codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack:     []any{testSliceFromBytes(inG1), testSliceFromBytes(favMsg), int64(1), testSliceFromBytes(offG2)},
			exit:      0,
			wantStack: []any{int64(0)},
		},

		// ---- FASTAGGREGATEVERIFY: both placements are observably false ----
		{
			name: "bls_fastaggregateverify_bottom_off_top_in",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(offG1),
				testSliceFromBytes(inG1),
				int64(2),
				testSliceFromBytes(favMsg),
				testSliceFromBytes(favSig),
			},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name: "bls_fastaggregateverify_bottom_in_top_off",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(inG1),
				testSliceFromBytes(offG1),
				int64(2),
				testSliceFromBytes(favMsg),
				testSliceFromBytes(favSig),
			},
			exit:      0,
			wantStack: []any{int64(0)},
		},
		{
			name: "bls_fastaggregateverify_off_subgroup_sig",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(inG1),
				int64(1),
				testSliceFromBytes(favMsg),
				testSliceFromBytes(offG2),
			},
			exit:      0,
			wantStack: []any{int64(0)},
		},

		// ---- extra combinations to keep the two-term arithmetic honest ----
		{
			name:  "bls_g1_add_both_in_ok",
			code:  codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{testSliceFromBytes(inG1), testSliceFromBytes(inG1b)},
			exit:  0,
		},
		{
			name:  "bls_g2_add_both_in_ok",
			code:  codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			stack: []any{testSliceFromBytes(inG2), testSliceFromBytes(inG2b)},
			exit:  0,
		},
	}

	const globalVersion = 13

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

			goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, goStack, globalVersion)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
			refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
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
			assertCrossSkippedGoStack(t, goStackCell, tt.wantStack)
		})
	}
}
