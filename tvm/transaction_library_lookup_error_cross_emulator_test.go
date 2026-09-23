//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTVMCrossEmulatorTransactionLibraryLookupError(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}
	data := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	actions := buildTransactionActionList(t, tlb.ActionChangeLibrary{Mode: 18, LibRef: tlb.LibRefHash{LibHash: make([]byte, 32)}})
	code := makeTransactionInternalActionsCode(t, actions, data)
	now := uint32(tonopsTestTime.Unix())
	shard := buildTransactionTestShardAccount(t, tonopsTestAddr, code, data, walletSendTestBalance, now)
	state := mustParseTransactionTestAccount(t, shard)
	state.StateInit.Lib = cell.BeginCell().EndCell().AsDict(256)
	var err error
	shard.Account, err = tlb.ToCell(state)
	if err != nil {
		t.Fatal(err)
	}
	msg := buildTransactionOutboundInternalCellWithAddresses(t, internalEmulationSrcAddr, tonopsTestAddr, 1_000_000_000, data)
	config := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), 14)
	ref, err := runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, config)
	if err != nil {
		t.Fatal(err)
	}
	want := transactionActionPhaseExpectation{valid: true, resultCode: 42}
	assertOrdinaryTransactionActionPhase(t, "reference", ref.txCell, want)
	got, err := testEmulateTransaction(NewTVM(), shard, msg, testTxParams{
		Address: tonopsTestAddr, Now: now, BlockLT: transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime, RandSeed: tonopsTestSeed, ConfigRoot: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertOrdinaryTransactionActionPhase(t, "go", got.TransactionCell, want)
	if !bytes.Equal(got.TransactionCell.Hash(), ref.txCell.Hash()) || !bytes.Equal(got.NextAccount.ShardAccountCell().Hash(), ref.shardCell.Hash()) {
		t.Fatal("library lookup failure differs from reference transaction or account hash")
	}
}
