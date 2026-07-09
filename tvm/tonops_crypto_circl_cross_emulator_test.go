//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func tonOpsCryptoCirclVersionCrossEmulatorVersions(t *testing.T) []int {
	t.Helper()

	return crossEmulatorVersionAuditVersions(t, "TVM_TONOPS_CRYPTO_CIRCL_VERSION_AUDIT")
}

type tonOpsCryptoCirclVersionedRuntimeCase struct {
	name  string
	code  *cell.Cell
	stack func() []any
	exit  int32
}

func TestTVMCrossEmulatorTonOpsCryptoCirclVersionAuditShardSelection(t *testing.T) {
	t.Setenv("TVM_TONOPS_CRYPTO_CIRCL_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_TONOPS_CRYPTO_CIRCL_VERSION_AUDIT_SHARD", "")

	all := tonOpsCryptoCirclVersionCrossEmulatorVersions(t)
	wantLen := vm.MaxSupportedGlobalVersion - 0 + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != 0 || all[len(all)-1] != vm.MaxSupportedGlobalVersion {
		t.Fatalf("default version selection = %v, want range %d..%d", all, 0, vm.MaxSupportedGlobalVersion)
	}

	t.Setenv("TVM_TONOPS_CRYPTO_CIRCL_VERSION_AUDIT_SHARDS", "4")
	t.Setenv("TVM_TONOPS_CRYPTO_CIRCL_VERSION_AUDIT_SHARD", "1")
	got := tonOpsCryptoCirclVersionCrossEmulatorVersions(t)
	want := []int{1, 5, 9, 13}
	if len(got) != len(want) {
		t.Fatalf("sharded version selection = %v, want %v", got, want)
	}
	for i, version := range want {
		if got[i] != version {
			t.Fatalf("sharded version selection = %v, want %v", got, want)
		}
	}
}

