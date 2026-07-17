//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const (
	expectedRegisteredOpcodeAvailabilityFuzzSeedCount = 640
	expectedRegisteredOpcodeAvailabilityFuzzSeedHash  = "fc495b540d3e0021db2a3b3f226fab705281ce809c8bb342cf6d5b8b93a1b0cf"
)

func TestTVMCrossEmulatorOpcodeMinGlobalVersionBoundaries(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tt := range opcodeMinGlobalVersionBoundaryCases() {
		if tt.min == 0 {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			assertCrossOpcodeVersionBoundary(t, tt, tt.min-1, true)
			assertCrossOpcodeVersionBoundary(t, tt, tt.min, false)
			if tt.min < vm.MaxSupportedGlobalVersion {
				assertCrossOpcodeVersionBoundary(t, tt, tt.min+1, false)
			}
		})
	}
}

func TestTVMCrossEmulatorOpcodeMinGlobalVersionRepresentativeAllVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_OPCODE_REPRESENTATIVE_VERSION_AUDIT")
	for _, tt := range opcodeMinGlobalVersionAllVersionRepresentativeCases() {
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					assertCrossOpcodeVersionBoundary(t, tt, version, version < tt.min)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorOpcodeMinGlobalVersionRepresentativeAllVersions(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := opcodeMinGlobalVersionAllVersionRepresentativeCases()
	if len(cases) == 0 {
		f.Fatal("opcode min-version representative case list is empty")
	}
	for _, seed := range opcodeMinGlobalVersionRepresentativeFuzzSeeds(cases) {
		f.Add(uint16(seed.caseIdx), uint8(seed.version))
	}
	f.Add(uint16(len(cases)+11), uint8(255))

	f.Fuzz(func(t *testing.T, rawCase uint16, rawVersion uint8) {
		tt := cases[int(rawCase)%len(cases)]
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertCrossOpcodeVersionBoundary(t, tt, version, version < tt.min)
	})
}

func FuzzTVMCrossEmulatorOpcodeMinGlobalVersionBoundaries(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := opcodeMinGlobalVersionBoundaryCases()
	if len(cases) == 0 {
		f.Fatal("opcode min-version boundary case list is empty")
	}
	f.Add(uint16(0), uint8(0))
	f.Add(uint16(0), uint8(vm.MaxSupportedGlobalVersion))
	for _, seed := range opcodeMinGlobalVersionBoundaryFuzzSeeds(cases) {
		f.Add(uint16(seed.caseIdx), uint8(seed.version))
	}
	f.Add(uint16(len(cases)+17), uint8(255))

	f.Fuzz(func(t *testing.T, rawCase uint16, rawVersion uint8) {
		tt := cases[int(rawCase)%len(cases)]
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertCrossOpcodeVersionBoundary(t, tt, version, version < tt.min)
	})
}

func TestTVMCrossEmulatorOpcodeMinGlobalVersionFuzzSeedAuditInventory(t *testing.T) {
	cases := opcodeMinGlobalVersionBoundaryCases()
	seeds := opcodeMinGlobalVersionBoundaryFuzzSeeds(cases)
	if len(seeds) == 0 {
		t.Fatal("opcode min-version boundary fuzz seeds are empty")
	}
	if len(seeds) != expectedOpcodeMinGlobalVersionBoundaryFuzzSeedCount {
		t.Fatalf("opcode min-version boundary fuzz seed count = %d, want %d:\n%s", len(seeds), expectedOpcodeMinGlobalVersionBoundaryFuzzSeedCount, strings.Join(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds), "\n"))
	}
	if got := crossEmulatorVersionNameInventoryHash(opcodeMinGlobalVersionBoundaryFuzzSeedInventory(cases, seeds)); got != expectedOpcodeMinGlobalVersionBoundaryFuzzSeedHash {
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

func TestTVMCrossEmulatorOpcodeMinGlobalVersionFullAllVersionsAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	runs := opcodeMinGlobalVersionAuditRuns(t, opcodeMinGlobalVersionBoundaryCases())
	if len(runs) == 0 {
		t.Fatal("no opcode min-version audit runs selected")
	}

	for _, run := range runs {
		run := run
		t.Run(fmt.Sprintf("%s_v%d", run.name, run.version), func(t *testing.T) {
			assertCrossOpcodeVersionBoundary(t, run.opcodeMinGlobalVersionCase, run.version, run.version < run.min)
		})
	}
}

func TestTVMCrossEmulatorOpcodeMinGlobalVersionAuditInventory(t *testing.T) {
	assertOpcodeMinGlobalVersionInventory(t)

	cases := opcodeMinGlobalVersionBoundaryCases()
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARD", "")
	runs := opcodeMinGlobalVersionAuditRuns(t, cases)
	wantRuns := len(cases) * (vm.MaxSupportedGlobalVersion - 0 + 1)
	if len(runs) != wantRuns {
		t.Fatalf("opcode min-version audit runs = %d, want %d", len(runs), wantRuns)
	}
}

func TestTVMCrossEmulatorRegisteredOpcodeAvailabilityAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := registeredOpcodeAvailabilityAuditCases()
	runs := registeredOpcodeAvailabilityAuditRuns(t, cases, registeredOpcodeAvailabilityAuditVersions())
	if len(runs) == 0 {
		t.Fatal("no registered opcode availability audit runs")
	}

	for _, run := range runs {
		run := run
		t.Run(fmt.Sprintf("%s_v%d", run.name, run.version), func(t *testing.T) {
			assertCrossRegisteredOpcodeAvailability(t, run.opName, run.code, run.version)
		})
	}
}

