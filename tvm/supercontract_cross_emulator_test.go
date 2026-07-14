//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type superContractStep struct {
	name    string
	builder *cell.Builder
}

type superContractVersionedPrefixCase struct {
	name    string
	steps   []superContractStep
	through string
	c7      func(*testing.T, int) tuple.Tuple
}

type superContractC7PrefixSuite struct {
	name  string
	steps func(*testing.T) []superContractStep
	c7    func(*testing.T, int) tuple.Tuple
}

const (
	superContractAddressPrefixCaseCount          = 36
	superContractC7PrefixSuiteCount              = 3
	superContractNegativeQuietVersionedCaseCount = 6
)

func TestTVMCrossEmulatorSupercontractByPrefixes(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	steps, c7 := buildSuperContractSteps(t)
	tonopsStart := 0
	cryptoStart := 0
	for i, step := range steps {
		if step.name == "getparam 2" {
			tonopsStart = i
		}
		if step.name == "push ecrecover hash" {
			cryptoStart = i
			break
		}
	}
	if tonopsStart == 0 || cryptoStart == 0 || cryptoStart <= tonopsStart {
		t.Fatal("failed to find supercontract split points")
	}

	runSuperContractPrefixes(t, steps[:tonopsStart], tuple.Tuple{})
	runSuperContractPrefixes(t, steps[tonopsStart:cryptoStart], c7)
	runSuperContractPrefixes(t, steps[cryptoStart:], tuple.Tuple{})
	runSuperContractPrefixes(t, buildFeeSuperContractSteps(t), feeTestC7(t))
	runSuperContractPrefixes(t, buildExtraBalanceSuperContractSteps(t), feeExtraBalanceTestC7(t))
	runSuperContractPrefixes(t, buildActionSuperContractSteps(t), feeTestC7(t))
}

func TestTVMCrossEmulatorSupercontractVersionedPrefixes(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := superContractVersionedPrefixCases(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
					runSuperContractVersionedPrefixCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractVersionedPrefixes(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%superContractVersionedPrefixCaseCount))
	}
	for i := 0; i < superContractVersionedPrefixCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := superContractVersionedPrefixCases(t)
		if len(tests) != superContractVersionedPrefixCaseCount {
			t.Fatalf("supercontract versioned prefix case count = %d, want %d", len(tests), superContractVersionedPrefixCaseCount)
		}
		tt := tests[int(rawCase)%len(tests)]
		runSuperContractVersionedPrefixCase(t, tt, version)
	})
}

const superContractVersionedPrefixCaseCount = 11

func TestTVMCrossEmulatorSupercontractCorePrefixesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	steps := superContractCorePrefixSteps(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_CORE_PREFIX_VERSION_AUDIT")
	for _, version := range versions {
		version := version
		t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
			for idx, step := range steps {
				idx := idx
				t.Run(strconv.Itoa(idx+1)+"_"+step.name, func(t *testing.T) {
					runSuperContractNoC7PrefixVersionCase(t, steps, idx, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractCorePrefixesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version))
	}
	for _, rawPrefix := range []uint8{0, 1, 2, 3, 7, 15, 31, 63, 127, 255} {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), rawPrefix)
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawPrefix uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		steps := superContractCorePrefixSteps(t)
		runSuperContractNoC7PrefixVersionCase(t, steps, int(rawPrefix)%len(steps), version)
	})
}

func TestTVMCrossEmulatorSupercontractTonopsPrefixesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	steps := superContractTonopsPrefixSteps(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_TONOPS_PREFIX_VERSION_AUDIT")
	for _, version := range versions {
		version := version
		t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
			c7 := superContractStepsC7WithGlobalVersion(t, version)
			for idx, step := range steps {
				idx := idx
				t.Run(strconv.Itoa(idx+1)+"_"+step.name, func(t *testing.T) {
					runSuperContractRawC7PrefixVersionCase(t, steps, idx, c7, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractTonopsPrefixesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version))
	}
	for _, rawPrefix := range []uint8{0, 1, 2, 3, 7, 15, 31, 63, 127, 255} {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), rawPrefix)
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawPrefix uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		steps := superContractTonopsPrefixSteps(t)
		c7 := superContractStepsC7WithGlobalVersion(t, version)
		runSuperContractRawC7PrefixVersionCase(t, steps, int(rawPrefix)%len(steps), c7, version)
	})
}

func TestTVMCrossEmulatorSupercontractC7PrefixesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	suites := superContractC7PrefixSuites(t)
	if len(suites) != superContractC7PrefixSuiteCount {
		t.Fatalf("supercontract c7 prefix suite count = %d, want %d", len(suites), superContractC7PrefixSuiteCount)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_C7_PREFIX_VERSION_AUDIT")
	for _, suite := range suites {
		suite := suite
		t.Run(suite.name, func(t *testing.T) {
			steps := suite.steps(t)
			for _, version := range versions {
				version := version
				t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
					c7 := suite.c7(t, version)
					for idx, step := range steps {
						idx := idx
						t.Run(strconv.Itoa(idx+1)+"_"+step.name, func(t *testing.T) {
							runSuperContractRawC7PrefixVersionCase(t, steps, idx, c7, version)
						})
					}
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractC7PrefixesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%superContractC7PrefixSuiteCount), uint8(version))
	}
	for suiteIdx := 0; suiteIdx < superContractC7PrefixSuiteCount; suiteIdx++ {
		for _, rawPrefix := range []uint8{0, 1, 2, 3, 7, 15, 31, 63, 127, 255} {
			f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(suiteIdx), rawPrefix)
		}
	}
	f.Add(uint8(255), uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawSuite, rawPrefix uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		suites := superContractC7PrefixSuites(t)
		if len(suites) != superContractC7PrefixSuiteCount {
			t.Fatalf("supercontract c7 prefix suite count = %d, want %d", len(suites), superContractC7PrefixSuiteCount)
		}

		suite := suites[int(rawSuite)%len(suites)]
		steps := suite.steps(t)
		c7 := suite.c7(t, version)
		runSuperContractRawC7PrefixVersionCase(t, steps, int(rawPrefix)%len(steps), c7, version)
	})
}

func TestTVMCrossEmulatorSupercontractCryptoPrefixesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	steps := superContractCryptoPrefixSteps(t)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_CRYPTO_PREFIX_VERSION_AUDIT")
	for _, version := range versions {
		version := version
		t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
			for idx, step := range steps {
				idx := idx
				t.Run(strconv.Itoa(idx+1)+"_"+step.name, func(t *testing.T) {
					runSuperContractNoC7PrefixVersionCase(t, steps, idx, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractCryptoPrefixesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version))
	}
	for _, rawPrefix := range []uint8{0, 1, 2, 3, 7, 15, 31, 63, 127, 255} {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), rawPrefix)
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawPrefix uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		steps := superContractCryptoPrefixSteps(t)
		runSuperContractNoC7PrefixVersionCase(t, steps, int(rawPrefix)%len(steps), version)
	})
}

