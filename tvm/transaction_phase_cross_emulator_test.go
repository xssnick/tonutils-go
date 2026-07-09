//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorTransactionNonComputePhaseParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	configRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	largeBody := buildTransactionZeroBitsCell(t, 520)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x42}, 32))
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build config address cell: %v", err)
	}
	stateInitData := cell.BeginCell().MustStoreUInt(0xACDC, 16).EndCell()
	stateInitNewData := cell.BeginCell().MustStoreUInt(0xDCAC, 16).EndCell()
	gasOverrideAddr := address.MustParseRawAddr("0:5E4A5F9DBA638789E6770C990D2959237ACA3BC19D15A734782C26CB19343CC6")
	stateInit := &tlb.StateInit{
		Code: makeTransactionInternalSuccessCode(t, stateInitNewData),
		Data: stateInitData,
	}
	stateInitAddr := stateInit.CalcAddress(0)
	stateInitCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to build state init cell: %v", err)
	}
	suspended := cell.NewDict(288)
	suspendedKey := cell.BeginCell().
		MustStoreInt(int64(stateInitAddr.Workchain()), 32).
		MustStoreSlice(stateInitAddr.Data(), 256).
		EndCell()
	if err = suspended.Set(suspendedKey, cell.BeginCell().EndCell()); err != nil {
		t.Fatalf("failed to store suspended address: %v", err)
	}
	suspendedCell, err := tlb.ToCell(&tlb.SuspendedAddressList{
		Addresses:      suspended,
		SuspendedUntil: now + 60,
	})
	if err != nil {
		t.Fatalf("failed to build suspended address list: %v", err)
	}
	badStateHash := bytes.Repeat([]byte{0x77}, 32)
	nextCode := cell.BeginCell().MustStoreUInt(0xD00D, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1000)
	overspendMsg := buildTransactionOutboundInternalCell(t, 2_000_000_000)
	extraCurrency := makeTransactionExtraCurrencies(t, 7, 11)
	nonCanonicalValueMsg := cell.BeginCell().
		MustStoreBoolBit(false).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		MustStoreAddr(address.NewAddressNone()).
		MustStoreAddr(tonopsTestAddr).
		MustStoreUInt(1, 4).
		MustStoreUInt(0, 8).
		EndCell()
	completeNonCanonicalValueMsg := transactionTestRelaxedInternalMessage(func(b *cell.Builder) {
		b.MustStoreUInt(4, 4).
			MustStoreSlice([]byte{0x00, 0x0a, 0xb1, 0x47}, 32)
	}, func(b *cell.Builder) {
		b.MustStoreBigCoins(big.NewInt(0))
	})
	sendThenSetCodeActions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 1, Msg: outMsg}, tlb.ActionSetCode{NewCode: nextCode})
	invalidReserveModeActions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 8,
		Currency: tlb.CurrencyCollection{
			Coins: tlb.FromNanoTONU(1),
		},
	})
	invalidSendModeActions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 4, Msg: outMsg})
	reserveExtraActions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 0,
		Currency: tlb.CurrencyCollection{
			Coins:           tlb.FromNanoTONU(0),
			ExtraCurrencies: extraCurrency,
		},
	})
	invalidChangeLibraryModeActions := buildTransactionActionList(t, tlb.ActionChangeLibrary{
		Mode:   3,
		LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)},
	})
	nonCanonicalValueActions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 0, Msg: nonCanonicalValueMsg})
	nonCanonicalValueSkipActions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 2, Msg: nonCanonicalValueMsg})
	completeNonCanonicalValueSkipActions := buildTransactionActionList(t, tlb.ActionSendMsg{Mode: 3, Msg: completeNonCanonicalValueMsg})
	malformedSkipActions := cell.BeginCell().
		MustStoreRef(buildTransactionMalformedSendAction(2)).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(2, 8).
		EndCell()
	tightStateConfig := referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSizeLimits): buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
	})
	tightStorageConfig := referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamGasPricesBasechain):   buildTransactionGasLimitsCell(t, 100, 500),
		int32(tlb.ConfigParamGasPricesMasterchain): buildTransactionGasLimitsCell(t, 100, 500),
	})
	specialConfig := referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamConfigAddress): configAddrCell,
	})
	suspendedConfig := referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
		int32(tlb.ConfigParamSuspendedAddressList): suspendedCell,
	})
	storageDebtAtDeleteLimit := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   transactionCoinsPtr(big.NewInt(500)),
	}
	storageDebtOverDeleteLimit := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   transactionCoinsPtr(big.NewInt(501)),
	}
	storageDebtLarge := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   transactionCoinsPtr(big.NewInt(1000)),
	}

	tests := []struct {
		name       string
		addr       *address.Address
		shard      *tlb.ShardAccount
		msg        *cell.Cell
		configRoot *cell.Cell
	}{
		{
			name:  "credit_phase_low_internal_value_no_gas_credit",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 777, false, 0),
		},
		{
			name:  "credit_phase_rich_internal_value",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "historical_gas_limit_override",
			addr:  gasOverrideAddr,
			shard: buildTransactionTestShardAccount(t, gasOverrideAddr, makeTransactionInternalSuccessCode(t, newData), origData, 10_000_000_000_000, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     gasOverrideAddr,
				Amount:      tlb.FromNanoTONU(10_000_000_000_000),
				Body:        body,
			}),
		},
		{
			name:       "special_account_zero_fees",
			addr:       specialAddr,
			shard:      buildTransactionTestShardAccount(t, specialAddr, makeTransactionInternalSuccessCode(t, newData), origData, walletSendTestBalance, now),
			configRoot: specialConfig,
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     specialAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				Body:        body,
			}),
		},
		{
			name:  "bounceable_no_state_low_value",
			shard: buildTransactionTestNoneShardAccount(t),
			msg:   transactionPhaseInternalMessage(t, body, 1000, true, 0),
		},
		{
			name:  "non_bounceable_no_state_rich_value",
			shard: buildTransactionTestNoneShardAccount(t),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "stateinit_activates_nonexist",
			addr:  stateInitAddr,
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:  "stateinit_bad_hash_bounces_nonexist",
			shard: buildTransactionTestNoneShardAccount(t),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:  "stateinit_reactivates_frozen",
			addr:  stateInitAddr,
			shard: buildTransactionTestFrozenShardAccount(t, stateInitAddr, stateInitCell.Hash(), walletSendTestBalance, tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}, LastPaid: now}),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:  "stateinit_bad_hash_bounces_frozen",
			addr:  stateInitAddr,
			shard: buildTransactionTestFrozenShardAccount(t, stateInitAddr, badStateHash, walletSendTestBalance, tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}, LastPaid: now}),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:       "stateinit_suspended_uninit_skips_compute",
			addr:       stateInitAddr,
			shard:      buildTransactionTestUninitShardAccount(t, stateInitAddr, 0, tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}, LastPaid: now}),
			configRoot: suspendedConfig,
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     stateInitAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				StateInit:   stateInit,
				Body:        body,
			}),
		},
		{
			name:       "storage_debt_freezes_active",
			shard:      buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, 50, storageDebtLarge),
			msg:        transactionPhaseInternalMessage(t, body, 0, false, 0),
			configRoot: tightStorageConfig,
		},
		{
			name:  "new_bounce_format_compute_failure_short_body",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				IHRFee:      tlb.FromNanoTONU(1),
				Body:        largeBody,
			}),
		},
		{
			name:  "new_bounce_format_compute_failure_full_body",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				IHRFee:      tlb.FromNanoTONU(3),
				Body:        largeBody,
			}),
		},
		{
			name:  "new_bounce_format_action_failure",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, overspendMsg, newData, 16), origData, 500, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				IHRFee:      tlb.FromNanoTONU(1),
				Body:        body,
			}),
		},
		{
			name:  "new_bounce_format_compute_failure_nofunds",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1000),
				IHRFee:      tlb.FromNanoTONU(1),
				Body:        body,
			}),
		},
		{
			name:       "action_state_limit_failure_rolls_back_state",
			shard:      buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSetCodeCode(t, nextCode, newData), origData, walletSendTestBalance, now),
			msg:        transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
			configRoot: tightStateConfig,
		},
		{
			name:       "action_state_limit_after_send_keeps_phase_counters",
			shard:      buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, sendThenSetCodeActions, newData), origData, walletSendTestBalance, now),
			msg:        transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
			configRoot: tightStateConfig,
		},
		{
			name:       "action_state_limit_after_skipped_actions_keeps_prepass_arg",
			shard:      buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, malformedSkipActions, newData), origData, walletSendTestBalance, now),
			msg:        transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
			configRoot: tightStateConfig,
		},
		{
			name:  "reserve_action_invalid_mode_keeps_action_phase_valid",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, invalidReserveModeActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "send_action_invalid_mode_keeps_action_phase_valid",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, invalidSendModeActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "reserve_action_extra_currency_keeps_action_phase_valid",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, reserveExtraActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "change_library_invalid_mode_keeps_action_phase_valid",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, invalidChangeLibraryModeActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "send_action_noncanonical_value_aborts_without_mode2",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, nonCanonicalValueActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "send_action_noncanonical_value_skipped_with_mode2",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, nonCanonicalValueSkipActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:  "send_action_complete_noncanonical_value_not_skipped_with_mode2",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalActionsCode(t, completeNonCanonicalValueSkipActions, newData), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
		},
		{
			name:       "storage_due_equal_delete_limit_keeps_uninit",
			shard:      buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 0, storageDebtAtDeleteLimit),
			msg:        transactionPhaseInternalMessage(t, body, 0, false, 0),
			configRoot: tightStorageConfig,
		},
		{
			name:       "storage_due_over_delete_limit_deletes_uninit",
			shard:      buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 0, storageDebtOverDeleteLimit),
			msg:        transactionPhaseInternalMessage(t, body, 0, false, 0),
			configRoot: tightStorageConfig,
		},
		{
			name:  "storage_due_over_delete_limit_with_credited_extra_keeps_uninit",
			shard: buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 0, storageDebtOverDeleteLimit),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled:     true,
				SrcAddr:         internalEmulationSrcAddr,
				DstAddr:         tonopsTestAddr,
				Amount:          tlb.FromNanoTONU(0),
				ExtraCurrencies: extraCurrency,
				Body:            body,
			}),
			configRoot: tightStorageConfig,
		},
		{
			name:  "bounced_inbound_compute_failure_bounces_like_cppnode",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, now),
			msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				Bounce:      true,
				Bounced:     true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				Body:        body,
			}),
		},
		{
			name:  "send_action_success_keeps_action_fees_and_sizes",
			shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, outMsg, newData, 1), origData, walletSendTestBalance, now),
			msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
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

			goRes, err := testEmulateTransaction(NewTVM(), tt.shard, tt.msg, testTxParams{
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
			if goRes.TransactionCell == nil || goRes.NextAccount.ShardAccountCell() == nil {
				t.Fatal("go transaction emulation did not produce cells")
			}

			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(tt.shard, tt.msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, testConfigRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
			assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
			assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
		})
	}
}