func FuzzTVMCrossEmulatorRegisteredOpcodeAvailabilityGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := registeredOpcodeAvailabilityAuditCases()
	if len(cases) == 0 {
		f.Fatal("registered opcode availability case list is empty")
	}
	for _, seed := range registeredOpcodeAvailabilityFuzzSeeds(cases) {
		f.Add(uint16(seed.caseIdx), uint8(seed.version))
	}
	f.Add(uint16(len(cases)+29), uint8(255))

	f.Fuzz(func(t *testing.T, rawCase uint16, rawVersion uint8) {
		tt := cases[int(rawCase)%len(cases)]
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertCrossRegisteredOpcodeAvailability(t, tt.opName, tt.code, version)
	})
}

func TestTVMCrossEmulatorRegisteredOpcodeAvailabilityFuzzSeedInventory(t *testing.T) {
	cases := registeredOpcodeAvailabilityAuditCases()
	seeds := registeredOpcodeAvailabilityFuzzSeeds(cases)
	assertRegisteredOpcodeAvailabilityFuzzSeedInventory(
		t,
		cases,
		seeds,
		expectedRegisteredOpcodeAvailabilityFuzzSeedCount,
		expectedRegisteredOpcodeAvailabilityFuzzSeedHash,
	)
}

func TestTVMCrossEmulatorRegisteredOpcodeAvailabilityAuditInventory(t *testing.T) {
	assertRegisteredOpcodeAvailabilityAuditInventory(t)

	cases := registeredOpcodeAvailabilityAuditCases()
	versions := []int{0, vm.MaxSupportedGlobalVersion}
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARD", "")
	runs := registeredOpcodeAvailabilityAuditRuns(t, cases, versions)
	if len(runs) != len(cases)*len(versions) {
		t.Fatalf("registered opcode availability audit runs = %d, want %d", len(runs), len(cases)*len(versions))
	}
}

func TestTVMCrossEmulatorOpcodeMinGlobalVersionAuditShardPartition(t *testing.T) {
	cases := opcodeMinGlobalVersionBoundaryCases()
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARD", "")
	all := opcodeMinGlobalVersionAuditRuns(t, cases)
	if len(all) == 0 {
		t.Fatal("opcode min-version audit has no runs")
	}

	want := make(map[string]struct{}, len(all))
	for _, run := range all {
		want[fmt.Sprintf("%s/v%d", run.name, run.version)] = struct{}{}
	}
	if len(want) != len(all) {
		t.Fatalf("opcode min-version audit has duplicate unsharded runs: got %d unique, want %d", len(want), len(all))
	}

	for _, shards := range []int{1, 2, 3, 4, 17} {
		seen := make(map[string]int, len(all))
		for shard := 0; shard < shards; shard++ {
			t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARDS", strconv.Itoa(shards))
			t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARD", strconv.Itoa(shard))
			for _, run := range opcodeMinGlobalVersionAuditRuns(t, cases) {
				key := fmt.Sprintf("%s/v%d", run.name, run.version)
				if _, ok := want[key]; !ok {
					t.Fatalf("%d-way opcode min-version shard produced unexpected run %s", shards, key)
				}
				seen[key]++
			}
		}

		if len(seen) != len(want) {
			t.Fatalf("%d-way opcode min-version sharding covered %d runs, want %d", shards, len(seen), len(want))
		}
		for key := range want {
			if seen[key] != 1 {
				t.Fatalf("%d-way opcode min-version sharding covered %s %d times", shards, key, seen[key])
			}
		}
	}
}

