//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorPrecompiledUint64Gas(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		Body:        cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatal(err)
	}
	base := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), 13)

	for _, tc := range []struct {
		name     string
		usage    uint64
		wantSkip bool
	}{
		{name: "zero", usage: 0},
		{name: "within limit", usage: 7},
		{name: "maximum signed", usage: math.MaxInt64, wantSkip: true},
		{name: "high bit", usage: 1 << 63, wantSkip: true},
		{name: "maximum unsigned", usage: math.MaxUint64, wantSkip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := referenceTransactionConfigRootWithOverrides(t, base, map[int32]*cell.Cell{
				int32(tlb.ConfigParamPrecompiledContracts): buildTransactionV13PrecompiledConfig(t, code, tc.usage),
			})
			result, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: transactionTestLogicalTime,
				RandSeed:    tonopsTestSeed,
				ConfigRoot:  root,
			})
			if err != nil {
				t.Fatalf("Go transaction: %v", err)
			}
			reference, err := runReferenceOrdinaryTransactionWithConfigRoot(
				shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, root,
			)
			if err != nil {
				t.Fatalf("C++ transaction: %v", err)
			}
			assertTransactionNonComputeParity(t, result.TransactionCell, reference.txCell)
			assertTransactionComputePhaseParity(t, result.TransactionCell, reference.txCell)
			assertShardAccountNonComputeParity(t, result.NextAccount.ShardAccountCell(), reference.shardCell)
			if tc.wantSkip {
				desc := testResultTransaction(t, result).Description.(tlb.TransactionDescriptionOrdinary)
				skipped, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped)
				if !ok || skipped.Reason.Type != tlb.ComputeSkipReasonNoGas {
					t.Fatalf("compute phase = %#v, want no_gas", desc.ComputePhase.Phase)
				}
			}
		})
	}
}
