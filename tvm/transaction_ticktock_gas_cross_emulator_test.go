//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTickTockConfiguredZeroGasLimits(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTickTockStateOnlyCode(t, newData, newData)
	configAddrCell, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: tickTockTestAddr.Data()})
	if err != nil {
		t.Fatalf("failed to build special account config: %v", err)
	}

	tests := []struct {
		name       string
		prices     tlb.ConfigGasLimitsPrices
		additional map[int32]*cell.Cell
	}{
		{
			name: "ordinary_zero_gas_limit",
			prices: tlb.ConfigGasLimitsPrices{
				GasPrice:      1 << 16,
				GasLimit:      0,
				BlockGasLimit: 1_000_000,
			},
		},
		{
			name: "special_zero_special_gas_limit",
			prices: tlb.ConfigGasLimitsPrices{
				HasSeparateSpecialLimit: true,
				GasPrice:                1 << 16,
				GasLimit:                1_000,
				SpecialGasLimit:         0,
				BlockGasLimit:           1_000_000,
			},
			additional: map[int32]*cell.Cell{
				int32(tlb.ConfigParamConfigAddress): configAddrCell,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gasCell, err := tlb.ToCell(&tt.prices)
			if err != nil {
				t.Fatalf("failed to build gas prices: %v", err)
			}
			overrides := map[int32]*cell.Cell{
				int32(tlb.ConfigParamGasPricesMasterchain): gasCell,
			}
			for id, value := range tt.additional {
				overrides[id] = value
			}
			configRoot := referenceTransactionConfigRootWithOverrides(t, baseConfigRoot, overrides)
			shard, err := buildTickTockShardAccountForTest(t, tickTockTestAddr, code, origData, tickTockTestBalance)
			if err != nil {
				t.Fatalf("failed to build tick/tock shard: %v", err)
			}

			goRes, err := testEmulateTickTockTransaction(NewTVM(), shard, false, testTxParams{
				Now:        now,
				RandSeed:   append([]byte(nil), tonopsTestSeed...),
				ConfigRoot: configRoot,
			})
			if err != nil {
				t.Fatalf("go tick emulation failed: %v", err)
			}
			refRes, err := runReferenceTickTockWithConfigRoot(code, origData, tickTockTestAddr, false, now, tickTockTestBalance, tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference tick emulation failed: %v", err)
			}

			assertTickTockComputeSkippedNoGas(t, "go", goRes.TransactionCell)
			assertTickTockComputeSkippedNoGas(t, "reference", refRes.txCell)
			if goRes.Accepted != refRes.accepted || goRes.GasUsed != refRes.gasUsed {
				t.Fatalf("execution result mismatch: go accepted/gas=%t/%d reference=%t/%d", goRes.Accepted, goRes.GasUsed, refRes.accepted, refRes.gasUsed)
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
			}
		})
	}
}

func assertTickTockComputeSkippedNoGas(t *testing.T, side string, txCell *cell.Cell) {
	t.Helper()

	phase := mustTransactionComputePhase(t, txCell)
	skipped, ok := phase.Phase.(tlb.ComputePhaseSkipped)
	if !ok || skipped.Reason.Type != tlb.ComputeSkipReasonNoGas {
		t.Fatalf("%s compute phase = %+v, want skipped/no_gas", side, phase)
	}
}
