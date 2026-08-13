//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionNewBounceGasCounterTruncatesToUint32(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const gasUsage = uint64(1<<32 + 7)
	now := uint32(tonopsTestTime.Unix())
	addr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x39}, 32))
	actions := buildTransactionActionList(t, tlb.ActionReserveCurrency{
		Mode: 16,
		Currency: tlb.CurrencyCollection{
			Coins: tlb.FromNanoTONU(3_000_000_000),
		},
	})
	data := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
	code := makeTransactionInternalActionsCode(t, actions, data)

	configAddr, err := tlb.ToCell(&tlb.ConfigParamAddress{Address: addr.Data()})
	if err != nil {
		t.Fatal(err)
	}
	gasPrices, err := tlb.ToCell(&tlb.ConfigGasLimitsPrices{
		HasSeparateSpecialLimit: true,
		GasPrice:                1,
		GasLimit:                gasUsage + 100,
		SpecialGasLimit:         gasUsage + 100,
		BlockGasLimit:           gasUsage + 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	baseConfig := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithOverrides(t,
		referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, 14),
		map[int32]*cell.Cell{
			int32(tlb.ConfigParamConfigAddress):        configAddr,
			int32(tlb.ConfigParamGasPricesBasechain):   gasPrices,
			int32(tlb.ConfigParamGasPricesMasterchain): gasPrices,
			int32(tlb.ConfigParamPrecompiledContracts): buildTransactionV13PrecompiledConfig(t, code, gasUsage),
		})
	shard := buildTransactionTestShardAccount(t, addr, code, data, 1_000_000_000, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		IHRFee:      tlb.FromNanoTONU(1),
		Body:        cell.BeginCell().EndCell(),
	})

	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(
		shard,
		msg,
		now,
		uint64(transactionTestLogicalTime),
		tonopsTestSeed,
		configRoot,
	)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}
	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch")
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch")
	}
	if goRes.GasUsed != int64(gasUsage) || refRes.gasUsed != int64(gasUsage) {
		t.Fatalf("gas used mismatch: go=%d reference=%d want=%d", goRes.GasUsed, refRes.gasUsed, gasUsage)
	}
}
