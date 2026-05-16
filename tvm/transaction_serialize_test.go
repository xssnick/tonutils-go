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
