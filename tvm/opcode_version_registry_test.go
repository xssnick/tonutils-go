package tvm

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const (
	expectedOpcodeMinGlobalVersionBaseCases                   = 130
	expectedOpcodeMinGlobalVersionGetParamLongCases           = 254
	expectedOpcodeMinGlobalVersionInMsgParamAliasCases        = 6
	expectedOpcodeMinGlobalVersionBoundaryCases               = expectedOpcodeMinGlobalVersionBaseCases + expectedOpcodeMinGlobalVersionGetParamLongCases + expectedOpcodeMinGlobalVersionInMsgParamAliasCases
	expectedOpcodeMinGlobalVersionRepresentativeCases         = 34
	expectedOpcodeMinGlobalVersionBoundaryFuzzSeedCount       = 1950
	expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedCount = 544
	expectedOpcodeMinGlobalVersionBaseHash                    = "42caf868cd1412ee24ed83beea2d073563c58c4d693ce88febf097eb86918fb5"
	expectedOpcodeMinGlobalVersionBoundaryHash                = "c9918390911ab415ee3f85fc4b1568ee49fb679bf23bc41c914dd0409e39dea5"
	expectedOpcodeMinGlobalVersionRepresentativeHash          = "4c266821ed0219dd68b8d9df618a9b950dba2ebc669fea2eaf31febdc2e3d170"
	expectedOpcodeMinGlobalVersionBoundaryFuzzSeedHash        = "61054b96b59fca7d05f62aa52157e09488a33368529431b504caad1519e31974"
	expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedHash  = "40f3b3908d08b94e5febeb8753e0c7799aaf12a458f80054cc50e3906a81da63"
)

type opcodeMinGlobalVersionCase struct {
	opcode uint64
	bits   uint
	min    int
	name   string
}

type opcodeMinGlobalVersionBoundaryFuzzSeed struct {
	caseIdx int
	version int
}

type opcodeVersionCaseKey struct {
	opcode uint64
	bits   uint
}

