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
	opfuncs "github.com/xssnick/tonutils-go/tvm/op/funcs"
	opstack "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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

	return executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"),
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(0),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: data,
			},
		},
	})
}

func executionProofAccountStateRoot(t *testing.T, account tlb.AccountState) *cell.Cell {
	t.Helper()

	root, err := tlb.ToCell(&account)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func executionProofStorageInfo() tlb.StorageInfo {
	return tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
	}
}

func TestExecuteDetailedWithAccountProofBuildsLiteserverStyleUsageProof(t *testing.T) {
	code, data := executionProofFixture(t)
	accountRoot := executionProofAccountRoot(t, code, data)

	res, err := NewTVM().ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack(), ExecutionConfig{
		AccountRoot: accountRoot,
		Config:      testPreparedBlockchainConfig(t),
	})
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

	proofRes, err := NewTVM().Execute(virtualState.StateInit.Code, virtualState.StateInit.Data, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack(), testExecutionConfig(t))
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

func TestPrepareAccountExecutionProofRejectsNilAndMalformedRoots(t *testing.T) {
	if _, _, err := prepareAccountExecutionProof(nil); err == nil || !strings.Contains(err.Error(), "account root is nil") {
		t.Fatalf("nil account root error = %v", err)
	}

	if _, _, err := prepareAccountExecutionProof(cell.BeginCell().EndCell()); err == nil || !strings.Contains(err.Error(), "failed to decode account state") {
		t.Fatalf("malformed account root error = %v", err)
	}

	root := executionProofAccountStateRoot(t, tlb.AccountState{})
	proof, acc, err := prepareAccountExecutionProof(root)
	if err != nil {
		t.Fatalf("prepare non-existing account proof: %v", err)
	}
	if proof == nil || acc == nil || acc.IsValid {
		t.Fatalf("unexpected non-existing account proof state: proof=%v acc=%+v", proof, acc)
	}
}

func TestAttachExecutionProofNoopsAndUninitializedBuilder(t *testing.T) {
	if err := attachExecutionProof(nil, nil, nil); err != nil {
		t.Fatalf("nil result/proof should be ignored: %v", err)
	}

	res := &ExecutionResult{}
	if err := attachExecutionProof(res, nil, nil); err != nil {
		t.Fatalf("nil proof should be ignored: %v", err)
	}
	if res.Proof != nil {
		t.Fatal("nil proof should not set result proof")
	}

	if err := attachExecutionProof(res, nil, &cell.MerkleProofBuilder{}); err == nil || !strings.Contains(err.Error(), "failed to build account execution proof") {
		t.Fatalf("uninitialized proof builder error = %v", err)
	}
}

func TestExecuteDetailedWithAccountProofNonExecutableAccountsReturnProof(t *testing.T) {
	addr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")
	gas := vm.GasWithLimit(777)

	cases := []struct {
		name string
		root *cell.Cell
	}{
		{
			name: "non_existing",
			root: executionProofAccountStateRoot(t, tlb.AccountState{}),
		},
		{
			name: "uninitialized",
			root: executionProofAccountStateRoot(t, tlb.AccountState{
				IsValid:     true,
				Address:     addr,
				StorageInfo: executionProofStorageInfo(),
				AccountStorage: tlb.AccountStorage{
					Status:  tlb.AccountStatusUninit,
					Balance: tlb.FromNanoTONU(3),
				},
			}),
		},
		{
			name: "frozen",
			root: executionProofAccountStateRoot(t, tlb.AccountState{
				IsValid:     true,
				Address:     addr,
				StorageInfo: executionProofStorageInfo(),
				AccountStorage: tlb.AccountStorage{
					Status:    tlb.AccountStatusFrozen,
					Balance:   tlb.FromNanoTONU(4),
					StateHash: []byte{1, 2, 3},
				},
			}),
		},
		{
			name: "active_without_code",
			root: executionProofAccountStateRoot(t, tlb.AccountState{
				IsValid:     true,
				Address:     addr,
				StorageInfo: executionProofStorageInfo(),
				AccountStorage: tlb.AccountStorage{
					Status:  tlb.AccountStatusActive,
					Balance: tlb.FromNanoTONU(5),
					StateInit: &tlb.StateInit{
						Data: cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
					},
				},
			}),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			stack := vm.NewStack()
			res, err := NewTVM().ExecuteGetMethod(nil, nil, tuple.Tuple{}, gas, stack, ExecutionConfig{
				AccountRoot: tt.root,
				Config:      testPreparedBlockchainConfig(t),
			})
			if err != nil {
				t.Fatalf("execution proof for non-executable account: %v", err)
			}
			if res.ExitCode != accountNotInitializedExitCode {
				t.Fatalf("exit code = %d, want %d", res.ExitCode, accountNotInitializedExitCode)
			}
			if res.Gas != gas || res.Stack != stack || res.Proof == nil {
				t.Fatalf("unexpected non-executable result: %+v", res)
			}
			if _, err = cell.UnwrapProof(res.Proof, tt.root.Hash()); err != nil {
				t.Fatalf("account proof is invalid: %v", err)
			}
		})
	}
}

