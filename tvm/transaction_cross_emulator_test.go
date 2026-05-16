//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestTVMCrossEmulatorTransactionExternal(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	configRoot := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	code := makeTransactionExternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, uint32(tonopsTestTime.Unix()))

	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	goRes, err := NewTVM().EmulateTransaction(shard, msgCell, TransactionEmulationConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	if !goRes.Accepted {
		t.Fatal("expected go transaction emulation to accept external message")
	}

	refRes, err := runReferenceOrdinaryTransaction(shard, msgCell, uint32(tonopsTestTime.Unix()), uint64(transactionTestLogicalTime), tonopsTestSeed)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.ExitCode != refRes.exitCode {
		t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
	}
	if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
	}

	switch {
	case goRes.Actions == nil && refRes.actionsCell == nil:
	case goRes.Actions == nil || refRes.actionsCell == nil:
		t.Fatalf("actions presence mismatch: go=%v reference=%v", goRes.Actions != nil, refRes.actionsCell != nil)
	default:
		if !bytes.Equal(goRes.Actions.Hash(), refRes.actionsCell.Hash()) {
			t.Fatalf("actions hash mismatch:\ngo=%s\nreference=%s", goRes.Actions.Dump(), refRes.actionsCell.Dump())
		}
	}
}

func TestTVMCrossEmulatorTransactionEdges(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	configRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	lowValue := uint64(777)
	richValue := uint64(1_000_000_000)
	outMsg := buildTransactionOutboundInternalCell(t, 1000)
	overspendMsg := buildTransactionOutboundInternalCell(t, 2_000_000_000)
	carryMsg := buildTransactionOutboundInternalCell(t, 0)
	largeBodyMsg := buildTransactionOutboundInternalCellWithBody(t, 1000, buildTransactionZeroBitsCell(t, 600))
	invalidSrcMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x22}, 32)), tonopsTestAddr, 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	extDestInternalMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x33}, 32)), 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	varDestInternalMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), address.NewAddressVar(0, tonopsTestAddr.Workchain(), 256, tonopsTestAddr.Data()), 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	shortVarDestInternalMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), address.NewAddressVar(0, tonopsTestAddr.Workchain(), 255, tonopsTestAddr.Data()), 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	unknownWorkchainDestMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), address.NewAddress(0, 42, bytes.Repeat([]byte{0x42}, 32)), 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	wrongAnycastPrefix := []byte{tonopsTestAddr.Data()[0] & 0x80}
	wrongAnycastPrefix[0] ^= 0x80
	anycastDestInternalMsg := buildTransactionOutboundInternalCellWithAddresses(t, address.NewAddressNone(), tonopsTestAddr.WithAnycast(address.NewAnycast(1, wrongAnycastPrefix)), 1000, cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell())
	stateInitData := cell.BeginCell().MustStoreUInt(0xACDC, 16).EndCell()
	stateInitNewData := cell.BeginCell().MustStoreUInt(0xDCAC, 16).EndCell()
	stateInit := &tlb.StateInit{
		Code: makeTransactionInternalSuccessCode(t, stateInitNewData),
		Data: stateInitData,
	}
	stateInitAddr := stateInit.CalcAddress(0)
	stateInitCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init cell: %v", err)
	}
	badStateHash := bytes.Repeat([]byte{0x77}, 32)
	storageDebt := tlb.FromNanoTONU(1000)
	storageDebtInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   &storageDebt,
	}
	storageDebtConfig := referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamGasPricesBasechain):   buildTransactionGasLimitsCell(t, 100, 500),
		int32(tlb.ConfigParamGasPricesMasterchain): buildTransactionGasLimitsCell(t, 100, 500),
	})
	nextCode := cell.BeginCell().MustStoreUInt(0xD00D, 16).EndCell()
	libCode := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	tooManyActions := buildTransactionRepeatedSetCodeActions(t, nextCode, 256)
	malformedSkipAction := buildTransactionMalformedSendAction(2)
	malformedBounceAction := buildTransactionMalformedSendAction(16)
	invalidExtOutMsg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: address.MustParseRawAddr(
			"0:1111111111111111111111111111111111111111111111111111111111111111",
		),
		Body: cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build invalid external outbound message: %v", err)
	}
	extOutMsg, err := tlb.ToCell(&tlb.ExternalMessageOut{
		SrcAddr: address.NewAddressNone(),
		DstAddr: address.NewAddressExt(0, 256, bytes.Repeat([]byte{0x11}, 32)),
		Body:    cell.BeginCell().MustStoreUInt(0xE0, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("failed to build external outbound message: %v", err)
	}

	tests := []struct {
		name       string
		addr       *address.Address
		shard      *tlb.ShardAccount
		msg        *cell.Cell
		configRoot *cell.Cell
	}{
		{
			name:  "internal_no_gas_credit",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(lowValue),
				Body:        body,
			}),
		},
		{
			name:  "internal_execute_credit",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "bounceable_no_state_low_value",
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1000),
				Body:        body,
			}),
		},
		{
			name:  "bounceable_no_state_rich_value",
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "non_bounceable_no_state_rich_value",
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "internal_stateinit_activates_nonexist",
			addr:  stateInitAddr,
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:  "internal_stateinit_bad_hash",
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:  "frozen_stateinit_reactivates",
			addr:  stateInitAddr,
			shard: buildTransactionTestFrozenShardAccount(t, stateInitAddr, stateInitCell.Hash(), walletSendTestBalance, tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}, LastPaid: now}),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:  "frozen_stateinit_bad_hash_bounces",
			addr:  stateInitAddr,
			shard: buildTransactionTestFrozenShardAccount(t, stateInitAddr, badStateHash, walletSendTestBalance, tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}, LastPaid: now}),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:       "storage_debt_freezes_active",
			shard:      buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, 50, storageDebtInfo),
			configRoot: storageDebtConfig,
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(0),
				Body:        body,
			}),
		},
		{
			name:       "storage_debt_deletes_uninit",
			shard:      buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 50, storageDebtInfo),
			configRoot: storageDebtConfig,
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(0),
				Body:        body,
			}),
		},
		{
			name:  "compute_stack_underflow_bounce",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_action_success",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, outMsg, newData, 0), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_invalid_src",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, invalidSrcMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_invalid_src_mode2_skip",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, invalidSrcMsg, newData, 3), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_ext_dest_rejected",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, extDestInternalMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_var_dest_repacked",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, varDestInternalMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_unknown_workchain_dest",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, unknownWorkchainDestMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_short_var_dest_rejected",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, shortVarDestInternalMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_internal_anycast_dest_rejected_v13",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, anycastDestInternalMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_action_mode64_pay_fees",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, carryMsg, newData, 65), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_action_mode2_nofunds_skip",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, overspendMsg, newData, 2), origData, 500, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_action_nofunds_bounce_on_fail",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, overspendMsg, newData, 16), origData, 500, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_action_all_balance_delete",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, carryMsg, newData, 160), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_action_body_repacked_to_ref",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, largeBodyMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_external_out_invalid_dest",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, invalidExtOutMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "send_external_out_success",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, extOutMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "rawreserve_limits_later_send",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalReserveSendCode(t, 10_800_000_000, 0, outMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "rawreserve_mode2_clamps",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalReserveSendCode(t, 20_000_000_000, 2, outMsg, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "rawreserve_mode5_keep_amount",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalReserveOnlyCode(t, 5_000_000, 5, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "rawreserve_mode12_invalid_bounce",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalReserveOnlyCode(t, 20_000_000_000, 28, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "setcode_action_success",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSetCodeCode(t, nextCode, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "setlib_private_success",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSetLibCode(t, libCode, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "changelib_missing_hash",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalChangeLibMissingCode(t, newData, 1), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "too_many_actions_256",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, tooManyActions, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "malformed_send_mode2_skip",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, malformedSkipAction, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
		{
			name:  "malformed_send_mode16_bounce",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, malformedBounceAction, newData), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(richValue),
				Body:        body,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testConfigRoot := configRoot
			if tt.configRoot != nil {
				testConfigRoot = tt.configRoot
			}
			testAddr := tonopsTestAddr
			if tt.addr != nil {
				testAddr = tt.addr
			}
			goRes, err := NewTVM().EmulateTransaction(tt.shard, tt.msg, TransactionEmulationConfig{
				Address:     testAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  testConfigRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			if goRes.TransactionCell == nil || goRes.ShardAccountCell == nil {
				t.Fatal("go transaction emulation did not produce cells")
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(tt.shard, tt.msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, testConfigRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Logf("shard account mismatch context:\ngo=%s\nreference=%s\ngo summary=%s\nreference summary=%s\ngo tx=%s\nreference tx=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump(), transactionCrossShardSummary(t, goRes.ShardAccountCell), transactionCrossShardSummary(t, refRes.shardCell), transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionC7Options(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	configRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	prevBlocks := tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333))
	libraryTarget := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	libraryRef := mustCrossLibraryCellForHash(t, libraryTarget.Hash())
	libraryCollection := mustCrossLibraryCollection(t, libraryTarget)

	txCode := func(builders ...*cell.Builder) *cell.Cell {
		all := make([]*cell.Builder, 0, 5+len(builders))
		for range 5 {
			all = append(all, stackop.DROP().Serialize())
		}
		all = append(all, builders...)
		return codeFromBuilders(t, all...)
	}

	tests := []struct {
		name       string
		code       *cell.Cell
		prevBlocks tuple.Tuple
		refLibs    *cell.Cell
		goLibs     []*cell.Cell
	}{
		{
			name: "prev_blocks_info",
			code: txCode(
				funcsop.PREVMCBLOCKS_100().Serialize(),
				cellsliceop.NEWC().Serialize(),
				cellsliceop.STU(16).Serialize(),
				cellsliceop.ENDC().Serialize(),
				execop.POPCTR(4).Serialize(),
			),
			prevBlocks: prevBlocks,
		},
		{
			name: "library_collection",
			code: txCode(
				stackop.PUSHREF(libraryRef).Serialize(),
				cellsliceop.XLOADQ().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.HASHCU().Serialize(),
				cellsliceop.NEWC().Serialize(),
				cellsliceop.STU(256).Serialize(),
				cellsliceop.ENDC().Serialize(),
				execop.POPCTR(4).Serialize(),
			),
			refLibs: libraryCollection,
			goLibs:  []*cell.Cell{libraryCollection},
		},
	}

	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        body,
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shard := buildTransactionTestShardAccount(t, tonopsTestAddr, tt.code, origData, walletSendTestBalance, now)
			goRes, err := NewTVM().EmulateTransaction(shard, msg, TransactionEmulationConfig{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
				PrevBlocks:  tt.prevBlocks,
				Libraries:   tt.goLibs,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot, referenceTransactionOptions{
				libs:       tt.refLibs,
				prevBlocks: tt.prevBlocks,
			})
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.ExitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.ExitCode, refRes.exitCode)
			}
			if goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.ShardAccountCell.Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.ShardAccountCell.Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func transactionCrossTxSummary(t *testing.T, txCell *cell.Cell) string {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		return fmt.Sprintf("decode tx failed: %v", err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		return fmt.Sprintf("desc=%T", tx.Description)
	}

	compute := fmt.Sprintf("%T", desc.ComputePhase.Phase)
	if skipped, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped); ok {
		compute = fmt.Sprintf("skip:%s", skipped.Reason.Type)
	} else if vmPhase, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM); ok {
		compute = fmt.Sprintf(
			"vm:success=%t msg_state=%t activated=%t gas=%s/%s credit=%s exit=%d steps=%d",
			vmPhase.Success,
			vmPhase.MsgStateUsed,
			vmPhase.AccountActivated,
			vmPhase.Details.GasUsed.String(),
			vmPhase.Details.GasLimit.String(),
			transactionCrossBigIntPtr(vmPhase.Details.GasCredit),
			vmPhase.Details.ExitCode,
			vmPhase.Details.VMSteps,
		)
	}
	bounce := "<nil>"
	if desc.BouncePhase != nil {
		switch phase := desc.BouncePhase.Phase.(type) {
		case tlb.BouncePhaseNoFunds:
			bounce = fmt.Sprintf("nofunds cells=%s bits=%s req=%s", phase.MsgSize.Cells.String(), phase.MsgSize.Bits.String(), phase.ReqFwdFees.Nano().String())
		case tlb.BouncePhaseOk:
			bounce = fmt.Sprintf("ok cells=%s bits=%s fees=%s fwd=%s", phase.MsgSize.Cells.String(), phase.MsgSize.Bits.String(), phase.MsgFees.Nano().String(), phase.FwdFees.Nano().String())
		default:
			bounce = fmt.Sprintf("%T", phase)
		}
	}
	action := "<nil>"
	if desc.ActionPhase != nil {
		action = fmt.Sprintf(
			"success=%t valid=%t code=%d arg=%s total=%d spec=%d skipped=%d msgs=%d nofunds=%t fwd=%s act=%s size=%s/%s",
			desc.ActionPhase.Success,
			desc.ActionPhase.Valid,
			desc.ActionPhase.ResultCode,
			transactionCrossInt32Ptr(desc.ActionPhase.ResultArg),
			desc.ActionPhase.TotalActions,
			desc.ActionPhase.SpecActions,
			desc.ActionPhase.SkippedActions,
			desc.ActionPhase.MessagesCreated,
			desc.ActionPhase.NoFunds,
			transactionCrossCoinsPtr(desc.ActionPhase.TotalFwdFees),
			transactionCrossCoinsPtr(desc.ActionPhase.TotalActionFees),
			desc.ActionPhase.TotalMsgSize.Cells.String(),
			desc.ActionPhase.TotalMsgSize.Bits.String(),
		)
	}
	return fmt.Sprintf("credit_first=%t compute=%s action=%s aborted=%t bounce=%s destroyed=%t", desc.CreditFirst, compute, action, desc.Aborted, bounce, desc.Destroyed)
}

func transactionCrossInt32Ptr(v *int32) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *v)
}

func transactionCrossBigIntPtr(v *big.Int) string {
	if v == nil {
		return "<nil>"
	}
	return v.String()
}

func transactionCrossCoinsPtr(v *tlb.Coins) string {
	if v == nil {
		return "<nil>"
	}
	return v.Nano().String()
}

func transactionCrossShardSummary(t *testing.T, shardCell *cell.Cell) string {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.LoadFromCell(&shard, shardCell.MustBeginParse()); err != nil {
		return fmt.Sprintf("decode shard failed: %v", err)
	}
	var acc tlb.AccountState
	if err := tlb.LoadFromCell(&acc, shard.Account.MustBeginParse()); err != nil {
		return fmt.Sprintf("decode account failed: %v", err)
	}
	if !acc.IsValid {
		return fmt.Sprintf("invalid account last_tx_lt=%d", shard.LastTransLT)
	}
	return fmt.Sprintf("status=%s balance=%s last_tx_lt=%d last_paid=%d cells=%s bits=%s due=%v", acc.Status, acc.Balance.Nano().String(), acc.LastTransactionLT, acc.StorageInfo.LastPaid, acc.StorageInfo.StorageUsed.CellsUsed.String(), acc.StorageInfo.StorageUsed.BitsUsed.String(), acc.StorageInfo.DuePayment)
}

func buildTransactionMalformedSendAction(mode uint8) *cell.Cell {
	return cell.BeginCell().
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(uint64(mode), 8).
		EndCell()
}

func mustTransactionMsgCell(t *testing.T, msg any) *cell.Cell {
	t.Helper()

	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatalf("failed to build transaction message: %v", err)
	}
	return msgCell
}