func opcodeMinGlobalVersionCases() []opcodeMinGlobalVersionCase {
	return []opcodeMinGlobalVersionCase{
		{opcode: 0xcf50, bits: 16, min: 12, name: "BTOS"},
		{opcode: 0xd766, bits: 16, min: 6, name: "CLEVEL"},
		{opcode: 0xd767, bits: 16, min: 6, name: "CLEVELMASK"},
		{opcode: 0x35da, bits: 14, min: 6, name: "CHASHI"},
		{opcode: 0xd768, bits: 16, min: 6, name: "CHASHI_LONG"},
		{opcode: 0x35db, bits: 14, min: 6, name: "CDEPTHI"},
		{opcode: 0xd76c, bits: 16, min: 6, name: "CDEPTHI_LONG"},
		{opcode: 0xd770, bits: 16, min: 6, name: "CHASHIX"},
		{opcode: 0xd771, bits: 16, min: 6, name: "CDEPTHIX"},
		{opcode: 0xdb4, bits: 12, min: 4, name: "RUNVM"},
		{opcode: 0xdb4000, bits: 24, min: 4, name: "RUNVM_0"},
		{opcode: 0xdb50, bits: 16, min: 4, name: "RUNVMX"},
		{opcode: 0xede3, bits: 16, min: 9, name: "SETCONTCTRMANY"},
		{opcode: 0xede300, bits: 24, min: 9, name: "SETCONTCTRMANY_0"},
		{opcode: 0xede4, bits: 16, min: 9, name: "SETCONTCTRMANYX"},
		{opcode: 0xa900, bits: 16, min: 4, name: "ADDDIVMOD"},
		{opcode: 0xa902, bits: 16, min: 4, name: "ADDDIVMODC"},
		{opcode: 0xa901, bits: 16, min: 4, name: "ADDDIVMODR"},
		{opcode: 0xa93000, bits: 24, min: 4, name: "ADDRSHIFT#MOD"},
		{opcode: 0xa93100, bits: 24, min: 4, name: "ADDRSHIFTR#MOD"},
		{opcode: 0xa93200, bits: 24, min: 4, name: "ADDRSHIFTC#MOD"},
		{opcode: 0xa920, bits: 16, min: 4, name: "ADDRSHIFTMOD"},
		{opcode: 0xa922, bits: 16, min: 4, name: "ADDRSHIFTMODC"},
		{opcode: 0xa921, bits: 16, min: 4, name: "ADDRSHIFTMODR"},
		{opcode: 0xa9c0, bits: 16, min: 4, name: "LSHIFTADDDIVMOD"},
		{opcode: 0xa9c2, bits: 16, min: 4, name: "LSHIFTADDDIVMODC"},
		{opcode: 0xa9c1, bits: 16, min: 4, name: "LSHIFTADDDIVMODR"},
		{opcode: 0xa9d000, bits: 24, min: 4, name: "LSHIFTADDDIVMOD#"},
		{opcode: 0xa9d100, bits: 24, min: 4, name: "LSHIFTADDDIVMODR#"},
		{opcode: 0xa9d200, bits: 24, min: 4, name: "LSHIFTADDDIVMODC#"},
		{opcode: 0xa980, bits: 16, min: 4, name: "MULADDDIVMOD"},
		{opcode: 0xa982, bits: 16, min: 4, name: "MULADDDIVMODC"},
		{opcode: 0xa981, bits: 16, min: 4, name: "MULADDDIVMODR"},
		{opcode: 0xa9b200, bits: 24, min: 4, name: "MULADDRSHIFTC#MOD"},
		{opcode: 0xa9a2, bits: 16, min: 4, name: "MULADDRSHIFTCMOD"},
		{opcode: 0xa9b000, bits: 24, min: 4, name: "MULADDRSHIFT#MOD"},
		{opcode: 0xa9a0, bits: 16, min: 4, name: "MULADDRSHIFTMOD"},
		{opcode: 0xa9b100, bits: 24, min: 4, name: "MULADDRSHIFTR#MOD"},
		{opcode: 0xa9a1, bits: 16, min: 4, name: "MULADDRSHIFTRMOD"},
		{opcode: 0xb7a900, bits: 24, min: 4, name: "QADDDIVMOD"},
		{opcode: 0xb7a901, bits: 24, min: 4, name: "QADDDIVMODR"},
		{opcode: 0xb7a902, bits: 24, min: 4, name: "QADDDIVMODC"},
		{opcode: 0xb7a920, bits: 24, min: 4, name: "QADDRSHIFTMOD"},
		{opcode: 0xb7a921, bits: 24, min: 4, name: "QADDRSHIFTMODR"},
		{opcode: 0xb7a922, bits: 24, min: 4, name: "QADDRSHIFTMODC"},
		{opcode: 0xb7a980, bits: 24, min: 4, name: "QMULADDDIVMOD"},
		{opcode: 0xb7a981, bits: 24, min: 4, name: "QMULADDDIVMODR"},
		{opcode: 0xb7a982, bits: 24, min: 4, name: "QMULADDDIVMODC"},
		{opcode: 0xb7a9a0, bits: 24, min: 4, name: "QMULADDRSHIFTMOD"},
		{opcode: 0xb7a9a1, bits: 24, min: 4, name: "QMULADDRSHIFTMODR"},
		{opcode: 0xb7a9a2, bits: 24, min: 4, name: "QMULADDRSHIFTMODC"},
		{opcode: 0xb7a9c0, bits: 24, min: 4, name: "QLSHIFTADDDIVMOD"},
		{opcode: 0xb7a9c1, bits: 24, min: 4, name: "QLSHIFTADDDIVMODR"},
		{opcode: 0xb7a9c2, bits: 24, min: 4, name: "QLSHIFTADDDIVMODC"},
		{opcode: 0xf807, bits: 16, min: 4, name: "GASCONSUMED"},
		{opcode: 0xf83400, bits: 24, min: 4, name: "PREVMCBLOCKS"},
		{opcode: 0xf83401, bits: 24, min: 4, name: "PREVKEYBLOCK"},
		{opcode: 0xf83402, bits: 24, min: 9, name: "PREVMCBLOCKS_100"},
		{opcode: 0xf835, bits: 16, min: 4, name: "GLOBALID"},
		{opcode: 0xf836, bits: 16, min: 6, name: "GETGASFEE"},
		{opcode: 0xf837, bits: 16, min: 6, name: "GETSTORAGEFEE"},
		{opcode: 0xf838, bits: 16, min: 6, name: "GETFORWARDFEE"},
		{opcode: 0xf839, bits: 16, min: 6, name: "GETPRECOMPILEDGAS"},
		{opcode: 0xf83a, bits: 16, min: 6, name: "GETORIGINALFWDFEE"},
		{opcode: 0xf83b, bits: 16, min: 6, name: "GETGASFEESIMPLE"},
		{opcode: 0xf83c, bits: 16, min: 6, name: "GETFORWARDFEESIMPLE"},
		{opcode: 0xf880, bits: 16, min: 10, name: "GETEXTRABALANCE"},
		{opcode: 0xf88111, bits: 24, min: 11, name: "INMSGPARAMS"},
		{opcode: 0xf890, bits: 16, min: 11, name: "INMSG_BOUNCE"},
		{opcode: 0xf891, bits: 16, min: 11, name: "INMSG_BOUNCED"},
		{opcode: 0xf892, bits: 16, min: 11, name: "INMSG_SRC"},
		{opcode: 0xf893, bits: 16, min: 11, name: "INMSG_FWDFEE"},
		{opcode: 0xf894, bits: 16, min: 11, name: "INMSG_LT"},
		{opcode: 0xf895, bits: 16, min: 11, name: "INMSG_UTIME"},
		{opcode: 0xf896, bits: 16, min: 11, name: "INMSG_ORIGVALUE"},
		{opcode: 0xf897, bits: 16, min: 11, name: "INMSG_VALUE"},
		{opcode: 0xf898, bits: 16, min: 11, name: "INMSG_VALUEEXTRA"},
		{opcode: 0xf899, bits: 16, min: 11, name: "INMSG_STATEINIT"},
		{opcode: 0x3e41, bits: 14, min: 4, name: "HASHEXT"},
		{opcode: 0xf90400, bits: 24, min: 4, name: "HASHEXT_0"},
		{opcode: 0xf912, bits: 16, min: 4, name: "ECRECOVER"},
		{opcode: 0xf913, bits: 16, min: 9, name: "SECP256K1_XONLY_PUBKEY_TWEAK_ADD"},
		{opcode: 0xf914, bits: 16, min: 4, name: "P256_CHKSIGNU"},
		{opcode: 0xf915, bits: 16, min: 4, name: "P256_CHKSIGNS"},
		{opcode: 0xf916, bits: 16, min: 12, name: "HASHBU"},
		{opcode: 0xf920, bits: 16, min: 4, name: "RIST255_FROMHASH"},
		{opcode: 0xf921, bits: 16, min: 4, name: "RIST255_VALIDATE"},
		{opcode: 0xf922, bits: 16, min: 4, name: "RIST255_ADD"},
		{opcode: 0xf923, bits: 16, min: 4, name: "RIST255_SUB"},
		{opcode: 0xf924, bits: 16, min: 4, name: "RIST255_MUL"},
		{opcode: 0xf925, bits: 16, min: 4, name: "RIST255_MULBASE"},
		{opcode: 0xf926, bits: 16, min: 4, name: "RIST255_PUSHL"},
		{opcode: 0xb7f921, bits: 24, min: 4, name: "RIST255_QVALIDATE"},
		{opcode: 0xb7f922, bits: 24, min: 4, name: "RIST255_QADD"},
		{opcode: 0xb7f923, bits: 24, min: 4, name: "RIST255_QSUB"},
		{opcode: 0xb7f924, bits: 24, min: 4, name: "RIST255_QMUL"},
		{opcode: 0xb7f925, bits: 24, min: 4, name: "RIST255_QMULBASE"},
		{opcode: 0xf93000, bits: 24, min: 4, name: "BLS_VERIFY"},
		{opcode: 0xf93001, bits: 24, min: 4, name: "BLS_AGGREGATE"},
		{opcode: 0xf93002, bits: 24, min: 4, name: "BLS_FASTAGGREGATEVERIFY"},
		{opcode: 0xf93003, bits: 24, min: 4, name: "BLS_AGGREGATEVERIFY"},
		{opcode: 0xf93010, bits: 24, min: 4, name: "BLS_G1_ADD"},
		{opcode: 0xf93011, bits: 24, min: 4, name: "BLS_G1_SUB"},
		{opcode: 0xf93012, bits: 24, min: 4, name: "BLS_G1_NEG"},
		{opcode: 0xf93013, bits: 24, min: 4, name: "BLS_G1_MUL"},
		{opcode: 0xf93014, bits: 24, min: 4, name: "BLS_G1_MULTIEXP"},
		{opcode: 0xf93015, bits: 24, min: 4, name: "BLS_G1_ZERO"},
		{opcode: 0xf93016, bits: 24, min: 4, name: "BLS_MAP_TO_G1"},
		{opcode: 0xf93017, bits: 24, min: 4, name: "BLS_G1_INGROUP"},
		{opcode: 0xf93018, bits: 24, min: 4, name: "BLS_G1_ISZERO"},
		{opcode: 0xf93020, bits: 24, min: 4, name: "BLS_G2_ADD"},
		{opcode: 0xf93021, bits: 24, min: 4, name: "BLS_G2_SUB"},
		{opcode: 0xf93022, bits: 24, min: 4, name: "BLS_G2_NEG"},
		{opcode: 0xf93023, bits: 24, min: 4, name: "BLS_G2_MUL"},
		{opcode: 0xf93024, bits: 24, min: 4, name: "BLS_G2_MULTIEXP"},
		{opcode: 0xf93025, bits: 24, min: 4, name: "BLS_G2_ZERO"},
		{opcode: 0xf93026, bits: 24, min: 4, name: "BLS_MAP_TO_G2"},
		{opcode: 0xf93027, bits: 24, min: 4, name: "BLS_G2_INGROUP"},
		{opcode: 0xf93028, bits: 24, min: 4, name: "BLS_G2_ISZERO"},
		{opcode: 0xf93030, bits: 24, min: 4, name: "BLS_PAIRING"},
		{opcode: 0xf93031, bits: 24, min: 4, name: "BLS_PUSHR"},
		{opcode: 0xfa48, bits: 16, min: 12, name: "LDSTDADDR"},
		{opcode: 0xfa49, bits: 16, min: 12, name: "LDSTDADDRQ"},
		{opcode: 0xfa50, bits: 16, min: 12, name: "LDOPTSTDADDR"},
		{opcode: 0xfa51, bits: 16, min: 12, name: "LDOPTSTDADDRQ"},
		{opcode: 0xfa52, bits: 16, min: 12, name: "STSTDADDR"},
		{opcode: 0xfa53, bits: 16, min: 12, name: "STSTDADDRQ"},
		{opcode: 0xfa54, bits: 16, min: 12, name: "STOPTSTDADDR"},
		{opcode: 0xfa55, bits: 16, min: 12, name: "STOPTSTDADDRQ"},
		{opcode: 0xfb08, bits: 16, min: 4, name: "SENDMSG"},
	}
}