func TestExecuteDetailedWithAccountProofMarksResultStackCells(t *testing.T) {
	code, data := executionProofResultSliceFixture(t)
	accountRoot := executionProofAccountRoot(t, code, data)

	res, err := NewTVM().ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack(), ExecutionConfig{
		AccountRoot: accountRoot,
		Config:      testPreparedBlockchainConfig(t),
	})
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

func TestMarkExecutionProofStackMarksNestedTupleBuilderAndCellValues(t *testing.T) {
	cellValue := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xA1, 8).EndCell()).
		EndCell()
	sliceValue := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xB2, 8).EndCell()).
		EndCell()
	builderValue := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xC3, 8).EndCell()).
		EndCell()
	unusedValue := executionProofUnusedDataBranch()
	root := cell.BeginCell().
		MustStoreRef(cellValue).
		MustStoreRef(sliceValue).
		MustStoreRef(builderValue).
		MustStoreRef(unusedValue).
		EndCell()

	proof := cell.NewMerkleProofBuilder(root)
	if _, err := proof.Root().BeginParse(); err != nil {
		t.Fatalf("load proof root: %v", err)
	}

	tracedCell, err := proof.Root().PeekRef(0)
	if err != nil {
		t.Fatalf("peek traced cell branch: %v", err)
	}
	tracedSliceCell, err := proof.Root().PeekRef(1)
	if err != nil {
		t.Fatalf("peek traced slice branch: %v", err)
	}
	tracedSlice, err := tracedSliceCell.BeginParse()
	if err != nil {
		t.Fatalf("parse traced slice branch: %v", err)
	}
	tracedBuilderRef, err := proof.Root().PeekRef(2)
	if err != nil {
		t.Fatalf("peek traced builder branch: %v", err)
	}
	builder := cell.BeginCell().MustStoreRef(tracedBuilderRef)

	stack := vm.NewStack()
	if err = stack.PushCell(tracedCell); err != nil {
		t.Fatalf("push cell stack value: %v", err)
	}
	if err = stack.PushTuple(tuple.NewTupleValue(
		tuple.NewTupleValue(tracedSlice),
		tuple.NewTupleValue(builder, big.NewInt(7)),
	)); err != nil {
		t.Fatalf("push tuple stack value: %v", err)
	}

	if err = markExecutionProofStack(stack, proof.UsageTree(), nil); err != nil {
		t.Fatalf("mark proof stack: %v", err)
	}

	accountProof, err := proof.CreateProof()
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	body, err := cell.UnwrapProof(accountProof, root.Hash())
	if err != nil {
		t.Fatalf("unwrap proof: %v", err)
	}

	for idx := 0; idx < 3; idx++ {
		ref, err := body.PeekRef(idx)
		if err != nil {
			t.Fatalf("peek marked ref %d: %v", idx, err)
		}
		if ref.IsSpecial() {
			t.Fatalf("stack-marked ref %d should stay ordinary", idx)
		}
	}
	unused, err := body.PeekRef(3)
	if err != nil {
		t.Fatalf("peek unused ref: %v", err)
	}
	if !unused.IsSpecial() || unused.GetType() != cell.PrunedCellType {
		t.Fatalf("unused ref should be pruned, got special=%v type=%v", unused.IsSpecial(), unused.GetType())
	}
}

