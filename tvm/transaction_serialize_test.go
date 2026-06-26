package tvm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionDirectAccountStorageSerializationMatchesTLB(t *testing.T) {
	depth := uint64(3)
	code := cell.BeginCell().MustStoreUInt(0xc0de, 16).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xda7a, 16).EndCell()
	libs := cell.NewDict(256)
	if err := libs.Set(
		cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x11}, 32), 256).EndCell(),
		cell.BeginCell().MustStoreUInt(0xaa, 8).EndCell(),
	); err != nil {
		t.Fatal(err)
	}

	stateInit := &tlb.StateInit{
		Depth:    &depth,
		TickTock: &tlb.TickTock{Tick: true},
		Code:     code,
		Data:     data,
		Lib:      libs,
	}
	extra := cell.NewDict(32)
	if err := extra.Set(
		cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
		cell.BeginCell().MustStoreUInt(0xbb, 8).EndCell(),
	); err != nil {
		t.Fatal(err)
	}

	cases := []tlb.AccountStorage{
		{
			Status:            tlb.AccountStatusActive,
			LastTransactionLT: 123,
			Balance:           tlb.FromNanoTON(big.NewInt(456)),
			ExtraCurrencies:   extra,
			StateInit:         stateInit,
		},
		{
			Status:            tlb.AccountStatusFrozen,
			LastTransactionLT: 124,
			Balance:           tlb.FromNanoTON(big.NewInt(789)),
			StateHash:         bytes.Repeat([]byte{0x22}, 32),
		},
		{
			Status:            tlb.AccountStatusUninit,
			LastTransactionLT: 125,
			Balance:           tlb.ZeroCoins,
		},
	}

	for _, storage := range cases {
		builder, err := buildTransactionAccountStorageBuilder(storage.Status, storage.LastTransactionLT, storage.Balance.Nano(), storage.ExtraCurrencies, storage.StateInit, storage.StateHash)
		if err != nil {
			t.Fatal(err)
		}
		got := builder.EndCell()

		want, err := tlb.ToCell(&storage)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Hash(), want.Hash()) {
			t.Fatalf("account storage %s hash mismatch: got %x want %x", storage.Status, got.Hash(), want.Hash())
		}
	}
}

func TestTransactionDirectAccountStateSerializationMatchesTLB(t *testing.T) {
	addr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x33}, 32))
	code := cell.BeginCell().MustStoreUInt(0x01, 8).EndCell()
	data := cell.BeginCell().MustStoreUInt(0x02, 8).EndCell()
	dictHash := bytes.Repeat([]byte{0x44}, 32)
	due := tlb.FromNanoTON(big.NewInt(100))

	storage := tlb.AccountStorage{
		Status:            tlb.AccountStatusActive,
		LastTransactionLT: 999,
		Balance:           tlb.FromNanoTON(big.NewInt(1000)),
		StateInit: &tlb.StateInit{
			Code: code,
			Data: data,
		},
	}
	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(3),
			BitsUsed:  big.NewInt(77),
		},
		StorageExtra: tlb.StorageExtraInfo{DictHash: dictHash},
		LastPaid:     55,
		DuePayment:   &due,
	}

	storageBuilder, err := buildTransactionAccountStorageBuilder(storage.Status, storage.LastTransactionLT, storage.Balance.Nano(), storage.ExtraCurrencies, storage.StateInit, storage.StateHash)
	if err != nil {
		t.Fatal(err)
	}
	got, err := buildTransactionAccountStateCell(addr, storageInfo.StorageUsed, dictHash, storageInfo.LastPaid, storageInfo.DuePayment, storageBuilder)
	if err != nil {
		t.Fatal(err)
	}

	want, err := tlb.ToCell(&tlb.AccountState{
		IsValid:        true,
		Address:        addr,
		StorageInfo:    storageInfo,
		AccountStorage: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Hash(), want.Hash()) {
		t.Fatalf("account state hash mismatch: got %x want %x", got.Hash(), want.Hash())
	}
}

