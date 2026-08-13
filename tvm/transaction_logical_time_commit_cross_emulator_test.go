//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
)

func TestTVMCrossEmulatorTransactionLogicalTimeCommitRange(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := mustReferenceTransactionConfigRoot(t)
	code := makeTransactionInternalSuccessCode(t, cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell())
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()

	tests := []struct {
		name        string
		storageLT   uint64
		shardLT     uint64
		createdLT   uint64
		logicalTime int64
		referenceLT uint64
	}{
		{
			name:        "account_end_lt_wrap",
			storageLT:   math.MaxUint64,
			shardLT:     math.MaxUint64 - 1,
			logicalTime: transactionTestLogicalTime,
			referenceLT: uint64(transactionTestLogicalTime),
		},
		{
			name:        "created_lt_wrap_before_account_storage",
			storageLT:   1,
			createdLT:   math.MaxUint64,
			logicalTime: transactionTestLogicalTime,
			referenceLT: uint64(transactionTestLogicalTime),
		},
		{
			name:      "near_max_account_lt_wrapped_fallback_and_end",
			storageLT: math.MaxUint64,
			shardLT:   math.MaxUint64 - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shard := buildTransactionLogicalTimeTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now, tt.storageLT, tt.shardLT)
			msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				CreatedLT:   tt.createdLT,
				CreatedAt:   now,
				Body:        cell.BeginCell().EndCell(),
			})

			_, refErr := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, tt.referenceLT, tonopsTestSeed, configRoot)
			if refErr == nil || !strings.Contains(refErr.Error(), "cannot commit new transaction") {
				t.Fatalf("reference error = %v, want logical-time commit rejection", refErr)
			}

			_, goErr := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     transactionTestLogicalTime,
				LogicalTime: tt.logicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if goErr == nil || !strings.Contains(goErr.Error(), "invalid logical time range") {
				t.Fatalf("go error = %v, want logical-time commit rejection", goErr)
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionHighFallbackLogicalTime(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := mustReferenceTransactionConfigRoot(t)
	prevLT := uint64(math.MaxInt64) + 123
	storageLT := prevLT + 1
	wantLT := (prevLT/transactionLTAlignment + 1) * transactionLTAlignment
	code := transactionLogicalTimeC7Code(t)
	shard := buildTransactionLogicalTimeTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, now, storageLT, prevLT)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		CreatedAt:   now,
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:    tonopsTestAddr,
		Now:        now,
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		ConfigRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, 0, tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.StartLT != wantLT {
		t.Fatalf("go start LT = %d, want fallback %d", goRes.StartLT, wantLT)
	}
	var refTx tlb.Transaction
	if err = tlb.Parse(&refTx, refRes.txCell); err != nil {
		t.Fatalf("failed to decode reference transaction: %v", err)
	}
	if refTx.LT != wantLT {
		t.Fatalf("reference start LT = %d, want fallback %d", refTx.LT, wantLT)
	}
	wantData := cell.BeginCell().
		MustStoreUInt(wantLT, 64).
		MustStoreUInt(wantLT, 64).
		EndCell()
	if got := testResultAccountState(goRes).StateInit.Data; !bytes.Equal(got.Hash(), wantData.Hash()) {
		t.Fatalf("go c7 trans_lt/block_lt data = %s, want %s; exit=%d accepted=%t tx=%s", got.Dump(), wantData.Dump(), goRes.ExitCode, goRes.Accepted, transactionCrossTxSummary(t, goRes.TransactionCell))
	}
	if got := transactionLogicalTimeTestShardData(t, refRes.shardCell); !bytes.Equal(got.Hash(), wantData.Hash()) {
		t.Fatalf("reference c7 trans_lt/block_lt data = %s, want %s", got.Dump(), wantData.Dump())
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}

func TestTVMCrossEmulatorTransactionBlockLogicalTimeBeforeStartBumps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := mustReferenceTransactionConfigRoot(t)
	const (
		storageLT = uint64(3_000_123)
		shardLT   = storageLT - 1
	)
	tests := []struct {
		name        string
		logicalTime int64
		blockLT     int64
		referenceLT uint64
		wantBlockLT uint64
	}{
		{
			name:        "derived_from_requested_lt",
			logicalTime: 1_000_000,
			referenceLT: 1_000_000,
			wantBlockLT: 1_000_000,
		},
		{
			name:        "explicit_block_lt_preserved",
			logicalTime: 1_000_000,
			blockLT:     2_000_000,
			referenceLT: 2_000_000,
			wantBlockLT: 2_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := transactionLogicalTimeC7Code(t)
			shard := buildTransactionLogicalTimeTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, now, storageLT, shardLT)
			msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     internalEmulationSrcAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(1_000_000_000),
				CreatedAt:   now,
				Body:        cell.BeginCell().EndCell(),
			})

			goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
				Address:     tonopsTestAddr,
				Now:         now,
				BlockLT:     tt.blockLT,
				LogicalTime: tt.logicalTime,
				RandSeed:    append([]byte(nil), tonopsTestSeed...),
				ConfigRoot:  configRoot,
			})
			if err != nil {
				t.Fatalf("go transaction emulation failed: %v", err)
			}
			refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, tt.referenceLT, tonopsTestSeed, configRoot)
			if err != nil {
				t.Fatalf("reference transaction emulation failed: %v", err)
			}

			if goRes.StartLT != storageLT {
				t.Fatalf("go start LT = %d, want account bump %d", goRes.StartLT, storageLT)
			}
			wantData := cell.BeginCell().
				MustStoreUInt(storageLT, 64).
				MustStoreUInt(tt.wantBlockLT, 64).
				EndCell()
			if got := testResultAccountState(goRes).StateInit.Data; !bytes.Equal(got.Hash(), wantData.Hash()) {
				t.Fatalf("go c7 trans_lt/block_lt data = %s, want %s", got.Dump(), wantData.Dump())
			}
			if got := transactionLogicalTimeTestShardData(t, refRes.shardCell); !bytes.Equal(got.Hash(), wantData.Hash()) {
				t.Fatalf("reference c7 trans_lt/block_lt data = %s, want %s", got.Dump(), wantData.Dump())
			}
			if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
				t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
			}
			if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
				t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
			}
		})
	}
}

