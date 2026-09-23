package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func historicalActionLibraryValidationActions(t *testing.T) *cell.Cell {
	t.Helper()

	// The library field points to an empty, malformed Hashmap 256 SimpleLib.
	state := cell.BeginCell().MustStoreUInt(1, 5).MustStoreRef(cell.BeginCell().EndCell()).EndCell()
	message := transactionStateInitLibraryTestMessage(state, false, true)
	return buildTransactionActionList(t, tlb.ActionSendMsg{Msg: message})
}

func TestHistoricalActionLibraryValidationScope(t *testing.T) {
	actions := historicalActionLibraryValidationActions(t)
	for version := uint32(0); version <= vm.MaxSupportedGlobalVersion; version++ {
		for _, historical := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%d/historical=%t", version, historical), func(t *testing.T) {
				loaded, err := transactionLoadActions(actions, version, historical)
				if err != nil {
					t.Fatal(err)
				}
				invalid := historical && version <= 3
				wantCode := int32(0)
				if invalid {
					wantCode = 34
				}
				if loaded.resultCode != wantCode || loaded.totalActions != 1 || loaded.skippedActions != 0 || loaded.bounce {
					t.Fatalf("prepass=%+v, want code %d", loaded, wantCode)
				}
				if invalid {
					if loaded.resultArg != nil || len(loaded.actions) != 0 {
						t.Fatalf("unexpected historical failure position/actions: %+v", loaded)
					}
				} else if loaded.resultArg != nil || len(loaded.actions) != 1 {
					t.Fatalf("modern prepass rejected opaque libraries: %+v", loaded)
				}
			})
		}
	}
}

func TestHistoricalActionLibraryValidationEntrypoints(t *testing.T) {
	actions := historicalActionLibraryValidationActions(t)
	code := codeFromBuilders(t, stackop.PUSHREF(actions).Serialize(), execop.POPCTR(5).Serialize())
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), 1_000_000_000, now)
	account, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(buildTransactionOutboundInternalCellWithAddresses(t,
		internalEmulationSrcAddr, tonopsTestAddr, 1000, cell.BeginCell().EndCell()))
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []uint32{0, 3} {
		block, err := transactionTestConfigWithGlobalVersion(t, version).NewBlockContext(BlockOptions{
			Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"ordinary", "tick", "tock"} {
			t.Run(fmt.Sprintf("v%d/%s", version, kind), func(t *testing.T) {
				machine := NewTVM()
				for _, historical := range []bool{false, true, false} {
					opts := TransactionOptions{LogicalTime: transactionTestLogicalTime, HistoricalActionLibraryValidation: historical}
					var result *TransactionExecutionResult
					if kind == "ordinary" {
						result, err = machine.EmulateTransaction(block, account, message, opts)
					} else {
						result, err = machine.EmulateTickTockTransaction(block, account, kind == "tock", opts)
					}
					if err != nil {
						t.Fatal(err)
					}
					transaction, err := result.ParseTransaction()
					if err != nil {
						t.Fatal(err)
					}
					var phase *tlb.ActionPhase
					switch desc := transaction.Description.(type) {
					case tlb.TransactionDescriptionOrdinary:
						phase = desc.ActionPhase
					case tlb.TransactionDescriptionTickTock:
						phase = desc.ActionPhase
					default:
						t.Fatalf("unexpected transaction description %T", desc)
					}
					if phase == nil || phase.Valid == historical || phase.Success || phase.ResultCode != 34 || phase.SkippedActions != 0 || len(result.OutMessages) != 0 {
						t.Fatalf("historical=%t: phase=%+v, outgoing=%d", historical, phase, len(result.OutMessages))
					}
				}
			})
		}
	}
}