func transactionPhaseInternalMessage(t *testing.T, body *cell.Cell, amount uint64, bounce bool, extraFlags uint64) *cell.Cell {
	t.Helper()

	return mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      bounce,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(amount),
		IHRFee:      tlb.FromNanoTONU(extraFlags),
		Body:        body,
	})
}

func TestTVMCrossEmulatorTransactionNonComputePhaseExternalParityGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	fixture := transactionNonComputePhaseExternalFixture(t)
	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
			for _, tt := range fixture.cases {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					runTransactionNonComputePhaseExternalVersionCase(t, fixture, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionNonComputePhaseExternalGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%transactionNonComputePhaseExternalCaseCount))
	}
	for i := 0; i < transactionNonComputePhaseExternalCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		fixture := transactionNonComputePhaseExternalFixture(t)
		if len(fixture.cases) != transactionNonComputePhaseExternalCaseCount {
			t.Fatalf("external transaction case count = %d, want %d", len(fixture.cases), transactionNonComputePhaseExternalCaseCount)
		}
		tt := fixture.cases[int(rawCase)%len(fixture.cases)]
		runTransactionNonComputePhaseExternalVersionCase(t, fixture, tt, version)
	})
}

const transactionNonComputePhaseExternalCaseCount = 4

type transactionNonComputePhaseExternalCase struct {
	name              string
	code              *cell.Cell
	wantAction        *transactionActionPhaseExpectation
	wantActionVersion func(uint32) transactionActionPhaseExpectation
}

