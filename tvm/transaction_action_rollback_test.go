package tvm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func TestTransactionDeleteRequestCommitsOnlyOnSuccess(t *testing.T) {
	for _, version := range []uint32{0, 2, 3, 4, 9, 10, 15} {
		for _, failure := range []int32{0, 37, 50} {
			t.Run(fmt.Sprintf("v%d/code%d", version, failure), func(t *testing.T) {
				cfg := transactionTestConfigWithGlobalVersion(t, version)
				data := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
				actions := []any{tlb.ActionSendMsg{Mode: 160, Msg: buildTransactionOutboundInternalCell(t, 0)}}
				if failure == 37 {
					actions = append(actions, tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCell(t, 1)})
				}
				if failure == 50 {
					cfg = actionRegressionLimitedConfig(t, version, 1, 1000, 1<<13)
				}
				out := applyActionRegression(t, cfg, data, buildTransactionActionList(t, actions...), false)
				wantDelete := failure == 0
				wantMessages := 0
				if wantDelete {
					wantMessages = 1
				}
				if out.phase.ResultCode != failure || out.phase.Success != wantDelete || out.deleteAccount != wantDelete {
					t.Fatalf("phase=%+v delete=%t, want code=%d delete=%t", out.phase, out.deleteAccount, failure, wantDelete)
				}
				if (out.phase.StatusChange.Type == tlb.AccStatusChangeDeleted) != wantDelete || len(out.outMsgs) != wantMessages {
					t.Fatalf("status=%v messages=%d, want deletion=%t", out.phase.StatusChange, len(out.outMsgs), wantDelete)
				}
				if !wantDelete && out.balance.Cmp(big.NewInt(1000)) != 0 {
					t.Fatalf("failed actions retained send debit: balance=%v", out.balance)
				}
			})
		}
	}
}

func TestTransactionDeleteRequestRollbackEntrypoints(t *testing.T) {
	for _, kind := range []string{"ordinary", "tick", "tock"} {
		for _, failure := range []int32{0, 37, 50} {
			t.Run(fmt.Sprintf("%s/code%d", kind, failure), func(t *testing.T) {
				oldData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
				newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
				actions := []any{tlb.ActionSendMsg{Mode: 160, Msg: buildTransactionOutboundInternalCell(t, 0)}}
				if failure == 37 {
					actions = append(actions, tlb.ActionSendMsg{Mode: 1, Msg: buildTransactionOutboundInternalCell(t, 1)})
				}
				cfg := transactionTestConfigWithGlobalVersion(t, 2)
				if failure == 50 {
					cfg = actionRegressionLimitedConfig(t, 2, 1, 1000, 1<<13)
				}
				result := emulateActionRegression(t, NewTVM(), kind, cfg, buildTransactionActionList(t, actions...), oldData, newData, false)
				tx, err := result.ParseTransaction()
				if err != nil {
					t.Fatal(err)
				}
				var phase *tlb.ActionPhase
				var destroyed, aborted bool
				switch desc := tx.Description.(type) {
				case tlb.TransactionDescriptionOrdinary:
					phase, destroyed, aborted = desc.ActionPhase, desc.Destroyed, desc.Aborted
				case tlb.TransactionDescriptionTickTock:
					phase, destroyed, aborted = desc.ActionPhase, desc.Destroyed, desc.Aborted
				default:
					t.Fatalf("unexpected description %T", desc)
				}
				wantDelete := failure == 0
				wantMessages := 0
				if wantDelete {
					wantMessages = 1
				}
				if phase == nil || phase.ResultCode != failure || destroyed != wantDelete || aborted == wantDelete || len(result.OutMessages) != wantMessages {
					t.Fatalf("phase=%+v destroyed=%t aborted=%t out=%d", phase, destroyed, aborted, len(result.OutMessages))
				}
				if !wantDelete {
					state := result.NextAccount.State()
					if state.Status != tlb.AccountStatusActive || state.StateInit == nil || !transactionCellEqual(state.StateInit.Data, oldData) {
						t.Fatal("failed action phase lost the active account state")
					}
				}
			})
		}
	}
}

func actionRegressionLimitedConfig(t *testing.T, version, accountCells, libraryCells, messageCells uint32) *PreparedBlockchainConfig {
	t.Helper()
	return transactionTestConfigWithParams(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: transactionTestGlobalVersionCell(t, version),
		tlb.ConfigParamSizeLimits:    buildTransactionSizeLimitsCell(t, 1<<21, messageCells, libraryCells, accountCells, accountCells),
	})
}

func applyActionRegression(t *testing.T, cfg *PreparedBlockchainConfig, data, actions *cell.Cell, historical bool) *transactionActionApplyResult {
	t.Helper()
	acc := &transactionRuntimeAccount{
		addr: tonopsTestAddr, status: tlb.AccountStatusActive,
		code: cell.BeginCell().EndCell(), data: cell.BeginCell().EndCell(), balance: big.NewInt(1000),
	}
	res := &MessageExecutionResult{Accepted: true, ExecutionResult: ExecutionResult{Data: data, Actions: actions, Committed: true}}
	out, err := transactionApplyActions(acc, res, uint64(transactionTestLogicalTime), uint32(tonopsTestTime.Unix()), cfg, big.NewInt(1000), nil, transactionZeroCurrencyBalance(), big.NewInt(0), preV9TestOriginalBalance(t, big.NewInt(1000), nil), historical)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func emulateActionRegression(t *testing.T, machine *TVM, kind string, cfg *PreparedBlockchainConfig, actions, oldData, newData *cell.Cell, historical bool) *TransactionExecutionResult {
	t.Helper()
	code := codeFromBuilders(t,
		stackop.PUSHREF(actions).Serialize(), execop.POPCTR(5).Serialize(),
		stackop.PUSHREF(newData).Serialize(), execop.POPCTR(4).Serialize(),
	)
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, oldData, 1_000_000_000, now)
	acc, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	block, err := cfg.NewBlockContext(BlockOptions{Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed})
	if err != nil {
		t.Fatal(err)
	}
	opts := TransactionOptions{LogicalTime: transactionTestLogicalTime, HistoricalNoActionStateLimits: historical}
	var result *TransactionExecutionResult
	if kind == "ordinary" {
		message, prepErr := PrepareMessage(buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1000, cell.BeginCell().EndCell()))
		if prepErr != nil {
			t.Fatal(prepErr)
		}
		result, err = machine.EmulateTransaction(block, acc, message, opts)
	} else {
		result, err = machine.EmulateTickTockTransaction(block, acc, kind == "tock", opts)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.ExitCode != 0 || result.NextAccount == nil {
		t.Fatalf("unexpected compute result: accepted=%t exit=%d", result.Accepted, result.ExitCode)
	}
	return result
}