func opcodeMinGlobalVersionBoundaryCases() []opcodeMinGlobalVersionCase {
	cases := append([]opcodeMinGlobalVersionCase(nil), opcodeMinGlobalVersionCases()...)
	for idx := uint64(0); idx < 255; idx++ {
		if idx == 17 {
			continue
		}
		cases = append(cases, opcodeMinGlobalVersionCase{
			opcode: 0xf88100 | idx,
			bits:   24,
			min:    11,
			name:   fmt.Sprintf("GETPARAMLONG_%02x", idx),
		})
	}
	for opcode := uint64(0xf89a); opcode < 0xf8a0; opcode++ {
		cases = append(cases, opcodeMinGlobalVersionCase{
			opcode: opcode,
			bits:   16,
			min:    11,
			name:   fmt.Sprintf("INMSGPARAM_%04x", opcode),
		})
	}

	return cases
}

func opcodeMinGlobalVersionAllVersionRepresentativeNames() map[string]struct{} {
	return map[string]struct{}{
		"RUNVM":                            {},
		"RUNVMX":                           {},
		"ADDDIVMOD":                        {},
		"ADDDIVMODC":                       {},
		"ADDDIVMODR":                       {},
		"GASCONSUMED":                      {},
		"GETGASFEE":                        {},
		"GETSTORAGEFEE":                    {},
		"GETFORWARDFEE":                    {},
		"GETORIGINALFWDFEE":                {},
		"CHASHI":                           {},
		"CDEPTHI":                          {},
		"CLEVEL":                           {},
		"CLEVELMASK":                       {},
		"CHASHIX":                          {},
		"CDEPTHIX":                         {},
		"GETPRECOMPILEDGAS":                {},
		"SETCONTCTRMANY":                   {},
		"PREVMCBLOCKS_100":                 {},
		"GETEXTRABALANCE":                  {},
		"INMSGPARAMS":                      {},
		"INMSG_VALUE":                      {},
		"BTOS":                             {},
		"HASHBU":                           {},
		"LDSTDADDR":                        {},
		"P256_CHKSIGNU":                    {},
		"P256_CHKSIGNS":                    {},
		"RIST255_VALIDATE":                 {},
		"RIST255_QVALIDATE":                {},
		"BLS_VERIFY":                       {},
		"BLS_G1_ADD":                       {},
		"BLS_G2_ADD":                       {},
		"BLS_PAIRING":                      {},
		"SECP256K1_XONLY_PUBKEY_TWEAK_ADD": {},
	}
}