type transactionNonComputePhaseExternalFixtureData struct {
	baseConfigRoot *cell.Cell
	now            uint32
	origData       *cell.Cell
	msg            *cell.Cell
	cases          []transactionNonComputePhaseExternalCase
}

func transactionNonComputePhaseExternalFixture(t *testing.T) transactionNonComputePhaseExternalFixtureData {
	t.Helper()

	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1000)
	shortVarDestMsg := buildTransactionOutboundInternalCellWithAddresses(t,
		address.NewAddressNone(),
		address.NewAddressVar(0, tonopsTestAddr.Workchain(), 255, tonopsTestAddr.Data()),
		1000,
		cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	)
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: tonopsTestAddr,
		Body:    body,
	})
	if err != nil {
		t.Fatalf("failed to build external message: %v", err)
	}

	return transactionNonComputePhaseExternalFixtureData{
		baseConfigRoot: mustReferenceTransactionConfigRoot(t),
		now:            uint32(tonopsTestTime.Unix()),
		origData:       cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell(),
		msg:            msgCell,
		cases: []transactionNonComputePhaseExternalCase{
			{
				name: "accepted_success",
				code: makeTransactionExternalSuccessCode(t, newData),
			},
			{
				name: "committed_throw_runs_actions",
				code: makeTransactionExternalCommitThenThrowCode(t, newData, outMsg, 42),
			},
			{
				name: "committed_throw_invalid_dest_action_code36",
				code: makeTransactionExternalCommitThenThrowCode(t, newData, shortVarDestMsg, 42),
				wantAction: &transactionActionPhaseExpectation{
					success:         false,
					valid:           true,
					resultCode:      36,
					skippedActions:  0,
					messagesCreated: 0,
				},
			},
			{
				name: "committed_throw_invalid_dest_action_mode2_skip",
				code: makeTransactionExternalCommitThenThrowCodeWithMode(t, newData, shortVarDestMsg, 2, 42),
				wantActionVersion: func(version uint32) transactionActionPhaseExpectation {
					skipped := uint16(1)
					if version < 8 {
						skipped = 0
					}
					return transactionActionPhaseExpectation{
						success:         true,
						valid:           true,
						resultCode:      0,
						skippedActions:  skipped,
						messagesCreated: 0,
					}
				},
			},
		},
	}
}

