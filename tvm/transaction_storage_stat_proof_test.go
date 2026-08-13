package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestBindAccountStorageStatValidatesAndAvoidsPrunedStateWalk(t *testing.T) {
	addr := address.MustParseRawAddr("0:" + "2b63f898590aa9e300fd0e696bc834b9ebe3ab75ec16dbc2a90955d960cc6a85")
	deepData := cell.BeginCell().MustStoreUInt(0x3a, 8).EndCell()
	fullOldData := cell.BeginCell().MustStoreRef(deepData).EndCell()
	fullNewData := cell.BeginCell().MustStoreRef(fullOldData).EndCell()
	fullCode := makeTransactionExternalSuccessCode(t, fullNewData)

	fullStorage := storageStatProofAccountStorage(t, fullCode, fullOldData)
	usage, statRoot, err := transactionComputeAccountStorageStat(fullStorage)
	if err != nil {
		t.Fatal(err)
	}

	prunedDeep, err := cell.CreatePrunedBranch(deepData, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	prunedOldData := cell.BeginCell().MustStoreRef(prunedDeep).EndCell()
	prunedNewData := cell.BeginCell().MustStoreRef(prunedOldData).EndCell()
	prunedCode := makeTransactionExternalSuccessCode(t, prunedNewData)
	if prunedCode.HashKey() != fullCode.HashKey() {
		t.Fatal("pruned code does not preserve the original root hash")
	}
	if prunedOldData.HashKey() != fullOldData.HashKey() || prunedNewData.HashKey() != fullNewData.HashKey() {
		t.Fatal("pruned data does not preserve the original root hashes")
	}

	accountCell := storageStatProofAccount(t, addr, prunedCode, prunedOldData, usage, statRoot)
	account, err := PrepareAccount(&tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
	}, addr)
	if err != nil {
		t.Fatal(err)
	}
	cfg := transactionTestConfigWithGlobalVersion(t, uint32(vmcore.MaxSupportedGlobalVersion))
	block, err := cfg.NewBlockContext(BlockOptions{
		Now:      uint32(tonopsTestTime.Unix()),
		BlockLT:  transactionTestLogicalTime,
		RandSeed: append([]byte(nil), tonopsTestSeed...),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, foreignStat, err := transactionComputeAccountStorageStat(
		cell.BeginCell().MustStoreRef(cell.BeginCell().MustStoreUInt(0x99, 8).EndCell()).EndCell(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = block.BindAccountStorageStat(account, foreignStat); err == nil {
		t.Fatal("storage stat with a different authenticated hash was accepted")
	}
	if err = block.BindAccountStorageStat(account, statRoot); err != nil {
		t.Fatalf("bind valid storage stat: %v", err)
	}

	runtime := account.runtime
	exceeds, err := transactionAccountStateExceedsLimits(
		&runtime,
		prunedCode,
		prunedNewData,
		nil,
		block.cfg,
		true,
	)
	if err != nil {
		t.Fatalf("check state limits through bound stat: %v", err)
	}
	if exceeds {
		t.Fatal("small account unexpectedly exceeds state limits")
	}

	messageCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: addr,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(messageCell)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewTVM().EmulateTransaction(block, account, message, TransactionOptions{
		LogicalTime: transactionTestLogicalTime,
		BuildProof:  true,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		t.Fatalf("emulate transaction with bound storage stat: %v", err)
	}
	if !result.Accepted || result.Proof == nil || result.NextAccount.State().StateInit.Data.HashKey() != fullNewData.HashKey() {
		t.Fatal("public emulation did not commit the expected data root")
	}
}

func storageStatProofAccountStorage(t *testing.T, code, data *cell.Cell) *cell.Cell {
	t.Helper()

	stateInit, err := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return cell.BeginCell().
		MustStoreUInt(0, 64).
		MustStoreBigCoins(big.NewInt(1_000_000_000)).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInit.ToBuilder()).
		EndCell()
}

func storageStatProofAccount(t *testing.T, addr *address.Address, code, data *cell.Cell, usage transactionUsage, statRoot *cell.Cell) *cell.Cell {
	t.Helper()

	storage := storageStatProofAccountStorage(t, code, data)
	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: new(big.Int).SetUint64(usage.cells),
			BitsUsed:  new(big.Int).SetUint64(usage.bits),
		},
		StorageExtra: tlb.StorageExtraInfo{DictHash: transactionAccountStorageStatRootHash(statRoot)},
		LastPaid:     uint32(tonopsTestTime.Unix()),
	}
	storageInfoCell, err := tlb.ToCell(&storageInfo)
	if err != nil {
		t.Fatal(err)
	}
	return cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreBuilder(storageInfoCell.ToBuilder()).
		MustStoreBuilder(storage.ToBuilder()).
		EndCell()
}
