package tvm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMHistoricalImplicitGas(t *testing.T) {
	machine := NewTVM()
	empty := cell.BeginCell().EndCell()
	tests := []struct {
		name     string
		code     *cell.Cell
		modern   int64
		historic int64
		steps    uint64
	}{
		{"ret", empty, 5, 0, 1},
		{"jmpref_ret", cell.BeginCell().MustStoreRef(empty).EndCell(), 115, 100, 2},
		{"explicit_ret", codeFromBuilders(t, execop.RET().Serialize()), 26, 26, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reuse the machine and return to modern rules to catch mode leakage.
			for _, schedule := range []vm.GasSchedule{vm.GasScheduleModern, vm.GasSchedule2019, vm.GasScheduleEarly2020, vm.GasScheduleModern} {
				cfg := testExecutionConfigWithVersion(t, 0)
				cfg.Historical.GasSchedule = schedule
				want := tc.modern
				if schedule != vm.GasScheduleModern {
					want = tc.historic
				}
				res, err := machine.Execute(tc.code, empty, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if res.ExitCode != 0 || res.GasUsed != want || res.Steps != tc.steps {
					t.Fatalf("schedule %d: exit/gas/steps = %d/%d/%d, want 0/%d/%d", schedule, res.ExitCode, res.GasUsed, res.Steps, want, tc.steps)
				}
			}
		})
	}

	code := cell.BeginCell().MustStoreRef(empty).EndCell()
	for _, schedule := range []vm.GasSchedule{vm.GasScheduleModern, vm.GasSchedule2019, vm.GasScheduleEarly2020} {
		cfg := testExecutionConfigWithVersion(t, 0)
		cfg.Historical.GasSchedule = schedule
		res, err := machine.Execute(code, empty, tuple.Tuple{}, vm.GasWithLimit(100), vm.NewStack(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		wantExit := int64(0)
		if schedule == vm.GasScheduleModern {
			wantExit = ^int64(vmerr.CodeOutOfGas)
		}
		if res.ExitCode != wantExit {
			t.Fatalf("schedule %d: exit at gas boundary = %d, want %d", schedule, res.ExitCode, wantExit)
		}
	}
}

func TestTVMHistoricalMessageAndGetMethod(t *testing.T) {
	machine := NewTVM()
	empty := cell.BeginCell().EndCell()
	code := cell.BeginCell().MustStoreRef(empty).EndCell()
	for _, schedule := range []vm.GasSchedule{vm.GasScheduleModern, vm.GasSchedule2019, vm.GasScheduleEarly2020} {
		t.Run(fmt.Sprintf("schedule_%d", schedule), func(t *testing.T) {
			cfg := testExecutionConfigWithVersion(t, 0)
			cfg.Historical.GasSchedule = schedule
			wantGas := int64(100)
			if schedule == vm.GasScheduleModern {
				wantGas = 115
			}
			get, err := machine.ExecuteGetMethod(code, empty, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			msgCfg := MessageEmulationConfig{
				Config: cfg.Config, Historical: cfg.Historical,
				Address: tonopsTestAddr, Now: uint32(tonopsTestTime.Unix()), RandSeed: tonopsTestSeed,
			}
			internal, err := machine.EmulateInternalMessage(code, empty, empty, 1, msgCfg)
			if err != nil {
				t.Fatal(err)
			}
			external, err := machine.EmulateExternalMessage(code, empty, &tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty}, msgCfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, res := range []*ExecutionResult{get, &internal.ExecutionResult, &external.ExecutionResult} {
				if res.ExitCode != 0 || res.GasUsed != wantGas || res.Steps != 2 {
					t.Fatalf("exit/gas/steps = %d/%d/%d, want 0/%d/2", res.ExitCode, res.GasUsed, res.Steps, wantGas)
				}
			}
		})
	}
}

func TestTVMRejectInvalidHistoricalConfig(t *testing.T) {
	machine := NewTVM()
	empty := cell.BeginCell().EndCell()
	msg := &tlb.ExternalMessage{DstAddr: tonopsTestAddr, Body: empty}
	msgCell, err := tlb.ToCell(msg)
	if err != nil {
		t.Fatal(err)
	}
	preparedMsg, err := PrepareMessage(msgCell)
	if err != nil {
		t.Fatal(err)
	}
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, empty, empty, 0, 0)
	acc, err := PrepareAccount(shard, tonopsTestAddr)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		version    uint32
		historical vm.HistoricalConfig
	}{
		{"unknown_schedule", 0, vm.HistoricalConfig{GasSchedule: 255}},
		{"versioned_gas", 1, vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019}},
		{"versioned_pop_c3", 16, vm.HistoricalConfig{PopC3Cell: true}},
		{"versioned_no_prng", 1, vm.HistoricalConfig{NoPRNG: true}},
		{"versioned_no_blkdrop2", 1, vm.HistoricalConfig{NoBLKDROP2: true}},
		{"versioned_nan_comparison", 4, vm.HistoricalConfig{NaNComparison: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testExecutionConfigWithVersion(t, tc.version)
			cfg.Historical = tc.historical
			block, err := cfg.Config.NewBlockContext(BlockOptions{Now: 1, RandSeed: make([]byte, 32)})
			if err != nil {
				t.Fatal(err)
			}
			msgCfg := MessageEmulationConfig{Config: cfg.Config, Historical: tc.historical}
			opts := TransactionOptions{Historical: tc.historical}
			// A zero-balance account also verifies validation before skipped compute.
			checks := map[string]func() error{
				"execute": func() error {
					_, err := machine.Execute(empty, empty, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), cfg)
					return err
				},
				"get": func() error {
					_, err := machine.ExecuteGetMethod(empty, empty, tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), cfg)
					return err
				},
				"internal": func() error {
					_, err := machine.EmulateInternalMessage(empty, empty, empty, 1, msgCfg)
					return err
				},
				"external": func() error {
					_, err := machine.EmulateExternalMessage(empty, empty, msg, msgCfg)
					return err
				},
				"transaction": func() error {
					_, err := machine.EmulateTransaction(block, acc, preparedMsg, opts)
					return err
				},
				"ticktock": func() error {
					_, err := machine.EmulateTickTockTransaction(block, acc, false, opts)
					return err
				},
				"accept": func() error {
					_, err := machine.CheckExternalMessageAccepted(block, acc, preparedMsg, opts)
					return err
				},
			}
			for name, check := range checks {
				t.Run(name, func(t *testing.T) {
					if err := check(); err == nil || !strings.Contains(err.Error(), "historical") {
						t.Fatalf("expected historical configuration error, got %v", err)
					}
				})
			}
		})
	}
}