func runTransactionNonComputePhaseExternalVersionCase(t *testing.T, fixture transactionNonComputePhaseExternalFixtureData, tt transactionNonComputePhaseExternalCase, version uint32) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, fixture.baseConfigRoot, version)
	machine := NewTVM()
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, tt.code, fixture.origData, walletSendTestBalance, fixture.now)

	goRes, err := testEmulateTransaction(machine, shard, fixture.msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         fixture.now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, fixture.msg, fixture.now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
	if tt.wantAction != nil {
		assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, *tt.wantAction)
		assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, *tt.wantAction)
	}
	if tt.wantActionVersion != nil {
		want := tt.wantActionVersion(version)
		assertOrdinaryTransactionActionPhase(t, "go", goRes.TransactionCell, want)
		assertOrdinaryTransactionActionPhase(t, "reference", refRes.txCell, want)
	}
}

type transactionActionPhaseExpectation struct {
	success         bool
	valid           bool
	resultCode      int32
	skippedActions  uint16
	messagesCreated uint16
}

func assertOrdinaryTransactionActionPhase(t *testing.T, side string, txCell *cell.Cell, want transactionActionPhaseExpectation) {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode %s transaction: %v", side, err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatalf("%s transaction description = %T, want ordinary", side, tx.Description)
	}
	if desc.ActionPhase == nil {
		t.Fatalf("%s transaction has no action phase", side)
	}
	phase := desc.ActionPhase
	if phase.Success != want.success ||
		phase.Valid != want.valid ||
		phase.ResultCode != want.resultCode ||
		phase.SkippedActions != want.skippedActions ||
		phase.MessagesCreated != want.messagesCreated {
		t.Fatalf("%s action phase mismatch: got success=%t valid=%t code=%d skipped=%d messages=%d",
			side,
			phase.Success,
			phase.Valid,
			phase.ResultCode,
			phase.SkippedActions,
			phase.MessagesCreated,
		)
	}
}

