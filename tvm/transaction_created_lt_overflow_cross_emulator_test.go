//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The reference transaction engine increments an inbound internal message's
// created_lt with uint64 arithmetic. MaxUint64 therefore wraps the transaction
// start LT to zero when the account has no previous transaction.
func TestTVMCrossEmulatorTransactionCreatedLTUint64Wrap(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	now := uint32(tonopsTestTime.Unix())
	configRoot := mustReferenceTransactionConfigRoot(t)
	origData := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	code := makeTransactionInternalSuccessCode(t, newData)
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, origData, walletSendTestBalance, now)
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     tonopsTestAddr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
		CreatedLT:   math.MaxUint64,
		CreatedAt:   now,
		Body:        cell.BeginCell().EndCell(),
	})

	goRes, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address:     tonopsTestAddr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  configRoot,
	})
	if err != nil {
		t.Fatalf("go transaction emulation failed: %v", err)
	}
	refRes, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, configRoot)
	if err != nil {
		t.Fatalf("reference transaction emulation failed: %v", err)
	}

	if goRes.StartLT != 0 {
		t.Fatalf("go start LT = %d, want wrapped zero", goRes.StartLT)
	}
	var refTx tlb.Transaction
	if err = tlb.LoadFromCell(&refTx, refRes.txCell.MustBeginParse()); err != nil {
		t.Fatalf("failed to decode reference transaction: %v", err)
	}
	if refTx.LT != 0 {
		t.Fatalf("reference start LT = %d, want wrapped zero", refTx.LT)
	}
	if goRes.GasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.GasUsed, refRes.gasUsed)
	}
	if !bytes.Equal(goRes.TransactionCell.Hash(), refRes.txCell.Hash()) {
		t.Fatalf("transaction hash mismatch:\ngo=%s\nreference=%s", transactionCrossTxSummary(t, goRes.TransactionCell), transactionCrossTxSummary(t, refRes.txCell))
	}
	if !bytes.Equal(goRes.NextAccount.ShardAccountCell().Hash(), refRes.shardCell.Hash()) {
		t.Fatalf("shard account hash mismatch:\ngo=%s\nreference=%s", transactionCrossShardSummary(t, goRes.NextAccount.ShardAccountCell()), transactionCrossShardSummary(t, refRes.shardCell))
	}
}
