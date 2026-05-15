package tvm

import (
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	opcellslice "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	opexec "github.com/xssnick/tonutils-go/tvm/op/exec"
	opstack "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func executionProofFixture(t *testing.T) (*cell.Cell, *cell.Cell) {
	t.Helper()

	codeTail := executionProofCodeTail(t,
		opstack.PUSHCTR(4).Serialize(),
		opcellslice.CTOS().Serialize(),
		opcellslice.LDREFRTOS().Serialize(),
		opcellslice.LDU(8).Serialize(),
		opstack.DROP().Serialize(),
		opstack.NIP().Serialize(),
		opexec.RET().Serialize(),
	)
	code := cell.BeginCell().MustStoreRef(codeTail).EndCell()

	usedData := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	unusedData := executionProofUnusedDataBranch()
	data := cell.BeginCell().MustStoreRef(usedData).MustStoreRef(unusedData).EndCell()

	return code, data
}

func executionProofResultSliceFixture(t *testing.T) (*cell.Cell, *cell.Cell) {
	t.Helper()

	codeTail := executionProofCodeTail(t,
		opstack.PUSHCTR(4).Serialize(),
		opcellslice.CTOS().Serialize(),
		opcellslice.LDREFRTOS().Serialize(),
		opcellslice.LDU(8).Serialize(),
		opexec.RET().Serialize(),
	)
	code := cell.BeginCell().MustStoreRef(codeTail).EndCell()

	usedData := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	unusedData := executionProofUnusedDataBranch()
	data := cell.BeginCell().MustStoreRef(usedData).MustStoreRef(unusedData).EndCell()

	return code, data
}

func executionProofUnusedDataBranch() *cell.Cell {
	// Keep this branch non-leaf: omitted ordinary leaf refs are materialized as proof boundaries.
	return cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()).
		EndCell()
}

func executionProofCodeTail(t *testing.T, ops ...*cell.Builder) *cell.Cell {
	t.Helper()
	return codeFromBuilders(t, ops...)
}

func executionProofAccountRoot(t *testing.T, code, data *cell.Cell) *cell.Cell {
	t.Helper()

	account, err := tlb.ToCell(&tlb.AccountState{
		IsValid: true,
		Address: address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"),
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
		},
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(0),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: data,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func TestExecuteDetailedWithAccountProofBuildsLiteserverStyleUsageProof(t *testing.T) {
	code, data := executionProofFixture(t)
	accountRoot := executionProofAccountRoot(t, code, data)

	res, err := NewTVM().ExecuteDetailedWithAccountProof(accountRoot, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack())
	if err != nil {
		t.Fatalf("execution with proof failed: %v", err)
	}
	if res.Proof == nil {
		t.Fatal("execution proof should be available")
	}

	accountBody, err := cell.UnwrapProof(res.Proof, accountRoot.Hash())
	if err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}

	var proofAccount tlb.AccountState
	if err = tlb.LoadFromCell(&proofAccount, accountBody.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode account proof body: %v", err)
	}
	if proofAccount.StateInit == nil || proofAccount.StateInit.Code == nil || proofAccount.StateInit.Data == nil {
		t.Fatal("account proof should include state init code and data")
	}

	codeTail, err := proofAccount.StateInit.Code.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if codeTail.IsSpecial() {
		t.Fatal("code proof should keep the implicit JMPREF target loaded")
	}

	loadedData, err := proofAccount.StateInit.Data.PeekRef(0)
	if err != nil {
		t.Fatal(err)
	}
	if loadedData.IsSpecial() {
		t.Fatal("loaded data branch should stay ordinary")
	}

	prunedData, err := proofAccount.StateInit.Data.PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}
	if !prunedData.IsSpecial() || prunedData.GetType() != cell.PrunedCellType {
		t.Fatalf("unloaded data branch should be pruned, got special=%v type=%v", prunedData.IsSpecial(), prunedData.GetType())
	}

	virtualAccount, err := cell.UnwrapProofVirtualized(res.Proof, accountRoot.Hash())
	if err != nil {
		t.Fatalf("failed to virtualize account proof: %v", err)
	}

	var virtualState tlb.AccountState
	if err = tlb.LoadFromCell(&virtualState, virtualAccount.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode virtualized account proof: %v", err)
	}

	proofRes, err := NewTVM().ExecuteDetailed(virtualState.StateInit.Code, virtualState.StateInit.Data, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack())
	if err != nil {
		t.Fatalf("execution from proof failed: %v", err)
	}
	if proofRes.ExitCode != res.ExitCode || proofRes.GasUsed != res.GasUsed {
		t.Fatalf("proof execution mismatch: exit %d/%d gas %d/%d", proofRes.ExitCode, res.ExitCode, proofRes.GasUsed, res.GasUsed)
	}
	if got := executionProofStackValue(t, proofRes.Stack); got != executionProofStackValue(t, res.Stack) {
		t.Fatalf("proof execution stack value mismatch: got=%d want=%d", got, executionProofStackValue(t, res.Stack))
	}
}

func TestExecuteDetailedWithAccountProofMarksResultStackCells(t *testing.T) {
	code, data := executionProofResultSliceFixture(t)
	accountRoot := executionProofAccountRoot(t, code, data)

	res, err := NewTVM().ExecuteDetailedWithAccountProof(accountRoot, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack())
	if err != nil {
		t.Fatalf("execution with proof failed: %v", err)
	}

	accountBody, err := cell.UnwrapProof(res.Proof, accountRoot.Hash())
	if err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
	var proofAccount tlb.AccountState
	if err = tlb.LoadFromCell(&proofAccount, accountBody.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode account proof body: %v", err)
	}

	resultReferencedData, err := proofAccount.StateInit.Data.PeekRef(1)
	if err != nil {
		t.Fatal(err)
	}
	if resultReferencedData.IsSpecial() {
		t.Fatal("result stack slice should keep referenced data branch ordinary")
	}
}

func TestExecuteDetailedWithAccountProofDoesNotFinalCommitGetMethodData(t *testing.T) {
	code := cell.BeginCell().MustStoreRef(executionProofCodeTail(t, opexec.RET().Serialize())).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	proofData := mustUsageProofWithLoadedRoot(t, data)
	accountRoot := executionProofAccountRoot(t, code, proofData)

	res, err := NewTVM().ExecuteDetailedWithAccountProof(accountRoot, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack())
	if err != nil {
		t.Fatalf("execution with proof failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if res.Data == nil || res.Data.HashKey() != proofData.HashKey() {
		t.Fatal("get method execution should leave data register unchanged without final commit")
	}
}

func TestEmulateExternalMessageWithAccountProofRejectsAddressMismatch(t *testing.T) {
	code, data := executionProofFixture(t)
	accountRoot := executionProofAccountRoot(t, code, data)
	msg := &tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    cell.BeginCell().EndCell(),
	}

	_, err := NewTVM().EmulateExternalMessage(code, data, msg, EmulateExternalMessageConfig{
		BuildProof:  true,
		AccountRoot: accountRoot,
	})
	if err == nil {
		t.Fatal("expected account proof address mismatch")
	}
	if !strings.Contains(err.Error(), "differs from execution address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func executionProofStackValue(t *testing.T, stack *vm.Stack) int64 {
	t.Helper()

	cp := stack.Copy()
	value, err := cp.PopInt()
	if err != nil {
		t.Fatalf("failed to pop result int: %v", err)
	}
	return value.Int64()
}