func TestTVMCrossEmulatorSupercontractAddressByPrefixes(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	version13Config := mustConfigDictCell(t, map[uint32]*cell.Cell{
		8: cell.BeginCell().
			MustStoreUInt(0xC4, 8).
			MustStoreUInt(13, 32).
			MustStoreUInt(0, 64).
			EndCell(),
	})
	refCfg := referenceGetMethodConfig{
		Address:    tonopsTestAddr,
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    uint64(tonopsTestBalance.Int64()),
		RandSeed:   tonopsTestSeed,
		ConfigRoot: version13Config,
	}
	steps := buildAddressSuperContractSteps(t)
	builders := make([]*cell.Builder, 0, len(steps))
	for _, step := range steps {
		builders = append(builders, step.builder)
	}

	for i := range steps {
		prefixCode := prependRawMethodDropBuilders(t, builders[:i+1]...)

		goStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("failed to build go stack at step %d: %v", i+1, err)
		}
		refStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("failed to build reference stack at step %d: %v", i+1, err)
		}

		goRes, err := runGoCrossCode(prefixCode, testEmptyCell(), feeTestC7(t), goStack)
		if err != nil {
			t.Fatalf("go tvm execution failed at step %d (%s): %v", i+1, steps[i].name, err)
		}
		refRes, err := runReferenceCrossCodeViaEmulator(prefixCode, testEmptyCell(), refStack, refCfg)
		if err != nil {
			t.Fatalf("reference tvm execution failed at step %d (%s): %v", i+1, steps[i].name, err)
		}

		if goRes.exitCode != refRes.exitCode {
			t.Fatalf("exit code mismatch at step %d (%s): go=%d reference=%d", i+1, steps[i].name, goRes.exitCode, refRes.exitCode)
		}
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("gas mismatch at step %d (%s): go=%d reference=%d", i+1, steps[i].name, goRes.gasUsed, refRes.gasUsed)
		}

		goStackCell, err := normalizeStackCell(goRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize go stack at step %d (%s): %v", i+1, steps[i].name, err)
		}
		refStackCell, err := normalizeStackCell(refRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize reference stack at step %d (%s): %v", i+1, steps[i].name, err)
		}
		if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
			t.Fatalf("stack mismatch at step %d (%s):\ngo=%s\nreference=%s", i+1, steps[i].name, goStackCell.Dump(), refStackCell.Dump())
		}
	}
}

func TestTVMCrossEmulatorSupercontractAddressByPrefixesAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	steps := buildAddressSuperContractSteps(t)
	if len(steps) != superContractAddressPrefixCaseCount {
		t.Fatalf("supercontract address prefix case count = %d, want %d", len(steps), superContractAddressPrefixCaseCount)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_ADDRESS_VERSION_AUDIT")
	for _, version := range versions {
		version := version
		t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
			for idx, step := range steps {
				idx := idx
				t.Run(strconv.Itoa(idx+1)+"_"+step.name, func(t *testing.T) {
					runSuperContractAddressPrefixVersionCase(t, steps, idx, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractAddressByPrefixesGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%superContractAddressPrefixCaseCount))
	}
	for idx := 0; idx < superContractAddressPrefixCaseCount; idx++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(idx))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawPrefix uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		steps := buildAddressSuperContractSteps(t)
		if len(steps) != superContractAddressPrefixCaseCount {
			t.Fatalf("supercontract address prefix case count = %d, want %d", len(steps), superContractAddressPrefixCaseCount)
		}
		runSuperContractAddressPrefixVersionCase(t, steps, int(rawPrefix)%len(steps), version)
	})
}

