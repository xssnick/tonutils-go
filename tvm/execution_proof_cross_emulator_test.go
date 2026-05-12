//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestExecutionProofCrossEmulatorGasAndStackParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code, data := executionProofFixture(t)
	stack := vm.NewStack()

	accountRoot := executionProofAccountRoot(t, code, data)
	goRes, proof, err := runGoCrossCodeWithExecutionProof(accountRoot, stack)
	if err != nil {
		t.Fatalf("go tvm execution with proof failed: %v", err)
	}
	refRes, err := runReferenceCrossCode(code, data, tuple.Tuple{}, stack)
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

	if proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
}

func runGoCrossCodeWithExecutionProof(accountRoot *cell.Cell, stack *vm.Stack) (*crossRunResult, *cell.Cell, error) {
	execStack := stack.Copy()
	if err := execStack.PushInt(big.NewInt(0)); err != nil {
		return nil, nil, err
	}

	res, err := NewTVM().ExecuteDetailedWithAccountProof(accountRoot, tuple.Tuple{}, vm.GasWithLimit(referenceDefaultMaxGas), execStack)
	if err != nil {
		return nil, nil, err
	}
	finalStack := execStack
	if res != nil && res.Stack != nil {
		finalStack = res.Stack
	}

	stackCell, err := stackToCell(finalStack)
	if err != nil {
		return nil, nil, err
	}

	return &crossRunResult{
		exitCode: int32(res.ExitCode),
		gasUsed:  res.GasUsed,
		stack:    stackCell,
	}, res.Proof, nil
}