func TestMarkExecutionProofStackNoopsAndPrimitiveValues(t *testing.T) {
	if err := markExecutionProofStack(nil, cell.NewCellUsageTree(), nil); err != nil {
		t.Fatalf("nil stack should be ignored: %v", err)
	}
	stack := vm.NewStack()
	if err := stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("push int: %v", err)
	}
	if err := markExecutionProofStack(stack, nil, nil); err != nil {
		t.Fatalf("nil usage tree should be ignored: %v", err)
	}
	if err := markExecutionProofValue(nil, cell.NewCellUsageTree(), map[cell.Hash]struct{}{}); err != nil {
		t.Fatalf("nil stack value should be ignored: %v", err)
	}
	if err := markExecutionProofValue(big.NewInt(1), cell.NewCellUsageTree(), map[cell.Hash]struct{}{}); err != nil {
		t.Fatalf("primitive stack value should be ignored: %v", err)
	}
	if err := markExecutionProofValue(tuple.NewTupleValue(), cell.NewCellUsageTree(), map[cell.Hash]struct{}{}); err != nil {
		t.Fatalf("empty tuple should be ignored: %v", err)
	}
	var nullSlice *cell.Slice
	if err := markExecutionProofValue(nullSlice, cell.NewCellUsageTree(), map[cell.Hash]struct{}{}); err != nil {
		t.Fatalf("null slice reference should be ignored: %v", err)
	}
	stack = vm.NewStack()
	if err := stack.PushOwnedSlice(nullSlice); err != nil {
		t.Fatalf("push null slice reference: %v", err)
	}
	if err := markExecutionProofStack(stack, cell.NewCellUsageTree(), nil); err != nil {
		t.Fatalf("mark stack with null slice reference: %v", err)
	}
}

func TestExecuteDetailedWithAccountProofDoesNotFinalCommitGetMethodData(t *testing.T) {
	code := cell.BeginCell().MustStoreRef(executionProofCodeTail(t, opexec.RET().Serialize())).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	proofData := mustUsageProofWithLoadedRoot(t, data)
	accountRoot := executionProofAccountRoot(t, code, proofData)

	res, err := NewTVM().ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack(), ExecutionConfig{
		AccountRoot: accountRoot,
		Config:      testPreparedBlockchainConfig(t),
	})
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

func TestExecuteDetailedWithAccountProofSignatureCheckAlwaysSucceedPerRun(t *testing.T) {
	signature := make([]byte, 64)
	signature[0] = 1
	signature[63] = 2
	data := cell.BeginCell().EndCell()
	baseMachine := NewTVM()

	for _, sigCase := range executionConfigSignatureCases {
		t.Run(sigCase.name, func(t *testing.T) {
			code := makeExecutionProofSignatureCheckAlwaysVariantCode(t, sigCase, signature)
			accountRoot := executionProofAccountRoot(t, code, data)

			for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
				t.Run("v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
					machine := *baseMachine

					if version < sigCase.minVersion {
						for _, always := range []bool{false, true} {
							res := runExecutionProofSignatureCheckAlwaysVariant(t, machine, version, accountRoot, sigCase, always)
							if res.exit != vmerr.CodeInvalidOpcode {
								t.Fatalf("%s v%d always=%t exit=%d, want invalid opcode", sigCase.name, version, always, res.exit)
							}
						}
						return
					}

					for _, tt := range []struct {
						name   string
						always bool
						want   bool
					}{
						{name: "default_rejects", always: false, want: false},
						{name: "configured_accepts", always: true, want: true},
						{name: "next_default_rejects", always: false, want: false},
					} {
						t.Run(tt.name, func(t *testing.T) {
							res := runExecutionProofSignatureCheckAlwaysVariant(t, machine, version, accountRoot, sigCase, tt.always)
							if !vm.IsSuccessExitCode(res.exit) {
								t.Fatalf("%s v%d always=%t exit=%d, want success", sigCase.name, version, tt.always, res.exit)
							}
							if res.ok != tt.want {
								t.Fatalf("%s v%d always=%t result=%t, want %t", sigCase.name, version, tt.always, res.ok, tt.want)
							}
						})
					}
				})
			}
		})
	}
}

