//go:build cgo && tvm_cross_emulator

package tvm

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	expectedCrossEmulatorVersionTestAnchorCount = 131
	expectedCrossEmulatorVersionTestAnchorHash  = "909bd9427b65ab69e4568d8937128f4f3af146379b331e6e71dd03fdbfea46df"
	expectedCrossEmulatorVersionFuzzerCount     = 135
	expectedCrossEmulatorVersionFuzzerHash      = "fbc332b5bf24be75baca10ab84d2eff51a9deca48fa064bf64a00ca0798c22b0"

	expectedCrossEmulatorFullRangeVersionFuzzerCount = 129
	expectedCrossEmulatorFullRangeVersionFuzzerHash  = "625bf3d68e3ab49bf47981e34096d3caeb33fb8af7aa01daf358630aa81f1b2f"

	expectedCrossEmulatorUnclassifiedExplicitVersionUserCount = 4
	expectedCrossEmulatorUnclassifiedExplicitVersionUserHash  = "be8567c92088edc86cdc877242001d982aefa32ce73c0166fa20472924b5321c"

	expectedCrossEmulatorTransactionVersionTestCount = 46
	expectedCrossEmulatorTransactionVersionTestHash  = "a81aa79410833955c13415a0c9b5c9f82b26b0a1c40f12b2f691e459d6ff3547"
	expectedCrossEmulatorTransactionVersionFuzzCount = 48
	expectedCrossEmulatorTransactionVersionFuzzHash  = "2bbcff7f27693d609863afd367a66292d7d07036419640531b3fdc1d9dd4460c"

	expectedCrossEmulatorTransactionPhaseVersionTestCount = 3
	expectedCrossEmulatorTransactionPhaseVersionTestHash  = "0d194e88056a8797c4d34761c70310af33adec22d92d59521f13b675a6e14550"
	expectedCrossEmulatorTransactionPhaseVersionFuzzCount = 3
	expectedCrossEmulatorTransactionPhaseVersionFuzzHash  = "06bb5931d9684d9f6ad8a90be4ec0025117e9fe9dd580834a0ed2c04a5254a01"

	expectedCrossEmulatorMessageVersionTestCount = 8
	expectedCrossEmulatorMessageVersionTestHash  = "347e043e5757fcbe4ba6d0dae03b3a28480e0d31a536ca84889a25cda8d924ba"
	expectedCrossEmulatorMessageVersionFuzzCount = 12
	expectedCrossEmulatorMessageVersionFuzzHash  = "bc587f73965da46d3b76290c1be35f1d44d8a40887392c80abb435f56d08fe91"

	expectedCrossEmulatorGetMethodVersionTestCount = 4
	expectedCrossEmulatorGetMethodVersionTestHash  = "d108476d1c6e2f1b5405ea4ca219e3f2a681a216c8ecda8ff931b596d5a322ba"
	expectedCrossEmulatorGetMethodVersionFuzzCount = 4
	expectedCrossEmulatorGetMethodVersionFuzzHash  = "994eb3a1b97de56191facc7769c1ab90ac6d39cf4bcacdde17ffa536a9d788f2"

	expectedCrossEmulatorExecutionProofVersionTestCount = 4
	expectedCrossEmulatorExecutionProofVersionTestHash  = "09890df12d4791aaef349d041c4d165c7c11babe00d29de89ca1edf055090d70"
	expectedCrossEmulatorExecutionProofVersionFuzzCount = 5
	expectedCrossEmulatorExecutionProofVersionFuzzHash  = "06d47a64e2e8a4c019a60df278167a74034e75cd9b2aee8cd7e6ff2b50114f32"

	expectedCrossEmulatorTickTockVersionTestCount = 5
	expectedCrossEmulatorTickTockVersionTestHash  = "20ee676d00bb7dc756abc4ebb3f7fd9bd7a5cacdaed5d75bef00f9bb9265f300"
	expectedCrossEmulatorTickTockVersionFuzzCount = 8
	expectedCrossEmulatorTickTockVersionFuzzHash  = "02aafb4ffd98d530e685284fd2581714c27d48a73e6656449d95d5219ae25996"

	expectedCrossEmulatorWalletSendVersionTestCount = 1
	expectedCrossEmulatorWalletSendVersionTestHash  = "0e7f7a78597159505b620bebf8dff8c54eeaa660e0fdb081c8803138fad02186"
	expectedCrossEmulatorWalletSendVersionFuzzCount = 2
	expectedCrossEmulatorWalletSendVersionFuzzHash  = "b653ece9721d234231911f02b83881d962bb2e5bac65f40d7917087b55a840ff"

	expectedCrossEmulatorRunVMVersionTestCount = 4
	expectedCrossEmulatorRunVMVersionTestHash  = "5f929d5cb466255d2d9a6981106458cdc7b7a5f436207e6978ca007271cd4d80"
	expectedCrossEmulatorRunVMVersionFuzzCount = 3
	expectedCrossEmulatorRunVMVersionFuzzHash  = "efa53a5c8caf6235de4d7dfb5104751bb0ca27660e11eab3361ed8c7d999d384"

	expectedCrossEmulatorTonOpsVersionTestCount = 7
	expectedCrossEmulatorTonOpsVersionTestHash  = "62460d147ea614eb4419bce0d7ab7cdad21c454bca10c5275894a74d4de3d32a"
	expectedCrossEmulatorTonOpsVersionFuzzCount = 7
	expectedCrossEmulatorTonOpsVersionFuzzHash  = "aec0cdd0a5c6401af24cd6bd855345f526d0cdb8ba0839e6812b2a6b0cbb788c"

	expectedCrossEmulatorSupercontractVersionTestCount = 7
	expectedCrossEmulatorSupercontractVersionTestHash  = "389a192608843aee63a6f0e5473890919d608dce7e01095b98436f87ad4d1b4b"
	expectedCrossEmulatorSupercontractVersionFuzzCount = 7
	expectedCrossEmulatorSupercontractVersionFuzzHash  = "c5e5e959528940217ca6859923b87aed3d99f0a473842ee5880ef89277c46741"

	expectedCrossEmulatorTupleVersionTestCount = 4
	expectedCrossEmulatorTupleVersionTestHash  = "abd8871c3c4ff4b4cf43db92b5788e024b3ecc807d575271d985ead517b33f94"
	expectedCrossEmulatorTupleVersionFuzzCount = 4
	expectedCrossEmulatorTupleVersionFuzzHash  = "59780be7273caa353a98510108b790f059b18a53917a7a962f9bdb7486cfd6f4"

	expectedCrossEmulatorStackOpsVersionTestCount = 6
	expectedCrossEmulatorStackOpsVersionTestHash  = "fc6310d83791b17eba1c8620f2e41492a2e7b34e7ee865b4ffef3e16d51d79be"
	expectedCrossEmulatorStackOpsVersionFuzzCount = 5
	expectedCrossEmulatorStackOpsVersionFuzzHash  = "a9105d7ea578fc3395bda0336f3d47890391a3a5a145ccd9b274d953795cc913"

	expectedCrossEmulatorDictOpsVersionTestCount = 4
	expectedCrossEmulatorDictOpsVersionTestHash  = "c866872123fd18a3525cc74d60c86154d103efaec25ca953f7394551ba82c503"
	expectedCrossEmulatorDictOpsVersionFuzzCount = 4
	expectedCrossEmulatorDictOpsVersionFuzzHash  = "39f6b8d683287f3e1f12da2eef24392e37869144e8e1adc07d1a211580bdaff5"

	expectedCrossEmulatorArithOpsVersionTestCount = 3
	expectedCrossEmulatorArithOpsVersionTestHash  = "8ef3e4a8ce6bf4ba2c4133bc15245d225c665b6433c81f9279d9eace1800c154"
	expectedCrossEmulatorArithOpsVersionFuzzCount = 3
	expectedCrossEmulatorArithOpsVersionFuzzHash  = "46d6e834fb485d13d2cad971fa3de75fe39f80007ffd130afbe2b9caa80b78e0"

	expectedCrossEmulatorContOpsVersionTestCount = 3
	expectedCrossEmulatorContOpsVersionTestHash  = "0bd7ea4e45b2e9b66129ede8a02d032718ef0e521c7bcdd7c2b736d058ca8393"
	expectedCrossEmulatorContOpsVersionFuzzCount = 3
	expectedCrossEmulatorContOpsVersionFuzzHash  = "1f1869cda3b2b809465d81cda75a3b3872cde0d7235fc47d14d970a709aea2cf"

	expectedCrossEmulatorCellOpsVersionTestCount = 4
	expectedCrossEmulatorCellOpsVersionTestHash  = "1735b8915b57782f618909100cf37e7390d4560017de3ea769e95d9083856fd4"
	expectedCrossEmulatorCellOpsVersionFuzzCount = 4
	expectedCrossEmulatorCellOpsVersionFuzzHash  = "db9ef8ac5ed91381c852438767473a69dbf1f77542e7c465bacbbf2ef9baceb1"

	expectedCrossEmulatorExceptionOpsVersionTestCount = 2
	expectedCrossEmulatorExceptionOpsVersionTestHash  = "605f5f4edadefc66ca028d30ae7f724f85b65281590940f8744b271dd1ab07bb"
	expectedCrossEmulatorExceptionOpsVersionFuzzCount = 2
	expectedCrossEmulatorExceptionOpsVersionFuzzHash  = "b744e3ecc467bf2290f3a88b59d103f7c851c9b002b971cb161e93f5436569d9"

	expectedCrossEmulatorVMRuntimeVersionTestCount = 4
	expectedCrossEmulatorVMRuntimeVersionTestHash  = "cc6b949c96d45cbb70eea6beb1cc99e238363bdc63ff7b8235b63b7759596192"
	expectedCrossEmulatorVMRuntimeVersionFuzzCount = 4
	expectedCrossEmulatorVMRuntimeVersionFuzzHash  = "5ac075bc249b0b0360428184319a24a9af689c8e3279f558352dd89fec7c76f1"

	expectedCrossEmulatorOpcodeGateVersionTestCount = 4
	expectedCrossEmulatorOpcodeGateVersionTestHash  = "9f18d65a466c034a37eee36e9ae66f95da5a63a4a32a76f18505321f0ecc75a3"
	expectedCrossEmulatorOpcodeGateVersionFuzzCount = 3
	expectedCrossEmulatorOpcodeGateVersionFuzzHash  = "5d9c04382d5a685336bf5ee041d1b9419f3e51f548e97750b0bdd0b6e4d6636d"

	expectedCrossEmulatorCoreConfigVersionTestCount = 6
	expectedCrossEmulatorCoreConfigVersionTestHash  = "cf8f62c0d7ed4573d6cac3dd69803c90fdb2e63ae9cfd9d771595167b8e1dee2"
	expectedCrossEmulatorCoreConfigVersionFuzzCount = 6
	expectedCrossEmulatorCoreConfigVersionFuzzHash  = "9abb31b3c1e112daf94ae1126f0ca84bd4997df8a9033a90c4a66ef1a0fd034f"

	expectedCrossEmulatorDifferentialMatrixVersionTestCount = 10
	expectedCrossEmulatorDifferentialMatrixVersionTestHash  = "232e63c7cd211af1c8fcaf32eac44f634410d8ed3c99a1c7925e733164d1d845"
	expectedCrossEmulatorDifferentialMatrixVersionFuzzCount = 1
	expectedCrossEmulatorDifferentialMatrixVersionFuzzHash  = "eb1af974ef27c8111bbdf1062c58671a61d2c76ed60290b1e0da5b0b2d9b900e"

	expectedCrossEmulatorReferenceMismatchUseCount = 26
	expectedCrossEmulatorReferenceMismatchUseHash  = "312c8057852d591fc73f86c4271898887bef3d7b6feda741f29d009cf1d0ba06"

	expectedCrossEmulatorDirectKnownReferenceSkipCount = 15
	expectedCrossEmulatorDirectKnownReferenceSkipHash  = "5e42f5a80416a3a31da602d6bf0c9072da0132aea5bd820d3844bac205fd6774"

	expectedCrossEmulatorSkipReferenceUseCount = 25
	expectedCrossEmulatorSkipReferenceUseHash  = "440b673550f7514e57abcbce6c6505cb5a3463ac289fb8164a10ac433774b95d"

	expectedCrossEmulatorVersionAuditPrefixUseCount = 66
	expectedCrossEmulatorVersionAuditPrefixUseHash  = "32a7f48dde592274da61ff256ef52317bfe828e1ede925d9fcc396bbb81f25c4"

	expectedCrossEmulatorVersionSelectionHelperCount = 5
	expectedCrossEmulatorVersionSelectionHelperHash  = "d7e6311cedb8ed6d4282fb03db6fca77197dc488a220c23f327f67a033fafd93"

	expectedCrossEmulatorVersionSelectionHelperShapeCount = 5
	expectedCrossEmulatorVersionSelectionHelperShapeHash  = "a40695588b85feffb3e9b3ab3d99bdc36107fadff5105ad18c13ba3bcf071dd3"
	expectedCrossEmulatorVersionSelectionHelperShardCount = 5
	expectedCrossEmulatorVersionSelectionHelperShardHash  = "8c72aa0a663dab4f97af242441e9ac104ae0894ddcb06655da4953bf23f06755"
)

var expectedCrossEmulatorReferenceMismatchReasons = map[string]int{
	"bundled reference emulator predates upstream ECRECOVER v=27/28 support":                              1,
	"bundled reference emulator predates upstream CHKSIG v14 zero/identity public-key rejection":          1,
	"bundled reference emulator predates upstream QRSHIFT# v14 NaN preservation":                          2,
	"bundled reference emulator predates upstream RIST255 v14 identity support":                           1,
	"bundled reference emulator predates upstream RIST255 v14 zero-scalar validation":                     1,
	"bundled reference emulator predates upstream RSHIFT# v14 NaN preservation":                           2,
	"bundled reference emulator predates upstream SENDMSG v14 user fwd fee handling":                      1,
	"bundled reference emulator predates upstream control-register v14 silent duplicate save-list writes": 2,
	"bundled reference emulator predates upstream transaction v14 failed-action message-balance restore":  2,
	"bundled reference emulator predates upstream v9 direct startup library code loading":                 13,
}

type crossEmulatorUnclassifiedExplicitVersionUser struct {
	reason         string
	versionAnchors []string
}

