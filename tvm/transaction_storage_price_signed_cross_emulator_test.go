//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionStoragePriceSignedArithmetic(t *testing.T) {
	if os.Getenv("TVM_NEGATIVE_STORAGE_PRICE_REFERENCE_HELPER") == "1" {
		runTransactionNegativeStoragePriceReferenceHelper(t)
		return
	}
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	baseConfig := mustReferenceTransactionConfigRoot(t)
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	code := makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell())

	t.Run("negative_window_rejected", func(t *testing.T) {
		prices := tlb.ConfigStoragePrices{
			ValidSince: now - 2,
			CellPrice:  1 << 63,
		}
		configRoot := transactionStoragePriceTestConfig(t, baseConfig, prices, nil)
		shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, data, math.MaxUint64, tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(1),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now - 1,
		})
		msg := transactionStoragePriceTestMessage(t, tonopsTestAddr, now)

		cmd := exec.Command(os.Args[0], "-test.run=^TestTVMCrossEmulatorTransactionStoragePriceSignedArithmetic$")
		cmd.Env = append(os.Environ(), "TVM_NEGATIVE_STORAGE_PRICE_REFERENCE_HELPER=1")
		output, refErr := cmd.CombinedOutput()
		if refErr == nil || !bytes.Contains(output, []byte("td::CntObject::WriteError")) {
			t.Fatalf("reference result = %v, want WriteError abort; output:\n%s", refErr, output)
		}
		_, goErr := testEmulateTransaction(NewTVM(), shard, msg, transactionStoragePriceTestParams(tonopsTestAddr, now, configRoot))
		if goErr == nil || !strings.Contains(goErr.Error(), "negative storage payment") {
			t.Fatalf("go error = %v, want negative storage-window rejection", goErr)
		}
	})

	t.Run("mixed_sign_window", func(t *testing.T) {
		prices := tlb.ConfigStoragePrices{
			ValidSince: now - 2,
			BitPrice:   math.MaxInt64,
			CellPrice:  1 << 63,
		}
		configRoot := transactionStoragePriceTestConfig(t, baseConfig, prices, nil)
		shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, data, math.MaxUint64, tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(1),
				BitsUsed:  big.NewInt(2),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now - 1,
		})
		msg := transactionStoragePriceTestMessage(t, tonopsTestAddr, now)

		assertTransactionStoragePriceSuccessParity(t, shard, msg, tonopsTestAddr, now, configRoot)
	})

	t.Run("special_account_bypasses_prices", func(t *testing.T) {
		specialAddr := address.NewAddress(0, 0xFF, bytes.Repeat([]byte{0x42}, 32))
		prices := tlb.ConfigStoragePrices{
			ValidSince:  now - 2,
			BitPrice:    1 << 63,
			CellPrice:   1 << 63,
			MCBitPrice:  1 << 63,
			MCCellPrice: 1 << 63,
		}
		configRoot := transactionStoragePriceTestConfig(t, baseConfig, prices, specialAddr)
		shard := buildTransactionTestShardAccountWithStorageInfo(t, specialAddr, code, data, walletSendTestBalance, tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(1),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now - 1,
		})
		msg := transactionStoragePriceTestMessage(t, specialAddr, now)

		assertTransactionStoragePriceSuccessParity(t, shard, msg, specialAddr, now, configRoot)
	})
}

func runTransactionNegativeStoragePriceReferenceHelper(t *testing.T) {
	now := uint32(tonopsTestTime.Unix())
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	code := makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell())
	prices := tlb.ConfigStoragePrices{
		ValidSince: now - 2,
		CellPrice:  1 << 63,
	}
	configRoot := transactionStoragePriceTestConfig(t, mustReferenceTransactionConfigRoot(t), prices, nil)
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, data, math.MaxUint64, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 1,
	})
	msg := transactionStoragePriceTestMessage(t, tonopsTestAddr, now)

	_, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	t.Fatalf("negative storage-price reference call returned instead of aborting: %v", err)
}

func transactionStoragePriceTestConfig(t *testing.T, base *cell.Cell, prices tlb.ConfigStoragePrices, specialAddr *address.Address) *cell.Cell {
	t.Helper()

	priceCell, err := tlb.ToCell(&prices)
	if err != nil {
		t.Fatalf("failed to build storage prices: %v", err)
	}
	pricesDict := cell.NewDict(32)
	if err = pricesDict.SetIntKey(new(big.Int).SetUint64(uint64(prices.ValidSince)), priceCell); err != nil {
		t.Fatalf("failed to store storage prices: %v", err)
	}
	storageConfig, err := tlb.ToCell(&tlb.StoragePricesConfig{Prices: pricesDict})
	if err != nil {
		t.Fatalf("failed to build storage prices config: %v", err)
	}
	overrides := map[int32]*cell.Cell{
		int32(tlb.ConfigParamStoragePrices): storageConfig,
	}
	if specialAddr != nil {
		overrides[int32(tlb.ConfigParamConfigAddress)], err = tlb.ToCell(&tlb.ConfigParamAddress{Address: specialAddr.Data()})
		if err != nil {
			t.Fatalf("failed to build special account config: %v", err)
		}
	}
	return referenceTransactionConfigRootWithOverrides(t, base, overrides)
}

func transactionStoragePriceTestMessage(t *testing.T, dst *address.Address, now uint32) *cell.Cell {
	t.Helper()

	return mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     dst,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		CreatedAt:   now,
		Body:        cell.BeginCell().EndCell(),
	})
}

func transactionStoragePriceTestParams(addr *address.Address, now uint32, configRoot *cell.Cell) testTxParams {
	return testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	}
}

func assertTransactionStoragePriceSuccessParity(t *testing.T, shard *tlb.ShardAccount, msg *cell.Cell, addr *address.Address, now uint32, configRoot *cell.Cell) {
	t.Helper()

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, transactionStoragePriceTestParams(addr, now, configRoot))
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}
