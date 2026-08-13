//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionStorageUsedRootShrinkWrapRejectsSerialization(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	code := cell.BeginCell().MustStoreUInt(0xC0DE, 16).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	shard := buildTransactionTestShardAccountWithStorageInfo(t, tonopsTestAddr, code, data, 256, tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     now,
		DuePayment:   transactionCoinsPtr(big.NewInt(1)),
	})
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(0),
		Body:        cell.BeginCell().EndCell(),
	})
	baseConfigRoot := mustReferenceTransactionConfigRoot(t)
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, baseConfigRoot, 14)
	_, goErr := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if goErr == nil {
		t.Fatal("go transaction emulation succeeded, want wrapped storage-used serialization rejection")
	}
	_, refErr := runReferenceOrdinaryTransactionWithConfigRoot(
		shard,
		msg,
		now,
		uint64(transactionTestLogicalTime),
		tonopsTestSeed,
		configRoot,
	)
	if refErr == nil || !strings.Contains(refErr.Error(), "cannot serialize new transaction") {
		t.Fatalf("reference error = %v, want wrapped storage-used serialization rejection", refErr)
	}
	if !strings.Contains(goErr.Error(), "failed to serialize account state") {
		t.Fatalf("go error = %v, want account-state serialization rejection", goErr)
	}
}