func makeExecutionProofSignatureCheckAlwaysVariantCode(t *testing.T, tt executionConfigSignatureCase, signature []byte) *cell.Cell {
	t.Helper()

	builders := []*cell.Builder{
		opstack.DROP().Serialize(),
	}
	if tt.fromSlice {
		builders = append(builders, opstack.PUSHSLICE(cell.BeginCell().MustStoreSlice([]byte{0x10, 0x20, 0x30, 0x40}, 32).ToSlice()).Serialize())
	} else {
		builders = append(builders, opstack.PUSHINT(big.NewInt(0)).Serialize())
	}

	builders = append(builders, opstack.PUSHSLICE(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()).Serialize())
	if tt.p256 {
		key := make([]byte, 33)
		key[0] = 0x05
		builders = append(builders, opstack.PUSHSLICE(cell.BeginCell().MustStoreSlice(key, 264).ToSlice()).Serialize())
	} else {
		builders = append(builders, opstack.PUSHINT(big.NewInt(2)).Serialize())
	}

	builders = append(builders, signatureCheckAlwaysVariantOpcode(t, tt))
	return codeFromBuilders(t, builders...)
}

type executionProofSignatureCheckAlwaysVariantResult struct {
	exit int64
	ok   bool
}

func runExecutionProofSignatureCheckAlwaysVariant(t *testing.T, machine TVM, version int, accountRoot *cell.Cell, tt executionConfigSignatureCase, always bool) executionProofSignatureCheckAlwaysVariantResult {
	t.Helper()

	stack := vm.NewStack()
	if err := stack.PushSmallInt(0); err != nil {
		t.Fatalf("push method id: %v", err)
	}

	res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, ExecutionConfig{
		AccountRoot:                 accountRoot,
		Config:                      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
		SignatureCheckAlwaysSucceed: always,
	})
	exit := exitCodeFromResult(res, err)
	if exit == -1 {
		t.Fatalf("execution with proof %s always=%t failed: %v", tt.name, always, err)
	}
	if res == nil {
		t.Fatalf("execution with proof %s always=%t returned nil result", tt.name, always)
	}
	if res.Proof == nil {
		t.Fatal("execution proof should be available")
	}
	if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("account execution proof is invalid: %v", err)
	}
	if !vm.IsSuccessExitCode(exit) {
		return executionProofSignatureCheckAlwaysVariantResult{exit: exit}
	}

	got, err := res.Stack.PopBool()
	if err != nil {
		t.Fatalf("pop %s result: %v", tt.name, err)
	}
	return executionProofSignatureCheckAlwaysVariantResult{exit: exit, ok: got}
}

func FuzzExecuteDetailedWithAccountProofGlobalVersionBoundary(f *testing.F) {
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(byte(version))
	}

	f.Fuzz(func(t *testing.T, rawVersion byte) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		code := codeFromBuilders(t,
			opstack.DROP().Serialize(),
			opfuncs.GASCONSUMED().Serialize(),
		)
		data := cell.BeginCell().EndCell()
		accountRoot := executionProofAccountRoot(t, code, data)
		stack := vm.NewStack()
		if err := stack.PushSmallInt(0); err != nil {
			t.Fatalf("push method id: %v", err)
		}

		machine := NewTVM()
		res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, ExecutionConfig{
			AccountRoot: accountRoot,
			Config:      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
		})
		if err != nil {
			t.Fatalf("v%d execution with proof failed: %v", version, err)
		}
		if res.Proof == nil {
			t.Fatalf("v%d execution proof is missing", version)
		}
		if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
			t.Fatalf("v%d execution proof is invalid: %v", version, err)
		}
		if version < 4 {
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("v%d exit code = %d, want invalid opcode", version, res.ExitCode)
			}
			return
		}

		if !vm.IsSuccessExitCode(res.ExitCode) {
			t.Fatalf("v%d exit code = %d, want success", version, res.ExitCode)
		}
		if _, err = res.Stack.PopInt(); err != nil {
			t.Fatalf("v%d expected GASCONSUMED result on stack: %v", version, err)
		}
	})
}