func TestTVMCrossEmulatorTonOpsCryptoCircl(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg := []byte("ton-circl-bls-cross")
	msg2 := []byte("ton-circl-bls-cross-2")
	pub1 := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig1 := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	sig2Msg2 := testBLSSigBytes(5, msg2)
	aggSig := testBLSAggregateSigBytes(t, sig1, sig2)
	aggDistinctSig := testBLSAggregateSigBytes(t, sig1, sig2Msg2)

	g1a := testBLSG1BytesForScalar(2)
	g1b := testBLSG1BytesForScalar(3)
	g2a := testBLSG2BytesForScalar(2)
	g2b := testBLSG2BytesForScalar(3)
	fp := testBLSFPBytes(7)
	fp2 := testBLSFP2Bytes(11)

	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)
	ristOrder := testBigIntDecimal(t, "723700557733226221397318656304299424085711635937990760600195093828545425057")
	blsOrderInt := new(big.Int).SetBytes(circlbls.Order())
	blsOrderMinusOne := new(big.Int).Sub(blsOrderInt, big.NewInt(1))
	blsOrderPlusOne := new(big.Int).Add(blsOrderInt, big.NewInt(1))

	type testCase struct {
		name          string
		code          *cell.Cell
		stack         []any
		exit          int32
		globalVersion int
		skipReference string
		goStack       []any
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
			name: "rist255_add_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_ADD().Serialize()),
			stack: []any{
				invalidRist,
				testRistrettoMulBaseInt(t, 1),
			},
			exit:          int32(vmerr.CodeRangeCheck),
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
			name: "rist255_mulbase_order",
			code: codeFromBuilders(t, funcsop.RIST255_MULBASE().Serialize()),
			stack: []any{
				ristOrder,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_qmulbase_order",
			code: codeFromBuilders(t, funcsop.RIST255_QMULBASE().Serialize()),
			stack: []any{
				ristOrder,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_mulbase_zero_v13_identity",
			code: codeFromBuilders(t, funcsop.RIST255_MULBASE().Serialize()),
			stack: []any{
				int64(0),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_mulbase_zero_v14_identity",
			code: codeFromBuilders(t, funcsop.RIST255_MULBASE().Serialize()),
			stack: []any{
				int64(0),
			},
			exit:          0,
			globalVersion: 14,
		},
		{
			name: "rist255_mul_identity_v13_range",
			code: codeFromBuilders(t, funcsop.RIST255_MUL().Serialize()),
			stack: []any{
				int64(0),
				int64(1),
			},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 13,
		},
		{
			name: "rist255_mul_identity_v14_succeeds",
			code: codeFromBuilders(t, funcsop.RIST255_MUL().Serialize()),
			stack: []any{
				int64(0),
				int64(1),
			},
			exit:          0,
			globalVersion: 14,
			skipReference: "bundled reference emulator predates upstream RIST255 v14 identity support",
			goStack:       []any{int64(0)},
		},
		{
			name: "rist255_qmul_invalid_zero_v13_succeeds",
			code: codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()),
			stack: []any{
				invalidRist,
				int64(0),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "rist255_qmul_invalid_zero_v14_fails_quietly",
			code: codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()),
			stack: []any{
				invalidRist,
				int64(0),
			},
			exit:          0,
			globalVersion: 14,
			skipReference: "bundled reference emulator predates upstream RIST255 v14 zero-scalar validation",
			goStack:       []any{int64(0)},
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
			name: "bls_verify_short_pub_underflow",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1[:47]),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
			globalVersion: 13,
		},
		{
			name: "bls_verify_short_sig_underflow",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(sig1[:95]),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
			globalVersion: 13,
		},
		{
			name: "bls_verify_non_byte_msg_underflow",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testBitSlice(7),
				testSliceFromBytes(sig1),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
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
			name: "bls_aggregate_short_sig_underflow",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: []any{
				testSliceFromBytes(sig1[:95]),
				int64(1),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
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
			name: "bls_fastaggregateverify_short_pub_underflow",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1[:47]),
				int64(1),
				testSliceFromBytes(msg),
				testSliceFromBytes(aggSig),
			},
			exit:          int32(vmerr.CodeCellUnderflow),
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
			name: "bls_aggregateverify_distinct_msgs_true",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg),
				testSliceFromBytes(pub2),
				testSliceFromBytes(msg2),
				int64(2),
				testSliceFromBytes(aggDistinctSig),
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_aggregateverify_swapped_msgs_false",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: []any{
				testSliceFromBytes(pub1),
				testSliceFromBytes(msg2),
				testSliceFromBytes(pub2),
				testSliceFromBytes(msg),
				int64(2),
				testSliceFromBytes(aggDistinctSig),
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
			name: "bls_g1_mul_order_zero",
			code: codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				blsOrderInt,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g1_mul_invalid_order",
			code: codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				blsOrderInt,
			},
			exit:          int32(vmerr.CodeUnknown),
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
			name: "bls_g1_multiexp_count_too_large",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				int64(5),
				int64(2),
			},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 13,
		},
		{
			name: "bls_g1_multiexp_invalid_term",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g1a),
				int64(5),
				testSliceFromBytes(invalidG1),
				int64(7),
				int64(2),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_multiexp_invalid_order_single",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				blsOrderInt,
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_multiexp_invalid_order_minus_one_single",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				blsOrderMinusOne,
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g1_multiexp_invalid_order_plus_one_single",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG1),
				blsOrderPlusOne,
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
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
			name: "bls_g1_ingroup_zero_true",
			code: codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG1ZeroBytes()),
			},
			exit:          0,
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
			name: "bls_g2_mul_order_zero",
			code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				blsOrderInt,
			},
			exit:          0,
			globalVersion: 13,
		},
		{
			name: "bls_g2_mul_invalid_order",
			code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				blsOrderInt,
			},
			exit:          int32(vmerr.CodeUnknown),
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
			name: "bls_g2_multiexp_count_too_large",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				int64(5),
				int64(2),
			},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 13,
		},
		{
			name: "bls_g2_multiexp_invalid_term",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(g2a),
				int64(5),
				testSliceFromBytes(invalidG2),
				int64(7),
				int64(2),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g2_multiexp_invalid_order_single",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				blsOrderInt,
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g2_multiexp_invalid_order_minus_one_single",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				blsOrderMinusOne,
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
			globalVersion: 13,
		},
		{
			name: "bls_g2_multiexp_invalid_order_plus_one_single",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: []any{
				testSliceFromBytes(invalidG2),
				blsOrderPlusOne,
				int64(1),
			},
			exit:          int32(vmerr.CodeUnknown),
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
			name: "bls_g2_ingroup_zero_true",
			code: codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			stack: []any{
				testSliceFromBytes(testBLSG2ZeroBytes()),
			},
			exit:          0,
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
			if tt.skipReference != "" {
				if goRes.exitCode != tt.exit {
					t.Fatalf("unexpected go exit code: got=%d expected=%d", goRes.exitCode, tt.exit)
				}
				assertCrossSkippedGoStack(t, goRes.stack, tt.goStack)
				t.Skip(tt.skipReference)
			}
			refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(tt.globalVersion)))
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
		})
	}
}

