//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionSendPackingRetries(t *testing.T) {
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
	state := &tlb.StateInit{
		Code: cell.BeginCell().MustStoreUInt(41, 8).EndCell(),
		Data: cell.BeginCell().MustStoreUInt(59, 9).EndCell(),
	}
	outgoing, layout, err := transactionInternalMessageToCellWithLayout(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(100),
		StateInit:   state,
		Body:        cell.BeginCell().MustStoreSlice(make([]byte, 63), 500).EndCell(),
	}, transactionOutboundLayout{})
	if err != nil || layout != (transactionOutboundLayout{}) {
		t.Fatalf("initial message must fit inline: layout=%+v err=%v", layout, err)
	}
	// One incoming nanoton pays for compute, leaving exactly 100 + feeBudget
	// for the send. Four nanotons per cell give an action fine of one per cell.
	inbound := buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1, data)
	for _, version := range []uint32{4, 6, 8, 10, 15, 16} {
		config := referenceTransactionConfigRootWithGlobalVersion(t, base, version)
		config = referenceTransactionConfigRootWithOverrides(t, config, map[int32]*cell.Cell{
			int32(tlb.ConfigParamMsgForwardPricesBasechain):   prices,
			int32(tlb.ConfigParamMsgForwardPricesMasterchain): prices,
			int32(tlb.ConfigParamGasPricesBasechain):          gasPrices,
			int32(tlb.ConfigParamGasPricesMasterchain):        gasPrices,
		})
		for _, tc := range []struct {
			name      string
			feeBudget uint64
			fine      uint64
			messages  uint16
		}{
			{name: "original", feeBudget: 7, fine: 2},
			{name: "state_init_ref", feeBudget: 10, fine: 3},
			{name: "body_ref", feeBudget: 14, fine: 4},
			{name: "success", feeBudget: 20, messages: 1},
		} {
			for _, mode := range []uint8{1, 3} {
				t.Run(fmt.Sprintf("v%d/%s/mode%d", version, tc.name, mode), func(t *testing.T) {
					actions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: mode, Msg: outgoing})
					code := makeTransactionInternalActionsCode(t, actions, data)
					shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, 100+tc.feeBudget, now)
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
						t.Fatalf("reference action phase=%+v, want success=%t messages=%d fine=%d", phase, wantSuccess, tc.messages, tc.fine)
					}
					if tc.fine == 0 {
						if phase.TotalActionFees != nil {
							t.Fatalf("reference action fees=%v, want absent", phase.TotalActionFees)
						}
					} else if phase.TotalActionFees == nil || phase.TotalActionFees.Nano().Uint64() != tc.fine {
						t.Fatalf("reference action fine=%v, want %d", phase.TotalActionFees, tc.fine)
					}
					if !wantSuccess && (phase.ResultCode != 37 || !phase.NoFunds) {
						t.Fatalf("reference must reject send with insufficient funds: %+v", phase)
					}
					if got.TransactionCell.HashKey() != reference.txCell.HashKey() ||
						got.NextAccount.ShardAccountCell().HashKey() != reference.shardCell.HashKey() {
						t.Fatalf("packing retry differs: transaction Go=%x reference=%x; account Go=%x reference=%x",
							got.TransactionCell.HashKey(), reference.txCell.HashKey(),
							got.NextAccount.ShardAccountCell().HashKey(), reference.shardCell.HashKey())
					}
				})
			}
		}
	}
}