func FuzzExecuteDetailedWithAccountProofConfigGlobalVersionPerRun(f *testing.F) {
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(byte(version))
	}

	f.Fuzz(func(t *testing.T, rawVersion byte) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		code := codeFromBuilders(t,
			opstack.DROP().Serialize(),
			opfuncs.GASCONSUMED().Serialize(),
		)
		data := cell.BeginCell().EndCell()
		accountRoot := executionProofAccountRoot(t, code, data)

		machine := NewTVM()

		res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(100_000), executionProofMethodStack(t), ExecutionConfig{
			AccountRoot: accountRoot,
			Config:      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
		})
		if err != nil {
			t.Fatalf("v%d execution with proof config failed: %v", version, err)
		}
		assertExecutionProofResult(t, accountRoot, res, "configured")
		if version < 4 {
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("configured v%d exit code = %d, want invalid opcode", version, res.ExitCode)
			}
		} else {
			assertExecutionProofGasConsumedSuccess(t, res, "configured")
		}

		leakCheck, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(100_000), executionProofMethodStack(t), ExecutionConfig{
			AccountRoot: accountRoot,
			Config:      testPreparedBlockchainConfig(t),
		})
		if err != nil {
			t.Fatalf("leak-check execution with proof failed: %v", err)
		}
		assertExecutionProofResult(t, accountRoot, leakCheck, "leak-check")
		assertExecutionProofGasConsumedSuccess(t, leakCheck, "leak-check")
	})
}

func executionProofMethodStack(t *testing.T) *vm.Stack {
	t.Helper()

	stack := vm.NewStack()
	if err := stack.PushSmallInt(0); err != nil {
		t.Fatalf("push method id: %v", err)
	}
	return stack
}

func assertExecutionProofResult(t *testing.T, accountRoot *cell.Cell, res *ExecutionResult, name string) {
	t.Helper()

	if res == nil {
		t.Fatalf("%s execution proof result is nil", name)
	}
	if res.Proof == nil {
		t.Fatalf("%s execution proof is missing", name)
	}
	if _, err := cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("%s execution proof is invalid: %v", name, err)
	}
}

func assertExecutionProofGasConsumedSuccess(t *testing.T, res *ExecutionResult, name string) {
	t.Helper()

	if !vm.IsSuccessExitCode(res.ExitCode) {
		t.Fatalf("%s exit code = %d, want success", name, res.ExitCode)
	}
	if _, err := res.Stack.PopInt(); err != nil {
		t.Fatalf("%s expected GASCONSUMED result on stack: %v", name, err)
	}
}

func FuzzExecuteDetailedWithAccountProofLibraryCodeCellStartupV9Boundary(f *testing.F) {
	f.Add(uint64(0), uint8(1))
	f.Add(uint64(0x7F), uint8(2))
	f.Add(uint64(0x80), uint8(3))
	f.Add(uint64(0x11223344), uint8(4))

	f.Fuzz(func(t *testing.T, rawSeed uint64, rawCount uint8) {
		target, values := fuzzLibraryStartupTarget(t, rawSeed, rawCount)
		code := mustLibraryCellForHash(t, target.Hash())
		libraries := mustLibraryCollection(t, target)
		accountRoot := executionProofAccountRoot(t, code, cell.BeginCell().EndCell())

		missing := executeExecutionProofLibraryStartupTarget(t, 9, accountRoot)
		if missing.ExitCode != vmerr.CodeCellUnderflow {
			t.Fatalf("missing v9 library exit = %d, want cell underflow", missing.ExitCode)
		}

		legacy := executeExecutionProofLibraryStartupTarget(t, 8, accountRoot, libraries)
		direct := executeExecutionProofLibraryStartupTarget(t, 9, accountRoot, libraries)
		assertLibraryStartupStack(t, legacy, values)
		assertLibraryStartupStack(t, direct, values)

		if legacy.Steps <= direct.Steps {
			t.Fatalf("execution proof v8 steps = %d, v9 steps = %d; v8 should include an implicit jump through the code ref", legacy.Steps, direct.Steps)
		}
		if legacy.GasUsed <= direct.GasUsed {
			t.Fatalf("execution proof v8 gas = %d, v9 gas = %d; v8 should charge the implicit code ref jump", legacy.GasUsed, direct.GasUsed)
		}
	})
}

