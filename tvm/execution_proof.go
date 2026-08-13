package tvm

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const accountNotInitializedExitCode = -256

func prepareAccountExecution(code, data *cell.Cell, gas vm.Gas, stack *vm.Stack, cfg ExecutionConfig) (*cell.Cell, *cell.Cell, []*cell.Cell, *cell.MerkleProofBuilder, *ExecutionResult, error) {
	proof, acc, err := prepareAccountExecutionProof(cfg.AccountRoot)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if !accountCanExecute(acc) {
		res := &ExecutionResult{
			ExitCode: accountNotInitializedExitCode,
			Gas:      gas,
			Stack:    stack,
		}
		if err = attachExecutionProof(res, nil, proof); err != nil {
			return nil, nil, nil, nil, res, err
		}
		return nil, nil, nil, nil, res, nil
	}

	if code != nil && !sameCellHash(code, acc.StateInit.Code) {
		return nil, nil, nil, nil, nil, fmt.Errorf("account root code differs from execution code")
	}
	if data != nil && !sameCellHash(data, acc.StateInit.Data) {
		return nil, nil, nil, nil, nil, fmt.Errorf("account root data differs from execution data")
	}

	libraries := cfg.Libraries
	if acc.StateInit.Lib != nil && acc.StateInit.Lib.AsCell() != nil {
		nextLibraries := make([]*cell.Cell, len(libraries)+1)
		copy(nextLibraries, libraries)
		nextLibraries[len(libraries)] = acc.StateInit.Lib.AsCell()
		libraries = nextLibraries
	}

	return acc.StateInit.Code, acc.StateInit.Data, libraries, proof, nil, nil
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
		if err := markExecutionProofStack(state.Stack, proof.ReadSet(), state.Cells.Trace()); err != nil {
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

func markExecutionProofStack(stack *vm.Stack, read *cell.ReadSet, gasTrace *cell.Trace) error {
	if stack == nil || read == nil {
		return nil
	}

	// The gas trace comes off the values first: the execution is over, and the
	// parses this walk does to reach a subtree must not be charged to it.
	stack = stack.WithoutTrace(gasTrace).Copy()
	seen := map[cell.Hash]struct{}{}
	for stack.Len() > 0 {
		val, err := stack.PopAny()
		if err != nil {
			return err
		}
		if err = markExecutionProofValue(val, read, seen); err != nil {
			return err
		}
	}
	return nil
}

func markExecutionProofValue(val any, read *cell.ReadSet, seen map[cell.Hash]struct{}) error {
	switch v := val.(type) {
	case *cell.Cell:
		return markExecutionProofCell(v, read, seen)
	case *cell.Slice:
		if v == nil {
			return nil
		}
		return markExecutionProofCell(v.RawCell(), read, seen)
	case *cell.Builder:
		return markExecutionProofCell(v.EndCell(), read, seen)
	case tuple.Tuple:
		ln := v.Len()
		for i := 0; i < ln; i++ {
			next, err := v.RawIndex(i)
			if err != nil {
				return err
			}
			if err = markExecutionProofValue(next, read, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

// markExecutionProofCell records c and everything below it. The walk keeps its
// own visited set rather than stopping at what the read set already holds: a
// cell the execution read is in the set while the references it never opened
// are not, and those are exactly what marking has to add.
//
// A stack value that never came from the proven tree is recorded too and costs
// nothing: the proof is built by walking the account root, so a hash it does not
// reach is never looked up.
func markExecutionProofCell(c *cell.Cell, read *cell.ReadSet, seen map[cell.Hash]struct{}) error {
	// Parsed without a trace: recording is explicit here, so a value still
	// carrying the recorder would only record the same cell twice.
	var loader cell.Slice
	if err := c.BeginParseIntoWithTrace(&loader, nil); err != nil {
		return err
	}
	loaded := loader.RawCell()
	key := loaded.HashKey()
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}
	read.Record(loaded)

	refsNum := loader.RefsNum()
	for i := 0; i < refsNum; i++ {
		ref, err := loader.PeekRefCellAt(i)
		if err != nil {
			return err
		}
		if err = markExecutionProofCell(ref, read, seen); err != nil {
			return err
		}
	}
	return nil
}
