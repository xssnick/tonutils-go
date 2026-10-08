//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// TestTVMDifferentialRawBitstreamAudit covers the opcode-dispatch space between
// valid registered serializations and their strict truncations. Random refs and
// heterogeneous stack values let otherwise arbitrary prefixes reach ref-code,
// type-check and exceptional execution paths. The seed range is configurable so
// parity audits can extend it without making the ordinary tagged suite expensive.
func TestTVMDifferentialRawBitstreamAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	start := parityFuzzEnvInt(t, "TVM_RAW_BITSTREAM_AUDIT_START", 0)
	seeds := parityFuzzEnvInt(t, "TVM_RAW_BITSTREAM_AUDIT_SEEDS", 512)
	if start < 0 || seeds <= 0 {
		t.Fatal("TVM_RAW_BITSTREAM_AUDIT_START must be non-negative and TVM_RAW_BITSTREAM_AUDIT_SEEDS must be positive")
	}
	versions := make([]int, vm.MaxSupportedGlobalVersion+1)
	for version := range versions {
		versions[version] = version
	}
	if _, configured := os.LookupEnv("TVM_RAW_BITSTREAM_AUDIT_VERSION"); configured {
		version := parityFuzzEnvInt(t, "TVM_RAW_BITSTREAM_AUDIT_VERSION", 0)
		if version < 0 || version > vm.MaxSupportedGlobalVersion {
			t.Fatalf("TVM_RAW_BITSTREAM_AUDIT_VERSION must be between 0 and %d", vm.MaxSupportedGlobalVersion)
		}
		versions = []int{version}
	}

	for _, globalVersion := range versions {
		globalVersion := globalVersion
		t.Run(fmt.Sprintf("v%d", globalVersion), func(t *testing.T) {
			refCfg := differentialFuzzExplicitVersionRefConfig(t, globalVersion)
			for i := 0; i < seeds; i++ {
				seed := uint64(start + i)
				t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
					r := rand.New(rand.NewSource(int64(seed)))
					raw := randomRawBitstreamAuditCode(r)
					stack := randomRawBitstreamAuditStack(r)
					t.Logf("code=%s stack=%#v", raw.Dump(), stack)

					runDifferentialFuzzCase(t, differentialFuzzCase{
						seed:             seed,
						family:           "raw_bitstream",
						op:               fmt.Sprintf("bits=%d refs=%d stack=%d", raw.BitsSize(), raw.RefsNum(), len(stack)),
						code:             raw,
						stack:            stack,
						globalVersion:    globalVersion,
						hasGlobalVersion: true,
						gasLimit:         20_000,
						refCfg:           refCfg,
					})
				})
			}
		})
	}
}

func TestTVMDifferentialImmediateNaNShiftAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const globalVersion = 13
	refCfg := differentialFuzzExplicitVersionRefConfig(t, globalVersion)
	families := []struct {
		name       string
		prefix     uint64
		prefixBits uint
	}{
		{name: "lshift", prefix: 0xaa, prefixBits: 8},
		{name: "rshift", prefix: 0xab, prefixBits: 8},
		{name: "qlshift", prefix: 0xb7aa, prefixBits: 16},
		{name: "qrshift", prefix: 0xb7ab, prefixBits: 16},
	}

	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			for shift := 1; shift <= 256; shift++ {
				shift := shift
				t.Run(fmt.Sprintf("shift_%d", shift), func(t *testing.T) {
					code := cell.BeginCell().MustStoreUInt((family.prefix<<8)|uint64(shift-1), family.prefixBits+8).EndCell()
					runDifferentialFuzzCase(t, differentialFuzzCase{
						seed:             uint64(shift),
						family:           "immediate_nan_shift",
						op:               fmt.Sprintf("%s shift=%d", family.name, shift),
						code:             code,
						stack:            []any{vm.NaN{}},
						globalVersion:    globalVersion,
						hasGlobalVersion: true,
						refCfg:           refCfg,
					})
				})
			}
		})
	}
}

func randomRawBitstreamAuditCode(r *rand.Rand) *cell.Cell {
	// The raw runner prepends the 8-bit method-id DROP, so 1015 exercises the
	// maximum resulting 1023-bit code cell without changing execution flow.
	bits := uint(1 + r.Intn(1015))
	payload := make([]byte, (bits+7)/8)
	_, _ = r.Read(payload)

	b := cell.BeginCell().MustStoreSlice(payload, bits)
	for range r.Intn(5) {
		b.MustStoreRef(randomRawBitstreamAuditCell(r))
	}
	return b.EndCell()
}

func randomRawBitstreamAuditCell(r *rand.Rand) *cell.Cell {
	bits := uint(r.Intn(1024))
	if bits == 0 {
		return cell.BeginCell().EndCell()
	}

	payload := make([]byte, (bits+7)/8)
	_, _ = r.Read(payload)
	return cell.BeginCell().MustStoreSlice(payload, bits).EndCell()
}

func randomRawBitstreamAuditStack(r *rand.Rand) []any {
	values := make([]any, 0, 6)
	for range r.Intn(7) {
		switch r.Intn(7) {
		case 0:
			values = append(values, int64(r.Intn(33)-16))
		case 1:
			x := new(big.Int).Lsh(big.NewInt(1), uint(r.Intn(256)))
			if r.Intn(2) == 0 {
				x.Neg(x)
			}
			values = append(values, x)
		case 2:
			values = append(values, vm.NaN{})
		case 3:
			values = append(values, randomRawBitstreamAuditCell(r))
		case 4:
			values = append(values, randomRawBitstreamAuditCell(r).MustBeginParse())
		case 5:
			values = append(values, tuple.NewTupleValue(big.NewInt(int64(r.Intn(9)-4))))
		case 6:
			values = append(values, nil)
		}
	}
	return values
}