func executeExecutionProofLibraryStartupTarget(t *testing.T, version int, accountRoot *cell.Cell, libraries ...*cell.Cell) *ExecutionResult {
	t.Helper()

	machine := NewTVM()
	res, err := machine.ExecuteGetMethod(nil, nil, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), ExecutionConfig{
		AccountRoot: accountRoot,
		Libraries:   libraries,
		Config:      testPreparedBlockchainConfigWithVersion(t, uint32(version)),
	})
	if err != nil {
		t.Fatalf("ExecuteDetailedWithAccountProofWithConfig v%d: %v", version, err)
	}
	if res == nil {
		t.Fatalf("ExecuteDetailedWithAccountProofWithConfig v%d returned nil result", version)
	}
	if res.Proof == nil {
		t.Fatalf("ExecuteDetailedWithAccountProofWithConfig v%d returned no proof", version)
	}
	if _, err = cell.UnwrapProof(res.Proof, accountRoot.Hash()); err != nil {
		t.Fatalf("v%d account execution proof is invalid: %v", version, err)
	}
	return res
}

func TestExecuteGetMethodDetailedWithLibrariesDoesNotFinalCommitData(t *testing.T) {
	code := cell.BeginCell().MustStoreRef(executionProofCodeTail(t, opexec.RET().Serialize())).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	proofData := mustUsageProofWithLoadedRoot(t, data)

	res, err := NewTVM().ExecuteGetMethod(code, proofData, tuple.Tuple{}, vm.GasWithLimit(1000000), vm.NewStack(), testExecutionConfig(t))
	if err != nil {
		t.Fatalf("get method execution failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if res.Proof != nil {
		t.Fatal("get method execution without proof should not build account proof")
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
		Config:      testPreparedBlockchainConfig(t),
	})
	if err == nil {
		t.Fatal("expected account proof address mismatch")
	}
	if !strings.Contains(err.Error(), "differs from execution address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareMessageExecutionProofDisabledReturnsInputs(t *testing.T) {
	code, data := executionProofFixture(t)
	cfgLib := cell.BeginCell().MustStoreUInt(0xC7, 8).EndCell()
	addr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")

	proof, gotCode, gotData, libraries, gotAddr, err := prepareMessageExecutionProof(code, data, MessageEmulationConfig{
		Libraries: []*cell.Cell{cfgLib},
	}, addr)
	if err != nil {
		t.Fatalf("prepare message proof failed: %v", err)
	}
	if proof != nil {
		t.Fatal("proof should be nil when proof building is disabled")
	}
	if gotCode != code || gotData != data || gotAddr != addr {
		t.Fatal("disabled proof path should return original code, data, and address")
	}
	if len(libraries) != 1 || libraries[0] != cfgLib {
		t.Fatalf("libraries = %v, want original config library", libraries)
	}
}

func TestPrepareMessageExecutionProofRejectsInvalidInputs(t *testing.T) {
	code, data := executionProofFixture(t)
	accountRoot := executionProofAccountRoot(t, code, data)
	inactiveRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"),
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusUninit,
			Balance: tlb.FromNanoTONU(0),
		},
	})
	otherCode := cell.BeginCell().MustStoreUInt(0xC0, 8).EndCell()
	otherData := cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell()

	for _, tt := range []struct {
		name string
		code *cell.Cell
		data *cell.Cell
		cfg  MessageEmulationConfig
		addr *address.Address
		want string
	}{
		{
			name: "missing account root",
			code: code,
			data: data,
			cfg:  MessageEmulationConfig{BuildProof: true},
			want: "account root is required",
		},
		{
			name: "inactive account",
			code: code,
			data: data,
			cfg: MessageEmulationConfig{
				BuildProof:  true,
				AccountRoot: inactiveRoot,
			},
			want: "not an active executable",
		},
		{
			name: "code mismatch",
			code: otherCode,
			data: data,
			cfg: MessageEmulationConfig{
				BuildProof:  true,
				AccountRoot: accountRoot,
			},
			want: "code differs",
		},
		{
			name: "data mismatch",
			code: code,
			data: otherData,
			cfg: MessageEmulationConfig{
				BuildProof:  true,
				AccountRoot: accountRoot,
			},
			want: "data differs",
		},
		{
			name: "address mismatch",
			code: code,
			data: data,
			cfg: MessageEmulationConfig{
				BuildProof:  true,
				AccountRoot: accountRoot,
			},
			addr: tonopsTestAddr,
			want: "differs from execution address",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, _, _, err := prepareMessageExecutionProof(tt.code, tt.data, tt.cfg, tt.addr)
			if err == nil {
				t.Fatal("expected prepare message proof error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPrepareMessageExecutionProofUsesAccountStateAndAppendsLibraries(t *testing.T) {
	accountCode := cell.BeginCell().MustStoreUInt(0xC0, 8).EndCell()
	accountData := cell.BeginCell().MustStoreUInt(0xD0, 8).EndCell()
	accountLibs, accountLibRoot := transactionTestLibraryDict(0xA7)
	cfgLib := cell.BeginCell().MustStoreUInt(0xC7, 8).EndCell()
	cfgLibraries := []*cell.Cell{cfgLib}
	accountAddr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")
	accountRoot := executionProofAccountStateRoot(t, tlb.AccountState{
		IsValid:     true,
		Address:     accountAddr,
		StorageInfo: executionProofStorageInfo(),
		AccountStorage: tlb.AccountStorage{
			Status:  tlb.AccountStatusActive,
			Balance: tlb.FromNanoTONU(0),
			StateInit: &tlb.StateInit{
				Code: accountCode,
				Data: accountData,
				Lib:  accountLibs,
			},
		},
	})

	proof, gotCode, gotData, libraries, gotAddr, err := prepareMessageExecutionProof(nil, nil, MessageEmulationConfig{
		BuildProof:  true,
		AccountRoot: accountRoot,
		Libraries:   cfgLibraries,
	}, nil)
	if err != nil {
		t.Fatalf("prepare message proof failed: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should be created")
	}
	if gotCode == nil || gotCode.HashKey() != accountCode.HashKey() {
		t.Fatal("proof path should use account code when explicit code is nil")
	}
	if gotData == nil || gotData.HashKey() != accountData.HashKey() {
		t.Fatal("proof path should use account data when explicit data is nil")
	}
	if gotAddr == nil || !gotAddr.Equals(accountAddr) {
		t.Fatalf("address = %v, want account address %v", gotAddr, accountAddr)
	}
	if len(libraries) != 2 {
		t.Fatalf("libraries len = %d, want 2", len(libraries))
	}
	if libraries[0] != cfgLib {
		t.Fatal("first library should be the config library")
	}
	if libraries[1] == nil || libraries[1].HashKey() != accountLibRoot.HashKey() {
		t.Fatal("second library should be the account state library root")
	}
	if len(cfgLibraries) != 1 || cfgLibraries[0] != cfgLib {
		t.Fatal("config libraries should not be mutated")
	}
}

func TestExecutionProofAddressBoundaries(t *testing.T) {
	accountAddr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")

	if got, err := executionProofAddress(nil, nil); err == nil {
		t.Fatalf("execution proof address = %v, want missing account address error", got)
	} else if !strings.Contains(err.Error(), "address is missing") {
		t.Fatalf("unexpected missing account address error: %v", err)
	}

	got, err := executionProofAddress(nil, accountAddr)
	if err != nil {
		t.Fatalf("nil selected address should use account address: %v", err)
	}
	if got != accountAddr {
		t.Fatal("nil selected address should return account address")
	}

	got, err = executionProofAddress(accountAddr, accountAddr)
	if err != nil {
		t.Fatalf("matching selected address failed: %v", err)
	}
	if got != accountAddr {
		t.Fatal("matching selected address should be returned")
	}

	varAddr := address.NewAddressVar(0, accountAddr.Workchain(), accountAddr.BitsLen(), accountAddr.Data())
	if got, err = executionProofAddress(varAddr, accountAddr); err == nil {
		t.Fatalf("execution proof address = %v, want type mismatch error", got)
	} else if !strings.Contains(err.Error(), "differs from execution address") {
		t.Fatalf("unexpected type mismatch error: %v", err)
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