func opcodeMinGlobalVersionAllVersionRepresentativeCases() []opcodeMinGlobalVersionCase {
	names := opcodeMinGlobalVersionAllVersionRepresentativeNames()
	var out []opcodeMinGlobalVersionCase
	for _, tt := range opcodeMinGlobalVersionBoundaryCases() {
		if _, ok := names[tt.name]; ok {
			out = append(out, tt)
		}
	}
	return out
}

func assertOpcodeMinGlobalVersionInventory(t testing.TB) {
	t.Helper()

	base := opcodeMinGlobalVersionCases()
	if len(base) != expectedOpcodeMinGlobalVersionBaseCases {
		t.Fatalf("opcode min-version base case count = %d, want %d", len(base), expectedOpcodeMinGlobalVersionBaseCases)
	}
	if got := opcodeMinGlobalVersionCaseHash(base); got != expectedOpcodeMinGlobalVersionBaseHash {
		t.Fatalf("opcode min-version base case hash = %s, want %s", got, expectedOpcodeMinGlobalVersionBaseHash)
	}

	boundary := opcodeMinGlobalVersionBoundaryCases()
	if len(boundary) != expectedOpcodeMinGlobalVersionBoundaryCases {
		t.Fatalf("opcode min-version boundary case count = %d, want %d", len(boundary), expectedOpcodeMinGlobalVersionBoundaryCases)
	}
	if got := opcodeMinGlobalVersionCaseHash(boundary); got != expectedOpcodeMinGlobalVersionBoundaryHash {
		t.Fatalf("opcode min-version boundary case hash = %s, want %s", got, expectedOpcodeMinGlobalVersionBoundaryHash)
	}

	representativeNames := opcodeMinGlobalVersionAllVersionRepresentativeNames()
	if len(representativeNames) != expectedOpcodeMinGlobalVersionRepresentativeCases {
		t.Fatalf("opcode min-version representative name count = %d, want %d", len(representativeNames), expectedOpcodeMinGlobalVersionRepresentativeCases)
	}
	if got := opcodeMinGlobalVersionNameHash(representativeNames); got != expectedOpcodeMinGlobalVersionRepresentativeHash {
		t.Fatalf("opcode min-version representative name hash = %s, want %s", got, expectedOpcodeMinGlobalVersionRepresentativeHash)
	}

	representatives := opcodeMinGlobalVersionAllVersionRepresentativeCases()
	if len(representatives) != expectedOpcodeMinGlobalVersionRepresentativeCases {
		t.Fatalf("opcode min-version representative case count = %d, want %d", len(representatives), expectedOpcodeMinGlobalVersionRepresentativeCases)
	}

	seenNames := make(map[string]opcodeMinGlobalVersionCase, len(boundary))
	seenKeys := make(map[opcodeVersionCaseKey]opcodeMinGlobalVersionCase, len(boundary))
	getParamLongCases := 0
	inMsgParamAliasCases := 0
	minVersions := map[int]struct{}{}
	representativeMinVersions := map[int]struct{}{}
	for _, tt := range boundary {
		if tt.name == "" {
			t.Fatal("opcode min-version case has empty name")
		}
		if prev, ok := seenNames[tt.name]; ok {
			t.Fatalf("duplicate opcode min-version case name %s for %#x/%d and %#x/%d", tt.name, prev.opcode, prev.bits, tt.opcode, tt.bits)
		}
		seenNames[tt.name] = tt

		if tt.bits == 0 || tt.bits > 64 {
			t.Fatalf("%s has unsupported instruction bit length %d", tt.name, tt.bits)
		}
		if tt.bits < 64 && tt.opcode >= uint64(1)<<tt.bits {
			t.Fatalf("%s opcode %#x does not fit into %d bits", tt.name, tt.opcode, tt.bits)
		}
		if tt.min <= 0 || tt.min > vm.MaxSupportedGlobalVersion {
			t.Fatalf("%s min global version = %d, want within (%d, %d]", tt.name, tt.min, 0, vm.MaxSupportedGlobalVersion)
		}
		minVersions[tt.min] = struct{}{}

		key := opcodeVersionCaseKey{opcode: tt.opcode, bits: tt.bits}
		if prev, ok := seenKeys[key]; ok {
			t.Fatalf("duplicate versioned opcode registry entry %#x/%d: %s and %s", tt.opcode, tt.bits, prev.name, tt.name)
		}
		seenKeys[key] = tt

		if strings.HasPrefix(tt.name, "GETPARAMLONG_") {
			getParamLongCases++
		}
		if strings.HasPrefix(tt.name, "INMSGPARAM_") {
			inMsgParamAliasCases++
		}
		if _, ok := representativeNames[tt.name]; ok {
			representativeMinVersions[tt.min] = struct{}{}
		}
	}

	if getParamLongCases != expectedOpcodeMinGlobalVersionGetParamLongCases {
		t.Fatalf("GETPARAMLONG min-version alias count = %d, want %d", getParamLongCases, expectedOpcodeMinGlobalVersionGetParamLongCases)
	}
	if inMsgParamAliasCases != expectedOpcodeMinGlobalVersionInMsgParamAliasCases {
		t.Fatalf("INMSGPARAM min-version alias count = %d, want %d", inMsgParamAliasCases, expectedOpcodeMinGlobalVersionInMsgParamAliasCases)
	}
	if _, ok := seenNames["GETPARAMLONG_11"]; ok {
		t.Fatal("GETPARAMLONG_11 must stay excluded because 0xf88111 is INMSGPARAMS")
	}

	for _, name := range opcodeMinGlobalVersionRequiredRepresentativeNames() {
		if _, ok := seenNames[name]; !ok {
			t.Fatalf("opcode min-version inventory is missing required representative %s", name)
		}
		if _, ok := representativeNames[name]; !ok {
			t.Fatalf("opcode min-version representative set is missing required %s", name)
		}
	}
	for version := range minVersions {
		if _, ok := representativeMinVersions[version]; !ok {
			t.Fatalf("opcode min-version representatives do not cover min global version %d", version)
		}
	}
}