func TestTVMCrossEmulatorTransactionNonComputePhaseTickTockParityGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	fixture := buildTransactionNonComputePhaseTickTockFixture(t)
	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_TICKTOCK_VERSION_AUDIT") {
		version := version
		t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
			for _, tt := range fixture.cases {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					runTransactionNonComputePhaseTickTockVersionCase(t, fixture, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionNonComputePhaseTickTockGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%transactionNonComputePhaseTickTockCaseCount))
	}
	for i := 0; i < transactionNonComputePhaseTickTockCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		fixture := buildTransactionNonComputePhaseTickTockFixture(t)
		if len(fixture.cases) != transactionNonComputePhaseTickTockCaseCount {
			t.Fatalf("tick/tock transaction case count = %d, want %d", len(fixture.cases), transactionNonComputePhaseTickTockCaseCount)
		}
		tt := fixture.cases[int(rawCase)%len(fixture.cases)]
		runTransactionNonComputePhaseTickTockVersionCase(t, fixture, tt, version)
	})
}

const transactionNonComputePhaseTickTockCaseCount = 5

type transactionNonComputePhaseTickTockCase struct {
	name   string
	code   *cell.Cell
	isTock bool
}

type transactionNonComputePhaseTickTockFixtureData struct {
	baseConfigRoot *cell.Cell
	now            uint32
	origData       *cell.Cell
	cases          []transactionNonComputePhaseTickTockCase
}

func buildTransactionNonComputePhaseTickTockFixture(t *testing.T) transactionNonComputePhaseTickTockFixtureData {
	t.Helper()

	tickData := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
	tockData := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()
	tickBody := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	tockBody := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	tickMsg, err := buildInternalMessageForEmulation(tickTockTestAddr, tickBody, 100)
	if err != nil {
		t.Fatalf("failed to build tick message: %v", err)
	}
	tockMsg, err := buildInternalMessageForEmulation(tickTockTestAddr, tockBody, 200)
	if err != nil {
		t.Fatalf("failed to build tock message: %v", err)
	}

	stateOnlyCode := makeTickTockStateOnlyCode(t, tickData, tockData)
	actionCode := makeTickTockSuccessCode(t, tickData, tockData, tickMsg, tockMsg)
	failCode := makeTickTockFailureCode(t)

	return transactionNonComputePhaseTickTockFixtureData{
		baseConfigRoot: mustReferenceTransactionConfigRoot(t),
		now:            uint32(tonopsTestTime.Unix()),
		origData:       cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell(),
		cases: []transactionNonComputePhaseTickTockCase{
			{name: "tick_state_only_success", code: stateOnlyCode},
			{name: "tock_state_only_success", code: stateOnlyCode, isTock: true},
			{name: "tick_action_success", code: actionCode},
			{name: "tock_action_success", code: actionCode, isTock: true},
			{name: "tick_failure", code: failCode},
		},
	}
}

func runTransactionNonComputePhaseTickTockVersionCase(t *testing.T, fixture transactionNonComputePhaseTickTockFixtureData, tt transactionNonComputePhaseTickTockCase, version int) {
	t.Helper()

	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, fixture.baseConfigRoot, uint32(version))
	machine := NewTVM()

	shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, tt.code, fixture.origData, tickTockTestBalance)
	if err != nil {
		t.Fatalf("failed to build tick/tock shard: %v", err)
	}
	goRes, err := testEmulateTickTockTransaction(machine, shard, tt.isTock, testTxParams{
		Now:        fixture.now,
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("go tick/tock emulation failed: %v", err)
	}

	refRes, err := runReferenceTickTockWithConfigRoot(tt.code, fixture.origData, tickTockTestAddr, tt.isTock, fixture.now, tickTockTestBalance, tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference tick/tock emulation failed: %v", err)
	}

	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
}

func assertShardAccountNonComputeParity(t *testing.T, goCell, refCell *cell.Cell) {
	t.Helper()

	var goShard, refShard tlb.ShardAccount
	if err := tlb.Parse(&goShard, goCell); err != nil {
		t.Fatalf("failed to parse go shard account: %v", err)
	}
	if err := tlb.Parse(&refShard, refCell); err != nil {
		t.Fatalf("failed to parse reference shard account: %v", err)
	}
	if goShard.LastTransLT != refShard.LastTransLT {
		t.Fatalf("last transaction lt mismatch: go=%d reference=%d", goShard.LastTransLT, refShard.LastTransLT)
	}
	if goShard.Account == nil || refShard.Account == nil || !bytes.Equal(goShard.Account.Hash(), refShard.Account.Hash()) {
		t.Fatalf("account state hash mismatch:\ngo=%s\nreference=%s", goCell.Dump(), refCell.Dump())
	}
}

