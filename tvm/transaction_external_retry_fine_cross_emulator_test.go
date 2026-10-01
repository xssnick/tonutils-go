//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorTransactionExternalSendPackingRetries(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library unavailable: %v", err)
	}

	base := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().EndCell()
	prices, err := tlb.ToCell(&tlb.ConfigMsgForwardPrices{CellPrice: 4 << 16})
	if err != nil {
		t.Fatal(err)
	}
	gasPrices := buildTransactionGasLimitsCell(t, 1_000_000, 10_000_000)
	outgoing := buildTransactionExternalPackingRetryMessage(t)
	// The incoming nanoton pays for compute, leaving precisely the test budget.
	inbound := buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1, data)

	for version := uint32(0); version <= vm.MaxSupportedGlobalVersion; version++ {
		config := referenceTransactionConfigRootWithGlobalVersion(t, base, version)
		config = referenceTransactionConfigRootWithOverrides(t, config, map[int32]*cell.Cell{
			int32(tlb.ConfigParamMsgForwardPricesBasechain):   prices,
			int32(tlb.ConfigParamMsgForwardPricesMasterchain): prices,
			int32(tlb.ConfigParamGasPricesBasechain):          gasPrices,
			int32(tlb.ConfigParamGasPricesMasterchain):        gasPrices,
		})
		for _, tc := range transactionExternalPackingRetryCases {
			for _, mode := range []uint8{0, 1, 2, 3} {
				t.Run(fmt.Sprintf("v%d/%s/mode%d", version, tc.name, mode), func(t *testing.T) {
					actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outgoing})
					code := makeTransactionInternalActionsCode(t, actions, data)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, tc.budget, now)
					reference, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, inbound, now,
						uint64(transactionTestLogicalTime), tonopsTestSeed, config)
					if err != nil {
						t.Fatal(err)
					}
					got, err := testEmulateTransaction(NewTVM(), shard, inbound, testTxParams{
						Address: tonopsTestAddr, Now: now, BlockLT: transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: config,
					})
					if err != nil {
						t.Fatal(err)
					}
					var tx tlb.Transaction
					if err := tlb.Parse(&tx, reference.txCell); err != nil {
						t.Fatal(err)
					}
					desc := tx.Description.(tlb.TransactionDescriptionOrdinary)
					compute, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
					if !ok || !compute.Success || compute.GasFees.Nano().Uint64() != 1 {
						t.Fatalf("reference compute must succeed for one nanoton: %+v", desc.ComputePhase)
					}
					phase := desc.ActionPhase
					wantSuccess := mode&2 != 0 || tc.messages != 0
					if phase == nil || !phase.Valid || phase.Success != wantSuccess || phase.MessagesCreated != tc.messages {
						t.Fatalf("reference action phase=%+v, want success=%t messages=%d", phase, wantSuccess, tc.messages)
					}
					fees := tc.fine
					if version < 4 {
						fees = 0
					}
					if tc.messages != 0 {
						fees = 16
					}
					assertOrdinaryTransactionActionFees(t, "reference", reference.txCell, fees)
					if got.TransactionCell.HashKey() != reference.txCell.HashKey() ||
						got.NextAccount.ShardAccountCell().HashKey() != reference.shardCell.HashKey() {
						t.Fatalf("external packing retry differs: transaction Go=%x reference=%x; account Go=%x reference=%x",
							got.TransactionCell.HashKey(), reference.txCell.HashKey(),
							got.NextAccount.ShardAccountCell().HashKey(), reference.shardCell.HashKey())
					}
				})
			}
		}
	}
}