func TestTVMCrossEmulatorSupercontractNegativeQuiet(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	msg := []byte("ton-circl-bls-cross")
	sig1 := testBLSSigBytes(3, msg)
	invalidRist := testInvalidRistrettoInt(t)
	invalidG1 := testInvalidBLSG1Bytes(t)
	invalidG2 := testInvalidBLSG2Bytes(t)
	feeC7 := feeTestC7(t)
	stdAddrSlice := cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice()
	addrNoneTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
	shortOptStdSlice := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
	invalidAnycast := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(31, 5).
		MustStoreSlice(bytes.Repeat([]byte{0xFF}, 4), 31).
		MustStoreInt(int64(tonopsTestAddr.Workchain()), 8).
		MustStoreSlice(tonopsTestAddr.Data(), 256).
		ToSlice()
	dataSizeLeaf := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	dataSizeRoot := cell.BeginCell().MustStoreRef(dataSizeLeaf).MustStoreRef(dataSizeLeaf).EndCell()

	ecrecoverHash := bytes.Repeat([]byte{0x42}, 32)
	_, ecrecoverR, ecrecoverS, _, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), ecrecoverHash)
	if !ok {
		t.Fatal("failed to build secp256k1 recovery fixture")
	}
	_, _, _, xonlyBasePub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x67}, 32), bytes.Repeat([]byte{0x22}, 32))
	if !ok {
		t.Fatal("failed to build secp256k1 xonly fixture")
	}
	secpXOnlyKey := new(big.Int).SetBytes(xonlyBasePub[1:33])
	secpTooLargeTweak := new(big.Int).SetBytes(localec.CurveOrderBytes())

	p256Curve := elliptic.P256()
	p256D := big.NewInt(123456789)
	p256X, p256Y := p256Curve.ScalarBaseMult(p256D.Bytes())
	p256Priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: p256Curve,
			X:     p256X,
			Y:     p256Y,
		},
		D: p256D,
	}
	p256SliceData := []byte("p256-signed-slice")
	p256SliceDigest := sha256.Sum256(p256SliceData)
	p256SliceR, p256SliceS, err := ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x24}, 1024)), p256Priv, p256SliceDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 slice fixture: %v", err)
	}
	p256SigS := make([]byte, 64)
	copy(p256SigS[32-len(p256SliceR.Bytes()):32], p256SliceR.Bytes())
	copy(p256SigS[64-len(p256SliceS.Bytes()):64], p256SliceS.Bytes())
	badP256Key := append([]byte{0x05}, bytes.Repeat([]byte{0x01}, 32)...)
	invalidMsgCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	extraReserveCell := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	type crossCase struct {
		name          string
		code          *cell.Cell
		stack         []any
		c7            tuple.Tuple
		globalVersion int
	}

	tests := []crossCase{
		{
			name:          "rist255_validate_invalid",
			code:          codeFromBuilders(t, funcsop.RIST255_VALIDATE().Serialize()),
			stack:         []any{invalidRist},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "rist255_qadd_invalid",
			code:          codeFromBuilders(t, funcsop.RIST255_QADD().Serialize()),
			stack:         []any{testRistrettoMulBaseInt(t, 1), invalidRist},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "rist255_qvalidate_invalid",
			code:          codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()),
			stack:         []any{invalidRist},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "rist255_qsub_invalid",
			code:          codeFromBuilders(t, funcsop.RIST255_QSUB().Serialize()),
			stack:         []any{testRistrettoMulBaseInt(t, 1), invalidRist},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "rist255_qmul_invalid",
			code:          codeFromBuilders(t, funcsop.RIST255_QMUL().Serialize()),
			stack:         []any{invalidRist, int64(3)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_verify_invalid_pub_false",
			code:          codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), testSliceFromBytes(msg), testSliceFromBytes(sig1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g1_add_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G1_ADD().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), testSliceFromBytes(testBLSG1BytesForScalar(2))},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_aggregate_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_AGGREGATE().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG2), int64(1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_fastaggregateverify_invalid_pub_false",
			code:          codeFromBuilders(t, funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), testSliceFromBytes(testBLSPubBytes(5)), int64(2), testSliceFromBytes(msg), testSliceFromBytes(testBLSAggregateSigBytes(t, testBLSSigBytes(3, msg), testBLSSigBytes(5, msg)))},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_aggregateverify_invalid_pub_false",
			code:          codeFromBuilders(t, funcsop.BLS_AGGREGATEVERIFY().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), testSliceFromBytes(msg), int64(1), testSliceFromBytes(sig1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g1_sub_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G1_SUB().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), testSliceFromBytes(testBLSG1BytesForScalar(2))},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g1_neg_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G1_NEG().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g1_mul_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G1_MUL().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), int64(3)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g1_ingroup_invalid_false",
			code:          codeFromBuilders(t, funcsop.BLS_G1_INGROUP().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_map_to_g1_underflow",
			code:          codeFromBuilders(t, funcsop.BLS_MAP_TO_G1().Serialize()),
			stack:         []any{testSliceFromBytes(testBLSFPBytes(7)[:47])},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g2_sub_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G2_SUB().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG2), testSliceFromBytes(testBLSG2BytesForScalar(2))},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g2_neg_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G2_NEG().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG2)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g2_mul_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_G2_MUL().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG2), int64(3)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_g2_ingroup_invalid_false",
			code:          codeFromBuilders(t, funcsop.BLS_G2_INGROUP().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG2)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_map_to_g2_underflow",
			code:          codeFromBuilders(t, funcsop.BLS_MAP_TO_G2().Serialize()),
			stack:         []any{testSliceFromBytes(testBLSFP2Bytes(11)[:95])},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "bls_pairing_invalid",
			code:          codeFromBuilders(t, funcsop.BLS_PAIRING().Serialize()),
			stack:         []any{testSliceFromBytes(invalidG1), testSliceFromBytes(testBLSG2BytesForScalar(1)), int64(1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "ecrecover_invalid_v",
			code:          codeFromBuilders(t, funcsop.ECRECOVER().Serialize()),
			stack:         []any{new(big.Int).SetBytes(ecrecoverHash), int64(4), ecrecoverR, ecrecoverS},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "secp256k1_xonly_pubkey_tweak_add_invalid_key",
			code:          codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack:         []any{new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32)), int64(1)},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "secp256k1_xonly_pubkey_tweak_add_tweak_ge_n",
			code:          codeFromBuilders(t, funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
			stack:         []any{secpXOnlyKey, secpTooLargeTweak},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "p256_chksigns_unaligned_slice",
			code:          codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
			stack:         []any{cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(), cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(), cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice()},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "p256_chksignu_bad_key",
			code:          codeFromBuilders(t, funcsop.P256_CHKSIGNU().Serialize()),
			stack:         []any{new(big.Int).SetBytes(bytes.Repeat([]byte{0x55}, 32)), cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(), cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice()},
			c7:            tuple.Tuple{},
			globalVersion: 13,
		},
		{
			name:          "ldmsgaddrq_fail",
			code:          codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack:         []any{cell.BeginCell().MustStoreUInt(0b11, 2).ToSlice()},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "ldstdaddrq_fail_nonstd",
			code:          codeFromBuilders(t, funcsop.LDSTDADDRQ().Serialize()),
			stack:         []any{addrNoneTail},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "ldoptstdaddrq_short_fail",
			code:          codeFromBuilders(t, funcsop.LDOPTSTDADDRQ().Serialize()),
			stack:         []any{shortOptStdSlice},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "rewritestdaddrq_fail_nonstd",
			code:          codeFromBuilders(t, funcsop.REWRITESTDADDRQ().Serialize()),
			stack:         []any{addrNoneTail},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "ststdaddrq_invalid_addr",
			code:          codeFromBuilders(t, funcsop.STSTDADDRQ().Serialize()),
			stack:         []any{addrNoneTail, cell.BeginCell()},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "parsemsgaddrq_invalid_anycast",
			code:          codeFromBuilders(t, funcsop.PARSEMSGADDRQ().Serialize()),
			stack:         []any{invalidAnycast},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "cdatasizeq_bound_fail",
			code:          codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize()),
			stack:         []any{dataSizeRoot, int64(1)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "configparam_miss",
			code:          codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			stack:         []any{int64(8)},
			c7:            buildSuperContractStepsC7ForNegative(t),
			globalVersion: 13,
		},
		{
			name:          "configoptparam_miss",
			code:          codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize()),
			stack:         []any{int64(8)},
			c7:            buildSuperContractStepsC7ForNegative(t),
			globalVersion: 13,
		},
		{
			name:          "getextrabalance_miss",
			code:          codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack:         []any{int64(9)},
			c7:            feeExtraBalanceTestC7(t),
			globalVersion: 13,
		},
		{
			name:          "getextrabalance_negative_id",
			code:          codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			stack:         []any{int64(-1)},
			c7:            feeExtraBalanceTestC7(t),
			globalVersion: 13,
		},
		{
			name:          "getgasfee_negative",
			code:          codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			stack:         []any{int64(-1), int64(0)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "getgasfeesimple_negative",
			code:          codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()),
			stack:         []any{int64(-1), int64(0)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "inmsg_value_alias",
			code:          codeFromBuilders(t, funcsop.INMSG_VALUE().Serialize()),
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "rawreserve_negative_amount",
			code:          codeFromBuilders(t, funcsop.RAWRESERVE().Serialize()),
			stack:         []any{int64(-1), int64(0)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "rawreservex_negative_amount",
			code:          codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize()),
			stack:         []any{int64(-1), extraReserveCell, int64(0)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "setlibcode_invalid_mode",
			code:          codeFromBuilders(t, funcsop.SETLIBCODE().Serialize()),
			stack:         []any{testEmptyCell(), int64(4)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "changelib_negative_hash",
			code:          codeFromBuilders(t, funcsop.CHANGELIB().Serialize()),
			stack:         []any{big.NewInt(-1), int64(1)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "sendmsg_invalid_mode",
			code:          codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack:         []any{testEmptyCell(), int64(256)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "sendmsg_invalid_message",
			code:          codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			stack:         []any{invalidMsgCell, int64(1)},
			c7:            feeC7,
			globalVersion: 13,
		},
		{
			name:          "ldmsgaddrq_success_reference_point",
			code:          codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize()),
			stack:         []any{stdAddrSlice},
			c7:            feeC7,
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

			goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tt.c7, goStack, tt.globalVersion)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCode(code, testEmptyCell(), tt.c7, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
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

func runSuperContractAddressPrefixVersionCase(t *testing.T, steps []superContractStep, prefixIdx int, version int) {
	t.Helper()

	builders := make([]*cell.Builder, 0, prefixIdx+1)
	for _, step := range steps[:prefixIdx+1] {
		builders = append(builders, step.builder)
	}
	code := prependRawMethodDropBuilders(t, builders...)
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack at step %d: %v", prefixIdx+1, err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack at step %d: %v", prefixIdx+1, err)
	}

	c7 := superContractFeeC7WithGlobalVersion(t, version)
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch at step %d (%s): go=%d reference=%d", prefixIdx+1, steps[prefixIdx].name, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch at step %d (%s): go=%d reference=%d", prefixIdx+1, steps[prefixIdx].name, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch at step %d (%s):\ngo=%s\nreference=%s", prefixIdx+1, steps[prefixIdx].name, goStackCell.Dump(), refStackCell.Dump())
	}
}

func TestTVMCrossEmulatorSupercontractNegativeQuietGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_SUPERCONTRACT_NEGATIVE_VERSION_AUDIT")
	for caseIdx := 0; caseIdx < superContractNegativeQuietVersionedCaseCount; caseIdx++ {
		caseIdx := caseIdx
		name, _, _, _, _ := superContractNegativeQuietVersionedCase(t, uint8(caseIdx), vm.MaxSupportedGlobalVersion)
		t.Run(name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run("v"+strconv.Itoa(version), func(t *testing.T) {
					runSuperContractNegativeQuietVersionedCase(t, uint8(caseIdx), version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorSupercontractNegativeQuietGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%superContractNegativeQuietVersionedCaseCount))
	}
	for caseIdx := 0; caseIdx < superContractNegativeQuietVersionedCaseCount; caseIdx++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(caseIdx))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		runSuperContractNegativeQuietVersionedCase(t, rawCase, version)
	})
}

func superContractCryptoPrefixSteps(t *testing.T) []superContractStep {
	t.Helper()

	steps, _ := buildSuperContractSteps(t)
	cryptoStart := superContractStepIndex(t, steps, "push ecrecover hash")
	return steps[cryptoStart:]
}

func superContractCorePrefixSteps(t *testing.T) []superContractStep {
	t.Helper()

	steps, _ := buildSuperContractSteps(t)
	tonopsStart := superContractStepIndex(t, steps, "getparam 2")
	return steps[:tonopsStart]
}

func superContractTonopsPrefixSteps(t *testing.T) []superContractStep {
	t.Helper()

	steps, _ := buildSuperContractSteps(t)
	tonopsStart := superContractStepIndex(t, steps, "getparam 2")
	cryptoStart := superContractStepIndex(t, steps, "push ecrecover hash")
	return steps[tonopsStart:cryptoStart]
}