func assertTransactionNonComputeParity(t *testing.T, goCell, refCell *cell.Cell) {
	t.Helper()

	goNorm := mustNormalizeTransactionComputePhase(t, goCell)
	refNorm := mustNormalizeTransactionComputePhase(t, refCell)
	if !bytes.Equal(goNorm.Hash(), refNorm.Hash()) {
		t.Fatalf(
			"non-compute transaction mismatch:\ngo summary=%s\nreference summary=%s\ngo normalized=%s\nreference normalized=%s\ngo tx=%s\nreference tx=%s",
			transactionCrossTxSummary(t, goCell),
			transactionCrossTxSummary(t, refCell),
			goNorm.Dump(),
			refNorm.Dump(),
			goCell.Dump(),
			refCell.Dump(),
		)
	}
}

func assertTransactionComputePhaseParity(t *testing.T, goCell, refCell *cell.Cell) {
	t.Helper()

	goPhase := mustTransactionComputePhase(t, goCell)
	refPhase := mustTransactionComputePhase(t, refCell)
	if !reflect.DeepEqual(goPhase, refPhase) {
		t.Fatalf(
			"compute phase mismatch:\ngo=%s\nreference=%s\ngo tx=%s\nreference tx=%s",
			transactionComputePhaseSummary(goPhase),
			transactionComputePhaseSummary(refPhase),
			goCell.Dump(),
			refCell.Dump(),
		)
	}
}

func mustTransactionComputePhase(t *testing.T, txCell *cell.Cell) tlb.ComputePhase {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode transaction: %v", err)
	}

	switch desc := tx.Description.(type) {
	case tlb.TransactionDescriptionOrdinary:
		return desc.ComputePhase
	case tlb.TransactionDescriptionTickTock:
		return desc.ComputePhase
	case tlb.TransactionDescriptionSplitPrepare:
		return desc.ComputePhase
	case tlb.TransactionDescriptionMergeInstall:
		return desc.ComputePhase
	default:
		t.Fatalf("transaction description %T has no compute phase", tx.Description)
	}
	return tlb.ComputePhase{}
}

func transactionComputePhaseSummary(phase tlb.ComputePhase) string {
	switch p := phase.Phase.(type) {
	case tlb.ComputePhaseSkipped:
		return fmt.Sprintf("skip:%s", p.Reason.Type)
	case tlb.ComputePhaseVM:
		return fmt.Sprintf(
			"vm:success=%t msg_state=%t activated=%t gas_fees=%s gas_used=%s gas_limit=%s credit=%s mode=%d exit=%d arg=%s steps=%d init=%x final=%x",
			p.Success,
			p.MsgStateUsed,
			p.AccountActivated,
			p.GasFees.Nano().String(),
			p.Details.GasUsed.String(),
			p.Details.GasLimit.String(),
			transactionCrossBigIntPtr(p.Details.GasCredit),
			p.Details.Mode,
			p.Details.ExitCode,
			transactionCrossInt32Ptr(p.Details.ExitArg),
			p.Details.VMSteps,
			p.Details.VMInitStateHash,
			p.Details.VMFinalStateHash,
		)
	default:
		return fmt.Sprintf("%T", phase.Phase)
	}
}

func mustNormalizeTransactionComputePhase(t *testing.T, txCell *cell.Cell) *cell.Cell {
	t.Helper()

	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode transaction: %v", err)
	}

	canonicalCompute := tlb.ComputePhase{
		Phase: tlb.ComputePhaseVM{
			Success:          false,
			MsgStateUsed:     false,
			AccountActivated: false,
			GasFees:          tlb.FromNanoTONU(0),
			Details: tlb.ComputePhaseVMDetails{
				GasUsed:          big.NewInt(0),
				GasLimit:         big.NewInt(0),
				Mode:             0,
				ExitCode:         0,
				VMSteps:          0,
				VMInitStateHash:  make([]byte, 32),
				VMFinalStateHash: make([]byte, 32),
			},
		},
	}

	switch desc := tx.Description.(type) {
	case tlb.TransactionDescriptionOrdinary:
		desc.ComputePhase = canonicalCompute
		tx.Description = desc
	case tlb.TransactionDescriptionTickTock:
		desc.ComputePhase = canonicalCompute
		tx.Description = desc
	case tlb.TransactionDescriptionSplitPrepare:
		desc.ComputePhase = canonicalCompute
		tx.Description = desc
	case tlb.TransactionDescriptionMergeInstall:
		desc.ComputePhase = canonicalCompute
		tx.Description = desc
	default:
		t.Fatalf("transaction description %T has no compute phase to normalize", tx.Description)
	}

	normalized, err := tx.ToCell()
	if err != nil {
		t.Fatalf("failed to serialize normalized transaction: %v", err)
	}
	return normalized
}

