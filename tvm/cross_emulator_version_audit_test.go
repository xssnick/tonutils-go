//go:build cgo && tvm_cross_emulator

package tvm

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func crossEmulatorVersionAuditVersions(t *testing.T, envPrefix string) []int {
	t.Helper()

	versions := make([]int, 0, vm.MaxSupportedGlobalVersion-0+1)
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
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
	wantLen := vm.MaxSupportedGlobalVersion - 0 + 1
	if len(all) != wantLen {
		t.Fatalf("default version selection len = %d, want %d", len(all), wantLen)
	}
	if all[0] != 0 || all[len(all)-1] != vm.MaxSupportedGlobalVersion {
		t.Fatalf("default version selection = %v, want range %d..%d", all, 0, vm.MaxSupportedGlobalVersion)
	}

	t.Setenv(prefix+"_SHARDS", "4")
	t.Setenv(prefix+"_SHARD", "1")
	got := crossEmulatorVersionAuditVersions(t, prefix)
	want := []int{1, 5, 9, 13, 17}
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
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			if seen[version] != 1 {
				t.Fatalf("%d-way shard partition covered v%d %d times; seen=%v", shards, version, seen[version], seen)
			}
		}
		if len(seen) != wantLen {
			t.Fatalf("%d-way shard partition covered %d versions, want %d; seen=%v", shards, len(seen), wantLen, seen)
		}
	}
}

func crossEmulatorVersionNameInventoryHash(names []string) string {
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	return fmt.Sprintf("%x", sum[:])
}
