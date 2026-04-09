//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorTonOpsCryptoCircl(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg := []byte("ton-circl-bls-cross")
	pub1 := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig1 := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	aggSig := testBLSAggregateSigBytes(t, sig1, sig2)

	g1a := testBLSG1BytesForScalar(2)
	g1b := testBLSG1BytesForScalar(3)
	g2a := testBLSG2BytesForScalar(2)
	g2b := testBLSG2BytesForScalar(3)
	fp := testBLSFPBytes(7)
	fp2 := testBLSFP2Bytes(11)

	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)

	type testCase struct {
		name          string
		code          *cell.Cell
		stack         []any
		exit          int32
		globalVersion int
	}

	tests := []testCase{
		{
			name: "rist255_fromhash",
			code: codeFromBuilders(t, funcsop.RIST255_FROMHASH().Serialize()),
			stack: []any{
				int64(1),
				int64(2),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name:          "rist255_pushl",
			code:          codeFromBuilders(t, funcsop.RIST255_PUSHL().Serialize()),
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_validate_valid",
			code: codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 1),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_validate_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()),
			stack: []any{
				invalidRist,
			},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 13,
		},
		{
			name: "rist255_qvalidate_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()),
			stack: []any{
				invalidRist,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_qadd_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QADD().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 1),
				invalidRist,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_qsub_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QSUB().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 1),
				invalidRist,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_qmul_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()),
			stack: []any{
				invalidRist,
				int64(3),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_mul",
			code: codeFromBuilders(t, funcsop.RIST255_MUL().Serialize()),
			stack: []any{
				testRistrettoMulBaseInt(t, 5),
				int64(3),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_mulbase",
			code: codeFromBuilders(t, funcsop.RIST255_MULBASE().Serialize()),
			stack: []any{
				int64(11),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name:          "bls_pushr",
			code:          codeFromBuilders(t, funcsop.BLS_PUSHR().Serialize()),
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_verify_true",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_verify_invalid_pub_false",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_aggregate",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{
				testSliceFromBytes(sig1),
				testSliceFromBytes(sig2),
				int64(2),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_fastaggregateverify_true",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(pub2),
				int64(2),
				testSliceFromBytes(msg),
				testSliceFromBytes(aggSig),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_fastaggregateverify_empty",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				int64(0),
				testSliceFromBytes(msg),
				testSliceFromBytes(aggSig),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_fastaggregateverify_invalid_pub_false",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(pub2),
				int64(2),
				testSliceFromBytes(msg),
				testSliceFromBytes(aggSig),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_aggregateverify_duplicate_msgs",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(pub2),
				testSliceFromBytes(msg),
				int64(2),
				testSliceFromBytes(aggSig),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_aggregateverify_invalid_pub_false",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(msg),
				int64(1),
				testSliceFromBytes(sig1),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_add",
			code: codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				testSliceFromBytes(g1b),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_mul_zero",
			code: codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				int64(0),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_multiexp_zero",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				int64(0),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_multiexp_two_terms",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				int64(5),
				testSliceFromBytes(g1b),
				int64(7),
				int64(2),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_iszero",
			code: codeFromBuilders(t, funcsop.BLS_G1_ISZERO().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG1ZeroBytes()),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_add_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(g1a),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_sub_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G1_SUB().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(g1a),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_neg_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G1_NEG().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_mul_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				int64(3),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_ingroup_invalid_false",
			code: codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_map_to_g1",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			stack: []any{
				testSliceFromBytes(fp),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_map_to_g1_underflow",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			stack: []any{
				testSliceFromBytes(fp[:47]),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
			globalVersion: 13,
		},
		{
			name: "bls_g2_add",
			code: codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				testSliceFromBytes(g2b),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g2_mul_zero",
			code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				int64(0),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g2_multiexp_zero",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				int64(0),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g2_multiexp_two_terms",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				int64(5),
				testSliceFromBytes(g2b),
				int64(7),
				int64(2),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g2_iszero",
			code: codeFromBuilders(t, funcsop.BLS_G2_ISZERO().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG2ZeroBytes()),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_aggregate_invalid",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g2_sub_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G2_SUB().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				testSliceFromBytes(g2a),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g2_neg_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G2_NEG().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g2_mul_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				int64(3),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_map_to_g2",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: []any{
				testSliceFromBytes(fp2),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_map_to_g2_underflow",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: []any{
				testSliceFromBytes(fp2[:95]),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
			globalVersion: 13,
		},
		{
			name: "bls_g2_ingroup_invalid_false",
			code: codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_pairing_false",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG1BytesForScalar(1)),
				testSliceFromBytes(testBLSG2BytesForScalar(1)),
				int64(1),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_pairing_true_cancel",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: func() []any {
				var neg circlbls.G2
				if err := neg.SetBytes(testBLSG2BytesForScalar(1)); err != nil {
					t.Fatalf("failed to parse g2: %v", err)
				}
				neg.Neg()
				return []any{
					testSliceFromBytes(testBLSG1BytesForScalar(1)),
					testSliceFromBytes(testBLSG2BytesForScalar(1)),
					testSliceFromBytes(testBLSG1BytesForScalar(1)),
					testSliceFromBytes(neg.BytesCompressed()),
					int64(2),
				}
			}(),
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_pairing_invalid",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				testSliceFromBytes(testBLSG2BytesForScalar(1)),
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
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

			goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, goStack, tt.globalVersion)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, testEmptyCell(), tuple.Tuple{}, refStack)
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
}

func testEmptyCell() *cell.Cell {
	return cell.BeginCell().EndCell()
}
