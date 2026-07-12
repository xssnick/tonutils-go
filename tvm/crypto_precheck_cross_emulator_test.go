//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorCryptoPrecheckParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	fullSignature := cell.BeginCell().MustStoreSlice(make([]byte, 64), 512).ToSlice()
	p256Key := cell.BeginCell().MustStoreSlice(make([]byte, 33), 264).ToSlice()
	emptySlice := cell.BeginCell().ToSlice()
	zero := big.NewInt(0)
	one := big.NewInt(1)

	msg := []byte("crypto-precheck-parity")
	pub := testBLSPubBytes(3)
	sig := testBLSSigBytes(3, msg)
	g1 := testBLSG1BytesForScalar(2)
	g2 := testBLSG2BytesForScalar(2)

	configParamCode := codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize())
	configOptParamCode := codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize())
	configRoot := tonopsCrossConfigWithGlobalVersion(t, referenceRawRunGlobalVersion)
	configParamC7 := prepareCrossTestC7WithConfigRoot(configRoot, configParamCode)
	configOptParamC7 := prepareCrossTestC7WithConfigRoot(configRoot, configOptParamCode)
	invalidConfigParamC7 := cryptoPrecheckInvalidConfigC7(t, configParamCode)
	invalidConfigOptParamC7 := cryptoPrecheckInvalidConfigC7(t, configOptParamCode)

	tests := []struct {
		name  string
		code  *cell.Cell
		stack []any
		c7    tuple.Tuple
		exit  int32
	}{
		{name: "CONFIGPARAM NaN is missing", code: configParamCode, stack: []any{vm.NaN{}}, c7: configParamC7},
		{name: "CONFIGOPTPARAM NaN is null", code: configOptParamCode, stack: []any{vm.NaN{}}, c7: configOptParamC7},
		{name: "CONFIGPARAM validates C7 before NaN", code: configParamCode, stack: []any{vm.NaN{}}, c7: invalidConfigParamC7, exit: int32(vmerr.CodeTypeCheck)},
		{name: "CONFIGOPTPARAM validates C7 before NaN", code: configOptParamCode, stack: []any{vm.NaN{}}, c7: invalidConfigOptParamC7, exit: int32(vmerr.CodeTypeCheck)},
		{name: "CHKSIGNU NaN hash", code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()), stack: []any{vm.NaN{}, fullSignature, one}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "CHKSIGNU NaN key", code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()), stack: []any{zero, fullSignature, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "CHKSIGNU signature type precedes NaN key export", code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()), stack: []any{zero, one, vm.NaN{}}, exit: int32(vmerr.CodeTypeCheck)},
		{name: "CHKSIGNS NaN key", code: codeFromBuilders(t, funcsop.CHKSIGNS().Serialize()), stack: []any{emptySlice, fullSignature, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "ECRECOVER NaN hash", code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()), stack: []any{vm.NaN{}, zero, zero, zero}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "ECRECOVER NaN r", code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()), stack: []any{zero, zero, vm.NaN{}, zero}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "ECRECOVER NaN s", code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()), stack: []any{zero, zero, zero, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "ECRECOVER v range precedes NaN s export", code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()), stack: []any{zero, big.NewInt(256), zero, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "SECP256K1 NaN key", code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()), stack: []any{vm.NaN{}, zero}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "SECP256K1 NaN tweak", code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()), stack: []any{zero, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "P256_CHKSIGNU NaN hash", code: codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()), stack: []any{vm.NaN{}, fullSignature, p256Key}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RIST255_FROMHASH NaN", code: codeFromBuilders(t, funcsop.RIST255_FROMHASH().Serialize()), stack: []any{zero, vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RIST255_VALIDATE NaN", code: codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()), stack: []any{vm.NaN{}}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RIST255_QVALIDATE NaN", code: codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()), stack: []any{vm.NaN{}}},
		{name: "RIST255_ADD NaN", code: codeFromBuilders(t, funcsop.RIST255_ADD().Serialize()), stack: []any{vm.NaN{}, zero}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RIST255_QADD NaN", code: codeFromBuilders(t, funcsop.RIST255_QADD().Serialize()), stack: []any{vm.NaN{}, zero}},
		{name: "RIST255_SUB NaN", code: codeFromBuilders(t, funcsop.RIST255_SUB().Serialize()), stack: []any{vm.NaN{}, zero}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "RIST255_QSUB NaN", code: codeFromBuilders(t, funcsop.RIST255_QSUB().Serialize()), stack: []any{vm.NaN{}, zero}},
		{name: "BLS_G1_MUL charges gas before NaN", code: codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()), stack: []any{testSliceFromBytes(g1), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "BLS_G2_MUL charges gas before NaN", code: codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()), stack: []any{testSliceFromBytes(g2), vm.NaN{}}, exit: int32(vmerr.CodeIntOverflow)},
		{name: "BLS_AGGREGATE rejects unavailable signature", code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()), stack: []any{testSliceFromBytes(sig), int64(2)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "BLS_AGGREGATE exact boundary", code: codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()), stack: []any{testSliceFromBytes(sig), int64(1)}},
		{name: "BLS_FASTAGGREGATEVERIFY rejects unavailable pubkey", code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()), stack: []any{testSliceFromBytes(pub), int64(2), testSliceFromBytes(msg), testSliceFromBytes(sig)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "BLS_FASTAGGREGATEVERIFY exact boundary", code: codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()), stack: []any{testSliceFromBytes(pub), int64(1), testSliceFromBytes(msg), testSliceFromBytes(sig)}},
		{name: "BLS_AGGREGATEVERIFY rejects unavailable message", code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()), stack: []any{testSliceFromBytes(pub), int64(1), testSliceFromBytes(sig)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "BLS_AGGREGATEVERIFY exact boundary", code: codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()), stack: []any{testSliceFromBytes(pub), testSliceFromBytes(msg), int64(1), testSliceFromBytes(sig)}},
		{name: "BLS_G1_MULTIEXP rejects unavailable scalar", code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()), stack: []any{testSliceFromBytes(g1), int64(1)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "BLS_G1_MULTIEXP exact boundary", code: codeFromBuilders(t, funcsop.BLS_G1_MULTIEXP().Serialize()), stack: []any{testSliceFromBytes(g1), int64(2), int64(1)}},
		{name: "BLS_G2_MULTIEXP rejects unavailable scalar", code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()), stack: []any{testSliceFromBytes(g2), int64(1)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "BLS_G2_MULTIEXP exact boundary", code: codeFromBuilders(t, funcsop.BLS_G2_MULTIEXP().Serialize()), stack: []any{testSliceFromBytes(g2), int64(2), int64(1)}},
		{name: "BLS_PAIRING rejects unavailable G2", code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()), stack: []any{testSliceFromBytes(g1), int64(1)}, exit: int32(vmerr.CodeRangeCheck)},
		{name: "BLS_PAIRING exact boundary", code: codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()), stack: []any{testSliceFromBytes(g1), testSliceFromBytes(g2), int64(1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runCryptoPrecheckParityCase(t, tt.code, tt.stack, tt.c7, tt.exit)
		})
	}
}