func opcodeMinGlobalVersionCaseHash(cases []opcodeMinGlobalVersionCase) string {
	items := make([]string, 0, len(cases))
	for _, tt := range cases {
		items = append(items, fmt.Sprintf("%s:%#x:%d:%d", tt.name, tt.opcode, tt.bits, tt.min))
	}
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func opcodeMinGlobalVersionNameHash(names map[string]struct{}) string {
	items := make([]string, 0, len(names))
	for name := range names {
		items = append(items, name)
	}
	sort.Strings(items)
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func opcodeMinGlobalVersionRequiredRepresentativeNames() []string {
	return []string{
		"RUNVM",
		"GETGASFEE",
		"SETCONTCTRMANY",
		"GETEXTRABALANCE",
		"INMSGPARAMS",
		"INMSG_VALUE",
		"BTOS",
		"HASHBU",
		"LDSTDADDR",
	}
}

func opcodeMinGlobalVersionCaseMap(t testing.TB) map[opcodeVersionCaseKey]opcodeMinGlobalVersionCase {
	t.Helper()

	cases := opcodeMinGlobalVersionBoundaryCases()
	res := make(map[opcodeVersionCaseKey]opcodeMinGlobalVersionCase, len(cases)+260)
	for _, tt := range cases {
		key := opcodeVersionCaseKey{opcode: tt.opcode, bits: tt.bits}
		if prev, ok := res[key]; ok {
			t.Fatalf("duplicate versioned opcode registry entry %#x/%d: %s and %s", tt.opcode, tt.bits, prev.name, tt.name)
		}
		res[key] = tt
	}
	return res
}

func TestOpcodeMinGlobalVersionInventory(t *testing.T) {
	assertOpcodeMinGlobalVersionInventory(t)
}

func TestOpcodeMinGlobalVersionBoundaryFuzzSeedInventory(t *testing.T) {
	cases := opcodeMinGlobalVersionBoundaryCases()
	seeds := opcodeMinGlobalVersionBoundaryFuzzSeeds(cases)
	if len(seeds) == 0 {
		t.Fatal("opcode min-version boundary fuzz seeds are empty")
	}
	if len(seeds) != expectedOpcodeMinGlobalVersionBoundaryFuzzSeedCount {
		t.Fatalf("opcode min-version boundary fuzz seed count = %d, want %d:\n%s", len(seeds), expectedOpcodeMinGlobalVersionBoundaryFuzzSeedCount, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds), "\n"))
	}
	if got := opcodeMinGlobalVersionBoundaryFuzzSeedInventoryHash(cases, seeds); got != expectedOpcodeMinGlobalVersionBoundaryFuzzSeedHash {
		t.Fatalf("opcode min-version boundary fuzz seed hash = %s, want %s:\n%s", got, expectedOpcodeMinGlobalVersionBoundaryFuzzSeedHash, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds), "\n"))
	}

	seen := make(map[int]map[int]struct{}, len(cases))
	for _, seed := range seeds {
		if seed.caseIdx < 0 || seed.caseIdx >= len(cases) {
			t.Fatalf("opcode min-version boundary fuzz seed case index %d outside [0, %d)", seed.caseIdx, len(cases))
		}
		if seed.version < 0 || seed.version > vm.MaxSupportedGlobalVersion {
			t.Fatalf("opcode min-version boundary fuzz seed %s version %d outside [%d, %d]", cases[seed.caseIdx].name, seed.version, 0, vm.MaxSupportedGlobalVersion)
		}
		if seen[seed.caseIdx] == nil {
			seen[seed.caseIdx] = make(map[int]struct{})
		}
		if _, ok := seen[seed.caseIdx][seed.version]; ok {
			t.Fatalf("duplicate opcode min-version boundary fuzz seed %s v%d", cases[seed.caseIdx].name, seed.version)
		}
		seen[seed.caseIdx][seed.version] = struct{}{}
	}

	for i, tt := range cases {
		versions := seen[i]
		if len(versions) == 0 {
			t.Fatalf("opcode min-version boundary fuzz seeds do not cover case %s", tt.name)
		}
		for _, version := range opcodeMinGlobalVersionRequiredBoundarySeedVersions(tt) {
			if _, ok := versions[version]; !ok {
				t.Fatalf("opcode min-version boundary fuzz seeds do not cover %s v%d", tt.name, version)
			}
		}
	}
}

