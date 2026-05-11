//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

const upstreamVMRegressionCrossGasLimit = int64(1000)

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
