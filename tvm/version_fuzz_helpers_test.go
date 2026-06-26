package tvm

import (
	"testing"

	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func tvmFuzzGlobalVersionCount() int {
	return MaxSupportedGlobalVersion - MinSupportedGlobalVersion + 1
}

func tvmFuzzGlobalVersion(raw int64) int {
	if raw >= int64(MinSupportedGlobalVersion) && raw <= int64(MaxSupportedGlobalVersion) {
		return int(raw)
	}

	version := int(raw % int64(tvmFuzzGlobalVersionCount()))
	if version < 0 {
		version = -version
	}
	return MinSupportedGlobalVersion + version
}

func tvmFuzzGlobalVersionUint32(raw uint32) uint32 {
	if raw >= uint32(MinSupportedGlobalVersion) && raw <= uint32(MaxSupportedGlobalVersion) {
		return raw
	}
	return uint32(MinSupportedGlobalVersion) + raw%uint32(tvmFuzzGlobalVersionCount())
}

func tvmFuzzGlobalVersionByte(raw byte) int {
	version := int(raw)
	if version >= MinSupportedGlobalVersion && version <= MaxSupportedGlobalVersion {
		return version
	}
	return MinSupportedGlobalVersion + int(raw)%tvmFuzzGlobalVersionCount()
}

func tvmFuzzGlobalVersionSeed(seed uint64) int {
	if seed >= uint64(MinSupportedGlobalVersion) && seed <= uint64(MaxSupportedGlobalVersion) {
		return int(seed)
	}
	return MinSupportedGlobalVersion + int(seed%uint64(tvmFuzzGlobalVersionCount()))
}

func tvmFuzzGlobalVersionMatrixSeed(start uint64, offset int, version int) uint64 {
	count := uint64(tvmFuzzGlobalVersionCount())
	residue := uint64(version - MinSupportedGlobalVersion)
	multiplier := start + uint64(offset)
	maxMultiplier := (^uint64(0) - residue) / count
	if multiplier > maxMultiplier {
		multiplier %= maxMultiplier + 1
	}
	return multiplier*count + residue
}

func TestTVMSupportedGlobalVersionConstantsMatchLocalFuzzAssumptions(t *testing.T) {
	if MinSupportedGlobalVersion != 0 {
		t.Fatalf("min supported global version = %d, want 0; update package-local fuzz loops that start from zero", MinSupportedGlobalVersion)
	}
	if MaxSupportedGlobalVersion != vmcore.DefaultGlobalVersion {
		t.Fatalf("max supported global version = %d, vm default = %d; update package-local fuzz helpers to cover the whole supported range", MaxSupportedGlobalVersion, vmcore.DefaultGlobalVersion)
	}
}

func TestTVMFuzzGlobalVersionMappersCoverSupportedRange(t *testing.T) {
	if tvmFuzzGlobalVersionCount() <= 0 {
		t.Fatalf("supported global version count = %d", tvmFuzzGlobalVersionCount())
	}
	if MaxSupportedGlobalVersion > 255 {
		t.Fatalf("byte fuzz global version seed cannot directly cover max global version %d", MaxSupportedGlobalVersion)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		if got := tvmFuzzGlobalVersion(int64(version)); got != version {
			t.Fatalf("int64 version seed %d mapped to %d, want %d", version, got, version)
		}
		if got := tvmFuzzGlobalVersionUint32(uint32(version)); got != uint32(version) {
			t.Fatalf("uint32 version seed %d mapped to %d, want %d", version, got, version)
		}
		if got := tvmFuzzGlobalVersionByte(byte(version)); got != version {
			t.Fatalf("byte version seed %d mapped to %d, want %d", version, got, version)
		}
		if got := tvmFuzzGlobalVersionSeed(uint64(version)); got != version {
			t.Fatalf("uint64 seed version %d mapped to %d, want %d", version, got, version)
		}

		seed := tvmFuzzGlobalVersionMatrixSeed(0, 0, version)
		if got := tvmFuzzGlobalVersionSeed(seed); got != version {
			t.Fatalf("matrix seed %d mapped to v%d, want v%d", seed, got, version)
		}
	}
}

func TestTVMFuzzGlobalVersionMappersClampArbitrarySeeds(t *testing.T) {
	int64Seeds := []int64{
		-1,
		int64(MinSupportedGlobalVersion) - 1,
		int64(MaxSupportedGlobalVersion) + 1,
		-123456789,
		123456789,
		-1 << 63,
		1<<63 - 1,
	}
	for _, seed := range int64Seeds {
		got := tvmFuzzGlobalVersion(seed)
		if got < MinSupportedGlobalVersion || got > MaxSupportedGlobalVersion {
			t.Fatalf("int64 seed %d mapped to v%d, want within [%d, %d]", seed, got, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
		}
	}

	uint32Seeds := []uint32{
		0,
		uint32(MaxSupportedGlobalVersion) + 1,
		123456789,
		^uint32(0),
	}
	for _, seed := range uint32Seeds {
		got := tvmFuzzGlobalVersionUint32(seed)
		if got < uint32(MinSupportedGlobalVersion) || got > uint32(MaxSupportedGlobalVersion) {
			t.Fatalf("uint32 seed %d mapped to v%d, want within [%d, %d]", seed, got, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
		}
	}

	for seed := 0; seed <= 255; seed++ {
		got := tvmFuzzGlobalVersionByte(byte(seed))
		if got < MinSupportedGlobalVersion || got > MaxSupportedGlobalVersion {
			t.Fatalf("byte seed %d mapped to v%d, want within [%d, %d]", seed, got, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
		}
	}

	uint64Seeds := []uint64{
		0,
		uint64(MaxSupportedGlobalVersion) + 1,
		123456789,
		^uint64(0),
	}
	for _, seed := range uint64Seeds {
		got := tvmFuzzGlobalVersionSeed(seed)
		if got < MinSupportedGlobalVersion || got > MaxSupportedGlobalVersion {
			t.Fatalf("uint64 seed %d mapped to v%d, want within [%d, %d]", seed, got, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
		}
	}
}

func TestTVMFuzzGlobalVersionMatrixSeedAvoidsOverflow(t *testing.T) {
	starts := []uint64{
		0,
		1,
		123456789,
		^uint64(0) - 128,
		^uint64(0),
	}
	offsets := []int{0, 1, 17, 4096}
	for _, start := range starts {
		for _, offset := range offsets {
			for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
				seed := tvmFuzzGlobalVersionMatrixSeed(start, offset, version)
				if got := tvmFuzzGlobalVersionSeed(seed); got != version {
					t.Fatalf("matrix seed start=%d offset=%d version=%d produced seed %d selecting v%d", start, offset, version, seed, got)
				}
			}
		}
	}
}