func TestOpcodeMinGlobalVersionRepresentativeFuzzSeedInventory(t *testing.T) {
	cases := opcodeMinGlobalVersionBoundaryCases()
	seeds := opcodeMinGlobalVersionRepresentativeFuzzSeeds(cases)
	if len(seeds) == 0 {
		t.Fatal("opcode min-version representative fuzz seeds are empty")
	}
	if len(seeds) != expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedCount {
		t.Fatalf("opcode min-version representative fuzz seed count = %d, want %d:\n%s", len(seeds), expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedCount, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds), "\n"))
	}
	if got := opcodeMinGlobalVersionBoundaryFuzzSeedInventoryHash(cases, seeds); got != expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedHash {
		t.Fatalf("opcode min-version representative fuzz seed hash = %s, want %s:\n%s", got, expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedHash, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds), "\n"))
	}

	representativeCases := opcodeMinGlobalVersionAllVersionRepresentativeCases()
	representativeSeeds := opcodeMinGlobalVersionRepresentativeFuzzSeeds(representativeCases)
	if len(representativeSeeds) != expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedCount {
		t.Fatalf("representative-only opcode min-version seed count = %d, want %d:\n%s", len(representativeSeeds), expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedCount, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(representativeCases, representativeSeeds), "\n"))
	}
	if got := opcodeMinGlobalVersionBoundaryFuzzSeedInventoryHash(representativeCases, representativeSeeds); got != expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedHash {
		t.Fatalf("representative-only opcode min-version seed hash = %s, want %s:\n%s", got, expectedOpcodeMinGlobalVersionRepresentativeFuzzSeedHash, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(representativeCases, representativeSeeds), "\n"))
	}

	representatives := opcodeMinGlobalVersionAllVersionRepresentativeNames()
	seen := make(map[int]map[int]struct{}, len(representatives))
	for _, seed := range seeds {
		if seed.caseIdx < 0 || seed.caseIdx >= len(cases) {
			t.Fatalf("opcode min-version representative fuzz seed case index %d outside [0, %d)", seed.caseIdx, len(cases))
		}
		tt := cases[seed.caseIdx]
		if _, ok := representatives[tt.name]; !ok {
			t.Fatalf("opcode min-version representative fuzz seed covers non-representative %s", tt.name)
		}
		if seed.version < 0 || seed.version > vm.MaxSupportedGlobalVersion {
			t.Fatalf("opcode min-version representative fuzz seed %s version %d outside [%d, %d]", tt.name, seed.version, 0, vm.MaxSupportedGlobalVersion)
		}
		if seen[seed.caseIdx] == nil {
			seen[seed.caseIdx] = make(map[int]struct{})
		}
		if _, ok := seen[seed.caseIdx][seed.version]; ok {
			t.Fatalf("duplicate opcode min-version representative fuzz seed %s v%d", tt.name, seed.version)
		}
		seen[seed.caseIdx][seed.version] = struct{}{}
	}

	for i, tt := range cases {
		if _, ok := representatives[tt.name]; !ok {
			continue
		}
		versions := seen[i]
		if len(versions) != tvmFuzzGlobalVersionCount() {
			t.Fatalf("opcode min-version representative fuzz seeds cover %s with %d versions, want %d", tt.name, len(versions), tvmFuzzGlobalVersionCount())
		}
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			if _, ok := versions[version]; !ok {
				t.Fatalf("opcode min-version representative fuzz seeds do not cover %s v%d", tt.name, version)
			}
		}
	}
}