func runSuperContractNoC7PrefixVersionCase(t *testing.T, steps []superContractStep, prefixIdx int, version int) {
	t.Helper()

	builders := make([]*cell.Builder, 0, prefixIdx+1)
	for _, step := range steps[:prefixIdx+1] {
		builders = append(builders, step.builder)
	}
	code := prependRawMethodDropBuilders(t, builders...)
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack at step %d: %v", prefixIdx+1, err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack at step %d: %v", prefixIdx+1, err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), tuple.Tuple{}, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch at step %d (%s): go=%d reference=%d", prefixIdx+1, steps[prefixIdx].name, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch at step %d (%s): go=%d reference=%d", prefixIdx+1, steps[prefixIdx].name, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch at step %d (%s):\ngo=%s\nreference=%s", prefixIdx+1, steps[prefixIdx].name, goStackCell.Dump(), refStackCell.Dump())
	}
}

func runSuperContractRawC7PrefixVersionCase(t *testing.T, steps []superContractStep, prefixIdx int, c7 tuple.Tuple, version int) {
	t.Helper()

	builders := make([]*cell.Builder, 0, prefixIdx+1)
	for _, step := range steps[:prefixIdx+1] {
		builders = append(builders, step.builder)
	}
	code := prependRawMethodDropBuilders(t, builders...)
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack at step %d: %v", prefixIdx+1, err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack at step %d: %v", prefixIdx+1, err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	refRes, err := runReferenceCrossCode(code, testEmptyCell(), c7, refStack)
	if err != nil {
		t.Fatalf("reference tvm execution failed at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch at step %d (%s): go=%d reference=%d", prefixIdx+1, steps[prefixIdx].name, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch at step %d (%s): go=%d reference=%d", prefixIdx+1, steps[prefixIdx].name, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack at step %d (%s): %v", prefixIdx+1, steps[prefixIdx].name, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch at step %d (%s):\ngo=%s\nreference=%s", prefixIdx+1, steps[prefixIdx].name, goStackCell.Dump(), refStackCell.Dump())
	}
}

func runSuperContractNegativeQuietVersionedCase(t *testing.T, rawCase uint8, version int) {
	t.Helper()

	_, body, stackValues, c7, refCfg := superContractNegativeQuietVersionedCase(t, rawCase, version)
	code := prependRawMethodDrop(body)
	goStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(stackValues...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refRes, err := runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
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

func superContractNegativeQuietVersionedCase(t *testing.T, rawCase uint8, version int) (string, *cell.Cell, []any, tuple.Tuple, *referenceGetMethodConfig) {
	t.Helper()

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	switch rawCase % superContractNegativeQuietVersionedCaseCount {
	case 0:
		return "rist255_qvalidate_invalid",
			codeFromBuilders(t, funcsop.RIST255_QVALIDATE().Serialize()),
			[]any{testInvalidRistrettoInt(t)},
			tuple.Tuple{},
			refCfg
	case 1:
		msg := []byte("ton-circl-bls-cross")
		return "bls_verify_invalid_pub_false",
			codeFromBuilders(t, funcsop.BLS_VERIFY().Serialize()),
			[]any{testSliceFromBytes(testInvalidBLSG1Bytes(t)), testSliceFromBytes(msg), testSliceFromBytes(testBLSSigBytes(3, msg))},
			tuple.Tuple{},
			refCfg
	case 2:
		p256Curve := elliptic.P256()
		p256D := big.NewInt(123456789)
		p256X, p256Y := p256Curve.ScalarBaseMult(p256D.Bytes())
		p256Priv := &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{
				Curve: p256Curve,
				X:     p256X,
				Y:     p256Y,
			},
			D: p256D,
		}
		p256SliceData := []byte("p256-signed-slice")
		p256SliceDigest := sha256.Sum256(p256SliceData)
		p256SliceR, p256SliceS, err := ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x24}, 1024)), p256Priv, p256SliceDigest[:])
		if err != nil {
			t.Fatalf("failed to sign p256 slice fixture: %v", err)
		}
		p256SigS := make([]byte, 64)
		copy(p256SigS[32-len(p256SliceR.Bytes()):32], p256SliceR.Bytes())
		copy(p256SigS[64-len(p256SliceS.Bytes()):64], p256SliceS.Bytes())
		badP256Key := append([]byte{0x05}, bytes.Repeat([]byte{0x01}, 32)...)

		return "p256_chksigns_unaligned_slice",
			codeFromBuilders(t, funcsop.P256_CHKSIGNS().Serialize()),
			[]any{cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice(), cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice(), cell.BeginCell().MustStoreSlice(badP256Key, 264).ToSlice()},
			tuple.Tuple{},
			refCfg
	case 3:
		return "getgasfee_negative",
			codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			[]any{int64(-1), int64(0)},
			superContractFeeC7WithGlobalVersion(t, version),
			refCfg
	case 4:
		return "getextrabalance_negative_id",
			codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			[]any{int64(-1)},
			superContractExtraBalanceC7WithGlobalVersion(t, version),
			refCfg
	default:
		sendMsgPrices := tlb.ConfigMsgForwardPrices{LumpPrice: 1}
		sendMsgConfig := tonopsCrossSendMsgConfig(t, uint32(version), sendMsgPrices)
		return "sendmsg_invalid_message",
			codeFromBuilders(t, funcsop.SENDMSG().Serialize()),
			[]any{cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(), int64(1)},
			makeTonopsTestC7(t, tonopsTestC7Config{
				ConfigRoot:     sendMsgConfig,
				UnpackedConfig: tonopsCrossSendMsgUnpackedConfig(t, sendMsgPrices),
			}),
			tonopsCrossRefConfig(sendMsgConfig)
	}
}

func runSuperContractPrefixes(t *testing.T, steps []superContractStep, c7 tuple.Tuple) {
	t.Helper()

	builders := make([]*cell.Builder, 0, len(steps))
	for _, step := range steps {
		builders = append(builders, step.builder)
	}

	for i := range steps {
		prefixCode := prependRawMethodDropBuilders(t, builders[:i+1]...)

		goStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("failed to build go stack at step %d: %v", i+1, err)
		}
		refStack, err := buildCrossStack()
		if err != nil {
			t.Fatalf("failed to build reference stack at step %d: %v", i+1, err)
		}

		goRes, err := runGoCrossCodeWithVersion(prefixCode, testEmptyCell(), c7, goStack, 13)
		if err != nil {
			t.Fatalf("go tvm execution failed at step %d (%s): %v", i+1, steps[i].name, err)
		}
		refRes, err := runReferenceCrossCode(prefixCode, testEmptyCell(), c7, refStack)
		if err != nil {
			t.Fatalf("reference tvm execution failed at step %d (%s): %v", i+1, steps[i].name, err)
		}

		if goRes.exitCode != refRes.exitCode {
			t.Fatalf("exit code mismatch at step %d (%s): go=%d reference=%d", i+1, steps[i].name, goRes.exitCode, refRes.exitCode)
		}
		if goRes.gasUsed != refRes.gasUsed {
			t.Fatalf("gas mismatch at step %d (%s): go=%d reference=%d", i+1, steps[i].name, goRes.gasUsed, refRes.gasUsed)
		}

		goStackCell, err := normalizeStackCell(goRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize go stack at step %d (%s): %v", i+1, steps[i].name, err)
		}
		refStackCell, err := normalizeStackCell(refRes.stack)
		if err != nil {
			t.Fatalf("failed to normalize reference stack at step %d (%s): %v", i+1, steps[i].name, err)
		}
		if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
			t.Fatalf("stack mismatch at step %d (%s):\ngo=%s\nreference=%s", i+1, steps[i].name, goStackCell.Dump(), refStackCell.Dump())
		}
	}
}

func superContractC7PrefixSuites(t *testing.T) []superContractC7PrefixSuite {
	t.Helper()

	return []superContractC7PrefixSuite{
		{
			name:  "fee",
			steps: buildFeeSuperContractSteps,
			c7: func(t *testing.T, version int) tuple.Tuple {
				return superContractFeeC7WithGlobalVersion(t, version)
			},
		},
		{
			name:  "extra_balance",
			steps: buildExtraBalanceSuperContractSteps,
			c7: func(t *testing.T, version int) tuple.Tuple {
				return superContractExtraBalanceC7WithGlobalVersion(t, version)
			},
		},
		{
			name:  "actions",
			steps: buildActionSuperContractSteps,
			c7: func(t *testing.T, version int) tuple.Tuple {
				return superContractFeeC7WithGlobalVersion(t, version)
			},
		},
	}
}