var expectedCrossEmulatorUnclassifiedExplicitVersionUsers = map[string]crossEmulatorUnclassifiedExplicitVersionUser{
	"parity_fuzz_cross_emulator_test.go:TestTVMDifferentialFuzzFamiliesCrossEmulatorSmoke": {
		reason: "mixed differential smoke covers default and version-matrix families; dedicated version matrix fuzzers own exhaustive version seeding",
		versionAnchors: []string{
			"FuzzTVMDifferentialVersionMatrixPrograms",
		},
	},
	"runvm_cross_emulator_test.go:TestTVMCrossEmulatorRunVM": {
		reason: "broad RUNVM/RUNVMX default-version matrix; dedicated RUNVM global-version fuzzers own version-sensitive coverage",
		versionAnchors: []string{
			"FuzzTVMCrossEmulatorRunVMChildGlobalVersionInheritance",
			"FuzzTVMCrossEmulatorRunVMFailedDataActionsVersionMatrix",
			"FuzzTVMCrossEmulatorRunVMVersionedChildOpcodeMatrix",
		},
	},
	"tonops_cross_emulator_test.go:TestTVMCrossEmulatorTonOps": {
		reason: "broad run_method-compatible tonops suite uses versioned config fixtures; dedicated tonops global-version fuzzers own version-sensitive coverage",
		versionAnchors: []string{
			"FuzzTVMCrossEmulatorDataSizeLowGasGlobalVersion",
			"FuzzTVMCrossEmulatorTonOpsSendMsgExtraFlagsRootSizeGlobalVersion",
			"FuzzTVMCrossEmulatorTonOpsSendMsgVersionedFeeEdges",
			"FuzzTVMCrossEmulatorTonOpsUnderflowPrecheckGlobalVersion",
		},
	},
	"tonops_crypto_circl_cross_emulator_test.go:TestTVMCrossEmulatorTonOpsCryptoCircl": {
		reason: "broad crypto suite carries per-case minimum versions; dedicated crypto global-version fuzzers own version-sensitive coverage",
		versionAnchors: []string{
			"FuzzTVMCrossEmulatorTonOpsCryptoCirclVersionedRuntimeEdges",
		},
	},
}

func crossEmulatorVersionAuditVersions(t *testing.T, envPrefix string) []int {
	t.Helper()

	versions := make([]int, 0, MaxSupportedGlobalVersion-MinSupportedGlobalVersion+1)
	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		versions = append(versions, version)
	}

	rawShards := os.Getenv(envPrefix + "_SHARDS")
	rawShard := os.Getenv(envPrefix + "_SHARD")
	if rawShards == "" && rawShard == "" {
		return versions
	}
	if rawShards == "" || rawShard == "" {
		t.Fatalf("%s_SHARDS and %s_SHARD must be set together", envPrefix, envPrefix)
	}

	shards, err := strconv.Atoi(rawShards)
	if err != nil || shards <= 0 {
		t.Fatalf("invalid %s_SHARDS=%q", envPrefix, rawShards)
	}
	shard, err := strconv.Atoi(rawShard)
	if err != nil || shard < 0 || shard >= shards {
		t.Fatalf("invalid %s_SHARD=%q for %d shards", envPrefix, rawShard, shards)
	}

	out := make([]int, 0, (len(versions)+shards-1)/shards)
	for idx, version := range versions {
		if idx%shards == shard {
			out = append(out, version)
		}
	}
	if len(out) == 0 {
		t.Skipf("no version audit versions selected for %s shard %d/%d", envPrefix, shard, shards)
	}
	return out
}

func TestTVMCrossEmulatorVersionAuditShardSelection(t *testing.T) {
	const prefix = "TVM_TEST_VERSION_AUDIT"

	t.Setenv(prefix+"_SHARDS", "")
	t.Setenv(prefix+"_SHARD", "")

	all := crossEmulatorVersionAuditVersions(t, prefix)
	wantLen := MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != MinSupportedGlobalVersion || all[len(all)-1] != MaxSupportedGlobalVersion {
		t.Fatalf("default version selection = %v, want range %d..%d", all, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
	}

	t.Setenv(prefix+"_SHARDS", "4")
	t.Setenv(prefix+"_SHARD", "1")
	got := crossEmulatorVersionAuditVersions(t, prefix)
	want := []int{1, 5, 9, 13}
	if len(got) != len(want) {
		t.Fatalf("sharded version selection = %v, want %v", got, want)
	}
	for i, version := range want {
		if got[i] != version {
			t.Fatalf("sharded version selection = %v, want %v", got, want)
		}
	}

	for _, shards := range []int{1, 2, 3, 4, wantLen} {
		seen := make(map[int]int, wantLen)
		for shard := 0; shard < shards; shard++ {
			t.Setenv(prefix+"_SHARDS", strconv.Itoa(shards))
			t.Setenv(prefix+"_SHARD", strconv.Itoa(shard))
			for _, version := range crossEmulatorVersionAuditVersions(t, prefix) {
				seen[version]++
			}
		}
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			if seen[version] != 1 {
				t.Fatalf("%d-way shard partition covered v%d %d times; seen=%v", shards, version, seen[version], seen)
			}
		}
		if len(seen) != wantLen {
			t.Fatalf("%d-way shard partition covered %d versions, want %d; seen=%v", shards, len(seen), wantLen, seen)
		}
	}
}

func TestTVMCrossEmulatorKnownReferenceMismatchInventory(t *testing.T) {
	const reasonPrefix = "bundled reference emulator predates upstream "

	got := make(map[string]int)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil || !strings.Contains(value, reasonPrefix) {
				return true
			}
			got[value]++
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan known reference mismatch reasons: %v", err)
	}

	if len(got) != len(expectedCrossEmulatorReferenceMismatchReasons) {
		t.Fatalf("known reference mismatch reason count = %d, want %d; got %v", len(got), len(expectedCrossEmulatorReferenceMismatchReasons), got)
	}
	for reason, want := range expectedCrossEmulatorReferenceMismatchReasons {
		if got[reason] != want {
			t.Fatalf("known reference mismatch reason %q count = %d, want %d; all %v", reason, got[reason], want, got)
		}
	}
	for reason, count := range got {
		if _, ok := expectedCrossEmulatorReferenceMismatchReasons[reason]; !ok {
			t.Fatalf("unexpected known reference mismatch reason %q count=%d; update local guards or inventory", reason, count)
		}
	}
}

func TestTVMCrossEmulatorKnownReferenceMismatchesHaveLocalAnchors(t *testing.T) {
	if len(expectedCrossEmulatorReferenceMismatchReasons) != len(knownReferenceMismatchLocalAnchors) {
		t.Fatalf("cross-emulator known reference mismatch reason count = %d, local anchor suffix count = %d", len(expectedCrossEmulatorReferenceMismatchReasons), len(knownReferenceMismatchLocalAnchors))
	}

	for reason := range expectedCrossEmulatorReferenceMismatchReasons {
		if !strings.HasPrefix(reason, knownReferenceMismatchPrefix) {
			t.Fatalf("known reference mismatch reason %q does not use prefix %q", reason, knownReferenceMismatchPrefix)
		}
		suffix := strings.TrimPrefix(reason, knownReferenceMismatchPrefix)
		anchors, ok := knownReferenceMismatchLocalAnchors[suffix]
		if !ok {
			t.Fatalf("known reference mismatch reason %q has no local coverage anchors for suffix %q", reason, suffix)
		}
		if len(anchors) == 0 {
			t.Fatalf("known reference mismatch reason %q has an empty local coverage anchor list", reason)
		}
		if _, ok = knownReferenceMismatchBoundaryVersions[suffix]; !ok {
			t.Fatalf("known reference mismatch reason %q has no boundary version inventory", reason)
		}
	}

	for suffix := range knownReferenceMismatchLocalAnchors {
		reason := knownReferenceMismatchPrefix + suffix
		if _, ok := expectedCrossEmulatorReferenceMismatchReasons[reason]; !ok {
			t.Fatalf("local reference mismatch coverage suffix %q has no cross-emulator reason inventory entry", suffix)
		}
	}
}

