package tvm

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func buildTransactionShardAccountCell(account *cell.Cell, lastHash []byte, lastLT uint64) *cell.Cell {
	return cell.BeginCell().
		MustStoreRef(account).
		MustStoreSlice(transactionBits256(lastHash), 256).
		MustStoreUInt(lastLT, 64).
		EndCell()
}

func buildTransactionAccountStateCell(addr *address.Address, storageUsed tlb.StorageUsed, storageExtraDictHash []byte, lastPaid uint32, duePayment *tlb.Coins, storage *cell.Builder) (*cell.Cell, error) {
	builder := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr)
	if err := storeTransactionStorageInfo(builder, storageUsed, storageExtraDictHash, lastPaid, duePayment); err != nil {
		return nil, fmt.Errorf("failed to serialize storage info: %w", err)
	}
	if err := builder.StoreBuilder(storage); err != nil {
		return nil, fmt.Errorf("failed to serialize account storage: %w", err)
	}
	return builder.EndCell(), nil
}

func buildTransactionAccountStorageCell(status tlb.AccountStatus, lastTxLT uint64, balance *big.Int, extraCurrencies *cell.Dictionary, stateInit *tlb.StateInit, stateHash []byte) (*cell.Cell, error) {
	builder, err := buildTransactionAccountStorageBuilder(status, lastTxLT, balance, extraCurrencies, stateInit, stateHash)
	if err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func buildTransactionAccountStorageBuilder(status tlb.AccountStatus, lastTxLT uint64, balance *big.Int, extraCurrencies *cell.Dictionary, stateInit *tlb.StateInit, stateHash []byte) (*cell.Builder, error) {
	builder := cell.BeginCell().
		MustStoreUInt(lastTxLT, 64)
	if err := builder.StoreBigCoins(balance); err != nil {
		return nil, fmt.Errorf("failed to serialize account balance: %w", err)
	}
	if err := builder.StoreDict(extraCurrencies); err != nil {
		return nil, fmt.Errorf("failed to serialize extra currencies: %w", err)
	}

	switch status {
	case tlb.AccountStatusActive:
		builder.MustStoreBoolBit(true)
		if err := storeTransactionStateInit(builder, stateInit); err != nil {
			return nil, fmt.Errorf("failed to serialize state init: %w", err)
		}
	case tlb.AccountStatusFrozen:
		builder.
			MustStoreBoolBit(false).
			MustStoreBoolBit(true).
			MustStoreSlice(transactionBits256(stateHash), 256)
	case tlb.AccountStatusUninit:
		builder.
			MustStoreBoolBit(false).
			MustStoreBoolBit(false)
	case tlb.AccountStatusNonExist:
		return nil, fmt.Errorf("non-existing account cannot be serialized as account storage")
	default:
		return nil, fmt.Errorf("unknown account status %s", status)
	}

	return builder, nil
}

func buildTransactionStateInitCell(stateInit *tlb.StateInit) (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeTransactionStateInit(builder, stateInit); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeTransactionStateInit(builder *cell.Builder, stateInit *tlb.StateInit) error {
	if stateInit.Depth == nil {
		builder.MustStoreBoolBit(false)
	} else {
		builder.MustStoreBoolBit(true)
		if err := builder.StoreUInt(*stateInit.Depth, 5); err != nil {
			return err
		}
	}

	if stateInit.TickTock == nil {
		builder.MustStoreBoolBit(false)
	} else {
		builder.
			MustStoreBoolBit(true).
			MustStoreBoolBit(stateInit.TickTock.Tick).
			MustStoreBoolBit(stateInit.TickTock.Tock)
	}

	if err := builder.StoreMaybeRef(stateInit.Code); err != nil {
		return err
	}
	if err := builder.StoreMaybeRef(stateInit.Data); err != nil {
		return err
	}
	return builder.StoreDict(stateInit.Lib)
}

func storeTransactionAccountStatus(builder *cell.Builder, status tlb.AccountStatus) error {
	switch status {
	case tlb.AccountStatusUninit:
		return builder.StoreUInt(0b00, 2)
	case tlb.AccountStatusFrozen:
		return builder.StoreUInt(0b01, 2)
	case tlb.AccountStatusActive:
		return builder.StoreUInt(0b10, 2)
	case tlb.AccountStatusNonExist:
		return builder.StoreUInt(0b11, 2)
	default:
		return fmt.Errorf("unknown account status %s", status)
	}
}

func storeTransactionCurrencyCollection(builder *cell.Builder, coins *big.Int, extraCurrencies *cell.Dictionary) error {
	if err := builder.StoreBigCoins(coins); err != nil {
		return err
	}
	return builder.StoreDict(extraCurrencies)
}

func storeTransactionStorageInfo(builder *cell.Builder, storageUsed tlb.StorageUsed, storageExtraDictHash []byte, lastPaid uint32, duePayment *tlb.Coins) error {
	if err := storeTransactionStorageUsed(builder, storageUsed); err != nil {
		return err
	}
	if err := storeTransactionStorageExtra(builder, storageExtraDictHash); err != nil {
		return err
	}

	builder.MustStoreUInt(uint64(lastPaid), 32)
	if duePayment == nil {
		builder.MustStoreBoolBit(false)
		return nil
	}
	builder.MustStoreBoolBit(true)
	return builder.StoreBigCoins(duePayment.Nano())
}

func storeTransactionStorageUsed(builder *cell.Builder, used tlb.StorageUsed) error {
	if err := builder.StoreBigVarUInt(used.CellsUsed, 7); err != nil {
		return err
	}
	return builder.StoreBigVarUInt(used.BitsUsed, 7)
}

func storeTransactionStorageExtra(builder *cell.Builder, dictHash []byte) error {
	if dictHash == nil {
		return builder.StoreUInt(0b000, 3)
	}
	return storeTransactionStorageExtraInfo(builder, dictHash)
}

func storeTransactionStorageExtraInfo(builder *cell.Builder, dictHash []byte) error {
	builder.MustStoreUInt(0b001, 3)
	return builder.StoreSlice(transactionBits256(dictHash), 256)
}

func buildTransactionHashUpdateCell(oldHash, newHash []byte) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0x72, 8).
		MustStoreSlice(transactionBits256(oldHash), 256).
		MustStoreSlice(transactionBits256(newHash), 256).
		EndCell()
}
