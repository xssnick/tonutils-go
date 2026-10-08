//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionAnycastWorkchainBoundary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	baseConfig := mustReferenceTransactionConfigRoot(t)
	statuses := []struct {
		name   string
		status tlb.AccountStatus
	}{
		{name: "active", status: tlb.AccountStatusActive},
		{name: "uninit", status: tlb.AccountStatusUninit},
		{name: "frozen", status: tlb.AccountStatusFrozen},
		{name: "account_none", status: tlb.AccountStatusNonExist},
	}

	for _, version := range []uint32{9, 15} {
		configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfig, version)
		for _, workchain := range []int32{-128, 126, 127} {
			for _, hasAnycast := range []bool{false, true} {
				for _, tc := range statuses {
					name := fmt.Sprintf("v%d/wc%d/anycast_%t/%s", version, workchain, hasAnycast, tc.name)
					t.Run(name, func(t *testing.T) {
						addr := address.NewAddress(0, byte(workchain), bytes.Repeat([]byte{0x3f}, 32))
						if hasAnycast {
							addr = addr.WithAnycast(address.NewAnycast(1, []byte{0x80}))
						}
						shard := transactionWorkchainBoundaryShard(t, addr, tc.status, newData, now)
						msgCell := mustTransactionMsgCell(t, &tlb.InternalMessage{
							IHRDisabled: true,
							SrcAddr:     internalEmulationSrcAddr,
							DstAddr:     addr,
							Amount:      tlb.FromNanoTONU(1_000_000_000),
							Body:        cell.BeginCell().EndCell(),
						})

						goRes, goErr := testEmulateTransaction(NewTVM(), shard, msgCell, testTxParams{
							Address:     addr,
							Now:         now,
							BlockLT:     transactionTestLogicalTime,
							LogicalTime: transactionTestLogicalTime,
							RandSeed:    append([]byte(nil), tonopsTestSeed...),
							ConfigRoot:  configRoot,
						})
						refRes, refErr := runReferenceOrdinaryTransactionWithConfigRoot(
							shard,
							msgCell,
							now,
							uint64(transactionTestLogicalTime),
							tonopsTestSeed,
							configRoot,
						)

						wantReject := tc.status != tlb.AccountStatusNonExist && hasAnycast && workchain == 127
						if wantReject {
							if goErr == nil || refErr == nil {
								t.Fatalf("serialization rejection mismatch: go=%v reference=%v", goErr, refErr)
							}
							if !strings.Contains(goErr.Error(), "final account address is not canonical") {
								t.Fatalf("unexpected Go rejection: %v", goErr)
							}
							if !strings.Contains(refErr.Error(), "-669") || !strings.Contains(refErr.Error(), "cannot serialize new transaction") {
								t.Fatalf("unexpected reference rejection: %v", refErr)
							}
							return
						}
						if goErr != nil || refErr != nil {
							t.Fatalf("transaction error mismatch: go=%v reference=%v", goErr, refErr)
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
		}
	}
}

func transactionWorkchainBoundaryShard(t *testing.T, addr *address.Address, status tlb.AccountStatus, newData *cell.Cell, now uint32) *tlb.ShardAccount {
	t.Helper()

	storage := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(1),
			BitsUsed:  big.NewInt(1_000),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
	}
	switch status {
	case tlb.AccountStatusActive:
		return buildTransactionTestStoredShardAccount(t, addr, status, walletSendTestBalance, storage, &tlb.StateInit{
			Code: makeTransactionInternalSuccessCode(t, newData),
			Data: cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell(),
		}, nil)
	case tlb.AccountStatusUninit:
		return buildTransactionTestStoredShardAccount(t, addr, status, walletSendTestBalance, storage, nil, nil)
	case tlb.AccountStatusFrozen:
		return buildTransactionTestStoredShardAccount(t, addr, status, walletSendTestBalance, storage, nil, bytes.Repeat([]byte{0x55}, 32))
	case tlb.AccountStatusNonExist:
		return buildTransactionTestNoneShardAccount(t)
	default:
		t.Fatalf("unsupported account status %s", status)
		return nil
	}
}