func TestOpcodeMinGlobalVersionsMatchUpstream(t *testing.T) {
	machine := NewTVM()

	for _, tt := range opcodeMinGlobalVersionBoundaryCases() {
		t.Run(tt.name, func(t *testing.T) {
			assertOpcodeMinGlobalVersion(t, machine, tt.opcode, tt.bits, tt.min)
		})
	}
}

func TestOpcodeMinGlobalVersionRegistryCoversRegisteredVersionedOps(t *testing.T) {
	expected := opcodeMinGlobalVersionCaseMap(t)

	for _, opGetter := range vm.List {
		op := opGetter()
		versioned, ok := op.(vm.VersionedOp)
		if !ok || versioned.MinGlobalVersion() == 0 {
			continue
		}
		if versioned.MinGlobalVersion() <= 0 || versioned.MinGlobalVersion() > vm.MaxSupportedGlobalVersion {
			t.Errorf("versioned opcode %s min global version = %d, want within (%d, %d]", op.SerializeText(), versioned.MinGlobalVersion(), 0, vm.MaxSupportedGlobalVersion)
		}

		key := opcodeVersionKeyFromRegisteredOp(t, op)
		tt, ok := expected[key]
		if !ok {
			t.Errorf("versioned opcode %s %#x/%d min=%d is missing from opcodeMinGlobalVersionCases", op.SerializeText(), key.opcode, key.bits, versioned.MinGlobalVersion())
			continue
		}
		if tt.min != versioned.MinGlobalVersion() {
			t.Errorf("versioned opcode %s %#x/%d min=%d, registry has %d", op.SerializeText(), key.opcode, key.bits, versioned.MinGlobalVersion(), tt.min)
		}
	}
}