func cryptoPrecheckInvalidConfigC7(t *testing.T, code *cell.Cell) tuple.Tuple {
	t.Helper()

	c7 := prepareCrossTestC7WithConfigRoot(nil, code)
	innerValue, err := c7.Index(0)
	if err != nil {
		t.Fatalf("failed to load C7 parameter tuple: %v", err)
	}
	inner, ok := innerValue.(tuple.Tuple)
	if !ok {
		t.Fatalf("C7 parameter tuple has type %T", innerValue)
	}
	if err = inner.Set(9, big.NewInt(7)); err != nil {
		t.Fatalf("failed to set invalid config root: %v", err)
	}
	return tuple.NewTupleValue(inner)
}

func runCryptoPrecheckParityCase(t *testing.T, code *cell.Cell, stackValues []any, c7 tuple.Tuple, wantExit int32) {
	t.Helper()

	code = prependRawMethodDrop(code)
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build Go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithGas(code, cell.BeginCell().EndCell(), c7, goStack, referenceDefaultMaxGas)
	if err != nil {
		t.Fatalf("Go TVM execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, cell.BeginCell().EndCell(), c7, refStack, referenceDefaultMaxGas)
	if err != nil {
		t.Fatalf("reference TVM execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: Go=%d reference=%d want=%d", goRes.exitCode, refRes.exitCode, wantExit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: Go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize Go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\nGo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}