func superContractVersionedPrefixCases(t *testing.T) []superContractVersionedPrefixCase {
	t.Helper()

	steps, _ := buildSuperContractSteps(t)
	cryptoStart := superContractStepIndex(t, steps, "push ecrecover hash")
	cryptoSteps := steps[cryptoStart:]
	addressSteps := buildAddressSuperContractSteps(t)
	feeSteps := buildFeeSuperContractSteps(t)
	extraBalanceSteps := buildExtraBalanceSuperContractSteps(t)
	actionSteps := buildActionSuperContractSteps(t)

	return []superContractVersionedPrefixCase{
		{name: "core_btos", steps: steps, through: "btos"},
		{name: "core_bls_pairing", steps: steps, through: "bls_pairing"},
		{name: "crypto_p256", steps: cryptoSteps, through: "p256_chksigns"},
		{name: "crypto_globals", steps: cryptoSteps, through: "getglobvar 5"},
		{name: "address_ldstdaddr", steps: addressSteps, through: "ldstdaddr"},
		{name: "address_stoptstdaddrq", steps: addressSteps, through: "stoptstdaddrq"},
		{name: "fee_getgasfee", steps: feeSteps, through: "getgasfee", c7: func(t *testing.T, version int) tuple.Tuple {
			return superContractFeeC7WithGlobalVersion(t, version)
		}},
		{name: "fee_hashext", steps: feeSteps, through: "hashext", c7: func(t *testing.T, version int) tuple.Tuple {
			return superContractFeeC7WithGlobalVersion(t, version)
		}},
		{name: "fee_rewritevaraddrq", steps: feeSteps, through: "rewritevaraddrq", c7: func(t *testing.T, version int) tuple.Tuple {
			return superContractFeeC7WithGlobalVersion(t, version)
		}},
		{name: "extra_balance", steps: extraBalanceSteps, through: "getextrabalance miss", c7: func(t *testing.T, version int) tuple.Tuple {
			return superContractExtraBalanceC7WithGlobalVersion(t, version)
		}},
		{name: "actions_sendmsg", steps: actionSteps, through: "sendmsg send", c7: func(t *testing.T, version int) tuple.Tuple {
			return superContractFeeC7WithGlobalVersion(t, version)
		}},
	}
}

func runSuperContractVersionedPrefixCase(t *testing.T, tt superContractVersionedPrefixCase, version int) {
	t.Helper()

	builders := superContractBuildersThrough(t, tt.steps, tt.through)
	var c7 tuple.Tuple
	if tt.c7 != nil {
		c7 = tt.c7(t, version)
	}
	runSuperContractVersionedPrefix(t, builders, c7, version)
}

func runSuperContractVersionedPrefix(t *testing.T, builders []*cell.Builder, c7 tuple.Tuple, version int) {
	t.Helper()

	code := prependRawMethodDropBuilders(t, builders...)
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, testEmptyCell(), c7, goStack, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	var refRes *crossRunResult
	if c7.Len() > 0 {
		refRes, err = runReferenceCrossCode(code, testEmptyCell(), c7, refStack)
	} else {
		refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
		refRes, err = runReferenceCrossCodeViaEmulator(code, testEmptyCell(), refStack, *refCfg)
	}
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
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

func superContractBuildersThrough(t *testing.T, steps []superContractStep, name string) []*cell.Builder {
	t.Helper()

	idx := superContractStepIndex(t, steps, name)
	builders := make([]*cell.Builder, 0, idx+1)
	for _, step := range steps[:idx+1] {
		builders = append(builders, step.builder)
	}
	return builders
}

func superContractStepIndex(t *testing.T, steps []superContractStep, name string) int {
	t.Helper()

	for i, step := range steps {
		if step.name == name {
			return i
		}
	}
	t.Fatalf("failed to find supercontract step %q", name)
	return -1
}

func superContractStepsConfigRoot(t *testing.T, version int) *cell.Cell {
	t.Helper()

	configValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	values := map[uint32]*cell.Cell{
		7: configValue,
	}
	if version >= 0 {
		versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: uint32(version)})
		if err != nil {
			t.Fatalf("failed to build global version config: %v", err)
		}
		values[uint32(tlb.ConfigParamGlobalVersion)] = versionCell
	}
	return mustConfigDictCell(t, values)
}

func superContractStepsC7WithGlobalVersion(t *testing.T, version int) tuple.Tuple {
	t.Helper()

	return superContractStepsC7(t, superContractStepsConfigRoot(t, version))
}

func superContractStepsC7(t *testing.T, configRoot *cell.Cell) tuple.Tuple {
	t.Helper()

	unpacked := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &unpacked, 0, makeStoragePricesSlice(100, 3, 5, 7, 11))
	mustSetTupleValue(t, &unpacked, 1, cell.BeginCell().MustStoreUInt(uint64(uint32(tonopsTestGlobalID)), 32).ToSlice())
	mustSetTupleValue(t, &unpacked, 2, makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true))
	mustSetTupleValue(t, &unpacked, 3, makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true))
	mustSetTupleValue(t, &unpacked, 4, makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000))
	mustSetTupleValue(t, &unpacked, 5, makeMsgPricesSlice(900, 120, 220, 400, 800, 1200))
	mustSetTupleValue(t, &unpacked, 6, makeSizeLimitsSlice(1<<20, 128))

	return makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		MyCode:         cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell(),
		IncomingValue:  tuple.NewTupleValue(big.NewInt(555), cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()),
		Balance:        tuple.NewTupleValue(big.NewInt(123456789), cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()),
		StorageFees:    tonopsTestStorageFees,
		UnpackedConfig: unpacked,
		Globals: map[int]any{
			1: int64(111),
			2: int64(222),
		},
		ExtraParams: map[int]any{
			2:  int64(42),
			13: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
}