func opcodeMinGlobalVersionBoundaryFuzzSeeds(cases []opcodeMinGlobalVersionCase) []opcodeMinGlobalVersionBoundaryFuzzSeed {
	seeds := make([]opcodeMinGlobalVersionBoundaryFuzzSeed, 0, len(cases)*5)
	for i, tt := range cases {
		for _, version := range opcodeMinGlobalVersionRequiredBoundarySeedVersions(tt) {
			seeds = append(seeds, opcodeMinGlobalVersionBoundaryFuzzSeed{
				caseIdx: i,
				version: version,
			})
		}
	}
	return seeds
}

func opcodeMinGlobalVersionRepresentativeFuzzSeeds(cases []opcodeMinGlobalVersionCase) []opcodeMinGlobalVersionBoundaryFuzzSeed {
	representatives := opcodeMinGlobalVersionAllVersionRepresentativeNames()
	seeds := make([]opcodeMinGlobalVersionBoundaryFuzzSeed, 0, len(representatives)*tvmFuzzGlobalVersionCount())
	for i, tt := range cases {
		if _, ok := representatives[tt.name]; !ok {
			continue
		}
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			seeds = append(seeds, opcodeMinGlobalVersionBoundaryFuzzSeed{
				caseIdx: i,
				version: version,
			})
		}
	}
	return seeds
}

func opcodeMinGlobalVersionRequiredBoundarySeedVersions(tt opcodeMinGlobalVersionCase) []int {
	seen := make(map[int]struct{}, 5)
	versions := make([]int, 0, 5)
	for _, version := range []int{0, tt.min - 1, tt.min, tt.min + 1, vm.MaxSupportedGlobalVersion} {
		if version < 0 || version > vm.MaxSupportedGlobalVersion {
			continue
		}
		if _, ok := seen[version]; ok {
			continue
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	return versions
}

func opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases []opcodeMinGlobalVersionCase, seeds []opcodeMinGlobalVersionBoundaryFuzzSeed) []string {
	items := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		name := "<invalid>"
		if seed.caseIdx >= 0 && seed.caseIdx < len(cases) {
			name = cases[seed.caseIdx].name
		}
		items = append(items, fmt.Sprintf("%s:v%d", name, seed.version))
	}
	sort.Strings(items)
	return items
}

func opcodeMinGlobalVersionBoundaryFuzzSeedInventoryHash(cases []opcodeMinGlobalVersionCase, seeds []opcodeMinGlobalVersionBoundaryFuzzSeed) string {
	sum := sha256.Sum256([]byte(strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds), "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func assertOpcodeMinGlobalVersion(t *testing.T, machine *TVM, opcode uint64, bits uint, want int) {
	t.Helper()

	code := cell.BeginCell().MustStoreUInt(opcode, bits).EndCell().MustBeginParse()
	getter := machine.matchOpcode(code)
	if getter == nil {
		t.Fatalf("opcode %#x/%d is not registered", opcode, bits)
	}

	got := 0
	if versioned, ok := getter().(vm.VersionedOp); ok {
		got = versioned.MinGlobalVersion()
	}
	if got != want {
		t.Fatalf("opcode %#x/%d min global version = %d, want %d", opcode, bits, got, want)
	}
}

func opcodeVersionKeyFromRegisteredOp(t *testing.T, op vm.OP) opcodeVersionCaseKey {
	t.Helper()

	gasPriced, ok := op.(vm.GasPricedOp)
	if !ok {
		t.Fatalf("versioned opcode %s does not expose instruction bit length", op.SerializeText())
	}
	bits := gasPriced.InstructionBits()
	if bits <= 0 || bits > 64 {
		t.Fatalf("versioned opcode %s has unsupported instruction bit length %d", op.SerializeText(), bits)
	}

	code := op.Serialize().EndCell().MustBeginParse()
	if code.BitsLeft() < uint(bits) {
		t.Fatalf("versioned opcode %s serialized to %d bits, want at least %d", op.SerializeText(), code.BitsLeft(), bits)
	}
	return opcodeVersionCaseKey{
		opcode: code.MustPreloadUInt(uint(bits)),
		bits:   uint(bits),
	}
}
