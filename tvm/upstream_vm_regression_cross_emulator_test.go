//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

const (
	upstreamVMRegressionCrossCaseCount = 8
	upstreamVMRegressionCrossGasLimit  = int64(1000)
)

func upstreamVMRegressionCrossCases(t *testing.T) []upstreamVMRegressionCase {
	t.Helper()

	return []upstreamVMRegressionCase{
		{name: "bug_div_short_any", code: rawCodeCellFromHex(t, "6883FF73A98D")},
		{name: "assert_code_not_null", code: rawCodeCellFromHex(t, "76ED40DE")},
		{name: "bug_exec_dict_getnear", code: rawCodeCellFromHex(t, "8B048B00006D72F47573655F6D656D6D656D8B007F")},
		{name: "bug_stack_overflow", code: rawCodeCellFromHex(t, "72A93AF8")},
		{name: "assert_extract_minmax_key", code: rawCodeCellFromHex(t, "6D6DEB21807AF49C2180EB21807AF41C")},
		{name: "unhandled_exception_1", code: rawCodeCellFromHex(t, "70EDA2ED00")},
		{name: "unhandled_exception_4", code: rawCodeCellFromHex(t, "7F853EA1C8CB3E")},
		{name: "unhandled_exception_5", code: rawCodeCellFromHex(t, "738B04016D21F41476A721F49F")},
	}
}

func TestTVMCrossEmulatorUpstreamVMRegressionsAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	versions := crossEmulatorVersionAuditVersions(t, "TVM_UPSTREAM_VM_REGRESSION_VERSION_AUDIT")
	for _, tt := range upstreamVMRegressionCrossCases(t) {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runUpstreamVMRegressionVersionedCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorUpstreamVMRegressionsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%upstreamVMRegressionCrossCaseCount), uint16(upstreamVMRegressionCrossGasLimit))
	}
	for i := 0; i < upstreamVMRegressionCrossCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), uint16(upstreamVMRegressionCrossGasLimit))
	}
	f.Add(uint8(255), uint8(255), uint16(1))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawGas uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		cases := upstreamVMRegressionCrossCases(t)
		if len(cases) != upstreamVMRegressionCrossCaseCount {
			t.Fatalf("upstream regression cross case count = %d, want %d", len(cases), upstreamVMRegressionCrossCaseCount)
		}

		gasLimit := int64(1 + rawGas%uint16(upstreamVMRegressionCrossGasLimit))
		runUpstreamVMRegressionVersionedCaseWithGas(t, cases[int(rawCase)%len(cases)], version, gasLimit)
	})
}

func runUpstreamVMRegressionVersionedCase(t *testing.T, tt upstreamVMRegressionCase, globalVersion int) {
	t.Helper()

	runUpstreamVMRegressionVersionedCaseWithGas(t, tt, globalVersion, upstreamVMRegressionCrossGasLimit)
}

func runUpstreamVMRegressionVersionedCaseWithGas(t *testing.T, tt upstreamVMRegressionCase, globalVersion int, gasLimit int64) {
	t.Helper()

	code := prependRawMethodDrop(tt.code)
	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, cell.BeginCell().EndCell(), tuple.Tuple{}, nil, goStack, globalVersion, gasLimit)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refCfg.GasLimit = gasLimit
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
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

func TestTVMCrossEmulatorUpstreamVMRegressions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tt := range upstreamVMRegressionCrossCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)

			goStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCodeWithGas(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, upstreamVMRegressionCrossGasLimit)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}

			refRes, err := runReferenceCrossCodeWithGas(code, cell.BeginCell().EndCell(), tuple.Tuple{}, refStack, upstreamVMRegressionCrossGasLimit)
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