func TestTVMCrossEmulatorRegisteredOpcodeAvailabilityAuditShardPartition(t *testing.T) {
	cases := registeredOpcodeAvailabilityAuditCases()
	versions := registeredOpcodeAvailabilityAuditVersions()
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARDS", "")
	t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARD", "")
	all := registeredOpcodeAvailabilityAuditRuns(t, cases, versions)
	if len(all) == 0 {
		t.Fatal("registered opcode availability audit has no runs")
	}

	want := make(map[string]struct{}, len(all))
	for _, run := range all {
		want[fmt.Sprintf("%s/v%d", run.name, run.version)] = struct{}{}
	}
	if len(want) != len(all) {
		t.Fatalf("registered opcode availability audit has duplicate unsharded runs: got %d unique, want %d", len(want), len(all))
	}

	for _, shards := range []int{1, 2, 3, 4, 17} {
		seen := make(map[string]int, len(all))
		for shard := 0; shard < shards; shard++ {
			t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARDS", strconv.Itoa(shards))
			t.Setenv("TVM_OPCODE_VERSION_AUDIT_SHARD", strconv.Itoa(shard))
			for _, run := range registeredOpcodeAvailabilityAuditRuns(t, cases, versions) {
				key := fmt.Sprintf("%s/v%d", run.name, run.version)
				if _, ok := want[key]; !ok {
					t.Fatalf("%d-way registered opcode availability shard produced unexpected run %s", shards, key)
				}
				seen[key]++
			}
		}

		if len(seen) != len(want) {
			t.Fatalf("%d-way registered opcode availability sharding covered %d runs, want %d", shards, len(seen), len(want))
		}
		for key := range want {
			if seen[key] != 1 {
				t.Fatalf("%d-way registered opcode availability sharding covered %s %d times", shards, key, seen[key])
			}
		}
	}
}

func TestTVMCrossEmulatorOpcodeVersionAuditShardParser(t *testing.T) {
	tests := []struct {
		name        string
		rawShards   string
		rawShard    string
		wantShard   int
		wantShards  int
		wantSharded bool
		wantErr     string
	}{
		{name: "unset"},
		{name: "valid", rawShards: "4", rawShard: "2", wantShard: 2, wantShards: 4, wantSharded: true},
		{name: "missing shard", rawShards: "4", wantErr: "must be set together"},
		{name: "missing shards", rawShard: "1", wantErr: "must be set together"},
		{name: "zero shards", rawShards: "0", rawShard: "0", wantErr: "invalid TVM_OPCODE_VERSION_AUDIT_SHARDS"},
		{name: "negative shard", rawShards: "4", rawShard: "-1", wantErr: "invalid TVM_OPCODE_VERSION_AUDIT_SHARD"},
		{name: "shard too large", rawShards: "4", rawShard: "4", wantErr: "invalid TVM_OPCODE_VERSION_AUDIT_SHARD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotShard, gotShards, gotSharded, err := opcodeVersionAuditShardFromEnv(tt.rawShards, tt.rawShard)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotShard != tt.wantShard || gotShards != tt.wantShards || gotSharded != tt.wantSharded {
				t.Fatalf("shard parse = (%d, %d, %t), want (%d, %d, %t)", gotShard, gotShards, gotSharded, tt.wantShard, tt.wantShards, tt.wantSharded)
			}
		})
	}
}

type registeredOpcodeAvailabilityAuditRun struct {
	registeredOpcodeAvailabilityAuditCase
	version int
}

type opcodeMinGlobalVersionAuditRun struct {
	opcodeMinGlobalVersionCase
	version int
}

func registeredOpcodeAvailabilityAuditVersions() []int {
	versions := make([]int, 0, vm.MaxSupportedGlobalVersion-0+1)
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		versions = append(versions, version)
	}
	return versions
}

func registeredOpcodeAvailabilityAuditRuns(t *testing.T, cases []registeredOpcodeAvailabilityAuditCase, versions []int) []registeredOpcodeAvailabilityAuditRun {
	t.Helper()

	runs := make([]registeredOpcodeAvailabilityAuditRun, 0, len(cases)*len(versions))
	for _, tc := range cases {
		for _, version := range versions {
			runs = append(runs, registeredOpcodeAvailabilityAuditRun{
				registeredOpcodeAvailabilityAuditCase: tc,
				version:                               version,
			})
		}
	}

	shard, shards, sharded := opcodeVersionAuditShard(t)
	if !sharded {
		return runs
	}

	out := make([]registeredOpcodeAvailabilityAuditRun, 0, (len(runs)+shards-1)/shards)
	for i, run := range runs {
		if i%shards == shard {
			out = append(out, run)
		}
	}
	return out
}

