package tvm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestHistoricalTransactionOptionsVersionScope(t *testing.T) {
	machine := NewTVM()
	empty := cell.BeginCell().EndCell()
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty})
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatal(err)
	}
	// No balance forces skipped compute if validation is accidentally deferred.
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, empty, empty, 0, 0)
	account, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		opts       TransactionOptions
		maxVersion uint32
	}{
		{"modern", TransactionOptions{}, ^uint32(0)},
		{"message_gas", TransactionOptions{HistoricalMessageGas: true}, 1},
		{"external_state_init", TransactionOptions{HistoricalExternalStateInit: true}, 4},
		{"both", TransactionOptions{HistoricalMessageGas: true, HistoricalExternalStateInit: true}, 1},
		{"storage_fee", TransactionOptions{HistoricalStorageFee: true}, 3},
		{"public_library_deploy", TransactionOptions{HistoricalPublicLibraryDeploy: true}, 4},
		{"storage_fee_public_library_deploy", TransactionOptions{HistoricalStorageFee: true, HistoricalPublicLibraryDeploy: true}, 3},
		{"message_gas_storage_fee", TransactionOptions{HistoricalMessageGas: true, HistoricalStorageFee: true}, 1},
		{"external_state_init_public_library_deploy", TransactionOptions{HistoricalExternalStateInit: true, HistoricalPublicLibraryDeploy: true}, 4},
		{"no_action_state_limits", TransactionOptions{HistoricalNoActionStateLimits: true}, 3},
		{"action_library_validation", TransactionOptions{HistoricalActionLibraryValidation: true}, 3},
		{"action_library_validation_message_gas", TransactionOptions{HistoricalActionLibraryValidation: true, HistoricalMessageGas: true}, 1},
		{"external_state_init_no_action_state_limits", TransactionOptions{HistoricalExternalStateInit: true, HistoricalNoActionStateLimits: true}, 3},
		{"external_state_init_pop_c3", TransactionOptions{HistoricalExternalStateInit: true, Historical: vm.HistoricalConfig{PopC3Cell: true}}, 0},
		{"nan_comparison", TransactionOptions{Historical: vm.HistoricalConfig{NaNComparison: true}}, 3},
		{"nan_comparison_2019", TransactionOptions{Historical: vm.HistoricalConfig{NaNComparison: true, GasSchedule: vm.GasSchedule2019}}, 0},
		{"nan_comparison_message_gas", TransactionOptions{HistoricalMessageGas: true, Historical: vm.HistoricalConfig{NaNComparison: true}}, 1},
		{"storage_fee_2019", TransactionOptions{HistoricalStorageFee: true, Historical: vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019}}, 0},
		{"public_library_deploy_no_prng", TransactionOptions{HistoricalPublicLibraryDeploy: true, Historical: vm.HistoricalConfig{NoPRNG: true}}, 0},
		{"message_gas_2019", TransactionOptions{HistoricalMessageGas: true, Historical: vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019}}, 0},
		{"message_gas_early_2020", TransactionOptions{HistoricalMessageGas: true, Historical: vm.HistoricalConfig{GasSchedule: vm.GasScheduleEarly2020}}, 0},
		{"message_gas_pop_c3", TransactionOptions{HistoricalMessageGas: true, Historical: vm.HistoricalConfig{PopC3Cell: true}}, 0},
		{"message_gas_no_prng", TransactionOptions{HistoricalMessageGas: true, Historical: vm.HistoricalConfig{NoPRNG: true}}, 0},
		{"message_gas_no_blkdrop2", TransactionOptions{HistoricalMessageGas: true, Historical: vm.HistoricalConfig{NoBLKDROP2: true}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, version := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16} {
				t.Run(fmt.Sprintf("version_%d", version), func(t *testing.T) {
					block, err := testPreparedBlockchainConfigWithVersion(t, version).NewBlockContext(BlockOptions{Now: 1, RandSeed: make([]byte, 32)})
					if err != nil {
						t.Fatal(err)
					}
					checks := map[string]func() error{
						"transaction": func() error {
							_, err := machine.EmulateTransaction(block, account, message, tc.opts)
							return err
						},
						"ticktock": func() error {
							_, err := machine.EmulateTickTockTransaction(block, account, false, tc.opts)
							return err
						},
						"accept": func() error {
							_, err := machine.CheckExternalMessageAccepted(block, account, message, tc.opts)
							return err
						},
					}
					for name, check := range checks {
						t.Run(name, func(t *testing.T) {
							err := check()
							if version <= tc.maxVersion {
								if err != nil {
									t.Fatalf("valid configuration rejected: %v", err)
								}
							} else if err == nil || !strings.Contains(err.Error(), "historical") {
								t.Fatalf("expected historical configuration error, got %v", err)
							}
						})
					}
				})
			}
		})
	}
}

func TestHistoricalMessageGasVersionOneEntrypoints(t *testing.T) {
	empty := cell.BeginCell().EndCell()
	code := codeFromBuilders(t, funcsop.ACCEPT().Serialize())
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, empty, walletSendTestBalance, now)
	account, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}
	msgCell, err := tlb.ToCell(&tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty})
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatal(err)
	}
	block, err := transactionTestConfigWithGlobalVersion(t, 1).NewBlockContext(BlockOptions{
		Now: now, BlockLT: transactionTestLogicalTime, RandSeed: tonopsTestSeed,
	})
	if err != nil {
		t.Fatal(err)
	}

	machine := NewTVM()
	for _, historical := range []bool{false, true, false} {
		opts := TransactionOptions{LogicalTime: transactionTestLogicalTime, HistoricalMessageGas: historical}
		accepted, err := machine.CheckExternalMessageAccepted(block, account, message, opts)
		if err != nil || !accepted {
			t.Fatalf("historical=%t: accept check = %t, %v", historical, accepted, err)
		}
		ordinary, err := machine.EmulateTransaction(block, account, message, opts)
		if err != nil {
			t.Fatal(err)
		}
		ticktock, err := machine.EmulateTickTockTransaction(block, account, false, opts)
		if err != nil {
			t.Fatal(err)
		}
		for name, result := range map[string]*TransactionExecutionResult{"ordinary": ordinary, "ticktock": ticktock} {
			if !result.Accepted || result.ExitCode != 0 || result.Steps == 0 || result.TransactionCell == nil || result.NextAccount == nil {
				t.Fatalf("historical=%t: %s failed to execute ACCEPT: %+v", historical, name, result)
			}
		}
	}
}