func TestTVMCrossEmulatorTonOpsCryptoCirclVersionedRuntimeEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg := []byte("ton-circl-versioned")
	msg2 := []byte("ton-circl-versioned-2")
	pub := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	sig2Msg2 := testBLSSigBytes(5, msg2)
	aggSig := testBLSAggregateSigBytes(t, sig, sig2)
	aggDistinctSig := testBLSAggregateSigBytes(t, sig, sig2Msg2)
	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)
	g1 := testBLSG1BytesForScalar(2)
	g2 := testBLSG2BytesForScalar(3)
	g2b := testBLSG2BytesForScalar(5)
	fp := testBLSFPBytes(7)
	fp2 := testBLSFP2Bytes(11)
	blsOrderInt := new(big.Int).SetBytes(circlbls.Order())

	type versionedCryptoCase struct {
		name  string
		code  *cell.Cell
		stack func() []any
		exit  int32
	}

	tests := []versionedCryptoCase{
		{
			name: "rist255_pushl",
			code: codeFromBuilders(t, funcsop.RIST255_PUSHL().Serialize()),
			exit: 0,
		},
		{
			name: "rist255_validate_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()),
			stack: func() []any {
				return []any{new(big.Int).Set(invalidRist)}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "rist255_qvalidate_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()),
			stack: func() []any {
				return []any{new(big.Int).Set(invalidRist)}
			},
			exit: 0,
		},
		{
			name: "rist255_qadd_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QADD().Serialize()),
			stack: func() []any {
				return []any{
					testRistrettoMulBaseInt(t, 1),
					new(big.Int).Set(invalidRist),
				}
			},
			exit: 0,
		},
		{
			name: "rist255_add_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_ADD().Serialize()),
			stack: func() []any {
				return []any{
					new(big.Int).Set(invalidRist),
					testRistrettoMulBaseInt(t, 1),
				}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "rist255_qsub_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QSUB().Serialize()),
			stack: func() []any {
				return []any{
					testRistrettoMulBaseInt(t, 1),
					new(big.Int).Set(invalidRist),
				}
			},
			exit: 0,
		},
		{
			name: "rist255_qmul_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()),
			stack: func() []any {
				return []any{
					new(big.Int).Set(invalidRist),
					int64(3),
				}
			},
			exit: 0,
		},
		{
			name: "bls_pushr",
			code: codeFromBuilders(t, funcsop.BLS_PUSHR().Serialize()),
			exit: 0,
		},
		{
			name: "bls_verify_true",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub),
					testSliceFromBytes(msg),
					testSliceFromBytes(sig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_verify_invalid_pub_false",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(invalidG1),
					testSliceFromBytes(msg),
					testSliceFromBytes(sig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_aggregate",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(sig),
					testSliceFromBytes(sig2),
					int64(2),
				}
			},
			exit: 0,
		},
		{
			name: "bls_aggregate_short_sig_underflow",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(sig[:95]),
					int64(1),
				}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_fastaggregateverify_true",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub),
					testSliceFromBytes(pub2),
					int64(2),
					testSliceFromBytes(msg),
					testSliceFromBytes(aggSig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_fastaggregateverify_short_pub_underflow",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub[:47]),
					int64(1),
					testSliceFromBytes(msg),
					testSliceFromBytes(aggSig),
				}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_aggregateverify_distinct_msgs_true",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub),
					testSliceFromBytes(msg),
					testSliceFromBytes(pub2),
					testSliceFromBytes(msg2),
					int64(2),
					testSliceFromBytes(aggDistinctSig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_map_to_g1_underflow",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(fp[:47])}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_g1_multiexp_count_too_large",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g1),
					int64(5),
					int64(2),
				}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "bls_g1_add_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(invalidG1),
					testSliceFromBytes(g1),
				}
			},
			exit: int32(vmerr.CodeUnknown),
		},
		{
			name: "bls_g1_ingroup_invalid_false",
			code: codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(invalidG1)}
			},
			exit: 0,
		},
		{
			name: "bls_g2_add",
			code: codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					testSliceFromBytes(g2b),
				}
			},
			exit: 0,
		},
		{
			name: "bls_g2_mul_order_zero",
			code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					blsOrderInt,
				}
			},
			exit: 0,
		},
		{
			name: "bls_g2_multiexp_two_terms",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					int64(5),
					testSliceFromBytes(g2b),
					int64(7),
					int64(2),
				}
			},
			exit: 0,
		},
		{
			name: "bls_g2_multiexp_count_too_large",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					int64(5),
					int64(2),
				}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "bls_map_to_g2",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(fp2)}
			},
			exit: 0,
		},
		{
			name: "bls_map_to_g2_underflow",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(fp2[:95])}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_g2_ingroup_invalid_false",
			code: codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(invalidG2)}
			},
			exit: 0,
		},
		{
			name: "bls_pairing_false",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g1),
					testSliceFromBytes(g2),
					int64(1),
				}
			},
			exit: 0,
		},
		{
			name: "bls_pairing_invalid",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(invalidG1),
					testSliceFromBytes(g2),
					int64(1),
				}
			},
			exit: int32(vmerr.CodeUnknown),
		},
	}

	versions := tonOpsCryptoCirclVersionCrossEmulatorVersions(t)
	for _, tt := range tests {
		for _, version := range versions {
			t.Run(fmt.Sprintf("%s/v%d", tt.name, version), func(t *testing.T) {
				wantExit := tt.exit
				if version < 4 {
					wantExit = int32(vmerr.CodeInvalidOpcode)
				}
				runTonOpsCryptoCirclVersionedParityCase(t, tt.code, tt.stack, version, wantExit)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTonOpsCryptoCirclVersionedRuntimeEdges(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tonOpsCryptoCirclVersionedRuntimeCaseCount))
	}
	for i := 0; i < tonOpsCryptoCirclVersionedRuntimeCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := tonOpsCryptoCirclVersionedRuntimeCases(t)
		if len(tests) != tonOpsCryptoCirclVersionedRuntimeCaseCount {
			t.Fatalf("tonops crypto circl versioned runtime case count = %d, want %d", len(tests), tonOpsCryptoCirclVersionedRuntimeCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runTonOpsCryptoCirclVersionedRuntimeCase(t, tt, version)
	})
}

const tonOpsCryptoCirclVersionedRuntimeCaseCount = 28

func runTonOpsCryptoCirclVersionedRuntimeCase(t *testing.T, tt tonOpsCryptoCirclVersionedRuntimeCase, version int) {
	t.Helper()

	wantExit := tt.exit
	if version < 4 {
		wantExit = int32(vmerr.CodeInvalidOpcode)
	}
	runTonOpsCryptoCirclVersionedParityCase(t, tt.code, tt.stack, version, wantExit)
}

func tonOpsCryptoCirclVersionedRuntimeCases(t *testing.T) []tonOpsCryptoCirclVersionedRuntimeCase {
	t.Helper()

	msg := []byte("ton-circl-versioned")
	msg2 := []byte("ton-circl-versioned-2")
	pub := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	sig2Msg2 := testBLSSigBytes(5, msg2)
	aggSig := testBLSAggregateSigBytes(t, sig, sig2)
	aggDistinctSig := testBLSAggregateSigBytes(t, sig, sig2Msg2)
	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)
	g1 := testBLSG1BytesForScalar(2)
	g2 := testBLSG2BytesForScalar(3)
	g2b := testBLSG2BytesForScalar(5)
	fp := testBLSFPBytes(7)
	fp2 := testBLSFP2Bytes(11)
	blsOrderInt := new(big.Int).SetBytes(circlbls.Order())

	return []tonOpsCryptoCirclVersionedRuntimeCase{
		{
			name: "rist255_pushl",
			code: codeFromBuilders(t, funcsop.RIST255_PUSHL().Serialize()),
			exit: 0,
		},
		{
			name: "rist255_validate_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()),
			stack: func() []any {
				return []any{new(big.Int).Set(invalidRist)}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "rist255_qvalidate_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()),
			stack: func() []any {
				return []any{new(big.Int).Set(invalidRist)}
			},
			exit: 0,
		},
		{
			name: "rist255_qadd_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QADD().Serialize()),
			stack: func() []any {
				return []any{
					testRistrettoMulBaseInt(t, 1),
					new(big.Int).Set(invalidRist),
				}
			},
			exit: 0,
		},
		{
			name: "rist255_add_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_ADD().Serialize()),
			stack: func() []any {
				return []any{
					new(big.Int).Set(invalidRist),
					testRistrettoMulBaseInt(t, 1),
				}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "rist255_qsub_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QSUB().Serialize()),
			stack: func() []any {
				return []any{
					testRistrettoMulBaseInt(t, 1),
					new(big.Int).Set(invalidRist),
				}
			},
			exit: 0,
		},
		{
			name: "rist255_qmul_invalid",
			code: codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()),
			stack: func() []any {
				return []any{
					new(big.Int).Set(invalidRist),
					int64(3),
				}
			},
			exit: 0,
		},
		{
			name: "bls_pushr",
			code: codeFromBuilders(t, funcsop.BLS_PUSHR().Serialize()),
			exit: 0,
		},
		{
			name: "bls_verify_true",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub),
					testSliceFromBytes(msg),
					testSliceFromBytes(sig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_verify_invalid_pub_false",
			code: codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(invalidG1),
					testSliceFromBytes(msg),
					testSliceFromBytes(sig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_aggregate",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(sig),
					testSliceFromBytes(sig2),
					int64(2),
				}
			},
			exit: 0,
		},
		{
			name: "bls_aggregate_short_sig_underflow",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(sig[:95]),
					int64(1),
				}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_fastaggregateverify_true",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub),
					testSliceFromBytes(pub2),
					int64(2),
					testSliceFromBytes(msg),
					testSliceFromBytes(aggSig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_fastaggregateverify_short_pub_underflow",
			code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub[:47]),
					int64(1),
					testSliceFromBytes(msg),
					testSliceFromBytes(aggSig),
				}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_aggregateverify_distinct_msgs_true",
			code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(pub),
					testSliceFromBytes(msg),
					testSliceFromBytes(pub2),
					testSliceFromBytes(msg2),
					int64(2),
					testSliceFromBytes(aggDistinctSig),
				}
			},
			exit: 0,
		},
		{
			name: "bls_map_to_g1_underflow",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(fp[:47])}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_g1_multiexp_count_too_large",
			code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g1),
					int64(5),
					int64(2),
				}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "bls_g1_add_invalid",
			code: codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(invalidG1),
					testSliceFromBytes(g1),
				}
			},
			exit: int32(vmerr.CodeUnknown),
		},
		{
			name: "bls_g1_ingroup_invalid_false",
			code: codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(invalidG1)}
			},
			exit: 0,
		},
		{
			name: "bls_g2_add",
			code: codeFromBuilders(t, funcsop.BLS_G2_ADD().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					testSliceFromBytes(g2b),
				}
			},
			exit: 0,
		},
		{
			name: "bls_g2_mul_order_zero",
			code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					blsOrderInt,
				}
			},
			exit: 0,
		},
		{
			name: "bls_g2_multiexp_two_terms",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					int64(5),
					testSliceFromBytes(g2b),
					int64(7),
					int64(2),
				}
			},
			exit: 0,
		},
		{
			name: "bls_g2_multiexp_count_too_large",
			code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g2),
					int64(5),
					int64(2),
				}
			},
			exit: int32(vmerr.CodeRangeCheck),
		},
		{
			name: "bls_map_to_g2",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(fp2)}
			},
			exit: 0,
		},
		{
			name: "bls_map_to_g2_underflow",
			code: codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(fp2[:95])}
			},
			exit: int32(vmerr.CodeCellUnderflow),
		},
		{
			name: "bls_g2_ingroup_invalid_false",
			code: codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			stack: func() []any {
				return []any{testSliceFromBytes(invalidG2)}
			},
			exit: 0,
		},
		{
			name: "bls_pairing_false",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(g1),
					testSliceFromBytes(g2),
					int64(1),
				}
			},
			exit: 0,
		},
		{
			name: "bls_pairing_invalid",
			code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack: func() []any {
				return []any{
					testSliceFromBytes(invalidG1),
					testSliceFromBytes(g2),
					int64(1),
				}
			},
			exit: int32(vmerr.CodeUnknown),
		},
	}
}

func runTonOpsCryptoCirclVersionedParityCase(t *testing.T, code *cell.Cell, stack func() []any, version int, wantExit int32) {
	t.Helper()

	var stackValues []any
	if stack != nil {
		stackValues = stack()
	}
	code = prependRawMethodDrop(code)
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantExit)
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

func testEmptyCell() *cell.Cell {
	return cell.BeginCell().EndCell()
}

func testBigIntDecimal(t *testing.T, src string) *big.Int {
	t.Helper()

	n, ok := new(big.Int).SetString(src, 10)
	if !ok {
		t.Fatalf("invalid decimal big int %q", src)
	}
	return n
}

func testBitSlice(bits uint) *cell.Slice {
	return cell.BeginCell().MustStoreUInt(0, bits).ToSlice()
}