func TestTransactionDirectSmallCellsMatchTLB(t *testing.T) {
	account := cell.BeginCell().MustStoreUInt(0x01, 8).EndCell()
	txHash := bytes.Repeat([]byte{0x55}, 32)
	shardCell := buildTransactionShardAccountCell(account, txHash, 777)
	wantShard, err := tlb.ToCell(&tlb.ShardAccount{
		Account:       account,
		LastTransHash: txHash,
		LastTransLT:   777,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shardCell.Hash(), wantShard.Hash()) {
		t.Fatalf("shard account hash mismatch: got %x want %x", shardCell.Hash(), wantShard.Hash())
	}

	oldHash := bytes.Repeat([]byte{0x66}, 32)
	newHash := bytes.Repeat([]byte{0x77}, 32)
	hashUpdate := buildTransactionHashUpdateCell(oldHash, newHash)
	wantHashUpdate, err := tlb.ToCell(&tlb.HashUpdate{OldHash: oldHash, NewHash: newHash})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hashUpdate.Hash(), wantHashUpdate.Hash()) {
		t.Fatalf("hash update mismatch: got %x want %x", hashUpdate.Hash(), wantHashUpdate.Hash())
	}
}

func TestTransactionDirectAccountStorageRejectsInvalidInputs(t *testing.T) {
	if _, err := buildTransactionAccountStorageBuilder(tlb.AccountStatusNonExist, 1, big.NewInt(0), nil, nil, nil); err == nil {
		t.Fatal("expected non-existing account storage error")
	}
	if _, err := buildTransactionAccountStorageBuilder(tlb.AccountStatus("UNKNOWN"), 1, big.NewInt(0), nil, nil, nil); err == nil {
		t.Fatal("expected unknown account status error")
	}
	if _, err := buildTransactionAccountStorageCell(tlb.AccountStatusActive, 1, big.NewInt(-1), nil, &tlb.StateInit{}, nil); err == nil {
		t.Fatal("expected account storage cell balance error")
	}
	if _, err := buildTransactionAccountStorageBuilder(tlb.AccountStatusActive, 1, big.NewInt(-1), nil, &tlb.StateInit{}, nil); err == nil {
		t.Fatal("expected negative account balance error")
	}

	depth := uint64(32)
	if _, err := buildTransactionAccountStorageBuilder(tlb.AccountStatusActive, 1, big.NewInt(0), nil, &tlb.StateInit{Depth: &depth}, nil); err == nil {
		t.Fatal("expected oversized state init depth error")
	}
}

func TestTransactionDirectStateAndStatusSerializationRejectsInvalidInputs(t *testing.T) {
	if err := storeTransactionAccountStatus(cell.BeginCell(), tlb.AccountStatus("UNKNOWN")); err == nil {
		t.Fatal("expected unknown account status error")
	}

	depth := uint64(32)
	if _, err := buildTransactionStateInitCell(&tlb.StateInit{Depth: &depth}); err == nil {
		t.Fatal("expected oversized state init depth error")
	}
}

func TestTransactionDirectCurrencyAndStorageSerializationRejectsInvalidInputs(t *testing.T) {
	if err := storeTransactionCurrencyCollection(cell.BeginCell(), big.NewInt(-1), nil); err == nil {
		t.Fatal("expected negative currency error")
	}
	tooBigCoins := new(big.Int).Lsh(big.NewInt(1), 16*8)
	if err := storeTransactionCurrencyCollection(cell.BeginCell(), tooBigCoins, nil); err == nil {
		t.Fatal("expected oversized currency error")
	}

	if err := storeTransactionStorageUsed(cell.BeginCell(), tlb.StorageUsed{
		CellsUsed: big.NewInt(-1),
		BitsUsed:  big.NewInt(0),
	}); err == nil {
		t.Fatal("expected negative storage cells error")
	}
	if err := storeTransactionStorageUsed(cell.BeginCell(), tlb.StorageUsed{
		CellsUsed: big.NewInt(0),
		BitsUsed:  big.NewInt(-1),
	}); err == nil {
		t.Fatal("expected negative storage bits error")
	}
}

