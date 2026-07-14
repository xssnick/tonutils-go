//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func tonOpsVersionCrossEmulatorVersions(t *testing.T) []int {
	t.Helper()

	return crossEmulatorVersionAuditVersions(t, "TVM_TONOPS_VERSION_AUDIT")
}

func TestTVMCrossEmulatorTonOpsVersionAuditShardSelection(t *testing.T) {
	t.Setenv("TVM_TONOPS_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_TONOPS_VERSION_AUDIT_SHARD", "")

	all := tonOpsVersionCrossEmulatorVersions(t)
	wantLen := vm.MaxSupportedGlobalVersion - 0 + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != 0 || all[len(all)-1] != vm.MaxSupportedGlobalVersion {
		t.Fatalf("default version selection = %v, want range %d..%d", all, 0, vm.MaxSupportedGlobalVersion)
	}

	t.Setenv("TVM_TONOPS_VERSION_AUDIT_SHARDS", "4")
	t.Setenv("TVM_TONOPS_VERSION_AUDIT_SHARD", "3")
	got := tonOpsVersionCrossEmulatorVersions(t)
	want := []int{3, 7, 11, 15}
	if len(got) != len(want) {
		t.Fatalf("sharded version selection = %v, want %v", got, want)
	}
	for i, version := range want {
		if got[i] != version {
			t.Fatalf("sharded version selection = %v, want %v", got, want)
		}
	}
}

func TestTVMCrossEmulatorTonOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
	// This suite targets the subset of tonops that is both run_method-compatible
	// and currently stable against the official reference harness. Transaction-only
	// gas ops such as ACCEPT/SETGASLIMIT/COMMIT stay local-only or are covered by
	// dedicated emulator/transaction parity suites when the run_method harness would
	// compare different environments.

	configValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	// Config param 8 is part of the C7 fixtures for config-reading tonops.
	// The raw reference runner itself stays on referenceRawRunGlobalVersion.
	globalVersionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: vm.MaxSupportedGlobalVersion})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	configRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
		7:                                    configValue,
		uint32(tlb.ConfigParamGlobalVersion): globalVersionCell,
	})
	negativeConfigValue := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	signedConfigRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
		^uint32(0):                           negativeConfigValue,
		uint32(tlb.ConfigParamGlobalVersion): globalVersionCell,
	})
	defaultRefCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, vm.MaxSupportedGlobalVersion))
	feeC7 := feeTestC7(t)
	conflictingRootFeeC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     tonopsCrossConflictingFeeConfig(t, globalVersionCell),
		UnpackedConfig: feeTestUnpackedConfig(t),
	})
	myCode := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	inMsgParams := tuple.NewTupleValue(
		big.NewInt(1),
		big.NewInt(0),
		cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice(),
		big.NewInt(333),
		big.NewInt(tonopsTestLogicalTime),
		big.NewInt(tonopsTestTime.Unix()),
		big.NewInt(4444),
		tuple.NewTupleValue(big.NewInt(5555), cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell()),
		cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
	)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:    configRoot,
		MyCode:        myCode,
		StorageFees:   tonopsTestStorageFees,
		IncomingValue: tuple.NewTupleValue(big.NewInt(555), cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()),
		Balance:       tuple.NewTupleValue(big.NewInt(123456789), cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()),
		Globals: map[int]any{
			1: int64(111),
			2: int64(222),
		},
		ExtraParams: map[int]any{
			2:   int64(42),
			15:  int64(6666),
			17:  inMsgParams,
			200: int64(20042),
		},
	})
	shortRandC7 := tuple.NewTupleValue(tuple.NewTupleValue())
	oversizedParams := tuple.NewTupleSized(256)
	mustSetTupleValue(t, &oversizedParams, 0, big.NewInt(1))
	oversizedParamsC7 := tuple.NewTupleValue(oversizedParams)
	signedConfigC7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot: signedConfigRoot,
	})
	sendMsgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.MustFromNano(big.NewInt(1000), 9),
		IHRFee:      tlb.MustFromNano(big.NewInt(0), 9),
		FwdFee:      tlb.MustFromNano(big.NewInt(0), 9),
		Body:        cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build sendmsg test message: %v", err)
	}
	sendMsgUserFwdFeeCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.MustFromNano(big.NewInt(1000), 9),
		IHRFee:      tlb.MustFromNano(big.NewInt(0), 9),
		FwdFee:      tlb.MustFromNano(big.NewInt(500), 9),
		Body:        cell.BeginCell().MustStoreUInt(0xAC, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build sendmsg fwd-fee test message: %v", err)
	}
	sendMsgPrices := tlb.ConfigMsgForwardPrices{LumpPrice: 1}
	sendMsgVersionConfig := func(version uint32) *cell.Cell {
		return tonopsCrossSendMsgConfig(t, version, sendMsgPrices)
	}
	sendMsgVersionC7 := func(version uint32) tuple.Tuple {
		root := sendMsgVersionConfig(version)
		return makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     root,
			UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, sendMsgPrices),
		})
	}
	sendMsgVersionRefCfg := func(version uint32) *referenceGetMethodConfig {
		return tonopsCrossRefConfig(sendMsgVersionConfig(version))
	}
	sendMsgRootPrices := tlb.ConfigMsgForwardPrices{LumpPrice: 11}
	sendMsgUnpackedPrices := tlb.ConfigMsgForwardPrices{LumpPrice: 99}
	sendMsgConflictingConfig := func(version uint32) *cell.Cell {
		return tonopsCrossSendMsgConfig(t, version, sendMsgRootPrices)
	}
	sendMsgConflictingC7 := func(version uint32) tuple.Tuple {
		root := sendMsgConflictingConfig(version)
		return makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     root,
			UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, sendMsgUnpackedPrices),
		})
	}
	sendMsgConflictingRefCfg := func(version uint32) *referenceGetMethodConfig {
		return tonopsCrossRefConfig(sendMsgConflictingConfig(version))
	}
	dataSizeLeaf := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	dataSizeRoot := cell.BeginCell().MustStoreRef(dataSizeLeaf).MustStoreRef(dataSizeLeaf).EndCell()
	stdAddrSlice := cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice()
	extAddrSlice := cell.BeginCell().MustStoreAddr(address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32))).ToSlice()
	varAddrSlice := cell.BeginCell().MustStoreAddr(address.NewAddressVar(0, tonopsTestAddr.Workchain(), 256, tonopsTestAddr.Data())).ToSlice()
	addrNoneTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
	addrNoneSlice := cell.BeginCell().MustStoreUInt(0, 2).ToSlice()
	shortStdAddrSlice := cell.BeginCell().MustStoreUInt(0b10, 2).ToSlice()
	invalidAnycast := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(31, 5).
		MustStoreSlice(bytes.Repeat([]byte{0xFF}, 4), 31).
		MustStoreInt(int64(tonopsTestAddr.Workchain()), 8).
		MustStoreSlice(tonopsTestAddr.Data(), 256).
		ToSlice()
	shortAnycastVarAddrSlice := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(4, 5).
		MustStoreUInt(0b1010, 4).
		MustStoreUInt(2, 9).
		MustStoreInt(0, 32).
		MustStoreUInt(0, 2).
		ToSlice()

	ecrecoverHash := bytes.Repeat([]byte{0x42}, 32)
	ecrecoverV, ecrecoverR, ecrecoverS, ecrecoverPub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), ecrecoverHash)
	if !ok {
		t.Fatal("failed to build secp256k1 recovery fixture")
	}
	ecrecoverEthV := ecrecoverV + 27
	ecrecoverSuccessGoStack := func() []any {
		return []any{
			int64(ecrecoverPub[0]),
			new(big.Int).SetBytes(ecrecoverPub[1:33]),
			new(big.Int).SetBytes(ecrecoverPub[33:65]),
			int64(-1),
		}
	}
	_, _, _, xonlyBasePub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x67}, 32), bytes.Repeat([]byte{0x22}, 32))
	if !ok {
		t.Fatal("failed to build secp256k1 xonly fixture")
	}
	secpXOnlyKey := new(big.Int).SetBytes(xonlyBasePub[1:33])
	secpTooLargeTweak := new(big.Int).SetBytes(localec.CurveOrderBytes())

	p256Priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate p256 key: %v", err)
	}
	p256HashBytes := bytes.Repeat([]byte{0x55}, 32)
	p256HashDigest := sha256.Sum256(p256HashBytes)
	p256R, p256S, err := ecdsa.Sign(rand.Reader, p256Priv, p256HashDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 hash fixture: %v", err)
	}
	p256SigU := make([]byte, 64)
	copy(p256SigU[32-len(p256R.Bytes()):32], p256R.Bytes())
	copy(p256SigU[64-len(p256S.Bytes()):64], p256S.Bytes())
	p256Pub := elliptic.MarshalCompressed(elliptic.P256(), p256Priv.PublicKey.X, p256Priv.PublicKey.Y)

	p256SliceData := []byte("p256-signed-slice")
	p256SliceDigest := sha256.Sum256(p256SliceData)
	p256SliceR, p256SliceS, err := ecdsa.Sign(rand.Reader, p256Priv, p256SliceDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 slice fixture: %v", err)
	}
	p256SigS := make([]byte, 64)
	copy(p256SigS[32-len(p256SliceR.Bytes()):32], p256SliceR.Bytes())
	copy(p256SigS[64-len(p256SliceS.Bytes()):64], p256SliceS.Bytes())
	badP256Key := append([]byte{0x05}, bytes.Repeat([]byte{0x01}, 32)...)
	edKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize))
	edPub := edKey.Public().(ed25519.PublicKey)
	edHash := bytes.Repeat([]byte{0x44}, 32)
	edSigU := ed25519.Sign(edKey, edHash)
	edSliceData := []byte("ed25519-signed-slice")
	edSigS := ed25519.Sign(edKey, edSliceData)
	edZeroKey := big.NewInt(0)
	edIdentityKey := new(big.Int).Lsh(big.NewInt(1), 248)
	forgedEdSig := make([]byte, ed25519.SignatureSize)
	forgedEdSig[0] = 1
	extraCurrency := cell.NewDict(32)
	if _, err = extraCurrency.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreVarUInt(12345, 32),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("failed to seed extra currency dict: %v", err)
	}

	type testCase struct {
		name             string
		code             *cell.Cell
		stack            []any
		exit             int32
		c7               tuple.Tuple
		globalVersion    int
		hasGlobalVersion bool
		refCfg           *referenceGetMethodConfig
		skipReference    string
		goStack          []any
	}
	ecrecoverEthereumReferenceSkip := func(version int) string {
		if version >= 14 {
			return "bundled reference emulator predates upstream ECRECOVER v=27/28 support"
		}
		return ""
	}
	sendMsgUserFwdFeeReferenceSkip := func(version int) string {
		if version >= 14 {
			return "bundled reference emulator predates upstream SENDMSG v14 user fwd fee handling"
		}
		return ""
	}
	ed25519SignatureCheckRejectedKeyReferenceSkip := func(version int) string {
		if version >= 14 {
			return "bundled reference emulator predates upstream CHKSIG v14 zero/identity public-key rejection"
		}
		return ""
	}
	ed25519SignatureCheckRejectedKeyGoStack := func(version int) []any {
		if version >= 14 {
			return []any{int64(0)}
		}
		return nil
	}
	sendMsgUserFwdFeeGoStack := func(version int) []any {
		if version >= 14 {
			return []any{int64(1)}
		}
		return nil
	}

	tests := []testCase{
		{
			name: "ecrecover_success",
			code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(ecrecoverV),
				ecrecoverR,
				ecrecoverS,
			},
			exit: 0,
		},
		{
			name: "ecrecover_invalid_v",
			code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(4),
				ecrecoverR,
				ecrecoverS,
			},
			exit: 0,
		},
		{
			name: "ecrecover_ethereum_v_rejected_v13",
			code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(ecrecoverEthV),
				ecrecoverR,
				ecrecoverS,
			},
			exit:          0,
			globalVersion: 13,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 13)),
		},
		{
			name: "ecrecover_ethereum_v_accepted_v14",
			code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(ecrecoverHash),
				int64(ecrecoverEthV),
				ecrecoverR,
				ecrecoverS,
			},
			exit:          0,
			globalVersion: 14,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 14)),
			skipReference: ecrecoverEthereumReferenceSkip(14),
			goStack:       ecrecoverSuccessGoStack(),
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_success",
			code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				secpXOnlyKey,
				int64(7),
			},
			exit: 0,
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_invalid_key",
			code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32)),
				int64(1),
			},
			exit: 0,
		},
		{
			name: "secp256k1_xonly_pubkey_tweak_add_tweak_ge_n",
			code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack: []any{
				secpXOnlyKey,
				secpTooLargeTweak,
			},
			exit: 0,
		},
		{
			name: "chksignu_success",
			code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(edHash),
				cell.BeginCell().MustStoreSlice(edSigU, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
			exit: 0,
		},
		{
			name: "chksignu_bad_key",
			code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(edHash),
				cell.BeginCell().MustStoreSlice(edSigU, 512).ToSlice(),
				new(big.Int).SetBytes(bytes.Repeat([]byte{0x22}, 32)),
			},
			exit: 0,
		},
		{
			name: "chksignu_identity_key_rejected_v14",
			code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(edHash),
				cell.BeginCell().MustStoreSlice(forgedEdSig, 512).ToSlice(),
				edIdentityKey,
			},
			exit:          0,
			globalVersion: 14,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 14)),
			skipReference: ed25519SignatureCheckRejectedKeyReferenceSkip(14),
			goStack:       ed25519SignatureCheckRejectedKeyGoStack(14),
		},
		{
			name: "chksignu_zero_key_rejected_v14",
			code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(edHash),
				cell.BeginCell().MustStoreSlice(forgedEdSig, 512).ToSlice(),
				edZeroKey,
			},
			exit:          0,
			globalVersion: 14,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 14)),
			skipReference: ed25519SignatureCheckRejectedKeyReferenceSkip(14),
			goStack:       ed25519SignatureCheckRejectedKeyGoStack(14),
		},
		{
			name: "chksigns_success",
			code: codeFromBuilders(t, funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(edSliceData, uint(len(edSliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(edSigS, 512).ToSlice(),
				new(big.Int).SetBytes(edPub),
			},
			exit: 0,
		},
		{
			name: "chksigns_identity_key_rejected_v14",
			code: codeFromBuilders(t, funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(edSliceData, uint(len(edSliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(forgedEdSig, 512).ToSlice(),
				edIdentityKey,
			},
			exit:          0,
			globalVersion: 14,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 14)),
			skipReference: ed25519SignatureCheckRejectedKeyReferenceSkip(14),
			goStack:       ed25519SignatureCheckRejectedKeyGoStack(14),
		},
		{
			name: "chksigns_zero_key_rejected_v14",
			code: codeFromBuilders(t, funcsop.CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(edSliceData, uint(len(edSliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(forgedEdSig, 512).ToSlice(),
				edZeroKey,
			},
			exit:          0,
			globalVersion: 14,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 14)),
			skipReference: ed25519SignatureCheckRejectedKeyReferenceSkip(14),
			goStack:       ed25519SignatureCheckRejectedKeyGoStack(14),
		},
		{
			name: "p256_chksignu_success",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
			exit: 0,
		},
		{
			name: "p256_chksignu_bad_key",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()),
			stack: []any{
				new(big.Int).SetBytes(p256HashBytes),
				cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice(),
			},
			exit: 0,
		},
		{
			name: "p256_chksigns_success",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice(p256SliceData, uint(len(p256SliceData))*8).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
			exit: 0,
		},
		{
			name: "p256_chksigns_unaligned_slice",
			code: codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
				cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
			},
			exit: vmerr.CodeCellUnderflow,
		},
		{
			name: "getparam_generic",
			code: codeFromBuilders(t, funcsop.GETPARAM(2).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "now",
			code: codeFromBuilders(t, funcsop.NOW().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "getparamlong_high_index",
			code: codeFromBuilders(t, funcsop.GETPARAMLONG(200).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "globalid",
			code: codeFromBuilders(t, funcsop.GLOBALID().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "gasconsumed_after_push",
			code: codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize(), funcsop.GASCONSUMED().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "accept_then_gasconsumed",
			code: codeFromBuilders(t, funcsop.ACCEPT().Serialize(), funcsop.GASCONSUMED().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name:  "setgaslimit_then_push",
			code:  codeFromBuilders(t, funcsop.SETGASLIMIT().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(1000)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setgaslimit_too_low_out_of_gas",
			code:  codeFromBuilders(t, funcsop.SETGASLIMIT().Serialize()),
			stack: []any{int64(1)},
			exit:  int32(^vmerr.CodeOutOfGas),
			c7:    c7,
		},
		{
			name:  "setgaslimit_huge_then_gasconsumed",
			code:  codeFromBuilders(t, funcsop.SETGASLIMIT().Serialize(), funcsop.GASCONSUMED().Serialize()),
			stack: []any{new(big.Int).Lsh(big.NewInt(1), 80)},
			exit:  0,
			c7:    c7,
		},
		{
			name: "commit_then_push",
			code: codeFromBuilders(t, funcsop.COMMIT().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "setcp_zero",
			code: codeFromBuilders(t, funcsop.SETCP(0).Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name:  "setcpx_zero",
			code:  codeFromBuilders(t, funcsop.SETCPX().Serialize(), stackop.PUSHINT(big.NewInt(7)).Serialize()),
			stack: []any{int64(0)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setcpx_unsupported",
			code:  codeFromBuilders(t, funcsop.SETCPX().Serialize()),
			stack: []any{int64(1)},
			exit:  int32(vmerr.CodeInvalidOpcode),
			c7:    c7,
		},
		{
			name: "blocklt",
			code: codeFromBuilders(t, funcsop.BLOCKLT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "ltime",
			code: codeFromBuilders(t, funcsop.LTIME().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "randseed",
			code: codeFromBuilders(t, funcsop.RANDSEED().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "balance",
			code: codeFromBuilders(t, funcsop.BALANCE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "myaddr",
			code: codeFromBuilders(t, funcsop.MYADDR().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "configroot",
			code: codeFromBuilders(t, funcsop.CONFIGROOT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "mycode",
			code: codeFromBuilders(t, funcsop.MYCODE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "incomingvalue",
			code: codeFromBuilders(t, funcsop.INCOMINGVALUE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "storagefees",
			code: codeFromBuilders(t, funcsop.STORAGEFEES().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "unpackedconfigtuple",
			code: codeFromBuilders(t, funcsop.UNPACKEDCONFIGTUPLE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "duepayment",
			code: codeFromBuilders(t, funcsop.DUEPAYMENT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "configdict",
			code: codeFromBuilders(t, funcsop.CONFIGDICT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name:  "configparam_hit",
			code:  codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "configparam_signed_negative_key",
			code:  codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(-1)},
			exit:  0,
			c7:    signedConfigC7,
		},
		{
			name:  "configparam_miss",
			code:  codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			stack: []any{int64(99)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "configoptparam_hit",
			code:  codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "configoptparam_miss",
			code:  codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize()),
			stack: []any{int64(99)},
			exit:  0,
			c7:    c7,
		},
		{
			name: "getglob_fixed",
			code: codeFromBuilders(t, funcsop.GETGLOB(1).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name:  "getglobvar",
			code:  codeFromBuilders(t, funcsop.GETGLOBVAR().Serialize()),
			stack: []any{int64(2)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglob_fixed_then_get",
			code:  codeFromBuilders(t, funcsop.SETGLOB(2).Serialize(), funcsop.GETGLOB(2).Serialize()),
			stack: []any{int64(456)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglobvar_then_get",
			code:  codeFromBuilders(t, funcsop.SETGLOBVAR().Serialize(), stackop.PUSHINT(big.NewInt(5)).Serialize(), funcsop.GETGLOBVAR().Serialize()),
			stack: []any{int64(999), int64(5)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglobvar_nil_noop",
			code:  codeFromBuilders(t, funcsop.SETGLOBVAR().Serialize(), stackop.PUSHINT(big.NewInt(10)).Serialize(), funcsop.GETGLOBVAR().Serialize()),
			stack: []any{nil, int64(10)},
			exit:  0,
			c7:    c7,
		},
		{
			name:  "setglobvar_one_item_underflow_precedes_type",
			code:  codeFromBuilders(t, funcsop.SETGLOBVAR().Serialize()),
			stack: []any{nil},
			exit:  int32(vmerr.CodeStackUnderflow),
			c7:    c7,
		},
		{
			name: "getparam_rejects_oversized_params_tuple",
			code: codeFromBuilders(t, funcsop.GETPARAM(0).Serialize()),
			exit: int32(vmerr.CodeTypeCheck),
			c7:   oversizedParamsC7,
		},
		{
			name: "inmsgparams",
			code: codeFromBuilders(t, funcsop.INMSGPARAMS().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsgparam_long_form",
			code: codeFromBuilders(t, funcsop.INMSGPARAM(2).Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_value_alias",
			code: codeFromBuilders(t, funcsop.INMSG_VALUE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_bounce_alias",
			code: codeFromBuilders(t, funcsop.INMSG_BOUNCE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_bounced_alias",
			code: codeFromBuilders(t, funcsop.INMSG_BOUNCED().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_src_alias",
			code: codeFromBuilders(t, funcsop.INMSG_SRC().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_fwdfee_alias",
			code: codeFromBuilders(t, funcsop.INMSG_FWDFEE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_lt_alias",
			code: codeFromBuilders(t, funcsop.INMSG_LT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_utime_alias",
			code: codeFromBuilders(t, funcsop.INMSG_UTIME().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_origvalue_alias",
			code: codeFromBuilders(t, funcsop.INMSG_ORIGVALUE().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_valueextra_alias",
			code: codeFromBuilders(t, funcsop.INMSG_VALUEEXTRA().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "inmsg_stateinit_alias",
			code: codeFromBuilders(t, funcsop.INMSG_STATEINIT().Serialize()),
			exit: 0,
			c7:   c7,
		},
		{
			name: "prevblocksinfotuple",
			code: codeFromBuilders(t, funcsop.PREVBLOCKSINFOTUPLE().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "prevmcblocks",
			code: codeFromBuilders(t, funcsop.PREVMCBLOCKS().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "prevkeyblock",
			code: codeFromBuilders(t, funcsop.PREVKEYBLOCK().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "prevmcblocks_100",
			code: codeFromBuilders(t, funcsop.PREVMCBLOCKS_100().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "getprecompiledgas",
			code: codeFromBuilders(t, funcsop.GETPRECOMPILEDGAS().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name: "randu256",
			code: codeFromBuilders(t, funcsop.RANDU256().Serialize()),
			exit: 0,
			c7:   feeC7,
		},
		{
			name:  "rand",
			code:  codeFromBuilders(t, funcsop.RAND().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setrand",
			code:  codeFromBuilders(t, funcsop.SETRAND().Serialize(), funcsop.RANDSEED().Serialize()),
			stack: []any{big.NewInt(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setrand_extends_short_c7_params",
			code:  codeFromBuilders(t, funcsop.SETRAND().Serialize(), funcsop.RANDSEED().Serialize()),
			stack: []any{big.NewInt(7)},
			exit:  0,
			c7:    shortRandC7,
		},
		{
			name:  "addrand",
			code:  codeFromBuilders(t, funcsop.ADDRAND().Serialize(), funcsop.RANDSEED().Serialize()),
			stack: []any{big.NewInt(7)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getstoragefee",
			code:  codeFromBuilders(t, funcsop.GETSTORAGEFEE().Serialize()),
			stack: []any{int64(2), int64(3), int64(10), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getstoragefee_uses_unpacked_config",
			code:  codeFromBuilders(t, funcsop.GETSTORAGEFEE().Serialize()),
			stack: []any{int64(2), int64(3), int64(10), int64(0)},
			exit:  0,
			c7:    conflictingRootFeeC7,
		},
		{
			name:  "getgasfee",
			code:  codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			stack: []any{int64(250), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getgasfee_uses_unpacked_config",
			code:  codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			stack: []any{int64(250), int64(0)},
			exit:  0,
			c7:    conflictingRootFeeC7,
		},
		{
			name:  "getforwardfee",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEE().Serialize()),
			stack: []any{int64(2), int64(8), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getforwardfee_uses_unpacked_config",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEE().Serialize()),
			stack: []any{int64(2), int64(8), int64(0)},
			exit:  0,
			c7:    conflictingRootFeeC7,
		},
		{
			name:  "getoriginalfwdfee",
			code:  codeFromBuilders(t, funcsop.GETORIGINALFWDFEE().Serialize()),
			stack: []any{big.NewInt(3200), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getforwardfeesimple",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEESIMPLE().Serialize()),
			stack: []any{int64(2), int64(8), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getgasfeesimple",
			code:  codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()),
			stack: []any{int64(250), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "getextrabalance_nan_id_range",
			code:  codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack: []any{vm.NaN{}},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "sha256u",
			code:  codeFromBuilders(t, funcsop.SHA256U().Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "hashext",
			code:  codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name: "hashext_bit_concat_success",
			code: codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice(),
				cell.BeginCell().MustStoreUInt(0xB, 4).ToSlice(),
				int64(2),
			},
			exit: 0,
			c7:   feeC7,
		},
		{
			name:  "hashext_unaligned_total_caught",
			code:  codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(), int64(1)},
			exit:  int32(vmerr.CodeCellUnderflow),
			c7:    feeC7,
		},
		{
			name: "hashext_type_error_drops_items",
			code: codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
			stack: []any{
				cell.BeginCell().MustStoreSlice([]byte("ok"), 16).ToSlice(),
				int64(777),
				int64(2),
			},
			exit: int32(vmerr.CodeTypeCheck),
			c7:   feeC7,
		},
		{
			name:  "hashext_sha512_tuple",
			code:  codeFromBuilders(t, funcsop.HASHEXT(1).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "hashext_blake2b512_tuple",
			code:  codeFromBuilders(t, funcsop.HASHEXT(2).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "hashext_keccak512_tuple",
			code:  codeFromBuilders(t, funcsop.HASHEXT(4).Serialize()),
			stack: []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "cdatasize",
			code:  codeFromBuilders(t, funcsop.CDATASIZE().Serialize()),
			stack: []any{dataSizeRoot, int64(10)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sdatasize",
			code:  codeFromBuilders(t, funcsop.SDATASIZE().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), int64(10)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sdatasizeq_success",
			code:  codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize()),
			stack: []any{dataSizeRoot.MustBeginParse(), int64(10)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldvarint16",
			code:  codeFromBuilders(t, funcsop.LDVARINT16().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 4).MustStoreInt(-17, 8).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldvaruint32",
			code:  codeFromBuilders(t, funcsop.LDVARUINT32().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(17, 8).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldvarint32",
			code:  codeFromBuilders(t, funcsop.LDVARINT32().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(1, 5).MustStoreInt(-17, 8).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "stvarint16",
			code:  codeFromBuilders(t, funcsop.STVARINT16().Serialize()),
			stack: []any{cell.BeginCell(), int64(-17)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "stvarint32",
			code:  codeFromBuilders(t, funcsop.STVARINT32().Serialize()),
			stack: []any{cell.BeginCell(), int64(-17)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "stvaruint32",
			code:  codeFromBuilders(t, funcsop.STVARUINT32().Serialize()),
			stack: []any{cell.BeginCell(), int64(17)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddr_ok",
			code:  codeFromBuilders(t, funcsop.LDMSGADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddrq_ok",
			code:  codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddr_none",
			code:  codeFromBuilders(t, funcsop.LDMSGADDR().Serialize()),
			stack: []any{addrNoneSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddr_ext",
			code:  codeFromBuilders(t, funcsop.LDMSGADDR().Serialize()),
			stack: []any{extAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddrq_short_std",
			code:  codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack: []any{shortStdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "ldmsgaddrq_fail",
			code:  codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack: []any{cell.BeginCell().MustStoreUInt(0b11, 2).ToSlice()},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "parsemsgaddr",
			code:  codeFromBuilders(t, funcsop.PARSEMSGADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "parsemsgaddr_ext",
			code:  codeFromBuilders(t, funcsop.PARSEMSGADDR().Serialize()),
			stack: []any{extAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "parsemsgaddrq_short_std",
			code:  codeFromBuilders(t, funcsop.PARSEMSGADDRQ().Serialize()),
			stack: []any{shortStdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "parsemsgaddrq_invalid_anycast",
			code:  codeFromBuilders(t, funcsop.PARSEMSGADDRQ().Serialize()),
			stack: []any{invalidAnycast},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritestdaddr",
			code:  codeFromBuilders(t, funcsop.REWRITESTDADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritestdaddrq",
			code:  codeFromBuilders(t, funcsop.REWRITESTDADDRQ().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritevaraddr",
			code:  codeFromBuilders(t, funcsop.REWRITEVARADDR().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritevaraddrq",
			code:  codeFromBuilders(t, funcsop.REWRITEVARADDRQ().Serialize()),
			stack: []any{stdAddrSlice},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rewritevaraddr_var_underflow",
			code:  codeFromBuilders(t, funcsop.REWRITEVARADDR().Serialize()),
			stack: []any{varAddrSlice},
			exit:  int32(vmerr.CodeCellUnderflow),
			c7:    feeC7,
		},
		{
			name:             "rewritevaraddr_short_anycast_prefix_v9",
			code:             codeFromBuilders(t, funcsop.REWRITEVARADDR().Serialize()),
			stack:            []any{shortAnycastVarAddrSlice},
			exit:             int32(vmerr.CodeCellUnderflow),
			c7:               feeC7,
			globalVersion:    9,
			hasGlobalVersion: true,
			refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 9)),
		},
		{
			name:             "rewritevaraddrq_short_anycast_prefix_v9",
			code:             codeFromBuilders(t, funcsop.REWRITEVARADDRQ().Serialize()),
			stack:            []any{shortAnycastVarAddrSlice},
			exit:             0,
			c7:               feeC7,
			globalVersion:    9,
			hasGlobalVersion: true,
			refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 9)),
		},
		{
			name:   "ldstdaddr",
			code:   codeFromBuilders(t, funcsop.LDSTDADDR().Serialize()),
			stack:  []any{stdAddrSlice},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ldstdaddrq",
			code:   codeFromBuilders(t, funcsop.LDSTDADDRQ().Serialize()),
			stack:  []any{stdAddrSlice},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ldstdaddrq_var_fail",
			code:   codeFromBuilders(t, funcsop.LDSTDADDRQ().Serialize()),
			stack:  []any{varAddrSlice},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ldoptstdaddr_none",
			code:   codeFromBuilders(t, funcsop.LDOPTSTDADDR().Serialize()),
			stack:  []any{addrNoneTail},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ldoptstdaddrq_none",
			code:   codeFromBuilders(t, funcsop.LDOPTSTDADDRQ().Serialize()),
			stack:  []any{addrNoneTail},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ldoptstdaddrq_std",
			code:   codeFromBuilders(t, funcsop.LDOPTSTDADDRQ().Serialize()),
			stack:  []any{stdAddrSlice},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ststdaddr",
			code:   codeFromBuilders(t, funcsop.STSTDADDR().Serialize()),
			stack:  []any{stdAddrSlice, cell.BeginCell()},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ststdaddrq",
			code:   codeFromBuilders(t, funcsop.STSTDADDRQ().Serialize()),
			stack:  []any{stdAddrSlice, cell.BeginCell()},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "ststdaddrq_var_fail",
			code:   codeFromBuilders(t, funcsop.STSTDADDRQ().Serialize()),
			stack:  []any{varAddrSlice, cell.BeginCell()},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "stoptstdaddr_none",
			code:   codeFromBuilders(t, funcsop.STOPTSTDADDR().Serialize()),
			stack:  []any{nil, cell.BeginCell()},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "stoptstdaddrq_none",
			code:   codeFromBuilders(t, funcsop.STOPTSTDADDRQ().Serialize()),
			stack:  []any{nil, cell.BeginCell()},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:   "stoptstdaddr_std",
			code:   codeFromBuilders(t, funcsop.STOPTSTDADDR().Serialize()),
			stack:  []any{stdAddrSlice, cell.BeginCell()},
			exit:   0,
			c7:     feeC7,
			refCfg: defaultRefCfg,
		},
		{
			name:  "rawreserve",
			code:  codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack: []any{big.NewInt(777), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rawreserve_mode_2",
			code:  codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack: []any{big.NewInt(777), int64(2)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:          "rawreserve_mode_16_rejected_v3",
			code:          codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack:         []any{big.NewInt(777), int64(16)},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 3,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 3)),
		},
		{
			name:          "rawreserve_mode_16_allowed_v4",
			code:          codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack:         []any{big.NewInt(777), int64(16)},
			exit:          0,
			globalVersion: 4,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 4)),
		},
		{
			name:  "rawreserve_negative_range",
			code:  codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack: []any{big.NewInt(-1), int64(0)},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "rawreservex",
			code:  codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize()),
			stack: []any{big.NewInt(777), nil, int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "rawreservex_extra_currency",
			code:  codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize()),
			stack: []any{big.NewInt(777), extraCurrency.AsCell(), int64(2)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:          "rawreservex_mode_16_rejected_v3",
			code:          codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize()),
			stack:         []any{big.NewInt(777), extraCurrency.AsCell(), int64(16)},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 3,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 3)),
		},
		{
			name:          "rawreservex_mode_16_allowed_v4",
			code:          codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize()),
			stack:         []any{big.NewInt(777), extraCurrency.AsCell(), int64(16)},
			exit:          0,
			globalVersion: 4,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 4)),
		},
		{
			name:  "setcode",
			code:  codeFromBuilders(t, funcsop.SETCODE().Serialize()),
			stack: []any{sendMsgCell},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setlibcode",
			code:  codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack: []any{sendMsgCell, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "setlibcode_mode_0",
			code:  codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack: []any{sendMsgCell, int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:          "setlibcode_mode_16_rejected_v3",
			code:          codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack:         []any{sendMsgCell, int64(16)},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 3,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 3)),
		},
		{
			name:          "setlibcode_mode_16_allowed_v4",
			code:          codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack:         []any{sendMsgCell, int64(16)},
			exit:          0,
			globalVersion: 4,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 4)),
		},
		{
			name:  "setlibcode_invalid_mode",
			code:  codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack: []any{sendMsgCell, int64(4)},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "changelib",
			code:  codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack: []any{big.NewInt(1), int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "changelib_negative_hash",
			code:  codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack: []any{big.NewInt(-1), int64(0)},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name:  "changelib_mode_0",
			code:  codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack: []any{big.NewInt(1), int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:          "changelib_mode_16_rejected_v3",
			code:          codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack:         []any{big.NewInt(1), int64(16)},
			exit:          int32(vmerr.CodeRangeCheck),
			globalVersion: 3,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 3)),
		},
		{
			name:          "changelib_mode_16_allowed_v4",
			code:          codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack:         []any{big.NewInt(1), int64(16)},
			exit:          0,
			globalVersion: 4,
			refCfg:        tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, 4)),
		},
		{
			name:  "sendmsg_fee_only",
			code:  codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsgCell, int64(1024)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:          "sendmsg_legacy_root_prices_with_conflicting_c7",
			code:          codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack:         []any{sendMsgCell, int64(1024)},
			exit:          0,
			c7:            sendMsgConflictingC7(5),
			globalVersion: 5,
			refCfg:        sendMsgConflictingRefCfg(5),
		},
		{
			name:          "sendmsg_user_fwd_fee_lower_bound_gv13",
			code:          codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack:         []any{sendMsgUserFwdFeeCell, int64(1024)},
			exit:          0,
			c7:            sendMsgVersionC7(13),
			globalVersion: 13,
			refCfg:        sendMsgVersionRefCfg(13),
		},
		{
			name:  "sendmsg_send",
			code:  codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsgCell, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sendmsg_ignore_errors_mode",
			code:  codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsgCell, int64(3)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sendmsg_invalid_mode_range",
			code:  codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack: []any{sendMsgCell, int64(256)},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
		{
			name: "sendrawmsg_visible_c5",
			code: codeFromBuilders(t,
				funcsop.SENDRAWMSG().Serialize(),
				execop.PUSHCTR(5).Serialize(),
			),
			stack: []any{sendMsgCell, int64(1)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name: "sendrawmsg_mode_0_visible_c5",
			code: codeFromBuilders(t,
				funcsop.SENDRAWMSG().Serialize(),
				execop.PUSHCTR(5).Serialize(),
			),
			stack: []any{sendMsgCell, int64(0)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name: "sendrawmsg_mode_128_visible_c5",
			code: codeFromBuilders(t,
				funcsop.SENDRAWMSG().Serialize(),
				execop.PUSHCTR(5).Serialize(),
			),
			stack: []any{sendMsgCell, int64(128)},
			exit:  0,
			c7:    feeC7,
		},
		{
			name:  "sendrawmsg_invalid_mode_range",
			code:  codeFromBuilders(t, funcsop.SENDRAWMSG().Serialize()),
			stack: []any{sendMsgCell, int64(256)},
			exit:  int32(vmerr.CodeRangeCheck),
			c7:    feeC7,
		},
	}
	versionedExit := func(version, min int, exit int32) int32 {
		if version < min {
			return int32(vmerr.CodeInvalidOpcode)
		}
		return exit
	}
	versionedParamsC7 := func(version int) tuple.Tuple {
		configRoot := tonopsCrossConfigWithGlobalVersion(t, uint32(version))
		return makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot: configRoot,
			ExtraParams: map[int]any{
				13:  tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
				15:  int64(444),
				16:  int64(555),
				17:  inMsgParams,
				200: int64(20042),
			},
		})
	}
	versionedFeeC7 := func(version int) tuple.Tuple {
		return makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     tonopsCrossConfigWithGlobalVersion(t, uint32(version)),
			UnpackedConfig: feeTestUnpackedConfig(t),
			Balance:        tuple.NewTupleValue(new(big.Int).Set(tonopsTestBalance), extraCurrency.AsCell()),
		})
	}
	for _, version := range tonOpsVersionCrossEmulatorVersions(t) {
		changelibExit := int32(0)
		if version < 4 {
			changelibExit = int32(vmerr.CodeRangeCheck)
		}
		sendMsgExit := int32(0)
		if version < 4 {
			sendMsgExit = int32(vmerr.CodeInvalidOpcode)
		}
		paramsC7 := versionedParamsC7(version)
		feeVersionC7 := versionedFeeC7(version)
		versionRefCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
		tests = append(tests,
			testCase{
				name:             fmt.Sprintf("changelib_mode_16_v%d", version),
				code:             codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
				stack:            []any{big.NewInt(1), int64(16)},
				exit:             changelibExit,
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
			},
			testCase{
				name:             fmt.Sprintf("sendmsg_user_fwd_fee_lower_bound_v%d", version),
				code:             codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
				stack:            []any{sendMsgUserFwdFeeCell, int64(1024)},
				exit:             sendMsgExit,
				c7:               sendMsgVersionC7(uint32(version)),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           sendMsgVersionRefCfg(uint32(version)),
				skipReference:    sendMsgUserFwdFeeReferenceSkip(version),
				goStack:          sendMsgUserFwdFeeGoStack(version),
			},
			testCase{
				name:             fmt.Sprintf("sendmsg_mode128_balance_fee_only_v%d", version),
				code:             codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
				stack:            []any{sendMsgCell, int64(1024 + 128)},
				exit:             sendMsgExit,
				c7:               sendMsgVersionC7(uint32(version)),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           sendMsgVersionRefCfg(uint32(version)),
			},
			testCase{
				name:             fmt.Sprintf("gasconsumed_after_push_v%d", version),
				code:             codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize(), funcsop.GASCONSUMED().Serialize()),
				exit:             versionedExit(version, 4, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getparamlong_randseed_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETPARAMLONG(6).Serialize()),
				exit:             versionedExit(version, 11, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getparamlong_high_index_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETPARAMLONG(200).Serialize()),
				exit:             versionedExit(version, 11, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("prevblocksinfotuple_v%d", version),
				code:             codeFromBuilders(t, funcsop.PREVBLOCKSINFOTUPLE().Serialize()),
				exit:             0,
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("prevmcblocks_v%d", version),
				code:             codeFromBuilders(t, funcsop.PREVMCBLOCKS().Serialize()),
				exit:             versionedExit(version, 4, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("prevkeyblock_v%d", version),
				code:             codeFromBuilders(t, funcsop.PREVKEYBLOCK().Serialize()),
				exit:             versionedExit(version, 4, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("prevmcblocks_100_v%d", version),
				code:             codeFromBuilders(t, funcsop.PREVMCBLOCKS_100().Serialize()),
				exit:             versionedExit(version, 9, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getprecompiledgas_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETPRECOMPILEDGAS().Serialize()),
				exit:             versionedExit(version, 6, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("inmsgparams_v%d", version),
				code:             codeFromBuilders(t, funcsop.INMSGPARAMS().Serialize()),
				exit:             versionedExit(version, 11, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("inmsgparam_long_src_v%d", version),
				code:             codeFromBuilders(t, funcsop.INMSGPARAM(2).Serialize()),
				exit:             versionedExit(version, 11, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("inmsg_value_alias_v%d", version),
				code:             codeFromBuilders(t, funcsop.INMSG_VALUE().Serialize()),
				exit:             versionedExit(version, 11, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("inmsg_stateinit_alias_v%d", version),
				code:             codeFromBuilders(t, funcsop.INMSG_STATEINIT().Serialize()),
				exit:             versionedExit(version, 11, 0),
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("randu256_then_randseed_v%d", version),
				code:             codeFromBuilders(t, funcsop.RANDU256().Serialize(), funcsop.RANDSEED().Serialize()),
				exit:             0,
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("rand_then_randseed_v%d", version),
				code:             codeFromBuilders(t, funcsop.RAND().Serialize(), funcsop.RANDSEED().Serialize()),
				stack:            []any{int64(7)},
				exit:             0,
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("setrand_then_randseed_v%d", version),
				code:             codeFromBuilders(t, funcsop.SETRAND().Serialize(), funcsop.RANDSEED().Serialize()),
				stack:            []any{big.NewInt(7)},
				exit:             0,
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("addrand_then_randseed_v%d", version),
				code:             codeFromBuilders(t, funcsop.ADDRAND().Serialize(), funcsop.RANDSEED().Serialize()),
				stack:            []any{big.NewInt(7)},
				exit:             0,
				c7:               paramsC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getstoragefee_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETSTORAGEFEE().Serialize()),
				stack:            []any{int64(2), int64(3), int64(10), int64(0)},
				exit:             versionedExit(version, 6, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getgasfee_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
				stack:            []any{int64(250), int64(0)},
				exit:             versionedExit(version, 6, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getforwardfee_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETFORWARDFEE().Serialize()),
				stack:            []any{int64(2), int64(8), int64(0)},
				exit:             versionedExit(version, 6, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getoriginalfwdfee_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETORIGINALFWDFEE().Serialize()),
				stack:            []any{big.NewInt(3200), int64(0)},
				exit:             versionedExit(version, 6, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getgasfeesimple_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()),
				stack:            []any{int64(250), int64(0)},
				exit:             versionedExit(version, 6, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getforwardfeesimple_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETFORWARDFEESIMPLE().Serialize()),
				stack:            []any{int64(2), int64(8), int64(0)},
				exit:             versionedExit(version, 6, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getextrabalance_hit_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
				stack:            []any{int64(7)},
				exit:             versionedExit(version, 10, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("getextrabalance_miss_v%d", version),
				code:             codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
				stack:            []any{int64(8)},
				exit:             versionedExit(version, 10, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("hashext_sha256_v%d", version),
				code:             codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
				stack:            []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1)},
				exit:             versionedExit(version, 4, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("hashext_dynamic_sha512_v%d", version),
				code:             codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
				stack:            []any{cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice(), int64(1), int64(1)},
				exit:             versionedExit(version, 4, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("hashexta_dynamic_sha256_v%d", version),
				code:             codeFromBuilders(t, funcsop.HASHEXT(1<<9|255).Serialize()),
				stack:            []any{cell.BeginCell().MustStoreUInt(0x11, 8), cell.BeginCell().MustStoreUInt(0xABCD, 16), int64(1), int64(0)},
				exit:             versionedExit(version, 4, 0),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("hashext_unaligned_total_v%d", version),
				code:             codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
				stack:            []any{cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(), int64(1)},
				exit:             versionedExit(version, 4, int32(vmerr.CodeCellUnderflow)),
				c7:               feeVersionC7,
				globalVersion:    version,
				hasGlobalVersion: true,
			},
			testCase{
				name:             fmt.Sprintf("ldstdaddr_std_v%d", version),
				code:             codeFromBuilders(t, funcsop.LDSTDADDR().Serialize()),
				stack:            []any{stdAddrSlice},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("ldstdaddrq_var_fail_v%d", version),
				code:             codeFromBuilders(t, funcsop.LDSTDADDRQ().Serialize()),
				stack:            []any{varAddrSlice},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("ldoptstdaddr_none_v%d", version),
				code:             codeFromBuilders(t, funcsop.LDOPTSTDADDR().Serialize()),
				stack:            []any{addrNoneTail},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("ldoptstdaddrq_short_fail_v%d", version),
				code:             codeFromBuilders(t, funcsop.LDOPTSTDADDRQ().Serialize()),
				stack:            []any{shortStdAddrSlice},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("ststdaddr_std_v%d", version),
				code:             codeFromBuilders(t, funcsop.STSTDADDR().Serialize()),
				stack:            []any{stdAddrSlice, cell.BeginCell()},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("ststdaddrq_var_fail_v%d", version),
				code:             codeFromBuilders(t, funcsop.STSTDADDRQ().Serialize()),
				stack:            []any{varAddrSlice, cell.BeginCell()},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("stoptstdaddr_none_v%d", version),
				code:             codeFromBuilders(t, funcsop.STOPTSTDADDR().Serialize()),
				stack:            []any{nil, cell.BeginCell()},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name:             fmt.Sprintf("stoptstdaddrq_std_v%d", version),
				code:             codeFromBuilders(t, funcsop.STOPTSTDADDRQ().Serialize()),
				stack:            []any{stdAddrSlice, cell.BeginCell()},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           versionRefCfg,
			},
			testCase{
				name: fmt.Sprintf("ecrecover_success_v%d", version),
				code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
				stack: []any{
					new(big.Int).SetBytes(ecrecoverHash),
					int64(ecrecoverV),
					ecrecoverR,
					ecrecoverS,
				},
				exit:             versionedExit(version, 4, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
			},
			testCase{
				name: fmt.Sprintf("ecrecover_ethereum_v_%s_v%d", map[bool]string{false: "rejected", true: "accepted"}[version >= 14], version),
				code: codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
				stack: []any{
					new(big.Int).SetBytes(ecrecoverHash),
					int64(ecrecoverEthV),
					ecrecoverR,
					ecrecoverS,
				},
				exit:             versionedExit(version, 4, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
				skipReference:    ecrecoverEthereumReferenceSkip(version),
				goStack:          ecrecoverSuccessGoStack(),
			},
			testCase{
				name: fmt.Sprintf("secp256k1_xonly_pubkey_tweak_add_success_v%d", version),
				code: codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
				stack: []any{
					secpXOnlyKey,
					int64(7),
				},
				exit:             versionedExit(version, 9, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
			},
			testCase{
				name: fmt.Sprintf("chksignu_identity_key_v%d", version),
				code: codeFromBuilders(t, funcsop.CHKSIGNU().Serialize()),
				stack: []any{
					new(big.Int).SetBytes(edHash),
					cell.BeginCell().MustStoreSlice(forgedEdSig, 512).ToSlice(),
					edIdentityKey,
				},
				exit:             0,
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
				skipReference:    ed25519SignatureCheckRejectedKeyReferenceSkip(version),
				goStack:          ed25519SignatureCheckRejectedKeyGoStack(version),
			},
			testCase{
				name: fmt.Sprintf("chksigns_identity_key_v%d", version),
				code: codeFromBuilders(t, funcsop.CHKSIGNS().Serialize()),
				stack: []any{
					cell.BeginCell().MustStoreSlice(edSliceData, uint(len(edSliceData))*8).ToSlice(),
					cell.BeginCell().MustStoreSlice(forgedEdSig, 512).ToSlice(),
					edIdentityKey,
				},
				exit:             0,
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
				skipReference:    ed25519SignatureCheckRejectedKeyReferenceSkip(version),
				goStack:          ed25519SignatureCheckRejectedKeyGoStack(version),
			},
			testCase{
				name: fmt.Sprintf("p256_chksignu_success_v%d", version),
				code: codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()),
				stack: []any{
					new(big.Int).SetBytes(p256HashBytes),
					cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice(),
					cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
				},
				exit:             versionedExit(version, 4, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
			},
			testCase{
				name: fmt.Sprintf("p256_chksigns_unaligned_slice_v%d", version),
				code: codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
				stack: []any{
					cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(),
					cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(),
					cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice(),
				},
				exit:             versionedExit(version, 4, int32(vmerr.CodeCellUnderflow)),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
			},
			testCase{
				name: fmt.Sprintf("hashbu_builder_with_ref_v%d", version),
				code: codeFromBuilders(t, funcsop.HASHBU().Serialize()),
				stack: []any{
					cell.BeginCell().
						MustStoreUInt(0xCA, 8).
						MustStoreRef(cell.BeginCell().MustStoreUInt(0xFE, 8).EndCell()),
				},
				exit:             versionedExit(version, 12, 0),
				globalVersion:    version,
				hasGlobalVersion: true,
				refCfg:           tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version))),
			},
		)
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

			globalVersion := tt.globalVersion
			if !tt.hasGlobalVersion && globalVersion == 0 {
				globalVersion = referenceRawRunGlobalVersion
			}
			goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tt.c7, goStack, globalVersion)
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
			var refRes *crossRunResult
			if tt.refCfg != nil {
				refRes, err = runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *tt.refCfg)
			} else {
				refRes, err = runReferenceCrossCode(code, cell.BeginCell().EndCell(), tt.c7, refStack)
			}
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

func TestTVMCrossEmulatorTonOpsSendMsgVersionedFeeEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range tonOpsVersionCrossEmulatorVersions(t) {
		for rawCase := uint8(0); rawCase < tonOpsSendMsgVersionedFeeCaseCount; rawCase++ {
			rawCase := rawCase
			t.Run(fmt.Sprintf("%s_v%d", tonOpsSendMsgVersionedFeeCaseName(rawCase), version), func(t *testing.T) {
				runTonOpsSendMsgVersionedFeeEdge(t, version, rawCase, uint16(0xA500+uint16(version)), uint64(version+1))
			})
		}
	}
}

func FuzzTVMCrossEmulatorTonOpsSendMsgVersionedFeeEdges(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tonOpsSendMsgVersionedFeeCaseCount), uint16(0xB500+version), uint64(version+1))
	}
	for rawCase := uint8(0); rawCase < tonOpsSendMsgVersionedFeeCaseCount; rawCase++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), rawCase, uint16(0xC500+uint16(rawCase)), uint64(100+rawCase))
	}
	f.Add(uint8(255), uint8(255), uint16(0xFFFF), uint64(1<<20+17))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8, bodyTag uint16, rawAmount uint64) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		runTonOpsSendMsgVersionedFeeEdge(t, version, rawCase, bodyTag, rawAmount)
	})
}

const tonOpsSendMsgVersionedFeeCaseCount = 2

func tonOpsSendMsgVersionedFeeCaseName(rawCase uint8) string {
	switch rawCase % tonOpsSendMsgVersionedFeeCaseCount {
	case 0:
		return "sendmsg_extra_currency_fee"
	default:
		return "sendmsg_ihr_enabled_fee"
	}
}

func runTonOpsSendMsgVersionedFeeEdge(t *testing.T, version int, rawCase uint8, bodyTag uint16, rawAmount uint64) {
	t.Helper()

	msg, c7, refCfg := tonOpsSendMsgVersionedFeeFixture(t, version, rawCase, bodyTag, rawAmount)
	wantExit := int32(0)
	if version < 4 {
		wantExit = int32(vmerr.CodeInvalidOpcode)
	}

	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))
	goStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantExit)
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

func tonOpsSendMsgVersionedFeeFixture(t *testing.T, version int, rawCase uint8, bodyTag uint16, rawAmount uint64) (*cell.Cell, tuple.Tuple, *referenceGetMethodConfig) {
	t.Helper()

	prices := tlb.ConfigMsgForwardPrices{
		LumpPrice: 1,
		BitPrice:  1 << 16,
		CellPrice: 3 << 16,
		FirstFrac: 1 << 15,
	}
	msg := tonOpsSendMsgExtraCurrencyFeeMessage(t, bodyTag, rawAmount)
	if rawCase%tonOpsSendMsgVersionedFeeCaseCount == 1 {
		prices.IHRFactor = 1 << 16
		msg = tonOpsSendMsgIHREnabledFeeMessage(t, bodyTag, rawAmount)
	}

	configRoot := tonopsCrossSendMsgConfig(t, uint32(version), prices)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, prices),
	})
	return msg, c7, tonopsCrossRefConfig(configRoot)
}

func tonOpsSendMsgExtraCurrencyFeeMessage(t *testing.T, bodyTag uint16, rawAmount uint64) *cell.Cell {
	t.Helper()

	extra := cell.NewDict(32)
	if _, err := extra.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(uint64(rawAmount%31)+1, 32).EndCell(),
		cell.BeginCell().MustStoreVarUInt(rawAmount%1_000_000+1, 32),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("failed to seed extra currency dict: %v", err)
	}

	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled:     true,
		SrcAddr:         tonopsTestAddr,
		DstAddr:         tonopsTestAddr,
		Amount:          tlb.FromNanoTONU(rawAmount%10_000 + 1),
		ExtraCurrencies: extra,
		IHRFee:          tlb.FromNanoTONU(0),
		FwdFee:          tlb.FromNanoTONU(0),
		CreatedLT:       1,
		CreatedAt:       uint32(tonopsTestTime.Unix()),
		Body:            cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build extra-currency SENDMSG fixture: %v", err)
	}
	return msg
}

func tonOpsSendMsgIHREnabledFeeMessage(t *testing.T, bodyTag uint16, rawAmount uint64) *cell.Cell {
	t.Helper()

	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: false,
		SrcAddr:     tonopsTestAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(rawAmount%10_000 + 1),
		IHRFee:      tlb.FromNanoTONU(rawAmount%50_000 + 1),
		FwdFee:      tlb.FromNanoTONU(0),
		CreatedLT:   1,
		CreatedAt:   uint32(tonopsTestTime.Unix()),
		Body:        cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build IHR-enabled SENDMSG fixture: %v", err)
	}
	return msg
}

func TestTVMCrossEmulatorTonOpsSendMsgExtraFlagsRootSizeGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range tonOpsVersionCrossEmulatorVersions(t) {
		for _, bodyBits := range []uint{340, 360, 380} {
			for _, extraFlags := range []uint64{0, 256, 65535} {
				bodyBits, extraFlags := bodyBits, extraFlags
				t.Run(fmt.Sprintf("v%d_body%d_extra%d", version, bodyBits, extraFlags), func(t *testing.T) {
					runTonOpsSendMsgExtraFlagsRootSizeGlobalVersion(t, version, bodyBits, extraFlags, uint16(0xD000+uint16(version)))
				})
			}
		}
	}
}

func FuzzTVMCrossEmulatorTonOpsSendMsgExtraFlagsRootSizeGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint16(340), uint64(0), uint16(0xD100+uint16(version)))
		f.Add(uint8(version), uint16(360), uint64(256), uint16(0xD200+uint16(version)))
		f.Add(uint8(version), uint16(380), uint64(65535), uint16(0xD300+uint16(version)))
	}
	f.Add(uint8(255), uint16(419), uint64((1<<48)+17), uint16(0xFFFF))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawBodyBits uint16, rawExtraFlags uint64, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		bodyBits := uint(rawBodyBits % 420)
		extraFlags := rawExtraFlags % 65536
		runTonOpsSendMsgExtraFlagsRootSizeGlobalVersion(t, version, bodyBits, extraFlags, bodyTag)
	})
}

func runTonOpsSendMsgExtraFlagsRootSizeGlobalVersion(t *testing.T, version int, bodyBits uint, extraFlags uint64, bodyTag uint16) {
	t.Helper()

	msg, c7, refCfg := tonOpsSendMsgExtraFlagsRootSizeFixture(t, version, bodyBits, extraFlags, bodyTag)
	wantExit := int32(0)
	if version < funcsop.SENDMSG().MinGlobalVersion() {
		wantExit = int32(vmerr.CodeInvalidOpcode)
	}

	code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))
	goStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(msg, int64(1024))
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantExit)
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

func tonOpsSendMsgExtraFlagsRootSizeFixture(t *testing.T, version int, bodyBits uint, extraFlags uint64, bodyTag uint16) (*cell.Cell, tuple.Tuple, *referenceGetMethodConfig) {
	t.Helper()

	body := cell.BeginCell()
	if bodyBits > 0 && bodyBits <= 16 {
		body.MustStoreUInt(uint64(bodyTag)>>(16-bodyBits), bodyBits)
	} else if bodyBits > 16 {
		body.MustStoreUInt(uint64(bodyTag), 16)
		body.MustStoreSlice(bytes.Repeat([]byte{byte(extraFlags)}, int((bodyBits-16+7)/8)), bodyBits-16)
	}
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		IHRFee:      tlb.FromNanoTONU(extraFlags),
		FwdFee:      tlb.FromNanoTONU(0),
		CreatedLT:   1,
		CreatedAt:   uint32(tonopsTestTime.Unix()),
		Body:        body.EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build extra-flags SENDMSG fixture: %v", err)
	}

	prices := tlb.ConfigMsgForwardPrices{
		CellPrice: 1 << 16,
	}
	configRoot := tonopsCrossSendMsgConfig(t, uint32(version), prices)
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, prices),
	})
	return msg, c7, tonopsCrossRefConfig(configRoot)
}

func TestTVMCrossEmulatorTonOpsUnderflowPrecheckGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := tonOpsUnderflowPrecheckCases(t)
	versions := tonOpsVersionCrossEmulatorVersions(t)
	for _, tt := range tests {
		tt := tt
		for _, version := range versions {
			version := version
			t.Run(fmt.Sprintf("%s_v%d", tt.name, version), func(t *testing.T) {
				runTonOpsUnderflowPrecheckVersionCase(t, tt, version)
			})
		}
	}
}

func FuzzTVMCrossEmulatorTonOpsUnderflowPrecheckGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tonOpsUnderflowPrecheckCaseCount))
	}
	for i := 0; i < tonOpsUnderflowPrecheckCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := tonOpsUnderflowPrecheckCases(t)
		if len(tests) != tonOpsUnderflowPrecheckCaseCount {
			t.Fatalf("tonops underflow precheck case count = %d, want %d", len(tests), tonOpsUnderflowPrecheckCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runTonOpsUnderflowPrecheckVersionCase(t, tt, version)
	})
}

const tonOpsUnderflowPrecheckCaseCount = 8

type tonOpsUnderflowPrecheckCase struct {
	name  string
	code  *cell.Cell
	stack []any
	exit  func(int) int32
}

func tonOpsUnderflowPrecheckCases(t *testing.T) []tonOpsUnderflowPrecheckCase {
	t.Helper()

	return []tonOpsUnderflowPrecheckCase{
		{
			name:  "getgasfee",
			code:  codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			stack: []any{int64(0)},
			exit: func(version int) int32 {
				if version < 6 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name:  "getstoragefee",
			code:  codeFromBuilders(t, funcsop.GETSTORAGEFEE().Serialize()),
			stack: []any{int64(0), int64(1)},
			exit: func(version int) int32 {
				if version < 6 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name:  "getforwardfee",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEE().Serialize()),
			stack: []any{int64(0)},
			exit: func(version int) int32 {
				if version < 6 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name:  "getoriginalfwdfee",
			code:  codeFromBuilders(t, funcsop.GETORIGINALFWDFEE().Serialize()),
			stack: []any{int64(0)},
			exit: func(version int) int32 {
				if version < 6 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name:  "getgasfeesimple",
			code:  codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()),
			stack: []any{int64(0)},
			exit: func(version int) int32 {
				if version < 6 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name:  "getforwardfeesimple",
			code:  codeFromBuilders(t, funcsop.GETFORWARDFEESIMPLE().Serialize()),
			stack: []any{int64(0)},
			exit: func(version int) int32 {
				if version < 6 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name: "getextrabalance",
			code: codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			exit: func(version int) int32 {
				if version < 10 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
		{
			name:  "hashext_dynamic",
			code:  codeFromBuilders(t, funcsop.HASHEXT(255).Serialize()),
			stack: []any{int64(0)},
			exit: func(version int) int32 {
				if version < 4 {
					return int32(vmerr.CodeInvalidOpcode)
				}
				return int32(vmerr.CodeStackUnderflow)
			},
		},
	}
}

func runTonOpsUnderflowPrecheckVersionCase(t *testing.T, tt tonOpsUnderflowPrecheckCase, version int) {
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

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	wantExit := tt.exit(version)
	if goRes.exitCode != wantExit || refRes.exitCode != wantExit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, wantExit)
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

func TestTVMCrossEmulatorDataSizeLowGasGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	fixture := tonOpsDataSizeLowGasFixture(t)

	for _, version := range tonOpsVersionCrossEmulatorVersions(t) {
		tests := tonOpsDataSizeLowGasCases()
		for _, tt := range tests {
			tt := tt
			version := version
			t.Run(fmt.Sprintf("%s_v%d", tt.name, version), func(t *testing.T) {
				runTonOpsDataSizeLowGasVersionCase(t, fixture, tt, version)
			})
		}
	}
}

func FuzzTVMCrossEmulatorDataSizeLowGasGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tonOpsDataSizeLowGasCaseCount))
	}
	for i := 0; i < tonOpsDataSizeLowGasCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		fixture := tonOpsDataSizeLowGasFixture(t)
		tests := tonOpsDataSizeLowGasCases()
		if len(tests) != tonOpsDataSizeLowGasCaseCount {
			t.Fatalf("tonops datasize low-gas case count = %d, want %d", len(tests), tonOpsDataSizeLowGasCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runTonOpsDataSizeLowGasVersionCase(t, fixture, tt, version)
	})
}

const tonOpsDataSizeLowGasCaseCount = 5

type tonOpsDataSizeLowGasFixtureData struct {
	root       *cell.Cell
	cdataSize  *cell.Cell
	cdataSizeQ *cell.Cell
	sdataSize  *cell.Cell
	sdataSizeQ *cell.Cell
}

type tonOpsDataSizeLowGasCase struct {
	name      string
	code      func(tonOpsDataSizeLowGasFixtureData) *cell.Cell
	stackArg  func(tonOpsDataSizeLowGasFixtureData) any
	bound     int64
	legacyGas int64
	modernGas int64
}

func tonOpsDataSizeLowGasFixture(t *testing.T) tonOpsDataSizeLowGasFixtureData {
	t.Helper()

	first := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	second := cell.BeginCell().MustStoreUInt(2, 2).EndCell()
	root := cell.BeginCell().MustStoreRef(first).MustStoreRef(second).EndCell()

	return tonOpsDataSizeLowGasFixtureData{
		root:       root,
		cdataSize:  prependRawMethodDrop(codeFromBuilders(t, funcsop.CDATASIZE().Serialize())),
		cdataSizeQ: prependRawMethodDrop(codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize())),
		sdataSize:  prependRawMethodDrop(codeFromBuilders(t, funcsop.SDATASIZE().Serialize())),
		sdataSizeQ: prependRawMethodDrop(codeFromBuilders(t, funcsop.SDATASIZEQ().Serialize())),
	}
}

func tonOpsDataSizeLowGasCases() []tonOpsDataSizeLowGasCase {
	cellArg := func(fixture tonOpsDataSizeLowGasFixtureData) any {
		return fixture.root
	}
	sliceArg := func(fixture tonOpsDataSizeLowGasFixtureData) any {
		return fixture.root.MustBeginParse()
	}
	return []tonOpsDataSizeLowGasCase{
		{
			name: "cdatasize_walks_refs_after_out_of_gas",
			code: func(fixture tonOpsDataSizeLowGasFixtureData) *cell.Cell {
				return fixture.cdataSize
			},
			stackArg:  cellArg,
			bound:     10,
			legacyGas: 344,
			modernGas: 144,
		},
		{
			name: "cdatasize_bound_overflow_adds_exception_gas",
			code: func(fixture tonOpsDataSizeLowGasFixtureData) *cell.Cell {
				return fixture.cdataSize
			},
			stackArg:  cellArg,
			bound:     1,
			legacyGas: 194,
			modernGas: 144,
		},
		{
			name: "cdatasizeq_walks_refs_after_out_of_gas",
			code: func(fixture tonOpsDataSizeLowGasFixtureData) *cell.Cell {
				return fixture.cdataSizeQ
			},
			stackArg:  cellArg,
			bound:     10,
			legacyGas: 344,
			modernGas: 144,
		},
		{
			name: "sdatasize_walks_refs_after_out_of_gas",
			code: func(fixture tonOpsDataSizeLowGasFixtureData) *cell.Cell {
				return fixture.sdataSize
			},
			stackArg:  sliceArg,
			bound:     10,
			legacyGas: 244,
			modernGas: 144,
		},
		{
			name: "sdatasizeq_walks_refs_after_out_of_gas",
			code: func(fixture tonOpsDataSizeLowGasFixtureData) *cell.Cell {
				return fixture.sdataSizeQ
			},
			stackArg:  sliceArg,
			bound:     10,
			legacyGas: 244,
			modernGas: 144,
		},
	}
}

func runTonOpsDataSizeLowGasVersionCase(t *testing.T, fixture tonOpsDataSizeLowGasFixtureData, tt tonOpsDataSizeLowGasCase, version int) {
	t.Helper()

	goStack, err := buildCrossStack(tt.stackArg(fixture), tt.bound)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stackArg(fixture), tt.bound)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	code := tt.code(fixture)
	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, testEmptyCell(), tuple.Tuple{}, nil, goStack, version, 120)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refCfg := *tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.GasLimit = 120
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != int32(^vmerr.CodeOutOfGas) || refRes.exitCode != int32(^vmerr.CodeOutOfGas) {
		t.Fatalf("unexpected exit code: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	wantGas := tt.legacyGas
	if version >= 4 {
		wantGas = tt.modernGas
	}
	if goRes.gasUsed != wantGas {
		t.Fatalf("gas used = %d, want %d", goRes.gasUsed, wantGas)
	}
}

func tonopsCrossConfigWithGlobalVersion(t *testing.T, version uint32) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	return mustConfigDictCell(t, map[uint32]*cell.Cell{
		uint32(tlb.ConfigParamGlobalVersion): versionCell,
	})
}

func tonopsCrossConflictingFeeConfig(t *testing.T, globalVersionCell *cell.Cell) *cell.Cell {
	t.Helper()

	storagePrices := cell.NewDict(32)
	if err := storagePrices.SetIntKey(big.NewInt(1), makeStoragePricesSlice(1, 99, 99, 99, 99).MustToCell()); err != nil {
		t.Fatalf("failed to seed conflicting storage prices: %v", err)
	}

	return mustConfigDictCell(t, map[uint32]*cell.Cell{
		uint32(tlb.ConfigParamGlobalVersion):               globalVersionCell,
		uint32(tlb.ConfigParamStoragePrices):               storagePrices.AsCell(),
		uint32(tlb.ConfigParamGasPricesMasterchain):        makeGasPricesSlice(1, 1, 9, 9, 9, 1, 9, 9, 9, true).MustToCell(),
		uint32(tlb.ConfigParamGasPricesBasechain):          makeGasPricesSlice(1, 1, 9, 9, 9, 1, 9, 9, 9, true).MustToCell(),
		uint32(tlb.ConfigParamMsgForwardPricesMasterchain): makeMsgPricesSlice(9, 9, 9, 9, 9, 9).MustToCell(),
		uint32(tlb.ConfigParamMsgForwardPricesBasechain):   makeMsgPricesSlice(9, 9, 9, 9, 9, 9).MustToCell(),
	})
}

func tonopsCrossSendMsgConfig(t *testing.T, version uint32, msgPrices tlb.ConfigMsgForwardPrices) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	msgPricesCell, err := tlb.ToCell(&msgPrices)
	if err != nil {
		t.Fatalf("failed to build msg prices config: %v", err)
	}
	sizeLimitsCell, err := tlb.ToCell(&tlb.SizeLimitsConfigV1{
		MaxMsgBits:      1 << 20,
		MaxMsgCells:     128,
		MaxLibraryCells: 1000,
		MaxVMDataDepth:  512,
		MaxExtMsgSize:   65535,
		MaxExtMsgDepth:  512,
	})
	if err != nil {
		t.Fatalf("failed to build size limits config: %v", err)
	}
	return mustConfigDictCell(t, map[uint32]*cell.Cell{
		uint32(tlb.ConfigParamGlobalVersion):               versionCell,
		uint32(tlb.ConfigParamMsgForwardPricesMasterchain): msgPricesCell,
		uint32(tlb.ConfigParamMsgForwardPricesBasechain):   msgPricesCell,
		uint32(tlb.ConfigParamSizeLimits):                  sizeLimitsCell,
	})
}

func tonopsCrossSendMsgUnpackedConfig(t *testing.T, msgPrices tlb.ConfigMsgForwardPrices) tuple.Tuple {
	t.Helper()

	unpacked := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &unpacked, 4, makeMsgPricesSlice(
		msgPrices.LumpPrice,
		msgPrices.BitPrice,
		msgPrices.CellPrice,
		msgPrices.IHRFactor,
		msgPrices.FirstFrac,
		msgPrices.NextFrac,
	))
	mustSetTupleValue(t, &unpacked, 5, makeMsgPricesSlice(
		msgPrices.LumpPrice,
		msgPrices.BitPrice,
		msgPrices.CellPrice,
		msgPrices.IHRFactor,
		msgPrices.FirstFrac,
		msgPrices.NextFrac,
	))
	mustSetTupleValue(t, &unpacked, 6, makeSizeLimitsSlice(1<<20, 128))
	return unpacked
}

func tonopsCrossRefConfig(configRoot *cell.Cell) *referenceGetMethodConfig {
	return &referenceGetMethodConfig{
		Address:    tonopsTestAddr,
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    uint64(tonopsTestBalance.Int64()),
		RandSeed:   tonopsTestSeed,
		ConfigRoot: configRoot,
	}
}