func TestTVMCrossEmulatorTransactionNonComputePhaseParityGlobalVersion(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	fixture := transactionNonComputePhaseVersionFixture(t)
	for _, version := range transactionVersionCrossEmulatorVersions(t) {
		version := version
		t.Run(fmt.Sprintf("global_v%d", version), func(t *testing.T) {
			for _, tt := range fixture.cases {
				if tt.minVersion != 0 && version < tt.minVersion {
					continue
				}
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					runTransactionNonComputePhaseVersionCase(t, fixture, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTransactionNonComputePhaseGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%transactionNonComputePhaseVersionCaseCount))
	}
	for i := 0; i < transactionNonComputePhaseVersionCaseCount; i++ {
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8) {
		version := uint32(tvmFuzzGlobalVersionByte(rawVersion))
		fixture := transactionNonComputePhaseVersionFixture(t)
		if len(fixture.cases) != transactionNonComputePhaseVersionCaseCount {
			t.Fatalf("transaction non-compute case count = %d, want %d", len(fixture.cases), transactionNonComputePhaseVersionCaseCount)
		}
		tt := transactionNonComputePhaseVersionCaseForFuzz(t, fixture.cases, version, rawCase)
		runTransactionNonComputePhaseVersionCase(t, fixture, tt, version)
	})
}

const transactionNonComputePhaseVersionCaseCount = 8

type transactionNonComputePhaseVersionCase struct {
	name       string
	shard      *tlb.ShardAccount
	msg        *cell.Cell
	minVersion uint32
	configRoot func(*testing.T, *cell.Cell) *cell.Cell
}

type transactionNonComputePhaseVersionFixtureData struct {
	baseConfigRoot *cell.Cell
	now            uint32
	cases          []transactionNonComputePhaseVersionCase
}

func transactionNonComputePhaseVersionFixture(t *testing.T) transactionNonComputePhaseVersionFixtureData {
	t.Helper()

	now := uint32(tonopsTestTime.Unix())
	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	largeBody := buildTransactionZeroBitsCell(t, 520)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	nextCode := cell.BeginCell().MustStoreUInt(0xD00D, 16).EndCell()
	outMsg := buildTransactionOutboundInternalCell(t, 1000)
	overspendMsg := buildTransactionOutboundInternalCell(t, 2_000_000_000)
	precompiledCode := makeTransactionInternalSuccessCode(t, newData)
	storageDebtOverDeleteLimit := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   transactionCoinsPtr(big.NewInt(501)),
	}
	storageDebtLarge := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 60,
		DuePayment:   transactionCoinsPtr(big.NewInt(1000)),
	}
	tightStorageConfig := func(t *testing.T, configRoot *cell.Cell) *cell.Cell {
		t.Helper()

		return referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
			int32(tlb.ConfigParamGasPricesBasechain):   buildTransactionGasLimitsCell(t, 100, 500),
			int32(tlb.ConfigParamGasPricesMasterchain): buildTransactionGasLimitsCell(t, 100, 500),
		})
	}
	tightStateConfig := func(t *testing.T, configRoot *cell.Cell) *cell.Cell {
		t.Helper()

		return referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
			int32(tlb.ConfigParamSizeLimits): buildTransactionSizeLimitsCell(t, 1<<21, 1<<13, 1000, 1, 1),
		})
	}

	return transactionNonComputePhaseVersionFixtureData{
		baseConfigRoot: mustReferenceTransactionConfigRoot(t),
		now:            now,
		cases: []transactionNonComputePhaseVersionCase{
			{
				name:  "send_success",
				shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, outMsg, newData, 1), origData, walletSendTestBalance, now),
				msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
			},
			{
				name:  "action_failure_bounce_message_balance",
				shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSendCode(t, overspendMsg, newData, 16), origData, 500, now),
				msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, true, 0),
			},
			{
				name:  "compute_failure_bounce_large_body",
				shard: buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionStackUnderflowCode(t), origData, walletSendTestBalance, now),
				msg: mustTransactionMsgCell(t, &tlb.InternalMessage{
					IHRDisabled: true,
					Bounce:      true,
					SrcAddr:     internalEmulationSrcAddr,
					DstAddr:     tonopsTestAddr,
					Amount:      tlb.FromNanoTONU(1_000_000_000),
					IHRFee:      tlb.FromNanoTONU(1),
					Body:        largeBody,
				}),
			},
			{
				name:  "non_bounceable_no_state_rich_value",
				shard: buildTransactionTestNoneShardAccount(t),
				msg:   transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
			},
			{
				name:       "storage_debt_freezes_active",
				shard:      buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, makeTransactionInternalSuccessCode(t, newData), origData, 50, storageDebtLarge),
				msg:        transactionPhaseInternalMessage(t, body, 0, false, 0),
				configRoot: tightStorageConfig,
			},
			{
				name:       "storage_due_over_delete_limit_deletes_uninit",
				shard:      buildTransactionTestUninitShardAccount(t, tonopsTestAddr, 0, storageDebtOverDeleteLimit),
				msg:        transactionPhaseInternalMessage(t, body, 0, false, 0),
				configRoot: tightStorageConfig,
			},
			{
				name:       "action_state_limit_failure_rolls_back_state",
				shard:      buildTransactionTestShardAccount(t, tonopsTestAddr, makeTransactionInternalSetCodeCode(t, nextCode, newData), origData, walletSendTestBalance, now),
				msg:        transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
				configRoot: tightStateConfig,
			},
			{
				name:       "precompiled_config_overrides_gas_usage",
				shard:      buildTransactionTestShardAccount(t, tonopsTestAddr, precompiledCode, origData, walletSendTestBalance, now),
				msg:        transactionPhaseInternalMessage(t, body, 1_000_000_000, false, 0),
				minVersion: 13,
				configRoot: func(t *testing.T, configRoot *cell.Cell) *cell.Cell {
					t.Helper()

					return referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
						int32(tlb.ConfigParamPrecompiledContracts): buildTransactionV13PrecompiledConfig(t, precompiledCode, 7),
					})
				},
			},
		},
	}
}

