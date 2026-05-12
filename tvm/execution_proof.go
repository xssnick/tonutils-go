package tvm

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const accountNotInitializedExitCode = -256

func (tvm *TVM) ExecuteDetailedWithAccountProof(accountRoot *cell.Cell, c7 tuple.Tuple, gas vm.Gas, stack *vm.Stack, libraries ...*cell.Cell) (*ExecutionResult, error) {
	proof, acc, err := prepareAccountExecutionProof(accountRoot)
	if err != nil {
		return nil, err
	}
	if !accountCanExecute(acc) {
		res := &ExecutionResult{
			ExitCode: accountNotInitializedExitCode,
			Gas:      gas,
			Stack:    stack,
		}
		if err = attachExecutionProof(res, nil, proof); err != nil {
			return res, err
		}
		return res, nil
	}

	code := acc.StateInit.Code
	data := acc.StateInit.Data
	if acc.StateInit.Lib != nil && acc.StateInit.Lib.AsCell() != nil {
		libraries = append(append([]*cell.Cell(nil), libraries...), acc.StateInit.Lib.AsCell())
	}

	return tvm.executeDetailedWithLibrariesRawOptions(code, data, c7, gas, stack, executeOptions{
		proof: proof,
	}, libraries...)
}

func prepareAccountExecutionProof(accountRoot *cell.Cell) (*cell.MerkleProofBuilder, *tlb.AccountState, error) {
	if accountRoot == nil {
		return nil, nil, fmt.Errorf("account root is nil")
	}

	proof := cell.NewMerkleProofBuilder(accountRoot)
	loader, err := proof.Root().BeginParse()
	if err != nil {
		return nil, nil, err
	}

	var acc tlb.AccountState
	if err = tlb.LoadFromCell(&acc, loader); err != nil {
		return nil, nil, fmt.Errorf("failed to decode account state: %w", err)
	}
	return proof, &acc, nil
}

func accountCanExecute(acc *tlb.AccountState) bool {
	return acc != nil &&
		acc.IsValid &&
		acc.Status == tlb.AccountStatusActive &&
		acc.StateInit != nil &&
		acc.StateInit.Code != nil
}

func attachExecutionProof(res *ExecutionResult, state *vm.State, proof *cell.MerkleProofBuilder) error {
	if res == nil || proof == nil {
		return nil
	}

	if state != nil {
		if err := markExecutionProofStack(state.Stack, proof.UsageTree(), state.Cells.Trace()); err != nil {
			return err
		}
	}

	accountProof, err := proof.CreateProof()
	if err != nil {
		return fmt.Errorf("failed to build account execution proof: %w", err)
	}
	res.Proof = accountProof
	return nil
}

func markExecutionProofStack(stack *vm.Stack, usageTree *cell.CellUsageTree, gasTrace *cell.Trace) error {
	if stack == nil || usageTree == nil {
		return nil
	}

	stack = stack.WithoutTrace(gasTrace).Copy()
	seen := map[cell.Hash]struct{}{}
	for stack.Len() > 0 {
		val, err := stack.PopAny()
		if err != nil {
			return err
		}
		if err = markExecutionProofValue(val, usageTree, seen); err != nil {
			return err
		}
	}
	return nil
}

func markExecutionProofValue(val any, usageTree *cell.CellUsageTree, seen map[cell.Hash]struct{}) error {
	switch v := val.(type) {
	case *cell.Cell:
		return markExecutionProofCell(v, usageTree, seen)
	case *cell.Slice:
		return markExecutionProofCell(v.BaseCell(), usageTree, seen)
	case *cell.Builder:
		return markExecutionProofCell(v.EndCell(), usageTree, seen)
	case tuple.Tuple:
		for i := 0; i < v.Len(); i++ {
			next, err := v.RawIndex(i)
			if err != nil {
				return err
			}
			if err = markExecutionProofValue(next, usageTree, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func markExecutionProofCell(c *cell.Cell, usageTree *cell.CellUsageTree, seen map[cell.Hash]struct{}) error {
	node, ok := usageTree.NodeForCell(c)
	if !ok {
		loader, err := c.WithTrace(nil).BeginParse()
		if err != nil {
			return err
		}
		for i := 0; i < loader.RefsNum(); i++ {
			ref, err := loader.PeekRefCellAt(i)
			if err != nil {
				return err
			}
			if err = markExecutionProofCell(ref, usageTree, seen); err != nil {
				return err
			}
		}
		return nil
	}

	loader, err := c.WithTrace(usageTree.Trace(node)).BeginParse()
	if err != nil {
		return err
	}
	base := loader.BaseCell()
	if _, ok = seen[base.HashKey()]; ok {
		return nil
	}
	seen[base.HashKey()] = struct{}{}

	for i := 0; i < loader.RefsNum(); i++ {
		ref, err := loader.PeekRefCellAt(i)
		if err != nil {
			return err
		}
		if err = markExecutionProofCell(ref, usageTree, seen); err != nil {
			return err
		}
	}
	return nil
}
