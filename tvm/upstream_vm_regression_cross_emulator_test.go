//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const upstreamVMRegressionCrossGasLimit = int64(1000)

var upstreamVMRegressionCrossSkip = map[string]string{
	"assert_lookup_prefix": "release get-method emulator path disagrees with the raw vm.cpp harness on this prefix-lookup exception regression",
	"memory_leak_new":      "release reference emulator returns nil on this raw-code continuation regression",
	"infinity_loop_1": "release reference emulator still hangs on this raw-code infinite-loop regression even under the same 1000-gas harness as vm.cpp",
	"infinity_loop_2": "release reference emulator still hangs on this raw-code infinite-loop regression even under the same 1000-gas harness as vm.cpp",
	"oom_1":           "release reference emulator still hangs on this raw-code oom regression even under the same 1000-gas harness as vm.cpp",
}

var upstreamVMRegressionNoStackCompare = map[string]string{
	"assert_pfx_dict_lookup": "raw-code prefix-dictionary regression is kept locally deterministic, but wrapped get-method stack parity is unstable on the release emulator path",
	"memory_leak":           "Go stack contains runtime-only values that are not representable via the TLB stack serializer",
}

func runGoCrossCodeNoStack(code, data *cell.Cell, c7 tuple.Tuple, stack *vm.Stack) (*crossRunResult, error) {
	execStack := stack.Copy()
	if err := execStack.PushInt(big.NewInt(0)); err != nil {
		return nil, err
	}

	machine := NewTVM()
	res, err := machine.ExecuteDetailedWithLibraries(code, data, c7, vm.GasWithLimit(upstreamVMRegressionCrossGasLimit), execStack)
	exitCode := int32(0)
	if err != nil {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) {
			return nil, err
		}
		exitCode = int32(vmErr.Code)
	} else {
		exitCode = int32(res.ExitCode)
	}

	return &crossRunResult{
		exitCode: exitCode,
		gasUsed:  res.GasUsed,
	}, nil
}

func TestTVMCrossEmulatorUpstreamVMRegressions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, tt := range upstreamVMRegressionCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			if reason, skip := upstreamVMRegressionCrossSkip[tt.name]; skip {
				t.Skip(reason)
			}

			code := prependRawMethodDrop(tt.code)

			goStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			var goRes *crossRunResult
			if _, skip := upstreamVMRegressionNoStackCompare[tt.name]; skip {
				goRes, err = runGoCrossCodeNoStack(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack)
			} else {
				goRes, err = runGoCrossCodeWithGas(code, cell.BeginCell().EndCell(), tuple.Tuple{}, goStack, upstreamVMRegressionCrossGasLimit)
			}
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

			if _, skip := upstreamVMRegressionNoStackCompare[tt.name]; skip {
				return
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