func transactionNonComputePhaseVersionCaseForFuzz(t *testing.T, cases []transactionNonComputePhaseVersionCase, version uint32, rawCase uint8) transactionNonComputePhaseVersionCase {
	t.Helper()

	start := int(rawCase) % len(cases)
	for i := 0; i < len(cases); i++ {
		tt := cases[(start+i)%len(cases)]
		if tt.minVersion == 0 || version >= tt.minVersion {
			return tt
		}
	}
	t.Fatalf("no transaction non-compute cases are available for v%d", version)
	return transactionNonComputePhaseVersionCase{}
}

func runTransactionNonComputePhaseVersionCase(t *testing.T, fixture transactionNonComputePhaseVersionFixtureData, tt transactionNonComputePhaseVersionCase, version uint32) {
	t.Helper()

	versionConfigRoot := referenceTransactionConfigRootWithGlobalVersion(t, fixture.baseConfigRoot, version)
	testConfigRoot := versionConfigRoot
	if tt.configRoot != nil {
		testConfigRoot = tt.configRoot(t, versionConfigRoot)
	}

	machine := NewTVM()

	goRes, err := testEmulateTransaction(machine, tt.shard, tt.msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         fixture.now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  testConfigRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(tt.shard, tt.msg, fixture.now, uint64(transactionTestLogicalTime), tonopsTestSeed, testConfigRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	assertTransactionNonComputeParity(t, goRes.TransactionCell, refRes.txCell)
	assertTransactionComputePhaseParity(t, goRes.TransactionCell, refRes.txCell)
	assertShardAccountNonComputeParity(t, goRes.NextAccount.ShardAccountCell(), refRes.shardCell)
}