func TestTVMCrossEmulatorTransactionExplicitFullWidthLogicalTime(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := mustReferenceTransactionConfigRoot(t)
	requestedLT := uint64(math.MaxInt64) + 1_234_567
	wantBlockLT := transactionBlockLogicalTime(requestedLT)
	code := transactionLogicalTimeC7Code(t)
	shard := buildTransactionLogicalTimeTestShardAccount(t, tonopsTestAddr, code, cell.BeginCell().EndCell(), walletSendTestBalance, now, 1, 0)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		CreatedAt:   now,
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:           tonopsTestAddr,
		Now:               now,
		BlockLT:           transactionTestLogicalTime,
		BlockLTUint64:     wantBlockLT,
		LogicalTime:       transactionTestLogicalTime,
		LogicalTimeUint64: requestedLT,
		RandSeed:          append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:        configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, requestedLT, tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.StartLT != requestedLT {
		t.Fatalf("go start LT = %d, want %d", goRes.StartLT, requestedLT)
	}
	wantData := cell.BeginCell().
		MustStoreUInt(requestedLT, 64).
		MustStoreUInt(wantBlockLT, 64).
		EndCell()
	if got := testResultAccountState(goRes).StateInit.Data; !bytes.Equal(got.Hash(), wantData.Hash()) {
		t.Fatalf("go c7 trans_lt/block_lt data = %s, want %s", got.Dump(), wantData.Dump())
	}
	if got := transactionLogicalTimeTestShardData(t, refRes.shardCell); !bytes.Equal(got.Hash(), wantData.Hash()) {
		t.Fatalf("reference c7 trans_lt/block_lt data = %s, want %s", got.Dump(), wantData.Dump())
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}

func transactionLogicalTimeC7Code(t *testing.T) *cell.Cell {
	t.Helper()

	return codeFromBuilders(t,
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		funcsop.BLOCKLT().Serialize(),
		funcsop.LTIME().Serialize(),
		cellsliceop.NEWC().Serialize(),
		cellsliceop.STU(64).Serialize(),
		cellsliceop.STU(64).Serialize(),
		cellsliceop.ENDC().Serialize(),
		execop.POPCTR(4).Serialize(),
	)
}

func transactionLogicalTimeTestShardData(t *testing.T, shardCell *cell.Cell) *cell.Cell {
	t.Helper()

	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, shardCell); err != nil {
		t.Fatalf("failed to decode shard account: %v", err)
	}
	var account tlb.AccountState
	if err := tlb.Parse(&account, shard.Account); err != nil {
		t.Fatalf("failed to decode account state: %v", err)
	}
	if account.StateInit == nil || account.StateInit.Data == nil {
		t.Fatal("account has no data")
	}
	return account.StateInit.Data
}

func buildTransactionLogicalTimeTestShardAccount(t *testing.T, addr *address.Address, code, data *cell.Cell, balance uint64, lastPaid uint32, storageLT, shardLT uint64) *tlb.ShardAccount {
	t.Helper()

	accountCell, err := tlb.ToCell(&tlb.AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     lastPaid,
		},
		AccountStorage: tlb.AccountStorage{
			Status:            tlb.AccountStatusActive,
			LastTransactionLT: storageLT,
			Balance:           tlb.FromNanoTONU(balance),
			StateInit: &tlb.StateInit{
				Code: code,
				Data: data,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to build account state: %v", err)
	}

	return &tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   shardLT,
	}
}
