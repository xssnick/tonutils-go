//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"os/exec"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const transactionMalformedLibraryReferenceHelper = "TVM_MALFORMED_LIBRARY_REFERENCE_HELPER"

func TestTVMCrossEmulatorTransactionChangedMalformedAccountLibrary(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	lib := transactionLibraryWithHashPrefix(t, false)
	validLeafDict := cell.NewDict(255)
	key := cell.BeginCell().MustStoreSlice(make([]byte, 32), 255).EndCell()
	value := cell.BeginCell().MustStoreBoolBit(false).MustStoreRef(cell.BeginCell().EndCell()).EndCell()
	if err := validLeafDict.Set(key, value); err != nil {
		t.Fatal(err)
	}
	malformedLibraries := cell.BeginCell().
		MustStoreUInt(0, 2). // empty root label, followed by a fork
		MustStoreRef(validLeafDict.AsCell()).
		MustStoreRef(cell.BeginCell().EndCell()). // invalid right fork subtree
		EndCell().AsDict(256)

	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	actions := buildTransactionActionList(t, tlb.ActionChangeLibrary{
		Mode:   2,
		LibRef: tlb.LibRefRef{Library: lib},
	})
	code := makeTransactionInternalActionsCode(t, actions, newData)
	addr := address.NewAddress(0, 0xFF, cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell().Hash())
	now := uint32(tonopsTestTime.Unix())
	account, err := tlb.ToCell(&tlb.AccountState{
		IsValid: true,
		Address: addr,
		StorageInfo: tlb.StorageInfo{
			StorageUsed: tlb.StorageUsed{
				CellsUsed: big.NewInt(0),
				BitsUsed:  big.NewInt(0),
			},
			StorageExtra: tlb.StorageExtraNone{},
			LastPaid:     now,
		},
		AccountStorage: tlb.AccountStorage{
			Balance: tlb.FromNanoTONU(walletSendTestBalance),
			Status:  tlb.AccountStatusActive,
			StateInit: &tlb.StateInit{
				Code: code,
				Data: cell.BeginCell().EndCell(),
				Lib:  malformedLibraries,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	shard := &tlb.ShardAccount{Account: account, LastTransHash: make([]byte, 32)}
	msg := mustTransactionMsgCell(t, &tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     internalEmulationSrcAddr,
		DstAddr:     addr,
		Amount:      tlb.FromNanoTONU(1_000_000_000),
	})
	config := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), 14)

	params := testTxParams{
		Address:     addr,
		Now:         now,
		BlockLT:     transactionTestLogicalTime,
		LogicalTime: transactionTestLogicalTime,
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		ConfigRoot:  config,
	}
	if os.Getenv(transactionMalformedLibraryReferenceHelper) == "1" {
		_, err = runReferenceOrdinaryTransactionWithConfigRoot(shard, msg, now, uint64(transactionTestLogicalTime), tonopsTestSeed, config)
		t.Fatalf("malformed-library reference call returned instead of aborting: %v", err)
	}

	if _, goErr := testEmulateTransaction(NewTVM(), shard, msg, params); goErr == nil {
		t.Fatal("Go accepted the malformed library mutation")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestTVMCrossEmulatorTransactionChangedMalformedAccountLibrary$")
	cmd.Env = append(os.Environ(), transactionMalformedLibraryReferenceHelper+"=1")
	output, refErr := cmd.CombinedOutput()
	if refErr == nil || !bytes.Contains(output, []byte("vm::VmError")) {
		t.Fatalf("reference result = %v, want uncaught VM error; output:\n%s", refErr, output)
	}
}

func transactionLibraryWithHashPrefix(t *testing.T, high bool) *cell.Cell {
	t.Helper()

	for tag := uint64(0); ; tag++ {
		lib := cell.BeginCell().MustStoreUInt(tag, 32).EndCell()
		if (lib.Hash()[0]&0x80 != 0) == high {
			return lib
		}
	}
}