func buildSuperContractSteps(t *testing.T) ([]superContractStep, tuple.Tuple) {
	t.Helper()

	c7 := superContractStepsC7(t, superContractStepsConfigRoot(t, -1))

	msg := []byte("supertrace")
	pub1 := testBLSPubBytes(3)
	pub2 := testBLSPubBytes(5)
	sig1 := testBLSSigBytes(3, msg)
	sig2 := testBLSSigBytes(5, msg)
	aggSig := testBLSAggregateSigBytes(t, sig1, sig2)

	g1Zero := testBLSG1ZeroBytes()
	g2Zero := testBLSG2ZeroBytes()
	g1a := testBLSG1BytesForScalar(2)
	g1b := testBLSG1BytesForScalar(7)
	g2a := testBLSG2BytesForScalar(2)
	g2b := testBLSG2BytesForScalar(7)
	fp := testBLSFPBytes(7)
	fp2 := testBLSFP2Bytes(11)

	ecrecoverHash := bytes.Repeat([]byte{0x42}, 32)
	ecrecoverV, ecrecoverR, ecrecoverS, _, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), ecrecoverHash)
	if !ok {
		t.Fatal("failed to build secp256k1 recovery fixture")
	}
	_, _, _, xonlyBasePub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x67}, 32), bytes.Repeat([]byte{0x22}, 32))
	if !ok {
		t.Fatal("failed to build secp256k1 xonly fixture")
	}
	secpXOnlyKey := new(big.Int).SetBytes(xonlyBasePub[1:33])

	p256Curve := elliptic.P256()
	p256D := big.NewInt(123456789)
	p256X, p256Y := p256Curve.ScalarBaseMult(p256D.Bytes())
	p256Priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: p256Curve,
			X:     p256X,
			Y:     p256Y,
		},
		D: p256D,
	}
	p256HashBytes := bytes.Repeat([]byte{0x55}, 32)
	p256HashDigest := sha256.Sum256(p256HashBytes)
	p256R, p256S, err := ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x42}, 1024)), p256Priv, p256HashDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 hash fixture: %v", err)
	}
	p256SigU := make([]byte, 64)
	copy(p256SigU[32-len(p256R.Bytes()):32], p256R.Bytes())
	copy(p256SigU[64-len(p256S.Bytes()):64], p256S.Bytes())
	p256Pub := elliptic.MarshalCompressed(p256Curve, p256Priv.PublicKey.X, p256Priv.PublicKey.Y)

	p256SliceData := []byte("p256-signed-slice")
	p256SliceDigest := sha256.Sum256(p256SliceData)
	p256SliceR, p256SliceS, err := ecdsa.Sign(bytes.NewReader(bytes.Repeat([]byte{0x24}, 1024)), p256Priv, p256SliceDigest[:])
	if err != nil {
		t.Fatalf("failed to sign p256 slice fixture: %v", err)
	}
	p256SigS := make([]byte, 64)
	copy(p256SigS[32-len(p256SliceR.Bytes()):32], p256SliceR.Bytes())
	copy(p256SigS[64-len(p256SliceS.Bytes()):64], p256SliceS.Bytes())

	steps := []superContractStep{
		pushIntStep("pushint 5", 5),
		opStep("inc", mathop.INC().Serialize()),
		opStep("dup", stackop.DUP().Serialize()),
		pushIntStep("pushint 2", 2),
		opStep("sub", mathop.SUB().Serialize()),
		opStep("swap", stackop.SWAP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("pushnan", mathop.PUSHNAN().Serialize()),
		opStep("isnan", mathop.ISNAN().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushIntStep("pushint 11", 11),
		pushIntStep("pushint 22", 22),
		opStep("tuple2", tupleop.TUPLE(2).Serialize()),
		opStep("dup tuple", stackop.DUP().Serialize()),
		opStep("tlen", tupleop.TLEN().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("pushnull", tupleop.PUSHNULL().Serialize()),
		opStep("isnull", tupleop.ISNULL().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		opStep("newc", cellsliceop.NEWC().Serialize()),
		pushIntStep("pushint 5", 5),
		opStep("stzeroes", cellsliceop.STZEROES().Serialize()),
		opStep("btos", cellsliceop.BTOS().Serialize()),
		opStep("dup slice", stackop.DUP().Serialize()),
		opStep("sbits", cellsliceop.SBITS().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("ldzeroes", cellsliceop.LDZEROES().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("newc", cellsliceop.NEWC().Serialize()),
		opStep("endc", cellsliceop.ENDC().Serialize()),
		opStep("dup cell", stackop.DUP().Serialize()),
		opStep("cdepth", cellsliceop.CDEPTH().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("clevel", cellsliceop.CLEVEL().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("pushslice 32b", testSliceFromBytes([]byte{0x12, 0x34, 0x56, 0x78})),
		opStep("plduz32", cellsliceop.PLDUZ(32).Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		opStep("rist255_pushl", funcsop.RIST255_PUSHL().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 1", 1),
		pushIntStep("pushint 2", 2),
		opStep("rist255_fromhash", funcsop.RIST255_FROMHASH().Serialize()),
		opStep("dup point", stackop.DUP().Serialize()),
		opStep("rist255_validate", funcsop.RIST255_VALIDATE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 3", 3),
		opStep("rist255_mulbase", funcsop.RIST255_MULBASE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push rist a", testRistrettoMulBaseInt(t, 5)),
		pushBigIntStep("push rist b", testRistrettoMulBaseInt(t, 7)),
		opStep("rist255_add", funcsop.RIST255_ADD().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push rist a", testRistrettoMulBaseInt(t, 9)),
		pushBigIntStep("push rist b", testRistrettoMulBaseInt(t, 4)),
		opStep("rist255_sub", funcsop.RIST255_SUB().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push rist a", testRistrettoMulBaseInt(t, 5)),
		pushIntStep("pushint 3", 3),
		opStep("rist255_mul", funcsop.RIST255_MUL().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push rist a", testRistrettoMulBaseInt(t, 5)),
		pushBigIntStep("push rist b", testRistrettoMulBaseInt(t, 7)),
		opStep("rist255_qadd", funcsop.RIST255_QADD().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		opStep("bls_pushr", funcsop.BLS_PUSHR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("bls_g1_zero", funcsop.BLS_G1_ZERO().Serialize()),
		opStep("dup g1", stackop.DUP().Serialize()),
		opStep("bls_g1_iszero", funcsop.BLS_G1_ISZERO().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("bls_g2_zero", funcsop.BLS_G2_ZERO().Serialize()),
		opStep("dup g2", stackop.DUP().Serialize()),
		opStep("bls_g2_iszero", funcsop.BLS_G2_ISZERO().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushSliceStep("push fp", testSliceFromBytes(fp)),
		opStep("bls_map_to_g1", funcsop.BLS_MAP_TO_G1().Serialize()),
		opStep("dup g1", stackop.DUP().Serialize()),
		opStep("bls_g1_ingroup", funcsop.BLS_G1_INGROUP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push fp2", testSliceFromBytes(fp2)),
		opStep("bls_map_to_g2", funcsop.BLS_MAP_TO_G2().Serialize()),
		opStep("dup g2", stackop.DUP().Serialize()),
		opStep("bls_g2_ingroup", funcsop.BLS_G2_INGROUP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushSliceStep("push pub1", testSliceFromBytes(pub1)),
		pushSliceStep("push msg", testSliceFromBytes(msg)),
		pushSliceStep("push sig1", testSliceFromBytes(sig1)),
		opStep("bls_verify", funcsop.BLS_VERIFY().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushSliceStep("push sig1", testSliceFromBytes(sig1)),
		pushSliceStep("push sig2", testSliceFromBytes(sig2)),
		pushIntStep("pushint 2", 2),
		opStep("bls_aggregate", funcsop.BLS_AGGREGATE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushSliceStep("push pub1", testSliceFromBytes(pub1)),
		pushSliceStep("push pub2", testSliceFromBytes(pub2)),
		pushIntStep("pushint 2", 2),
		pushSliceStep("push msg", testSliceFromBytes(msg)),
		pushSliceStep("push aggsig", testSliceFromBytes(aggSig)),
		opStep("bls_fastaggregateverify", funcsop.BLS_FASTAGGREGATEVERIFY().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push pub1", testSliceFromBytes(pub1)),
		pushSliceStep("push msg", testSliceFromBytes(msg)),
		pushSliceStep("push pub2", testSliceFromBytes(pub2)),
		pushSliceStep("push msg", testSliceFromBytes(msg)),
		pushIntStep("pushint 2", 2),
		pushSliceStep("push aggsig", testSliceFromBytes(aggSig)),
		opStep("bls_aggregateverify", funcsop.BLS_AGGREGATEVERIFY().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push g1a", testSliceFromBytes(g1a)),
		pushSliceStep("push g1b", testSliceFromBytes(g1b)),
		opStep("bls_g1_add", funcsop.BLS_G1_ADD().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push g1a", testSliceFromBytes(g1a)),
		pushIntStep("pushint 0", 0),
		opStep("bls_g1_mul", funcsop.BLS_G1_MUL().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushIntStep("pushint 0", 0),
		opStep("bls_g1_multiexp", funcsop.BLS_G1_MULTIEXP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push g1a", testSliceFromBytes(g1a)),
		pushIntStep("pushint 5", 5),
		pushSliceStep("push g1b", testSliceFromBytes(g1b)),
		pushIntStep("pushint 7", 7),
		pushIntStep("pushint 2", 2),
		opStep("bls_g1_multiexp 2", funcsop.BLS_G1_MULTIEXP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push g2a", testSliceFromBytes(g2a)),
		pushSliceStep("push g2b", testSliceFromBytes(g2b)),
		opStep("bls_g2_add", funcsop.BLS_G2_ADD().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push g2a", testSliceFromBytes(g2a)),
		pushIntStep("pushint 0", 0),
		opStep("bls_g2_mul", funcsop.BLS_G2_MUL().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 0", 0),
		opStep("bls_g2_multiexp", funcsop.BLS_G2_MULTIEXP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push g2a", testSliceFromBytes(g2a)),
		pushIntStep("pushint 5", 5),
		pushSliceStep("push g2b", testSliceFromBytes(g2b)),
		pushIntStep("pushint 7", 7),
		pushIntStep("pushint 2", 2),
		opStep("bls_g2_multiexp 2", funcsop.BLS_G2_MULTIEXP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		pushSliceStep("push g1 zero", testSliceFromBytes(g1Zero)),
		pushSliceStep("push g2 zero", testSliceFromBytes(g2Zero)),
		pushIntStep("pushint 1", 1),
		opStep("bls_pairing", funcsop.BLS_PAIRING().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),

		opStep("getparam 2", funcsop.GETPARAM(2).Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("blocklt", funcsop.BLOCKLT().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("ltime", funcsop.LTIME().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("randseed", funcsop.RANDSEED().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("balance", funcsop.BALANCE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("myaddr", funcsop.MYADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("configroot", funcsop.CONFIGROOT().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("mycode", funcsop.MYCODE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("incomingvalue", funcsop.INCOMINGVALUE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("storagefees", funcsop.STORAGEFEES().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("configdict", funcsop.CONFIGDICT().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 7", 7),
		opStep("configparam hit", funcsop.CONFIGPARAM().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 8", 8),
		opStep("configparam miss", funcsop.CONFIGPARAM().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 7", 7),
		opStep("configoptparam hit", funcsop.CONFIGOPTPARAM().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 8", 8),
		opStep("configoptparam miss", funcsop.CONFIGOPTPARAM().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("getglob 1", funcsop.GETGLOB(1).Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 2", 2),
		opStep("getglobvar 2", funcsop.GETGLOBVAR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push ecrecover hash", new(big.Int).SetBytes(ecrecoverHash)),
		pushIntStep("push ecrecover v", int64(ecrecoverV)),
		pushBigIntStep("push ecrecover r", ecrecoverR),
		pushBigIntStep("push ecrecover s", ecrecoverS),
		opStep("ecrecover", funcsop.ECRECOVER().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push secp xonly key", secpXOnlyKey),
		pushIntStep("push secp tweak", 7),
		opStep("secp256k1_xonly_pubkey_tweak_add", funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushBigIntStep("push p256 hash", new(big.Int).SetBytes(p256HashBytes)),
		pushSliceStep("push p256 sigu", cell.BeginCell().MustStoreSlice(p256SigU, 512).ToSlice()),
		pushSliceStep("push p256 pub", cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice()),
		opStep("p256_chksignu", funcsop.P256_CHKSIGNU().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push p256 slice data", cell.BeginCell().MustStoreSlice(p256SliceData, uint(len(p256SliceData))*8).ToSlice()),
		pushSliceStep("push p256 sigs", cell.BeginCell().MustStoreSlice(p256SigS, 512).ToSlice()),
		pushSliceStep("push p256 pub", cell.BeginCell().MustStoreSlice(p256Pub, 264).ToSlice()),
		opStep("p256_chksigns", funcsop.P256_CHKSIGNS().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 456", 456),
		opStep("setglob 2", funcsop.SETGLOB(2).Serialize()),
		opStep("getglob 2", funcsop.GETGLOB(2).Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 999", 999),
		pushIntStep("pushint 5", 5),
		opStep("setglobvar 5", funcsop.SETGLOBVAR().Serialize()),
		pushIntStep("pushint 5", 5),
		opStep("getglobvar 5", funcsop.GETGLOBVAR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
	}

	return steps, c7
}

func buildFeeSuperContractSteps(t *testing.T) []superContractStep {
	t.Helper()

	stdAddrSlice := cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice()
	invalidAnycast := cell.BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(31, 5).
		MustStoreSlice(bytes.Repeat([]byte{0xFF}, 4), 31).
		MustStoreInt(int64(tonopsTestAddr.Workchain()), 8).
		MustStoreSlice(tonopsTestAddr.Data(), 256).
		ToSlice()
	dataSizeLeaf := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	dataSizeRoot := cell.BeginCell().MustStoreRef(dataSizeLeaf).MustStoreRef(dataSizeLeaf).EndCell()

	return []superContractStep{
		opStep("prevblocksinfotuple", funcsop.PREVBLOCKSINFOTUPLE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("prevmcblocks", funcsop.PREVMCBLOCKS().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("prevkeyblock", funcsop.PREVKEYBLOCK().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("prevmcblocks_100", funcsop.PREVMCBLOCKS_100().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("duepayment", funcsop.DUEPAYMENT().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("inmsg_value", funcsop.INMSG_VALUE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("getprecompiledgas", funcsop.GETPRECOMPILEDGAS().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("randu256", funcsop.RANDU256().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 7", 7),
		opStep("rand", funcsop.RAND().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 7", 7),
		opStep("setrand", funcsop.SETRAND().Serialize()),
		opStep("randseed after setrand", funcsop.RANDSEED().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 7", 7),
		opStep("addrand", funcsop.ADDRAND().Serialize()),
		opStep("randseed after addrand", funcsop.RANDSEED().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 2", 2),
		pushIntStep("pushint 3", 3),
		pushIntStep("pushint 10", 10),
		pushIntStep("pushint 0", 0),
		opStep("getstoragefee", funcsop.GETSTORAGEFEE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 250", 250),
		pushIntStep("pushint 0", 0),
		opStep("getgasfee", funcsop.GETGASFEE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 2", 2),
		pushIntStep("pushint 8", 8),
		pushIntStep("pushint 0", 0),
		opStep("getforwardfee", funcsop.GETFORWARDFEE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 3200", 3200),
		pushIntStep("pushint 0", 0),
		opStep("getoriginalfwdfee", funcsop.GETORIGINALFWDFEE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 2", 2),
		pushIntStep("pushint 8", 8),
		pushIntStep("pushint 0", 0),
		opStep("getforwardfeesimple", funcsop.GETFORWARDFEESIMPLE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("pushint 250", 250),
		pushIntStep("pushint 0", 0),
		opStep("getgasfeesimple", funcsop.GETGASFEESIMPLE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push hello world", cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice()),
		opStep("sha256u", funcsop.SHA256U().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push hashext slice", cell.BeginCell().MustStoreSlice([]byte("hello world"), 88).ToSlice()),
		pushIntStep("pushint 1", 1),
		opStep("hashext", funcsop.HASHEXT(0).Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("newc for hashbu", cellsliceop.NEWC().Serialize()),
		opStep("hashbu", funcsop.HASHBU().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushCellStep("push datasize cell", dataSizeRoot),
		pushIntStep("pushint 10", 10),
		opStep("cdatasize", funcsop.CDATASIZE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushCellStep("push datasizeq cell", dataSizeRoot),
		pushIntStep("pushint 1", 1),
		opStep("cdatasizeq", funcsop.CDATASIZEQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push datasize slice", dataSizeRoot.MustBeginParse()),
		pushIntStep("pushint 10", 10),
		opStep("sdatasize", funcsop.SDATASIZE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push ldvarint16 src", cell.BeginCell().MustStoreUInt(1, 4).MustStoreInt(-17, 8).ToSlice()),
		opStep("ldvarint16", funcsop.LDVARINT16().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push ldvaruint32 src", cell.BeginCell().MustStoreUInt(1, 5).MustStoreUInt(17, 8).ToSlice()),
		opStep("ldvaruint32", funcsop.LDVARUINT32().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push ldvarint32 src", cell.BeginCell().MustStoreUInt(1, 5).MustStoreInt(-17, 8).ToSlice()),
		opStep("ldvarint32", funcsop.LDVARINT32().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("newc for stvarint16", cellsliceop.NEWC().Serialize()),
		pushIntStep("pushint -17", -17),
		opStep("stvarint16", funcsop.STVARINT16().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("newc for stvarint32", cellsliceop.NEWC().Serialize()),
		pushIntStep("pushint -17", -17),
		opStep("stvarint32", funcsop.STVARINT32().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("ldmsgaddr", funcsop.LDMSGADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("ldmsgaddrq", funcsop.LDMSGADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("parsemsgaddr", funcsop.PARSEMSGADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push invalid anycast", invalidAnycast),
		opStep("parsemsgaddrq", funcsop.PARSEMSGADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("rewritestdaddr", funcsop.REWRITESTDADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("rewritestdaddrq", funcsop.REWRITESTDADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("rewritevaraddr", funcsop.REWRITEVARADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("rewritevaraddrq", funcsop.REWRITEVARADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
	}
}

func buildExtraBalanceSuperContractSteps(t *testing.T) []superContractStep {
	t.Helper()

	return []superContractStep{
		pushIntStep("push extra balance key 7", 7),
		opStep("getextrabalance hit", funcsop.GETEXTRABALANCE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushIntStep("push extra balance key 9", 9),
		opStep("getextrabalance miss", funcsop.GETEXTRABALANCE().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
	}
}

func buildSuperContractStepsC7ForNegative(t *testing.T) tuple.Tuple {
	t.Helper()

	return superContractStepsC7(t, superContractStepsConfigRoot(t, -1))
}

func buildActionSuperContractSteps(t *testing.T) []superContractStep {
	t.Helper()

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
	extra := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()

	return []superContractStep{
		pushBigIntStep("push rawreserve amount", big.NewInt(777)),
		pushIntStep("push rawreserve mode", 0),
		opStep("rawreserve", funcsop.RAWRESERVE().Serialize()),
		pushBigIntStep("push rawreservex amount", big.NewInt(777)),
		pushCellStep("push rawreservex extra", extra),
		pushIntStep("push rawreservex mode", 3),
		opStep("rawreservex", funcsop.RAWRESERVEX().Serialize()),
		pushCellStep("push setcode cell", sendMsgCell),
		opStep("setcode", funcsop.SETCODE().Serialize()),
		pushCellStep("push setlibcode cell", sendMsgCell),
		pushIntStep("push setlibcode mode", 1),
		opStep("setlibcode", funcsop.SETLIBCODE().Serialize()),
		pushBigIntStep("push changelib hash", big.NewInt(1)),
		pushIntStep("push changelib mode", 1),
		opStep("changelib", funcsop.CHANGELIB().Serialize()),
		pushCellStep("push sendmsg fee cell", sendMsgCell),
		pushIntStep("push sendmsg fee mode", 1024),
		opStep("sendmsg fee only", funcsop.SENDMSG().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushCellStep("push sendmsg send cell", sendMsgCell),
		pushIntStep("push sendmsg send mode", 1),
		opStep("sendmsg send", funcsop.SENDMSG().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
	}
}

func feeExtraBalanceTestC7(t *testing.T) tuple.Tuple {
	t.Helper()

	return superContractExtraBalanceC7(t, nil)
}

func superContractFeeC7WithGlobalVersion(t *testing.T, version int) tuple.Tuple {
	t.Helper()

	return makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     tonopsCrossConfigWithGlobalVersion(t, uint32(version)),
		UnpackedConfig: feeTestUnpackedConfig(t),
		ExtraParams: map[int]any{
			13: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
}

func superContractExtraBalanceC7WithGlobalVersion(t *testing.T, version int) tuple.Tuple {
	t.Helper()

	return superContractExtraBalanceC7(t, tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
}

func superContractExtraBalanceC7(t *testing.T, configRoot *cell.Cell) tuple.Tuple {
	t.Helper()

	extraDict := cell.NewDict(32)
	if _, err := extraDict.SetBuilderWithMode(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreVarUInt(12345, 32),
		cell.DictSetModeSet,
	); err != nil {
		t.Fatalf("failed to seed extra balance dict: %v", err)
	}

	unpacked := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &unpacked, 0, makeStoragePricesSlice(100, 3, 5, 7, 11))
	mustSetTupleValue(t, &unpacked, 2, makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true))
	mustSetTupleValue(t, &unpacked, 3, makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true))
	mustSetTupleValue(t, &unpacked, 4, makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000))
	mustSetTupleValue(t, &unpacked, 5, makeMsgPricesSlice(900, 120, 220, 400, 800, 1200))
	mustSetTupleValue(t, &unpacked, 6, makeSizeLimitsSlice(1<<20, 128))

	return makeTonopsTestC7(t, tonopsTestC7Config{
		ConfigRoot:     configRoot,
		Balance:        tuple.NewTupleValue(new(big.Int).Set(tonopsTestBalance), extraDict.AsCell()),
		UnpackedConfig: unpacked,
		ExtraParams: map[int]any{
			13: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
}

func buildAddressSuperContractSteps(t *testing.T) []superContractStep {
	t.Helper()

	stdAddrSlice := cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice()
	addrNoneTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()

	return []superContractStep{
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("ldstdaddr", funcsop.LDSTDADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("ldstdaddrq", funcsop.LDSTDADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push addr none tail", addrNoneTail),
		opStep("ldoptstdaddr", funcsop.LDOPTSTDADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push addr none tail", addrNoneTail),
		opStep("ldoptstdaddrq", funcsop.LDOPTSTDADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("newc for ststdaddr", cellsliceop.NEWC().Serialize()),
		opStep("ststdaddr", funcsop.STSTDADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		pushSliceStep("push std addr", stdAddrSlice),
		opStep("newc for ststdaddrq", cellsliceop.NEWC().Serialize()),
		opStep("ststdaddrq", funcsop.STSTDADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("pushnull for stoptstdaddr", tupleop.PUSHNULL().Serialize()),
		opStep("newc for stoptstdaddr", cellsliceop.NEWC().Serialize()),
		opStep("stoptstdaddr", funcsop.STOPTSTDADDR().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("pushnull for stoptstdaddrq", tupleop.PUSHNULL().Serialize()),
		opStep("newc for stoptstdaddrq", cellsliceop.NEWC().Serialize()),
		opStep("stoptstdaddrq", funcsop.STOPTSTDADDRQ().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
		opStep("drop", stackop.DROP().Serialize()),
	}
}

func prependRawMethodDropBuilders(t *testing.T, builders ...*cell.Builder) *cell.Cell {
	t.Helper()

	all := make([]*cell.Builder, 0, len(builders)+1)
	all = append(all, cell.BeginCell().MustStoreUInt(0x30, 8))
	all = append(all, builders...)
	return chainedCodeFromBuilders(t, all...)
}

func chainedCodeFromBuilders(t *testing.T, builders ...*cell.Builder) *cell.Cell {
	t.Helper()

	if len(builders) == 0 {
		return cell.BeginCell().EndCell()
	}

	chunks := make([]*cell.Builder, 0, len(builders))
	current := cell.BeginCell()

	for i, builder := range builders {
		reserveRefs := 0
		if i < len(builders)-1 {
			reserveRefs = 1
		}

		if current.BitsUsed()+builder.BitsUsed() >= 1024 || current.RefsUsed()+builder.RefsUsed()+reserveRefs > 4 {
			if current.BitsUsed() == 0 && current.RefsUsed() == 0 {
				t.Fatalf("single code fragment does not fit into a code cell at builder %d", i)
			}
			chunks = append(chunks, current)
			current = cell.BeginCell()
		}

		if err := current.StoreBuilder(builder); err != nil {
			t.Fatalf("failed to build chained code cell at builder %d: %v", i, err)
		}
	}

	chunks = append(chunks, current)

	root := chunks[len(chunks)-1].EndCell()
	for i := len(chunks) - 2; i >= 0; i-- {
		if err := chunks[i].StoreRef(root); err != nil {
			t.Fatalf("failed to link chained code cell %d: %v", i, err)
		}
		root = chunks[i].EndCell()
	}

	return root
}

func opStep(name string, builder *cell.Builder) superContractStep {
	return superContractStep{name: name, builder: builder}
}

func pushIntStep(name string, value int64) superContractStep {
	return superContractStep{name: name, builder: stackop.PUSHINT(big.NewInt(value)).Serialize()}
}

func pushBigIntStep(name string, value *big.Int) superContractStep {
	return superContractStep{name: name, builder: stackop.PUSHINT(value).Serialize()}
}

func pushSliceStep(name string, value *cell.Slice) superContractStep {
	return superContractStep{name: name, builder: stackop.PUSHSLICEINLINE(value).Serialize()}
}

func pushCellStep(name string, value *cell.Cell) superContractStep {
	return superContractStep{name: name, builder: stackop.PUSHREF(value).Serialize()}
}