func TestTVMCrossEmulatorKnownReferenceMismatchUseInventory(t *testing.T) {
	uses := crossEmulatorReferenceMismatchUseInventory(t)
	if len(uses) != expectedCrossEmulatorReferenceMismatchUseCount {
		t.Fatalf("known reference mismatch use count = %d, want %d:\n%s", len(uses), expectedCrossEmulatorReferenceMismatchUseCount, strings.Join(uses, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(uses); got != expectedCrossEmulatorReferenceMismatchUseHash {
		t.Fatalf("known reference mismatch use hash = %s, want %s:\n%s", got, expectedCrossEmulatorReferenceMismatchUseHash, strings.Join(uses, "\n"))
	}
}

func TestTVMCrossEmulatorDirectKnownReferenceSkipInventory(t *testing.T) {
	uses := crossEmulatorDirectKnownReferenceSkipInventory(t)
	if len(uses) != expectedCrossEmulatorDirectKnownReferenceSkipCount {
		t.Fatalf("direct known-reference skip count = %d, want %d:\n%s", len(uses), expectedCrossEmulatorDirectKnownReferenceSkipCount, strings.Join(uses, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(uses); got != expectedCrossEmulatorDirectKnownReferenceSkipHash {
		t.Fatalf("direct known-reference skip hash = %s, want %s:\n%s", got, expectedCrossEmulatorDirectKnownReferenceSkipHash, strings.Join(uses, "\n"))
	}
}

func TestTVMCrossEmulatorSkipReferenceUseInventory(t *testing.T) {
	uses := crossEmulatorSkipReferenceUseInventory(t)
	if len(uses) != expectedCrossEmulatorSkipReferenceUseCount {
		t.Fatalf("skipReference use count = %d, want %d:\n%s", len(uses), expectedCrossEmulatorSkipReferenceUseCount, strings.Join(uses, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(uses); got != expectedCrossEmulatorSkipReferenceUseHash {
		t.Fatalf("skipReference use hash = %s, want %s:\n%s", got, expectedCrossEmulatorSkipReferenceUseHash, strings.Join(uses, "\n"))
	}

	var unclassified []string
	for _, use := range uses {
		if !crossEmulatorSkipReferenceUseHasKnownReason(use) {
			unclassified = append(unclassified, use)
		}
	}
	if len(unclassified) > 0 {
		t.Fatalf("skipReference uses without known-reference reason or helper:\n%s", strings.Join(unclassified, "\n"))
	}
}

func TestTVMCrossEmulatorSkipReferenceKnownReasonClassifierRejectsLookalikes(t *testing.T) {
	cases := []struct {
		name string
		use  string
		want bool
	}{
		{
			name: "known reason literal",
			use:  "test.go:Test:case:" + knownReferenceMismatchPrefix + "RSHIFT# v14 NaN preservation",
			want: true,
		},
		{
			name: "local variable helper",
			use:  "test.go:Test:case:rshiftCodeSkipReference",
			want: true,
		},
		{
			name: "top-level helper value",
			use:  "test.go:Test:case:contOpsDuplicateSaveReferenceSkip",
			want: true,
		},
		{
			name: "versioned helper call",
			use:  "test.go:Test:case:ecrecoverEthereumReferenceSkip(version)",
			want: true,
		},
		{
			name: "versioned chksig helper call",
			use:  "test.go:Test:case:ed25519ChksigRejectedKeyReferenceSkip(version)",
			want: true,
		},
		{
			name: "unknown lookalike ident",
			use:  "test.go:Test:case:someReferenceSkip",
		},
		{
			name: "unknown lookalike call",
			use:  "test.go:Test:case:madeUpSkipReference(version)",
		},
		{
			name: "reference-looking text without prefix",
			use:  "test.go:Test:case:reference emulator predates something",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := crossEmulatorSkipReferenceUseHasKnownReason(tt.use); got != tt.want {
				t.Fatalf("known reason classifier = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTVMCrossEmulatorVersionTestsHaveFuzzAnchors(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	fuzzAnchors := make(map[string]struct{}, len(fuzzes))
	for _, name := range fuzzes {
		fuzzAnchors[crossEmulatorVersionFuzzKey(name)] = struct{}{}
	}

	aliases := crossEmulatorVersionFuzzAliases()
	var missing []string
	for _, name := range tests {
		if !crossEmulatorVersionTestNeedsFuzzAnchor(name) {
			continue
		}

		key := crossEmulatorVersionFuzzKey(name)
		if _, ok := fuzzAnchors[key]; ok {
			continue
		}

		ok := false
		for _, alias := range aliases[key] {
			if _, exists := fuzzAnchors[alias]; exists {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, name+" -> "+key)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator version tests without fuzz anchors:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	versionTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorVersionTestNeedsFuzzAnchor(name) {
			versionTests = append(versionTests, name)
		}
	}
	sort.Strings(versionTests)

	versionFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorVersionFuzzNeedsSupportedRange(name) {
			versionFuzzes = append(versionFuzzes, name)
		}
	}
	sort.Strings(versionFuzzes)

	if len(versionTests) != expectedCrossEmulatorVersionTestAnchorCount {
		t.Fatalf("cross-emulator version test anchor count = %d, want %d", len(versionTests), expectedCrossEmulatorVersionTestAnchorCount)
	}
	if got := crossEmulatorVersionNameInventoryHash(versionTests); got != expectedCrossEmulatorVersionTestAnchorHash {
		t.Fatalf("cross-emulator version test anchor hash = %s, want %s", got, expectedCrossEmulatorVersionTestAnchorHash)
	}
	if len(versionFuzzes) != expectedCrossEmulatorVersionFuzzerCount {
		t.Fatalf("cross-emulator version fuzzer count = %d, want %d", len(versionFuzzes), expectedCrossEmulatorVersionFuzzerCount)
	}
	if got := crossEmulatorVersionNameInventoryHash(versionFuzzes); got != expectedCrossEmulatorVersionFuzzerHash {
		t.Fatalf("cross-emulator version fuzzer hash = %s, want %s", got, expectedCrossEmulatorVersionFuzzerHash)
	}
}

func TestTVMCrossEmulatorTransactionVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	transactionTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if strings.HasPrefix(name, "TestTVMCrossEmulatorTransaction") && !crossEmulatorVersionAuditOnlyTest(name) {
			transactionTests = append(transactionTests, name)
		}
	}
	sort.Strings(transactionTests)

	transactionFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if strings.HasPrefix(name, "FuzzTVMCrossEmulatorTransaction") {
			transactionFuzzes = append(transactionFuzzes, name)
		}
	}
	sort.Strings(transactionFuzzes)

	if len(transactionTests) != expectedCrossEmulatorTransactionVersionTestCount {
		t.Fatalf("transaction cross-emulator version test count = %d, want %d", len(transactionTests), expectedCrossEmulatorTransactionVersionTestCount)
	}
	if got := crossEmulatorVersionNameInventoryHash(transactionTests); got != expectedCrossEmulatorTransactionVersionTestHash {
		t.Fatalf("transaction cross-emulator version test hash = %s, want %s", got, expectedCrossEmulatorTransactionVersionTestHash)
	}
	if len(transactionFuzzes) != expectedCrossEmulatorTransactionVersionFuzzCount {
		t.Fatalf("transaction cross-emulator version fuzzer count = %d, want %d", len(transactionFuzzes), expectedCrossEmulatorTransactionVersionFuzzCount)
	}
	if got := crossEmulatorVersionNameInventoryHash(transactionFuzzes); got != expectedCrossEmulatorTransactionVersionFuzzHash {
		t.Fatalf("transaction cross-emulator version fuzzer hash = %s, want %s", got, expectedCrossEmulatorTransactionVersionFuzzHash)
	}
}

func TestTVMCrossEmulatorTransactionPhaseVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	transactionPhaseTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorTransactionPhaseVersionFunctionName(name, "Test") {
			transactionPhaseTests = append(transactionPhaseTests, name)
		}
	}
	sort.Strings(transactionPhaseTests)

	transactionPhaseFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorTransactionPhaseVersionFunctionName(name, "Fuzz") {
			transactionPhaseFuzzes = append(transactionPhaseFuzzes, name)
		}
	}
	sort.Strings(transactionPhaseFuzzes)

	if len(transactionPhaseTests) != expectedCrossEmulatorTransactionPhaseVersionTestCount {
		t.Fatalf("transaction-phase cross-emulator version test count = %d, want %d:\n%s", len(transactionPhaseTests), expectedCrossEmulatorTransactionPhaseVersionTestCount, strings.Join(transactionPhaseTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(transactionPhaseTests); got != expectedCrossEmulatorTransactionPhaseVersionTestHash {
		t.Fatalf("transaction-phase cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorTransactionPhaseVersionTestHash, strings.Join(transactionPhaseTests, "\n"))
	}
	if len(transactionPhaseFuzzes) != expectedCrossEmulatorTransactionPhaseVersionFuzzCount {
		t.Fatalf("transaction-phase cross-emulator version fuzzer count = %d, want %d:\n%s", len(transactionPhaseFuzzes), expectedCrossEmulatorTransactionPhaseVersionFuzzCount, strings.Join(transactionPhaseFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(transactionPhaseFuzzes); got != expectedCrossEmulatorTransactionPhaseVersionFuzzHash {
		t.Fatalf("transaction-phase cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorTransactionPhaseVersionFuzzHash, strings.Join(transactionPhaseFuzzes, "\n"))
	}
}

func crossEmulatorTransactionPhaseVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorTransactionNonComputePhase") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorMessageVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	messageTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorMessageVersionFunctionName(name, "Test") {
			messageTests = append(messageTests, name)
		}
	}
	sort.Strings(messageTests)

	messageFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorMessageVersionFunctionName(name, "Fuzz") {
			messageFuzzes = append(messageFuzzes, name)
		}
	}
	sort.Strings(messageFuzzes)

	if len(messageTests) != expectedCrossEmulatorMessageVersionTestCount {
		t.Fatalf("message cross-emulator version test count = %d, want %d:\n%s", len(messageTests), expectedCrossEmulatorMessageVersionTestCount, strings.Join(messageTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(messageTests); got != expectedCrossEmulatorMessageVersionTestHash {
		t.Fatalf("message cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorMessageVersionTestHash, strings.Join(messageTests, "\n"))
	}
	if len(messageFuzzes) != expectedCrossEmulatorMessageVersionFuzzCount {
		t.Fatalf("message cross-emulator version fuzzer count = %d, want %d:\n%s", len(messageFuzzes), expectedCrossEmulatorMessageVersionFuzzCount, strings.Join(messageFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(messageFuzzes); got != expectedCrossEmulatorMessageVersionFuzzHash {
		t.Fatalf("message cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorMessageVersionFuzzHash, strings.Join(messageFuzzes, "\n"))
	}
}

func crossEmulatorMessageVersionFunctionName(name, prefix string) bool {
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorDirectMessage") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorCheckExternalMessageAccepted") ||
		(strings.HasPrefix(name, prefix+"TVMCrossEmulatorInternalMessage") &&
			crossEmulatorVersionFunctionNameClassified(name))
}

func TestTVMCrossEmulatorGetMethodVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	getMethodTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorGetMethodVersionFunctionName(name, "Test") {
			getMethodTests = append(getMethodTests, name)
		}
	}
	sort.Strings(getMethodTests)

	getMethodFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorGetMethodVersionFunctionName(name, "Fuzz") {
			getMethodFuzzes = append(getMethodFuzzes, name)
		}
	}
	sort.Strings(getMethodFuzzes)

	if len(getMethodTests) != expectedCrossEmulatorGetMethodVersionTestCount {
		t.Fatalf("get-method cross-emulator version test count = %d, want %d:\n%s", len(getMethodTests), expectedCrossEmulatorGetMethodVersionTestCount, strings.Join(getMethodTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(getMethodTests); got != expectedCrossEmulatorGetMethodVersionTestHash {
		t.Fatalf("get-method cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorGetMethodVersionTestHash, strings.Join(getMethodTests, "\n"))
	}
	if len(getMethodFuzzes) != expectedCrossEmulatorGetMethodVersionFuzzCount {
		t.Fatalf("get-method cross-emulator version fuzzer count = %d, want %d:\n%s", len(getMethodFuzzes), expectedCrossEmulatorGetMethodVersionFuzzCount, strings.Join(getMethodFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(getMethodFuzzes); got != expectedCrossEmulatorGetMethodVersionFuzzHash {
		t.Fatalf("get-method cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorGetMethodVersionFuzzHash, strings.Join(getMethodFuzzes, "\n"))
	}
}

func crossEmulatorGetMethodVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorGetMethod")
}

func TestTVMCrossEmulatorExecutionProofVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	executionProofTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorExecutionProofVersionFunctionName(name, "Test") {
			executionProofTests = append(executionProofTests, name)
		}
	}
	sort.Strings(executionProofTests)

	executionProofFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorExecutionProofVersionFunctionName(name, "Fuzz") {
			executionProofFuzzes = append(executionProofFuzzes, name)
		}
	}
	sort.Strings(executionProofFuzzes)

	if len(executionProofTests) != expectedCrossEmulatorExecutionProofVersionTestCount {
		t.Fatalf("execution-proof cross-emulator version test count = %d, want %d:\n%s", len(executionProofTests), expectedCrossEmulatorExecutionProofVersionTestCount, strings.Join(executionProofTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(executionProofTests); got != expectedCrossEmulatorExecutionProofVersionTestHash {
		t.Fatalf("execution-proof cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorExecutionProofVersionTestHash, strings.Join(executionProofTests, "\n"))
	}
	if len(executionProofFuzzes) != expectedCrossEmulatorExecutionProofVersionFuzzCount {
		t.Fatalf("execution-proof cross-emulator version fuzzer count = %d, want %d:\n%s", len(executionProofFuzzes), expectedCrossEmulatorExecutionProofVersionFuzzCount, strings.Join(executionProofFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(executionProofFuzzes); got != expectedCrossEmulatorExecutionProofVersionFuzzHash {
		t.Fatalf("execution-proof cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorExecutionProofVersionFuzzHash, strings.Join(executionProofFuzzes, "\n"))
	}
}

func crossEmulatorExecutionProofVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"ExecutionProofCrossEmulator") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorTickTockVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	tickTockTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorTickTockVersionFunctionName(name, "Test") {
			tickTockTests = append(tickTockTests, name)
		}
	}
	sort.Strings(tickTockTests)

	tickTockFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorTickTockVersionFunctionName(name, "Fuzz") {
			tickTockFuzzes = append(tickTockFuzzes, name)
		}
	}
	sort.Strings(tickTockFuzzes)

	if len(tickTockTests) != expectedCrossEmulatorTickTockVersionTestCount {
		t.Fatalf("tick/tock cross-emulator version test count = %d, want %d:\n%s", len(tickTockTests), expectedCrossEmulatorTickTockVersionTestCount, strings.Join(tickTockTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(tickTockTests); got != expectedCrossEmulatorTickTockVersionTestHash {
		t.Fatalf("tick/tock cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorTickTockVersionTestHash, strings.Join(tickTockTests, "\n"))
	}
	if len(tickTockFuzzes) != expectedCrossEmulatorTickTockVersionFuzzCount {
		t.Fatalf("tick/tock cross-emulator version fuzzer count = %d, want %d:\n%s", len(tickTockFuzzes), expectedCrossEmulatorTickTockVersionFuzzCount, strings.Join(tickTockFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(tickTockFuzzes); got != expectedCrossEmulatorTickTockVersionFuzzHash {
		t.Fatalf("tick/tock cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorTickTockVersionFuzzHash, strings.Join(tickTockFuzzes, "\n"))
	}
}

func crossEmulatorTickTockVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorTickTock") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorWalletSendVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	walletSendTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorWalletSendVersionFunctionName(name, "Test") {
			walletSendTests = append(walletSendTests, name)
		}
	}
	sort.Strings(walletSendTests)

	walletSendFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorWalletSendVersionFunctionName(name, "Fuzz") {
			walletSendFuzzes = append(walletSendFuzzes, name)
		}
	}
	sort.Strings(walletSendFuzzes)

	if len(walletSendTests) != expectedCrossEmulatorWalletSendVersionTestCount {
		t.Fatalf("wallet-send cross-emulator version test count = %d, want %d:\n%s", len(walletSendTests), expectedCrossEmulatorWalletSendVersionTestCount, strings.Join(walletSendTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(walletSendTests); got != expectedCrossEmulatorWalletSendVersionTestHash {
		t.Fatalf("wallet-send cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorWalletSendVersionTestHash, strings.Join(walletSendTests, "\n"))
	}
	if len(walletSendFuzzes) != expectedCrossEmulatorWalletSendVersionFuzzCount {
		t.Fatalf("wallet-send cross-emulator version fuzzer count = %d, want %d:\n%s", len(walletSendFuzzes), expectedCrossEmulatorWalletSendVersionFuzzCount, strings.Join(walletSendFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(walletSendFuzzes); got != expectedCrossEmulatorWalletSendVersionFuzzHash {
		t.Fatalf("wallet-send cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorWalletSendVersionFuzzHash, strings.Join(walletSendFuzzes, "\n"))
	}
}

func crossEmulatorWalletSendVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorWallet") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorRunVMVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	runVMTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorRunVMVersionFunctionName(name, "Test") {
			runVMTests = append(runVMTests, name)
		}
	}
	sort.Strings(runVMTests)

	runVMFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorRunVMVersionFunctionName(name, "Fuzz") {
			runVMFuzzes = append(runVMFuzzes, name)
		}
	}
	sort.Strings(runVMFuzzes)

	if len(runVMTests) != expectedCrossEmulatorRunVMVersionTestCount {
		t.Fatalf("runvm cross-emulator version test count = %d, want %d:\n%s", len(runVMTests), expectedCrossEmulatorRunVMVersionTestCount, strings.Join(runVMTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(runVMTests); got != expectedCrossEmulatorRunVMVersionTestHash {
		t.Fatalf("runvm cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorRunVMVersionTestHash, strings.Join(runVMTests, "\n"))
	}
	if len(runVMFuzzes) != expectedCrossEmulatorRunVMVersionFuzzCount {
		t.Fatalf("runvm cross-emulator version fuzzer count = %d, want %d:\n%s", len(runVMFuzzes), expectedCrossEmulatorRunVMVersionFuzzCount, strings.Join(runVMFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(runVMFuzzes); got != expectedCrossEmulatorRunVMVersionFuzzHash {
		t.Fatalf("runvm cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorRunVMVersionFuzzHash, strings.Join(runVMFuzzes, "\n"))
	}
}

func crossEmulatorRunVMVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorRunVM") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorTonOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	tonOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorTonOpsVersionFunctionName(name, "Test") {
			tonOpsTests = append(tonOpsTests, name)
		}
	}
	sort.Strings(tonOpsTests)

	tonOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorTonOpsVersionFunctionName(name, "Fuzz") {
			tonOpsFuzzes = append(tonOpsFuzzes, name)
		}
	}
	sort.Strings(tonOpsFuzzes)

	if len(tonOpsTests) != expectedCrossEmulatorTonOpsVersionTestCount {
		t.Fatalf("tonops cross-emulator version test count = %d, want %d:\n%s", len(tonOpsTests), expectedCrossEmulatorTonOpsVersionTestCount, strings.Join(tonOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(tonOpsTests); got != expectedCrossEmulatorTonOpsVersionTestHash {
		t.Fatalf("tonops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorTonOpsVersionTestHash, strings.Join(tonOpsTests, "\n"))
	}
	if len(tonOpsFuzzes) != expectedCrossEmulatorTonOpsVersionFuzzCount {
		t.Fatalf("tonops cross-emulator version fuzzer count = %d, want %d:\n%s", len(tonOpsFuzzes), expectedCrossEmulatorTonOpsVersionFuzzCount, strings.Join(tonOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(tonOpsFuzzes); got != expectedCrossEmulatorTonOpsVersionFuzzHash {
		t.Fatalf("tonops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorTonOpsVersionFuzzHash, strings.Join(tonOpsFuzzes, "\n"))
	}
}

func crossEmulatorTonOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorTonOps") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorDataSize")) &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorSupercontractVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	supercontractTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorSupercontractVersionFunctionName(name, "Test") {
			supercontractTests = append(supercontractTests, name)
		}
	}
	sort.Strings(supercontractTests)

	supercontractFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorSupercontractVersionFunctionName(name, "Fuzz") {
			supercontractFuzzes = append(supercontractFuzzes, name)
		}
	}
	sort.Strings(supercontractFuzzes)

	if len(supercontractTests) != expectedCrossEmulatorSupercontractVersionTestCount {
		t.Fatalf("supercontract cross-emulator version test count = %d, want %d:\n%s", len(supercontractTests), expectedCrossEmulatorSupercontractVersionTestCount, strings.Join(supercontractTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(supercontractTests); got != expectedCrossEmulatorSupercontractVersionTestHash {
		t.Fatalf("supercontract cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorSupercontractVersionTestHash, strings.Join(supercontractTests, "\n"))
	}
	if len(supercontractFuzzes) != expectedCrossEmulatorSupercontractVersionFuzzCount {
		t.Fatalf("supercontract cross-emulator version fuzzer count = %d, want %d:\n%s", len(supercontractFuzzes), expectedCrossEmulatorSupercontractVersionFuzzCount, strings.Join(supercontractFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(supercontractFuzzes); got != expectedCrossEmulatorSupercontractVersionFuzzHash {
		t.Fatalf("supercontract cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorSupercontractVersionFuzzHash, strings.Join(supercontractFuzzes, "\n"))
	}
}

func crossEmulatorSupercontractVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorSupercontract") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorTupleVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	tupleTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorTupleVersionFunctionName(name, "Test") {
			tupleTests = append(tupleTests, name)
		}
	}
	sort.Strings(tupleTests)

	tupleFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorTupleVersionFunctionName(name, "Fuzz") {
			tupleFuzzes = append(tupleFuzzes, name)
		}
	}
	sort.Strings(tupleFuzzes)

	if len(tupleTests) != expectedCrossEmulatorTupleVersionTestCount {
		t.Fatalf("tuple cross-emulator version test count = %d, want %d:\n%s", len(tupleTests), expectedCrossEmulatorTupleVersionTestCount, strings.Join(tupleTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(tupleTests); got != expectedCrossEmulatorTupleVersionTestHash {
		t.Fatalf("tuple cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorTupleVersionTestHash, strings.Join(tupleTests, "\n"))
	}
	if len(tupleFuzzes) != expectedCrossEmulatorTupleVersionFuzzCount {
		t.Fatalf("tuple cross-emulator version fuzzer count = %d, want %d:\n%s", len(tupleFuzzes), expectedCrossEmulatorTupleVersionFuzzCount, strings.Join(tupleFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(tupleFuzzes); got != expectedCrossEmulatorTupleVersionFuzzHash {
		t.Fatalf("tuple cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorTupleVersionFuzzHash, strings.Join(tupleFuzzes, "\n"))
	}
}

func crossEmulatorTupleVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorTuple") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorStackOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	stackOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorStackOpsVersionFunctionName(name, "Test") {
			stackOpsTests = append(stackOpsTests, name)
		}
	}
	sort.Strings(stackOpsTests)

	stackOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorStackOpsVersionFunctionName(name, "Fuzz") {
			stackOpsFuzzes = append(stackOpsFuzzes, name)
		}
	}
	sort.Strings(stackOpsFuzzes)

	if len(stackOpsTests) != expectedCrossEmulatorStackOpsVersionTestCount {
		t.Fatalf("stackops cross-emulator version test count = %d, want %d:\n%s", len(stackOpsTests), expectedCrossEmulatorStackOpsVersionTestCount, strings.Join(stackOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(stackOpsTests); got != expectedCrossEmulatorStackOpsVersionTestHash {
		t.Fatalf("stackops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorStackOpsVersionTestHash, strings.Join(stackOpsTests, "\n"))
	}
	if len(stackOpsFuzzes) != expectedCrossEmulatorStackOpsVersionFuzzCount {
		t.Fatalf("stackops cross-emulator version fuzzer count = %d, want %d:\n%s", len(stackOpsFuzzes), expectedCrossEmulatorStackOpsVersionFuzzCount, strings.Join(stackOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(stackOpsFuzzes); got != expectedCrossEmulatorStackOpsVersionFuzzHash {
		t.Fatalf("stackops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorStackOpsVersionFuzzHash, strings.Join(stackOpsFuzzes, "\n"))
	}
}

func crossEmulatorStackOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorStackOps") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorStackDepth")) &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorDictOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	dictOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorDictOpsVersionFunctionName(name, "Test") {
			dictOpsTests = append(dictOpsTests, name)
		}
	}
	sort.Strings(dictOpsTests)

	dictOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorDictOpsVersionFunctionName(name, "Fuzz") {
			dictOpsFuzzes = append(dictOpsFuzzes, name)
		}
	}
	sort.Strings(dictOpsFuzzes)

	if len(dictOpsTests) != expectedCrossEmulatorDictOpsVersionTestCount {
		t.Fatalf("dictops cross-emulator version test count = %d, want %d:\n%s", len(dictOpsTests), expectedCrossEmulatorDictOpsVersionTestCount, strings.Join(dictOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(dictOpsTests); got != expectedCrossEmulatorDictOpsVersionTestHash {
		t.Fatalf("dictops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorDictOpsVersionTestHash, strings.Join(dictOpsTests, "\n"))
	}
	if len(dictOpsFuzzes) != expectedCrossEmulatorDictOpsVersionFuzzCount {
		t.Fatalf("dictops cross-emulator version fuzzer count = %d, want %d:\n%s", len(dictOpsFuzzes), expectedCrossEmulatorDictOpsVersionFuzzCount, strings.Join(dictOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(dictOpsFuzzes); got != expectedCrossEmulatorDictOpsVersionFuzzHash {
		t.Fatalf("dictops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorDictOpsVersionFuzzHash, strings.Join(dictOpsFuzzes, "\n"))
	}
}

func crossEmulatorDictOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorDictOps") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorArithOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	arithOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorArithOpsVersionFunctionName(name, "Test") {
			arithOpsTests = append(arithOpsTests, name)
		}
	}
	sort.Strings(arithOpsTests)

	arithOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorArithOpsVersionFunctionName(name, "Fuzz") {
			arithOpsFuzzes = append(arithOpsFuzzes, name)
		}
	}
	sort.Strings(arithOpsFuzzes)

	if len(arithOpsTests) != expectedCrossEmulatorArithOpsVersionTestCount {
		t.Fatalf("arithops cross-emulator version test count = %d, want %d:\n%s", len(arithOpsTests), expectedCrossEmulatorArithOpsVersionTestCount, strings.Join(arithOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(arithOpsTests); got != expectedCrossEmulatorArithOpsVersionTestHash {
		t.Fatalf("arithops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorArithOpsVersionTestHash, strings.Join(arithOpsTests, "\n"))
	}
	if len(arithOpsFuzzes) != expectedCrossEmulatorArithOpsVersionFuzzCount {
		t.Fatalf("arithops cross-emulator version fuzzer count = %d, want %d:\n%s", len(arithOpsFuzzes), expectedCrossEmulatorArithOpsVersionFuzzCount, strings.Join(arithOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(arithOpsFuzzes); got != expectedCrossEmulatorArithOpsVersionFuzzHash {
		t.Fatalf("arithops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorArithOpsVersionFuzzHash, strings.Join(arithOpsFuzzes, "\n"))
	}
}

func crossEmulatorArithOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorArithOps") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorContOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	contOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorContOpsVersionFunctionName(name, "Test") {
			contOpsTests = append(contOpsTests, name)
		}
	}
	sort.Strings(contOpsTests)

	contOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorContOpsVersionFunctionName(name, "Fuzz") {
			contOpsFuzzes = append(contOpsFuzzes, name)
		}
	}
	sort.Strings(contOpsFuzzes)

	if len(contOpsTests) != expectedCrossEmulatorContOpsVersionTestCount {
		t.Fatalf("contops cross-emulator version test count = %d, want %d:\n%s", len(contOpsTests), expectedCrossEmulatorContOpsVersionTestCount, strings.Join(contOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(contOpsTests); got != expectedCrossEmulatorContOpsVersionTestHash {
		t.Fatalf("contops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorContOpsVersionTestHash, strings.Join(contOpsTests, "\n"))
	}
	if len(contOpsFuzzes) != expectedCrossEmulatorContOpsVersionFuzzCount {
		t.Fatalf("contops cross-emulator version fuzzer count = %d, want %d:\n%s", len(contOpsFuzzes), expectedCrossEmulatorContOpsVersionFuzzCount, strings.Join(contOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(contOpsFuzzes); got != expectedCrossEmulatorContOpsVersionFuzzHash {
		t.Fatalf("contops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorContOpsVersionFuzzHash, strings.Join(contOpsFuzzes, "\n"))
	}
}

func crossEmulatorContOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorContOps") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorContExecScenarios")) &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorCellOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	cellOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorCellOpsVersionFunctionName(name, "Test") {
			cellOpsTests = append(cellOpsTests, name)
		}
	}
	sort.Strings(cellOpsTests)

	cellOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorCellOpsVersionFunctionName(name, "Fuzz") {
			cellOpsFuzzes = append(cellOpsFuzzes, name)
		}
	}
	sort.Strings(cellOpsFuzzes)

	if len(cellOpsTests) != expectedCrossEmulatorCellOpsVersionTestCount {
		t.Fatalf("cellops cross-emulator version test count = %d, want %d:\n%s", len(cellOpsTests), expectedCrossEmulatorCellOpsVersionTestCount, strings.Join(cellOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(cellOpsTests); got != expectedCrossEmulatorCellOpsVersionTestHash {
		t.Fatalf("cellops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorCellOpsVersionTestHash, strings.Join(cellOpsTests, "\n"))
	}
	if len(cellOpsFuzzes) != expectedCrossEmulatorCellOpsVersionFuzzCount {
		t.Fatalf("cellops cross-emulator version fuzzer count = %d, want %d:\n%s", len(cellOpsFuzzes), expectedCrossEmulatorCellOpsVersionFuzzCount, strings.Join(cellOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(cellOpsFuzzes); got != expectedCrossEmulatorCellOpsVersionFuzzHash {
		t.Fatalf("cellops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorCellOpsVersionFuzzHash, strings.Join(cellOpsFuzzes, "\n"))
	}
}

func crossEmulatorCellOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorAdvancedCellOps") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorCellOpsMatrix") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorFixedSizeBits")) &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorExceptionOpsVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	exceptionOpsTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorExceptionOpsVersionFunctionName(name, "Test") {
			exceptionOpsTests = append(exceptionOpsTests, name)
		}
	}
	sort.Strings(exceptionOpsTests)

	exceptionOpsFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorExceptionOpsVersionFunctionName(name, "Fuzz") {
			exceptionOpsFuzzes = append(exceptionOpsFuzzes, name)
		}
	}
	sort.Strings(exceptionOpsFuzzes)

	if len(exceptionOpsTests) != expectedCrossEmulatorExceptionOpsVersionTestCount {
		t.Fatalf("exceptionops cross-emulator version test count = %d, want %d:\n%s", len(exceptionOpsTests), expectedCrossEmulatorExceptionOpsVersionTestCount, strings.Join(exceptionOpsTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(exceptionOpsTests); got != expectedCrossEmulatorExceptionOpsVersionTestHash {
		t.Fatalf("exceptionops cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorExceptionOpsVersionTestHash, strings.Join(exceptionOpsTests, "\n"))
	}
	if len(exceptionOpsFuzzes) != expectedCrossEmulatorExceptionOpsVersionFuzzCount {
		t.Fatalf("exceptionops cross-emulator version fuzzer count = %d, want %d:\n%s", len(exceptionOpsFuzzes), expectedCrossEmulatorExceptionOpsVersionFuzzCount, strings.Join(exceptionOpsFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(exceptionOpsFuzzes); got != expectedCrossEmulatorExceptionOpsVersionFuzzHash {
		t.Fatalf("exceptionops cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorExceptionOpsVersionFuzzHash, strings.Join(exceptionOpsFuzzes, "\n"))
	}
}

func crossEmulatorExceptionOpsVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorThrowOps") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorCaughtTry")) &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorVMRuntimeVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	vmRuntimeTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorVMRuntimeVersionFunctionName(name, "Test") {
			vmRuntimeTests = append(vmRuntimeTests, name)
		}
	}
	sort.Strings(vmRuntimeTests)

	vmRuntimeFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorVMRuntimeVersionFunctionName(name, "Fuzz") {
			vmRuntimeFuzzes = append(vmRuntimeFuzzes, name)
		}
	}
	sort.Strings(vmRuntimeFuzzes)

	if len(vmRuntimeTests) != expectedCrossEmulatorVMRuntimeVersionTestCount {
		t.Fatalf("vm-runtime cross-emulator version test count = %d, want %d:\n%s", len(vmRuntimeTests), expectedCrossEmulatorVMRuntimeVersionTestCount, strings.Join(vmRuntimeTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(vmRuntimeTests); got != expectedCrossEmulatorVMRuntimeVersionTestHash {
		t.Fatalf("vm-runtime cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorVMRuntimeVersionTestHash, strings.Join(vmRuntimeTests, "\n"))
	}
	if len(vmRuntimeFuzzes) != expectedCrossEmulatorVMRuntimeVersionFuzzCount {
		t.Fatalf("vm-runtime cross-emulator version fuzzer count = %d, want %d:\n%s", len(vmRuntimeFuzzes), expectedCrossEmulatorVMRuntimeVersionFuzzCount, strings.Join(vmRuntimeFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(vmRuntimeFuzzes); got != expectedCrossEmulatorVMRuntimeVersionFuzzHash {
		t.Fatalf("vm-runtime cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorVMRuntimeVersionFuzzHash, strings.Join(vmRuntimeFuzzes, "\n"))
	}
}

func crossEmulatorVMRuntimeVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorMethodHarness") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorGas") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorDecodeFailures") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorUpstreamVMRegressions")) &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorOpcodeGateVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	opcodeGateTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorOpcodeGateVersionFunctionName(name, "Test") {
			opcodeGateTests = append(opcodeGateTests, name)
		}
	}
	sort.Strings(opcodeGateTests)

	opcodeGateFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorOpcodeGateVersionFunctionName(name, "Fuzz") {
			opcodeGateFuzzes = append(opcodeGateFuzzes, name)
		}
	}
	sort.Strings(opcodeGateFuzzes)

	if len(opcodeGateTests) != expectedCrossEmulatorOpcodeGateVersionTestCount {
		t.Fatalf("opcode-gate cross-emulator version test count = %d, want %d:\n%s", len(opcodeGateTests), expectedCrossEmulatorOpcodeGateVersionTestCount, strings.Join(opcodeGateTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(opcodeGateTests); got != expectedCrossEmulatorOpcodeGateVersionTestHash {
		t.Fatalf("opcode-gate cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorOpcodeGateVersionTestHash, strings.Join(opcodeGateTests, "\n"))
	}
	if len(opcodeGateFuzzes) != expectedCrossEmulatorOpcodeGateVersionFuzzCount {
		t.Fatalf("opcode-gate cross-emulator version fuzzer count = %d, want %d:\n%s", len(opcodeGateFuzzes), expectedCrossEmulatorOpcodeGateVersionFuzzCount, strings.Join(opcodeGateFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(opcodeGateFuzzes); got != expectedCrossEmulatorOpcodeGateVersionFuzzHash {
		t.Fatalf("opcode-gate cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorOpcodeGateVersionFuzzHash, strings.Join(opcodeGateFuzzes, "\n"))
	}
}

func crossEmulatorOpcodeGateVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMCrossEmulatorOpcodeMinGlobalVersion") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorRegisteredOpcodeAvailability")
}

func TestTVMCrossEmulatorCoreConfigVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	coreConfigTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorCoreConfigVersionFunctionName(name, "Test") {
			coreConfigTests = append(coreConfigTests, name)
		}
	}
	sort.Strings(coreConfigTests)

	coreConfigFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorCoreConfigVersionFunctionName(name, "Fuzz") {
			coreConfigFuzzes = append(coreConfigFuzzes, name)
		}
	}
	sort.Strings(coreConfigFuzzes)

	if len(coreConfigTests) != expectedCrossEmulatorCoreConfigVersionTestCount {
		t.Fatalf("core-config cross-emulator version test count = %d, want %d:\n%s", len(coreConfigTests), expectedCrossEmulatorCoreConfigVersionTestCount, strings.Join(coreConfigTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(coreConfigTests); got != expectedCrossEmulatorCoreConfigVersionTestHash {
		t.Fatalf("core-config cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorCoreConfigVersionTestHash, strings.Join(coreConfigTests, "\n"))
	}
	if len(coreConfigFuzzes) != expectedCrossEmulatorCoreConfigVersionFuzzCount {
		t.Fatalf("core-config cross-emulator version fuzzer count = %d, want %d:\n%s", len(coreConfigFuzzes), expectedCrossEmulatorCoreConfigVersionFuzzCount, strings.Join(coreConfigFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(coreConfigFuzzes); got != expectedCrossEmulatorCoreConfigVersionFuzzHash {
		t.Fatalf("core-config cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorCoreConfigVersionFuzzHash, strings.Join(coreConfigFuzzes, "\n"))
	}
}

func crossEmulatorCoreConfigVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	if name == prefix+"TVMCrossEmulatorAllGlobalVersionsSmoke" {
		return true
	}
	return (strings.HasPrefix(name, prefix+"TVMCrossEmulatorExecutionConfig") ||
		strings.HasPrefix(name, prefix+"TVMCrossEmulatorLibrary")) &&
		!strings.HasPrefix(name, prefix+"TVMCrossEmulatorGetMethod") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorDifferentialMatrixVersionFunctionInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	differentialTests := make([]string, 0, len(tests))
	for _, name := range tests {
		if crossEmulatorDifferentialMatrixVersionFunctionName(name, "Test") {
			differentialTests = append(differentialTests, name)
		}
	}
	sort.Strings(differentialTests)

	differentialFuzzes := make([]string, 0, len(fuzzes))
	for _, name := range fuzzes {
		if crossEmulatorDifferentialMatrixVersionFunctionName(name, "Fuzz") {
			differentialFuzzes = append(differentialFuzzes, name)
		}
	}
	sort.Strings(differentialFuzzes)

	if len(differentialTests) != expectedCrossEmulatorDifferentialMatrixVersionTestCount {
		t.Fatalf("differential-matrix cross-emulator version test count = %d, want %d:\n%s", len(differentialTests), expectedCrossEmulatorDifferentialMatrixVersionTestCount, strings.Join(differentialTests, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(differentialTests); got != expectedCrossEmulatorDifferentialMatrixVersionTestHash {
		t.Fatalf("differential-matrix cross-emulator version test hash = %s, want %s:\n%s", got, expectedCrossEmulatorDifferentialMatrixVersionTestHash, strings.Join(differentialTests, "\n"))
	}
	if len(differentialFuzzes) != expectedCrossEmulatorDifferentialMatrixVersionFuzzCount {
		t.Fatalf("differential-matrix cross-emulator version fuzzer count = %d, want %d:\n%s", len(differentialFuzzes), expectedCrossEmulatorDifferentialMatrixVersionFuzzCount, strings.Join(differentialFuzzes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(differentialFuzzes); got != expectedCrossEmulatorDifferentialMatrixVersionFuzzHash {
		t.Fatalf("differential-matrix cross-emulator version fuzzer hash = %s, want %s:\n%s", got, expectedCrossEmulatorDifferentialMatrixVersionFuzzHash, strings.Join(differentialFuzzes, "\n"))
	}
}

func crossEmulatorDifferentialMatrixVersionFunctionName(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.HasPrefix(name, prefix+"TVMDifferential") &&
		crossEmulatorVersionFunctionNameClassified(name)
}

func TestTVMCrossEmulatorVersionFunctionsHaveScopedInventory(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	var missing []string
	for _, name := range tests {
		if !crossEmulatorVersionTestNeedsFuzzAnchor(name) {
			continue
		}
		if !crossEmulatorVersionFunctionHasScopedInventory(name, "Test") {
			missing = append(missing, name)
		}
	}
	for _, name := range fuzzes {
		if !crossEmulatorVersionFuzzNeedsSupportedRange(name) {
			continue
		}
		if !crossEmulatorVersionFunctionHasScopedInventory(name, "Fuzz") {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator version functions without scoped inventory:\n%s", strings.Join(missing, "\n"))
	}
}

func crossEmulatorVersionFunctionHasScopedInventory(name, prefix string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return true
	}
	if strings.HasPrefix(name, prefix+"TVMCrossEmulatorTransaction") {
		return true
	}
	return crossEmulatorMessageVersionFunctionName(name, prefix) ||
		crossEmulatorGetMethodVersionFunctionName(name, prefix) ||
		crossEmulatorExecutionProofVersionFunctionName(name, prefix) ||
		crossEmulatorTickTockVersionFunctionName(name, prefix) ||
		crossEmulatorWalletSendVersionFunctionName(name, prefix) ||
		crossEmulatorRunVMVersionFunctionName(name, prefix) ||
		crossEmulatorTonOpsVersionFunctionName(name, prefix) ||
		crossEmulatorSupercontractVersionFunctionName(name, prefix) ||
		crossEmulatorTupleVersionFunctionName(name, prefix) ||
		crossEmulatorStackOpsVersionFunctionName(name, prefix) ||
		crossEmulatorDictOpsVersionFunctionName(name, prefix) ||
		crossEmulatorArithOpsVersionFunctionName(name, prefix) ||
		crossEmulatorContOpsVersionFunctionName(name, prefix) ||
		crossEmulatorCellOpsVersionFunctionName(name, prefix) ||
		crossEmulatorExceptionOpsVersionFunctionName(name, prefix) ||
		crossEmulatorVMRuntimeVersionFunctionName(name, prefix) ||
		crossEmulatorOpcodeGateVersionFunctionName(name, prefix) ||
		crossEmulatorCoreConfigVersionFunctionName(name, prefix) ||
		crossEmulatorDifferentialMatrixVersionFunctionName(name, prefix)
}

func TestVersionFuzzAnchorScannerIncludesNonTVMPrefixes(t *testing.T) {
	tests, fuzzes := crossEmulatorTestFunctionNames(t)

	if !crossEmulatorFunctionNamePresent(tests, "TestExecutionProofCrossEmulatorGlobalVersionBoundary") {
		t.Fatal("cross-emulator version scanner missed non-TVM-prefix execution proof test")
	}
	if !crossEmulatorFunctionNamePresent(fuzzes, "FuzzExecutionProofCrossEmulatorGlobalVersionBoundary") {
		t.Fatal("cross-emulator version scanner missed non-TVM-prefix execution proof fuzz")
	}
}

func TestTVMCrossEmulatorExplicitConfigHelperAudit(t *testing.T) {
	const body = `
		func fixture(t testingT, base *cell.Cell) {
			_ = referenceTransactionConfigRootWithGlobalVersionAndCapabilities(t, base, 12, 4)
		}
	`

	if !crossEmulatorFunctionBodyUsesExplicitVersion(body) {
		t.Fatal("cross-emulator explicit-version scanner missed transaction config helper with capabilities")
	}
}

func TestTVMCrossEmulatorFuzzersHaveBaselineSeeds(t *testing.T) {
	fuzzers := crossEmulatorFuzzFunctionBodies(t)
	seeds := crossEmulatorFuzzSupportedRangeSeedCoverage(t)

	var missing []string
	for _, fuzz := range fuzzers {
		if !seeds[fuzz.path+":"+fuzz.name].baseline {
			missing = append(missing, fuzz.path+":"+fuzz.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator fuzzers without baseline f.Add seeds:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorVersionFuzzersUseSupportedRangeSeeds(t *testing.T) {
	fuzzers := crossEmulatorFuzzFunctionBodies(t)
	seeds := crossEmulatorFuzzSupportedRangeSeedCoverage(t)

	var missing []string
	for _, fuzz := range fuzzers {
		if !crossEmulatorVersionFuzzNeedsSupportedRange(fuzz.name) {
			continue
		}
		if !crossEmulatorFuzzBodyUsesVersionNormalizer(fuzz.body) {
			missing = append(missing, fuzz.path+":"+fuzz.name+" missing supported-version normalizer")
		}
		seed := seeds[fuzz.path+":"+fuzz.name]
		if !seed.min || !seed.max {
			missing = append(missing, fuzz.path+":"+fuzz.name+" missing supported-version boundary f.Add seeds")
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator version fuzzers missing supported-range wiring:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorVersionFuzzersSeedFullSupportedRange(t *testing.T) {
	fuzzers := crossEmulatorFuzzFunctionBodies(t)
	seeds := crossEmulatorFuzzSupportedRangeSeedCoverage(t)

	required := make([]string, 0, len(fuzzers))
	var missing []string
	for _, fuzz := range fuzzers {
		if !crossEmulatorVersionFuzzNeedsFullSupportedRangeSeedLoop(fuzz.name) {
			continue
		}
		required = append(required, fuzz.name)
		if !seeds[fuzz.path+":"+fuzz.name].fullRange {
			missing = append(missing, fuzz.path+":"+fuzz.name)
		}
	}
	sort.Strings(required)

	if len(required) != expectedCrossEmulatorFullRangeVersionFuzzerCount {
		t.Fatalf("cross-emulator full-range version fuzzer count = %d, want %d", len(required), expectedCrossEmulatorFullRangeVersionFuzzerCount)
	}
	if got := crossEmulatorVersionNameInventoryHash(required); got != expectedCrossEmulatorFullRangeVersionFuzzerHash {
		t.Fatalf("cross-emulator full-range version fuzzer hash = %s, want %s", got, expectedCrossEmulatorFullRangeVersionFuzzerHash)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator version fuzzers without full supported-range f.Add seed loops:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorFuzzSeedScannerCoversFullRangeHelpers(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "opcode_version_cross_emulator_test.go", `
	package tvm

	func FuzzCrossEmulatorDifferentialHelperFixture(f *testing.F) {
		for _, seed := range differentialFuzzVersionMatrixProgramSeeds() {
			f.Add(int64(seed.version), uint8(seed.family), uint16(seed.program))
		}
	}

	func FuzzCrossEmulatorRegisteredHelperFixture(f *testing.F) {
		cases := opcodeMinGlobalVersionBoundaryCases()
		for _, seed := range registeredOpcodeAvailabilityFuzzSeeds(cases) {
			f.Add(uint16(seed.caseIdx), uint8(seed.version))
		}
	}

	func FuzzCrossEmulatorRepresentativeHelperFixture(f *testing.F) {
		cases := opcodeMinGlobalVersionBoundaryCases()
		for _, seed := range opcodeMinGlobalVersionRepresentativeFuzzSeeds(cases) {
			f.Add(uint16(seed.caseIdx), uint8(seed.version))
		}
	}

	func FuzzCrossEmulatorMappedArgFixture(f *testing.F) {
		f.Add(uint8(MinSupportedGlobalVersion), uint8(99))
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(99))
		f.Add(uint8(99), uint8(MinSupportedGlobalVersion))
		f.Add(uint8(99), uint8(MaxSupportedGlobalVersion))

		f.Fuzz(func(t *testing.T, rawVersion uint8, payload uint8) {
			_ = tvmFuzzGlobalVersionByte(rawVersion)
		})
	}

	func FuzzCrossEmulatorMappedArgNegativeFixture(f *testing.F) {
		f.Add(uint8(MinSupportedGlobalVersion), uint8(99))
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(99))

		f.Fuzz(func(t *testing.T, payload uint8, rawVersion uint8) {
			_ = tvmFuzzGlobalVersionByte(rawVersion)
		})
	}
`, 0)
	if err != nil {
		t.Fatalf("parse cross-emulator helper seed fixture: %v", err)
	}

	checked := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		baseline, min, max, fullRange := crossEmulatorFuzzFunctionSeedsSupportedRange(fn)
		if fn.Name.Name == "FuzzCrossEmulatorMappedArgNegativeFixture" {
			if !baseline || min || max || fullRange {
				t.Fatalf("%s cross-emulator seed scanner coverage = baseline:%t min:%t max:%t full:%t, want baseline only", fn.Name.Name, baseline, min, max, fullRange)
			}
			checked++
			continue
		}
		if fn.Name.Name == "FuzzCrossEmulatorMappedArgFixture" {
			if !baseline || !min || !max || fullRange {
				t.Fatalf("%s cross-emulator seed scanner coverage = baseline:%t min:%t max:%t full:%t, want baseline min/max only", fn.Name.Name, baseline, min, max, fullRange)
			}
			checked++
			continue
		}
		if !baseline || !min || !max || !fullRange {
			t.Fatalf("%s cross-emulator helper seed scanner coverage = baseline:%t min:%t max:%t full:%t, want all true", fn.Name.Name, baseline, min, max, fullRange)
		}
		checked++
	}
	if checked != 5 {
		t.Fatalf("cross-emulator helper seed scanner fixture checked %d functions, want 5", checked)
	}
}

func TestTVMCrossEmulatorRuntimeGateAnchorsAreVersionAudited(t *testing.T) {
	fuzzers := crossEmulatorFuzzFunctionBodies(t)
	seeds := crossEmulatorFuzzSupportedRangeSeedCoverage(t)
	byName := make(map[string]crossEmulatorFunctionSource, len(fuzzers))
	for _, fuzz := range fuzzers {
		if prev, ok := byName[fuzz.name]; ok {
			t.Fatalf("duplicate cross-emulator fuzzer name %s in %s and %s", fuzz.name, prev.path, fuzz.path)
		}
		byName[fuzz.name] = fuzz
	}

	var audited []string
	var missing []string
	for file, anchors := range expectedRuntimeGlobalVersionGateCrossEmulatorAnchors {
		for _, anchor := range anchors {
			fuzz, ok := byName[anchor]
			if !ok {
				missing = append(missing, file+":"+anchor+" missing cross-emulator fuzz body")
				continue
			}
			audited = append(audited, anchor)
			if !crossEmulatorVersionFuzzNeedsSupportedRange(anchor) {
				missing = append(missing, file+":"+anchor+" is not classified by cross-emulator version audit")
				continue
			}
			if !crossEmulatorFuzzBodyUsesVersionNormalizer(fuzz.body) {
				missing = append(missing, file+":"+anchor+" missing supported-version normalizer")
			}

			seed := seeds[fuzz.path+":"+fuzz.name]
			if !seed.min || !seed.max {
				missing = append(missing, file+":"+anchor+" missing supported-version boundary f.Add seeds")
			}
			if crossEmulatorVersionFuzzNeedsFullSupportedRangeSeedLoop(anchor) && !seed.fullRange {
				missing = append(missing, file+":"+anchor+" missing full supported-version f.Add seed loop")
			}
		}
	}

	if len(audited) == 0 {
		t.Fatal("runtime global-version gate cross-emulator anchor audit found no anchors")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("runtime global-version gate cross-emulator anchors missing version audit coverage:\n%s", strings.Join(missing, "\n"))
	}
}

type crossEmulatorFuzzSupportedRangeSeeds struct {
	baseline  bool
	min       bool
	max       bool
	fullRange bool
}

func crossEmulatorFuzzSupportedRangeSeedCoverage(t *testing.T) map[string]crossEmulatorFuzzSupportedRangeSeeds {
	t.Helper()

	coverage := make(map[string]crossEmulatorFuzzSupportedRangeSeeds)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		crossEmulatorFile := strings.HasSuffix(filepath.Base(path), "cross_emulator_test.go")
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Fuzz") ||
				(!strings.Contains(fn.Name.Name, "CrossEmulator") && !crossEmulatorFile) {
				continue
			}

			baseline, min, max, fullRange := crossEmulatorFuzzFunctionSeedsSupportedRange(fn)
			coverage[filepath.ToSlash(path)+":"+fn.Name.Name] = crossEmulatorFuzzSupportedRangeSeeds{
				baseline:  baseline,
				min:       min,
				max:       max,
				fullRange: fullRange,
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator fuzz seed coverage: %v", err)
	}
	if len(coverage) == 0 {
		t.Fatal("cross-emulator fuzz seed coverage scan found no fuzzers")
	}
	return coverage
}

func crossEmulatorFuzzFunctionSeedsSupportedRange(fn *ast.FuncDecl) (baseline, min, max, fullRange bool) {
	versionSeedArgIndexes := crossEmulatorFuzzVersionSeedArgIndexes(fn)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			switch astCallName(node.Fun) {
			case "differentialFuzzVersionMatrixProgramSeeds",
				"registeredOpcodeAvailabilityFuzzSeeds",
				"opcodeMinGlobalVersionRepresentativeFuzzSeeds":
				min = true
				max = true
				fullRange = true
			}
			if !crossEmulatorFuzzAddCall(node.Fun) {
				return true
			}
			baseline = true
			for idx, arg := range node.Args {
				if len(versionSeedArgIndexes) > 0 {
					if _, ok := versionSeedArgIndexes[idx]; !ok {
						continue
					}
				}
				if astExprContainsIdent(arg, "MinSupportedGlobalVersion") {
					min = true
				}
				if astExprContainsIdent(arg, "MaxSupportedGlobalVersion") {
					max = true
				}
			}
		case *ast.ForStmt:
			versionVar := crossEmulatorSupportedRangeForLoopVar(node)
			if versionVar == "" || !crossEmulatorForBodyAddsFuzzVersionSeed(node.Body, versionVar) {
				return true
			}
			min = true
			max = true
			fullRange = true
		}
		return true
	})
	return baseline, min, max, fullRange
}

func crossEmulatorFuzzVersionSeedArgIndexes(fn *ast.FuncDecl) map[int]struct{} {
	paramIndexes := make(map[string]int)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !versionFuzzFuzzFuncCall(call) || len(call.Args) != 1 {
			return true
		}
		fnLit, ok := call.Args[0].(*ast.FuncLit)
		if !ok || fnLit.Type.Params == nil {
			return true
		}
		idx := 0
		for _, field := range fnLit.Type.Params.List {
			for _, name := range field.Names {
				if idx > 0 {
					paramIndexes[name.Name] = idx - 1
				}
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
		return false
	})

	indexes := make(map[int]struct{})
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !crossEmulatorVersionNormalizerCall(call.Fun) || len(call.Args) == 0 {
			return true
		}
		for _, name := range versionFuzzIdentNames(call.Args[0]) {
			if idx, ok := paramIndexes[name]; ok {
				indexes[idx] = struct{}{}
			}
		}
		return true
	})
	return indexes
}

func crossEmulatorVersionNormalizerCall(expr ast.Expr) bool {
	if versionFuzzSupportedRangeMapperCall(expr) {
		return true
	}
	return astCallName(expr) == "differentialFuzzVersionMatrixProgramCase"
}

func crossEmulatorSupportedRangeForLoopVar(stmt *ast.ForStmt) string {
	init, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || len(init.Lhs) != 1 || len(init.Rhs) != 1 || init.Tok != token.DEFINE {
		return ""
	}
	ident, ok := init.Lhs[0].(*ast.Ident)
	if !ok || !astExprContainsIdent(init.Rhs[0], "MinSupportedGlobalVersion") {
		return ""
	}

	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LEQ || !astExprContainsIdent(cond.X, ident.Name) || !astExprContainsIdent(cond.Y, "MaxSupportedGlobalVersion") {
		return ""
	}

	post, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !astExprContainsIdent(post.X, ident.Name) {
		return ""
	}
	return ident.Name
}

func crossEmulatorForBodyAddsFuzzVersionSeed(body *ast.BlockStmt, versionVar string) bool {
	addsVersion := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !crossEmulatorFuzzAddCall(call.Fun) {
			return true
		}
		for _, arg := range call.Args {
			if astExprContainsIdent(arg, versionVar) {
				addsVersion = true
				return false
			}
		}
		return true
	})
	return addsVersion
}

func crossEmulatorFuzzAddCall(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "f"
}

func astExprContainsIdent(expr ast.Expr, name string) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

func astCallName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return expr.Sel.Name
	default:
		return ""
	}
}

func TestTVMCrossEmulatorVersionAuditAllVersionTestsUseSupportedRangeSelection(t *testing.T) {
	tests := crossEmulatorTestFunctionBodies(t)

	var missing []string
	for _, test := range tests {
		if !crossEmulatorAllVersionTestNeedsSupportedRangeSelection(test.name) {
			continue
		}
		if !crossEmulatorTestBodyUsesSupportedVersionSelection(test.body) {
			missing = append(missing, test.path+":"+test.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator all-version tests without supported-version selection:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorVersionAuditVersionNamedTestsUseSupportedRangeSelection(t *testing.T) {
	tests := crossEmulatorTestFunctionBodies(t)

	var missing []string
	for _, test := range tests {
		if !crossEmulatorVersionNamedTestNeedsSupportedRangeSelection(test.name) {
			continue
		}
		if !crossEmulatorTestBodyUsesSupportedVersionSelection(test.body) {
			missing = append(missing, test.path+":"+test.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator version-named tests without supported-version selection:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorVersionNamedTestsUseShardedSelection(t *testing.T) {
	tests := crossEmulatorTestFunctionBodies(t)

	var missing []string
	for _, test := range tests {
		if !crossEmulatorVersionNamedTestNeedsSupportedRangeSelection(test.name) {
			continue
		}
		if !crossEmulatorTestBodyUsesShardedVersionSelection(test.body) {
			missing = append(missing, test.path+":"+test.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("cross-emulator version-named tests without sharded supported-version selection:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMCrossEmulatorExplicitVersionUsersAreNamedOrInventoried(t *testing.T) {
	functions := append(crossEmulatorTestFunctionBodies(t), crossEmulatorFuzzFunctionBodies(t)...)
	functionNames := make(map[string]string, len(functions))
	for _, fn := range functions {
		if prev, ok := functionNames[fn.name]; ok {
			t.Fatalf("duplicate cross-emulator function name %s in %s and %s", fn.name, prev, fn.path)
		}
		functionNames[fn.name] = fn.path + ":" + fn.name
	}
	fuzzSeeds := crossEmulatorFuzzSupportedRangeSeedCoverage(t)

	var unclassified []string
	for _, fn := range functions {
		if crossEmulatorVersionAuditOnlyTest(fn.name) ||
			crossEmulatorVersionFunctionNameClassified(fn.name) ||
			!crossEmulatorFunctionBodyUsesExplicitVersion(fn.body) {
			continue
		}
		unclassified = append(unclassified, fn.path+":"+fn.name)
	}
	sort.Strings(unclassified)

	if len(unclassified) != expectedCrossEmulatorUnclassifiedExplicitVersionUserCount {
		t.Fatalf("unclassified cross-emulator explicit-version user count = %d, want %d:\n%s", len(unclassified), expectedCrossEmulatorUnclassifiedExplicitVersionUserCount, strings.Join(unclassified, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(unclassified); got != expectedCrossEmulatorUnclassifiedExplicitVersionUserHash {
		t.Fatalf("unclassified cross-emulator explicit-version user hash = %s, want %s:\n%s", got, expectedCrossEmulatorUnclassifiedExplicitVersionUserHash, strings.Join(unclassified, "\n"))
	}

	seen := make(map[string]struct{}, len(unclassified))
	for _, name := range unclassified {
		seen[name] = struct{}{}
		entry, ok := expectedCrossEmulatorUnclassifiedExplicitVersionUsers[name]
		if !ok {
			t.Fatalf("unclassified cross-emulator explicit-version user %s has no allowlist reason", name)
		}
		if entry.reason == "" {
			t.Fatalf("unclassified cross-emulator explicit-version user %s has empty allowlist reason", name)
		}
		if len(entry.versionAnchors) == 0 {
			t.Fatalf("unclassified cross-emulator explicit-version user %s has no dedicated version anchors", name)
		}
		for _, anchor := range entry.versionAnchors {
			key, ok := functionNames[anchor]
			if !ok {
				t.Fatalf("unclassified cross-emulator explicit-version user %s lost dedicated version anchor %s", name, anchor)
			}
			if !crossEmulatorVersionFunctionNameClassified(anchor) {
				t.Fatalf("dedicated version anchor %s for %s is not version-classified", anchor, name)
			}
			if strings.HasPrefix(anchor, "Fuzz") && !fuzzSeeds[key].fullRange {
				t.Fatalf("dedicated version fuzzer anchor %s for %s lacks full supported-range seeds", anchor, name)
			}
		}
	}
	for name := range expectedCrossEmulatorUnclassifiedExplicitVersionUsers {
		if _, ok := seen[name]; !ok {
			t.Fatalf("allowlist tracks obsolete unclassified explicit-version user %s", name)
		}
	}
}

func TestTVMCrossEmulatorVersionAuditPrefixesAreWellFormed(t *testing.T) {
	uses := crossEmulatorVersionAuditPrefixUsesFromSource(t)

	var malformed []string
	for _, use := range uses {
		if !crossEmulatorVersionAuditPrefixWellFormed(use.prefix) {
			malformed = append(malformed, use.String())
		}
	}

	if len(malformed) > 0 {
		sort.Strings(malformed)
		t.Fatalf("malformed cross-emulator version audit prefixes:\n%s", strings.Join(malformed, "\n"))
	}
}

func TestTVMCrossEmulatorVersionAuditPrefixUseInventory(t *testing.T) {
	inventory := crossEmulatorVersionAuditPrefixUseInventory(t)
	if len(inventory) != expectedCrossEmulatorVersionAuditPrefixUseCount {
		t.Fatalf("cross-emulator version audit prefix use count = %d, want %d:\n%s", len(inventory), expectedCrossEmulatorVersionAuditPrefixUseCount, strings.Join(inventory, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(inventory); got != expectedCrossEmulatorVersionAuditPrefixUseHash {
		t.Fatalf("cross-emulator version audit prefix use hash = %s, want %s:\n%s", got, expectedCrossEmulatorVersionAuditPrefixUseHash, strings.Join(inventory, "\n"))
	}
}

func TestTVMCrossEmulatorVersionSelectionHelpersUseAuditVersions(t *testing.T) {
	helpers := crossEmulatorVersionSelectionHelpers(t)
	if len(helpers) != expectedCrossEmulatorVersionSelectionHelperCount {
		t.Fatalf("cross-emulator version selection helper count = %d, want %d:\n%s", len(helpers), expectedCrossEmulatorVersionSelectionHelperCount, strings.Join(helpers, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(helpers); got != expectedCrossEmulatorVersionSelectionHelperHash {
		t.Fatalf("cross-emulator version selection helper hash = %s, want %s:\n%s", got, expectedCrossEmulatorVersionSelectionHelperHash, strings.Join(helpers, "\n"))
	}

	helperNames := make(map[string]struct{}, len(helpers))
	for _, helper := range helpers {
		parts := strings.Split(helper, ":")
		if len(parts) < 2 {
			t.Fatalf("bad cross-emulator version selection helper inventory entry %q", helper)
		}
		helperNames[parts[1]] = struct{}{}
	}

	for _, use := range crossEmulatorVersionSelectionHelperCallUses(t) {
		if _, ok := helperNames[use.helper]; !ok {
			t.Fatalf("cross-emulator version selection helper call %s uses helper without audit-version wrapper inventory", use.String())
		}
	}
}

func TestTVMCrossEmulatorVersionSelectionHelperShapes(t *testing.T) {
	shapes := crossEmulatorVersionSelectionHelperShapeInventory(t)
	if len(shapes) != expectedCrossEmulatorVersionSelectionHelperShapeCount {
		t.Fatalf("cross-emulator version selection helper shape count = %d, want %d:\n%s", len(shapes), expectedCrossEmulatorVersionSelectionHelperShapeCount, strings.Join(shapes, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(shapes); got != expectedCrossEmulatorVersionSelectionHelperShapeHash {
		t.Fatalf("cross-emulator version selection helper shape hash = %s, want %s:\n%s", got, expectedCrossEmulatorVersionSelectionHelperShapeHash, strings.Join(shapes, "\n"))
	}
}

func TestTVMCrossEmulatorVersionSelectionHelpersHaveShardTests(t *testing.T) {
	shards := crossEmulatorVersionSelectionHelperShardInventory(t)
	if len(shards) != expectedCrossEmulatorVersionSelectionHelperShardCount {
		t.Fatalf("cross-emulator version selection helper shard-test count = %d, want %d:\n%s", len(shards), expectedCrossEmulatorVersionSelectionHelperShardCount, strings.Join(shards, "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(shards); got != expectedCrossEmulatorVersionSelectionHelperShardHash {
		t.Fatalf("cross-emulator version selection helper shard-test hash = %s, want %s:\n%s", got, expectedCrossEmulatorVersionSelectionHelperShardHash, strings.Join(shards, "\n"))
	}
}

func crossEmulatorTestFunctionNames(t *testing.T) (tests, fuzzes []string) {
	t.Helper()

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		crossEmulatorFile := strings.HasSuffix(filepath.Base(path), "cross_emulator_test.go")
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			switch {
			case strings.HasPrefix(name, "Test") && (strings.Contains(name, "CrossEmulator") || crossEmulatorFile):
				tests = append(tests, name)
			case strings.HasPrefix(name, "Fuzz") && (strings.Contains(name, "CrossEmulator") || crossEmulatorFile):
				fuzzes = append(fuzzes, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator test functions: %v", err)
	}
	if len(tests) == 0 || len(fuzzes) == 0 {
		t.Fatalf("cross-emulator test function scan found tests=%d fuzzes=%d", len(tests), len(fuzzes))
	}
	return tests, fuzzes
}

func crossEmulatorVersionNameInventoryHash(names []string) string {
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func crossEmulatorReferenceMismatchUseInventory(t *testing.T) []string {
	t.Helper()

	var uses []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || knownReferenceMismatchCoverageAuditFile(path) || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.Contains(value, knownReferenceMismatchPrefix) {
					return true
				}
				uses = append(uses, filepath.ToSlash(path)+":"+fn.Name.Name+":"+value)
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan known reference mismatch use inventory: %v", err)
	}
	if len(uses) == 0 {
		t.Fatal("known reference mismatch use inventory is empty")
	}
	sort.Strings(uses)
	return uses
}

func crossEmulatorDirectKnownReferenceSkipInventory(t *testing.T) []string {
	t.Helper()

	var uses []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || knownReferenceMismatchCoverageAuditFile(path) || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !crossEmulatorDirectSkipCall(call) {
					return true
				}
				reason := crossEmulatorCallKnownReferenceReason(call)
				if reason == "" {
					return true
				}
				pos := fset.Position(call.Pos())
				uses = append(uses, fmt.Sprintf("%s:%s:%d:%s", filepath.ToSlash(path), fn.Name.Name, pos.Line, reason))
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan direct known-reference skips: %v", err)
	}
	if len(uses) == 0 {
		t.Fatal("direct known-reference skip inventory is empty")
	}
	sort.Strings(uses)
	return uses
}

func crossEmulatorDirectSkipCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Skip" || sel.Sel.Name == "Skipf"
}

func crossEmulatorCallKnownReferenceReason(call *ast.CallExpr) string {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil || !strings.Contains(value, knownReferenceMismatchPrefix) {
			continue
		}
		return value
	}
	return ""
}

func crossEmulatorSkipReferenceUseInventory(t *testing.T) []string {
	t.Helper()

	var uses []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				skipRef := crossEmulatorCompositeLiteralField(lit, "skipReference")
				if skipRef == nil {
					return true
				}
				name := crossEmulatorCompositeLiteralNameField(lit, "name")
				if name == "" {
					name = "<unnamed>"
				}
				uses = append(uses, filepath.ToSlash(path)+":"+fn.Name.Name+":"+name+":"+crossEmulatorSkipReferenceExprName(skipRef))
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan skipReference use inventory: %v", err)
	}
	if len(uses) == 0 {
		t.Fatal("skipReference use inventory is empty")
	}
	sort.Strings(uses)
	return uses
}

func crossEmulatorSkipReferenceUseHasKnownReason(use string) bool {
	idx := strings.LastIndex(use, ":")
	if idx < 0 {
		return false
	}
	value := use[idx+1:]
	return strings.HasPrefix(value, knownReferenceMismatchPrefix) ||
		crossEmulatorKnownReferenceSkipReferenceExpressions[value]
}

var crossEmulatorKnownReferenceSkipReferenceExpressions = map[string]bool{
	"contOpsDuplicateSaveReferenceSkip":              true,
	"ecrecoverEthereumReferenceSkip(14)":             true,
	"ecrecoverEthereumReferenceSkip(version)":        true,
	"ed25519ChksigRejectedKeyReferenceSkip(14)":      true,
	"ed25519ChksigRejectedKeyReferenceSkip(version)": true,
	"qrshiftCodeSkipReference":                       true,
	"rshiftCodeSkipReference":                        true,
	"sendMsgUserFwdFeeReferenceSkip(version)":        true,
}

func crossEmulatorCompositeLiteralField(lit *ast.CompositeLit, key string) ast.Expr {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if ok && ident.Name == key {
			return kv.Value
		}
	}
	return nil
}

func crossEmulatorCompositeLiteralNameField(lit *ast.CompositeLit, key string) string {
	expr := crossEmulatorCompositeLiteralField(lit, key)
	value, ok := expr.(*ast.BasicLit)
	if !ok || value.Kind != token.STRING {
		return crossEmulatorSkipReferenceExprName(expr)
	}
	text, err := strconv.Unquote(value.Value)
	if err != nil {
		return ""
	}
	return text
}

func crossEmulatorSkipReferenceExprName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			text, err := strconv.Unquote(expr.Value)
			if err == nil {
				return text
			}
		}
		return expr.Value
	case *ast.CallExpr:
		args := make([]string, 0, len(expr.Args))
		for _, arg := range expr.Args {
			args = append(args, crossEmulatorSkipReferenceExprName(arg))
		}
		return astCallName(expr.Fun) + "(" + strings.Join(args, ",") + ")"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

type crossEmulatorVersionAuditPrefixUse struct {
	path     string
	function string
	line     int
	prefix   string
}

func (u crossEmulatorVersionAuditPrefixUse) String() string {
	return u.path + ":" + u.function + ":" + strconv.Itoa(u.line) + ":" + u.prefix
}

func crossEmulatorVersionAuditPrefixUsesFromSource(t *testing.T) []crossEmulatorVersionAuditPrefixUse {
	t.Helper()

	var uses []crossEmulatorVersionAuditPrefixUse
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !crossEmulatorVersionAuditCall(call.Fun) {
					return true
				}
				if len(call.Args) != 2 {
					pos := fset.Position(call.Pos())
					t.Fatalf("%s:%d crossEmulatorVersionAuditVersions arg count = %d, want 2", filepath.ToSlash(path), pos.Line, len(call.Args))
				}

				lit, ok := call.Args[1].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					pos := fset.Position(call.Args[1].Pos())
					t.Fatalf("%s:%d crossEmulatorVersionAuditVersions prefix must be a string literal", filepath.ToSlash(path), pos.Line)
				}
				prefix, err := strconv.Unquote(lit.Value)
				if err != nil {
					pos := fset.Position(lit.Pos())
					t.Fatalf("%s:%d unquote cross-emulator version audit prefix: %v", filepath.ToSlash(path), pos.Line, err)
				}
				pos := fset.Position(lit.Pos())
				uses = append(uses, crossEmulatorVersionAuditPrefixUse{
					path:     filepath.ToSlash(path),
					function: fn.Name.Name,
					line:     pos.Line,
					prefix:   prefix,
				})
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator version audit prefixes: %v", err)
	}
	if len(uses) == 0 {
		t.Fatal("cross-emulator version audit prefix scan found no uses")
	}
	return uses
}

func crossEmulatorVersionAuditPrefixUseInventory(t *testing.T) []string {
	t.Helper()

	uses := crossEmulatorVersionAuditPrefixUsesFromSource(t)
	inventory := make([]string, 0, len(uses))
	seen := make(map[string]struct{}, len(uses))
	for _, use := range uses {
		key := use.path + ":" + use.function + ":" + use.prefix
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate cross-emulator version audit prefix use %s", key)
		}
		seen[key] = struct{}{}
		inventory = append(inventory, key)
	}
	sort.Strings(inventory)
	return inventory
}

func crossEmulatorVersionSelectionHelpers(t *testing.T) []string {
	t.Helper()

	var helpers []string
	for _, use := range crossEmulatorVersionAuditPrefixUsesFromSource(t) {
		if !strings.HasSuffix(use.function, "CrossEmulatorVersions") {
			continue
		}
		helpers = append(helpers, use.path+":"+use.function+":"+use.prefix)
	}
	if len(helpers) == 0 {
		t.Fatal("cross-emulator version selection helper inventory is empty")
	}
	sort.Strings(helpers)
	return helpers
}

type crossEmulatorVersionSelectionHelperDecl struct {
	path   string
	prefix string
	fn     *ast.FuncDecl
}

func crossEmulatorVersionSelectionHelperDecls(t *testing.T) []crossEmulatorVersionSelectionHelperDecl {
	t.Helper()

	var helpers []crossEmulatorVersionSelectionHelperDecl
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasSuffix(fn.Name.Name, "CrossEmulatorVersions") {
				continue
			}
			helpers = append(helpers, crossEmulatorVersionSelectionHelperDecl{
				path:   filepath.ToSlash(path),
				prefix: crossEmulatorVersionSelectionHelperAuditPrefix(t, filepath.ToSlash(path), fn),
				fn:     fn,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator version selection helpers: %v", err)
	}
	if len(helpers) == 0 {
		t.Fatal("cross-emulator version selection helper scan found no helpers")
	}
	sort.Slice(helpers, func(i, j int) bool {
		return helpers[i].path+":"+helpers[i].fn.Name.Name < helpers[j].path+":"+helpers[j].fn.Name.Name
	})
	return helpers
}

func crossEmulatorVersionSelectionHelperShapeInventory(t *testing.T) []string {
	t.Helper()

	helpers := crossEmulatorVersionSelectionHelperDecls(t)
	items := make([]string, 0, len(helpers))
	for _, helper := range helpers {
		shape := crossEmulatorVersionSelectionHelperShape(t, helper)
		items = append(items, helper.path+":"+helper.fn.Name.Name+":"+helper.prefix+":"+shape)
	}
	sort.Strings(items)
	return items
}

func crossEmulatorVersionSelectionHelperShape(t *testing.T, helper crossEmulatorVersionSelectionHelperDecl) string {
	t.Helper()

	if crossEmulatorVersionSelectionHelperDirectReturn(helper.fn) {
		return "direct"
	}
	if helper.fn.Name.Name == "transactionVersionCrossEmulatorVersions" && crossEmulatorVersionSelectionHelperUint32Conversion(helper.fn) {
		return "uint32-conversion"
	}
	t.Fatalf("cross-emulator version selection helper %s:%s has unsupported body shape", helper.path, helper.fn.Name.Name)
	return ""
}

func crossEmulatorVersionSelectionHelperDirectReturn(fn *ast.FuncDecl) bool {
	stmts := crossEmulatorVersionSelectionHelperStatements(fn)
	if len(stmts) != 1 {
		return false
	}
	ret, ok := stmts[0].(*ast.ReturnStmt)
	return ok && len(ret.Results) == 1 && crossEmulatorVersionAuditCallExpr(ret.Results[0])
}

func crossEmulatorVersionSelectionHelperUint32Conversion(fn *ast.FuncDecl) bool {
	stmts := crossEmulatorVersionSelectionHelperStatements(fn)
	if len(stmts) != 4 {
		return false
	}

	versions, ok := crossEmulatorAssignAuditResult(stmts[0])
	if !ok {
		return false
	}
	out, ok := crossEmulatorAssignUint32Slice(stmts[1])
	if !ok {
		return false
	}
	versionVar, ok := crossEmulatorRangeOverIdent(stmts[2], versions)
	if !ok {
		return false
	}
	rangeStmt := stmts[2].(*ast.RangeStmt)
	if len(rangeStmt.Body.List) != 1 || !crossEmulatorAppendUint32Cast(rangeStmt.Body.List[0], out, versionVar) {
		return false
	}
	return crossEmulatorReturnIdent(stmts[3], out)
}

func crossEmulatorVersionSelectionHelperStatements(fn *ast.FuncDecl) []ast.Stmt {
	stmts := make([]ast.Stmt, 0, len(fn.Body.List))
	for _, stmt := range fn.Body.List {
		if crossEmulatorTHelperStmt(stmt) {
			continue
		}
		stmts = append(stmts, stmt)
	}
	return stmts
}

func crossEmulatorTHelperStmt(stmt ast.Stmt) bool {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Helper" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "t"
}

func crossEmulatorAssignAuditResult(stmt ast.Stmt) (string, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return "", false
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || !crossEmulatorVersionAuditCallExpr(assign.Rhs[0]) {
		return "", false
	}
	return ident.Name, true
}

func crossEmulatorAssignUint32Slice(stmt ast.Stmt) (string, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return "", false
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || astCallName(call.Fun) != "make" || len(call.Args) != 3 {
		return "", false
	}
	arr, ok := call.Args[0].(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return "", false
	}
	elt, ok := arr.Elt.(*ast.Ident)
	if !ok || elt.Name != "uint32" {
		return "", false
	}
	return ident.Name, true
}

func crossEmulatorRangeOverIdent(stmt ast.Stmt, collection string) (string, bool) {
	rangeStmt, ok := stmt.(*ast.RangeStmt)
	if !ok {
		return "", false
	}
	collectionIdent, ok := rangeStmt.X.(*ast.Ident)
	if !ok || collectionIdent.Name != collection {
		return "", false
	}
	valueIdent, ok := rangeStmt.Value.(*ast.Ident)
	if !ok || valueIdent.Name == "_" {
		return "", false
	}
	return valueIdent.Name, true
}

func crossEmulatorAppendUint32Cast(stmt ast.Stmt, out, versionVar string) bool {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	left, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || left.Name != out {
		return false
	}
	appendCall, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || astCallName(appendCall.Fun) != "append" || len(appendCall.Args) != 2 {
		return false
	}
	appendOut, ok := appendCall.Args[0].(*ast.Ident)
	if !ok || appendOut.Name != out {
		return false
	}
	cast, ok := appendCall.Args[1].(*ast.CallExpr)
	if !ok || astCallName(cast.Fun) != "uint32" || len(cast.Args) != 1 {
		return false
	}
	arg, ok := cast.Args[0].(*ast.Ident)
	return ok && arg.Name == versionVar
}

func crossEmulatorReturnIdent(stmt ast.Stmt, name string) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	ident, ok := ret.Results[0].(*ast.Ident)
	return ok && ident.Name == name
}

func crossEmulatorVersionAuditCallExpr(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	return ok && crossEmulatorVersionAuditCall(call.Fun)
}

func crossEmulatorVersionSelectionHelperAuditPrefix(t *testing.T, path string, fn *ast.FuncDecl) string {
	t.Helper()

	var prefixes []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !crossEmulatorVersionAuditCall(call.Fun) {
			return true
		}
		if len(call.Args) != 2 {
			t.Fatalf("%s:%s crossEmulatorVersionAuditVersions arg count = %d, want 2", path, fn.Name.Name, len(call.Args))
		}
		lit, ok := call.Args[1].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Fatalf("%s:%s crossEmulatorVersionAuditVersions prefix must be a string literal", path, fn.Name.Name)
		}
		prefix, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("%s:%s unquote cross-emulator version audit prefix: %v", path, fn.Name.Name, err)
		}
		prefixes = append(prefixes, prefix)
		return true
	})
	if len(prefixes) != 1 {
		t.Fatalf("%s:%s crossEmulatorVersionAuditVersions calls = %d, want 1", path, fn.Name.Name, len(prefixes))
	}
	return prefixes[0]
}

func crossEmulatorVersionSelectionHelperShardInventory(t *testing.T) []string {
	t.Helper()

	helpers := make(map[string]struct{})
	for _, helper := range crossEmulatorVersionSelectionHelperDecls(t) {
		helpers[helper.fn.Name.Name] = struct{}{}
	}

	var inventory []string
	seen := make(map[string]struct{})
	inventorySeen := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.Contains(fn.Name.Name, "VersionAuditShardSelection") {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				helper := astCallName(call.Fun)
				if _, ok := helpers[helper]; !ok {
					return true
				}
				key := filepath.ToSlash(path) + ":" + fn.Name.Name + ":" + helper
				if _, ok := inventorySeen[key]; !ok {
					inventorySeen[key] = struct{}{}
					inventory = append(inventory, key)
				}
				seen[helper] = struct{}{}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator version selection helper shard tests: %v", err)
	}
	for helper := range helpers {
		if _, ok := seen[helper]; !ok {
			t.Fatalf("cross-emulator version selection helper %s has no VersionAuditShardSelection test", helper)
		}
	}
	if len(inventory) == 0 {
		t.Fatal("cross-emulator version selection helper shard-test inventory is empty")
	}
	sort.Strings(inventory)
	return inventory
}

type crossEmulatorVersionSelectionHelperCallUse struct {
	path     string
	function string
	helper   string
}

func (u crossEmulatorVersionSelectionHelperCallUse) String() string {
	return u.path + ":" + u.function + ":" + u.helper
}

func crossEmulatorVersionSelectionHelperCallUses(t *testing.T) []crossEmulatorVersionSelectionHelperCallUse {
	t.Helper()

	var uses []crossEmulatorVersionSelectionHelperCallUse
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "cross_emulator_version_audit_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := astCallName(call.Fun)
				if name == "" || name == "crossEmulatorVersionAuditVersions" || !strings.HasSuffix(name, "CrossEmulatorVersions") {
					return true
				}
				uses = append(uses, crossEmulatorVersionSelectionHelperCallUse{
					path:     filepath.ToSlash(path),
					function: fn.Name.Name,
					helper:   name,
				})
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator version selection helper uses: %v", err)
	}
	if len(uses) == 0 {
		t.Fatal("cross-emulator version selection helper call use inventory is empty")
	}
	sort.Slice(uses, func(i, j int) bool {
		return uses[i].String() < uses[j].String()
	})
	return uses
}

func crossEmulatorVersionAuditCall(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "crossEmulatorVersionAuditVersions"
}

func crossEmulatorVersionAuditPrefixWellFormed(prefix string) bool {
	if !strings.HasPrefix(prefix, "TVM_") || !strings.HasSuffix(prefix, "_VERSION_AUDIT") || strings.Contains(prefix, "__") {
		return false
	}
	for _, r := range prefix {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

type crossEmulatorFunctionSource struct {
	path string
	name string
	body string
}

func crossEmulatorTestFunctionBodies(t *testing.T) []crossEmulatorFunctionSource {
	return crossEmulatorFunctionBodies(t, "Test")
}

func crossEmulatorFuzzFunctionBodies(t *testing.T) []crossEmulatorFunctionSource {
	return crossEmulatorFunctionBodies(t, "Fuzz")
}

func crossEmulatorFunctionBodies(t *testing.T, prefix string) []crossEmulatorFunctionSource {
	t.Helper()

	var functions []crossEmulatorFunctionSource
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		crossEmulatorFile := strings.HasSuffix(filepath.Base(path), "cross_emulator_test.go")
		tokenFile := fset.File(file.Pos())
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			if !strings.HasPrefix(name, prefix) ||
				(!strings.Contains(name, "CrossEmulator") && !(prefix == "Fuzz" && crossEmulatorFile)) {
				continue
			}

			start := tokenFile.Offset(fn.Pos())
			end := tokenFile.Offset(fn.End())
			functions = append(functions, crossEmulatorFunctionSource{
				path: filepath.ToSlash(path),
				name: name,
				body: string(src[start:end]),
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan cross-emulator %s functions: %v", prefix, err)
	}
	if len(functions) == 0 {
		t.Fatalf("cross-emulator %s function body scan found no functions", prefix)
	}
	return functions
}

func crossEmulatorFunctionNamePresent(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func crossEmulatorVersionFuzzNeedsSupportedRange(name string) bool {
	return crossEmulatorVersionFunctionNameClassified(name)
}

func crossEmulatorVersionFuzzNeedsFullSupportedRangeSeedLoop(name string) bool {
	if strings.Contains(name, "Boundary") || strings.Contains(name, "Boundaries") {
		return false
	}
	return strings.Contains(name, "AllGlobalVersions") ||
		strings.Contains(name, "AllVersions") ||
		strings.Contains(name, "GlobalVersion") ||
		strings.Contains(name, "OldVersion") ||
		strings.Contains(name, "Versioned") ||
		strings.Contains(name, "VersionInvariance") ||
		strings.Contains(name, "VersionMatrix")
}

func crossEmulatorAllVersionTestNeedsSupportedRangeSelection(name string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.Contains(name, "AllGlobalVersions") ||
		strings.Contains(name, "AllVersions")
}

func crossEmulatorVersionNamedTestNeedsSupportedRangeSelection(name string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	if strings.Contains(name, "Boundary") || strings.Contains(name, "Boundaries") {
		return false
	}
	return strings.Contains(name, "AllGlobalVersions") ||
		strings.Contains(name, "AllVersions") ||
		strings.Contains(name, "GlobalVersion") ||
		strings.Contains(name, "OldVersion") ||
		strings.Contains(name, "Versioned") ||
		strings.Contains(name, "VersionInvariance") ||
		strings.Contains(name, "VersionMatrix")
}

func crossEmulatorTestBodyUsesSupportedVersionSelection(body string) bool {
	if crossEmulatorTestBodyUsesShardedVersionSelection(body) {
		return true
	}
	return strings.Contains(body, "MinSupportedGlobalVersion") &&
		strings.Contains(body, "MaxSupportedGlobalVersion")
}

func crossEmulatorTestBodyUsesShardedVersionSelection(body string) bool {
	return strings.Contains(body, "crossEmulatorVersionAuditVersions(") ||
		strings.Contains(body, "CrossEmulatorVersions(") ||
		strings.Contains(body, "opcodeMinGlobalVersionAuditRuns(") ||
		strings.Contains(body, "differentialFuzzVersionMatrixProgramSeeds(") ||
		strings.Contains(body, "runCrossEmulatorRunVMVersionedChildOpcodeMatrix(")
}

func crossEmulatorFuzzBodyUsesVersionNormalizer(body string) bool {
	return strings.Contains(body, "tvmFuzzGlobalVersion(") ||
		strings.Contains(body, "tvmFuzzGlobalVersionByte(") ||
		strings.Contains(body, "tvmFuzzGlobalVersionSeed(") ||
		strings.Contains(body, "tvmFuzzGlobalVersionUint32(") ||
		strings.Contains(body, "differentialFuzzVersionMatrixProgramCase(")
}

func crossEmulatorVersionTestNeedsFuzzAnchor(name string) bool {
	if crossEmulatorVersionAuditOnlyTest(name) {
		return false
	}
	return strings.Contains(name, "AllGlobalVersions") ||
		strings.Contains(name, "GlobalVersion") ||
		strings.Contains(name, "OldVersion") ||
		strings.Contains(name, "Versioned") ||
		strings.Contains(name, "VersionInvariance") ||
		strings.Contains(name, "VersionMatrix") ||
		strings.Contains(name, "Boundary") ||
		strings.Contains(name, "Boundaries")
}

func crossEmulatorVersionFunctionNameClassified(name string) bool {
	return strings.Contains(name, "AllGlobalVersions") ||
		strings.Contains(name, "AllVersions") ||
		strings.Contains(name, "GlobalVersion") ||
		strings.Contains(name, "OldVersion") ||
		strings.Contains(name, "Versioned") ||
		strings.Contains(name, "VersionInvariance") ||
		strings.Contains(name, "VersionMatrix") ||
		strings.Contains(name, "Boundary") ||
		strings.Contains(name, "Boundaries")
}

func crossEmulatorFunctionBodyUsesExplicitVersion(body string) bool {
	return strings.Contains(body, "WithGlobalVersion") ||
		strings.Contains(body, "GlobalVersion:") ||
		strings.Contains(body, "GlobalVersionSet") ||
		strings.Contains(body, "tonopsCrossConfigWithGlobalVersion") ||
		strings.Contains(body, "referenceTransactionConfigRootWithGlobalVersion") ||
		strings.Contains(body, "transactionTestConfigWithGlobalVersion") ||
		strings.Contains(body, "crossEmulatorVersionAuditVersions") ||
		strings.Contains(body, "tvmFuzzGlobalVersion") ||
		strings.Contains(body, "MinSupportedGlobalVersion") ||
		strings.Contains(body, "MaxSupportedGlobalVersion") ||
		strings.Contains(body, "referenceRawRunGlobalVersion")
}

func crossEmulatorVersionAuditOnlyTest(name string) bool {
	return strings.Contains(name, "VersionAudit") ||
		strings.Contains(name, "VersionMatrixAudit") ||
		strings.Contains(name, "Anchors") ||
		strings.Contains(name, "ExplicitConfigHelperAudit") ||
		strings.Contains(name, "AuditShard") ||
		strings.Contains(name, "AuditInventory") ||
		strings.Contains(name, "FuzzSeedInventory") ||
		strings.Contains(name, "FuzzSeedScanner") ||
		strings.Contains(name, "FunctionInventory") ||
		strings.Contains(name, "ShardPartition") ||
		strings.Contains(name, "ShardParser") ||
		strings.Contains(name, "KnownReferenceMismatch")
}

func crossEmulatorVersionFuzzKey(name string) string {
	name = strings.TrimPrefix(name, "Test")
	name = strings.TrimPrefix(name, "Fuzz")
	name = strings.TrimPrefix(name, "TVMCrossEmulator")
	name = strings.ReplaceAll(name, "CrossEmulator", "")
	name = strings.TrimSuffix(name, "AllGlobalVersionsSmoke")
	name = strings.TrimSuffix(name, "AllGlobalVersions")
	name = strings.ReplaceAll(name, "GlobalVersion", "")
	name = strings.ReplaceAll(name, "AllVersions", "")
	name = strings.TrimSuffix(name, "Smoke")
	return name
}

func crossEmulatorVersionFuzzAliases() map[string][]string {
	return map[string][]string{
		"AdvancedCellOpsVersionedEdges": {
			"AdvancedCellOpsGenerated",
		},
		"OpcodeMinFullAudit": {
			"OpcodeMinBoundaries",
			"OpcodeMinRepresentative",
		},
		"RunVMXVersionedChildOpcodeMatrix": {
			"RunVMVersionedChildOpcodeMatrix",
		},
		"TVMDifferentialFuzzExplicitZero": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzRawRichC7ExplicitZero": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzVersionMatrixC7ParamFamiliesUseComparableContext": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzVersionMatrixCoversVersionAwareFamilies": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzVersionMatrixFamiliesGenerateConfiguredVersions": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzVersionMatrixFamiliesSetExplicitVersions": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzVersionMatrixSeedsSelectRequestedVersion": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialFuzzVersionedMsgAddressC7ParamRegression": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialVersionMatrixProgramFuzzerSeedsCoverVersionsAndFamilies": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"TVMDifferentialVersionMatrixProgramFuzzerBaseline": {
			"TVMDifferentialVersionMatrixPrograms",
		},
		"Transaction": {
			"TransactionBasicSuccess",
		},
		"TransactionInvalidSourceMode2": {
			"TransactionInvalidSourceDestinationMode2",
		},
		"TransactionNonComputePhaseExternalParity": {
			"TransactionNonComputePhaseExternal",
		},
		"TransactionNonComputePhaseParity": {
			"TransactionNonComputePhase",
		},
		"TransactionNonComputePhaseTickTockParity": {
			"TransactionNonComputePhaseTickTock",
		},
	}
}
