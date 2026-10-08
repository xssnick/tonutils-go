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
)

func TestTVMCrossEmulatorTransactionFrozenPrefixUsesAddressRewriteBeforeV10(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	stateDepth := uint64(2)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	stateInit := &tlb.StateInit{
		Depth: &stateDepth,
		Code:  code,
		Data:  origData,
	}
	stateInitCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}

	rewritePrefix := byte(0)
	if stateInitCell.Hash()[0]&0x80 == 0 {
		rewritePrefix = 0x80
	}
	plainAddr := address.NewAddress(0, 0, stateInitCell.Hash())
	anycastAddr := plainAddr.WithAnycast(address.NewAnycast(1, []byte{rewritePrefix}))
	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now - 1,
	}
	storagePrices := transactionVersionStoragePricesCell(t, 100)
	gasLimits := buildTransactionGasLimitsCell(t, 10, 1_000_000)

	for _, addrCase := range []struct {
		name string
		addr *address.Address
	}{
		{name: "no_anycast", addr: plainAddr},
		{name: "anycast_depth_1", addr: anycastAddr},
	} {
		t.Run(addrCase.name, func(t *testing.T) {
			msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     addrCase.addr,
				Amount:      tlb.FromNanoTONU(0),
				Body:        cell.BeginCell().EndCell(),
			})
			shard := buildTransactionTestStoredShardAccount(t, addrCase.addr, tlb.AccountStatusActive, 50, storageInfo, stateInit, nil)

			for _, version := range []uint32{9, 10} {
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, version)
					configRoot = referenceTransactionConfigRootWithOverrides(t, configRoot, map[int32]*cell.Cell{
						int32(tlb.ConfigParamStoragePrices):        storagePrices,
						int32(tlb.ConfigParamGasPricesBasechain):   gasLimits,
						int32(tlb.ConfigParamGasPricesMasterchain): gasLimits,
					})

					goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
						Address:     addrCase.addr,
						Now:         now,
						BlockLT:     transactionTestLogicalTime,
						LogicalTime: transactionTestLogicalTime,
						RandSeed:    append([]byte(nil), tonopsTestSeed...),
						ConfigRoot:  configRoot,
					})
					if err != nil {
						t.Fatalf("go transaction emulation failed: %v", err)
					}
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

					if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
						t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", goRes.TransactionCell.Dump(), refRes.txCell.Dump())
					}
					if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
						t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", goRes.NextAccount.ShardAccountCell().Dump(), refRes.shardCell.Dump())
					}
				})
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionFrozenFixedPrefixUnfreeze(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	now := uint32(tonopsTestTime.Unix())
	stateDepth := uint64(6)
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	stateInit := &tlb.StateInit{
		Depth: &stateDepth,
		Code:  makeTransactionInternalSuccessCode(t, newData),
		Data:  origData,
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("failed to serialize state init: %v", err)
	}
	addr := address.NewAddress(0, 0, stateCell.Hash())
	storageCell, err := buildTransactionAccountStorageCell(
		tlb.AccountStatusFrozen,
		0,
		new(big.Int).SetUint64(walletSendTestBalance),
		nil,
		nil,
		stateCell.Hash(),
	)
	if err != nil {
		t.Fatalf("failed to serialize frozen account storage: %v", err)
	}
	storageUsage, _, err := transactionComputeAccountStorageStat(storageCell, 0)
	if err != nil {
		t.Fatalf("failed to compute frozen account storage usage: %v", err)
	}
	shard := buildTransactionTestFrozenShardAccount(t, addr, stateCell.Hash(), walletSendTestBalance, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: new(big.Int).SetUint64(storageUsage.cells),
			BitsUsed:  new(big.Int).SetUint64(storageUsage.bits),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	})
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		StateInit:   stateInit,
		Body:        cell.BeginCell().EndCell(),
	})

	for _, tc := range []struct {
		version uint32
		status  tlb.AccountStatus
	}{
		{version: 15, status: tlb.AccountStatusUninit},
		{version: 16, status: tlb.AccountStatusActive},
	} {
		t.Run(fmt.Sprintf("v%d", tc.version), func(t *testing.T) {
			configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, tc.version)
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

			goStatus := transactionCrossShardStatus(t, goRes.NextAccount.ShardAccountCell())
			refStatus := transactionCrossShardStatus(t, refRes.shardCell)
			if goStatus != tc.status || refStatus != tc.status {
				t.Fatalf("account status mismatch: go=%s reference=%s want=%s", goStatus, refStatus, tc.status)
			}
			if tc.version < 16 {
				assertOrdinaryTransactionComputeSkipped(t, "go", goRes.TransactionCell, tlb.ComputeSkipReasonBadState)
				assertOrdinaryTransactionComputeSkipped(t, "reference", refRes.txCell, tlb.ComputeSkipReasonBadState)
			} else {
				for _, result := range []struct {
					side   string
					txCell *cell.Cell
				}{
					{side: "go", txCell: goRes.TransactionCell},
					{side: "reference", txCell: refRes.txCell},
				} {
					phase, ok := mustTransactionComputePhase(t, result.txCell).Phase.(tlb.ComputePhaseVM)
					if !ok || !phase.Success {
						t.Fatalf("%s compute phase = %+v, want successful VM execution", result.side, phase)
					}
				}
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
