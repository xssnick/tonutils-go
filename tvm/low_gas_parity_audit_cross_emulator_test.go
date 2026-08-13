//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
)

// TestTVMDifferentialLowGasBoundaryAudit pins execution parity around the
// instruction, exception, cell-load and cell-create gas boundaries. It caught
// a dictionary descent that continued after its traced root load had already
// exhausted gas, then charged another 500 gas for materializing the key. An
// explicit zero limit is pinned separately by TestTVMCrossEmulatorZeroGasLimitParity
// because the general differential generator uses zero as its unset sentinel.
func TestTVMDifferentialLowGasBoundaryAudit(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	start := parityFuzzEnvInt(t, "TVM_PARITY_LOW_GAS_START", 8_192)
	seeds := parityFuzzEnvInt(t, "TVM_PARITY_LOW_GAS_SEEDS", 256)
	if start < 0 || seeds <= 0 {
		t.Fatal("TVM_PARITY_LOW_GAS_START must be non-negative and TVM_PARITY_LOW_GAS_SEEDS must be positive")
	}

	gasLimits := []int64{1, 10, 25, 49, 50, 51, 75, 100, 150, 250, 500, 1_000, 2_000}
	for _, family := range supportedDifferentialFuzzFamilies() {
		if family == "mixed" {
			continue
		}

		t.Run(family, func(t *testing.T) {
			for seed := uint64(start); seed < uint64(start+seeds); seed++ {
				r := rand.New(rand.NewSource(int64(seed)))
				tc := generateDifferentialFuzzCaseWithFamily(t, r, seed, family)
				version := tc.globalVersion
				if !tc.hasGlobalVersion && version == 0 {
					version = referenceRawRunGlobalVersion
				}
				if differentialFuzzKnownReferenceMismatchReason(tc, version) != "" {
					continue
				}

				baseOp := tc.op
				for _, gasLimit := range gasLimits {
					tc.gasLimit = gasLimit
					tc.op = fmt.Sprintf("%s [gas=%d]", baseOp, gasLimit)
					runDifferentialFuzzCase(t, tc)
				}
			}
		})
	}
}