func opcodeMinGlobalVersionAuditRuns(t *testing.T, cases []opcodeMinGlobalVersionCase) []opcodeMinGlobalVersionAuditRun {
	t.Helper()

	runs := make([]opcodeMinGlobalVersionAuditRun, 0, len(cases)*(vm.MaxSupportedGlobalVersion-0+1))
	for _, tt := range cases {
		if tt.min == 0 {
			continue
		}
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			runs = append(runs, opcodeMinGlobalVersionAuditRun{
				opcodeMinGlobalVersionCase: tt,
				version:                    version,
			})
		}
	}

	shard, shards, sharded := opcodeVersionAuditShard(t)
	if !sharded {
		return runs
	}

	out := make([]opcodeMinGlobalVersionAuditRun, 0, (len(runs)+shards-1)/shards)
	for i, run := range runs {
		if i%shards == shard {
			out = append(out, run)
		}
	}
	return out
}

func opcodeVersionAuditShard(t *testing.T) (int, int, bool) {
	t.Helper()

	rawShards := os.Getenv("TVM_OPCODE_VERSION_AUDIT_SHARDS")
	rawShard := os.Getenv("TVM_OPCODE_VERSION_AUDIT_SHARD")
	shard, shards, sharded, err := opcodeVersionAuditShardFromEnv(rawShards, rawShard)
	if err != nil {
		t.Fatal(err)
	}
	return shard, shards, sharded
}

func opcodeVersionAuditShardFromEnv(rawShards, rawShard string) (int, int, bool, error) {
	if rawShards == "" && rawShard == "" {
		return 0, 0, false, nil
	}
	if rawShards == "" || rawShard == "" {
		return 0, 0, false, fmt.Errorf("TVM_OPCODE_VERSION_AUDIT_SHARDS and TVM_OPCODE_VERSION_AUDIT_SHARD must be set together")
	}

	shards, err := strconv.Atoi(rawShards)
	if err != nil || shards <= 0 {
		return 0, 0, false, fmt.Errorf("invalid TVM_OPCODE_VERSION_AUDIT_SHARDS=%q", rawShards)
	}
	shard, err := strconv.Atoi(rawShard)
	if err != nil || shard < 0 || shard >= shards {
		return 0, 0, false, fmt.Errorf("invalid TVM_OPCODE_VERSION_AUDIT_SHARD=%q for %d shards", rawShard, shards)
	}

	return shard, shards, true, nil
}

func assertCrossOpcodeVersionBoundary(t *testing.T, tt opcodeMinGlobalVersionCase, version int, wantInvalid bool) {
	t.Helper()

	code := prependRawMethodDrop(opcodeMinVersionInstructionCode(tt))
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
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

	goInvalid := goRes.exitCode == vmerr.CodeInvalidOpcode
	refInvalid := refRes.exitCode == vmerr.CodeInvalidOpcode
	if goInvalid != refInvalid {
		t.Fatalf("invalid-opcode mismatch for %#x/%d at v%d: go exit=%d reference exit=%d", tt.opcode, tt.bits, version, goRes.exitCode, refRes.exitCode)
	}
	if goInvalid != wantInvalid {
		t.Fatalf("unexpected opcode availability for %#x/%d at v%d: invalid=%v want=%v go exit=%d reference exit=%d", tt.opcode, tt.bits, version, goInvalid, wantInvalid, goRes.exitCode, refRes.exitCode)
	}
	if wantInvalid && goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch for %#x/%d at v%d: go=%d reference=%d", tt.opcode, tt.bits, version, goRes.gasUsed, refRes.gasUsed)
	}
}

func assertCrossRegisteredOpcodeAvailability(t *testing.T, name string, code *cell.Cell, version int) {
	t.Helper()

	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goExit, err := runGoRegisteredOpcodeAvailability(code, version)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}
	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(version)))
	refCfg.GasLimit = registeredOpcodeAvailabilityAuditGasLimit
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	goInvalid := goExit == vmerr.CodeInvalidOpcode
	refInvalid := refRes.exitCode == vmerr.CodeInvalidOpcode
	if goInvalid != refInvalid {
		t.Fatalf("invalid-opcode mismatch for %s at v%d: go exit=%d reference exit=%d", name, version, goExit, refRes.exitCode)
	}
}

func runGoRegisteredOpcodeAvailability(code *cell.Cell, version int) (int32, error) {
	stack := vm.NewStack()
	if err := stack.PushSmallInt(0); err != nil {
		return 0, err
	}

	machine := NewTVM()
	cfg, err := crossRunPreparedBlockchainConfig(version)
	if err != nil {
		return 0, err
	}
	res, err := machine.Execute(
		code,
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		vm.GasWithLimit(registeredOpcodeAvailabilityAuditGasLimit),
		stack,
		ExecutionConfig{Config: cfg})

	if err != nil {
		return 0, err
	}
	return int32(res.ExitCode), nil
}