func TestTransactionDirectAccountStateRejectsInvalidInputs(t *testing.T) {
	if _, err := buildTransactionAccountStateCell(tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(-1),
		BitsUsed:  big.NewInt(0),
	}, nil, 0, nil, cell.BeginCell()); err == nil {
		t.Fatal("expected invalid storage info error")
	}

	largeStorage := cell.BeginCell().MustStoreSlice(make([]byte, 100), 800)
	if _, err := buildTransactionAccountStateCell(tonopsTestAddr, tlb.StorageUsed{
		CellsUsed: big.NewInt(0),
		BitsUsed:  big.NewInt(0),
	}, nil, 0, nil, largeStorage); err == nil {
		t.Fatal("expected oversized account storage builder error")
	}
}

func TestTransactionBuildCellRejectsInvalidEnvelopeInputs(t *testing.T) {
	t.Run("account address", func(t *testing.T) {
		params := transactionSerializeTestBuildParams()
		params.accountAddr = nil
		if _, err := buildTransactionCell(params); err == nil {
			t.Fatal("expected invalid account address error")
		}
	})

	t.Run("description", func(t *testing.T) {
		params := transactionSerializeTestBuildParams()
		params.description.skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonType("UNKNOWN")}
		if _, err := buildTransactionCell(params); err == nil {
			t.Fatal("expected description serialization error")
		}
	})

	t.Run("original status", func(t *testing.T) {
		params := transactionSerializeTestBuildParams()
		params.origStatus = tlb.AccountStatus("UNKNOWN")
		if _, err := buildTransactionCell(params); err == nil {
			t.Fatal("expected original status serialization error")
		}
	})

	t.Run("end status", func(t *testing.T) {
		params := transactionSerializeTestBuildParams()
		params.endStatus = tlb.AccountStatus("UNKNOWN")
		if _, err := buildTransactionCell(params); err == nil {
			t.Fatal("expected end status serialization error")
		}
	})

	t.Run("total fees", func(t *testing.T) {
		params := transactionSerializeTestBuildParams()
		params.totalFees = big.NewInt(-1)
		if _, err := buildTransactionCell(params); err == nil {
			t.Fatal("expected total fees serialization error")
		}
	})
}

func TestTransactionBuildComputeSkippedPhase(t *testing.T) {
	phase := buildTransactionComputePhase(transactionBuildDescriptionParams{
		skipReason: &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas},
	})

	skipped, ok := phase.Phase.(tlb.ComputePhaseSkipped)
	if !ok {
		t.Fatalf("compute phase = %T, want skipped", phase.Phase)
	}
	if skipped.Reason.Type != tlb.ComputeSkipReasonNoGas {
		t.Fatalf("skip reason = %s, want %s", skipped.Reason.Type, tlb.ComputeSkipReasonNoGas)
	}
}

func TestFillTransactionExecutionResultRejectsMalformedTransactionCell(t *testing.T) {
	malformed := cell.BeginCell().MustStoreUInt(0, 1).EndCell()
	if err := fillTransactionExecutionResult(&TransactionExecutionResult{}, malformed, nil, nil, nil, 1); err == nil {
		t.Fatal("expected malformed transaction cell error")
	}
}

func transactionSerializeTestBuildParams() transactionBuildParams {
	return transactionBuildParams{
		accountAddr: tonopsTestAddr,
		startLT:     1,
		now:         uint32(tonopsTestTime.Unix()),
		origStatus:  tlb.AccountStatusActive,
		endStatus:   tlb.AccountStatusActive,
		totalFees:   big.NewInt(0),
		description: transactionBuildDescriptionParams{
			skipReason: &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState},
		},
	}
}
